package inventory

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/netip"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
	"sigs.k8s.io/yaml"

	"github.com/kinstall/kinstall/internal/domain"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// HostImportRow is the standardized bulk-import schema across .xlsx / CSV / YAML / JSON / Ansible INI (05 §4.4).
type HostImportRow struct {
	RowNumber    int               `json:"rowNumber" yaml:"-"`
	Name         string            `json:"name" yaml:"name"`
	Address      string            `json:"address" yaml:"address"`
	Port         int               `json:"port" yaml:"port"`
	User         string            `json:"user" yaml:"user"`
	Credential   string            `json:"credential" yaml:"credential"`
	JumpHosts    []string          `json:"jumpHosts,omitempty" yaml:"jump_hosts,omitempty"`
	Group        string            `json:"group,omitempty" yaml:"group,omitempty"`
	Biz          string            `json:"biz,omitempty" yaml:"biz,omitempty"`
	Set          string            `json:"set,omitempty" yaml:"set,omitempty"`
	Module       string            `json:"module,omitempty" yaml:"module,omitempty"`
	Region       string            `json:"region,omitempty" yaml:"region,omitempty"`
	SecurityArea string            `json:"securityArea,omitempty" yaml:"security_area,omitempty"`
	Zone         string            `json:"zone,omitempty" yaml:"zone,omitempty"`
	Rack         string            `json:"rack,omitempty" yaml:"rack,omitempty"`
	Arch         string            `json:"arch,omitempty" yaml:"arch,omitempty"`
	PageSize     int               `json:"pageSize,omitempty" yaml:"page_size,omitempty"`
	Owner        string            `json:"owner,omitempty" yaml:"owner,omitempty"`
	AssetTag     string            `json:"assetTag,omitempty" yaml:"asset_tag,omitempty"`
	Labels       map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
	HostKeyFP    string            `json:"hostkeyFp,omitempty" yaml:"hostkey_fp,omitempty"`
}

// ParseOptions customizes multi-format CMDB parsing & column header mapping.
type ParseOptions struct {
	ColumnMap        map[string]string `json:"columnMap,omitempty"`
	SanitizeHostname bool              `json:"sanitizeHostname,omitempty"`
}

// ImportRowError pinpoints exact row-level validation failures (T-M1-04).
type ImportRowError struct {
	RowNumber int    `json:"rowNumber"`
	Field     string `json:"field"`
	Code      string `json:"code"`
	Message   string `json:"message"`
}

// ImportResult holds parsed rows plus row-level dry-run validation errors and detected headers.
type ImportResult struct {
	Rows            []HostImportRow  `json:"rows"`
	Errors          []ImportRowError `json:"errors"`
	DetectedHeaders []string         `json:"detectedHeaders,omitempty"`
}

// MergedLabels merges explicit row labels with canonical CMDB topology attributes.
func (r HostImportRow) MergedLabels() map[string]string {
	out := make(map[string]string, len(r.Labels)+8)
	for k, v := range r.Labels {
		if strings.TrimSpace(k) != "" && strings.TrimSpace(v) != "" {
			out[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	setIf := func(k, v string) {
		if tv := strings.TrimSpace(v); tv != "" {
			out[k] = tv
		}
	}
	setIf(domain.LabelCMDBBiz, r.Biz)
	setIf(domain.LabelCMDBSet, r.Set)
	mod := r.Module
	if mod == "" {
		mod = r.Group
	}
	setIf(domain.LabelCMDBModule, mod)
	setIf(domain.LabelCMDBRegion, r.Region)
	setIf(domain.LabelCMDBSecurityArea, r.SecurityArea)
	setIf(domain.LabelCMDBOwner, r.Owner)
	setIf(domain.LabelCMDBAssetTag, r.AssetTag)
	return out
}

// ParseHosts dispatches universal CMDB import across xlsx / csv / yaml / json (incl. LiveOps & HCS) / ansible INI.
func ParseHosts(format string, raw []byte, opts ParseOptions) (*ImportResult, error) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "xlsx":
		return ParseXLSXWithOptions(bytes.NewReader(raw), opts)
	case "yaml", "yml":
		return ParseYAML(raw)
	case "json", "liveops_json", "http_json", "huawei_hcs":
		return ParseJSONHosts(raw, opts)
	case "ansible", "ansible_ini", "ini":
		return ParseAnsibleINI(raw, opts)
	default:
		return ParseCSVWithOptions(bytes.NewReader(raw), opts)
	}
}

// ParseXLSX parses an Excel workbook (.xlsx) via excelize v2.11.1 (05 §4.4, T-M1-04).
func ParseXLSX(r io.Reader) (*ImportResult, error) {
	return ParseXLSXWithOptions(r, ParseOptions{})
}

// ParseXLSXWithOptions parses an Excel workbook (.xlsx) with Chinese/English/LiveOps header detection and optional column mapping.
func ParseXLSXWithOptions(r io.Reader, opts ParseOptions) (*ImportResult, error) {
	f, err := excelize.OpenReader(r)
	if err != nil {
		return nil, fmt.Errorf("open xlsx: %w", err)
	}
	defer func() { _ = f.Close() }()
	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil, fmt.Errorf("xlsx contains no sheets")
	}
	rows, err := f.GetRows(sheets[0])
	if err != nil {
		return nil, fmt.Errorf("read xlsx rows: %w", err)
	}
	return parseTabularRowsWithOptions(rows, opts), nil
}

// ParseCSV parses standard CSV host import sheets (with automatic UTF-8 BOM stripping and Chinese/English headers).
func ParseCSV(r io.Reader) (*ImportResult, error) {
	return ParseCSVWithOptions(r, ParseOptions{})
}

// ParseCSVWithOptions parses CSV host sheets with custom column mapping.
func ParseCSVWithOptions(r io.Reader, opts ParseOptions) (*ImportResult, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read csv: %w", err)
	}
	raw = bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})
	cr := csv.NewReader(bytes.NewReader(raw))
	cr.FieldsPerRecord = -1
	rows, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("read csv: %w", err)
	}
	return parseTabularRowsWithOptions(rows, opts), nil
}

// ParseYAML parses YAML host lists.
func ParseYAML(raw []byte) (*ImportResult, error) {
	raw = bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})
	var rows []HostImportRow
	if err := yaml.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("parse yaml hosts: %w", err)
	}
	for i := range rows {
		rows[i].RowNumber = i + 1
		if rows[i].Port == 0 {
			rows[i].Port = 22
		}
		if rows[i].User == "" {
			rows[i].User = "root"
		}
	}
	return validateRows(rows), nil
}

// ParseJSONHosts parses generic JSON arrays, {hosts:[...]}, {data:[...]}, LiveOps ops-cmdb host lists,
// and Huawei HCS/OpenStack {servers:[...]} exports.
func ParseJSONHosts(raw []byte, opts ParseOptions) (*ImportResult, error) {
	raw = bytes.TrimSpace(bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF}))
	if len(raw) == 0 {
		return &ImportResult{}, nil
	}
	var items []map[string]any
	if raw[0] == '[' {
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, fmt.Errorf("parse json host array: %w", err)
		}
	} else {
		var envelope map[string]any
		if err := json.Unmarshal(raw, &envelope); err != nil {
			return nil, fmt.Errorf("parse json host object: %w", err)
		}
		for _, key := range []string{"hosts", "items", "data", "servers", "list", "rows", "records"} {
			if sub, ok := envelope[key]; ok {
				switch v := sub.(type) {
				case []any:
					for _, el := range v {
						if m, ok := el.(map[string]any); ok {
							items = append(items, m)
						}
					}
				case map[string]any:
					for _, nested := range []string{"list", "items", "records", "rows", "hosts"} {
						if arr, ok := v[nested].([]any); ok {
							for _, el := range arr {
								if m, ok := el.(map[string]any); ok {
									items = append(items, m)
								}
							}
						}
					}
				}
			}
			if len(items) > 0 {
				break
			}
		}
		if len(items) == 0 && (envelope["name"] != nil || envelope["host_name"] != nil || envelope["address"] != nil || envelope["host_innerip"] != nil) {
			items = append(items, envelope)
		}
	}

	var parsed []HostImportRow
	headerSet := make(map[string]bool)
	for i, m := range items {
		for k := range m {
			headerSet[k] = true
		}
		row := mapObjectToHostRow(i+1, m, opts)
		parsed = append(parsed, row)
	}
	res := validateRows(parsed)
	for h := range headerSet {
		res.DetectedHeaders = append(res.DetectedHeaders, h)
	}
	return res, nil
}

func mapObjectToHostRow(rowNum int, m map[string]any, opts ParseOptions) HostImportRow {
	norm := make(map[string]string, len(m))
	for k, v := range m {
		canon := canonicalColumnName(k, opts.ColumnMap)
		if canon == "" {
			continue
		}
		if _, exists := norm[canon]; exists && norm[canon] != "" {
			continue
		}
		if s := stringifyAny(v); s != "" {
			norm[canon] = s
		}
	}
	// Huawei HCS / OpenStack Nova server extraction for addresses & metadata
	if norm["address"] == "" {
		if addrs, ok := m["addresses"].(map[string]any); ok {
			for _, v := range addrs {
				if arr, ok := v.([]any); ok && len(arr) > 0 {
					if entry, ok := arr[0].(map[string]any); ok {
						if ip := stringifyAny(entry["addr"]); ip != "" {
							norm["address"] = ip
							break
						}
					}
				}
			}
		}
	}
	if norm["zone"] == "" {
		if az := stringifyAny(m["OS-EXT-AZ:availability_zone"]); az != "" {
			norm["zone"] = az
		}
	}

	labels := parseLabelsKV(norm["labels"])
	if rawLabels, ok := m["labels"].(map[string]any); ok {
		for k, v := range rawLabels {
			if sv := stringifyAny(v); sv != "" {
				labels[k] = sv
			}
		}
	}
	if meta, ok := m["metadata"].(map[string]any); ok {
		for k, v := range meta {
			if sv := stringifyAny(v); sv != "" {
				labels[k] = sv
			}
		}
	}

	name := norm["name"]
	if name == "" && norm["address"] != "" {
		name = "host-" + strings.ReplaceAll(norm["address"], ".", "-")
	}
	if opts.SanitizeHostname {
		name = SanitizeDNS1123(name)
	}
	port := parsePortOrDefault(norm["port"], 22)
	user := norm["user"]
	if user == "" {
		user = "root"
	}
	pageSize := parsePageSize(norm["page_size"])

	return HostImportRow{
		RowNumber:    rowNum,
		Name:         name,
		Address:      norm["address"],
		Port:         port,
		User:         user,
		Credential:   norm["credential"],
		JumpHosts:    splitComma(norm["jump_hosts"]),
		Group:        norm["module"],
		Biz:          norm["biz"],
		Set:          norm["set"],
		Module:       norm["module"],
		Region:       norm["region"],
		SecurityArea: norm["security_area"],
		Zone:         norm["zone"],
		Rack:         norm["rack"],
		Arch:         normalizeArch(norm["arch"]),
		PageSize:     pageSize,
		Owner:        norm["owner"],
		AssetTag:     norm["asset_tag"],
		Labels:       labels,
		HostKeyFP:    norm["hostkey_fp"],
	}
}

// ParseAnsibleINI parses standard Ansible INI inventory files ([group] + host key=val pairs).
func ParseAnsibleINI(raw []byte, opts ParseOptions) (*ImportResult, error) {
	raw = bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})
	sc := bufio.NewScanner(bytes.NewReader(raw))
	currentGroup := ""
	skipSection := false
	lineNo := 0
	var parsed []HostImportRow
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			sec := strings.TrimSpace(line[1 : len(line)-1])
			if strings.Contains(sec, ":vars") || strings.Contains(sec, ":children") {
				skipSection = true
			} else {
				skipSection = false
				currentGroup = sec
			}
			continue
		}
		if skipSection {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		hostToken := fields[0]
		vars := make(map[string]string)
		for _, tok := range fields[1:] {
			if k, v, ok := strings.Cut(tok, "="); ok {
				vars[strings.ToLower(strings.TrimSpace(k))] = strings.Trim(strings.TrimSpace(v), `"'`)
			}
		}
		addr := vars["ansible_host"]
		if addr == "" {
			addr = vars["ansible_ssh_host"]
		}
		name := hostToken
		if addr == "" {
			// If token itself is an IP address, generate DNS-1123 host name
			if _, perr := netip.ParseAddr(hostToken); perr == nil {
				addr = hostToken
				name = "node-" + strings.ReplaceAll(hostToken, ".", "-")
			} else {
				addr = hostToken
			}
		}
		if opts.SanitizeHostname {
			name = SanitizeDNS1123(name)
		}
		portStr := vars["ansible_port"]
		if portStr == "" {
			portStr = vars["ansible_ssh_port"]
		}
		user := vars["ansible_user"]
		if user == "" {
			user = vars["ansible_ssh_user"]
		}
		if user == "" {
			user = "root"
		}
		mod := vars["module"]
		if mod == "" {
			mod = currentGroup
		}
		labels := make(map[string]string)
		for k, v := range vars {
			if !strings.HasPrefix(k, "ansible_") && k != "biz" && k != "set" && k != "module" && k != "region" && k != "security_area" && k != "zone" && k != "rack" && k != "arch" && k != "owner" && k != "credential" {
				labels[k] = v
			}
		}
		parsed = append(parsed, HostImportRow{
			RowNumber:    lineNo,
			Name:         name,
			Address:      addr,
			Port:         parsePortOrDefault(portStr, 22),
			User:         user,
			Credential:   vars["credential"],
			Group:        currentGroup,
			Biz:          vars["biz"],
			Set:          vars["set"],
			Module:       mod,
			Region:       vars["region"],
			SecurityArea: vars["security_area"],
			Zone:         vars["zone"],
			Rack:         vars["rack"],
			Arch:         normalizeArch(vars["arch"]),
			Owner:        vars["owner"],
			Labels:       labels,
		})
	}
	res := validateRows(parsed)
	res.DetectedHeaders = []string{"ansible_host", "ansible_port", "ansible_user", "group"}
	return res, nil
}

var headerAliasMap = map[string]string{
	// name
	"name": "name", "hostname": "name", "host_name": "name", "host": "name",
	"主机名": "name", "主机名称": "name", "节点名": "name", "节点名称": "name", "服务器名": "name", "机器名": "name",
	// address
	"address": "address", "ip": "address", "inner_ip": "address", "host_innerip": "address",
	"private_ip": "address", "ssh_host": "address", "ansible_host": "address", "innerip": "address",
	"内网ip": "address", "ip地址": "address", "管理ip": "address", "主机ip": "address", "地址": "address", "服务器ip": "address",
	// port
	"port": "port", "ssh_port": "port", "sshport": "port", "ansible_port": "port",
	"端口": "port", "ssh端口": "port",
	// user
	"user": "user", "username": "user", "ssh_user": "user", "sshuser": "user", "ansible_user": "user",
	"用户": "user", "用户名": "user", "ssh用户": "user", "登录用户": "user",
	// credential
	"credential": "credential", "credential_id": "credential", "credential_name": "credential", "cred": "credential",
	"凭据": "credential", "ssh凭据": "credential", "凭据名": "credential", "凭据引用": "credential", "密钥": "credential",
	// biz
	"biz": "biz", "business": "biz", "biz_name": "biz", "app": "biz", "appid": "biz", "system": "biz",
	"业务": "biz", "业务系统": "biz", "所属业务": "biz", "应用系统": "biz",
	// set
	"set": "set", "cluster": "set", "env": "set", "set_name": "set", "environment": "set",
	"集群": "set", "环境": "set", "业务集群": "set", "集群环境": "set", "部署环境": "set",
	// module / group
	"module": "module", "group": "module", "module_name": "module", "group_name": "module", "role": "module",
	"模块": "module", "业务模块": "module", "分组": "module", "主机组": "module", "角色": "module",
	// region / idc
	"region": "region", "idc": "region", "datacenter": "region", "dc": "region", "region_name": "region",
	"区域": "region", "机房": "region", "区域机房": "region", "数据中心": "region", "物理区域": "region",
	// security_area
	"security_area": "security_area", "securityarea": "security_area", "sec_area": "security_area", "network_zone": "security_area", "vpc": "security_area",
	"安全域": "security_area", "网络区域": "security_area", "安全区": "security_area",
	// fault_zone
	"zone": "zone", "az": "zone", "fault_zone": "zone", "faultzone": "zone", "availability_zone": "zone",
	"故障域": "zone", "可用区": "zone",
	// fault_rack
	"rack": "rack", "cabinet": "rack", "fault_rack": "rack", "faultrack": "rack",
	"机架": "rack", "机柜": "rack",
	// arch
	"arch": "arch", "cpu_arch": "arch", "architecture": "arch", "os_arch": "arch",
	"架构": "arch", "cpu架构": "arch",
	// page_size
	"page_size": "page_size", "pagesize": "page_size",
	"页大小": "page_size", "内核页大小": "page_size",
	// owner
	"owner": "owner", "operator": "owner", "maintainer": "owner", "created_by": "owner",
	"负责人": "owner", "运维负责人": "owner", "管理员": "owner",
	// asset_tag
	"asset_tag": "asset_tag", "assettag": "asset_tag", "sn": "asset_tag", "serial": "asset_tag", "asset_id": "asset_tag",
	"资产编号": "asset_tag", "资产号": "asset_tag", "序列号": "asset_tag",
	// labels
	"labels": "labels", "tags": "labels", "标签": "labels",
	// jump_hosts
	"jump_hosts": "jump_hosts", "jumphosts": "jump_hosts", "bastion": "jump_hosts", "跳板机": "jump_hosts",
	// hostkey_fp
	"hostkey_fp": "hostkey_fp", "hostkeyfp": "hostkey_fp", "fingerprint": "hostkey_fp", "主机指纹": "hostkey_fp",
}

var parenKeyRe = regexp.MustCompile(`\(([a-zA-Z0-9_]+)\)`)

func canonicalColumnName(rawHeader string, customMap map[string]string) string {
	clean := strings.TrimSpace(strings.TrimPrefix(rawHeader, "\uFEFF"))
	if clean == "" {
		return ""
	}
	if len(customMap) > 0 {
		if mapped, ok := customMap[clean]; ok && strings.TrimSpace(mapped) != "" {
			return strings.ToLower(strings.TrimSpace(mapped))
		}
		if mapped, ok := customMap[strings.ToLower(clean)]; ok && strings.TrimSpace(mapped) != "" {
			return strings.ToLower(strings.TrimSpace(mapped))
		}
	}
	low := strings.ToLower(strings.Trim(clean, "* \t"))
	if canon, ok := headerAliasMap[low]; ok {
		return canon
	}
	// Support dual headers like "主机名(name)" or "内网IP(address)"
	if m := parenKeyRe.FindStringSubmatch(clean); len(m) == 2 {
		if canon, ok := headerAliasMap[strings.ToLower(m[1])]; ok {
			return canon
		}
	}
	// Strip parenthesized hint e.g. "主机名(必填)" -> "主机名"
	if idx := strings.IndexAny(low, "(（"); idx > 0 {
		prefix := strings.TrimSpace(low[:idx])
		if canon, ok := headerAliasMap[prefix]; ok {
			return canon
		}
	}
	return low
}

func parseTabularRowsWithOptions(records [][]string, opts ParseOptions) *ImportResult {
	if len(records) < 2 {
		res := &ImportResult{}
		if len(records) == 1 {
			for _, h := range records[0] {
				if t := strings.TrimSpace(h); t != "" {
					res.DetectedHeaders = append(res.DetectedHeaders, t)
				}
			}
		}
		return res
	}
	colIdx := make(map[string]int)
	detected := make([]string, 0, len(records[0]))
	for i, h := range records[0] {
		rawH := strings.TrimSpace(strings.TrimPrefix(h, "\uFEFF"))
		if rawH != "" {
			detected = append(detected, rawH)
		}
		canon := canonicalColumnName(rawH, opts.ColumnMap)
		if canon != "" {
			if _, exists := colIdx[canon]; !exists {
				colIdx[canon] = i
			}
		}
	}
	cell := func(row []string, key string) string {
		idx, ok := colIdx[key]
		if !ok || idx >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[idx])
	}
	var parsed []HostImportRow
	for i := 1; i < len(records); i++ {
		rec := records[i]
		if len(rec) == 0 || (cell(rec, "name") == "" && cell(rec, "address") == "") {
			continue
		}
		port := parsePortOrDefault(cell(rec, "port"), 22)
		user := cell(rec, "user")
		if user == "" {
			user = "root"
		}
		name := cell(rec, "name")
		if opts.SanitizeHostname {
			name = SanitizeDNS1123(name)
		}
		mod := cell(rec, "module")
		parsed = append(parsed, HostImportRow{
			RowNumber:    i + 1, // 1-based sheet row including header
			Name:         name,
			Address:      cell(rec, "address"),
			Port:         port,
			User:         user,
			Credential:   cell(rec, "credential"),
			JumpHosts:    splitComma(cell(rec, "jump_hosts")),
			Group:        mod,
			Biz:          cell(rec, "biz"),
			Set:          cell(rec, "set"),
			Module:       mod,
			Region:       cell(rec, "region"),
			SecurityArea: cell(rec, "security_area"),
			Zone:         cell(rec, "zone"),
			Rack:         cell(rec, "rack"),
			Arch:         normalizeArch(cell(rec, "arch")),
			PageSize:     parsePageSize(cell(rec, "page_size")),
			Owner:        cell(rec, "owner"),
			AssetTag:     cell(rec, "asset_tag"),
			Labels:       parseLabelsKV(cell(rec, "labels")),
			HostKeyFP:    cell(rec, "hostkey_fp"),
		})
	}
	res := validateRows(parsed)
	res.DetectedHeaders = detected
	return res
}

func parsePortOrDefault(ps string, def int) int {
	ps = strings.TrimSpace(ps)
	if ps == "" {
		return def
	}
	if p, err := strconv.Atoi(ps); err == nil {
		return p
	}
	return -1
}

func parsePageSize(ps string) int {
	low := strings.ToLower(strings.TrimSpace(ps))
	switch low {
	case "":
		return 0
	case "4k", "4096":
		return 4096
	case "64k", "65536":
		return 65536
	default:
		if v, err := strconv.Atoi(low); err == nil && v > 0 {
			return v
		}
		return 0
	}
}

func normalizeArch(a string) string {
	switch strings.ToLower(strings.TrimSpace(a)) {
	case "":
		return ""
	case "amd64", "x86_64", "x86", "x64":
		return "amd64"
	case "arm64", "aarch64", "kunpeng", "鲲鹏", "飞腾":
		return "arm64"
	default:
		return strings.ToLower(strings.TrimSpace(a))
	}
}

// SanitizeDNS1123 converts external CMDB hostnames (e.g., underscores, dots, uppercase) into valid DNS-1123 names.
func SanitizeDNS1123(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	var b strings.Builder
	for _, r := range s {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-':
			b.WriteRune(r)
		case r == '_' || r == '.' || r == ' ':
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 63 {
		out = strings.TrimRight(out[:63], "-")
	}
	return out
}

func splitComma(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func stringifyAny(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(x)
	case float64:
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case bool:
		if x {
			return "true"
		}
		return "false"
	default:
		return ""
	}
}

func validateRows(rows []HostImportRow) *ImportResult {
	res := &ImportResult{Rows: make([]HostImportRow, 0, len(rows))}
	seenNames := make(map[string]int)
	seenAddrs := make(map[string]int)
	for _, r := range rows {
		ok := true
		if !domain.ValidateDNS1123(r.Name) {
			res.Errors = append(res.Errors, ImportRowError{RowNumber: r.RowNumber, Field: "name", Code: sdk.CodeHostInvalidName, Message: fmt.Sprintf("invalid DNS-1123 hostname %q", r.Name)})
			ok = false
		} else if prev, dup := seenNames[r.Name]; dup {
			res.Errors = append(res.Errors, ImportRowError{RowNumber: r.RowNumber, Field: "name", Code: sdk.CodeHostInvalidName, Message: fmt.Sprintf("duplicate hostname %q (first seen at row %d)", r.Name, prev)})
			ok = false
		} else {
			seenNames[r.Name] = r.RowNumber
		}
		if r.Address == "" {
			res.Errors = append(res.Errors, ImportRowError{RowNumber: r.RowNumber, Field: "address", Code: sdk.CodeAPIInvalidRequest, Message: "missing SSH address"})
			ok = false
		}
		if r.Port <= 0 || r.Port > 65535 {
			res.Errors = append(res.Errors, ImportRowError{RowNumber: r.RowNumber, Field: "port", Code: sdk.CodeAPIInvalidRequest, Message: fmt.Sprintf("invalid SSH port %d", r.Port)})
			ok = false
		} else if r.Address != "" {
			addrKey := fmt.Sprintf("%s:%d", r.Address, r.Port)
			if prev, dup := seenAddrs[addrKey]; dup {
				res.Errors = append(res.Errors, ImportRowError{RowNumber: r.RowNumber, Field: "address", Code: sdk.CodeHostInvalidName, Message: fmt.Sprintf("duplicate address %s (first seen at row %d)", addrKey, prev)})
				ok = false
			} else {
				seenAddrs[addrKey] = r.RowNumber
			}
		}
		if ok {
			res.Rows = append(res.Rows, r)
		}
	}
	return res
}

func parseLabelsKV(s string) map[string]string {
	out := make(map[string]string)
	for _, pair := range strings.Split(s, ";") {
		k, v, ok := strings.Cut(strings.TrimSpace(pair), "=")
		if ok && strings.TrimSpace(k) != "" {
			out[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return out
}

// CheckClaimConflicts validates proposed claims against existing claims on a host,
// blocking port collisions, overlapping paths, Docker Bridge CIDR collisions, and memory overcommit (T-M1-05, D2 §1.3, R2).
func CheckClaimConflicts(host domain.Host, existing []domain.HostClaim, proposed sdk.HostRoleClaims, ignoreInstanceID string) error {
	for _, ex := range existing {
		if ignoreInstanceID != "" && ex.InstanceID == ignoreInstanceID {
			continue
		}
		// 1. Port + protocol collision (e.g. R2 SonarQube vs Attu port 9000)
		for _, ep := range ex.Ports {
			eProto := normProto(ep.Proto)
			for _, pp := range proposed.Ports {
				if ep.Port == pp.Port && eProto == normProto(pp.Proto) {
					return sdk.NewError(sdk.CodeHostClaimConflict, "HOST", 409, sdk.CodeHostClaimConflict, false,
						fmt.Errorf("host %s port %d/%s already claimed by instance %s", host.Name, pp.Port, eProto, ex.InstanceID))
				}
			}
		}
		// 2. Data directory overlap (exact or parent/child prefix)
		for _, ed := range ex.Paths {
			for _, pd := range proposed.Dirs {
				if pathsOverlap(ed, pd) {
					return sdk.NewError(sdk.CodeHostClaimConflict, "HOST", 409, sdk.CodeHostClaimConflict, false,
						fmt.Errorf("host %s path %q overlaps %q claimed by instance %s", host.Name, pd, ed, ex.InstanceID))
				}
			}
		}
		// 3. Docker Bridge subnet CIDR collision (D2 §1.3 subnets_json / R2)
		for _, es := range ex.Subnets {
			epfx, err1 := netip.ParsePrefix(es)
			if err1 != nil {
				continue
			}
			for _, ps := range proposed.Subnets {
				ppfx, err2 := netip.ParsePrefix(ps)
				if err2 != nil {
					continue
				}
				if epfx.Overlaps(ppfx) {
					return sdk.NewError(sdk.CodeHostClaimConflict, "HOST", 409, sdk.CodeHostClaimConflict, false,
						fmt.Errorf("host %s Docker subnet %s overlaps %s claimed by instance %s", host.Name, ps, es, ex.InstanceID))
				}
			}
		}
	}

	// 4. Proposed Docker subnets vs host physical NIC subnets
	if host.Facts != nil {
		for _, nic := range host.Facts.NICs {
			for _, addr := range nic.Addresses {
				npfx, err := netip.ParsePrefix(addr)
				if err != nil {
					continue
				}
				for _, ps := range proposed.Subnets {
					ppfx, perr := netip.ParsePrefix(ps)
					if perr == nil && npfx.Overlaps(ppfx) {
						return sdk.NewError(sdk.CodeHostClaimConflict, "HOST", 409, sdk.CodeHostClaimConflict, false,
							fmt.Errorf("host %s Docker subnet %s overlaps host NIC %s (%s)", host.Name, ps, nic.Name, addr))
					}
				}
			}
		}
		// 5. Memory capacity budget check
		if host.Facts.MemMB > 0 && proposed.MemoryMB > 0 {
			usedMB := proposed.MemoryMB
			for _, ex := range existing {
				if ignoreInstanceID == "" || ex.InstanceID != ignoreInstanceID {
					usedMB += ex.MemMB
				}
			}
			if usedMB > host.Facts.MemMB {
				return sdk.NewError(sdk.CodeHostClaimConflict, "HOST", 409, sdk.CodeHostClaimConflict, false,
					fmt.Errorf("host %s memory overcommitted: requested total %d MiB > host %d MiB", host.Name, usedMB, host.Facts.MemMB))
			}
		}
	}
	return nil
}

func normProto(p string) string {
	if p == "" {
		return "tcp"
	}
	return strings.ToLower(p)
}

func pathsOverlap(a, b string) bool {
	ca, cb := path.Clean(a), path.Clean(b)
	if ca == cb {
		return true
	}
	return strings.HasPrefix(ca+"/", cb+"/") || strings.HasPrefix(cb+"/", ca+"/")
}

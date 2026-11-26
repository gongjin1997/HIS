package inventory

import (
	"encoding/csv"
	"fmt"
	"io"
	"net/netip"
	"path"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
	"sigs.k8s.io/yaml"

	"github.com/kinstall/kinstall/internal/domain"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// HostImportRow is the standardized bulk-import schema across .xlsx / CSV / YAML (05 §4.4).
type HostImportRow struct {
	RowNumber  int               `json:"rowNumber" yaml:"-"`
	Name       string            `json:"name" yaml:"name"`
	Address    string            `json:"address" yaml:"address"`
	Port       int               `json:"port" yaml:"port"`
	User       string            `json:"user" yaml:"user"`
	Credential string            `json:"credential" yaml:"credential"`
	JumpHosts  []string          `json:"jumpHosts,omitempty" yaml:"jump_hosts,omitempty"`
	Group      string            `json:"group,omitempty" yaml:"group,omitempty"`
	Zone       string            `json:"zone,omitempty" yaml:"zone,omitempty"`
	Rack       string            `json:"rack,omitempty" yaml:"rack,omitempty"`
	Labels     map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
	HostKeyFP  string            `json:"hostkeyFp,omitempty" yaml:"hostkey_fp,omitempty"`
}

// ImportRowError pinpoints exact row-level validation failures (T-M1-04).
type ImportRowError struct {
	RowNumber int    `json:"rowNumber"`
	Field     string `json:"field"`
	Code      string `json:"code"`
	Message   string `json:"message"`
}

// ImportResult holds parsed rows plus row-level dry-run validation errors.
type ImportResult struct {
	Rows   []HostImportRow  `json:"rows"`
	Errors []ImportRowError `json:"errors"`
}

// ParseXLSX parses an Excel workbook (.xlsx) via excelize v2.11.1 (05 §4.4, T-M1-04).
func ParseXLSX(r io.Reader) (*ImportResult, error) {
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
	return parseTabularRows(rows), nil
}

// ParseCSV parses standard CSV host import sheets.
func ParseCSV(r io.Reader) (*ImportResult, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	rows, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("read csv: %w", err)
	}
	return parseTabularRows(rows), nil
}

// ParseYAML parses YAML host lists.
func ParseYAML(raw []byte) (*ImportResult, error) {
	var rows []HostImportRow
	if err := yaml.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("parse yaml hosts: %w", err)
	}
	for i := range rows {
		rows[i].RowNumber = i + 1
		if rows[i].Port == 0 {
			rows[i].Port = 22
		}
	}
	return validateRows(rows), nil
}

func parseTabularRows(records [][]string) *ImportResult {
	if len(records) < 2 {
		return &ImportResult{}
	}
	colIdx := make(map[string]int)
	for i, h := range records[0] {
		colIdx[strings.ToLower(strings.TrimSpace(h))] = i
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
		port := 22
		if ps := cell(rec, "port"); ps != "" {
			if p, err := strconv.Atoi(ps); err == nil {
				port = p
			} else {
				port = -1
			}
		}
		user := cell(rec, "user")
		if user == "" {
			user = "root"
		}
		var jumps []string
		if j := cell(rec, "jump_hosts"); j != "" {
			for _, part := range strings.Split(j, ",") {
				if p := strings.TrimSpace(part); p != "" {
					jumps = append(jumps, p)
				}
			}
		}
		labels := parseLabelsKV(cell(rec, "labels"))
		parsed = append(parsed, HostImportRow{
			RowNumber:  i + 1, // 1-based sheet row including header
			Name:       cell(rec, "name"),
			Address:    cell(rec, "address"),
			Port:       port,
			User:       user,
			Credential: cell(rec, "credential"),
			JumpHosts:  jumps,
			Group:      cell(rec, "group"),
			Zone:       cell(rec, "zone"),
			Rack:       cell(rec, "rack"),
			Labels:     labels,
			HostKeyFP:  cell(rec, "hostkey_fp"),
		})
	}
	return validateRows(parsed)
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

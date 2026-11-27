package inventory

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"

	"github.com/kinstall/kinstall/internal/domain"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// TemplateHeaders defines dual Chinese+English headers that round-trip cleanly through ParseXLSX/ParseCSV.
var TemplateHeaders = []string{
	"主机名(name)", "内网IP(address)", "SSH端口(port)", "SSH用户(user)",
	"业务系统(biz)", "集群环境(set)", "业务模块(module)",
	"区域机房(region)", "安全域(security_area)", "故障域(zone)", "机架(rack)",
	"CPU架构(arch)", "页大小(page_size)", "凭据名(credential)",
	"负责人(owner)", "资产编号(asset_tag)", "标签(labels)",
}

var templateSampleRows = [][]string{
	{"his-db-01", "10.20.1.11", "22", "root", "HIS核心门诊", "prod-交易集群", "关系数据库", "北京主中心", "核心内网", "az-1", "rack-a01", "arm64", "65536", "", "张工", "HIS-SRV-001", "tier=db;ha=primary"},
	{"his-cache-01", "10.20.1.12", "22", "root", "HIS核心门诊", "prod-交易集群", "内存缓存", "北京主中心", "核心内网", "az-2", "rack-b02", "amd64", "4096", "", "张工", "HIS-SRV-002", "tier=cache;env=prod"},
	{"his-app-01", "10.20.2.21", "22", "root", "电子病历EMR", "uat-测试集群", "应用网关", "同城灾备中心", "DMZ区", "az-1", "rack-c03", "amd64", "4096", "", "李工", "HIS-SRV-003", "tier=app"},
}

// GenerateHostTemplateXLSX builds a formatted two-sheet Excel workbook for CMDB bulk host import.
func GenerateHostTemplateXLSX() ([]byte, error) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	mainSheet := "主机导入模板"
	_ = f.SetSheetName("Sheet1", mainSheet)

	headerStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "#FFFFFF", Size: 11},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"#0F62FE"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})

	for i, h := range TemplateHeaders {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		_ = f.SetCellValue(mainSheet, cell, h)
		_ = f.SetCellStyle(mainSheet, cell, cell, headerStyle)
		colName, _ := excelize.ColumnNumberToName(i + 1)
		_ = f.SetColWidth(mainSheet, colName, colName, 18)
	}
	for rIdx, row := range templateSampleRows {
		for cIdx, val := range row {
			cell, _ := excelize.CoordinatesToCellName(cIdx+1, rIdx+2)
			_ = f.SetCellValue(mainSheet, cell, val)
		}
	}

	guideSheet := "填写说明"
	_, _ = f.NewSheet(guideSheet)
	guides := [][]string{
		{"列名", "是否必填", "格式与示例", "说明"},
		{"主机名(name)", "必填", "DNS-1123 小写字母/数字/短横线，如 his-db-01", "项目内唯一标识"},
		{"内网IP(address)", "必填", "IPv4/IPv6 或域名，如 10.20.1.11", "SSH 管理地址"},
		{"SSH端口(port)", "可选", "默认 22（1-65535）", "SSH 监听端口"},
		{"SSH用户(user)", "可选", "默认 root", "远程登录账号"},
		{"业务系统(biz)", "建议填写", "如 HIS核心门诊 / 电子病历EMR", "CMDB 一级业务拓扑节点"},
		{"集群环境(set)", "建议填写", "如 prod-交易集群 / uat-测试集群", "CMDB 二级集群节点"},
		{"业务模块(module)", "建议填写", "如 关系数据库 / 内存缓存 / 应用网关", "CMDB 三级模块节点"},
		{"区域机房(region)", "建议填写", "如 北京主中心 / 同城灾备中心", "物理机房/区域维度"},
		{"安全域(security_area)", "建议填写", "如 核心内网 / DMZ区 / 等保三级区", "网络安全分区维度"},
		{"故障域(zone) / 机架(rack)", "可选", "az-1 / rack-a01", "高可用反亲和调度依据"},
		{"CPU架构(arch) / 页大小(page_size)", "可选", "amd64 或 arm64；4096 或 65536", "鲲鹏/麒麟V10 64K 页节点请填 65536"},
		{"凭据名(credential)", "可选", "留空将自动使用导入时选择的默认凭据", "填写凭据库名称或 ID"},
		{"标签(labels)", "可选", "key1=val1;key2=val2", "分号分隔，可用于动态分组筛选"},
	}
	for rIdx, row := range guides {
		for cIdx, val := range row {
			cell, _ := excelize.CoordinatesToCellName(cIdx+1, rIdx+1)
			_ = f.SetCellValue(guideSheet, cell, val)
		}
	}
	_ = f.SetColWidth(guideSheet, "A", "A", 28)
	_ = f.SetColWidth(guideSheet, "B", "B", 12)
	_ = f.SetColWidth(guideSheet, "C", "C", 40)
	_ = f.SetColWidth(guideSheet, "D", "D", 44)

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// GenerateHostTemplateCSV returns a UTF-8 BOM CSV template that opens cleanly in Excel without garbled Chinese.
func GenerateHostTemplateCSV() []byte {
	var buf bytes.Buffer
	buf.Write([]byte{0xEF, 0xBB, 0xBF})
	w := csv.NewWriter(&buf)
	_ = w.Write(TemplateHeaders)
	for _, r := range templateSampleRows {
		_ = w.Write(r)
	}
	w.Flush()
	return buf.Bytes()
}

var exportHeaders = []string{
	"主机ID", "主机名(name)", "内网IP(address)", "SSH端口(port)", "SSH用户(user)", "状态",
	"业务系统(biz)", "集群环境(set)", "业务模块(module)",
	"区域机房(region)", "安全域(security_area)", "故障域(zone)", "机架(rack)",
	"CPU架构(arch)", "页大小(page_size)", "操作系统", "CPU核数", "内存(MB)",
	"监听端口数", "运行实例", "负责人(owner)", "资产编号(asset_tag)", "标签(labels)",
}

func hostExportRow(h domain.Host, claims []domain.HostClaim, instByID map[string]domain.Instance) []string {
	lbl := h.Labels
	if lbl == nil {
		lbl = map[string]string{}
	}
	osStr := strings.TrimSpace(h.OSID + " " + h.OSVersion)
	cpuCores := ""
	memMB := ""
	listenCount := "0"
	if h.Facts != nil {
		if h.Facts.CPUCores > 0 {
			cpuCores = strconv.Itoa(h.Facts.CPUCores)
		}
		if h.Facts.MemMB > 0 {
			memMB = strconv.Itoa(h.Facts.MemMB)
		}
		listenCount = strconv.Itoa(len(h.Facts.ListeningPorts))
		if osStr == "" {
			osStr = strings.TrimSpace(h.Facts.OS.ID + " " + h.Facts.OS.VersionID)
		}
	}
	instNames := make([]string, 0, len(claims))
	seenInst := make(map[string]bool)
	for _, c := range claims {
		if seenInst[c.InstanceID] {
			continue
		}
		seenInst[c.InstanceID] = true
		if inst, ok := instByID[c.InstanceID]; ok {
			instNames = append(instNames, fmt.Sprintf("%s(%s)", inst.Name, inst.Product))
		} else {
			instNames = append(instNames, c.InstanceID)
		}
	}
	var extraLabels []string
	for k, v := range lbl {
		if strings.HasPrefix(k, "cmdb.") {
			continue
		}
		extraLabels = append(extraLabels, k+"="+v)
	}
	sort.Strings(extraLabels)
	return []string{
		h.ID, h.Name, h.Address, strconv.Itoa(h.SSHPort), h.SSHUser, string(h.State),
		lbl[domain.LabelCMDBBiz], lbl[domain.LabelCMDBSet], lbl[domain.LabelCMDBModule],
		lbl[domain.LabelCMDBRegion], lbl[domain.LabelCMDBSecurityArea], h.FaultZone, h.FaultRack,
		h.Arch, strconv.Itoa(h.PageSize), osStr, cpuCores, memMB,
		listenCount, strings.Join(instNames, ", "), lbl[domain.LabelCMDBOwner], lbl[domain.LabelCMDBAssetTag], strings.Join(extraLabels, ";"),
	}
}

// ExportHostsXLSX generates a two-sheet CMDB asset & listening-port Excel ledger.
func ExportHostsXLSX(hosts []domain.Host, claimsByHost map[string][]domain.HostClaim, instByID map[string]domain.Instance) ([]byte, error) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	sheet1 := "CMDB主机台账"
	_ = f.SetSheetName("Sheet1", sheet1)
	headerStyle, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true, Color: "#FFFFFF", Size: 11},
		Fill: excelize.Fill{Type: "pattern", Color: []string{"#0F62FE"}, Pattern: 1},
	})
	for i, h := range exportHeaders {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		_ = f.SetCellValue(sheet1, cell, h)
		_ = f.SetCellStyle(sheet1, cell, cell, headerStyle)
		colName, _ := excelize.ColumnNumberToName(i + 1)
		_ = f.SetColWidth(sheet1, colName, colName, 16)
	}
	for rIdx, host := range hosts {
		vals := hostExportRow(host, claimsByHost[host.ID], instByID)
		for cIdx, v := range vals {
			cell, _ := excelize.CoordinatesToCellName(cIdx+1, rIdx+2)
			_ = f.SetCellValue(sheet1, cell, v)
		}
	}

	sheet2 := "监听端口与进程盘点"
	_, _ = f.NewSheet(sheet2)
	procHeaders := []string{"主机名", "内网IP", "协议", "监听地址", "端口", "进程名", "纳管状态", "关联实例"}
	for i, h := range procHeaders {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		_ = f.SetCellValue(sheet2, cell, h)
		_ = f.SetCellStyle(sheet2, cell, cell, headerStyle)
	}
	procs := buildProcessInventory(hosts, claimsByHost, instByID)
	for rIdx, p := range procs {
		vals := []any{p.HostName, p.HostAddress, p.Proto, p.Address, p.Port, p.Process, p.Classification, p.InstanceName}
		for cIdx, v := range vals {
			cell, _ := excelize.CoordinatesToCellName(cIdx+1, rIdx+2)
			_ = f.SetCellValue(sheet2, cell, v)
		}
	}

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ExportHostsCSV exports the CMDB host ledger as UTF-8 BOM CSV.
func ExportHostsCSV(hosts []domain.Host, claimsByHost map[string][]domain.HostClaim, instByID map[string]domain.Instance) []byte {
	var buf bytes.Buffer
	buf.Write([]byte{0xEF, 0xBB, 0xBF})
	w := csv.NewWriter(&buf)
	_ = w.Write(exportHeaders)
	for _, host := range hosts {
		_ = w.Write(hostExportRow(host, claimsByHost[host.ID], instByID))
	}
	w.Flush()
	return buf.Bytes()
}

// MatchDynamicGroup evaluates whether a host matches ALL rules in a CMDBDynamicGroup (logical AND).
func MatchDynamicGroup(h domain.Host, rules []domain.CMDBGroupRule) bool {
	for _, r := range rules {
		if !matchSingleRule(h, r) {
			return false
		}
	}
	return true
}

func matchSingleRule(h domain.Host, r domain.CMDBGroupRule) bool {
	field := strings.ToLower(strings.TrimSpace(r.Field))
	op := strings.ToLower(strings.TrimSpace(r.Op))
	if op == "" {
		op = "eq"
	}
	want := strings.TrimSpace(r.Value)
	lbl := h.Labels
	if lbl == nil {
		lbl = map[string]string{}
	}

	var actual string
	var actualNum int
	isNumeric := false

	switch {
	case field == "biz":
		actual = lbl[domain.LabelCMDBBiz]
	case field == "set":
		actual = lbl[domain.LabelCMDBSet]
	case field == "module":
		actual = lbl[domain.LabelCMDBModule]
	case field == "region":
		actual = lbl[domain.LabelCMDBRegion]
	case field == "security_area":
		actual = lbl[domain.LabelCMDBSecurityArea]
	case field == "owner":
		actual = lbl[domain.LabelCMDBOwner]
	case field == "fault_zone" || field == "zone":
		actual = h.FaultZone
	case field == "fault_rack" || field == "rack":
		actual = h.FaultRack
	case field == "arch":
		actual = h.Arch
	case field == "os_family":
		actual = h.OSFamily
	case field == "os_id":
		actual = h.OSID
	case field == "state":
		actual = string(h.State)
	case field == "name":
		actual = h.Name
	case field == "address":
		actual = h.Address
	case field == "page_size":
		actualNum = h.PageSize
		actual = strconv.Itoa(h.PageSize)
		isNumeric = true
	case field == "min_cpu" || field == "cpu_cores":
		if h.Facts != nil {
			actualNum = h.Facts.CPUCores
		}
		actual = strconv.Itoa(actualNum)
		isNumeric = true
	case field == "min_mem_mb" || field == "mem_mb":
		if h.Facts != nil {
			actualNum = h.Facts.MemMB
		}
		actual = strconv.Itoa(actualNum)
		isNumeric = true
	case strings.HasPrefix(field, "label."):
		key := strings.TrimPrefix(r.Field, "label.")
		actual = lbl[key]
	default:
		actual = lbl[r.Field]
	}

	switch op {
	case "exists":
		return strings.TrimSpace(actual) != ""
	case "eq":
		return strings.EqualFold(strings.TrimSpace(actual), want)
	case "neq":
		return !strings.EqualFold(strings.TrimSpace(actual), want)
	case "contains":
		return strings.Contains(strings.ToLower(actual), strings.ToLower(want))
	case "prefix":
		return strings.HasPrefix(strings.ToLower(actual), strings.ToLower(want))
	case "in":
		for _, part := range strings.Split(want, ",") {
			if strings.EqualFold(strings.TrimSpace(actual), strings.TrimSpace(part)) {
				return true
			}
		}
		return false
	case "gte":
		wantNum, err := strconv.Atoi(want)
		if err != nil {
			return false
		}
		if !isNumeric {
			actualNum, _ = strconv.Atoi(actual)
		}
		return actualNum >= wantNum
	case "lte":
		wantNum, err := strconv.Atoi(want)
		if err != nil {
			return false
		}
		if !isNumeric {
			actualNum, _ = strconv.Atoi(actual)
		}
		return actualNum <= wantNum
	default:
		return strings.EqualFold(strings.TrimSpace(actual), want)
	}
}

// --- CMDB Topology Tree, CI Impact Graph & Process Inventory ---

type TopologyTreeNode struct {
	Key      string             `json:"key"`
	Label    string             `json:"label"`
	Level    string             `json:"level"` // biz | set | module | region | security_area | zone
	Count    int                `json:"count"`
	HostIDs  []string           `json:"hostIds"`
	Children []TopologyTreeNode `json:"children,omitempty"`
}

type ProcessInventoryItem struct {
	HostID         string `json:"hostId"`
	HostName       string `json:"hostName"`
	HostAddress    string `json:"hostAddress"`
	Proto          string `json:"proto"`
	Address        string `json:"address"`
	Port           int    `json:"port"`
	Process        string `json:"process"`
	Classification string `json:"classification"` // managed | system | shadow
	InstanceID     string `json:"instanceId,omitempty"`
	InstanceName   string `json:"instanceName,omitempty"`
	Product        string `json:"product,omitempty"`
}

type HostImpactInstance struct {
	InstanceID   string          `json:"instanceId"`
	InstanceName string          `json:"instanceName"`
	Product      string          `json:"product"`
	Env          string          `json:"env"`
	State        string          `json:"state"`
	Role         string          `json:"role"`
	StackID      string          `json:"stackId,omitempty"`
	StackName    string          `json:"stackName,omitempty"`
	Ports        []sdk.PortClaim `json:"ports"`
	Paths        []string        `json:"paths"`
	Subnets      []string        `json:"subnets"`
	MemMB        int             `json:"memMb"`
}

type HostImpactSummary struct {
	HostID           string                 `json:"hostId"`
	HostName         string                 `json:"hostName"`
	Address          string                 `json:"address"`
	State            string                 `json:"state"`
	Biz              string                 `json:"biz"`
	Set              string                 `json:"set"`
	Module           string                 `json:"module"`
	Region           string                 `json:"region"`
	SecurityArea     string                 `json:"securityArea"`
	FaultZone        string                 `json:"faultZone"`
	InstanceCount    int                    `json:"instanceCount"`
	StackCount       int                    `json:"stackCount"`
	ClaimedMemMB     int                    `json:"claimedMemMb"`
	TotalMemMB       int                    `json:"totalMemMb"`
	ListeningCount   int                    `json:"listeningCount"`
	ShadowPortCount  int                    `json:"shadowPortCount"`
	AffectedStacks   []string               `json:"affectedStacks"`
	Instances        []HostImpactInstance   `json:"instances"`
	ListeningEntries []ProcessInventoryItem `json:"listeningEntries"`
}

type CMDBOverview struct {
	TotalHosts     int                    `json:"totalHosts"`
	ReadyHosts     int                    `json:"readyHosts"`
	ProbedHosts    int                    `json:"probedHosts"`
	TotalInstances int                    `json:"totalInstances"`
	TotalStacks    int                    `json:"totalStacks"`
	ShadowPorts    int                    `json:"shadowPorts"`
	BizTree        []TopologyTreeNode     `json:"bizTree"`
	PhysicalTree   []TopologyTreeNode     `json:"physicalTree"`
	HostImpacts    []HostImpactSummary    `json:"hostImpacts"`
	Processes      []ProcessInventoryItem `json:"processes"`
}

var systemPorts = map[int]bool{
	22: true, 53: true, 68: true, 111: true, 123: true, 323: true,
}

// BuildHostImpact computes full CI blast radius for a single host.
func BuildHostImpact(h domain.Host, claims []domain.HostClaim, instByID map[string]domain.Instance, stackByID map[string]domain.Stack) HostImpactSummary {
	lbl := h.Labels
	if lbl == nil {
		lbl = map[string]string{}
	}
	claimedPorts := make(map[int]domain.Instance)
	affectedStackMap := make(map[string]bool)
	var impactInsts []HostImpactInstance
	claimedMem := 0

	for _, c := range claims {
		claimedMem += c.MemMB
		inst, ok := instByID[c.InstanceID]
		item := HostImpactInstance{
			InstanceID: c.InstanceID,
			Role:       c.Role,
			Ports:      c.Ports,
			Paths:      c.Paths,
			Subnets:    c.Subnets,
			MemMB:      c.MemMB,
		}
		if ok {
			item.InstanceName = inst.Name
			item.Product = inst.Product
			item.Env = inst.Env
			item.State = inst.State
			if inst.StackID != nil && *inst.StackID != "" {
				item.StackID = *inst.StackID
				if st, sok := stackByID[*inst.StackID]; sok {
					item.StackName = st.Name
					affectedStackMap[st.Name] = true
				} else {
					affectedStackMap[*inst.StackID] = true
				}
			}
			for _, p := range c.Ports {
				claimedPorts[p.Port] = inst
			}
		}
		impactInsts = append(impactInsts, item)
	}

	var affectedStacks []string
	for s := range affectedStackMap {
		affectedStacks = append(affectedStacks, s)
	}
	sort.Strings(affectedStacks)

	var entries []ProcessInventoryItem
	shadowCount := 0
	totalMem := 0
	if h.Facts != nil {
		totalMem = h.Facts.MemMB
		for _, lp := range h.Facts.ListeningPorts {
			cls := "shadow"
			instID, instName, prod := "", "", ""
			if inst, ok := claimedPorts[lp.Port]; ok {
				cls = "managed"
				instID = inst.ID
				instName = inst.Name
				prod = inst.Product
			} else if systemPorts[lp.Port] || strings.Contains(strings.ToLower(lp.Process), "sshd") || strings.Contains(strings.ToLower(lp.Process), "systemd") {
				cls = "system"
			} else {
				shadowCount++
			}
			entries = append(entries, ProcessInventoryItem{
				HostID:         h.ID,
				HostName:       h.Name,
				HostAddress:    h.Address,
				Proto:          lp.Proto,
				Address:        lp.Address,
				Port:           lp.Port,
				Process:        lp.Process,
				Classification: cls,
				InstanceID:     instID,
				InstanceName:   instName,
				Product:        prod,
			})
		}
	}

	return HostImpactSummary{
		HostID:           h.ID,
		HostName:         h.Name,
		Address:          h.Address,
		State:            string(h.State),
		Biz:              defaultLabel(lbl[domain.LabelCMDBBiz], "未归类业务"),
		Set:              defaultLabel(lbl[domain.LabelCMDBSet], "默认集群"),
		Module:           defaultLabel(lbl[domain.LabelCMDBModule], "空闲池"),
		Region:           defaultLabel(lbl[domain.LabelCMDBRegion], "默认机房"),
		SecurityArea:     defaultLabel(lbl[domain.LabelCMDBSecurityArea], "内网区"),
		FaultZone:        defaultLabel(h.FaultZone, "default"),
		InstanceCount:    len(impactInsts),
		StackCount:       len(affectedStacks),
		ClaimedMemMB:     claimedMem,
		TotalMemMB:       totalMem,
		ListeningCount:   len(entries),
		ShadowPortCount:  shadowCount,
		AffectedStacks:   affectedStacks,
		Instances:        impactInsts,
		ListeningEntries: entries,
	}
}

func buildProcessInventory(hosts []domain.Host, claimsByHost map[string][]domain.HostClaim, instByID map[string]domain.Instance) []ProcessInventoryItem {
	var out []ProcessInventoryItem
	for _, h := range hosts {
		imp := BuildHostImpact(h, claimsByHost[h.ID], instByID, nil)
		out = append(out, imp.ListeningEntries...)
	}
	return out
}

// BuildCMDBOverview constructs the unified Business Topology tree, Physical/Security tree, CI Impact list, and Process Inventory.
func BuildCMDBOverview(hosts []domain.Host, instances []domain.Instance, stacks []domain.Stack, claimsByHost map[string][]domain.HostClaim) *CMDBOverview {
	instByID := make(map[string]domain.Instance, len(instances))
	for _, inst := range instances {
		instByID[inst.ID] = inst
	}
	stackByID := make(map[string]domain.Stack, len(stacks))
	for _, st := range stacks {
		stackByID[st.ID] = st
	}

	ov := &CMDBOverview{
		TotalHosts:     len(hosts),
		TotalInstances: len(instances),
		TotalStacks:    len(stacks),
		HostImpacts:    make([]HostImpactSummary, 0, len(hosts)),
		Processes:      make([]ProcessInventoryItem, 0),
	}

	bizMap := make(map[string]map[string]map[string][]string)
	physMap := make(map[string]map[string]map[string][]string)

	for _, h := range hosts {
		if h.State == domain.HostReady || h.State == domain.HostActive {
			ov.ReadyHosts++
		}
		if h.Facts != nil {
			ov.ProbedHosts++
		}
		imp := BuildHostImpact(h, claimsByHost[h.ID], instByID, stackByID)
		ov.ShadowPorts += imp.ShadowPortCount
		ov.HostImpacts = append(ov.HostImpacts, imp)
		ov.Processes = append(ov.Processes, imp.ListeningEntries...)

		add3Level(bizMap, imp.Biz, imp.Set, imp.Module, h.ID)
		add3Level(physMap, imp.Region, imp.SecurityArea, imp.FaultZone, h.ID)
	}

	ov.BizTree = mapTo3LevelTree(bizMap, "biz", "set", "module")
	ov.PhysicalTree = mapTo3LevelTree(physMap, "region", "security_area", "zone")
	return ov
}

func defaultLabel(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return strings.TrimSpace(v)
}

func add3Level(m map[string]map[string]map[string][]string, l1, l2, l3, hostID string) {
	if m[l1] == nil {
		m[l1] = make(map[string]map[string][]string)
	}
	if m[l1][l2] == nil {
		m[l1][l2] = make(map[string][]string)
	}
	m[l1][l2][l3] = append(m[l1][l2][l3], hostID)
}

func mapTo3LevelTree(m map[string]map[string]map[string][]string, lvl1, lvl2, lvl3 string) []TopologyTreeNode {
	l1Keys := make([]string, 0, len(m))
	for k := range m {
		l1Keys = append(l1Keys, k)
	}
	sort.Strings(l1Keys)
	out := make([]TopologyTreeNode, 0, len(l1Keys))
	for _, k1 := range l1Keys {
		n1 := TopologyTreeNode{Key: lvl1 + ":" + k1, Label: k1, Level: lvl1}
		l2Keys := make([]string, 0, len(m[k1]))
		for k2 := range m[k1] {
			l2Keys = append(l2Keys, k2)
		}
		sort.Strings(l2Keys)
		for _, k2 := range l2Keys {
			n2 := TopologyTreeNode{Key: lvl1 + ":" + k1 + "/" + lvl2 + ":" + k2, Label: k2, Level: lvl2}
			l3Keys := make([]string, 0, len(m[k1][k2]))
			for k3 := range m[k1][k2] {
				l3Keys = append(l3Keys, k3)
			}
			sort.Strings(l3Keys)
			for _, k3 := range l3Keys {
				ids := m[k1][k2][k3]
				n3 := TopologyTreeNode{
					Key:     n2.Key + "/" + lvl3 + ":" + k3,
					Label:   k3,
					Level:   lvl3,
					Count:   len(ids),
					HostIDs: ids,
				}
				n2.Children = append(n2.Children, n3)
				n2.HostIDs = append(n2.HostIDs, ids...)
				n2.Count += len(ids)
			}
			n1.Children = append(n1.Children, n2)
			n1.HostIDs = append(n1.HostIDs, n2.HostIDs...)
			n1.Count += n2.Count
		}
		out = append(out, n1)
	}
	return out
}

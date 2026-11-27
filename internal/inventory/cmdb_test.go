package inventory_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"

	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/internal/inventory"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

func TestCMDBTemplateRoundTripAndMultiFormatImport(t *testing.T) {
	// 1. XLSX Template -> ParseXLSX round-trip
	xlsxBytes, err := inventory.GenerateHostTemplateXLSX()
	if err != nil || len(xlsxBytes) < 1000 {
		t.Fatalf("GenerateHostTemplateXLSX failed: len=%d err=%v", len(xlsxBytes), err)
	}
	resXlsx, err := inventory.ParseXLSX(bytes.NewReader(xlsxBytes))
	if err != nil {
		t.Fatalf("ParseXLSX template round-trip: %v", err)
	}
	if len(resXlsx.Errors) != 0 || len(resXlsx.Rows) != 3 {
		t.Fatalf("expected 3 clean template rows, got rows=%d errs=%+v", len(resXlsx.Rows), resXlsx.Errors)
	}
	if resXlsx.Rows[0].Biz != "HIS核心门诊" || resXlsx.Rows[0].Arch != "arm64" || resXlsx.Rows[0].PageSize != 65536 {
		t.Fatalf("unexpected first template row: %+v", resXlsx.Rows[0])
	}

	// 2. CSV Template (with UTF-8 BOM) -> ParseCSV round-trip
	csvBytes := inventory.GenerateHostTemplateCSV()
	if !bytes.HasPrefix(csvBytes, []byte{0xEF, 0xBB, 0xBF}) {
		t.Fatalf("expected UTF-8 BOM prefix on CSV template")
	}
	resCsv, err := inventory.ParseCSV(bytes.NewReader(csvBytes))
	if err != nil || len(resCsv.Errors) != 0 || len(resCsv.Rows) != 3 {
		t.Fatalf("CSV template round-trip failed: rows=%d errs=%+v err=%v", len(resCsv.Rows), resCsv.Errors, err)
	}

	// 3. Pure Chinese CSV + Custom Column Mapping override
	customCSV := "自定义机器名,私网地址,所属部门,机房位置\nmy-node-01,10.99.1.1,门诊系统,上海机房\n"
	resMapped, err := inventory.ParseCSVWithOptions(strings.NewReader(customCSV), inventory.ParseOptions{
		ColumnMap: map[string]string{
			"自定义机器名": "name",
			"私网地址":   "address",
			"所属部门":   "biz",
			"机房位置":   "region",
		},
	})
	if err != nil || len(resMapped.Errors) != 0 || len(resMapped.Rows) != 1 {
		t.Fatalf("mapped CSV failed: rows=%+v errs=%+v err=%v", resMapped.Rows, resMapped.Errors, err)
	}
	if resMapped.Rows[0].Name != "my-node-01" || resMapped.Rows[0].Biz != "门诊系统" || resMapped.Rows[0].Region != "上海机房" {
		t.Fatalf("unexpected mapped row: %+v", resMapped.Rows[0])
	}

	// 4. LiveOps JSON format + hostname sanitization
	liveopsJSON := `{"data":[{"host_name":"HIS_App_01","host_innerip":"10.30.1.11","group_name":"门诊网关","region":"北京中心","arch":"aarch64"}]}`
	resLO, err := inventory.ParseHosts("liveops_json", []byte(liveopsJSON), inventory.ParseOptions{SanitizeHostname: true})
	if err != nil || len(resLO.Errors) != 0 || len(resLO.Rows) != 1 {
		t.Fatalf("LiveOps JSON parse failed: rows=%+v errs=%+v err=%v", resLO.Rows, resLO.Errors, err)
	}
	if resLO.Rows[0].Name != "his-app-01" || resLO.Rows[0].Module != "门诊网关" || resLO.Rows[0].Arch != "arm64" {
		t.Fatalf("unexpected LiveOps row: %+v", resLO.Rows[0])
	}

	// 5. Huawei HCS Nova servers JSON format
	hcsJSON := `{"servers":[{"name":"HCS_DB_01","OS-EXT-AZ:availability_zone":"az1.dc1","addresses":{"vpc-his":[{"addr":"10.30.2.21"}]}}]}`
	resHCS, err := inventory.ParseHosts("huawei_hcs", []byte(hcsJSON), inventory.ParseOptions{SanitizeHostname: true})
	if err != nil || len(resHCS.Errors) != 0 || len(resHCS.Rows) != 1 {
		t.Fatalf("HCS JSON parse failed: rows=%+v errs=%+v err=%v", resHCS.Rows, resHCS.Errors, err)
	}
	if resHCS.Rows[0].Address != "10.30.2.21" || resHCS.Rows[0].Zone != "az1.dc1" {
		t.Fatalf("unexpected HCS row: %+v", resHCS.Rows[0])
	}

	// 6. Ansible INI format
	ansibleINI := "[his-db]\nhis-pg-01 ansible_host=10.30.3.11 ansible_port=2222 ansible_user=ops biz=HIS set=prod\n"
	resAns, err := inventory.ParseHosts("ansible", []byte(ansibleINI), inventory.ParseOptions{SanitizeHostname: true})
	if err != nil || len(resAns.Errors) != 0 || len(resAns.Rows) != 1 {
		t.Fatalf("Ansible INI parse failed: rows=%+v errs=%+v err=%v", resAns.Rows, resAns.Errors, err)
	}
	if resAns.Rows[0].Port != 2222 || resAns.Rows[0].User != "ops" || resAns.Rows[0].Module != "his-db" || resAns.Rows[0].Biz != "HIS" {
		t.Fatalf("unexpected Ansible row: %+v", resAns.Rows[0])
	}
}

func TestCMDBDynamicGroupsTopologyImpactAndExport(t *testing.T) {
	h1 := domain.Host{
		ID: "host_01", Name: "his-db-01", Address: "10.20.1.11", SSHPort: 22, SSHUser: "root",
		State: domain.HostActive, FaultZone: "az-1", FaultRack: "rack-a1", Arch: "arm64", PageSize: 65536,
		Labels: map[string]string{
			domain.LabelCMDBBiz:          "HIS核心门诊",
			domain.LabelCMDBSet:          "prod",
			domain.LabelCMDBModule:       "关系数据库",
			domain.LabelCMDBRegion:       "北京主中心",
			domain.LabelCMDBSecurityArea: "核心内网",
			"tier":                       "db",
		},
		Facts: &sdk.FactsView{
			CPUCores: 16,
			MemMB:    65536,
			ListeningPorts: []sdk.ListeningPort{
				{Proto: "tcp", Address: "0.0.0.0", Port: 22, Process: "sshd"},
				{Proto: "tcp", Address: "0.0.0.0", Port: 5432, Process: "postgres"},
				{Proto: "tcp", Address: "0.0.0.0", Port: 9999, Process: "legacy-agent"},
			},
		},
	}
	h2 := domain.Host{
		ID: "host_02", Name: "his-app-01", Address: "10.20.1.12", SSHPort: 22, SSHUser: "root",
		State: domain.HostReady, FaultZone: "az-2", Arch: "amd64", PageSize: 4096,
		Labels: map[string]string{
			domain.LabelCMDBBiz:    "HIS核心门诊",
			domain.LabelCMDBSet:    "prod",
			domain.LabelCMDBModule: "应用网关",
		},
		Facts: &sdk.FactsView{CPUCores: 8, MemMB: 16384},
	}

	// Dynamic Group predicates
	rules := []domain.CMDBGroupRule{
		{Field: "biz", Op: "contains", Value: "HIS"},
		{Field: "arch", Op: "eq", Value: "arm64"},
		{Field: "page_size", Op: "gte", Value: "65536"},
		{Field: "cpu_cores", Op: "gte", Value: "16"},
		{Field: "label.tier", Op: "in", Value: "db,cache"},
	}
	if !inventory.MatchDynamicGroup(h1, rules) {
		t.Fatalf("expected h1 to match Kunpeng 64K DB dynamic group")
	}
	if inventory.MatchDynamicGroup(h2, rules) {
		t.Fatalf("expected h2 NOT to match Kunpeng 64K DB dynamic group")
	}

	// Topology + CI Impact + Shadow port detection
	stID := "stack_his"
	inst := domain.Instance{ID: "inst_pg", StackID: &stID, Env: "prod", Name: "pg-main", Product: "postgresql", State: "ready"}
	st := domain.Stack{ID: stID, Name: "his-core-stack"}
	claims := map[string][]domain.HostClaim{
		h1.ID: {{InstanceID: inst.ID, HostID: h1.ID, Role: "primary", Ports: []sdk.PortClaim{{Port: 5432, Proto: "tcp"}}, MemMB: 16384}},
	}
	ov := inventory.BuildCMDBOverview([]domain.Host{h1, h2}, []domain.Instance{inst}, []domain.Stack{st}, claims)
	if len(ov.BizTree) != 1 || ov.BizTree[0].Label != "HIS核心门诊" || ov.BizTree[0].Count != 2 {
		t.Fatalf("unexpected bizTree: %+v", ov.BizTree)
	}
	if ov.ShadowPorts != 1 {
		t.Fatalf("expected exactly 1 shadow port (9999/legacy-agent), got %d (processes=%+v)", ov.ShadowPorts, ov.Processes)
	}
	if ov.HostImpacts[0].StackCount != 1 || ov.HostImpacts[0].InstanceCount != 1 {
		t.Fatalf("expected h1 impact to report 1 instance & 1 stack, got %+v", ov.HostImpacts[0])
	}

	// Export XLSX & CSV ledger
	instByID := map[string]domain.Instance{inst.ID: inst}
	xlsxOut, err := inventory.ExportHostsXLSX([]domain.Host{h1, h2}, claims, instByID)
	if err != nil {
		t.Fatalf("ExportHostsXLSX: %v", err)
	}
	wb, err := excelize.OpenReader(bytes.NewReader(xlsxOut))
	if err != nil {
		t.Fatalf("open exported xlsx: %v", err)
	}
	defer func() { _ = wb.Close() }()
	if sheets := wb.GetSheetList(); len(sheets) != 2 {
		t.Fatalf("expected 2 sheets in exported ledger, got %v", sheets)
	}
	csvOut := inventory.ExportHostsCSV([]domain.Host{h1, h2}, claims, instByID)
	if !strings.Contains(string(csvOut), "pg-main(postgresql)") || !strings.Contains(string(csvOut), "HIS核心门诊") {
		t.Fatalf("exported CSV missing expected CMDB fields: %s", string(csvOut))
	}
}

package cmdb

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/xuri/excelize/v2"
)

// sanitizeExcelCell neutralizes CSV/Excel formula injection (=, +, -, @, \t, \r) per LiveOps 03.3.
func sanitizeExcelCell(v string) string {
	if v == "" {
		return ""
	}
	switch v[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + v
	default:
		return v
	}
}

// ExportModelTemplateXLSX dynamically generates a rich .xlsx import template from a CMDB model's live attribute schema.
func (s *Service) ExportModelTemplateXLSX(ctx context.Context, projectID, modelCode string) ([]byte, string, error) {
	if err := s.EnsureDefaults(ctx, projectID); err != nil {
		return nil, "", err
	}
	mdl, err := s.GetModel(ctx, projectID, modelCode)
	if err != nil {
		return nil, "", err
	}
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	sheet := "CI_Import"
	_ = f.SetSheetName("Sheet1", sheet)

	headerStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "#FFFFFF", Size: 11},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"#1D4ED8"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})
	hintStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Italic: true, Color: "#475569", Size: 9},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"#F1F5F9"}, Pattern: 1},
		Alignment: &excelize.Alignment{WrapText: true, Vertical: "center"},
	})

	type colDef struct {
		key, title, hint string
		enums            []string
	}
	cols := []colDef{
		{key: "name", title: "CI 名称*", hint: "必填，资产显示名称"},
		{key: "unique_key", title: "唯一标识(unique_key)", hint: "可选，用于幂等覆盖更新"},
		{key: "env", title: "环境(env)", hint: "prod / uat / sit / dev", enums: []string{"prod", "beta", "uat", "sit", "dev"}},
		{key: "status", title: "状态(status)", hint: "active / maintenance / offline", enums: []string{"active", "maintenance", "offline", "decommissioned"}},
		{key: "owner", title: "负责人(owner)", hint: "责任人或团队名"},
	}
	for _, a := range mdl.Attributes {
		title := fmt.Sprintf("%s(%s)", a.PropName, a.PropID)
		if a.Required {
			title += "*"
		}
		hint := fmt.Sprintf("类型:%s", a.FieldType)
		if a.Unit != "" {
			hint += " 单位:" + a.Unit
		}
		if len(a.EnumOptions) > 0 {
			hint += " 可选:" + strings.Join(a.EnumOptions, "/")
		}
		cols = append(cols, colDef{key: a.PropID, title: title, hint: hint, enums: a.EnumOptions})
	}

	for idx, c := range cols {
		colName, _ := excelize.ColumnNumberToName(idx + 1)
		_ = f.SetCellValue(sheet, fmt.Sprintf("%s1", colName), c.title)
		_ = f.SetCellValue(sheet, fmt.Sprintf("%s2", colName), c.hint)
		_ = f.SetCellStyle(sheet, fmt.Sprintf("%s1", colName), fmt.Sprintf("%s1", colName), headerStyle)
		_ = f.SetCellStyle(sheet, fmt.Sprintf("%s2", colName), fmt.Sprintf("%s2", colName), hintStyle)
		_ = f.SetColWidth(sheet, colName, colName, 22)
		if len(c.enums) > 0 {
			dv := excelize.NewDataValidation(true)
			dv.Sqref = fmt.Sprintf("%s3:%s500", colName, colName)
			_ = dv.SetDropList(c.enums)
			_ = f.AddDataValidation(sheet, dv)
		}
	}
	_ = f.SetRowHeight(sheet, 1, 24)
	_ = f.SetRowHeight(sheet, 2, 22)

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), fmt.Sprintf("cmdb-%s-template.xlsx", modelCode), nil
}

// ExportInstancesXLSX exports all CI instances of a model into a formula-injection-safe .xlsx workbook.
func (s *Service) ExportInstancesXLSX(ctx context.Context, projectID, modelCode string) ([]byte, string, error) {
	if err := s.EnsureDefaults(ctx, projectID); err != nil {
		return nil, "", err
	}
	mdl, err := s.GetModel(ctx, projectID, modelCode)
	if err != nil {
		return nil, "", err
	}
	items, err := s.ListCIInstances(ctx, projectID, modelCode, "")
	if err != nil {
		return nil, "", err
	}
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	sheet := "CI_Export"
	_ = f.SetSheetName("Sheet1", sheet)

	headerStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "#FFFFFF", Size: 11},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"#0F172A"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})

	headers := []string{"ID", "CI 名称", "唯一标识", "环境", "状态", "负责人"}
	propIDs := make([]string, 0, len(mdl.Attributes))
	for _, a := range mdl.Attributes {
		headers = append(headers, fmt.Sprintf("%s(%s)", a.PropName, a.PropID))
		propIDs = append(propIDs, a.PropID)
	}
	for i, h := range headers {
		col, _ := excelize.ColumnNumberToName(i + 1)
		_ = f.SetCellValue(sheet, fmt.Sprintf("%s1", col), h)
		_ = f.SetCellStyle(sheet, fmt.Sprintf("%s1", col), fmt.Sprintf("%s1", col), headerStyle)
		_ = f.SetColWidth(sheet, col, col, 20)
	}

	for rIdx, it := range items {
		rowNum := rIdx + 2
		base := []string{it.ID, it.Name, it.UniqueKey, it.Env, it.Status, it.Owner}
		for i, v := range base {
			col, _ := excelize.ColumnNumberToName(i + 1)
			_ = f.SetCellValue(sheet, fmt.Sprintf("%s%d", col, rowNum), sanitizeExcelCell(v))
		}
		for j, pid := range propIDs {
			col, _ := excelize.ColumnNumberToName(len(base) + j + 1)
			raw := ""
			if val, ok := it.Attrs[pid]; ok && val != nil {
				raw = fmt.Sprint(val)
			}
			_ = f.SetCellValue(sheet, fmt.Sprintf("%s%d", col, rowNum), sanitizeExcelCell(raw))
		}
	}

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), fmt.Sprintf("cmdb-%s-export.xlsx", modelCode), nil
}

// ImportInstancesXLSX parses an uploaded .xlsx workbook, validates each row against dynamic model attributes,
// and performs dry-run or real upsert commit.
func (s *Service) ImportInstancesXLSX(ctx context.Context, projectID, modelCode, actor string, r io.Reader, dryRun bool) (*ExcelImportReport, error) {
	if err := s.EnsureDefaults(ctx, projectID); err != nil {
		return nil, err
	}
	mdl, err := s.GetModel(ctx, projectID, modelCode)
	if err != nil {
		return nil, err
	}
	f, err := excelize.OpenReader(r)
	if err != nil {
		return nil, fmt.Errorf("invalid xlsx workbook: %w", err)
	}
	defer func() { _ = f.Close() }()

	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil, fmt.Errorf("empty xlsx workbook")
	}
	rows, err := f.GetRows(sheets[0])
	if err != nil {
		return nil, err
	}
	if len(rows) < 2 {
		return &ExcelImportReport{ModelCode: modelCode, DryRun: dryRun, Rows: []ExcelImportRowResult{}}, nil
	}

	// Parse column mapping from row 0 header (supports "显示名(prop_id)*" or raw "prop_id")
	colMap := make(map[int]string)
	for i, cell := range rows[0] {
		c := strings.TrimSpace(strings.TrimSuffix(cell, "*"))
		if strings.Contains(c, "(") && strings.Contains(c, ")") {
			start := strings.LastIndex(c, "(")
			end := strings.LastIndex(c, ")")
			if end > start {
				colMap[i] = strings.TrimSpace(c[start+1 : end])
				continue
			}
		}
		switch c {
		case "CI 名称", "名称", "name":
			colMap[i] = "name"
		case "唯一标识", "unique_key":
			colMap[i] = "unique_key"
		case "环境", "env":
			colMap[i] = "env"
		case "状态", "status":
			colMap[i] = "status"
		case "负责人", "owner":
			colMap[i] = "owner"
		default:
			colMap[i] = c
		}
	}

	// Detect if row 2 is the template hint row ("必填，资产显示名称" / "类型:...")
	startRow := 1
	if len(rows) > 1 && len(rows[1]) > 0 && (strings.Contains(rows[1][0], "必填") || strings.Contains(rows[1][0], "类型:")) {
		startRow = 2
	}

	attrByID := make(map[string]ModelAttribute, len(mdl.Attributes))
	for _, a := range mdl.Attributes {
		attrByID[a.PropID] = a
	}

	report := &ExcelImportReport{
		ModelCode: modelCode,
		DryRun:    dryRun,
		Rows:      make([]ExcelImportRowResult, 0),
	}

	for rIdx := startRow; rIdx < len(rows); rIdx++ {
		row := rows[rIdx]
		if isEmptyRow(row) {
			continue
		}
		report.TotalRows++
		ci := CIInstance{
			ProjectID: projectID,
			ModelCode: modelCode,
			Status:    "active",
			Env:       "prod",
			Attrs:     make(map[string]any),
		}
		rowFailed := false
		for cIdx, val := range row {
			key, ok := colMap[cIdx]
			if !ok {
				continue
			}
			v := strings.TrimSpace(strings.TrimPrefix(val, "'"))
			if v == "" {
				continue
			}
			switch key {
			case "name":
				ci.Name = v
			case "unique_key":
				ci.UniqueKey = v
			case "env":
				ci.Env = v
			case "status":
				ci.Status = v
			case "owner":
				ci.Owner = v
			default:
				if attr, exists := attrByID[key]; exists {
					coerced, cerr := coerceAttrValue(attr, v)
					if cerr != nil {
						report.Errors++
						report.Rows = append(report.Rows, ExcelImportRowResult{Row: rIdx + 1, Name: ci.Name, Action: "error", Message: cerr.Error()})
						rowFailed = true
					} else {
						ci.Attrs[key] = coerced
					}
				}
			}
			if rowFailed {
				break
			}
		}
		if rowFailed {
			continue
		}
		if ci.Name == "" {
			report.Errors++
			report.Rows = append(report.Rows, ExcelImportRowResult{Row: rIdx + 1, Action: "error", Message: "CI 名称不能为空"})
			continue
		}
		if err := validateAttrs(mdl.Attributes, ci.Attrs, nil); err != nil {
			report.Errors++
			report.Rows = append(report.Rows, ExcelImportRowResult{Row: rIdx + 1, Name: ci.Name, Action: "error", Message: err.Error()})
			continue
		}

		// Check if matching existing CI by unique_key or name
		existing, _ := s.findCIByUniqueOrName(ctx, projectID, modelCode, ci.UniqueKey, ci.Name)
		if dryRun {
			act := "create"
			if existing != nil {
				act = "update"
				report.Updated++
			} else {
				report.Created++
			}
			report.Rows = append(report.Rows, ExcelImportRowResult{Row: rIdx + 1, Name: ci.Name, Action: act, Message: "预检通过"})
			continue
		}

		if existing != nil {
			ci.ID = existing.ID
			if _, err := s.UpdateCIInstance(ctx, projectID, existing.ID, actor, &ci); err != nil {
				report.Errors++
				report.Rows = append(report.Rows, ExcelImportRowResult{Row: rIdx + 1, Name: ci.Name, Action: "error", Message: err.Error()})
				continue
			}
			report.Updated++
			report.Rows = append(report.Rows, ExcelImportRowResult{Row: rIdx + 1, Name: ci.Name, Action: "update", Message: "已覆盖更新"})
		} else {
			if _, err := s.CreateCIInstance(ctx, projectID, actor, &ci); err != nil {
				report.Errors++
				report.Rows = append(report.Rows, ExcelImportRowResult{Row: rIdx + 1, Name: ci.Name, Action: "error", Message: err.Error()})
				continue
			}
			report.Created++
			report.Rows = append(report.Rows, ExcelImportRowResult{Row: rIdx + 1, Name: ci.Name, Action: "create", Message: "已新增入库"})
		}
	}

	return report, nil
}

func isEmptyRow(row []string) bool {
	for _, c := range row {
		if strings.TrimSpace(c) != "" {
			return false
		}
	}
	return true
}

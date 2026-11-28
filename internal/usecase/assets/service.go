package assets

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/xuri/excelize/v2"

	"github.com/kinstall/kinstall/internal/adapter/store"
	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/pkg/platform/auth"
)

type Service struct {
	repo *store.Repo
}

func New(repo *store.Repo) *Service {
	return &Service{repo: repo}
}

func (s *Service) ListModels(ctx context.Context, scope domain.Scope) ([]domain.AssetModel, error) {
	return s.repo.ListAssetModels(ctx, scope)
}

func (s *Service) CreateModel(ctx context.Context, tc auth.TrustedContext, scope domain.Scope, groupID, code, name, icon, desc string, fields []domain.ModelField) (domain.AssetModel, error) {
	code, name = strings.TrimSpace(code), strings.TrimSpace(name)
	if code == "" || name == "" {
		return domain.AssetModel{}, domain.ValidationError("模型编码和名称不能为空")
	}
	m, err := s.repo.CreateAssetModel(ctx, scope, groupID, code, name, icon, desc, fields)
	if err == nil {
		_ = s.repo.AppendAuditLog(ctx, domain.AuditLog{
			TenantID: scope.TenantID, AppID: scope.AppID, ActorID: tc.SubjectID, ActorName: tc.Username,
			Action: "CMDB_MODEL_CREATE", ResourceType: "asset_model", ResourceID: m.ModelID, RequestID: tc.TraceID,
		}, "assets", 201, "创建 CMDB 模型 "+m.Name)
	}
	return m, err
}

func (s *Service) ListRelationTypes(ctx context.Context, scope domain.Scope) ([]domain.RelationType, error) {
	return s.repo.ListRelationTypes(ctx, scope)
}

func (s *Service) DeleteRelationType(ctx context.Context, scope domain.Scope, relTypeID string) error {
	return s.repo.DeleteRelationType(ctx, scope, relTypeID)
}

func (s *Service) ListInstances(ctx context.Context, scope domain.Scope, modelID, keyword string) ([]domain.AssetInstance, error) {
	return s.repo.ListAssetInstances(ctx, scope, modelID, keyword)
}

func (s *Service) GetInstance(ctx context.Context, scope domain.Scope, instanceID string) (domain.AssetInstance, error) {
	return s.repo.GetAssetInstance(ctx, scope, instanceID)
}

func (s *Service) CreateInstance(ctx context.Context, tc auth.TrustedContext, scope domain.Scope, modelID, name, uniqueKey, status string, attrs map[string]any) (domain.AssetInstance, error) {
	name, uniqueKey = strings.TrimSpace(name), strings.TrimSpace(uniqueKey)
	if modelID == "" || name == "" || uniqueKey == "" {
		return domain.AssetInstance{}, domain.ValidationError("模型ID、实例名称和唯一标识均不能为空")
	}
	fields, err := s.repo.ListModelFields(ctx, scope, modelID)
	if err != nil {
		return domain.AssetInstance{}, err
	}
	cleanAttrs, uniques, err := domain.ValidateInstanceAttrs(fields, attrs)
	if err != nil {
		return domain.AssetInstance{}, err
	}
	inst, err := s.repo.CreateAssetInstance(ctx, scope, modelID, name, uniqueKey, status, tc.SubjectID, cleanAttrs, uniques)
	if err == nil {
		_ = s.repo.AppendAuditLog(ctx, domain.AuditLog{
			TenantID: scope.TenantID, AppID: scope.AppID, ActorID: tc.SubjectID, ActorName: tc.Username,
			Action: "CMDB_INSTANCE_CREATE", ResourceType: "asset_instance", ResourceID: inst.InstanceID, RequestID: tc.TraceID,
		}, "assets", 201, "新增 CMDB 资产实例 "+inst.Name)
	}
	return inst, err
}

func (s *Service) UpdateInstance(ctx context.Context, tc auth.TrustedContext, scope domain.Scope, instanceID string, expectedVer int64, name, status string, attrs map[string]any) (domain.AssetInstance, error) {
	existing, err := s.repo.GetAssetInstance(ctx, scope, instanceID)
	if err != nil {
		return domain.AssetInstance{}, err
	}
	if name == "" {
		name = existing.Name
	}
	if status == "" {
		status = existing.Status
	}
	fields, err := s.repo.ListModelFields(ctx, scope, existing.ModelID)
	if err != nil {
		return domain.AssetInstance{}, err
	}
	cleanAttrs, uniques, err := domain.ValidateInstanceAttrs(fields, attrs)
	if err != nil {
		return domain.AssetInstance{}, err
	}
	inst, err := s.repo.UpdateAssetInstance(ctx, scope, instanceID, expectedVer, name, status, tc.SubjectID, cleanAttrs, uniques)
	if err == nil {
		_ = s.repo.AppendAuditLog(ctx, domain.AuditLog{
			TenantID: scope.TenantID, AppID: scope.AppID, ActorID: tc.SubjectID, ActorName: tc.Username,
			Action: "CMDB_INSTANCE_UPDATE", ResourceType: "asset_instance", ResourceID: inst.InstanceID, RequestID: tc.TraceID,
		}, "assets", 200, fmt.Sprintf("更新 CMDB 实例 %s (v%d)", inst.Name, inst.Version))
	}
	return inst, err
}

func (s *Service) CreateEdge(ctx context.Context, tc auth.TrustedContext, scope domain.Scope, treeKind domain.TreeKind, relTypeID, sourceID, targetID string, metadata map[string]any) (domain.AssetEdge, error) {
	e, err := s.repo.CreateAssetEdge(ctx, scope, treeKind, relTypeID, sourceID, targetID, metadata)
	if err == nil {
		_ = s.repo.AppendAuditLog(ctx, domain.AuditLog{
			TenantID: scope.TenantID, AppID: scope.AppID, ActorID: tc.SubjectID, ActorName: tc.Username,
			Action: "CMDB_EDGE_CREATE", ResourceType: "asset_edge", ResourceID: e.EdgeID, RequestID: tc.TraceID,
		}, "assets", 201, fmt.Sprintf("建立 %s 拓扑边 %s -> %s", treeKind, sourceID, targetID))
	}
	return e, err
}

func (s *Service) DeleteEdge(ctx context.Context, scope domain.Scope, edgeID string) error {
	return s.repo.DeleteAssetEdge(ctx, scope, edgeID)
}

func (s *Service) GetTopologyGraph(ctx context.Context, scope domain.Scope, treeKind domain.TreeKind) (domain.TopologyGraph, error) {
	return s.repo.GetTopologyGraph(ctx, scope, treeKind)
}

func (s *Service) CascadeDeleteInstance(ctx context.Context, tc auth.TrustedContext, scope domain.Scope, rootID string) (domain.DeleteOperation, error) {
	op, err := s.repo.CascadeDeleteInstance(ctx, scope, rootID, tc.SubjectID)
	if err == nil {
		_ = s.repo.AppendAuditLog(ctx, domain.AuditLog{
			TenantID: scope.TenantID, AppID: scope.AppID, ActorID: tc.SubjectID, ActorName: tc.Username,
			Action: "CMDB_CASCADE_DELETE", ResourceType: "asset_instance", ResourceID: rootID, RequestID: tc.TraceID,
		}, "assets", 200, fmt.Sprintf("级联删除子树 %s（共 %d 项）", rootID, op.DeletedItems))
	}
	return op, err
}

// ExportInstancesXLSX exports all instances of a given CMDB model (or an empty import template when templateOnly=true) to native .xlsx.
func (s *Service) ExportInstancesXLSX(ctx context.Context, scope domain.Scope, modelID string, templateOnly bool) ([]byte, error) {
	fields, err := s.repo.ListModelFields(ctx, scope, modelID)
	if err != nil {
		return nil, err
	}
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	sheet := "Instances"
	_ = f.SetSheetName("Sheet1", sheet)

	headers := []string{"unique_key", "name", "status"}
	for _, fld := range fields {
		headers = append(headers, fld.FieldKey)
	}
	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		_ = f.SetCellValue(sheet, cell, h)
	}

	if !templateOnly {
		instances, err := s.repo.ListAssetInstances(ctx, scope, modelID, "")
		if err != nil {
			return nil, err
		}
		for rowIdx, inst := range instances {
			r := rowIdx + 2
			_ = f.SetCellValue(sheet, fmt.Sprintf("A%d", r), inst.UniqueKey)
			_ = f.SetCellValue(sheet, fmt.Sprintf("B%d", r), inst.Name)
			_ = f.SetCellValue(sheet, fmt.Sprintf("C%d", r), inst.Status)
			for colOffset, fld := range fields {
				cell, _ := excelize.CoordinatesToCellName(4+colOffset, r)
				if val, ok := inst.Attrs[fld.FieldKey]; ok && val != nil {
					_ = f.SetCellValue(sheet, cell, fmt.Sprint(val))
				}
			}
		}
	}

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

type ImportSummary struct {
	Imported int      `json:"imported"`
	Skipped  int      `json:"skipped"`
	Errors   []string `json:"errors,omitempty"`
}

// ImportInstancesXLSX parses an uploaded .xlsx workbook and creates validated CMDB instances inside the scoped tenant/app.
func (s *Service) ImportInstancesXLSX(ctx context.Context, tc auth.TrustedContext, scope domain.Scope, modelID string, r io.Reader) (ImportSummary, error) {
	wb, err := excelize.OpenReader(r)
	if err != nil {
		return ImportSummary{}, domain.ValidationError("无效的 Excel (.xlsx) 文件: " + err.Error())
	}
	defer func() { _ = wb.Close() }()
	sheets := wb.GetSheetList()
	if len(sheets) == 0 {
		return ImportSummary{}, domain.ValidationError("Excel 文件缺少工作表")
	}
	rows, err := wb.GetRows(sheets[0])
	if err != nil || len(rows) < 2 {
		return ImportSummary{}, domain.ValidationError("Excel 至少需要表头和 1 行数据")
	}
	headers := rows[0]
	var sum ImportSummary
	for i := 1; i < len(rows); i++ {
		row := rows[i]
		attrs := make(map[string]any)
		var ukey, name, status string
		for c, h := range headers {
			val := ""
			if c < len(row) {
				val = strings.TrimSpace(row[c])
			}
			switch strings.ToLower(strings.TrimSpace(h)) {
			case "unique_key":
				ukey = val
			case "name":
				name = val
			case "status":
				status = val
			default:
				if h != "" && val != "" {
					attrs[strings.TrimSpace(h)] = val
				}
			}
		}
		if ukey == "" && name == "" {
			continue
		}
		if status == "" {
			status = "ACTIVE"
		}
		if _, err := s.CreateInstance(ctx, tc, scope, modelID, name, ukey, status, attrs); err != nil {
			sum.Skipped++
			sum.Errors = append(sum.Errors, fmt.Sprintf("第 %d 行 (%s): %v", i+1, ukey, err))
			continue
		}
		sum.Imported++
	}
	return sum, nil
}

package cmdb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/kinstall/kinstall/internal/ports"

	"github.com/kinstall/kinstall/internal/domain"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// Service implements the Dynamic Meta-Model CMDB, Dual Topology, Service Templates,
// Excelize .xlsx I/O, and NetBox-inspired DCIM/IPAM engine.
type Service struct {
	writeDB ports.QueryExec
	readDB  ports.QueryExec
}

func NewService(writeDB, readDB ports.QueryExec) *Service {
	if readDB == nil {
		readDB = writeDB
	}
	return &Service{writeDB: writeDB, readDB: readDB}
}

func parseTS(s string) time.Time {
	s = strings.TrimSpace(s)
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05.999999-07", "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

func invalidErr(msg string) error {
	return sdk.NewError(sdk.CodeAPIInvalidRequest, "CMDB", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, errors.New(msg))
}

func conflictErr(msg string) error {
	return sdk.NewError(sdk.CodeLockConflict, "CMDB", http.StatusConflict, sdk.CodeLockConflict, false, errors.New(msg))
}

func notFoundErr(res, id string) error {
	return sdk.NewError(sdk.CodeDBNotFound, "CMDB", http.StatusNotFound, sdk.CodeDBNotFound, false, fmt.Errorf("%s %q not found", res, id))
}

// GetMetaModelOverview returns all model groups, models (with attributes & instance counts), and model relations.
func (s *Service) GetMetaModelOverview(ctx context.Context, projectID string) (*MetaModelOverview, error) {
	if err := s.EnsureDefaults(ctx, projectID); err != nil {
		return nil, err
	}

	out := &MetaModelOverview{
		Groups:    make([]ModelGroup, 0),
		Models:    make([]Model, 0),
		Relations: make([]ModelRelation, 0),
	}

	gRows, err := s.readDB.QueryRaw(ctx,
		`SELECT id, project_id, code, name, sort_order, is_builtin, created_at
		 FROM cmdb_model_groups WHERE project_id = ? ORDER BY sort_order ASC, name ASC`, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = gRows.Close() }()
	for gRows.Next() {
		var g ModelGroup
		var cAt string
		if err := gRows.Scan(&g.ID, &g.ProjectID, &g.Code, &g.Name, &g.SortOrder, &g.IsBuiltin, &cAt); err != nil {
			return nil, err
		}
		g.CreatedAt = parseTS(cAt)
		out.Groups = append(out.Groups, g)
	}

	// Count instances per model
	counts := make(map[string]int)
	cRows, err := s.readDB.QueryRaw(ctx,
		`SELECT model_code, COUNT(1) FROM cmdb_ci_instances WHERE project_id = ? GROUP BY model_code`, projectID)
	if err == nil {
		defer func() { _ = cRows.Close() }()
		for cRows.Next() {
			var mc string
			var cnt int
			if err := cRows.Scan(&mc, &cnt); err == nil {
				counts[mc] = cnt
			}
		}
	}

	// Load all attributes keyed by model_code
	attrsByModel := make(map[string][]ModelAttribute)
	aRows, err := s.readDB.QueryRaw(ctx,
		`SELECT id, project_id, model_code, attr_group, prop_id, prop_name, field_type, unit, required, readonly, is_unique, display, enum_options_json, default_value, source_kind, collector_plugin_id, sort_order
		 FROM cmdb_model_attributes WHERE project_id = ? ORDER BY sort_order ASC, prop_id ASC`, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = aRows.Close() }()
	for aRows.Next() {
		var a ModelAttribute
		var ft, enumsStr string
		if err := aRows.Scan(&a.ID, &a.ProjectID, &a.ModelCode, &a.AttrGroup, &a.PropID, &a.PropName, &ft, &a.Unit, &a.Required, &a.Readonly, &a.IsUnique, &a.Display, &enumsStr, &a.DefaultValue, &a.SourceKind, &a.CollectorPluginID, &a.SortOrder); err != nil {
			return nil, err
		}
		a.FieldType = FieldType(ft)
		_ = json.Unmarshal([]byte(enumsStr), &a.EnumOptions)
		if a.EnumOptions == nil {
			a.EnumOptions = []string{}
		}
		attrsByModel[a.ModelCode] = append(attrsByModel[a.ModelCode], a)
	}

	mRows, err := s.readDB.QueryRaw(ctx,
		`SELECT id, project_id, group_code, code, name, icon, description, pos_x, pos_y, is_builtin, is_mainline, mainline_parent_code, created_at, updated_at
		 FROM cmdb_models WHERE project_id = ? ORDER BY is_builtin DESC, code ASC`, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = mRows.Close() }()
	for mRows.Next() {
		var m Model
		var cAt, uAt string
		if err := mRows.Scan(&m.ID, &m.ProjectID, &m.GroupCode, &m.Code, &m.Name, &m.Icon, &m.Description, &m.PosX, &m.PosY, &m.IsBuiltin, &m.IsMainline, &m.MainlineParentCode, &cAt, &uAt); err != nil {
			return nil, err
		}
		m.CreatedAt, m.UpdatedAt = parseTS(cAt), parseTS(uAt)
		m.InstanceCount = counts[m.Code]
		m.Attributes = attrsByModel[m.Code]
		if m.Attributes == nil {
			m.Attributes = []ModelAttribute{}
		}
		out.Models = append(out.Models, m)
	}

	rRows, err := s.readDB.QueryRaw(ctx,
		`SELECT id, project_id, source_model, target_model, relation_type, label, mapping_kind, created_at
		 FROM cmdb_model_relations WHERE project_id = ? ORDER BY created_at ASC`, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rRows.Close() }()
	for rRows.Next() {
		var r ModelRelation
		var cAt string
		if err := rRows.Scan(&r.ID, &r.ProjectID, &r.SourceModel, &r.TargetModel, &r.RelationType, &r.Label, &r.MappingKind, &cAt); err != nil {
			return nil, err
		}
		r.CreatedAt = parseTS(cAt)
		out.Relations = append(out.Relations, r)
	}

	return out, nil
}

func (s *Service) GetModel(ctx context.Context, projectID, modelCode string) (*Model, error) {
	if err := s.EnsureDefaults(ctx, projectID); err != nil {
		return nil, err
	}
	var m Model
	var cAt, uAt string
	err := s.readDB.ScanRaw(ctx, `SELECT id, project_id, group_code, code, name, icon, description, pos_x, pos_y, is_builtin, is_mainline, mainline_parent_code, created_at, updated_at
		 FROM cmdb_models WHERE project_id = ? AND code = ?`, []any{projectID, modelCode}, &m.ID, &m.ProjectID, &m.GroupCode, &m.Code, &m.Name, &m.Icon, &m.Description, &m.PosX, &m.PosY, &m.IsBuiltin, &m.IsMainline, &m.MainlineParentCode, &cAt, &uAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFoundErr("cmdb_model", modelCode)
	}
	if err != nil {
		return nil, err
	}
	m.CreatedAt, m.UpdatedAt = parseTS(cAt), parseTS(uAt)

	m.Attributes = make([]ModelAttribute, 0)
	rows, err := s.readDB.QueryRaw(ctx,
		`SELECT id, project_id, model_code, attr_group, prop_id, prop_name, field_type, unit, required, readonly, is_unique, display, enum_options_json, default_value, source_kind, collector_plugin_id, sort_order
		 FROM cmdb_model_attributes WHERE project_id = ? AND model_code = ? ORDER BY sort_order ASC, prop_id ASC`, projectID, modelCode)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var a ModelAttribute
		var ft, enumsStr string
		if err := rows.Scan(&a.ID, &a.ProjectID, &a.ModelCode, &a.AttrGroup, &a.PropID, &a.PropName, &ft, &a.Unit, &a.Required, &a.Readonly, &a.IsUnique, &a.Display, &enumsStr, &a.DefaultValue, &a.SourceKind, &a.CollectorPluginID, &a.SortOrder); err != nil {
			return nil, err
		}
		a.FieldType = FieldType(ft)
		_ = json.Unmarshal([]byte(enumsStr), &a.EnumOptions)
		if a.EnumOptions == nil {
			a.EnumOptions = []string{}
		}
		m.Attributes = append(m.Attributes, a)
	}
	return &m, nil
}

func (s *Service) CreateModelGroup(ctx context.Context, projectID, code, name string, sortOrder int) (*ModelGroup, error) {
	if err := s.EnsureDefaults(ctx, projectID); err != nil {
		return nil, err
	}
	code = strings.TrimSpace(code)
	name = strings.TrimSpace(name)
	if code == "" || name == "" {
		return nil, invalidErr("group code and name are required")
	}
	now := time.Now().UTC()
	g := &ModelGroup{
		ID:        domain.NewID("mgrp"),
		ProjectID: projectID,
		Code:      code,
		Name:      name,
		SortOrder: sortOrder,
		IsBuiltin: false,
		CreatedAt: now,
	}
	_, err := s.writeDB.ExecRaw(ctx,
		`INSERT INTO cmdb_model_groups (id, project_id, code, name, sort_order, is_builtin, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		g.ID, g.ProjectID, g.Code, g.Name, g.SortOrder, false, now.Format(time.RFC3339Nano),
	)
	if err != nil {
		return nil, conflictErr("model group code already exists: " + err.Error())
	}
	return g, nil
}

func (s *Service) CreateModel(ctx context.Context, projectID string, m *Model) (*Model, error) {
	if err := s.EnsureDefaults(ctx, projectID); err != nil {
		return nil, err
	}
	m.Code = strings.TrimSpace(m.Code)
	m.Name = strings.TrimSpace(m.Name)
	if m.Code == "" || m.Name == "" {
		return nil, invalidErr("model code and name are required")
	}
	if m.GroupCode == "" {
		m.GroupCode = "t_app"
	}
	if m.Icon == "" {
		m.Icon = "Box"
	}
	now := time.Now().UTC()
	m.ID = domain.NewID("mdl")
	m.ProjectID = projectID
	m.IsBuiltin = false
	m.CreatedAt, m.UpdatedAt = now, now
	_, err := s.writeDB.ExecRaw(ctx,
		`INSERT INTO cmdb_models (id, project_id, group_code, code, name, icon, description, pos_x, pos_y, is_builtin, is_mainline, mainline_parent_code, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID, m.ProjectID, m.GroupCode, m.Code, m.Name, m.Icon, m.Description, m.PosX, m.PosY, false, m.IsMainline, m.MainlineParentCode, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
	)
	if err != nil {
		return nil, conflictErr("model code already exists: " + err.Error())
	}
	m.Attributes = []ModelAttribute{}
	return m, nil
}

func (s *Service) UpdateModelPosition(ctx context.Context, projectID, modelCode string, posX, posY float64) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.writeDB.ExecRaw(ctx,
		`UPDATE cmdb_models SET pos_x = ?, pos_y = ?, updated_at = ? WHERE project_id = ? AND code = ?`,
		posX, posY, now, projectID, modelCode,
	)
	return err
}

func (s *Service) DeleteModel(ctx context.Context, projectID, modelCode string) error {
	m, err := s.GetModel(ctx, projectID, modelCode)
	if err != nil {
		return err
	}
	if m.IsBuiltin {
		return invalidErr("built-in core models cannot be deleted")
	}
	_, _ = s.writeDB.ExecRaw(ctx, `DELETE FROM cmdb_model_attributes WHERE project_id = ? AND model_code = ?`, projectID, modelCode)
	_, _ = s.writeDB.ExecRaw(ctx, `DELETE FROM cmdb_model_relations WHERE project_id = ? AND (source_model = ? OR target_model = ?)`, projectID, modelCode, modelCode)
	_, err = s.writeDB.ExecRaw(ctx, `DELETE FROM cmdb_models WHERE project_id = ? AND code = ?`, projectID, modelCode)
	return err
}

func (s *Service) UpsertModelAttribute(ctx context.Context, projectID, modelCode string, a *ModelAttribute) (*ModelAttribute, error) {
	if _, err := s.GetModel(ctx, projectID, modelCode); err != nil {
		return nil, err
	}
	a.PropID = strings.TrimSpace(a.PropID)
	a.PropName = strings.TrimSpace(a.PropName)
	if a.PropID == "" || a.PropName == "" {
		return nil, invalidErr("propId and propName are required")
	}
	if a.AttrGroup == "" {
		a.AttrGroup = "基础属性"
	}
	if a.FieldType == "" {
		a.FieldType = FieldString
	}
	if a.SourceKind == "" {
		a.SourceKind = "manual"
	}
	if a.EnumOptions == nil {
		a.EnumOptions = []string{}
	}
	enumsB, _ := json.Marshal(a.EnumOptions)
	if a.ID == "" {
		a.ID = domain.NewID("mattr")
	}
	a.ProjectID = projectID
	a.ModelCode = modelCode
	_, err := s.writeDB.ExecRaw(ctx,
		`INSERT INTO cmdb_model_attributes (id, project_id, model_code, attr_group, prop_id, prop_name, field_type, unit, required, readonly, is_unique, display, enum_options_json, default_value, source_kind, collector_plugin_id, sort_order)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (project_id, model_code, prop_id) DO UPDATE SET
		   attr_group = excluded.attr_group,
		   prop_name = excluded.prop_name,
		   field_type = excluded.field_type,
		   unit = excluded.unit,
		   required = excluded.required,
		   readonly = excluded.readonly,
		   is_unique = excluded.is_unique,
		   display = excluded.display,
		   enum_options_json = excluded.enum_options_json,
		   default_value = excluded.default_value,
		   source_kind = excluded.source_kind,
		   collector_plugin_id = excluded.collector_plugin_id,
		   sort_order = excluded.sort_order`,
		a.ID, projectID, modelCode, a.AttrGroup, a.PropID, a.PropName, string(a.FieldType), a.Unit, a.Required, a.Readonly, a.IsUnique, a.Display, string(enumsB), a.DefaultValue, a.SourceKind, a.CollectorPluginID, a.SortOrder,
	)
	if err != nil {
		return nil, err
	}
	return a, nil
}

func (s *Service) DeleteModelAttribute(ctx context.Context, projectID, attrID string) error {
	_, err := s.writeDB.ExecRaw(ctx, `DELETE FROM cmdb_model_attributes WHERE project_id = ? AND id = ?`, projectID, attrID)
	return err
}

func (s *Service) CreateModelRelation(ctx context.Context, projectID string, r *ModelRelation) (*ModelRelation, error) {
	if err := s.EnsureDefaults(ctx, projectID); err != nil {
		return nil, err
	}
	if r.SourceModel == "" || r.TargetModel == "" || r.RelationType == "" {
		return nil, invalidErr("sourceModel, targetModel and relationType are required")
	}
	if r.MappingKind == "" {
		r.MappingKind = "1:N"
	}
	if r.Label == "" {
		r.Label = r.RelationType
	}
	now := time.Now().UTC()
	r.ID = domain.NewID("mrel")
	r.ProjectID = projectID
	r.CreatedAt = now
	_, err := s.writeDB.ExecRaw(ctx,
		`INSERT INTO cmdb_model_relations (id, project_id, source_model, target_model, relation_type, label, mapping_kind, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, projectID, r.SourceModel, r.TargetModel, r.RelationType, r.Label, r.MappingKind, now.Format(time.RFC3339Nano),
	)
	if err != nil {
		return nil, conflictErr("model relation already exists: " + err.Error())
	}
	return r, nil
}

func (s *Service) DeleteModelRelation(ctx context.Context, projectID, relID string) error {
	_, err := s.writeDB.ExecRaw(ctx, `DELETE FROM cmdb_model_relations WHERE project_id = ? AND id = ?`, projectID, relID)
	return err
}

// --- Dynamic JSONB Schema Validator (LiveOps 03.3) ---

func coerceAttrValue(attr ModelAttribute, raw string) (any, error) {
	switch attr.FieldType {
	case FieldInt, FieldLong:
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("字段 %s(%s) 需要整数，实际值 %q", attr.PropName, attr.PropID, raw)
		}
		return n, nil
	case FieldDouble:
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, fmt.Errorf("字段 %s(%s) 需要数值，实际值 %q", attr.PropName, attr.PropID, raw)
		}
		return f, nil
	case FieldBool:
		b, err := strconv.ParseBool(strings.ToLower(raw))
		if err != nil {
			return nil, fmt.Errorf("字段 %s(%s) 需要布尔值(true/false)，实际值 %q", attr.PropName, attr.PropID, raw)
		}
		return b, nil
	case FieldIP:
		if _, err := netip.ParseAddr(raw); err != nil {
			return nil, fmt.Errorf("字段 %s(%s) 需要合法 IP 地址，实际值 %q", attr.PropName, attr.PropID, raw)
		}
		return raw, nil
	case FieldEnum:
		if len(attr.EnumOptions) > 0 {
			for _, opt := range attr.EnumOptions {
				if opt == raw {
					return raw, nil
				}
			}
			return nil, fmt.Errorf("字段 %s(%s) 值 %q 不在允许枚举 [%s] 内", attr.PropName, attr.PropID, raw, strings.Join(attr.EnumOptions, ", "))
		}
		return raw, nil
	default:
		return raw, nil
	}
}

func validateAttrs(schema []ModelAttribute, incoming map[string]any, existing map[string]any) error {
	if incoming == nil {
		incoming = map[string]any{}
	}
	for _, a := range schema {
		val, ok := incoming[a.PropID]
		isEmpty := !ok || val == nil || strings.TrimSpace(fmt.Sprint(val)) == ""
		if a.Required && isEmpty {
			return invalidErr(fmt.Sprintf("必填字段「%s (%s)」不能为空", a.PropName, a.PropID))
		}
		if existing != nil && a.Readonly {
			oldVal := existing[a.PropID]
			if !isEmpty && oldVal != nil && fmt.Sprint(oldVal) != fmt.Sprint(val) {
				return invalidErr(fmt.Sprintf("只读字段「%s (%s)」不可修改", a.PropName, a.PropID))
			}
		}
		if isEmpty {
			continue
		}
		switch a.FieldType {
		case FieldInt, FieldLong:
			if _, err := strconv.ParseInt(fmt.Sprint(val), 10, 64); err != nil {
				if _, ferr := strconv.ParseFloat(fmt.Sprint(val), 64); ferr != nil {
					return invalidErr(fmt.Sprintf("字段「%s」必须为整数", a.PropName))
				}
			}
		case FieldDouble:
			if _, err := strconv.ParseFloat(fmt.Sprint(val), 64); err != nil {
				return invalidErr(fmt.Sprintf("字段「%s」必须为数值", a.PropName))
			}
		case FieldIP:
			if _, err := netip.ParseAddr(strings.TrimSpace(fmt.Sprint(val))); err != nil {
				return invalidErr(fmt.Sprintf("字段「%s」必须为合法 IPv4/IPv6 地址", a.PropName))
			}
		case FieldEnum:
			if len(a.EnumOptions) > 0 {
				sv := fmt.Sprint(val)
				matched := false
				for _, opt := range a.EnumOptions {
					if opt == sv {
						matched = true
						break
					}
				}
				if !matched {
					return invalidErr(fmt.Sprintf("字段「%s」取值 %q 不在枚举范围 [%s] 内", a.PropName, sv, strings.Join(a.EnumOptions, ", ")))
				}
			}
		}
	}
	return nil
}

// --- Generic CI Instances + History + Lineage Graph ---

func (s *Service) ListCIInstances(ctx context.Context, projectID, modelCode, keyword string) ([]CIInstance, error) {
	if err := s.EnsureDefaults(ctx, projectID); err != nil {
		return nil, err
	}
	q := `SELECT ci.id, ci.project_id, ci.model_code, COALESCE(m.name, ci.model_code), ci.name, ci.unique_key, ci.status, ci.env, ci.owner, ci.attrs_json, ci.version, ci.created_by, ci.created_at, ci.updated_at
	      FROM cmdb_ci_instances ci
	      LEFT JOIN cmdb_models m ON m.project_id = ci.project_id AND m.code = ci.model_code
	      WHERE ci.project_id = ?`
	args := []any{projectID}
	if modelCode != "" && modelCode != "all" {
		q += ` AND ci.model_code = ?`
		args = append(args, modelCode)
	}
	q += ` ORDER BY ci.updated_at DESC, ci.name ASC`

	rows, err := s.readDB.QueryRaw(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]CIInstance, 0)
	kw := strings.ToLower(strings.TrimSpace(keyword))
	for rows.Next() {
		var ci CIInstance
		var attrsStr, cAt, uAt string
		if err := rows.Scan(&ci.ID, &ci.ProjectID, &ci.ModelCode, &ci.ModelName, &ci.Name, &ci.UniqueKey, &ci.Status, &ci.Env, &ci.Owner, &attrsStr, &ci.Version, &ci.CreatedBy, &cAt, &uAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(attrsStr), &ci.Attrs)
		if ci.Attrs == nil {
			ci.Attrs = map[string]any{}
		}
		ci.CreatedAt, ci.UpdatedAt = parseTS(cAt), parseTS(uAt)
		if kw != "" {
			hay := strings.ToLower(ci.Name + " " + ci.UniqueKey + " " + ci.Owner + " " + ci.Env + " " + attrsStr)
			if !strings.Contains(hay, kw) {
				continue
			}
		}
		out = append(out, ci)
	}
	return out, rows.Err()
}

func (s *Service) GetCIInstance(ctx context.Context, projectID, ciID string) (*CIInstance, error) {
	var ci CIInstance
	var attrsStr, cAt, uAt string
	err := s.readDB.ScanRaw(ctx, `SELECT ci.id, ci.project_id, ci.model_code, COALESCE(m.name, ci.model_code), ci.name, ci.unique_key, ci.status, ci.env, ci.owner, ci.attrs_json, ci.version, ci.created_by, ci.created_at, ci.updated_at
		 FROM cmdb_ci_instances ci
		 LEFT JOIN cmdb_models m ON m.project_id = ci.project_id AND m.code = ci.model_code
		 WHERE ci.project_id = ? AND ci.id = ?`, []any{projectID, ciID}, &ci.ID, &ci.ProjectID, &ci.ModelCode, &ci.ModelName, &ci.Name, &ci.UniqueKey, &ci.Status, &ci.Env, &ci.Owner, &attrsStr, &ci.Version, &ci.CreatedBy, &cAt, &uAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFoundErr("cmdb_ci_instance", ciID)
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(attrsStr), &ci.Attrs)
	if ci.Attrs == nil {
		ci.Attrs = map[string]any{}
	}
	ci.CreatedAt, ci.UpdatedAt = parseTS(cAt), parseTS(uAt)
	return &ci, nil
}

func (s *Service) findCIByUniqueOrName(ctx context.Context, projectID, modelCode, uniqueKey, name string) (*CIInstance, error) {
	var id string
	if uniqueKey != "" {
		if err := s.readDB.ScanRaw(ctx, `SELECT id FROM cmdb_ci_instances WHERE project_id = ? AND model_code = ? AND unique_key = ?`, []any{projectID, modelCode, uniqueKey}, &id); err == nil && id != "" {
			return s.GetCIInstance(ctx, projectID, id)
		}
	}
	if name != "" {
		if err := s.readDB.ScanRaw(ctx, `SELECT id FROM cmdb_ci_instances WHERE project_id = ? AND model_code = ? AND name = ?`, []any{projectID, modelCode, name}, &id); err == nil && id != "" {
			return s.GetCIInstance(ctx, projectID, id)
		}
	}
	return nil, notFoundErr("cmdb_ci_instance", name)
}

func (s *Service) CreateCIInstance(ctx context.Context, projectID, actor string, ci *CIInstance) (*CIInstance, error) {
	mdl, err := s.GetModel(ctx, projectID, ci.ModelCode)
	if err != nil {
		return nil, err
	}
	ci.Name = strings.TrimSpace(ci.Name)
	if ci.Name == "" {
		return nil, invalidErr("CI 名称不能为空")
	}
	if ci.Status == "" {
		ci.Status = "active"
	}
	if ci.Env == "" {
		ci.Env = "prod"
	}
	if ci.Attrs == nil {
		ci.Attrs = map[string]any{}
	}
	if err := validateAttrs(mdl.Attributes, ci.Attrs, nil); err != nil {
		return nil, err
	}
	// Derive uniqueKey from first unique attribute if not provided
	if ci.UniqueKey == "" {
		for _, a := range mdl.Attributes {
			if a.IsUnique {
				if v, ok := ci.Attrs[a.PropID]; ok && strings.TrimSpace(fmt.Sprint(v)) != "" {
					ci.UniqueKey = strings.TrimSpace(fmt.Sprint(v))
					break
				}
			}
		}
	}
	now := time.Now().UTC()
	ci.ID = domain.NewID("ci")
	ci.ProjectID = projectID
	ci.ModelName = mdl.Name
	ci.Version = 1
	ci.CreatedBy = actor
	ci.CreatedAt, ci.UpdatedAt = now, now
	attrsB, _ := json.Marshal(ci.Attrs)

	_, err = s.writeDB.ExecRaw(ctx,
		`INSERT INTO cmdb_ci_instances (id, project_id, model_code, name, unique_key, status, env, owner, attrs_json, version, created_by, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?)`,
		ci.ID, projectID, ci.ModelCode, ci.Name, ci.UniqueKey, ci.Status, ci.Env, ci.Owner, string(attrsB), actor, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
	)
	if err != nil {
		return nil, conflictErr("CI 唯一键冲突或写入失败: " + err.Error())
	}
	s.recordCIHistory(ctx, projectID, ci.ID, ci.ModelCode, "create", nil, ci.Attrs, actor)
	return ci, nil
}

func (s *Service) UpdateCIInstance(ctx context.Context, projectID, ciID, actor string, patch *CIInstance) (*CIInstance, error) {
	existing, err := s.GetCIInstance(ctx, projectID, ciID)
	if err != nil {
		return nil, err
	}
	mdl, err := s.GetModel(ctx, projectID, existing.ModelCode)
	if err != nil {
		return nil, err
	}
	if patch.Name != "" {
		existing.Name = strings.TrimSpace(patch.Name)
	}
	if patch.UniqueKey != "" {
		existing.UniqueKey = strings.TrimSpace(patch.UniqueKey)
	}
	if patch.Status != "" {
		existing.Status = patch.Status
	}
	if patch.Env != "" {
		existing.Env = patch.Env
	}
	if patch.Owner != "" {
		existing.Owner = patch.Owner
	}
	before := make(map[string]any, len(existing.Attrs))
	for k, v := range existing.Attrs {
		before[k] = v
	}
	merged := make(map[string]any, len(existing.Attrs))
	for k, v := range existing.Attrs {
		merged[k] = v
	}
	for k, v := range patch.Attrs {
		merged[k] = v
	}
	if err := validateAttrs(mdl.Attributes, merged, before); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	existing.Attrs = merged
	existing.Version++
	existing.UpdatedAt = now
	attrsB, _ := json.Marshal(existing.Attrs)
	_, err = s.writeDB.ExecRaw(ctx,
		`UPDATE cmdb_ci_instances SET name = ?, unique_key = ?, status = ?, env = ?, owner = ?, attrs_json = ?, version = ?, updated_at = ?
		 WHERE project_id = ? AND id = ?`,
		existing.Name, existing.UniqueKey, existing.Status, existing.Env, existing.Owner, string(attrsB), existing.Version, now.Format(time.RFC3339Nano), projectID, ciID,
	)
	if err != nil {
		return nil, conflictErr("更新 CI 实例失败: " + err.Error())
	}
	s.recordCIHistory(ctx, projectID, existing.ID, existing.ModelCode, "update", before, existing.Attrs, actor)
	return existing, nil
}

func (s *Service) DeleteCIInstance(ctx context.Context, projectID, ciID, actor string) error {
	existing, err := s.GetCIInstance(ctx, projectID, ciID)
	if err != nil {
		return err
	}
	_, err = s.writeDB.ExecRaw(ctx, `DELETE FROM cmdb_ci_instances WHERE project_id = ? AND id = ?`, projectID, ciID)
	if err == nil {
		s.recordCIHistory(ctx, projectID, existing.ID, existing.ModelCode, "delete", existing.Attrs, nil, actor)
	}
	return err
}

func (s *Service) recordCIHistory(ctx context.Context, projectID, ciID, modelCode, action string, before, after map[string]any, actor string) {
	if before == nil {
		before = map[string]any{}
	}
	if after == nil {
		after = map[string]any{}
	}
	bJSON, _ := json.Marshal(before)
	aJSON, _ := json.Marshal(after)
	_, _ = s.writeDB.ExecRaw(ctx,
		`INSERT INTO cmdb_ci_history (id, project_id, ci_id, model_code, action, before_json, after_json, actor, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		domain.NewID("cih"), projectID, ciID, modelCode, action, string(bJSON), string(aJSON), actor, time.Now().UTC().Format(time.RFC3339Nano),
	)
}

func (s *Service) ListCIHistory(ctx context.Context, projectID, ciID string, limit int) ([]CIHistoryEntry, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	q := `SELECT id, project_id, ci_id, model_code, action, before_json, after_json, actor, created_at
	      FROM cmdb_ci_history WHERE project_id = ?`
	args := []any{projectID}
	if ciID != "" {
		q += ` AND ci_id = ?`
		args = append(args, ciID)
	}
	q += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.readDB.QueryRaw(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]CIHistoryEntry, 0)
	for rows.Next() {
		var h CIHistoryEntry
		var bStr, aStr, cAt string
		if err := rows.Scan(&h.ID, &h.ProjectID, &h.CIID, &h.ModelCode, &h.Action, &bStr, &aStr, &h.Actor, &cAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(bStr), &h.Before)
		_ = json.Unmarshal([]byte(aStr), &h.After)
		h.CreatedAt = parseTS(cAt)
		out = append(out, h)
	}
	return out, rows.Err()
}

func (s *Service) CreateCIRelation(ctx context.Context, projectID string, rel *CIRelation) (*CIRelation, error) {
	if rel.SourceCIID == "" || rel.TargetCIID == "" || rel.SourceCIID == rel.TargetCIID {
		return nil, invalidErr("sourceCiId and distinct targetCiId are required")
	}
	if rel.RelationType == "" {
		rel.RelationType = "depends_on"
	}
	if rel.Label == "" {
		rel.Label = rel.RelationType
	}
	now := time.Now().UTC()
	rel.ID = domain.NewID("cirel")
	rel.ProjectID = projectID
	rel.CreatedAt = now
	_, err := s.writeDB.ExecRaw(ctx,
		`INSERT INTO cmdb_ci_relations (id, project_id, source_ci_id, target_ci_id, relation_type, label, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		rel.ID, projectID, rel.SourceCIID, rel.TargetCIID, rel.RelationType, rel.Label, now.Format(time.RFC3339Nano),
	)
	if err != nil {
		return nil, conflictErr("CI 关联关系已存在或目标 CI 不存在: " + err.Error())
	}
	return rel, nil
}

func (s *Service) DeleteCIRelation(ctx context.Context, projectID, relID string) error {
	_, err := s.writeDB.ExecRaw(ctx, `DELETE FROM cmdb_ci_relations WHERE project_id = ? AND id = ?`, projectID, relID)
	return err
}

// GetCILineageGraph runs a multi-hop BFS (up to maxDepth=3) upstream & downstream from rootCIID.
func (s *Service) GetCILineageGraph(ctx context.Context, projectID, rootCIID string, maxDepth int) (*CIGraph, error) {
	root, err := s.GetCIInstance(ctx, projectID, rootCIID)
	if err != nil {
		return nil, err
	}
	if maxDepth <= 0 || maxDepth > 5 {
		maxDepth = 3
	}

	// Load all relations for project
	allRels := make([]CIRelation, 0)
	rRows, err := s.readDB.QueryRaw(ctx,
		`SELECT id, project_id, source_ci_id, target_ci_id, relation_type, label, created_at
		 FROM cmdb_ci_relations WHERE project_id = ?`, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rRows.Close() }()
	for rRows.Next() {
		var r CIRelation
		var cAt string
		if err := rRows.Scan(&r.ID, &r.ProjectID, &r.SourceCIID, &r.TargetCIID, &r.RelationType, &r.Label, &cAt); err == nil {
			r.CreatedAt = parseTS(cAt)
			allRels = append(allRels, r)
		}
	}

	type qItem struct {
		id    string
		depth int
		dir   string
	}
	visited := map[string]qItem{root.ID: {id: root.ID, depth: 0, dir: "root"}}
	queue := []qItem{{id: root.ID, depth: 0, dir: "root"}}
	edgeSeen := map[string]bool{}
	matchedEdges := make([]CIRelation, 0)

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur.depth >= maxDepth {
			continue
		}
		for _, r := range allRels {
			if r.SourceCIID == cur.id {
				if !edgeSeen[r.ID] {
					edgeSeen[r.ID] = true
					matchedEdges = append(matchedEdges, r)
				}
				if _, ok := visited[r.TargetCIID]; !ok {
					nxt := qItem{id: r.TargetCIID, depth: cur.depth + 1, dir: "downstream"}
					visited[r.TargetCIID] = nxt
					queue = append(queue, nxt)
				}
			}
			if r.TargetCIID == cur.id {
				if !edgeSeen[r.ID] {
					edgeSeen[r.ID] = true
					matchedEdges = append(matchedEdges, r)
				}
				if _, ok := visited[r.SourceCIID]; !ok {
					nxt := qItem{id: r.SourceCIID, depth: cur.depth + 1, dir: "upstream"}
					visited[r.SourceCIID] = nxt
					queue = append(queue, nxt)
				}
			}
		}
	}

	nodes := make([]CIGraphNode, 0, len(visited))
	for id, info := range visited {
		ci, err := s.GetCIInstance(ctx, projectID, id)
		if err != nil {
			continue
		}
		nodes = append(nodes, CIGraphNode{
			ID:        ci.ID,
			ModelCode: ci.ModelCode,
			ModelName: ci.ModelName,
			Name:      ci.Name,
			Status:    ci.Status,
			Env:       ci.Env,
			Depth:     info.depth,
			Direction: info.dir,
		})
	}

	return &CIGraph{
		RootID: root.ID,
		Nodes:  nodes,
		Edges:  matchedEdges,
	}, nil
}

// GetOverviewStats aggregates CMDB asset totals, model breakdown, DCIM U utilization, IPAM usage & recent changes.
func (s *Service) GetOverviewStats(ctx context.Context, projectID string) (*OverviewStats, error) {
	if err := s.EnsureDefaults(ctx, projectID); err != nil {
		return nil, err
	}
	st := &OverviewStats{
		ModelBreakdown:  make(map[string]int),
		RecentHistories: make([]CIHistoryEntry, 0),
	}
	_ = s.readDB.ScanRaw(ctx, `SELECT COUNT(1) FROM cmdb_models WHERE project_id = ?`, []any{projectID}, &st.TotalModels)
	_ = s.readDB.ScanRaw(ctx, `SELECT COUNT(1) FROM cmdb_ci_instances WHERE project_id = ?`, []any{projectID}, &st.TotalCIs)
	_ = s.readDB.ScanRaw(ctx, `SELECT COUNT(1) FROM cmdb_ci_relations WHERE project_id = ?`, []any{projectID}, &st.TotalRelations)

	rows, err := s.readDB.QueryRaw(ctx,
		`SELECT COALESCE(m.name, ci.model_code), COUNT(1)
		 FROM cmdb_ci_instances ci
		 LEFT JOIN cmdb_models m ON m.project_id = ci.project_id AND m.code = ci.model_code
		 WHERE ci.project_id = ? GROUP BY COALESCE(m.name, ci.model_code)`, projectID)
	if err == nil {
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var name string
			var cnt int
			if err := rows.Scan(&name, &cnt); err == nil {
				st.ModelBreakdown[name] = cnt
			}
		}
	}

	racks, _ := s.ListRacks(ctx, projectID)
	st.TotalRacks = len(racks)
	for _, r := range racks {
		st.TotalRackU += r.UHeight
		st.UsedRackU += r.UsedU
	}
	prefixes, _ := s.ListPrefixes(ctx, projectID)
	st.TotalPrefixes = len(prefixes)
	for _, p := range prefixes {
		st.TotalUsableIPs += p.TotalUsable
		st.AllocatedIPs += p.UsedCount
	}
	st.RecentHistories, _ = s.ListCIHistory(ctx, projectID, "", 12)
	return st, nil
}

package cmdb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/kinstall/kinstall/pkg/platform/db"
	"github.com/kinstall/kinstall/pkg/platform/id"
)

type Service struct {
	store *db.Store
}

func NewService(store *db.Store) *Service {
	return &Service{store: store}
}

// =============================================================================
// Dynamic Meta-Model Engine
// =============================================================================

func (s *Service) ListModelGroups(ctx context.Context, wsID string) ([]ModelGroup, error) {
	rows, err := s.store.Query(ctx,
		`SELECT id, workspace_id, code, name, sort_order, created_at FROM cmdb_model_groups WHERE workspace_id = ? ORDER BY sort_order ASC, name ASC`,
		wsID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]ModelGroup, 0)
	for rows.Next() {
		var g ModelGroup
		if err := rows.Scan(&g.ID, &g.WorkspaceID, &g.Code, &g.Name, &g.SortOrder, &g.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *Service) CreateModelGroup(ctx context.Context, wsID, code, name string, sortOrder int) (*ModelGroup, error) {
	code = strings.ToLower(strings.TrimSpace(code))
	name = strings.TrimSpace(name)
	if code == "" || name == "" {
		return nil, errors.New("分组标识与名称不能为空")
	}
	g := &ModelGroup{
		ID:          id.New("mgrp"),
		WorkspaceID: wsID,
		Code:        code,
		Name:        name,
		SortOrder:   sortOrder,
		CreatedAt:   nowISO(),
	}
	if _, err := s.store.Exec(ctx,
		`INSERT INTO cmdb_model_groups(id, workspace_id, code, name, sort_order, created_at) VALUES(?,?,?,?,?,?)`,
		g.ID, g.WorkspaceID, g.Code, g.Name, g.SortOrder, g.CreatedAt,
	); err != nil {
		return nil, err
	}
	return g, nil
}

func (s *Service) ListModels(ctx context.Context, wsID string, includeAttrs bool) ([]Model, error) {
	rows, err := s.store.Query(ctx,
		`SELECT m.id, m.workspace_id, m.group_code, m.code, m.name, m.icon, m.description, m.is_builtin, m.pos_x, m.pos_y, m.created_at, m.updated_at,
		        (SELECT COUNT(1) FROM cmdb_ci_instances c WHERE c.workspace_id = m.workspace_id AND c.model_code = m.code) AS cnt
		 FROM cmdb_models m WHERE m.workspace_id = ? ORDER BY m.is_builtin DESC, m.code ASC`,
		wsID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]Model, 0)
	for rows.Next() {
		var (
			m       Model
			builtin int
		)
		if err := rows.Scan(&m.ID, &m.WorkspaceID, &m.GroupCode, &m.Code, &m.Name, &m.Icon, &m.Description, &builtin, &m.PosX, &m.PosY, &m.CreatedAt, &m.UpdatedAt, &m.InstanceCount); err != nil {
			return nil, err
		}
		m.IsBuiltin = builtin == 1
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if includeAttrs {
		for i := range out {
			attrs, err := s.ListModelAttrs(ctx, wsID, out[i].Code)
			if err != nil {
				return nil, err
			}
			out[i].Attrs = attrs
		}
	}
	return out, nil
}

func (s *Service) CreateModel(ctx context.Context, wsID, groupCode, code, name, icon, desc string) (*Model, error) {
	code = strings.ToLower(strings.TrimSpace(code))
	name = strings.TrimSpace(name)
	if code == "" || name == "" {
		return nil, errors.New("模型编码和名称不能为空")
	}
	if groupCode == "" {
		groupCode = "application"
	}
	if icon == "" {
		icon = "box"
	}
	now := nowISO()
	m := &Model{
		ID:          id.New("mdl"),
		WorkspaceID: wsID,
		GroupCode:   groupCode,
		Code:        code,
		Name:        name,
		Icon:        icon,
		Description: strings.TrimSpace(desc),
		PosX:        220,
		PosY:        180,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if _, err := s.store.Exec(ctx,
		`INSERT INTO cmdb_models(id, workspace_id, group_code, code, name, icon, description, is_builtin, pos_x, pos_y, created_at, updated_at)
		 VALUES(?,?,?,?,?,?,?,0,?,?,?,?)`,
		m.ID, m.WorkspaceID, m.GroupCode, m.Code, m.Name, m.Icon, m.Description, m.PosX, m.PosY, now, now,
	); err != nil {
		return nil, fmt.Errorf("创建模型失败: %w", err)
	}
	return m, nil
}

func (s *Service) DeleteModel(ctx context.Context, wsID, code string) error {
	var builtin int
	if err := s.store.QueryRow(ctx, `SELECT is_builtin FROM cmdb_models WHERE workspace_id = ? AND code = ?`, wsID, code).Scan(&builtin); err != nil {
		return errors.New("模型不存在")
	}
	if builtin == 1 {
		return errors.New("内置核心模型不允许删除")
	}
	var cnt int
	_ = s.store.QueryRow(ctx, `SELECT COUNT(1) FROM cmdb_ci_instances WHERE workspace_id = ? AND model_code = ?`, wsID, code).Scan(&cnt)
	if cnt > 0 {
		return fmt.Errorf("该模型下仍存在 %d 条 CI 实例，请先清理后再删除", cnt)
	}
	_, err := s.store.Exec(ctx, `DELETE FROM cmdb_models WHERE workspace_id = ? AND code = ?`, wsID, code)
	return err
}

func (s *Service) SaveModelPositions(ctx context.Context, wsID string, positions []ModelPosition) error {
	now := nowISO()
	return s.store.WithTx(ctx, func(ctx context.Context, tx *db.Tx) error {
		for _, p := range positions {
			if _, err := tx.Exec(ctx,
				`UPDATE cmdb_models SET pos_x = ?, pos_y = ?, updated_at = ? WHERE workspace_id = ? AND code = ?`,
				p.PosX, p.PosY, now, wsID, p.Code,
			); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Service) ListModelAttrs(ctx context.Context, wsID, modelCode string) ([]ModelAttr, error) {
	rows, err := s.store.Query(ctx,
		`SELECT id, workspace_id, model_code, attr_key, name, field_type, group_name, required, readonly, is_unique, default_value, options_json, sort_order, created_at
		 FROM cmdb_model_attrs WHERE workspace_id = ? AND model_code = ? ORDER BY sort_order ASC, attr_key ASC`,
		wsID, modelCode,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]ModelAttr, 0)
	for rows.Next() {
		var (
			a             ModelAttr
			req, ro, uniq int
			optsRaw       string
		)
		if err := rows.Scan(&a.ID, &a.WorkspaceID, &a.ModelCode, &a.AttrKey, &a.Name, &a.FieldType, &a.GroupName, &req, &ro, &uniq, &a.DefaultValue, &optsRaw, &a.SortOrder, &a.CreatedAt); err != nil {
			return nil, err
		}
		a.Required = req == 1
		a.Readonly = ro == 1
		a.IsUnique = uniq == 1
		a.Options = make([]string, 0)
		_ = json.Unmarshal([]byte(optsRaw), &a.Options)
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Service) UpsertModelAttr(ctx context.Context, wsID string, a ModelAttr) (*ModelAttr, error) {
	a.AttrKey = strings.ToLower(strings.TrimSpace(a.AttrKey))
	a.Name = strings.TrimSpace(a.Name)
	if a.ModelCode == "" || a.AttrKey == "" || a.Name == "" {
		return nil, errors.New("模型编码、属性键与名称不能为空")
	}
	if a.FieldType == "" {
		a.FieldType = "string"
	}
	if a.GroupName == "" {
		a.GroupName = "基础属性"
	}
	if a.Options == nil {
		a.Options = make([]string, 0)
	}
	a.ID = id.New("attr")
	a.WorkspaceID = wsID
	a.CreatedAt = nowISO()
	req, ro, uniq := boolInt(a.Required), boolInt(a.Readonly), boolInt(a.IsUnique)
	if _, err := s.store.Exec(ctx,
		`INSERT INTO cmdb_model_attrs(id, workspace_id, model_code, attr_key, name, field_type, group_name, required, readonly, is_unique, default_value, options_json, sort_order, created_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(workspace_id, model_code, attr_key) DO UPDATE SET
		   name=excluded.name, field_type=excluded.field_type, group_name=excluded.group_name,
		   required=excluded.required, readonly=excluded.readonly, is_unique=excluded.is_unique,
		   default_value=excluded.default_value, options_json=excluded.options_json, sort_order=excluded.sort_order`,
		a.ID, wsID, a.ModelCode, a.AttrKey, a.Name, a.FieldType, a.GroupName, req, ro, uniq, a.DefaultValue, mustJSON(a.Options), a.SortOrder, a.CreatedAt,
	); err != nil {
		return nil, err
	}
	return &a, nil
}

func (s *Service) DeleteModelAttr(ctx context.Context, wsID, attrID string) error {
	_, err := s.store.Exec(ctx, `DELETE FROM cmdb_model_attrs WHERE workspace_id = ? AND id = ?`, wsID, attrID)
	return err
}

func (s *Service) ListModelRelations(ctx context.Context, wsID string) ([]ModelRelation, error) {
	rows, err := s.store.Query(ctx,
		`SELECT id, workspace_id, source_model, target_model, relation_code, relation_name, cardinality, created_at
		 FROM cmdb_model_relations WHERE workspace_id = ? ORDER BY created_at ASC`,
		wsID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]ModelRelation, 0)
	for rows.Next() {
		var r ModelRelation
		if err := rows.Scan(&r.ID, &r.WorkspaceID, &r.SourceModel, &r.TargetModel, &r.RelationCode, &r.RelationName, &r.Cardinality, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Service) CreateModelRelation(ctx context.Context, wsID, src, dst, code, name, card string) (*ModelRelation, error) {
	if src == "" || dst == "" || code == "" {
		return nil, errors.New("源模型、目标模型与关系标识不能为空")
	}
	if name == "" {
		name = code
	}
	if card == "" {
		card = "N:M"
	}
	r := &ModelRelation{
		ID:           id.New("mrel"),
		WorkspaceID:  wsID,
		SourceModel:  src,
		TargetModel:  dst,
		RelationCode: code,
		RelationName: name,
		Cardinality:  card,
		CreatedAt:    nowISO(),
	}
	if _, err := s.store.Exec(ctx,
		`INSERT INTO cmdb_model_relations(id, workspace_id, source_model, target_model, relation_code, relation_name, cardinality, created_at)
		 VALUES(?,?,?,?,?,?,?,?)`,
		r.ID, wsID, r.SourceModel, r.TargetModel, r.RelationCode, r.RelationName, r.Cardinality, r.CreatedAt,
	); err != nil {
		return nil, err
	}
	return r, nil
}

func (s *Service) DeleteModelRelation(ctx context.Context, wsID, relID string) error {
	_, err := s.store.Exec(ctx, `DELETE FROM cmdb_model_relations WHERE workspace_id = ? AND id = ?`, wsID, relID)
	return err
}

// =============================================================================
// Unified CI Instance & Host-as-CI SSOT Engine
// =============================================================================

func (s *Service) validateCIData(ctx context.Context, wsID, modelCode string, data map[string]any) error {
	attrs, err := s.ListModelAttrs(ctx, wsID, modelCode)
	if err != nil {
		return err
	}
	if data == nil {
		data = make(map[string]any)
	}
	for _, a := range attrs {
		val, exists := data[a.AttrKey]
		empty := !exists || val == nil || strings.TrimSpace(fmt.Sprint(val)) == ""
		if empty {
			if a.DefaultValue != "" {
				data[a.AttrKey] = a.DefaultValue
				continue
			}
			if a.Required {
				return fmt.Errorf("属性 %q (%s) 为必填项", a.Name, a.AttrKey)
			}
			continue
		}
		strVal := strings.TrimSpace(fmt.Sprint(val))
		switch a.FieldType {
		case "ip":
			if net.ParseIP(strVal) == nil {
				return fmt.Errorf("属性 %q (%s) 必须是合法 IP 地址: %q", a.Name, a.AttrKey, strVal)
			}
		case "int":
			if _, err := strconv.ParseFloat(strVal, 64); err != nil {
				return fmt.Errorf("属性 %q (%s) 必须是数字: %q", a.Name, a.AttrKey, strVal)
			}
		case "enum":
			if len(a.Options) > 0 {
				matched := false
				for _, opt := range a.Options {
					if opt == strVal {
						matched = true
						break
					}
				}
				if !matched {
					return fmt.Errorf("属性 %q (%s) 值 %q 不在允许枚举列表 %v 内", a.Name, a.AttrKey, strVal, a.Options)
				}
			}
		}
	}
	return nil
}

func (s *Service) ListCIs(ctx context.Context, wsID, modelCode, env, keyword string) ([]CIInstance, error) {
	var (
		clauses = []string{"c.workspace_id = ?"}
		args    = []any{wsID}
	)
	if strings.TrimSpace(modelCode) != "" {
		clauses = append(clauses, "c.model_code = ?")
		args = append(args, strings.TrimSpace(modelCode))
	}
	if strings.TrimSpace(env) != "" {
		clauses = append(clauses, "c.env = ?")
		args = append(args, strings.TrimSpace(env))
	}
	if strings.TrimSpace(keyword) != "" {
		kw := "%" + strings.TrimSpace(keyword) + "%"
		clauses = append(clauses, "(c.name LIKE ? OR c.unique_key LIKE ? OR c.owner LIKE ? OR c.data_json LIKE ?)")
		args = append(args, kw, kw, kw, kw)
	}
	q := `SELECT c.id, c.workspace_id, c.model_code, c.unique_key, c.name, c.env, c.status, c.owner, c.tags_json, c.data_json, c.created_at, c.updated_at,
	             COALESCE(h.ip,''), COALESCE(h.ssh_port,22), COALESCE(h.ssh_user,'root'), COALESCE(h.credential_id,''), COALESCE(h.exec_mode,'auto'),
	             COALESCE(h.agent_id,''), COALESCE(h.agent_status,''), COALESCE(h.os_family,''), COALESCE(h.os_version,''), COALESCE(h.kernel,''),
	             COALESCE(h.arch,'amd64'), COALESCE(h.cpu_cores,0), COALESCE(h.mem_mb,0), COALESCE(h.disk_gb,0), COALESCE(h.load_avg,''),
	             COALESCE(h.cpu_usage_pct,0), COALESCE(h.mem_usage_pct,0), COALESCE(h.facts_json,'{}'), COALESCE(h.last_seen_at,''), COALESCE(h.updated_at,'')
	      FROM cmdb_ci_instances c
	      LEFT JOIN cmdb_host_endpoints h ON h.ci_id = c.id
	      WHERE ` + strings.Join(clauses, " AND ") + `
	      ORDER BY c.updated_at DESC, c.name ASC`
	rows, err := s.store.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]CIInstance, 0)
	for rows.Next() {
		ci, err := scanCIRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ci)
	}
	return out, rows.Err()
}

func (s *Service) GetCI(ctx context.Context, wsID, ciID string) (*CIInstance, error) {
	q := `SELECT c.id, c.workspace_id, c.model_code, c.unique_key, c.name, c.env, c.status, c.owner, c.tags_json, c.data_json, c.created_at, c.updated_at,
	             COALESCE(h.ip,''), COALESCE(h.ssh_port,22), COALESCE(h.ssh_user,'root'), COALESCE(h.credential_id,''), COALESCE(h.exec_mode,'auto'),
	             COALESCE(h.agent_id,''), COALESCE(h.agent_status,''), COALESCE(h.os_family,''), COALESCE(h.os_version,''), COALESCE(h.kernel,''),
	             COALESCE(h.arch,'amd64'), COALESCE(h.cpu_cores,0), COALESCE(h.mem_mb,0), COALESCE(h.disk_gb,0), COALESCE(h.load_avg,''),
	             COALESCE(h.cpu_usage_pct,0), COALESCE(h.mem_usage_pct,0), COALESCE(h.facts_json,'{}'), COALESCE(h.last_seen_at,''), COALESCE(h.updated_at,'')
	      FROM cmdb_ci_instances c
	      LEFT JOIN cmdb_host_endpoints h ON h.ci_id = c.id
	      WHERE c.workspace_id = ? AND c.id = ?`
	rows, err := s.store.Query(ctx, q, wsID, ciID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return nil, sql.ErrNoRows
	}
	ci, err := scanCIRow(rows)
	if err != nil {
		return nil, err
	}
	return &ci, nil
}

func (s *Service) FindHostByIP(ctx context.Context, wsID, ip string) (*CIInstance, error) {
	var ciID string
	err := s.store.QueryRow(ctx,
		`SELECT ci_id FROM cmdb_host_endpoints WHERE workspace_id = ? AND ip = ? LIMIT 1`,
		wsID, strings.TrimSpace(ip),
	).Scan(&ciID)
	if err != nil {
		return nil, err
	}
	return s.GetCI(ctx, wsID, ciID)
}

func scanCIRow(rows *sql.Rows) (CIInstance, error) {
	var (
		ci       CIInstance
		tagsRaw  string
		dataRaw  string
		ep       HostEndpoint
		factsRaw string
	)
	err := rows.Scan(
		&ci.ID, &ci.WorkspaceID, &ci.ModelCode, &ci.UniqueKey, &ci.Name, &ci.Env, &ci.Status, &ci.Owner, &tagsRaw, &dataRaw, &ci.CreatedAt, &ci.UpdatedAt,
		&ep.IP, &ep.SSHPort, &ep.SSHUser, &ep.CredentialID, &ep.ExecMode,
		&ep.AgentID, &ep.AgentStatus, &ep.OSFamily, &ep.OSVersion, &ep.Kernel,
		&ep.Arch, &ep.CPUCores, &ep.MemMB, &ep.DiskGB, &ep.LoadAvg,
		&ep.CPUUsagePct, &ep.MemUsagePct, &factsRaw, &ep.LastSeenAt, &ep.UpdatedAt,
	)
	if err != nil {
		return ci, err
	}
	ci.Tags = make([]string, 0)
	_ = json.Unmarshal([]byte(tagsRaw), &ci.Tags)
	ci.Data = make(map[string]any)
	_ = json.Unmarshal([]byte(dataRaw), &ci.Data)
	if ci.ModelCode == "m_host" || ep.IP != "" {
		ep.CIID = ci.ID
		ep.Facts = make(map[string]any)
		_ = json.Unmarshal([]byte(factsRaw), &ep.Facts)
		if ep.AgentStatus == "" {
			ep.AgentStatus = "OFFLINE"
		}
		ci.Host = &ep
	}
	return ci, nil
}

func (s *Service) CreateCI(ctx context.Context, wsID, actor string, req UpsertCIRequest) (*CIInstance, error) {
	if req.ModelCode == "m_host" {
		return s.UpsertHostCI(ctx, wsID, actor, req)
	}
	req.ModelCode = strings.TrimSpace(req.ModelCode)
	req.Name = strings.TrimSpace(req.Name)
	req.UniqueKey = strings.TrimSpace(req.UniqueKey)
	if req.ModelCode == "" || req.Name == "" {
		return nil, errors.New("模型编码与实例名称不能为空")
	}
	if req.UniqueKey == "" {
		req.UniqueKey = id.Raw()
	}
	if req.Env == "" {
		req.Env = "prod"
	}
	if req.Status == "" {
		req.Status = "active"
	}
	if req.Tags == nil {
		req.Tags = make([]string, 0)
	}
	if req.Data == nil {
		req.Data = make(map[string]any)
	}
	if err := s.validateCIData(ctx, wsID, req.ModelCode, req.Data); err != nil {
		return nil, err
	}
	now := nowISO()
	ciID := id.New("ci")
	err := s.store.WithTx(ctx, func(ctx context.Context, tx *db.Tx) error {
		if _, err := tx.Exec(ctx,
			`INSERT INTO cmdb_ci_instances(id, workspace_id, model_code, unique_key, name, env, status, owner, tags_json, data_json, created_at, updated_at)
			 VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
			ciID, wsID, req.ModelCode, req.UniqueKey, req.Name, req.Env, req.Status, req.Owner, mustJSON(req.Tags), mustJSON(req.Data), now, now,
		); err != nil {
			return err
		}
		_, err := tx.Exec(ctx,
			`INSERT INTO cmdb_ci_history(id, workspace_id, ci_id, action, actor, before_json, after_json, created_at) VALUES(?,?,?,?,?,?,?,?)`,
			id.New("hist"), wsID, ciID, "create", actor, "{}", mustJSON(req.Data), now,
		)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("创建 CI 实例失败: %w", err)
	}
	return s.GetCI(ctx, wsID, ciID)
}

// UpsertHostCI atomically writes BOTH cmdb_ci_instances(model_code='m_host') AND cmdb_host_endpoints.
// This guarantees ONE Host SSOT across CMDB, Dual Topology, DCIM, WebSSH, Agent Fleet, Jobs & Delivery.
func (s *Service) UpsertHostCI(ctx context.Context, wsID, actor string, req UpsertCIRequest) (*CIInstance, error) {
	req.ModelCode = "m_host"
	if req.Data == nil {
		req.Data = make(map[string]any)
	}
	ep := HostEndpoint{SSHPort: 22, SSHUser: "root", ExecMode: "auto", AgentStatus: "OFFLINE", Arch: "amd64"}
	if req.Host != nil {
		ep = *req.Host
	}
	if ep.IP == "" {
		if v, ok := req.Data["ip"].(string); ok {
			ep.IP = strings.TrimSpace(v)
		}
	}
	if ep.IP == "" {
		ep.IP = strings.TrimSpace(req.UniqueKey)
	}
	if ep.IP == "" {
		return nil, errors.New("主机管理 IP 不能为空")
	}
	req.Data["ip"] = ep.IP
	if req.UniqueKey == "" {
		req.UniqueKey = ep.IP
	}
	if req.Name == "" {
		req.Name = ep.IP
	}
	if req.Env == "" {
		req.Env = "prod"
	}
	if req.Status == "" {
		req.Status = "active"
	}
	if req.Tags == nil {
		req.Tags = make([]string, 0)
	}
	if ep.SSHPort <= 0 {
		ep.SSHPort = 22
	}
	if ep.SSHUser == "" {
		ep.SSHUser = "root"
	}
	if ep.ExecMode == "" {
		ep.ExecMode = "auto"
	}
	if ep.Arch == "" {
		ep.Arch = "amd64"
	}
	if ep.AgentStatus == "" {
		ep.AgentStatus = "OFFLINE"
	}
	if ep.Facts == nil {
		ep.Facts = make(map[string]any)
	}
	if err := s.validateCIData(ctx, wsID, "m_host", req.Data); err != nil {
		return nil, err
	}

	now := nowISO()
	var ciID string
	err := s.store.WithTx(ctx, func(ctx context.Context, tx *db.Tx) error {
		var existingID, beforeJSON string
		qErr := tx.QueryRow(ctx,
			`SELECT id, data_json FROM cmdb_ci_instances WHERE workspace_id = ? AND model_code = 'm_host' AND unique_key = ?`,
			wsID, req.UniqueKey,
		).Scan(&existingID, &beforeJSON)
		if errors.Is(qErr, sql.ErrNoRows) {
			ciID = id.New("ci")
			if _, err := tx.Exec(ctx,
				`INSERT INTO cmdb_ci_instances(id, workspace_id, model_code, unique_key, name, env, status, owner, tags_json, data_json, created_at, updated_at)
				 VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
				ciID, wsID, "m_host", req.UniqueKey, req.Name, req.Env, req.Status, req.Owner, mustJSON(req.Tags), mustJSON(req.Data), now, now,
			); err != nil {
				return err
			}
			_, _ = tx.Exec(ctx,
				`INSERT INTO cmdb_ci_history(id, workspace_id, ci_id, action, actor, before_json, after_json, created_at) VALUES(?,?,?,?,?,?,?,?)`,
				id.New("hist"), wsID, ciID, "create", actor, "{}", mustJSON(req.Data), now,
			)
		} else if qErr != nil {
			return qErr
		} else {
			ciID = existingID
			if _, err := tx.Exec(ctx,
				`UPDATE cmdb_ci_instances SET name=?, env=?, status=?, owner=?, tags_json=?, data_json=?, updated_at=? WHERE id=?`,
				req.Name, req.Env, req.Status, req.Owner, mustJSON(req.Tags), mustJSON(req.Data), now, ciID,
			); err != nil {
				return err
			}
			_, _ = tx.Exec(ctx,
				`INSERT INTO cmdb_ci_history(id, workspace_id, ci_id, action, actor, before_json, after_json, created_at) VALUES(?,?,?,?,?,?,?,?)`,
				id.New("hist"), wsID, ciID, "update", actor, beforeJSON, mustJSON(req.Data), now,
			)
		}

		_, err := tx.Exec(ctx,
			`INSERT INTO cmdb_host_endpoints(ci_id, workspace_id, ip, ssh_port, ssh_user, credential_id, exec_mode, agent_id, agent_status, os_family, os_version, kernel, arch, cpu_cores, mem_mb, disk_gb, load_avg, cpu_usage_pct, mem_usage_pct, facts_json, last_seen_at, updated_at)
			 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
			 ON CONFLICT(ci_id) DO UPDATE SET
			   ip=excluded.ip, ssh_port=excluded.ssh_port, ssh_user=excluded.ssh_user,
			   credential_id=CASE WHEN excluded.credential_id<>'' THEN excluded.credential_id ELSE cmdb_host_endpoints.credential_id END,
			   exec_mode=excluded.exec_mode,
			   agent_id=CASE WHEN excluded.agent_id<>'' THEN excluded.agent_id ELSE cmdb_host_endpoints.agent_id END,
			   agent_status=CASE WHEN excluded.agent_status<>'' THEN excluded.agent_status ELSE cmdb_host_endpoints.agent_status END,
			   os_family=CASE WHEN excluded.os_family<>'' THEN excluded.os_family ELSE cmdb_host_endpoints.os_family END,
			   os_version=CASE WHEN excluded.os_version<>'' THEN excluded.os_version ELSE cmdb_host_endpoints.os_version END,
			   kernel=CASE WHEN excluded.kernel<>'' THEN excluded.kernel ELSE cmdb_host_endpoints.kernel END,
			   arch=CASE WHEN excluded.arch<>'' THEN excluded.arch ELSE cmdb_host_endpoints.arch END,
			   cpu_cores=CASE WHEN excluded.cpu_cores>0 THEN excluded.cpu_cores ELSE cmdb_host_endpoints.cpu_cores END,
			   mem_mb=CASE WHEN excluded.mem_mb>0 THEN excluded.mem_mb ELSE cmdb_host_endpoints.mem_mb END,
			   disk_gb=CASE WHEN excluded.disk_gb>0 THEN excluded.disk_gb ELSE cmdb_host_endpoints.disk_gb END,
			   load_avg=CASE WHEN excluded.load_avg<>'' THEN excluded.load_avg ELSE cmdb_host_endpoints.load_avg END,
			   cpu_usage_pct=CASE WHEN excluded.cpu_usage_pct>0 THEN excluded.cpu_usage_pct ELSE cmdb_host_endpoints.cpu_usage_pct END,
			   mem_usage_pct=CASE WHEN excluded.mem_usage_pct>0 THEN excluded.mem_usage_pct ELSE cmdb_host_endpoints.mem_usage_pct END,
			   facts_json=CASE WHEN excluded.facts_json<>'{}' THEN excluded.facts_json ELSE cmdb_host_endpoints.facts_json END,
			   last_seen_at=CASE WHEN excluded.last_seen_at<>'' THEN excluded.last_seen_at ELSE cmdb_host_endpoints.last_seen_at END,
			   updated_at=excluded.updated_at`,
			ciID, wsID, ep.IP, ep.SSHPort, ep.SSHUser, ep.CredentialID, ep.ExecMode, ep.AgentID, ep.AgentStatus,
			ep.OSFamily, ep.OSVersion, ep.Kernel, ep.Arch, ep.CPUCores, ep.MemMB, ep.DiskGB, ep.LoadAvg,
			ep.CPUUsagePct, ep.MemUsagePct, mustJSON(ep.Facts), ep.LastSeenAt, now,
		)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("保存主机资产失败: %w", err)
	}
	return s.GetCI(ctx, wsID, ciID)
}

func (s *Service) UpdateCI(ctx context.Context, wsID, ciID, actor string, req UpsertCIRequest) (*CIInstance, error) {
	cur, err := s.GetCI(ctx, wsID, ciID)
	if err != nil {
		return nil, errors.New("CI 实例不存在")
	}
	if req.Name == "" {
		req.Name = cur.Name
	}
	if req.Env == "" {
		req.Env = cur.Env
	}
	if req.Status == "" {
		req.Status = cur.Status
	}
	if req.Tags == nil {
		req.Tags = cur.Tags
	}
	if req.Data == nil {
		req.Data = cur.Data
	}
	if err := s.validateCIData(ctx, wsID, cur.ModelCode, req.Data); err != nil {
		return nil, err
	}
	now := nowISO()
	err = s.store.WithTx(ctx, func(ctx context.Context, tx *db.Tx) error {
		if _, err := tx.Exec(ctx,
			`UPDATE cmdb_ci_instances SET name=?, env=?, status=?, owner=?, tags_json=?, data_json=?, updated_at=? WHERE workspace_id=? AND id=?`,
			req.Name, req.Env, req.Status, req.Owner, mustJSON(req.Tags), mustJSON(req.Data), now, wsID, ciID,
		); err != nil {
			return err
		}
		if cur.ModelCode == "m_host" && req.Host != nil {
			ep := req.Host
			if ep.SSHPort <= 0 {
				ep.SSHPort = 22
			}
			if ep.SSHUser == "" {
				ep.SSHUser = "root"
			}
			if ep.ExecMode == "" {
				ep.ExecMode = "auto"
			}
			_, err := tx.Exec(ctx,
				`UPDATE cmdb_host_endpoints SET ip=?, ssh_port=?, ssh_user=?, credential_id=?, exec_mode=?, updated_at=? WHERE ci_id=?`,
				ep.IP, ep.SSHPort, ep.SSHUser, ep.CredentialID, ep.ExecMode, now, ciID,
			)
			if err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx,
			`INSERT INTO cmdb_ci_history(id, workspace_id, ci_id, action, actor, before_json, after_json, created_at) VALUES(?,?,?,?,?,?,?,?)`,
			id.New("hist"), wsID, ciID, "update", actor, mustJSON(cur.Data), mustJSON(req.Data), now,
		)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.GetCI(ctx, wsID, ciID)
}

func (s *Service) UpdateHostTelemetry(ctx context.Context, wsID, ciID string, ep HostEndpoint) error {
	now := nowISO()
	if ep.Facts == nil {
		ep.Facts = make(map[string]any)
	}
	_, err := s.store.Exec(ctx,
		`UPDATE cmdb_host_endpoints SET
		   agent_id=CASE WHEN ?<>'' THEN ? ELSE agent_id END,
		   agent_status=CASE WHEN ?<>'' THEN ? ELSE agent_status END,
		   os_family=CASE WHEN ?<>'' THEN ? ELSE os_family END,
		   os_version=CASE WHEN ?<>'' THEN ? ELSE os_version END,
		   kernel=CASE WHEN ?<>'' THEN ? ELSE kernel END,
		   arch=CASE WHEN ?<>'' THEN ? ELSE arch END,
		   cpu_cores=CASE WHEN ?>0 THEN ? ELSE cpu_cores END,
		   mem_mb=CASE WHEN ?>0 THEN ? ELSE mem_mb END,
		   disk_gb=CASE WHEN ?>0 THEN ? ELSE disk_gb END,
		   load_avg=CASE WHEN ?<>'' THEN ? ELSE load_avg END,
		   cpu_usage_pct=?, mem_usage_pct=?,
		   facts_json=CASE WHEN ?<>'{}' THEN ? ELSE facts_json END,
		   last_seen_at=?, updated_at=?
		 WHERE workspace_id=? AND ci_id=?`,
		ep.AgentID, ep.AgentID,
		ep.AgentStatus, ep.AgentStatus,
		ep.OSFamily, ep.OSFamily,
		ep.OSVersion, ep.OSVersion,
		ep.Kernel, ep.Kernel,
		ep.Arch, ep.Arch,
		ep.CPUCores, ep.CPUCores,
		ep.MemMB, ep.MemMB,
		ep.DiskGB, ep.DiskGB,
		ep.LoadAvg, ep.LoadAvg,
		ep.CPUUsagePct, ep.MemUsagePct,
		mustJSON(ep.Facts), mustJSON(ep.Facts),
		now, now, wsID, ciID,
	)
	return err
}

// SyncDeployedServiceCI auto-registers or updates a deployed database/middleware/app CI and links runs_on -> hostCIID.
func (s *Service) SyncDeployedServiceCI(ctx context.Context, wsID, actor, modelCode, uniqueKey, name, env, hostCIID string, data map[string]any) (*CIInstance, error) {
	if modelCode == "" {
		modelCode = "m_app"
	}
	if data == nil {
		data = make(map[string]any)
	}
	now := nowISO()
	var existingID string
	err := s.store.QueryRow(ctx,
		`SELECT id FROM cmdb_ci_instances WHERE workspace_id = ? AND model_code = ? AND unique_key = ?`,
		wsID, modelCode, uniqueKey,
	).Scan(&existingID)
	var ci *CIInstance
	if errors.Is(err, sql.ErrNoRows) {
		ciID := id.New("ci")
		if _, err := s.store.Exec(ctx,
			`INSERT INTO cmdb_ci_instances(id, workspace_id, model_code, unique_key, name, env, status, owner, tags_json, data_json, created_at, updated_at)
			 VALUES(?,?,?,?,?,?, 'active', ?, '["auto-synced"]', ?, ?, ?)`,
			ciID, wsID, modelCode, uniqueKey, name, env, actor, mustJSON(data), now, now,
		); err != nil {
			return nil, err
		}
		ci, err = s.GetCI(ctx, wsID, ciID)
		if err != nil {
			return nil, err
		}
	} else if err == nil {
		_, _ = s.store.Exec(ctx,
			`UPDATE cmdb_ci_instances SET name=?, env=?, status='active', data_json=?, updated_at=? WHERE id=?`,
			name, env, mustJSON(data), now, existingID,
		)
		ci, err = s.GetCI(ctx, wsID, existingID)
		if err != nil {
			return nil, err
		}
	} else {
		return nil, err
	}
	if hostCIID != "" {
		_, _ = s.CreateCIRelation(ctx, wsID, ci.ID, hostCIID, "runs_on", "由发布编排引擎自动同步关联")
	}
	return ci, nil
}

func (s *Service) DeleteCI(ctx context.Context, wsID, ciID string) error {
	_, err := s.store.Exec(ctx, `DELETE FROM cmdb_ci_instances WHERE workspace_id = ? AND id = ?`, wsID, ciID)
	return err
}

func (s *Service) ListCIHistory(ctx context.Context, wsID, ciID string) ([]CIHistory, error) {
	rows, err := s.store.Query(ctx,
		`SELECT id, ci_id, action, actor, before_json, after_json, created_at
		 FROM cmdb_ci_history WHERE workspace_id = ? AND ci_id = ? ORDER BY created_at DESC LIMIT 50`,
		wsID, ciID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]CIHistory, 0)
	for rows.Next() {
		var (
			h                 CIHistory
			beforeRaw, aftRaw string
		)
		if err := rows.Scan(&h.ID, &h.CIID, &h.Action, &h.Actor, &beforeRaw, &aftRaw, &h.CreatedAt); err != nil {
			return nil, err
		}
		h.BeforeData = make(map[string]any)
		h.AfterData = make(map[string]any)
		_ = json.Unmarshal([]byte(beforeRaw), &h.BeforeData)
		_ = json.Unmarshal([]byte(aftRaw), &h.AfterData)
		out = append(out, h)
	}
	return out, rows.Err()
}

// =============================================================================
// CI Relations & Multi-Hop BFS Lineage Graph
// =============================================================================

func (s *Service) CreateCIRelation(ctx context.Context, wsID, srcID, dstID, relCode, note string) (*CIRelation, error) {
	if srcID == "" || dstID == "" || srcID == dstID {
		return nil, errors.New("源实例与目标实例不能相同或为空")
	}
	if relCode == "" {
		relCode = "depends_on"
	}
	r := &CIRelation{
		ID:           id.New("crel"),
		WorkspaceID:  wsID,
		SourceCIID:   srcID,
		TargetCIID:   dstID,
		RelationCode: relCode,
		Note:         note,
		CreatedAt:    nowISO(),
	}
	if _, err := s.store.Exec(ctx,
		`INSERT INTO cmdb_ci_relations(id, workspace_id, source_ci_id, target_ci_id, relation_code, note, created_at)
		 VALUES(?,?,?,?,?,?,?)
		 ON CONFLICT(workspace_id, source_ci_id, target_ci_id, relation_code) DO UPDATE SET note=excluded.note`,
		r.ID, wsID, r.SourceCIID, r.TargetCIID, r.RelationCode, r.Note, r.CreatedAt,
	); err != nil {
		return nil, err
	}
	return r, nil
}

func (s *Service) DeleteCIRelation(ctx context.Context, wsID, relID string) error {
	_, err := s.store.Exec(ctx, `DELETE FROM cmdb_ci_relations WHERE workspace_id = ? AND id = ?`, wsID, relID)
	return err
}

func (s *Service) GetLineage(ctx context.Context, wsID, rootCIID string, maxDepth int) (*LineageGraph, error) {
	if maxDepth <= 0 || maxDepth > 4 {
		maxDepth = 2
	}
	visited := map[string]bool{rootCIID: true}
	frontier := []string{rootCIID}
	edgeMap := make(map[string]CIRelation)

	for depth := 0; depth < maxDepth && len(frontier) > 0; depth++ {
		nextFrontier := make([]string, 0)
		for _, curID := range frontier {
			edges, err := s.listEdgesForCI(ctx, wsID, curID)
			if err != nil {
				return nil, err
			}
			for _, e := range edges {
				edgeMap[e.ID] = e
				other := e.TargetCIID
				if other == curID {
					other = e.SourceCIID
				}
				if !visited[other] {
					visited[other] = true
					nextFrontier = append(nextFrontier, other)
				}
			}
		}
		frontier = nextFrontier
	}

	nodes := make([]CIInstance, 0, len(visited))
	for ciID := range visited {
		if ci, err := s.GetCI(ctx, wsID, ciID); err == nil && ci != nil {
			nodes = append(nodes, *ci)
		}
	}
	edges := make([]CIRelation, 0, len(edgeMap))
	for _, e := range edgeMap {
		edges = append(edges, e)
	}
	return &LineageGraph{RootCIID: rootCIID, Nodes: nodes, Edges: edges}, nil
}

func (s *Service) listEdgesForCI(ctx context.Context, wsID, ciID string) ([]CIRelation, error) {
	rows, err := s.store.Query(ctx,
		`SELECT id, workspace_id, source_ci_id, target_ci_id, relation_code, note, created_at
		 FROM cmdb_ci_relations WHERE workspace_id = ? AND (source_ci_id = ? OR target_ci_id = ?)`,
		wsID, ciID, ciID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]CIRelation, 0)
	for rows.Next() {
		var e CIRelation
		if err := rows.Scan(&e.ID, &e.WorkspaceID, &e.SourceCIID, &e.TargetCIID, &e.RelationCode, &e.Note, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// =============================================================================
// Dual Topology Trees & Service Templates
// =============================================================================

func (s *Service) GetTopologyTree(ctx context.Context, wsID, treeType string) ([]*TopologyNode, error) {
	if treeType != "resource" {
		treeType = "business"
	}
	rows, err := s.store.Query(ctx,
		`SELECT n.id, n.workspace_id, n.tree_type, n.parent_id, n.node_type, n.name, n.code, n.service_template_id, n.labels_json, n.sort_order,
		        (SELECT COUNT(1) FROM cmdb_ci_topo_bindings b WHERE b.topo_node_id = n.id) AS cnt
		 FROM cmdb_topology_nodes n WHERE n.workspace_id = ? AND n.tree_type = ? ORDER BY n.sort_order ASC, n.name ASC`,
		wsID, treeType,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	byID := make(map[string]*TopologyNode)
	ordered := make([]*TopologyNode, 0)
	for rows.Next() {
		n := &TopologyNode{Labels: make(map[string]string), Children: make([]*TopologyNode, 0)}
		var labelsRaw string
		if err := rows.Scan(&n.ID, &n.WorkspaceID, &n.TreeType, &n.ParentID, &n.NodeType, &n.Name, &n.Code, &n.ServiceTemplateID, &labelsRaw, &n.SortOrder, &n.CICount); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(labelsRaw), &n.Labels)
		byID[n.ID] = n
		ordered = append(ordered, n)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	roots := make([]*TopologyNode, 0)
	for _, n := range ordered {
		if n.ParentID == "" {
			roots = append(roots, n)
		} else if parent, ok := byID[n.ParentID]; ok {
			parent.Children = append(parent.Children, n)
		} else {
			roots = append(roots, n)
		}
	}
	return roots, nil
}

func (s *Service) CreateTopologyNode(ctx context.Context, wsID, treeType, parentID, nodeType, name, code, tplID string) (*TopologyNode, error) {
	if name == "" {
		return nil, errors.New("节点名称不能为空")
	}
	if treeType != "resource" {
		treeType = "business"
	}
	if code == "" {
		code = id.Raw()
	}
	now := nowISO()
	n := &TopologyNode{
		ID:                id.New("topo"),
		WorkspaceID:       wsID,
		TreeType:          treeType,
		ParentID:          parentID,
		NodeType:          nodeType,
		Name:              name,
		Code:              code,
		ServiceTemplateID: tplID,
		Labels:            make(map[string]string),
		Children:          make([]*TopologyNode, 0),
	}
	_, err := s.store.Exec(ctx,
		`INSERT INTO cmdb_topology_nodes(id, workspace_id, tree_type, parent_id, node_type, name, code, service_template_id, labels_json, sort_order, created_at, updated_at)
		 VALUES(?,?,?,?,?,?,?,?,?,100,?,?)`,
		n.ID, wsID, n.TreeType, n.ParentID, n.NodeType, n.Name, n.Code, n.ServiceTemplateID, "{}", now, now,
	)
	if err != nil {
		return nil, err
	}
	return n, nil
}

func (s *Service) DeleteTopologyNode(ctx context.Context, wsID, nodeID string) error {
	var childCnt int
	_ = s.store.QueryRow(ctx, `SELECT COUNT(1) FROM cmdb_topology_nodes WHERE workspace_id = ? AND parent_id = ?`, wsID, nodeID).Scan(&childCnt)
	if childCnt > 0 {
		return errors.New("该拓扑节点仍存在子节点，请先删除子节点")
	}
	_, err := s.store.Exec(ctx, `DELETE FROM cmdb_topology_nodes WHERE workspace_id = ? AND id = ?`, wsID, nodeID)
	return err
}

func (s *Service) BindCIToTopoNode(ctx context.Context, wsID, ciID, topoNodeID string) error {
	_, err := s.store.Exec(ctx,
		`INSERT INTO cmdb_ci_topo_bindings(id, workspace_id, ci_id, topo_node_id, created_at) VALUES(?,?,?,?,?)
		 ON CONFLICT(workspace_id, ci_id, topo_node_id) DO NOTHING`,
		id.New("bind"), wsID, ciID, topoNodeID, nowISO(),
	)
	return err
}

func (s *Service) UnbindCIFromTopoNode(ctx context.Context, wsID, ciID, topoNodeID string) error {
	_, err := s.store.Exec(ctx, `DELETE FROM cmdb_ci_topo_bindings WHERE workspace_id = ? AND ci_id = ? AND topo_node_id = ?`, wsID, ciID, topoNodeID)
	return err
}

func (s *Service) ListTopoNodeCIs(ctx context.Context, wsID, topoNodeID string) ([]CIInstance, error) {
	rows, err := s.store.Query(ctx,
		`SELECT c.id, c.workspace_id, c.model_code, c.unique_key, c.name, c.env, c.status, c.owner, c.tags_json, c.data_json, c.created_at, c.updated_at,
		        COALESCE(h.ip,''), COALESCE(h.ssh_port,22), COALESCE(h.ssh_user,'root'), COALESCE(h.credential_id,''), COALESCE(h.exec_mode,'auto'),
		        COALESCE(h.agent_id,''), COALESCE(h.agent_status,''), COALESCE(h.os_family,''), COALESCE(h.os_version,''), COALESCE(h.kernel,''),
		        COALESCE(h.arch,'amd64'), COALESCE(h.cpu_cores,0), COALESCE(h.mem_mb,0), COALESCE(h.disk_gb,0), COALESCE(h.load_avg,''),
		        COALESCE(h.cpu_usage_pct,0), COALESCE(h.mem_usage_pct,0), COALESCE(h.facts_json,'{}'), COALESCE(h.last_seen_at,''), COALESCE(h.updated_at,'')
		 FROM cmdb_ci_topo_bindings b
		 JOIN cmdb_ci_instances c ON c.id = b.ci_id
		 LEFT JOIN cmdb_host_endpoints h ON h.ci_id = c.id
		 WHERE b.workspace_id = ? AND b.topo_node_id = ?
		 ORDER BY c.name ASC`,
		wsID, topoNodeID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]CIInstance, 0)
	for rows.Next() {
		ci, err := scanCIRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ci)
	}
	return out, rows.Err()
}

func (s *Service) ListServiceTemplates(ctx context.Context, wsID string) ([]ServiceTemplate, error) {
	rows, err := s.store.Query(ctx,
		`SELECT id, workspace_id, code, name, category, ports_json, processes_json, env_defaults_json, description, created_at, updated_at
		 FROM cmdb_service_templates WHERE workspace_id = ? ORDER BY name ASC`,
		wsID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]ServiceTemplate, 0)
	for rows.Next() {
		var (
			t                      ServiceTemplate
			portsR, procsR, envDef string
		)
		if err := rows.Scan(&t.ID, &t.WorkspaceID, &t.Code, &t.Name, &t.Category, &portsR, &procsR, &envDef, &t.Description, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		t.Ports = make([]int, 0)
		t.Processes = make([]string, 0)
		t.EnvDefaults = make(map[string]string)
		_ = json.Unmarshal([]byte(portsR), &t.Ports)
		_ = json.Unmarshal([]byte(procsR), &t.Processes)
		_ = json.Unmarshal([]byte(envDef), &t.EnvDefaults)
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Service) CreateServiceTemplate(ctx context.Context, wsID string, t ServiceTemplate) (*ServiceTemplate, error) {
	if strings.TrimSpace(t.Code) == "" || strings.TrimSpace(t.Name) == "" {
		return nil, errors.New("服务模板编码与名称不能为空")
	}
	if t.Ports == nil {
		t.Ports = make([]int, 0)
	}
	if t.Processes == nil {
		t.Processes = make([]string, 0)
	}
	if t.EnvDefaults == nil {
		t.EnvDefaults = make(map[string]string)
	}
	now := nowISO()
	t.ID = id.New("stpl")
	t.WorkspaceID = wsID
	t.CreatedAt = now
	t.UpdatedAt = now
	_, err := s.store.Exec(ctx,
		`INSERT INTO cmdb_service_templates(id, workspace_id, code, name, category, ports_json, processes_json, env_defaults_json, description, created_at, updated_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		t.ID, wsID, t.Code, t.Name, t.Category, mustJSON(t.Ports), mustJSON(t.Processes), mustJSON(t.EnvDefaults), t.Description, now, now,
	)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// =============================================================================
// Dynamic Groups (Rule-Based Host/CI Targeting)
// =============================================================================

func (s *Service) ListDynamicGroups(ctx context.Context, wsID string) ([]DynamicGroup, error) {
	rows, err := s.store.Query(ctx,
		`SELECT id, workspace_id, name, model_code, conditions_json, description, created_at, updated_at
		 FROM cmdb_dynamic_groups WHERE workspace_id = ? ORDER BY name ASC`,
		wsID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]DynamicGroup, 0)
	for rows.Next() {
		var (
			g       DynamicGroup
			condRaw string
		)
		if err := rows.Scan(&g.ID, &g.WorkspaceID, &g.Name, &g.ModelCode, &condRaw, &g.Description, &g.CreatedAt, &g.UpdatedAt); err != nil {
			return nil, err
		}
		g.Conditions = make([]DynamicCondition, 0)
		_ = json.Unmarshal([]byte(condRaw), &g.Conditions)
		out = append(out, g)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		matched, _ := s.EvaluateDynamicGroup(ctx, wsID, out[i].ID)
		out[i].MatchCount = len(matched)
	}
	return out, nil
}

func (s *Service) CreateDynamicGroup(ctx context.Context, wsID, name, modelCode string, conds []DynamicCondition, desc string) (*DynamicGroup, error) {
	if strings.TrimSpace(name) == "" {
		return nil, errors.New("动态分组名称不能为空")
	}
	if modelCode == "" {
		modelCode = "m_host"
	}
	if conds == nil {
		conds = make([]DynamicCondition, 0)
	}
	now := nowISO()
	g := &DynamicGroup{
		ID:          id.New("dgrp"),
		WorkspaceID: wsID,
		Name:        strings.TrimSpace(name),
		ModelCode:   modelCode,
		Conditions:  conds,
		Description: desc,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	_, err := s.store.Exec(ctx,
		`INSERT INTO cmdb_dynamic_groups(id, workspace_id, name, model_code, conditions_json, description, created_at, updated_at)
		 VALUES(?,?,?,?,?,?,?,?)`,
		g.ID, wsID, g.Name, g.ModelCode, mustJSON(g.Conditions), g.Description, now, now,
	)
	if err != nil {
		return nil, err
	}
	return g, nil
}

func (s *Service) DeleteDynamicGroup(ctx context.Context, wsID, groupID string) error {
	_, err := s.store.Exec(ctx, `DELETE FROM cmdb_dynamic_groups WHERE workspace_id = ? AND id = ?`, wsID, groupID)
	return err
}

func (s *Service) EvaluateDynamicGroup(ctx context.Context, wsID, groupID string) ([]CIInstance, error) {
	var (
		modelCode, condRaw string
	)
	if err := s.store.QueryRow(ctx, `SELECT model_code, conditions_json FROM cmdb_dynamic_groups WHERE workspace_id = ? AND id = ?`, wsID, groupID).Scan(&modelCode, &condRaw); err != nil {
		return nil, err
	}
	var conds []DynamicCondition
	_ = json.Unmarshal([]byte(condRaw), &conds)
	all, err := s.ListCIs(ctx, wsID, modelCode, "", "")
	if err != nil {
		return nil, err
	}
	matched := make([]CIInstance, 0)
	for _, ci := range all {
		if matchesAllConditions(ci, conds) {
			matched = append(matched, ci)
		}
	}
	return matched, nil
}

func matchesAllConditions(ci CIInstance, conds []DynamicCondition) bool {
	for _, c := range conds {
		actual := extractFieldValue(ci, c.Field)
		switch strings.ToLower(c.Op) {
		case "eq", "=":
			if !strings.EqualFold(actual, c.Value) {
				return false
			}
		case "neq", "!=":
			if strings.EqualFold(actual, c.Value) {
				return false
			}
		case "contains":
			if !strings.Contains(strings.ToLower(actual), strings.ToLower(c.Value)) {
				return false
			}
		case "in":
			parts := strings.Split(c.Value, ",")
			ok := false
			for _, p := range parts {
				if strings.EqualFold(actual, strings.TrimSpace(p)) {
					ok = true
					break
				}
			}
			if !ok {
				return false
			}
		case "gte", ">=":
			av, e1 := strconv.ParseFloat(actual, 64)
			tv, e2 := strconv.ParseFloat(c.Value, 64)
			if e1 != nil || e2 != nil || av < tv {
				return false
			}
		case "lte", "<=":
			av, e1 := strconv.ParseFloat(actual, 64)
			tv, e2 := strconv.ParseFloat(c.Value, 64)
			if e1 != nil || e2 != nil || av > tv {
				return false
			}
		}
	}
	return true
}

func extractFieldValue(ci CIInstance, field string) string {
	switch field {
	case "name":
		return ci.Name
	case "env":
		return ci.Env
	case "status":
		return ci.Status
	case "owner":
		return ci.Owner
	case "uniqueKey", "unique_key":
		return ci.UniqueKey
	case "tags":
		return strings.Join(ci.Tags, ",")
	case "agent_status":
		if ci.Host != nil {
			return ci.Host.AgentStatus
		}
	case "ip":
		if ci.Host != nil && ci.Host.IP != "" {
			return ci.Host.IP
		}
	}
	if v, ok := ci.Data[field]; ok && v != nil {
		return fmt.Sprint(v)
	}
	return ""
}

// =============================================================================
// Platform Panorama Stats (for Dashboard)
// =============================================================================

func (s *Service) GetOverviewStats(ctx context.Context, wsID string) (*OverviewStats, error) {
	st := &OverviewStats{
		ModelBreakdown: make(map[string]int),
		EnvBreakdown:   make(map[string]int),
	}
	_ = s.store.QueryRow(ctx, `SELECT COUNT(1) FROM cmdb_models WHERE workspace_id = ?`, wsID).Scan(&st.TotalModels)
	_ = s.store.QueryRow(ctx, `SELECT COUNT(1) FROM cmdb_ci_instances WHERE workspace_id = ?`, wsID).Scan(&st.TotalCIs)
	_ = s.store.QueryRow(ctx, `SELECT COUNT(1) FROM cmdb_ci_instances WHERE workspace_id = ? AND model_code = 'm_host'`, wsID).Scan(&st.TotalHosts)
	_ = s.store.QueryRow(ctx, `SELECT COUNT(1) FROM exec_agents WHERE workspace_id = ? AND status = 'ONLINE'`, wsID).Scan(&st.OnlineAgents)
	_ = s.store.QueryRow(ctx, `SELECT COUNT(1) FROM dcim_racks WHERE workspace_id = ?`, wsID).Scan(&st.TotalRacks)
	_ = s.store.QueryRow(ctx, `SELECT COUNT(1) FROM ipam_prefixes WHERE workspace_id = ?`, wsID).Scan(&st.TotalPrefixes)
	_ = s.store.QueryRow(ctx, `SELECT COUNT(1) FROM ipam_addresses WHERE workspace_id = ?`, wsID).Scan(&st.AllocatedIPs)
	_ = s.store.QueryRow(ctx, `SELECT COUNT(1) FROM cfg_namespaces WHERE workspace_id = ?`, wsID).Scan(&st.TotalConfigs)
	_ = s.store.QueryRow(ctx, `SELECT COUNT(1) FROM delivery_plugins WHERE workspace_id = ?`, wsID).Scan(&st.TotalPlugins)
	_ = s.store.QueryRow(ctx, `SELECT COUNT(1) FROM delivery_releases WHERE workspace_id = ?`, wsID).Scan(&st.TotalReleases)
	_ = s.store.QueryRow(ctx, `SELECT COUNT(1) FROM artifact_records WHERE workspace_id = ?`, wsID).Scan(&st.TotalArtifacts)
	_ = s.store.QueryRow(ctx, `SELECT COUNT(1) FROM audit_logs WHERE workspace_id = ?`, wsID).Scan(&st.RecentAuditLogs)

	if rows, err := s.store.Query(ctx, `SELECT model_code, COUNT(1) FROM cmdb_ci_instances WHERE workspace_id = ? GROUP BY model_code`, wsID); err == nil {
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var k string
			var v int
			if rows.Scan(&k, &v) == nil {
				st.ModelBreakdown[k] = v
			}
		}
	}
	if rows, err := s.store.Query(ctx, `SELECT env, COUNT(1) FROM cmdb_ci_instances WHERE workspace_id = ? GROUP BY env`, wsID); err == nil {
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var k string
			var v int
			if rows.Scan(&k, &v) == nil {
				st.EnvBreakdown[k] = v
			}
		}
	}
	return st, nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

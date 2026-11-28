package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/pkg/platform/db"
	"github.com/kinstall/kinstall/pkg/platform/id"
)

func (r *Repo) ListAssetModels(ctx context.Context, scope domain.Scope) ([]domain.AssetModel, error) {
	rows, err := r.db.Query(ctx,
		`SELECT m.tenant_id, m.app_id, m.model_id, m.group_id, m.code, m.name, m.icon, m.is_builtin, m.description, m.created_at, m.updated_at,
		        (SELECT COUNT(*) FROM assets_instances i WHERE i.tenant_id=m.tenant_id AND i.app_id=m.app_id AND i.model_id=m.model_id) AS inst_count
		 FROM assets_models m
		 WHERE m.tenant_id=? AND m.app_id=?
		 ORDER BY m.created_at ASC`,
		scope.TenantID, scope.AppID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.AssetModel
	for rows.Next() {
		var m domain.AssetModel
		var builtin int
		var ca, ua string
		if err := rows.Scan(&m.TenantID, &m.AppID, &m.ModelID, &m.GroupID, &m.Code, &m.Name, &m.Icon, &builtin, &m.Description, &ca, &ua, &m.InstanceCount); err != nil {
			return nil, err
		}
		m.IsBuiltin = builtin != 0
		m.CreatedAt, m.UpdatedAt = parseTime(ca), parseTime(ua)
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		fields, err := r.ListModelFields(ctx, scope, out[i].ModelID)
		if err != nil {
			return nil, err
		}
		out[i].Fields = fields
	}
	return out, nil
}

func (r *Repo) ListModelFields(ctx context.Context, scope domain.Scope, modelID string) ([]domain.ModelField, error) {
	rows, err := r.db.Query(ctx,
		`SELECT tenant_id, app_id, model_id, field_key, name, field_type, required, is_unique, enum_values_json, default_value, sort_order
		 FROM assets_model_fields WHERE tenant_id=? AND app_id=? AND model_id=? ORDER BY sort_order ASC, field_key ASC`,
		scope.TenantID, scope.AppID, modelID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.ModelField
	for rows.Next() {
		var f domain.ModelField
		var req, uniq int
		var enumRaw string
		if err := rows.Scan(&f.TenantID, &f.AppID, &f.ModelID, &f.FieldKey, &f.Name, &f.FieldType, &req, &uniq, &enumRaw, &f.DefaultValue, &f.SortOrder); err != nil {
			return nil, err
		}
		f.Required = req != 0
		f.IsUnique = uniq != 0
		unmarshalJSON(enumRaw, &f.EnumValues)
		out = append(out, f)
	}
	return out, rows.Err()
}

func (r *Repo) CreateAssetModel(ctx context.Context, scope domain.Scope, groupID, code, name, icon, desc string, fields []domain.ModelField) (domain.AssetModel, error) {
	now := r.Now()
	nowStr := formatTime(now)
	if groupID == "" {
		groupID = "grp-res"
	}
	if icon == "" {
		icon = "box"
	}
	mid := id.New("mdl")
	m := domain.AssetModel{
		TenantID: scope.TenantID, AppID: scope.AppID, ModelID: mid, GroupID: groupID,
		Code: code, Name: name, Icon: icon, Description: desc, Fields: fields,
		CreatedAt: now, UpdatedAt: now,
	}
	err := r.db.WithScopeTx(ctx, dbScope(scope), func(ctx context.Context, tx *db.Tx) error {
		if _, err := tx.Exec(ctx,
			`INSERT INTO assets_models (tenant_id, app_id, model_id, group_id, code, name, icon, is_builtin, description, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?)`,
			scope.TenantID, scope.AppID, mid, groupID, code, name, icon, desc, nowStr, nowStr,
		); err != nil {
			if isUniqueViolation(err) {
				return domain.NewError(domain.ErrAlreadyExists, "MODEL_EXISTS", "模型编码已存在", nil)
			}
			return err
		}
		for i, f := range fields {
			ft := f.FieldType
			if ft == "" {
				ft = "string"
			}
			if _, err := tx.Exec(ctx,
				`INSERT INTO assets_model_fields (tenant_id, app_id, model_id, field_key, name, field_type, required, is_unique, enum_values_json, default_value, sort_order)
				 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				scope.TenantID, scope.AppID, mid, f.FieldKey, f.Name, ft, boolInt(f.Required), boolInt(f.IsUnique), marshalJSON(f.EnumValues), f.DefaultValue, i+1,
			); err != nil {
				return err
			}
		}
		return nil
	})
	return m, err
}

func (r *Repo) ListRelationTypes(ctx context.Context, scope domain.Scope) ([]domain.RelationType, error) {
	rows, err := r.db.Query(ctx,
		`SELECT tenant_id, app_id, rel_type_id, code, name, source_model_id, target_model_id, cardinality, created_at
		 FROM assets_relation_types WHERE tenant_id=? AND app_id=? ORDER BY created_at ASC`,
		scope.TenantID, scope.AppID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.RelationType
	for rows.Next() {
		var rt domain.RelationType
		var ca string
		if err := rows.Scan(&rt.TenantID, &rt.AppID, &rt.RelTypeID, &rt.Code, &rt.Name, &rt.SourceModelID, &rt.TargetModelID, &rt.Cardinality, &ca); err != nil {
			return nil, err
		}
		rt.CreatedAt = parseTime(ca)
		out = append(out, rt)
	}
	return out, rows.Err()
}

func (r *Repo) DeleteRelationType(ctx context.Context, scope domain.Scope, relTypeID string) error {
	return r.db.WithScopeTx(ctx, dbScope(scope), func(ctx context.Context, tx *db.Tx) error {
		var used int
		if err := tx.QueryRow(ctx,
			`SELECT COUNT(*) FROM assets_edges WHERE tenant_id=? AND app_id=? AND rel_type_id=?`,
			scope.TenantID, scope.AppID, relTypeID,
		).Scan(&used); err != nil {
			return err
		}
		if used > 0 {
			return domain.NewError(domain.ErrReferenceInUse, "RELATION_TYPE_IN_USE", "该关系类型仍被拓扑边引用，禁止删除", nil)
		}
		res, err := tx.Exec(ctx,
			`DELETE FROM assets_relation_types WHERE tenant_id=? AND app_id=? AND rel_type_id=?`,
			scope.TenantID, scope.AppID, relTypeID,
		)
		if err != nil {
			if isFKViolation(err) {
				return domain.NewError(domain.ErrReferenceInUse, "RELATION_TYPE_IN_USE", "该关系类型仍被引用", nil)
			}
			return err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return domain.NotFoundError("relation type not found")
		}
		return nil
	})
}

func (r *Repo) ListAssetInstances(ctx context.Context, scope domain.Scope, modelID, keyword string) ([]domain.AssetInstance, error) {
	q := `SELECT i.tenant_id, i.app_id, i.instance_id, i.model_id, m.code, m.name, i.name, i.unique_key, i.status, i.attrs_json, i.version, i.created_by, i.updated_by, i.created_at, i.updated_at
	      FROM assets_instances i
	      JOIN assets_models m ON i.tenant_id=m.tenant_id AND i.app_id=m.app_id AND i.model_id=m.model_id
	      WHERE i.tenant_id=? AND i.app_id=?`
	args := []any{scope.TenantID, scope.AppID}
	if modelID != "" {
		q += ` AND i.model_id=?`
		args = append(args, modelID)
	}
	if kw := strings.TrimSpace(keyword); kw != "" {
		q += ` AND (LOWER(i.name) LIKE ? OR LOWER(i.unique_key) LIKE ? OR LOWER(i.attrs_json) LIKE ?)`
		like := "%" + strings.ToLower(kw) + "%"
		args = append(args, like, like, like)
	}
	q += ` ORDER BY i.updated_at DESC`
	rows, err := r.db.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.AssetInstance
	for rows.Next() {
		var inst domain.AssetInstance
		var attrsRaw, ca, ua string
		if err := rows.Scan(&inst.TenantID, &inst.AppID, &inst.InstanceID, &inst.ModelID, &inst.ModelCode, &inst.ModelName, &inst.Name, &inst.UniqueKey, &inst.Status, &attrsRaw, &inst.Version, &inst.CreatedBy, &inst.UpdatedBy, &ca, &ua); err != nil {
			return nil, err
		}
		inst.Attrs = make(map[string]any)
		unmarshalJSON(attrsRaw, &inst.Attrs)
		inst.CreatedAt, inst.UpdatedAt = parseTime(ca), parseTime(ua)
		out = append(out, inst)
	}
	return out, rows.Err()
}

func (r *Repo) GetAssetInstance(ctx context.Context, scope domain.Scope, instanceID string) (domain.AssetInstance, error) {
	var inst domain.AssetInstance
	var attrsRaw, ca, ua string
	err := r.db.QueryRow(ctx,
		`SELECT i.tenant_id, i.app_id, i.instance_id, i.model_id, m.code, m.name, i.name, i.unique_key, i.status, i.attrs_json, i.version, i.created_by, i.updated_by, i.created_at, i.updated_at
		 FROM assets_instances i
		 JOIN assets_models m ON i.tenant_id=m.tenant_id AND i.app_id=m.app_id AND i.model_id=m.model_id
		 WHERE i.tenant_id=? AND i.app_id=? AND i.instance_id=?`,
		scope.TenantID, scope.AppID, instanceID,
	).Scan(&inst.TenantID, &inst.AppID, &inst.InstanceID, &inst.ModelID, &inst.ModelCode, &inst.ModelName, &inst.Name, &inst.UniqueKey, &inst.Status, &attrsRaw, &inst.Version, &inst.CreatedBy, &inst.UpdatedBy, &ca, &ua)
	if err != nil {
		return domain.AssetInstance{}, mapNotFound(err, "asset instance not found")
	}
	inst.Attrs = make(map[string]any)
	unmarshalJSON(attrsRaw, &inst.Attrs)
	inst.CreatedAt, inst.UpdatedAt = parseTime(ca), parseTime(ua)
	return inst, nil
}

// CreateAssetInstance persists a new CMDB instance and enforces unique field indexes atomically (LiveOps S01/S04).
func (r *Repo) CreateAssetInstance(ctx context.Context, scope domain.Scope, modelID, name, uniqueKey, status, actorID string, attrs map[string]any, uniques map[string]string) (domain.AssetInstance, error) {
	now := r.Now()
	nowStr := formatTime(now)
	if status == "" {
		status = "ACTIVE"
	}
	iid := id.New("inst")
	inst := domain.AssetInstance{
		TenantID: scope.TenantID, AppID: scope.AppID, InstanceID: iid, ModelID: modelID,
		Name: name, UniqueKey: uniqueKey, Status: status, Attrs: attrs, Version: 1,
		CreatedBy: actorID, UpdatedBy: actorID, CreatedAt: now, UpdatedAt: now,
	}
	err := r.db.WithScopeTx(ctx, dbScope(scope), func(ctx context.Context, tx *db.Tx) error {
		if _, err := tx.Exec(ctx,
			`INSERT INTO assets_instances (tenant_id, app_id, instance_id, model_id, name, unique_key, status, attrs_json, version, created_by, updated_by, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?, ?)`,
			scope.TenantID, scope.AppID, iid, modelID, name, uniqueKey, status, marshalJSON(attrs), actorID, actorID, nowStr, nowStr,
		); err != nil {
			if isUniqueViolation(err) {
				return domain.NewError(domain.ErrAlreadyExists, "INSTANCE_DUPLICATE", "同模型下唯一标识已存在", nil)
			}
			if isFKViolation(err) {
				return domain.NotFoundError("model not found in current scope")
			}
			return err
		}
		for fk, fv := range uniques {
			if _, err := tx.Exec(ctx,
				`INSERT INTO assets_field_uniques (tenant_id, app_id, model_id, field_key, field_value, instance_id) VALUES (?, ?, ?, ?, ?, ?)`,
				scope.TenantID, scope.AppID, modelID, fk, fv, iid,
			); err != nil {
				if isUniqueViolation(err) {
					return domain.NewError(domain.ErrAlreadyExists, "UNIQUE_FIELD_CONFLICT", "唯一约束字段冲突: "+fk+"="+fv, nil)
				}
				return err
			}
		}
		return nil
	})
	return inst, err
}

// UpdateAssetInstance enforces If-Match optimistic version locking and updates assets_field_uniques atomically.
func (r *Repo) UpdateAssetInstance(ctx context.Context, scope domain.Scope, instanceID string, expectedVer int64, name, status, actorID string, attrs map[string]any, uniques map[string]string) (domain.AssetInstance, error) {
	nowStr := r.NowStr()
	var modelID string
	err := r.db.WithScopeTx(ctx, dbScope(scope), func(ctx context.Context, tx *db.Tx) error {
		var curVer int64
		if err := tx.QueryRow(ctx,
			`SELECT model_id, version FROM assets_instances WHERE tenant_id=? AND app_id=? AND instance_id=?`,
			scope.TenantID, scope.AppID, instanceID,
		).Scan(&modelID, &curVer); err != nil {
			return mapNotFound(err, "asset instance not found")
		}
		if curVer != expectedVer {
			return domain.NewError(domain.ErrVersionMismatch, "VERSION_MISMATCH", "实例已被其他操作修改，请刷新后重试", map[string]any{"currentVersion": curVer})
		}
		res, err := tx.Exec(ctx,
			`UPDATE assets_instances SET name=?, status=?, attrs_json=?, version=version+1, updated_by=?, updated_at=?
			 WHERE tenant_id=? AND app_id=? AND instance_id=? AND version=?`,
			name, status, marshalJSON(attrs), actorID, nowStr, scope.TenantID, scope.AppID, instanceID, expectedVer,
		)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return domain.NewError(domain.ErrVersionMismatch, "VERSION_MISMATCH", "并发版本冲突", nil)
		}
		if _, err := tx.Exec(ctx,
			`DELETE FROM assets_field_uniques WHERE tenant_id=? AND app_id=? AND instance_id=?`,
			scope.TenantID, scope.AppID, instanceID,
		); err != nil {
			return err
		}
		for fk, fv := range uniques {
			if _, err := tx.Exec(ctx,
				`INSERT INTO assets_field_uniques (tenant_id, app_id, model_id, field_key, field_value, instance_id) VALUES (?, ?, ?, ?, ?, ?)`,
				scope.TenantID, scope.AppID, modelID, fk, fv, instanceID,
			); err != nil {
				if isUniqueViolation(err) {
					return domain.NewError(domain.ErrAlreadyExists, "UNIQUE_FIELD_CONFLICT", "唯一约束字段冲突: "+fk+"="+fv, nil)
				}
				return err
			}
		}
		return nil
	})
	if err != nil {
		return domain.AssetInstance{}, err
	}
	return r.GetAssetInstance(ctx, scope, instanceID)
}

// CreateAssetEdge enforces composite tenant isolation (S02), single-parent tree constraint, and cycle-freedom (S03).
func (r *Repo) CreateAssetEdge(ctx context.Context, scope domain.Scope, treeKind domain.TreeKind, relTypeID, sourceID, targetID string, metadata map[string]any) (domain.AssetEdge, error) {
	if !treeKind.Valid() {
		return domain.AssetEdge{}, domain.ValidationError("invalid tree_kind")
	}
	if sourceID == targetID {
		return domain.AssetEdge{}, domain.ValidationError("self-loop topology edge is prohibited")
	}
	if relTypeID == "" {
		relTypeID = "rel-contains"
	}
	now := r.Now()
	nowStr := formatTime(now)
	eid := id.New("edg")
	edge := domain.AssetEdge{
		TenantID: scope.TenantID, AppID: scope.AppID, EdgeID: eid, TreeKind: treeKind,
		RelTypeID: relTypeID, SourceID: sourceID, TargetID: targetID, Metadata: metadata, CreatedAt: now,
	}
	err := r.db.WithScopeTx(ctx, dbScope(scope), func(ctx context.Context, tx *db.Tx) error {
		// Verify both endpoints belong to the SAME (tenant_id, app_id) scope (LiveOps S02)
		var cnt int
		if err := tx.QueryRow(ctx,
			`SELECT COUNT(*) FROM assets_instances WHERE tenant_id=? AND app_id=? AND instance_id IN (?, ?)`,
			scope.TenantID, scope.AppID, sourceID, targetID,
		).Scan(&cnt); err != nil {
			return err
		}
		if cnt != 2 {
			return domain.NewError(domain.ErrForbidden, "CROSS_SCOPE_EDGE_DENIED", "禁止跨租户或引用不存在的资产建立拓扑边", nil)
		}

		// For single-parent trees (business / resource), check that source is NOT a descendant of target (cycle check S03).
		if treeKind.IsSingleParent() {
			if err := checkTreeAcyclicTx(ctx, tx, scope, treeKind, sourceID, targetID); err != nil {
				return err
			}
		}

		if _, err := tx.Exec(ctx,
			`INSERT INTO assets_edges (tenant_id, app_id, edge_id, tree_kind, rel_type_id, source_id, target_id, metadata_json, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			scope.TenantID, scope.AppID, eid, string(treeKind), relTypeID, sourceID, targetID, marshalJSON(metadata), nowStr,
		); err != nil {
			if isUniqueViolation(err) {
				if treeKind.IsSingleParent() {
					return domain.NewError(domain.ErrStateConflict, "SINGLE_PARENT_VIOLATION", "业务/资源拓扑树严格遵守单父约束，目标节点已有父节点", nil)
				}
				return domain.NewError(domain.ErrAlreadyExists, "EDGE_EXISTS", "拓扑边已存在", nil)
			}
			return err
		}
		return nil
	})
	return edge, err
}

func checkTreeAcyclicTx(ctx context.Context, tx *db.Tx, scope domain.Scope, treeKind domain.TreeKind, newParent, child string) error {
	cur := newParent
	visited := map[string]bool{child: true}
	for range 256 {
		if visited[cur] {
			return domain.NewError(domain.ErrStateConflict, "TOPOLOGY_CYCLE_DETECTED", "检测到树拓扑成环，操作已拒绝", nil)
		}
		visited[cur] = true
		var parent string
		err := tx.QueryRow(ctx,
			`SELECT source_id FROM assets_edges WHERE tenant_id=? AND app_id=? AND tree_kind=? AND target_id=? LIMIT 1`,
			scope.TenantID, scope.AppID, string(treeKind), cur,
		).Scan(&parent)
		if errors.Is(err, sql.ErrNoRows) {
			break // reached root
		}
		if err != nil {
			return err
		}
		cur = parent
	}
	return nil
}

func (r *Repo) DeleteAssetEdge(ctx context.Context, scope domain.Scope, edgeID string) error {
	res, err := r.db.Exec(ctx,
		`DELETE FROM assets_edges WHERE tenant_id=? AND app_id=? AND edge_id=?`,
		scope.TenantID, scope.AppID, edgeID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return domain.NotFoundError("edge not found")
	}
	return nil
}

func (r *Repo) GetTopologyGraph(ctx context.Context, scope domain.Scope, treeKind domain.TreeKind) (domain.TopologyGraph, error) {
	instances, err := r.ListAssetInstances(ctx, scope, "", "")
	if err != nil {
		return domain.TopologyGraph{}, err
	}
	q := `SELECT tenant_id, app_id, edge_id, tree_kind, rel_type_id, source_id, target_id, metadata_json, created_at
	      FROM assets_edges WHERE tenant_id=? AND app_id=?`
	args := []any{scope.TenantID, scope.AppID}
	if treeKind != "" {
		q += ` AND tree_kind=?`
		args = append(args, string(treeKind))
	}
	q += ` ORDER BY created_at ASC`
	rows, err := r.db.Query(ctx, q, args...)
	if err != nil {
		return domain.TopologyGraph{}, err
	}
	defer func() { _ = rows.Close() }()
	var edges []domain.AssetEdge
	for rows.Next() {
		var e domain.AssetEdge
		var tk, metaRaw, ca string
		if err := rows.Scan(&e.TenantID, &e.AppID, &e.EdgeID, &tk, &e.RelTypeID, &e.SourceID, &e.TargetID, &metaRaw, &ca); err != nil {
			return domain.TopologyGraph{}, err
		}
		e.TreeKind = domain.TreeKind(tk)
		e.Metadata = make(map[string]any)
		unmarshalJSON(metaRaw, &e.Metadata)
		e.CreatedAt = parseTime(ca)
		edges = append(edges, e)
	}
	return domain.TopologyGraph{
		TreeKind:  treeKind,
		Instances: instances,
		Edges:     edges,
	}, rows.Err()
}

// CascadeDeleteInstance implements checkpointed bottom-up cascade delete for CMDB subtrees (LiveOps 领域/02).
func (r *Repo) CascadeDeleteInstance(ctx context.Context, scope domain.Scope, rootID, actorID string) (domain.DeleteOperation, error) {
	now := r.Now()
	nowStr := formatTime(now)
	opID := id.New("delop")
	op := domain.DeleteOperation{
		TenantID: scope.TenantID, AppID: scope.AppID, OpID: opID, RootInstanceID: rootID,
		State: "RUNNING", CreatedBy: actorID, CreatedAt: now, UpdatedAt: now,
	}

	err := r.db.WithScopeTx(ctx, dbScope(scope), func(ctx context.Context, tx *db.Tx) error {
		var exists int
		if err := tx.QueryRow(ctx,
			`SELECT COUNT(*) FROM assets_instances WHERE tenant_id=? AND app_id=? AND instance_id=?`,
			scope.TenantID, scope.AppID, rootID,
		).Scan(&exists); err != nil || exists == 0 {
			return domain.NotFoundError("asset instance not found")
		}

		// Collect BFS subtree across tree edges (business/resource)
		type item struct {
			id    string
			depth int
		}
		queue := []item{{id: rootID, depth: 0}}
		seen := map[string]bool{rootID: true}
		var ordered []item
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			ordered = append(ordered, cur)

			children, err := listTreeChildrenTx(ctx, tx, scope, cur.id)
			if err != nil {
				return err
			}
			for _, child := range children {
				if !seen[child] {
					seen[child] = true
					queue = append(queue, item{id: child, depth: cur.depth + 1})
				}
			}
		}

		op.TotalItems = len(ordered)
		if _, err := tx.Exec(ctx,
			`INSERT INTO assets_delete_operations (tenant_id, app_id, op_id, root_instance_id, state, total_items, deleted_items, error_message, created_by, created_at, updated_at)
			 VALUES (?, ?, ?, ?, 'RUNNING', ?, 0, '', ?, ?, ?)`,
			scope.TenantID, scope.AppID, opID, rootID, op.TotalItems, actorID, nowStr, nowStr,
		); err != nil {
			return err
		}
		for _, it := range ordered {
			if _, err := tx.Exec(ctx,
				`INSERT INTO assets_delete_items (tenant_id, app_id, op_id, instance_id, depth, status) VALUES (?, ?, ?, ?, ?, 'PENDING')`,
				scope.TenantID, scope.AppID, opID, it.id, it.depth,
			); err != nil {
				return err
			}
		}

		// Delete bottom-up (reverse BFS order)
		for i := len(ordered) - 1; i >= 0; i-- {
			it := ordered[i]
			if _, err := tx.Exec(ctx,
				`DELETE FROM assets_instances WHERE tenant_id=? AND app_id=? AND instance_id=?`,
				scope.TenantID, scope.AppID, it.id,
			); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx,
				`UPDATE assets_delete_items SET status='DELETED' WHERE tenant_id=? AND app_id=? AND op_id=? AND instance_id=?`,
				scope.TenantID, scope.AppID, opID, it.id,
			); err != nil {
				return err
			}
			op.DeletedItems++
		}
		op.State = "COMPLETED"
		_, err := tx.Exec(ctx,
			`UPDATE assets_delete_operations SET state='COMPLETED', deleted_items=?, updated_at=? WHERE tenant_id=? AND app_id=? AND op_id=?`,
			op.DeletedItems, nowStr, scope.TenantID, scope.AppID, opID,
		)
		return err
	})
	return op, err
}

func listTreeChildrenTx(ctx context.Context, tx *db.Tx, scope domain.Scope, parentID string) ([]string, error) {
	rows, err := tx.Query(ctx,
		`SELECT target_id FROM assets_edges WHERE tenant_id=? AND app_id=? AND tree_kind IN ('business','resource') AND source_id=?`,
		scope.TenantID, scope.AppID, parentID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var child string
		if err := rows.Scan(&child); err != nil {
			return nil, err
		}
		out = append(out, child)
	}
	return out, rows.Err()
}

package store

import (
	"context"
	"fmt"

	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/pkg/platform/db"
	"github.com/kinstall/kinstall/pkg/platform/id"
)

// ============================================================================
// Configurations — Optimistic Locking + Atomic History + Outbox (LiveOps S05-S07)
// ============================================================================

func (r *Repo) ListConfigurations(ctx context.Context, scope domain.Scope, projectID string) ([]domain.Configuration, error) {
	q := `SELECT tenant_id, app_id, cfg_id, project_id, name, format, description, entries_json, version, deleted, created_by, updated_by, created_at, updated_at
	      FROM cfg_configurations WHERE tenant_id=? AND app_id=? AND deleted=0`
	args := []any{scope.TenantID, scope.AppID}
	if projectID != "" {
		q += ` AND project_id=?`
		args = append(args, projectID)
	}
	q += ` ORDER BY updated_at DESC`
	rows, err := r.db.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Configuration
	for rows.Next() {
		c, err := scanConfigRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *Repo) GetConfiguration(ctx context.Context, scope domain.Scope, cfgID string) (domain.Configuration, error) {
	var c domain.Configuration
	var entriesRaw, ca, ua string
	var del int
	err := r.db.QueryRow(ctx,
		`SELECT tenant_id, app_id, cfg_id, project_id, name, format, description, entries_json, version, deleted, created_by, updated_by, created_at, updated_at
		 FROM cfg_configurations WHERE tenant_id=? AND app_id=? AND cfg_id=?`,
		scope.TenantID, scope.AppID, cfgID,
	).Scan(&c.TenantID, &c.AppID, &c.CfgID, &c.ProjectID, &c.Name, &c.Format, &c.Description, &entriesRaw, &c.Version, &del, &c.CreatedBy, &c.UpdatedBy, &ca, &ua)
	if err != nil {
		return domain.Configuration{}, mapNotFound(err, "configuration not found")
	}
	c.Deleted = del != 0
	unmarshalJSON(entriesRaw, &c.Entries)
	c.CreatedAt, c.UpdatedAt = parseTime(ca), parseTime(ua)
	return c, nil
}

func scanConfigRow(rows interface{ Scan(...any) error }) (domain.Configuration, error) {
	var c domain.Configuration
	var entriesRaw, ca, ua string
	var del int
	if err := rows.Scan(&c.TenantID, &c.AppID, &c.CfgID, &c.ProjectID, &c.Name, &c.Format, &c.Description, &entriesRaw, &c.Version, &del, &c.CreatedBy, &c.UpdatedBy, &ca, &ua); err != nil {
		return domain.Configuration{}, err
	}
	c.Deleted = del != 0
	unmarshalJSON(entriesRaw, &c.Entries)
	c.CreatedAt, c.UpdatedAt = parseTime(ca), parseTime(ua)
	return c, nil
}

// CreateConfiguration writes cfg_configurations + initial cfg_history (v1) + sys_outbox_events in ONE atomic tx.
func (r *Repo) CreateConfiguration(ctx context.Context, scope domain.Scope, projectID, name, format, desc, actorID string, entries []domain.ConfigEntry) (domain.Configuration, error) {
	now := r.Now()
	nowStr := formatTime(now)
	if format == "" {
		format = "kv"
	}
	cid := id.New("cfg")
	entriesJSON := marshalJSON(entries)
	cfg := domain.Configuration{
		TenantID: scope.TenantID, AppID: scope.AppID, CfgID: cid, ProjectID: projectID,
		Name: name, Format: format, Description: desc, Entries: entries, Version: 1,
		CreatedBy: actorID, UpdatedBy: actorID, CreatedAt: now, UpdatedAt: now,
	}

	err := r.db.WithScopeTx(ctx, dbScope(scope), func(ctx context.Context, tx *db.Tx) error {
		if _, err := tx.Exec(ctx,
			`INSERT INTO cfg_configurations (tenant_id, app_id, cfg_id, project_id, name, format, description, entries_json, version, deleted, created_by, updated_by, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, 0, ?, ?, ?, ?)`,
			scope.TenantID, scope.AppID, cid, projectID, name, format, desc, entriesJSON, actorID, actorID, nowStr, nowStr,
		); err != nil {
			if isUniqueViolation(err) {
				return domain.NewError(domain.ErrAlreadyExists, "CONFIG_EXISTS", "同项目下配置名称已存在", nil)
			}
			return err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO cfg_history (tenant_id, app_id, cfg_id, version, entries_json, change_summary, changed_by, created_at)
			 VALUES (?, ?, ?, 1, ?, '创建初始版本 v1', ?, ?)`,
			scope.TenantID, scope.AppID, cid, entriesJSON, actorID, nowStr,
		); err != nil {
			return err
		}
		_, err := tx.Exec(ctx,
			`INSERT INTO sys_outbox_events (event_id, tenant_id, app_id, dedup_key, topic, aggregate_type, aggregate_id, payload_json, status, published_at, created_at)
			 VALUES (?, ?, ?, ?, 'config.version.published', 'configuration', ?, ?, 'PENDING', '', ?)`,
			id.New("evt"), scope.TenantID, scope.AppID, fmt.Sprintf("cfg:%s:v1", cid), cid,
			marshalJSON(map[string]any{"cfgId": cid, "version": 1, "actor": actorID}), nowStr,
		)
		return err
	})
	return cfg, err
}

// UpdateConfiguration enforces LiveOps S05-S07:
//   - Requires matching If-Match expectedVer -> 412 VERSION_MISMATCH on conflict
//   - Atomically updates cfg_configurations (version = version + 1), appends immutable cfg_history snapshot, and writes sys_outbox_events!
func (r *Repo) UpdateConfiguration(ctx context.Context, scope domain.Scope, cfgID string, expectedVer int64, desc, changeSummary, actorID string, entries []domain.ConfigEntry) (domain.Configuration, error) {
	nowStr := r.NowStr()
	entriesJSON := marshalJSON(entries)
	if changeSummary == "" {
		changeSummary = fmt.Sprintf("更新至版本 v%d", expectedVer+1)
	}
	err := r.db.WithScopeTx(ctx, dbScope(scope), func(ctx context.Context, tx *db.Tx) error {
		var curVer int64
		var del int
		if err := tx.QueryRow(ctx,
			`SELECT version, deleted FROM cfg_configurations WHERE tenant_id=? AND app_id=? AND cfg_id=?`,
			scope.TenantID, scope.AppID, cfgID,
		).Scan(&curVer, &del); err != nil {
			return mapNotFound(err, "configuration not found")
		}
		if del != 0 {
			return domain.NewError(domain.ErrStateConflict, "CONFIG_DELETED", "配置已软删墓碑化，禁止更新", nil)
		}
		if curVer != expectedVer {
			return domain.NewError(domain.ErrVersionMismatch, "VERSION_MISMATCH", "配置版本已过期，请刷新后重试", map[string]any{"currentVersion": curVer})
		}

		newVer := expectedVer + 1
		res, err := tx.Exec(ctx,
			`UPDATE cfg_configurations SET description=?, entries_json=?, version=?, updated_by=?, updated_at=?
			 WHERE tenant_id=? AND app_id=? AND cfg_id=? AND version=? AND deleted=0`,
			desc, entriesJSON, newVer, actorID, nowStr, scope.TenantID, scope.AppID, cfgID, expectedVer,
		)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return domain.NewError(domain.ErrVersionMismatch, "VERSION_MISMATCH", "配置并发更新冲突", nil)
		}

		if _, err := tx.Exec(ctx,
			`INSERT INTO cfg_history (tenant_id, app_id, cfg_id, version, entries_json, change_summary, changed_by, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			scope.TenantID, scope.AppID, cfgID, newVer, entriesJSON, changeSummary, actorID, nowStr,
		); err != nil {
			return err
		}

		_, err = tx.Exec(ctx,
			`INSERT INTO sys_outbox_events (event_id, tenant_id, app_id, dedup_key, topic, aggregate_type, aggregate_id, payload_json, status, published_at, created_at)
			 VALUES (?, ?, ?, ?, 'config.version.published', 'configuration', ?, ?, 'PENDING', '', ?)`,
			id.New("evt"), scope.TenantID, scope.AppID, fmt.Sprintf("cfg:%s:v%d", cfgID, newVer), cfgID,
			marshalJSON(map[string]any{"cfgId": cfgID, "version": newVer, "actor": actorID}), nowStr,
		)
		return err
	})
	if err != nil {
		return domain.Configuration{}, err
	}
	return r.GetConfiguration(ctx, scope, cfgID)
}

// TombstoneConfiguration soft-deletes a configuration while preserving immutable cfg_history snapshots (LiveOps 领域/03).
func (r *Repo) TombstoneConfiguration(ctx context.Context, scope domain.Scope, cfgID string, expectedVer int64, actorID string) error {
	nowStr := r.NowStr()
	return r.db.WithScopeTx(ctx, dbScope(scope), func(ctx context.Context, tx *db.Tx) error {
		var envRef int
		if err := tx.QueryRow(ctx,
			`SELECT COUNT(*) FROM cfg_env_members WHERE tenant_id=? AND app_id=? AND cfg_id=?`,
			scope.TenantID, scope.AppID, cfgID,
		).Scan(&envRef); err != nil {
			return err
		}
		if envRef > 0 {
			return domain.NewError(domain.ErrReferenceInUse, "CONFIG_REFERENCED_BY_ENV", "该配置仍被环境引用，请先从环境移除后再删除", nil)
		}
		res, err := tx.Exec(ctx,
			`UPDATE cfg_configurations SET deleted=1, version=version+1, updated_by=?, updated_at=?
			 WHERE tenant_id=? AND app_id=? AND cfg_id=? AND version=? AND deleted=0`,
			actorID, nowStr, scope.TenantID, scope.AppID, cfgID, expectedVer,
		)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return domain.NewError(domain.ErrVersionMismatch, "VERSION_MISMATCH", "配置不存在或版本冲突", nil)
		}
		return nil
	})
}

func (r *Repo) ListConfigHistory(ctx context.Context, scope domain.Scope, cfgID string) ([]domain.ConfigSnapshot, error) {
	rows, err := r.db.Query(ctx,
		`SELECT tenant_id, app_id, cfg_id, version, entries_json, change_summary, changed_by, created_at
		 FROM cfg_history WHERE tenant_id=? AND app_id=? AND cfg_id=? ORDER BY version DESC`,
		scope.TenantID, scope.AppID, cfgID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.ConfigSnapshot
	for rows.Next() {
		var s domain.ConfigSnapshot
		var raw, ca string
		if err := rows.Scan(&s.TenantID, &s.AppID, &s.CfgID, &s.Version, &raw, &s.ChangeSummary, &s.ChangedBy, &ca); err != nil {
			return nil, err
		}
		unmarshalJSON(raw, &s.Entries)
		s.CreatedAt = parseTime(ca)
		out = append(out, s)
	}
	return out, rows.Err()
}

// ============================================================================
// Environments — Ordered Members, Pre-Mutation History & Conflict ViewCheck
// ============================================================================

func (r *Repo) ListEnvironments(ctx context.Context, scope domain.Scope, projectID string) ([]domain.Environment, error) {
	q := `SELECT tenant_id, app_id, env_id, project_id, code, name, description, version, created_by, updated_by, created_at, updated_at
	      FROM cfg_environments WHERE tenant_id=? AND app_id=?`
	args := []any{scope.TenantID, scope.AppID}
	if projectID != "" {
		q += ` AND project_id=?`
		args = append(args, projectID)
	}
	q += ` ORDER BY created_at ASC`
	rows, err := r.db.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Environment
	for rows.Next() {
		var e domain.Environment
		var ca, ua string
		if err := rows.Scan(&e.TenantID, &e.AppID, &e.EnvID, &e.ProjectID, &e.Code, &e.Name, &e.Description, &e.Version, &e.CreatedBy, &e.UpdatedBy, &ca, &ua); err != nil {
			return nil, err
		}
		e.CreatedAt, e.UpdatedAt = parseTime(ca), parseTime(ua)
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		members, err := r.listEnvMembers(ctx, scope, out[i].EnvID)
		if err != nil {
			return nil, err
		}
		out[i].Members = members
	}
	return out, nil
}

func (r *Repo) GetEnvironment(ctx context.Context, scope domain.Scope, envID string) (domain.Environment, error) {
	var e domain.Environment
	var ca, ua string
	err := r.db.QueryRow(ctx,
		`SELECT tenant_id, app_id, env_id, project_id, code, name, description, version, created_by, updated_by, created_at, updated_at
		 FROM cfg_environments WHERE tenant_id=? AND app_id=? AND env_id=?`,
		scope.TenantID, scope.AppID, envID,
	).Scan(&e.TenantID, &e.AppID, &e.EnvID, &e.ProjectID, &e.Code, &e.Name, &e.Description, &e.Version, &e.CreatedBy, &e.UpdatedBy, &ca, &ua)
	if err != nil {
		return domain.Environment{}, mapNotFound(err, "environment not found")
	}
	e.CreatedAt, e.UpdatedAt = parseTime(ca), parseTime(ua)
	members, err := r.listEnvMembers(ctx, scope, envID)
	if err != nil {
		return domain.Environment{}, err
	}
	e.Members = members
	return e, nil
}

func (r *Repo) listEnvMembers(ctx context.Context, scope domain.Scope, envID string) ([]domain.EnvMember, error) {
	rows, err := r.db.Query(ctx,
		`SELECT m.cfg_id, c.name, c.version, m.priority
		 FROM cfg_env_members m
		 JOIN cfg_configurations c ON c.tenant_id=m.tenant_id AND c.app_id=m.app_id AND c.cfg_id=m.cfg_id
		 WHERE m.tenant_id=? AND m.app_id=? AND m.env_id=?
		 ORDER BY m.priority ASC`,
		scope.TenantID, scope.AppID, envID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.EnvMember
	for rows.Next() {
		var m domain.EnvMember
		if err := rows.Scan(&m.CfgID, &m.CfgName, &m.CfgVersion, &m.Priority); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r *Repo) CreateEnvironment(ctx context.Context, scope domain.Scope, projectID, code, name, desc, actorID string, members []domain.EnvMember) (domain.Environment, error) {
	now := r.Now()
	nowStr := formatTime(now)
	eid := id.New("env")
	err := r.db.WithScopeTx(ctx, dbScope(scope), func(ctx context.Context, tx *db.Tx) error {
		if _, err := tx.Exec(ctx,
			`INSERT INTO cfg_environments (tenant_id, app_id, env_id, project_id, code, name, description, version, created_by, updated_by, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?, ?)`,
			scope.TenantID, scope.AppID, eid, projectID, code, name, desc, actorID, actorID, nowStr, nowStr,
		); err != nil {
			if isUniqueViolation(err) {
				return domain.NewError(domain.ErrAlreadyExists, "ENV_EXISTS", "同项目下环境编码已存在", nil)
			}
			return err
		}
		for i, m := range members {
			prio := m.Priority
			if prio <= 0 {
				prio = (i + 1) * 10
			}
			if _, err := tx.Exec(ctx,
				`INSERT INTO cfg_env_members (tenant_id, app_id, env_id, cfg_id, priority) VALUES (?, ?, ?, ?, ?)`,
				scope.TenantID, scope.AppID, eid, m.CfgID, prio,
			); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return domain.Environment{}, err
	}
	return r.GetEnvironment(ctx, scope, eid)
}

// UpdateEnvironmentMembers atomically archives pre-mutation snapshot in cfg_env_history and updates members under If-Match.
func (r *Repo) UpdateEnvironmentMembers(ctx context.Context, scope domain.Scope, envID string, expectedVer int64, actorID string, members []domain.EnvMember) (domain.Environment, error) {
	nowStr := r.NowStr()
	prevMembers, err := r.listEnvMembers(ctx, scope, envID)
	if err != nil {
		return domain.Environment{}, err
	}
	err = r.db.WithScopeTx(ctx, dbScope(scope), func(ctx context.Context, tx *db.Tx) error {
		res, err := tx.Exec(ctx,
			`UPDATE cfg_environments SET version=version+1, updated_by=?, updated_at=?
			 WHERE tenant_id=? AND app_id=? AND env_id=? AND version=?`,
			actorID, nowStr, scope.TenantID, scope.AppID, envID, expectedVer,
		)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return domain.NewError(domain.ErrVersionMismatch, "VERSION_MISMATCH", "环境版本已变更，请刷新后重试", nil)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO cfg_env_history (tenant_id, app_id, env_id, version, members_json, changed_by, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			scope.TenantID, scope.AppID, envID, expectedVer, marshalJSON(prevMembers), actorID, nowStr,
		); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`DELETE FROM cfg_env_members WHERE tenant_id=? AND app_id=? AND env_id=?`,
			scope.TenantID, scope.AppID, envID,
		); err != nil {
			return err
		}
		for i, m := range members {
			prio := m.Priority
			if prio <= 0 {
				prio = (i + 1) * 10
			}
			if _, err := tx.Exec(ctx,
				`INSERT INTO cfg_env_members (tenant_id, app_id, env_id, cfg_id, priority) VALUES (?, ?, ?, ?, ?)`,
				scope.TenantID, scope.AppID, envID, m.CfgID, prio,
			); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return domain.Environment{}, err
	}
	return r.GetEnvironment(ctx, scope, envID)
}

package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/kinstall/kinstall/internal/domain"
)

type cmdbRepo struct{ s *DBStore }

func (r *cmdbRepo) CreateDynamicGroup(ctx context.Context, g *domain.CMDBDynamicGroup) error {
	now := time.Now().UTC().Truncate(time.Microsecond)
	if g.ID == "" {
		g.ID = domain.NewID(domain.PrefixDynamicGroup)
	}
	if g.Rules == nil {
		g.Rules = []domain.CMDBGroupRule{}
	}
	rulesB, err := json.Marshal(g.Rules)
	if err != nil {
		return err
	}
	g.CreatedAt, g.UpdatedAt = now, now
	_, err = r.s.writeDB.NewRaw(
		`INSERT INTO cmdb_dynamic_groups (id, project_id, name, description, rules_json, created_by, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		g.ID, g.ProjectID, g.Name, g.Description, string(rulesB), g.CreatedBy, fmtTime(now), fmtTime(now),
	).Exec(ctx)
	return err
}

func (r *cmdbRepo) GetDynamicGroup(ctx context.Context, id string) (*domain.CMDBDynamicGroup, error) {
	var g domain.CMDBDynamicGroup
	var rulesStr, cAt, uAt string
	err := r.s.readDB.NewRaw(
		`SELECT id, project_id, name, description, rules_json, created_by, created_at, updated_at
		 FROM cmdb_dynamic_groups WHERE id = ?`, id,
	).Scan(ctx, &g.ID, &g.ProjectID, &g.Name, &g.Description, &rulesStr, &g.CreatedBy, &cAt, &uAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFoundErr("cmdb_dynamic_group", id)
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(rulesStr), &g.Rules)
	if g.Rules == nil {
		g.Rules = []domain.CMDBGroupRule{}
	}
	g.CreatedAt, g.UpdatedAt = parseTime(cAt), parseTime(uAt)
	return &g, nil
}

func (r *cmdbRepo) ListDynamicGroups(ctx context.Context, projectID string) ([]domain.CMDBDynamicGroup, error) {
	rows, err := r.s.readDB.QueryContext(ctx,
		`SELECT id, project_id, name, description, rules_json, created_by, created_at, updated_at
		 FROM cmdb_dynamic_groups WHERE project_id = ? ORDER BY name ASC`, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]domain.CMDBDynamicGroup, 0)
	for rows.Next() {
		var g domain.CMDBDynamicGroup
		var rulesStr, cAt, uAt string
		if err := rows.Scan(&g.ID, &g.ProjectID, &g.Name, &g.Description, &rulesStr, &g.CreatedBy, &cAt, &uAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(rulesStr), &g.Rules)
		if g.Rules == nil {
			g.Rules = []domain.CMDBGroupRule{}
		}
		g.CreatedAt, g.UpdatedAt = parseTime(cAt), parseTime(uAt)
		out = append(out, g)
	}
	return out, rows.Err()
}

func (r *cmdbRepo) UpdateDynamicGroup(ctx context.Context, g *domain.CMDBDynamicGroup) error {
	now := time.Now().UTC().Truncate(time.Microsecond)
	if g.Rules == nil {
		g.Rules = []domain.CMDBGroupRule{}
	}
	rulesB, err := json.Marshal(g.Rules)
	if err != nil {
		return err
	}
	res, err := r.s.writeDB.NewRaw(
		`UPDATE cmdb_dynamic_groups SET name = ?, description = ?, rules_json = ?, updated_at = ? WHERE id = ?`,
		g.Name, g.Description, string(rulesB), fmtTime(now), g.ID,
	).Exec(ctx)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return notFoundErr("cmdb_dynamic_group", g.ID)
	}
	g.UpdatedAt = now
	return nil
}

func (r *cmdbRepo) DeleteDynamicGroup(ctx context.Context, id string) error {
	_, err := r.s.writeDB.NewRaw(`DELETE FROM cmdb_dynamic_groups WHERE id = ?`, id).Exec(ctx)
	return err
}

func (r *cmdbRepo) CreateSyncSource(ctx context.Context, src *domain.CMDBSyncSource) error {
	now := time.Now().UTC().Truncate(time.Microsecond)
	if src.ID == "" {
		src.ID = domain.NewID(domain.PrefixCMDBSync)
	}
	if src.LastStatus == "" {
		src.LastStatus = "never"
	}
	mapB, err := json.Marshal(src.Mapping)
	if err != nil {
		return err
	}
	src.CreatedAt, src.UpdatedAt = now, now
	_, err = r.s.writeDB.NewRaw(
		`INSERT INTO cmdb_sync_sources (id, project_id, name, kind, endpoint, credential_id, mapping_json, payload_text, last_status, last_summary, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		src.ID, src.ProjectID, src.Name, src.Kind, src.Endpoint, src.CredentialID, string(mapB), src.PayloadText, src.LastStatus, src.LastSummary, fmtTime(now), fmtTime(now),
	).Exec(ctx)
	return err
}

func scanSyncSource(scanner func(dest ...any) error) (*domain.CMDBSyncSource, error) {
	var src domain.CMDBSyncSource
	var credID, lastSync sql.NullString
	var mapStr, cAt, uAt string
	if err := scanner(
		&src.ID, &src.ProjectID, &src.Name, &src.Kind, &src.Endpoint, &credID,
		&mapStr, &src.PayloadText, &lastSync, &src.LastStatus, &src.LastSummary, &cAt, &uAt,
	); err != nil {
		return nil, err
	}
	if credID.Valid && credID.String != "" {
		v := credID.String
		src.CredentialID = &v
	}
	if lastSync.Valid && lastSync.String != "" {
		t := parseTime(lastSync.String)
		src.LastSyncAt = &t
	}
	_ = json.Unmarshal([]byte(mapStr), &src.Mapping)
	src.CreatedAt, src.UpdatedAt = parseTime(cAt), parseTime(uAt)
	return &src, nil
}

func (r *cmdbRepo) GetSyncSource(ctx context.Context, id string) (*domain.CMDBSyncSource, error) {
	row := r.s.readDB.QueryRowContext(ctx,
		`SELECT id, project_id, name, kind, endpoint, credential_id, mapping_json, payload_text, last_sync_at, last_status, last_summary, created_at, updated_at
		 FROM cmdb_sync_sources WHERE id = ?`, id)
	src, err := scanSyncSource(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFoundErr("cmdb_sync_source", id)
	}
	return src, err
}

func (r *cmdbRepo) ListSyncSources(ctx context.Context, projectID string) ([]domain.CMDBSyncSource, error) {
	rows, err := r.s.readDB.QueryContext(ctx,
		`SELECT id, project_id, name, kind, endpoint, credential_id, mapping_json, payload_text, last_sync_at, last_status, last_summary, created_at, updated_at
		 FROM cmdb_sync_sources WHERE project_id = ? ORDER BY name ASC`, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]domain.CMDBSyncSource, 0)
	for rows.Next() {
		src, err := scanSyncSource(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, *src)
	}
	return out, rows.Err()
}

func (r *cmdbRepo) UpdateSyncSourceStatus(ctx context.Context, id, status, summary string, syncAt time.Time) error {
	now := fmtTime(time.Now().UTC())
	_, err := r.s.writeDB.NewRaw(
		`UPDATE cmdb_sync_sources SET last_sync_at = ?, last_status = ?, last_summary = ?, updated_at = ? WHERE id = ?`,
		fmtTime(syncAt), status, summary, now, id,
	).Exec(ctx)
	return err
}

func (r *cmdbRepo) DeleteSyncSource(ctx context.Context, id string) error {
	_, err := r.s.writeDB.NewRaw(`DELETE FROM cmdb_sync_sources WHERE id = ?`, id).Exec(ctx)
	return err
}

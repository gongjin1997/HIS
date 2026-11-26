package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"time"

	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/migrations"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

func subFSFor(dir string) (fs.FS, error) {
	return fs.Sub(migrations.FS, dir)
}

// --- ProjectRepo ---

type projectRepo struct{ s *DBStore }

func (r *projectRepo) Create(ctx context.Context, p *domain.Project) error {
	now := time.Now().UTC().Truncate(time.Microsecond)
	if p.ID == "" {
		p.ID = domain.NewID(domain.PrefixProject)
	}
	p.CreatedAt, p.UpdatedAt = now, now
	_, err := r.s.writeDB.NewRaw(
		`INSERT INTO projects (id, name, description, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		p.ID, p.Name, p.Description, fmtTime(p.CreatedAt), fmtTime(p.UpdatedAt),
	).Exec(ctx)
	return err
}

func (r *projectRepo) Get(ctx context.Context, id string) (*domain.Project, error) {
	var p domain.Project
	var cAt, uAt string
	err := r.s.readDB.NewRaw(`SELECT id, name, description, created_at, updated_at FROM projects WHERE id = ?`, id).
		Scan(ctx, &p.ID, &p.Name, &p.Description, &cAt, &uAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFoundErr("project", id)
	}
	if err != nil {
		return nil, err
	}
	p.CreatedAt, p.UpdatedAt = parseTime(cAt), parseTime(uAt)
	return &p, nil
}

func (r *projectRepo) GetByName(ctx context.Context, name string) (*domain.Project, error) {
	var p domain.Project
	var cAt, uAt string
	err := r.s.readDB.NewRaw(`SELECT id, name, description, created_at, updated_at FROM projects WHERE name = ?`, name).
		Scan(ctx, &p.ID, &p.Name, &p.Description, &cAt, &uAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFoundErr("project", name)
	}
	if err != nil {
		return nil, err
	}
	p.CreatedAt, p.UpdatedAt = parseTime(cAt), parseTime(uAt)
	return &p, nil
}

func (r *projectRepo) List(ctx context.Context) ([]domain.Project, error) {
	rows, err := r.s.readDB.QueryContext(ctx, `SELECT id, name, description, created_at, updated_at FROM projects ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Project
	for rows.Next() {
		var p domain.Project
		var cAt, uAt string
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &cAt, &uAt); err != nil {
			return nil, err
		}
		p.CreatedAt, p.UpdatedAt = parseTime(cAt), parseTime(uAt)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *projectRepo) Delete(ctx context.Context, id string) error {
	_, err := r.s.writeDB.NewRaw(`DELETE FROM projects WHERE id = ?`, id).Exec(ctx)
	return err
}

// --- CredentialRepo ---

type credentialRepo struct{ s *DBStore }

func (r *credentialRepo) Create(ctx context.Context, c *domain.Credential) error {
	now := time.Now().UTC().Truncate(time.Microsecond)
	if c.ID == "" {
		c.ID = domain.NewID(domain.PrefixCredential)
	}
	if c.State == "" {
		c.State = domain.CredentialActive
	}
	if c.KeyVersion <= 0 {
		c.KeyVersion = 1
	}
	if c.Version <= 0 {
		c.Version = 1
	}
	c.CreatedAt, c.UpdatedAt = now, now
	_, err := r.s.writeDB.NewRaw(
		`INSERT INTO credentials (id, project_id, name, kind, state, key_version, envelope_json, created_by, version, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.ProjectID, c.Name, c.Kind, string(c.State), c.KeyVersion, string(c.EnvelopeJSON), c.CreatedBy, c.Version, fmtTime(now), fmtTime(now),
	).Exec(ctx)
	return err
}

func (r *credentialRepo) Get(ctx context.Context, id string) (*domain.Credential, error) {
	var c domain.Credential
	var state, envStr, cAt, uAt string
	var prevStr sql.NullString
	err := r.s.readDB.NewRaw(
		`SELECT id, project_id, name, kind, state, key_version, envelope_json, prev_envelope, created_by, version, created_at, updated_at
		 FROM credentials WHERE id = ?`, id,
	).Scan(ctx, &c.ID, &c.ProjectID, &c.Name, &c.Kind, &state, &c.KeyVersion, &envStr, &prevStr, &c.CreatedBy, &c.Version, &cAt, &uAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFoundErr("credential", id)
	}
	if err != nil {
		return nil, err
	}
	c.State = domain.CredentialState(state)
	c.EnvelopeJSON = json.RawMessage(envStr)
	if prevStr.Valid {
		c.PrevEnvelope = json.RawMessage(prevStr.String)
	}
	c.CreatedAt, c.UpdatedAt = parseTime(cAt), parseTime(uAt)
	return &c, nil
}

func (r *credentialRepo) ListByProject(ctx context.Context, projectID string) ([]domain.Credential, error) {
	rows, err := r.s.readDB.QueryContext(ctx,
		`SELECT id, project_id, name, kind, state, key_version, envelope_json, created_by, version, created_at, updated_at
		 FROM credentials WHERE project_id = ? ORDER BY created_at DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Credential
	for rows.Next() {
		var c domain.Credential
		var state, envStr, cAt, uAt string
		if err := rows.Scan(&c.ID, &c.ProjectID, &c.Name, &c.Kind, &state, &c.KeyVersion, &envStr, &c.CreatedBy, &c.Version, &cAt, &uAt); err != nil {
			return nil, err
		}
		c.State = domain.CredentialState(state)
		c.EnvelopeJSON = json.RawMessage(envStr)
		c.CreatedAt, c.UpdatedAt = parseTime(cAt), parseTime(uAt)
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *credentialRepo) UpdateEnvelope(ctx context.Context, id string, expectedVer int64, envelopeJSON []byte, keyVersion int, state domain.CredentialState) error {
	now := fmtTime(time.Now().UTC())
	res, err := r.s.writeDB.NewRaw(
		`UPDATE credentials SET prev_envelope = envelope_json, envelope_json = ?, key_version = ?, state = ?, version = version + 1, updated_at = ?
		 WHERE id = ? AND version = ?`,
		string(envelopeJSON), keyVersion, string(state), now, id, expectedVer,
	).Exec(ctx)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sdk.NewError(sdk.CodeAPIETagConflict, "API", http.StatusPreconditionFailed, sdk.CodeAPIETagConflict, true, fmt.Errorf("credential %s version conflict", id))
	}
	return nil
}

func (r *credentialRepo) Delete(ctx context.Context, id string) error {
	_, err := r.s.writeDB.NewRaw(`DELETE FROM credentials WHERE id = ?`, id).Exec(ctx)
	return err
}

// --- HostRepo ---

type hostRepo struct{ s *DBStore }

func (r *hostRepo) Create(ctx context.Context, h *domain.Host) error {
	if !domain.ValidateDNS1123(h.Name) {
		return sdk.NewError(sdk.CodeHostInvalidName, "HOST", http.StatusUnprocessableEntity, sdk.CodeHostInvalidName, false, fmt.Errorf("invalid hostname %q", h.Name))
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if h.ID == "" {
		h.ID = domain.NewID(domain.PrefixHost)
	}
	if h.SSHPort <= 0 {
		h.SSHPort = 22
	}
	if h.WorkDir == "" {
		h.WorkDir = "/var/lib/kinstall/tmp"
	}
	if h.HostKeyMode == "" {
		h.HostKeyMode = "tofu"
	}
	if h.State == "" {
		h.State = domain.HostPending
	}
	if h.PageSize <= 0 {
		h.PageSize = 4096
	}
	if h.Version <= 0 {
		h.Version = 1
	}
	h.CreatedAt, h.UpdatedAt = now, now
	labelsJSON, _ := json.Marshal(nonNilMap(h.Labels))
	factsJSON := "{}"
	if h.Facts != nil {
		if b, err := json.Marshal(h.Facts); err == nil {
			factsJSON = string(b)
		}
	}
	_, err := r.s.writeDB.NewRaw(
		`INSERT INTO hosts (id, project_id, name, address, ssh_port, ssh_user, credential_id, jump_host_id, legacy_rsa, work_dir,
		 hostkey_mode, hostkey_fp, state, fault_zone, fault_rack, labels_json, facts_json, os_family, os_id, os_version, arch, page_size, init_profile, version, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		h.ID, h.ProjectID, h.Name, h.Address, h.SSHPort, h.SSHUser, h.CredentialID, h.JumpHostID, h.LegacyRSA, h.WorkDir,
		h.HostKeyMode, h.HostKeyFP, string(h.State), h.FaultZone, h.FaultRack, string(labelsJSON), factsJSON,
		h.OSFamily, h.OSID, h.OSVersion, h.Arch, h.PageSize, h.InitProfile, h.Version, fmtTime(now), fmtTime(now),
	).Exec(ctx)
	return err
}

func scanHostRow(scanner func(dest ...any) error) (*domain.Host, error) {
	var h domain.Host
	var state, labelsStr, factsStr, cAt, uAt string
	var jumpID, factsAt sql.NullString
	err := scanner(
		&h.ID, &h.ProjectID, &h.Name, &h.Address, &h.SSHPort, &h.SSHUser, &h.CredentialID, &jumpID, &h.LegacyRSA, &h.WorkDir,
		&h.HostKeyMode, &h.HostKeyFP, &state, &h.FaultZone, &h.FaultRack, &labelsStr, &factsStr, &factsAt,
		&h.OSFamily, &h.OSID, &h.OSVersion, &h.Arch, &h.PageSize, &h.InitProfile, &h.Version, &cAt, &uAt,
	)
	if err != nil {
		return nil, err
	}
	if jumpID.Valid {
		v := jumpID.String
		h.JumpHostID = &v
	}
	h.State = domain.HostState(state)
	_ = json.Unmarshal([]byte(labelsStr), &h.Labels)
	if h.Labels == nil {
		h.Labels = map[string]string{}
	}
	if factsStr != "" && factsStr != "{}" {
		var fv sdk.FactsView
		if json.Unmarshal([]byte(factsStr), &fv) == nil {
			h.Facts = &fv
		}
	}
	if factsAt.Valid && factsAt.String != "" {
		t := parseTime(factsAt.String)
		h.FactsAt = &t
	}
	h.CreatedAt, h.UpdatedAt = parseTime(cAt), parseTime(uAt)
	return &h, nil
}

const hostSelectCols = `id, project_id, name, address, ssh_port, ssh_user, credential_id, jump_host_id, legacy_rsa, work_dir,
	hostkey_mode, hostkey_fp, state, fault_zone, fault_rack, labels_json, facts_json, facts_at,
	os_family, os_id, os_version, arch, page_size, init_profile, version, created_at, updated_at`

func (r *hostRepo) Get(ctx context.Context, id string) (*domain.Host, error) {
	row := r.s.readDB.QueryRowContext(ctx, `SELECT `+hostSelectCols+` FROM hosts WHERE id = ?`, id)
	h, err := scanHostRow(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, sdk.NewError(sdk.CodeHostNotFound, "HOST", http.StatusNotFound, sdk.CodeHostNotFound, false, fmt.Errorf("host %s not found", id))
	}
	return h, err
}

func (r *hostRepo) GetByName(ctx context.Context, projectID, name string) (*domain.Host, error) {
	row := r.s.readDB.QueryRowContext(ctx, `SELECT `+hostSelectCols+` FROM hosts WHERE project_id = ? AND name = ?`, projectID, name)
	h, err := scanHostRow(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, sdk.NewError(sdk.CodeHostNotFound, "HOST", http.StatusNotFound, sdk.CodeHostNotFound, false, fmt.Errorf("host %s not found", name))
	}
	return h, err
}

func (r *hostRepo) ListByProject(ctx context.Context, projectID string) ([]domain.Host, error) {
	rows, err := r.s.readDB.QueryContext(ctx, `SELECT `+hostSelectCols+` FROM hosts WHERE project_id = ? ORDER BY name ASC`, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Host
	for rows.Next() {
		h, err := scanHostRow(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, *h)
	}
	return out, rows.Err()
}

func (r *hostRepo) Update(ctx context.Context, h *domain.Host) error {
	now := time.Now().UTC().Truncate(time.Microsecond)
	labelsJSON, _ := json.Marshal(nonNilMap(h.Labels))
	res, err := r.s.writeDB.NewRaw(
		`UPDATE hosts SET address = ?, ssh_port = ?, ssh_user = ?, credential_id = ?, jump_host_id = ?, legacy_rsa = ?, work_dir = ?,
		 hostkey_mode = ?, hostkey_fp = ?, state = ?, fault_zone = ?, fault_rack = ?, labels_json = ?, init_profile = ?,
		 version = version + 1, updated_at = ?
		 WHERE id = ? AND version = ?`,
		h.Address, h.SSHPort, h.SSHUser, h.CredentialID, h.JumpHostID, h.LegacyRSA, h.WorkDir,
		h.HostKeyMode, h.HostKeyFP, string(h.State), h.FaultZone, h.FaultRack, string(labelsJSON), h.InitProfile,
		fmtTime(now), h.ID, h.Version,
	).Exec(ctx)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sdk.NewError(sdk.CodeAPIETagConflict, "API", http.StatusPreconditionFailed, sdk.CodeAPIETagConflict, true, fmt.Errorf("host %s version conflict", h.ID))
	}
	h.Version++
	h.UpdatedAt = now
	return nil
}

func (r *hostRepo) UpdateFacts(ctx context.Context, id string, facts sdk.FactsView) error {
	now := fmtTime(time.Now().UTC())
	b, err := json.Marshal(facts)
	if err != nil {
		return err
	}
	ps := facts.PageSize
	if ps <= 0 {
		ps = 4096
	}
	_, err = r.s.writeDB.NewRaw(
		`UPDATE hosts SET facts_json = ?, facts_at = ?, os_family = ?, os_id = ?, os_version = ?, arch = ?, page_size = ?, state = ?, updated_at = ?
		 WHERE id = ?`,
		string(b), now, facts.OS.Family, facts.OS.ID, facts.OS.VersionID, facts.Arch, ps, string(domain.HostReady), now, id,
	).Exec(ctx)
	return err
}

func (r *hostRepo) UpdateHostKey(ctx context.Context, id, fp string) error {
	_, err := r.s.writeDB.NewRaw(`UPDATE hosts SET hostkey_fp = ?, updated_at = ? WHERE id = ?`, fp, fmtTime(time.Now().UTC()), id).Exec(ctx)
	return err
}

func (r *hostRepo) Delete(ctx context.Context, id string) error {
	_, err := r.s.writeDB.NewRaw(`DELETE FROM hosts WHERE id = ?`, id).Exec(ctx)
	return err
}

// --- StackRepo ---

type stackRepo struct{ s *DBStore }

func (r *stackRepo) Create(ctx context.Context, st *domain.Stack) error {
	now := time.Now().UTC().Truncate(time.Microsecond)
	if st.ID == "" {
		st.ID = domain.NewID(domain.PrefixStack)
	}
	if st.State == "" {
		st.State = "pending"
	}
	if st.Version <= 0 {
		st.Version = 1
	}
	dag := string(st.ResolvedDAGJSON)
	if dag == "" {
		dag = "{}"
	}
	st.CreatedAt, st.UpdatedAt = now, now
	_, err := r.s.writeDB.NewRaw(
		`INSERT INTO stacks (id, project_id, env, name, spec_yaml, resolved_dag_json, state, version, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		st.ID, st.ProjectID, st.Env, st.Name, st.SpecYAML, dag, st.State, st.Version, fmtTime(now), fmtTime(now),
	).Exec(ctx)
	return err
}

func (r *stackRepo) Get(ctx context.Context, id string) (*domain.Stack, error) {
	var st domain.Stack
	var dagStr, cAt, uAt string
	err := r.s.readDB.NewRaw(
		`SELECT id, project_id, env, name, spec_yaml, resolved_dag_json, state, version, created_at, updated_at FROM stacks WHERE id = ?`, id,
	).Scan(ctx, &st.ID, &st.ProjectID, &st.Env, &st.Name, &st.SpecYAML, &dagStr, &st.State, &st.Version, &cAt, &uAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFoundErr("stack", id)
	}
	if err != nil {
		return nil, err
	}
	st.ResolvedDAGJSON = json.RawMessage(dagStr)
	st.CreatedAt, st.UpdatedAt = parseTime(cAt), parseTime(uAt)
	return &st, nil
}

func (r *stackRepo) ListByProject(ctx context.Context, projectID string) ([]domain.Stack, error) {
	rows, err := r.s.readDB.QueryContext(ctx,
		`SELECT id, project_id, env, name, spec_yaml, resolved_dag_json, state, version, created_at, updated_at
		 FROM stacks WHERE project_id = ? ORDER BY created_at DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Stack
	for rows.Next() {
		var st domain.Stack
		var dagStr, cAt, uAt string
		if err := rows.Scan(&st.ID, &st.ProjectID, &st.Env, &st.Name, &st.SpecYAML, &dagStr, &st.State, &st.Version, &cAt, &uAt); err != nil {
			return nil, err
		}
		st.ResolvedDAGJSON = json.RawMessage(dagStr)
		st.CreatedAt, st.UpdatedAt = parseTime(cAt), parseTime(uAt)
		out = append(out, st)
	}
	return out, rows.Err()
}

func (r *stackRepo) Update(ctx context.Context, st *domain.Stack) error {
	now := time.Now().UTC().Truncate(time.Microsecond)
	dag := string(st.ResolvedDAGJSON)
	if dag == "" {
		dag = "{}"
	}
	res, err := r.s.writeDB.NewRaw(
		`UPDATE stacks SET spec_yaml = ?, resolved_dag_json = ?, state = ?, version = version + 1, updated_at = ?
		 WHERE id = ? AND version = ?`,
		st.SpecYAML, dag, st.State, fmtTime(now), st.ID, st.Version,
	).Exec(ctx)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sdk.NewError(sdk.CodeAPIETagConflict, "API", http.StatusPreconditionFailed, sdk.CodeAPIETagConflict, true, fmt.Errorf("stack %s version conflict", st.ID))
	}
	st.Version++
	st.UpdatedAt = now
	return nil
}

func (r *stackRepo) Delete(ctx context.Context, id string) error {
	_, err := r.s.writeDB.NewRaw(`DELETE FROM stacks WHERE id = ?`, id).Exec(ctx)
	return err
}

// --- InstanceRepo ---

type instanceRepo struct{ s *DBStore }

func (r *instanceRepo) Create(ctx context.Context, inst *domain.Instance) error {
	now := time.Now().UTC().Truncate(time.Microsecond)
	if inst.ID == "" {
		inst.ID = domain.NewID(domain.PrefixInstance)
	}
	if inst.State == "" {
		inst.State = "pending"
	}
	if inst.PluginSchemaVer <= 0 {
		inst.PluginSchemaVer = 1
	}
	if inst.Version <= 0 {
		inst.Version = 1
	}
	desired := defaultJSON(inst.DesiredSpecJSON, "{}")
	stateJSON := defaultJSON(inst.StateJSON, "{}")
	outputsJSON := defaultJSON(inst.OutputsJSON, "{}")
	var lastApp any
	if len(inst.LastAppliedJSON) > 0 {
		lastApp = string(inst.LastAppliedJSON)
	}
	inst.CreatedAt, inst.UpdatedAt = now, now
	_, err := r.s.writeDB.NewRaw(
		`INSERT INTO instances (id, project_id, stack_id, env, name, product, deploy_target, product_version, bom_revision,
		 plugin_schema_ver, desired_spec_json, last_applied_json, state_json, outputs_json, state, version, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		inst.ID, inst.ProjectID, inst.StackID, inst.Env, inst.Name, inst.Product, string(inst.DeployTarget),
		inst.ProductVersion, inst.BOMRevision, inst.PluginSchemaVer, desired, lastApp, stateJSON, outputsJSON,
		inst.State, inst.Version, fmtTime(now), fmtTime(now),
	).Exec(ctx)
	return err
}

const instanceSelectCols = `id, project_id, stack_id, env, name, product, deploy_target, product_version, bom_revision,
	plugin_schema_ver, desired_spec_json, last_applied_json, state_json, outputs_json, state, version, created_at, updated_at`

func scanInstanceRow(scan func(dest ...any) error) (*domain.Instance, error) {
	var inst domain.Instance
	var stackID, lastApp sql.NullString
	var dt, desired, stateJ, outJ, cAt, uAt string
	err := scan(
		&inst.ID, &inst.ProjectID, &stackID, &inst.Env, &inst.Name, &inst.Product, &dt, &inst.ProductVersion,
		&inst.BOMRevision, &inst.PluginSchemaVer, &desired, &lastApp, &stateJ, &outJ, &inst.State, &inst.Version, &cAt, &uAt,
	)
	if err != nil {
		return nil, err
	}
	if stackID.Valid {
		v := stackID.String
		inst.StackID = &v
	}
	inst.DeployTarget = sdk.DeployTarget(dt)
	inst.DesiredSpecJSON = json.RawMessage(desired)
	if lastApp.Valid {
		inst.LastAppliedJSON = json.RawMessage(lastApp.String)
	}
	inst.StateJSON = json.RawMessage(stateJ)
	inst.OutputsJSON = json.RawMessage(outJ)
	inst.CreatedAt, inst.UpdatedAt = parseTime(cAt), parseTime(uAt)
	return &inst, nil
}

func (r *instanceRepo) Get(ctx context.Context, id string) (*domain.Instance, error) {
	row := r.s.readDB.QueryRowContext(ctx, `SELECT `+instanceSelectCols+` FROM instances WHERE id = ?`, id)
	inst, err := scanInstanceRow(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFoundErr("instance", id)
	}
	return inst, err
}

func (r *instanceRepo) ListByProject(ctx context.Context, projectID string) ([]domain.Instance, error) {
	rows, err := r.s.readDB.QueryContext(ctx, `SELECT `+instanceSelectCols+` FROM instances WHERE project_id = ? ORDER BY created_at DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Instance
	for rows.Next() {
		inst, err := scanInstanceRow(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, *inst)
	}
	return out, rows.Err()
}

func (r *instanceRepo) ListByStack(ctx context.Context, stackID string) ([]domain.Instance, error) {
	rows, err := r.s.readDB.QueryContext(ctx, `SELECT `+instanceSelectCols+` FROM instances WHERE stack_id = ? ORDER BY created_at ASC`, stackID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Instance
	for rows.Next() {
		inst, err := scanInstanceRow(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, *inst)
	}
	return out, rows.Err()
}

func (r *instanceRepo) Update(ctx context.Context, inst *domain.Instance) error {
	now := time.Now().UTC().Truncate(time.Microsecond)
	var lastApp any
	if len(inst.LastAppliedJSON) > 0 {
		lastApp = string(inst.LastAppliedJSON)
	}
	res, err := r.s.writeDB.NewRaw(
		`UPDATE instances SET product_version = ?, bom_revision = ?, plugin_schema_ver = ?, desired_spec_json = ?,
		 last_applied_json = ?, state_json = ?, outputs_json = ?, state = ?, version = version + 1, updated_at = ?
		 WHERE id = ? AND version = ?`,
		inst.ProductVersion, inst.BOMRevision, inst.PluginSchemaVer, defaultJSON(inst.DesiredSpecJSON, "{}"),
		lastApp, defaultJSON(inst.StateJSON, "{}"), defaultJSON(inst.OutputsJSON, "{}"), inst.State, fmtTime(now),
		inst.ID, inst.Version,
	).Exec(ctx)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sdk.NewError(sdk.CodeAPIETagConflict, "API", http.StatusPreconditionFailed, sdk.CodeAPIETagConflict, true, fmt.Errorf("instance %s version conflict", inst.ID))
	}
	inst.Version++
	inst.UpdatedAt = now
	return nil
}

func (r *instanceRepo) Delete(ctx context.Context, id string) error {
	_, err := r.s.writeDB.NewRaw(`DELETE FROM instances WHERE id = ?`, id).Exec(ctx)
	return err
}

func (r *instanceRepo) ReplaceClaims(ctx context.Context, instanceID string, claims []domain.HostClaim) error {
	tx, err := r.s.writeDB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.NewRaw(`DELETE FROM instance_host_claims WHERE instance_id = ?`, instanceID).Exec(ctx); err != nil {
		return err
	}
	for _, c := range claims {
		id := c.ID
		if id == "" {
			id = domain.NewID(domain.PrefixClaim)
		}
		portsJ, _ := json.Marshal(c.Ports)
		pathsJ, _ := json.Marshal(c.Paths)
		subnetsJ, _ := json.Marshal(c.Subnets)
		if _, err := tx.NewRaw(
			`INSERT INTO instance_host_claims (id, instance_id, host_id, role, ports_json, paths_json, subnets_json, mem_mb)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			id, instanceID, c.HostID, c.Role, string(portsJ), string(pathsJ), string(subnetsJ), c.MemMB,
		).Exec(ctx); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *instanceRepo) ListClaimsByHost(ctx context.Context, hostID string) ([]domain.HostClaim, error) {
	rows, err := r.s.readDB.QueryContext(ctx,
		`SELECT id, instance_id, host_id, role, ports_json, paths_json, subnets_json, mem_mb
		 FROM instance_host_claims WHERE host_id = ?`, hostID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.HostClaim
	for rows.Next() {
		var c domain.HostClaim
		var portsS, pathsS, subnetsS string
		if err := rows.Scan(&c.ID, &c.InstanceID, &c.HostID, &c.Role, &portsS, &pathsS, &subnetsS, &c.MemMB); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(portsS), &c.Ports)
		_ = json.Unmarshal([]byte(pathsS), &c.Paths)
		_ = json.Unmarshal([]byte(subnetsS), &c.Subnets)
		out = append(out, c)
	}
	return out, rows.Err()
}

// --- PlanRepo & StateBackupRepo ---

type planRepo struct{ s *DBStore }

func (r *planRepo) Create(ctx context.Context, p *domain.Plan) error {
	now := time.Now().UTC().Truncate(time.Microsecond)
	if p.ID == "" {
		p.ID = domain.NewID(domain.PrefixPlan)
	}
	if p.ExpiresAt.IsZero() {
		p.ExpiresAt = now.Add(24 * time.Hour)
	}
	p.CreatedAt = now
	_, err := r.s.writeDB.NewRaw(
		`INSERT INTO plans (id, project_id, target_type, target_id, input_hash, diff_json, warnings_json, pipeline_json, created_by, expires_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.ProjectID, p.TargetType, p.TargetID, p.InputHash, defaultJSON(p.DiffJSON, "{}"),
		defaultJSON(p.WarningsJSON, "[]"), defaultJSON(p.PipelineJSON, "{}"), p.CreatedBy, fmtTime(p.ExpiresAt), fmtTime(now),
	).Exec(ctx)
	return err
}

func (r *planRepo) Get(ctx context.Context, id string) (*domain.Plan, error) {
	var p domain.Plan
	var diffS, warnS, pipeS, expS, cAt string
	var consumed sql.NullString
	err := r.s.readDB.NewRaw(
		`SELECT id, project_id, target_type, target_id, input_hash, diff_json, warnings_json, pipeline_json, created_by, expires_at, consumed_by_op_id, created_at
		 FROM plans WHERE id = ?`, id,
	).Scan(ctx, &p.ID, &p.ProjectID, &p.TargetType, &p.TargetID, &p.InputHash, &diffS, &warnS, &pipeS, &p.CreatedBy, &expS, &consumed, &cAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFoundErr("plan", id)
	}
	if err != nil {
		return nil, err
	}
	p.DiffJSON = json.RawMessage(diffS)
	p.WarningsJSON = json.RawMessage(warnS)
	p.PipelineJSON = json.RawMessage(pipeS)
	p.ExpiresAt = parseTime(expS)
	p.CreatedAt = parseTime(cAt)
	if consumed.Valid {
		v := consumed.String
		p.ConsumedByOpID = &v
	}
	return &p, nil
}

func (r *planRepo) MarkConsumed(ctx context.Context, planID, opID string) error {
	_, err := r.s.writeDB.NewRaw(`UPDATE plans SET consumed_by_op_id = ? WHERE id = ? AND consumed_by_op_id IS NULL`, opID, planID).Exec(ctx)
	return err
}

type stateBackupRepo struct{ s *DBStore }

func (r *stateBackupRepo) Create(ctx context.Context, bk *domain.StateBackup) error {
	now := time.Now().UTC().Truncate(time.Microsecond)
	if bk.ID == "" {
		bk.ID = domain.NewID(domain.PrefixStateBackup)
	}
	bk.CreatedAt = now
	_, err := r.s.writeDB.NewRaw(
		`INSERT INTO instance_state_backups (id, instance_id, from_schema_ver, to_schema_ver, raw_state_json, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		bk.ID, bk.InstanceID, bk.FromSchemaVer, bk.ToSchemaVer, defaultJSON(bk.RawStateJSON, "{}"), fmtTime(now),
	).Exec(ctx)
	return err
}

func (r *stateBackupRepo) ListByInstance(ctx context.Context, instanceID string) ([]domain.StateBackup, error) {
	rows, err := r.s.readDB.QueryContext(ctx,
		`SELECT id, instance_id, from_schema_ver, to_schema_ver, raw_state_json, created_at
		 FROM instance_state_backups WHERE instance_id = ? ORDER BY created_at DESC`, instanceID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.StateBackup
	for rows.Next() {
		var bk domain.StateBackup
		var rawS, cAt string
		if err := rows.Scan(&bk.ID, &bk.InstanceID, &bk.FromSchemaVer, &bk.ToSchemaVer, &rawS, &cAt); err != nil {
			return nil, err
		}
		bk.RawStateJSON = json.RawMessage(rawS)
		bk.CreatedAt = parseTime(cAt)
		out = append(out, bk)
	}
	return out, rows.Err()
}

func nonNilMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

func defaultJSON(raw json.RawMessage, def string) string {
	if len(raw) == 0 {
		return def
	}
	return string(raw)
}

package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/kinstall/kinstall/internal/domain"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

const pgAuditAdvisoryLockKey int64 = 0x6b696e7374616c6c // "kinstall" (G1 §5.2)

// --- AuditRepo (Fork-free serialized SHA-256 / SM3 hash chain, G1 §5.2) ---

type auditRepo struct{ s *DBStore }

func (r *auditRepo) AppendChained(ctx context.Context, entry domain.AuditEntry, computeHash func(seq int64, ts time.Time, prevSHA, prevSM3 string) (string, string)) (int64, error) {
	now := time.Now().UTC().Truncate(time.Microsecond) // Align with PG TIMESTAMPTZ microsecond precision!

	tx, err := r.s.writeDB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	if r.s.dialect == "postgres" {
		if _, err := tx.NewRaw(`SELECT pg_advisory_xact_lock(?)`, pgAuditAdvisoryLockKey).Exec(ctx); err != nil {
			return 0, err
		}
	}

	headQuery := `SELECT last_seq, last_sha256, last_sm3 FROM audit_chain_head WHERE chain_id = 1`
	if r.s.dialect == "postgres" {
		headQuery += ` FOR UPDATE`
	}
	var lastSeq int64
	var lastSHA, lastSM3 string
	if err := tx.NewRaw(headQuery).Scan(ctx, &lastSeq, &lastSHA, &lastSM3); err != nil {
		return 0, err
	}
	nextSeq := lastSeq + 1
	recSHA, recSM3 := computeHash(nextSeq, now, lastSHA, lastSM3)
	payload := entry.PayloadJSON
	if payload == "" {
		payload = "{}"
	}
	if _, err := tx.NewRaw(
		`INSERT INTO audit_logs (seq, occurred_at, actor_id, actor_type, source_ip, project_id, action, target_type, target_id,
		 result, err_code, trace_id, payload_json, prev_sha256, record_sha256, prev_sm3, record_sm3)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		nextSeq, fmtTime(now), entry.ActorID, entry.ActorType, entry.SourceIP, entry.ProjectID,
		entry.Action, entry.TargetType, entry.TargetID, entry.Result, entry.ErrCode, entry.TraceID,
		payload, lastSHA, recSHA, lastSM3, recSM3,
	).Exec(ctx); err != nil {
		return 0, err
	}

	nextSM3Head := lastSM3
	if recSM3 != "" {
		nextSM3Head = recSM3
	}
	if _, err := tx.NewRaw(
		`UPDATE audit_chain_head SET last_seq = ?, last_sha256 = ?, last_sm3 = ? WHERE chain_id = 1`,
		nextSeq, recSHA, nextSM3Head,
	).Exec(ctx); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return nextSeq, nil
}

func (r *auditRepo) List(ctx context.Context, projectID string, afterSeq int64, limit int) ([]domain.AuditRecord, error) {
	if limit <= 0 {
		limit = 200
	}
	var rows *sql.Rows
	var err error
	if projectID != "" {
		rows, err = r.s.readDB.QueryContext(ctx,
			`SELECT seq, occurred_at, actor_id, actor_type, source_ip, project_id, action, target_type, target_id,
			 result, err_code, trace_id, payload_json, prev_sha256, record_sha256, prev_sm3, record_sm3
			 FROM audit_logs WHERE project_id = ? AND seq > ? ORDER BY seq ASC LIMIT ?`,
			projectID, afterSeq, limit)
	} else {
		rows, err = r.s.readDB.QueryContext(ctx,
			`SELECT seq, occurred_at, actor_id, actor_type, source_ip, project_id, action, target_type, target_id,
			 result, err_code, trace_id, payload_json, prev_sha256, record_sha256, prev_sm3, record_sm3
			 FROM audit_logs WHERE seq > ? ORDER BY seq ASC LIMIT ?`,
			afterSeq, limit)
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.AuditRecord
	for rows.Next() {
		var rec domain.AuditRecord
		var ts string
		if err := rows.Scan(
			&rec.Seq, &ts, &rec.Entry.ActorID, &rec.Entry.ActorType, &rec.Entry.SourceIP, &rec.Entry.ProjectID,
			&rec.Entry.Action, &rec.Entry.TargetType, &rec.Entry.TargetID, &rec.Entry.Result, &rec.Entry.ErrCode,
			&rec.Entry.TraceID, &rec.Entry.PayloadJSON, &rec.PrevSHA256, &rec.RecordSHA256, &rec.PrevSM3, &rec.RecordSM3,
		); err != nil {
			return nil, err
		}
		rec.OccurredAt = parseTime(ts)
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (r *auditRepo) GetSyslogCursor(ctx context.Context, sinkID string) (int64, error) {
	var seq int64
	err := r.s.readDB.NewRaw(`SELECT last_sent_seq FROM audit_syslog_cursor WHERE sink_id = ?`, sinkID).Scan(ctx, &seq)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return seq, err
}

func (r *auditRepo) UpdateSyslogCursor(ctx context.Context, sinkID string, seq int64) error {
	now := fmtTime(time.Now().UTC())
	_, err := r.s.writeDB.NewRaw(
		`INSERT INTO audit_syslog_cursor (sink_id, last_sent_seq, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT (sink_id) DO UPDATE SET last_sent_seq = excluded.last_sent_seq, updated_at = excluded.updated_at`,
		sinkID, seq, now,
	).Exec(ctx)
	return err
}

// --- ApprovalRepo (Dual Go + DB chk_no_self_approval enforcement, G1 §4.2) ---

type approvalRepo struct{ s *DBStore }

func (r *approvalRepo) Create(ctx context.Context, ap *domain.Approval) error {
	now := time.Now().UTC().Truncate(time.Microsecond)
	if ap.Status == "" {
		ap.Status = domain.ApprovalPending
	}
	if ap.ExpiresAt.IsZero() {
		ap.ExpiresAt = now.Add(24 * time.Hour)
	}
	ap.CreatedAt = now
	_, err := r.s.writeDB.NewRaw(
		`INSERT INTO operation_approvals (operation_id, project_id, env_name, requested_by, reason, status, expires_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		ap.OperationID, ap.ProjectID, ap.EnvName, ap.RequestedBy, ap.Reason, string(ap.Status), fmtTime(ap.ExpiresAt), fmtTime(now),
	).Exec(ctx)
	return err
}

func (r *approvalRepo) Get(ctx context.Context, opID string) (*domain.Approval, error) {
	var ap domain.Approval
	var st, expS, cAt string
	var decBy, decAt sql.NullString
	err := r.s.readDB.NewRaw(
		`SELECT operation_id, project_id, env_name, requested_by, reason, status, decided_by, decision_note, expires_at, created_at, decided_at
		 FROM operation_approvals WHERE operation_id = ?`, opID,
	).Scan(ctx, &ap.OperationID, &ap.ProjectID, &ap.EnvName, &ap.RequestedBy, &ap.Reason, &st, &decBy, &ap.DecisionNote, &expS, &cAt, &decAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFoundErr("approval", opID)
	}
	if err != nil {
		return nil, err
	}
	ap.Status = domain.ApprovalStatus(st)
	if decBy.Valid {
		v := decBy.String
		ap.DecidedBy = &v
	}
	ap.ExpiresAt, ap.CreatedAt = parseTime(expS), parseTime(cAt)
	if decAt.Valid && decAt.String != "" {
		t := parseTime(decAt.String)
		ap.DecidedAt = &t
	}
	return &ap, nil
}

func (r *approvalRepo) ListPending(ctx context.Context, projectID string) ([]domain.Approval, error) {
	rows, err := r.s.readDB.QueryContext(ctx,
		`SELECT operation_id, project_id, env_name, requested_by, reason, status, decided_by, decision_note, expires_at, created_at, decided_at
		 FROM operation_approvals WHERE project_id = ? AND status = 'pending' ORDER BY created_at DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Approval
	for rows.Next() {
		var ap domain.Approval
		var st, expS, cAt string
		var decBy, decAt sql.NullString
		if err := rows.Scan(&ap.OperationID, &ap.ProjectID, &ap.EnvName, &ap.RequestedBy, &ap.Reason, &st, &decBy, &ap.DecisionNote, &expS, &cAt, &decAt); err != nil {
			return nil, err
		}
		ap.Status = domain.ApprovalStatus(st)
		if decBy.Valid {
			v := decBy.String
			ap.DecidedBy = &v
		}
		ap.ExpiresAt, ap.CreatedAt = parseTime(expS), parseTime(cAt)
		out = append(out, ap)
	}
	return out, rows.Err()
}

func (r *approvalRepo) Decide(ctx context.Context, opID, decidedBy string, approve bool, note string) error {
	now := fmtTime(time.Now().UTC())
	tx, err := r.s.writeDB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var requestedBy, curStatus, expS string
	if err := tx.NewRaw(`SELECT requested_by, status, expires_at FROM operation_approvals WHERE operation_id = ?`, opID).
		Scan(ctx, &requestedBy, &curStatus, &expS); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return notFoundErr("approval", opID)
		}
		return err
	}
	if requestedBy == decidedBy {
		return sdk.NewError(sdk.CodeAuthForbidden, "AUTH", http.StatusForbidden, sdk.CodeAuthForbidden, false,
			fmt.Errorf("requester %s cannot self-approve operation %s", decidedBy, opID))
	}
	if curStatus != string(domain.ApprovalPending) {
		return sdk.NewError(sdk.CodeEngInvalidTransition, "ENG", http.StatusConflict, sdk.CodeEngInvalidTransition, false,
			fmt.Errorf("approval %s is already %s", opID, curStatus))
	}
	newApStatus := string(domain.ApprovalRejected)
	newOpState := string(domain.OpRejected)
	if approve {
		newApStatus = string(domain.ApprovalApproved)
		newOpState = string(domain.OpPending)
	}
	res, err := tx.NewRaw(
		`UPDATE operation_approvals SET status = ?, decided_by = ?, decision_note = ?, decided_at = ?
		 WHERE operation_id = ? AND status = 'pending' AND expires_at > ? AND requested_by <> ?`,
		newApStatus, decidedBy, note, now, opID, now, decidedBy,
	).Exec(ctx)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sdk.NewError(sdk.CodeAuthForbidden, "AUTH", http.StatusConflict, sdk.CodeAuthForbidden, false,
			fmt.Errorf("approval %s expired, already decided, or self-approval blocked", opID))
	}
	_, err = tx.NewRaw(
		`UPDATE operations SET state = ?, approved_by = ?, updated_at = ? WHERE id = ? AND state = 'awaiting_approval'`,
		newOpState, decidedBy, now, opID,
	).Exec(ctx)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// --- MediaPolicyRepo (Global / Project / Instance 3-tier scope, ADR-077) ---

type mediaPolicyRepo struct{ s *DBStore }

func (r *mediaPolicyRepo) Create(ctx context.Context, mp *domain.MediaPolicy) error {
	now := time.Now().UTC().Truncate(time.Microsecond)
	if mp.ID == "" {
		mp.ID = domain.NewID(domain.PrefixMediaPolicy)
	}
	if mp.Scope == "" {
		mp.Scope = "project"
	}
	if mp.DeliveryMode == "" {
		mp.DeliveryMode = "auto"
	}
	if mp.Version <= 0 {
		mp.Version = 1
	}
	srcJ, _ := json.Marshal(mp.Sources)
	mp.CreatedAt, mp.UpdatedAt = now, now
	_, err := r.s.writeDB.NewRaw(
		`INSERT INTO media_policies (id, scope, project_id, instance_id, name, is_default, delivery_mode, offline_strict, strict_local, sources_json, last_probe_json, version, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		mp.ID, mp.Scope, mp.ProjectID, mp.InstanceID, mp.Name, mp.IsDefault, mp.DeliveryMode, mp.OfflineStrict, mp.StrictLocal,
		string(srcJ), defaultJSON(mp.LastProbeJSON, "{}"), mp.Version, fmtTime(now), fmtTime(now),
	).Exec(ctx)
	return err
}

func scanMediaPolicy(scan func(dest ...any) error) (*domain.MediaPolicy, error) {
	var mp domain.MediaPolicy
	var projID, instID, probedAt sql.NullString
	var srcS, probeS, cAt, uAt string
	err := scan(&mp.ID, &mp.Scope, &projID, &instID, &mp.Name, &mp.IsDefault, &mp.DeliveryMode, &mp.OfflineStrict, &mp.StrictLocal,
		&srcS, &probeS, &probedAt, &mp.Version, &cAt, &uAt)
	if err != nil {
		return nil, err
	}
	if projID.Valid {
		v := projID.String
		mp.ProjectID = &v
	}
	if instID.Valid {
		v := instID.String
		mp.InstanceID = &v
	}
	_ = json.Unmarshal([]byte(srcS), &mp.Sources)
	mp.LastProbeJSON = json.RawMessage(probeS)
	if probedAt.Valid && probedAt.String != "" {
		t := parseTime(probedAt.String)
		mp.LastProbedAt = &t
	}
	mp.CreatedAt, mp.UpdatedAt = parseTime(cAt), parseTime(uAt)
	return &mp, nil
}

const mediaPolicyCols = `id, scope, project_id, instance_id, name, is_default, delivery_mode, offline_strict, strict_local,
	sources_json, last_probe_json, last_probed_at, version, created_at, updated_at`

func (r *mediaPolicyRepo) Get(ctx context.Context, id string) (*domain.MediaPolicy, error) {
	row := r.s.readDB.QueryRowContext(ctx, `SELECT `+mediaPolicyCols+` FROM media_policies WHERE id = ?`, id)
	mp, err := scanMediaPolicy(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFoundErr("media_policy", id)
	}
	return mp, err
}

func (r *mediaPolicyRepo) List(ctx context.Context, projectID string) ([]domain.MediaPolicy, error) {
	rows, err := r.s.readDB.QueryContext(ctx,
		`SELECT `+mediaPolicyCols+` FROM media_policies WHERE project_id = ? OR scope = 'global' ORDER BY scope DESC, is_default DESC, name ASC`, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.MediaPolicy
	for rows.Next() {
		mp, err := scanMediaPolicy(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, *mp)
	}
	return out, rows.Err()
}

// ResolveEffective applies 3-tier precedence: instance > project default > global default (D3 §7).
func (r *mediaPolicyRepo) ResolveEffective(ctx context.Context, projectID, instanceID string) (*domain.MediaPolicy, error) {
	if instanceID != "" {
		row := r.s.readDB.QueryRowContext(ctx, `SELECT `+mediaPolicyCols+` FROM media_policies WHERE scope = 'instance' AND instance_id = ? LIMIT 1`, instanceID)
		if mp, err := scanMediaPolicy(row.Scan); err == nil {
			return mp, nil
		}
	}
	if projectID != "" {
		row := r.s.readDB.QueryRowContext(ctx, `SELECT `+mediaPolicyCols+` FROM media_policies WHERE scope = 'project' AND project_id = ? ORDER BY is_default DESC, created_at ASC LIMIT 1`, projectID)
		if mp, err := scanMediaPolicy(row.Scan); err == nil {
			return mp, nil
		}
	}
	row := r.s.readDB.QueryRowContext(ctx, `SELECT `+mediaPolicyCols+` FROM media_policies WHERE scope = 'global' ORDER BY is_default DESC, created_at ASC LIMIT 1`)
	mp, err := scanMediaPolicy(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		// Safe built-in fallback: local -> online
		return &domain.MediaPolicy{
			ID:           "mp_builtin_default",
			Scope:        "global",
			Name:         "default",
			DeliveryMode: "auto",
			Sources: []domain.MediaSourceConfig{
				{Type: "local", Priority: 1, Internal: true, Roots: []string{"/var/lib/kinstall/artifacts"}},
				{Type: "online", Priority: 100, Internal: false},
			},
		}, nil
	}
	return mp, err
}

func (r *mediaPolicyRepo) Update(ctx context.Context, mp *domain.MediaPolicy) error {
	now := time.Now().UTC().Truncate(time.Microsecond)
	srcJ, _ := json.Marshal(mp.Sources)
	var probedAt any
	if mp.LastProbedAt != nil {
		probedAt = fmtTime(*mp.LastProbedAt)
	}
	res, err := r.s.writeDB.NewRaw(
		`UPDATE media_policies SET name = ?, is_default = ?, delivery_mode = ?, offline_strict = ?, strict_local = ?,
		 sources_json = ?, last_probe_json = ?, last_probed_at = ?, version = version + 1, updated_at = ?
		 WHERE id = ? AND version = ?`,
		mp.Name, mp.IsDefault, mp.DeliveryMode, mp.OfflineStrict, mp.StrictLocal,
		string(srcJ), defaultJSON(mp.LastProbeJSON, "{}"), probedAt, fmtTime(now), mp.ID, mp.Version,
	).Exec(ctx)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sdk.NewError(sdk.CodeAPIETagConflict, "API", http.StatusPreconditionFailed, sdk.CodeAPIETagConflict, true, fmt.Errorf("media_policy %s version conflict", mp.ID))
	}
	mp.Version++
	mp.UpdatedAt = now
	return nil
}

func (r *mediaPolicyRepo) Delete(ctx context.Context, id string) error {
	_, err := r.s.writeDB.NewRaw(`DELETE FROM media_policies WHERE id = ?`, id).Exec(ctx)
	return err
}

// --- AuthRepo (Users, Sessions, Scoped API Tokens, G1 §4.1) ---

type authRepo struct{ s *DBStore }

func (r *authRepo) CreateUser(ctx context.Context, u *domain.User) error {
	now := time.Now().UTC().Truncate(time.Microsecond)
	if u.ID == "" {
		u.ID = domain.NewID(domain.PrefixUser)
	}
	if u.AuthSource == "" {
		u.AuthSource = "local"
	}
	rbJ, _ := json.Marshal(u.RoleBindings)
	u.CreatedAt, u.UpdatedAt = now, now
	_, err := r.s.writeDB.NewRaw(
		`INSERT INTO users (id, username, display_name, auth_source, oidc_issuer, oidc_subject, password_hash, role_bindings_json, failed_login_attempts, disabled, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		u.ID, u.Username, u.DisplayName, u.AuthSource, u.OIDCIssuer, u.OIDCSubject, u.PasswordHash, string(rbJ), u.FailedLoginAttempts, u.Disabled, fmtTime(now), fmtTime(now),
	).Exec(ctx)
	return err
}

func scanUser(scan func(dest ...any) error) (*domain.User, error) {
	var u domain.User
	var rbS, cAt, uAt string
	var locked sql.NullString
	err := scan(&u.ID, &u.Username, &u.DisplayName, &u.AuthSource, &u.OIDCIssuer, &u.OIDCSubject,
		&u.PasswordHash, &rbS, &u.FailedLoginAttempts, &locked, &u.Disabled, &cAt, &uAt)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(rbS), &u.RoleBindings)
	if locked.Valid && locked.String != "" {
		t := parseTime(locked.String)
		u.LockedUntil = &t
	}
	u.CreatedAt, u.UpdatedAt = parseTime(cAt), parseTime(uAt)
	return &u, nil
}

const userCols = `id, username, display_name, auth_source, oidc_issuer, oidc_subject, password_hash, role_bindings_json, failed_login_attempts, locked_until, disabled, created_at, updated_at`

func (r *authRepo) GetUserByID(ctx context.Context, id string) (*domain.User, error) {
	row := r.s.readDB.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE id = ?`, id)
	u, err := scanUser(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFoundErr("user", id)
	}
	return u, err
}

func (r *authRepo) GetUserByUsername(ctx context.Context, username string) (*domain.User, error) {
	row := r.s.readDB.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE username = ?`, username)
	u, err := scanUser(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFoundErr("user", username)
	}
	return u, err
}

func (r *authRepo) GetUserByOIDC(ctx context.Context, issuer, subject string) (*domain.User, error) {
	row := r.s.readDB.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE auth_source = 'oidc' AND oidc_issuer = ? AND oidc_subject = ?`, issuer, subject)
	u, err := scanUser(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFoundErr("user", subject)
	}
	return u, err
}

func (r *authRepo) ListUsers(ctx context.Context) ([]domain.User, error) {
	rows, err := r.s.readDB.QueryContext(ctx, `SELECT `+userCols+` FROM users ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.User
	for rows.Next() {
		u, err := scanUser(rows.Scan)
		if err != nil {
			return nil, err
		}
		u.PasswordHash = "" // never leak password hashes
		out = append(out, *u)
	}
	return out, rows.Err()
}

func (r *authRepo) RecordLoginFailure(ctx context.Context, userID string, maxAttempts int, lockDuration time.Duration) (int, *time.Time, error) {
	tx, err := r.s.writeDB.BeginTx(ctx, nil)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var cur int
	if err := tx.NewRaw(`SELECT failed_login_attempts FROM users WHERE id = ?`, userID).Scan(ctx, &cur); err != nil {
		return 0, nil, err
	}
	next := cur + 1
	now := time.Now().UTC().Truncate(time.Microsecond)
	var lockedUntil *time.Time
	var lockedArg any
	if next >= maxAttempts {
		t := now.Add(lockDuration)
		lockedUntil = &t
		lockedArg = fmtTime(t)
	}
	if _, err := tx.NewRaw(`UPDATE users SET failed_login_attempts = ?, locked_until = ?, updated_at = ? WHERE id = ?`,
		next, lockedArg, fmtTime(now), userID).Exec(ctx); err != nil {
		return 0, nil, err
	}
	if err := tx.Commit(); err != nil {
		return 0, nil, err
	}
	return next, lockedUntil, nil
}

func (r *authRepo) ResetLoginFailures(ctx context.Context, userID string) error {
	_, err := r.s.writeDB.NewRaw(`UPDATE users SET failed_login_attempts = 0, locked_until = NULL, updated_at = ? WHERE id = ?`,
		fmtTime(time.Now().UTC()), userID).Exec(ctx)
	return err
}

func (r *authRepo) CreateSession(ctx context.Context, sess *domain.UserSession) error {
	now := time.Now().UTC().Truncate(time.Microsecond)
	if sess.ID == "" {
		sess.ID = domain.NewID(domain.PrefixSession)
	}
	sess.CreatedAt = now
	_, err := r.s.writeDB.NewRaw(
		`INSERT INTO user_sessions (id, user_id, token_hash, source_ip, user_agent, expires_at, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		sess.ID, sess.UserID, sess.TokenHash, sess.SourceIP, sess.UserAgent, fmtTime(sess.ExpiresAt), fmtTime(now),
	).Exec(ctx)
	return err
}

func (r *authRepo) GetSessionByTokenHash(ctx context.Context, hash string) (*domain.UserSession, error) {
	var sess domain.UserSession
	var expS, cAt string
	var revAt sql.NullString
	err := r.s.readDB.NewRaw(
		`SELECT id, user_id, token_hash, source_ip, user_agent, expires_at, revoked_at, created_at FROM user_sessions WHERE token_hash = ?`, hash,
	).Scan(ctx, &sess.ID, &sess.UserID, &sess.TokenHash, &sess.SourceIP, &sess.UserAgent, &expS, &revAt, &cAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFoundErr("session", "hash")
	}
	if err != nil {
		return nil, err
	}
	sess.ExpiresAt, sess.CreatedAt = parseTime(expS), parseTime(cAt)
	if revAt.Valid && revAt.String != "" {
		t := parseTime(revAt.String)
		sess.RevokedAt = &t
	}
	return &sess, nil
}

func (r *authRepo) RevokeSession(ctx context.Context, id string) error {
	_, err := r.s.writeDB.NewRaw(`UPDATE user_sessions SET revoked_at = ? WHERE id = ?`, fmtTime(time.Now().UTC()), id).Exec(ctx)
	return err
}

func (r *authRepo) CreateAPIToken(ctx context.Context, tok *domain.APIToken) error {
	now := time.Now().UTC().Truncate(time.Microsecond)
	if tok.ID == "" {
		tok.ID = domain.NewID(domain.PrefixAPIToken)
	}
	scopesJ, _ := json.Marshal(tok.Scopes)
	tok.CreatedAt = now
	_, err := r.s.writeDB.NewRaw(
		`INSERT INTO api_tokens (id, user_id, project_id, name, token_prefix, token_hash, scopes_json, expires_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		tok.ID, tok.UserID, tok.ProjectID, tok.Name, tok.TokenPrefix, tok.TokenHash, string(scopesJ), fmtTime(tok.ExpiresAt), fmtTime(now),
	).Exec(ctx)
	return err
}

func (r *authRepo) GetAPITokenByHash(ctx context.Context, hash string) (*domain.APIToken, error) {
	var tok domain.APIToken
	var projID, lastUsed, revAt sql.NullString
	var scopesS, expS, cAt string
	err := r.s.readDB.NewRaw(
		`SELECT id, user_id, project_id, name, token_prefix, token_hash, scopes_json, expires_at, last_used_at, revoked_at, created_at
		 FROM api_tokens WHERE token_hash = ?`, hash,
	).Scan(ctx, &tok.ID, &tok.UserID, &projID, &tok.Name, &tok.TokenPrefix, &tok.TokenHash, &scopesS, &expS, &lastUsed, &revAt, &cAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFoundErr("api_token", "hash")
	}
	if err != nil {
		return nil, err
	}
	if projID.Valid {
		v := projID.String
		tok.ProjectID = &v
	}
	_ = json.Unmarshal([]byte(scopesS), &tok.Scopes)
	tok.ExpiresAt, tok.CreatedAt = parseTime(expS), parseTime(cAt)
	if lastUsed.Valid && lastUsed.String != "" {
		t := parseTime(lastUsed.String)
		tok.LastUsedAt = &t
	}
	if revAt.Valid && revAt.String != "" {
		t := parseTime(revAt.String)
		tok.RevokedAt = &t
	}
	return &tok, nil
}

func (r *authRepo) ListAPITokens(ctx context.Context, userID string) ([]domain.APIToken, error) {
	rows, err := r.s.readDB.QueryContext(ctx,
		`SELECT id, user_id, project_id, name, token_prefix, token_hash, scopes_json, expires_at, last_used_at, revoked_at, created_at
		 FROM api_tokens WHERE user_id = ? ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.APIToken
	for rows.Next() {
		var tok domain.APIToken
		var projID, lastUsed, revAt sql.NullString
		var scopesS, expS, cAt string
		if err := rows.Scan(&tok.ID, &tok.UserID, &projID, &tok.Name, &tok.TokenPrefix, &tok.TokenHash, &scopesS, &expS, &lastUsed, &revAt, &cAt); err != nil {
			return nil, err
		}
		if projID.Valid {
			v := projID.String
			tok.ProjectID = &v
		}
		_ = json.Unmarshal([]byte(scopesS), &tok.Scopes)
		tok.ExpiresAt, tok.CreatedAt = parseTime(expS), parseTime(cAt)
		out = append(out, tok)
	}
	return out, rows.Err()
}

func (r *authRepo) RevokeAPIToken(ctx context.Context, id string) error {
	_, err := r.s.writeDB.NewRaw(`UPDATE api_tokens SET revoked_at = ? WHERE id = ?`, fmtTime(time.Now().UTC()), id).Exec(ctx)
	return err
}

// --- IdempotencyRepo (24h dedup with PK (project_id, actor_id, key_hash), G1 §2.2) ---

type idempotencyRepo struct{ s *DBStore }

func (r *idempotencyRepo) Acquire(ctx context.Context, rec domain.IdempotencyRecord) (*domain.IdempotencyRecord, bool, error) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	if rec.ExpiresAt.IsZero() {
		rec.ExpiresAt = now.Add(24 * time.Hour)
	}
	tx, err := r.s.writeDB.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = tx.Rollback() }()

	// Evict expired entry for same key before attempting insert
	_, _ = tx.NewRaw(`DELETE FROM idempotency_keys WHERE project_id = ? AND actor_id = ? AND key_hash = ? AND expires_at < ?`,
		rec.ProjectID, rec.ActorID, rec.KeyHash, fmtTime(now)).Exec(ctx)

	res, err := tx.NewRaw(`
		INSERT INTO idempotency_keys (project_id, actor_id, key_hash, endpoint, request_hash, status, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?, 'in_progress', ?, ?)
		ON CONFLICT (project_id, actor_id, key_hash) DO NOTHING`,
		rec.ProjectID, rec.ActorID, rec.KeyHash, rec.Endpoint, rec.RequestHash, fmtTime(rec.ExpiresAt), fmtTime(now),
	).Exec(ctx)
	if err != nil {
		return nil, false, err
	}
	n, _ := res.RowsAffected()
	if n == 1 {
		if err := tx.Commit(); err != nil {
			return nil, false, err
		}
		rec.Status = "in_progress"
		rec.CreatedAt = now
		return &rec, true, nil
	}

	var ex domain.IdempotencyRecord
	var respCode sql.NullInt64
	var respBody sql.NullString
	var expS, cAt string
	err = tx.NewRaw(
		`SELECT project_id, actor_id, key_hash, endpoint, request_hash, status, response_code, response_body, expires_at, created_at
		 FROM idempotency_keys WHERE project_id = ? AND actor_id = ? AND key_hash = ?`,
		rec.ProjectID, rec.ActorID, rec.KeyHash,
	).Scan(ctx, &ex.ProjectID, &ex.ActorID, &ex.KeyHash, &ex.Endpoint, &ex.RequestHash, &ex.Status, &respCode, &respBody, &expS, &cAt)
	if err != nil {
		return nil, false, err
	}
	if respCode.Valid {
		ex.ResponseCode = int(respCode.Int64)
	}
	if respBody.Valid {
		ex.ResponseBody = respBody.String
	}
	ex.ExpiresAt, ex.CreatedAt = parseTime(expS), parseTime(cAt)
	_ = tx.Commit()
	return &ex, false, nil
}

func (r *idempotencyRepo) Complete(ctx context.Context, projectID, actorID, keyHash string, status int, body string) error {
	_, err := r.s.writeDB.NewRaw(
		`UPDATE idempotency_keys SET status = 'completed', response_code = ?, response_body = ?
		 WHERE project_id = ? AND actor_id = ? AND key_hash = ?`,
		status, body, projectID, actorID, keyHash,
	).Exec(ctx)
	return err
}

func (r *idempotencyRepo) Abort(ctx context.Context, projectID, actorID, keyHash string) error {
	_, err := r.s.writeDB.NewRaw(
		`DELETE FROM idempotency_keys WHERE project_id = ? AND actor_id = ? AND key_hash = ? AND status = 'in_progress'`,
		projectID, actorID, keyHash,
	).Exec(ctx)
	return err
}

// --- NotificationRepo & KubeconfigRepo ---

type notificationRepo struct{ s *DBStore }

func (r *notificationRepo) CreateChannel(ctx context.Context, ch *domain.NotificationChannel) error {
	now := time.Now().UTC().Truncate(time.Microsecond)
	if ch.ID == "" {
		ch.ID = domain.NewID(domain.PrefixNotifyChan)
	}
	evJ, _ := json.Marshal(ch.Events)
	ch.CreatedAt, ch.UpdatedAt = now, now
	_, err := r.s.writeDB.NewRaw(
		`INSERT INTO notification_channels (id, project_id, name, kind, config_json, credential_ref, events_json, enabled, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ch.ID, ch.ProjectID, ch.Name, ch.Kind, defaultJSON(ch.ConfigJSON, "{}"), ch.CredentialRef, string(evJ), ch.Enabled, fmtTime(now), fmtTime(now),
	).Exec(ctx)
	return err
}

func (r *notificationRepo) ListChannels(ctx context.Context, projectID string) ([]domain.NotificationChannel, error) {
	rows, err := r.s.readDB.QueryContext(ctx,
		`SELECT id, project_id, name, kind, config_json, credential_ref, events_json, enabled, created_at, updated_at
		 FROM notification_channels WHERE project_id = ? ORDER BY name ASC`, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.NotificationChannel
	for rows.Next() {
		var ch domain.NotificationChannel
		var cfgS, evS, cAt, uAt string
		var cred sql.NullString
		if err := rows.Scan(&ch.ID, &ch.ProjectID, &ch.Name, &ch.Kind, &cfgS, &cred, &evS, &ch.Enabled, &cAt, &uAt); err != nil {
			return nil, err
		}
		ch.ConfigJSON = json.RawMessage(cfgS)
		if cred.Valid {
			v := cred.String
			ch.CredentialRef = &v
		}
		_ = json.Unmarshal([]byte(evS), &ch.Events)
		ch.CreatedAt, ch.UpdatedAt = parseTime(cAt), parseTime(uAt)
		out = append(out, ch)
	}
	return out, rows.Err()
}

func (r *notificationRepo) UpsertDelivery(ctx context.Context, d *domain.NotificationDelivery) (bool, error) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	tx, err := r.s.writeDB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	var existingID string
	err = tx.NewRaw(
		`SELECT id FROM notification_deliveries WHERE channel_id = ? AND dedup_key = ? AND window_end_at > ? LIMIT 1`,
		d.ChannelID, d.DedupKey, fmtTime(now),
	).Scan(ctx, &existingID)
	if err == nil && existingID != "" {
		if _, err := tx.NewRaw(`UPDATE notification_deliveries SET event_count = event_count + 1 WHERE id = ?`, existingID).Exec(ctx); err != nil {
			return false, err
		}
		d.ID = existingID
		return true, tx.Commit()
	}
	if d.ID == "" {
		d.ID = domain.NewID(domain.PrefixNotifyDeliv)
	}
	d.CreatedAt = now
	if _, err := tx.NewRaw(
		`INSERT INTO notification_deliveries (id, channel_id, dedup_key, event_count, status, attempt, last_error, window_end_at, created_at)
		 VALUES (?, ?, ?, 1, 'pending', 0, '', ?, ?)`,
		d.ID, d.ChannelID, d.DedupKey, fmtTime(d.WindowEndAt), fmtTime(now),
	).Exec(ctx); err != nil {
		return false, err
	}
	return false, tx.Commit()
}

func (r *notificationRepo) MarkDelivery(ctx context.Context, id, status, lastErr string) error {
	var sentAt any
	if status == "sent" {
		sentAt = fmtTime(time.Now().UTC())
	}
	_, err := r.s.writeDB.NewRaw(
		`UPDATE notification_deliveries SET status = ?, attempt = attempt + 1, last_error = ?, sent_at = COALESCE(?, sent_at) WHERE id = ?`,
		status, lastErr, sentAt, id,
	).Exec(ctx)
	return err
}

type kubeconfigRepo struct{ s *DBStore }

func (r *kubeconfigRepo) Create(ctx context.Context, kc *domain.ClusterKubeconfig) error {
	now := time.Now().UTC().Truncate(time.Microsecond)
	if kc.ID == "" {
		kc.ID = domain.NewID(domain.PrefixKubeconfig)
	}
	nsJ, _ := json.Marshal(kc.Namespaces)
	kc.CreatedAt = now
	_, err := r.s.writeDB.NewRaw(
		`INSERT INTO cluster_kubeconfigs (id, instance_id, user_id, k8s_role, namespaces_json, cert_serial, expires_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		kc.ID, kc.InstanceID, kc.UserID, kc.K8sRole, string(nsJ), kc.CertSerial, fmtTime(kc.ExpiresAt), fmtTime(now),
	).Exec(ctx)
	return err
}

func (r *kubeconfigRepo) Get(ctx context.Context, id string) (*domain.ClusterKubeconfig, error) {
	var kc domain.ClusterKubeconfig
	var nsS, expS, cAt string
	var revAt, revBy sql.NullString
	err := r.s.readDB.NewRaw(
		`SELECT id, instance_id, user_id, k8s_role, namespaces_json, cert_serial, expires_at, revoked_at, revoked_by, created_at
		 FROM cluster_kubeconfigs WHERE id = ?`, id,
	).Scan(ctx, &kc.ID, &kc.InstanceID, &kc.UserID, &kc.K8sRole, &nsS, &kc.CertSerial, &expS, &revAt, &revBy, &cAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFoundErr("kubeconfig", id)
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(nsS), &kc.Namespaces)
	kc.ExpiresAt, kc.CreatedAt = parseTime(expS), parseTime(cAt)
	if revAt.Valid && revAt.String != "" {
		t := parseTime(revAt.String)
		kc.RevokedAt = &t
	}
	if revBy.Valid {
		v := revBy.String
		kc.RevokedBy = &v
	}
	return &kc, nil
}

func (r *kubeconfigRepo) ListByInstance(ctx context.Context, instanceID string) ([]domain.ClusterKubeconfig, error) {
	rows, err := r.s.readDB.QueryContext(ctx,
		`SELECT id, instance_id, user_id, k8s_role, namespaces_json, cert_serial, expires_at, revoked_at, revoked_by, created_at
		 FROM cluster_kubeconfigs WHERE instance_id = ? ORDER BY created_at DESC`, instanceID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.ClusterKubeconfig
	for rows.Next() {
		var kc domain.ClusterKubeconfig
		var nsS, expS, cAt string
		var revAt, revBy sql.NullString
		if err := rows.Scan(&kc.ID, &kc.InstanceID, &kc.UserID, &kc.K8sRole, &nsS, &kc.CertSerial, &expS, &revAt, &revBy, &cAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(nsS), &kc.Namespaces)
		kc.ExpiresAt, kc.CreatedAt = parseTime(expS), parseTime(cAt)
		if revAt.Valid && revAt.String != "" {
			t := parseTime(revAt.String)
			kc.RevokedAt = &t
		}
		if revBy.Valid {
			v := revBy.String
			kc.RevokedBy = &v
		}
		out = append(out, kc)
	}
	return out, rows.Err()
}

func (r *kubeconfigRepo) Revoke(ctx context.Context, id, revokedBy string) error {
	_, err := r.s.writeDB.NewRaw(
		`UPDATE cluster_kubeconfigs SET revoked_at = ?, revoked_by = ? WHERE id = ? AND revoked_at IS NULL`,
		fmtTime(time.Now().UTC()), strings.TrimSpace(revokedBy), id,
	).Exec(ctx)
	return err
}

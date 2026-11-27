package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/internal/ports"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// --- OperationRepo (Fencing Token & Lease Takeover, D2 §2.1) ---

type operationRepo struct{ s *DBStore }

func (r *operationRepo) Create(ctx context.Context, op *domain.Operation) error {
	now := time.Now().UTC().Truncate(time.Microsecond)
	if op.ID == "" {
		op.ID = domain.NewID(domain.PrefixOperation)
	}
	if op.State == "" {
		op.State = domain.OpPending
	}
	op.CreatedAt, op.UpdatedAt = now, now
	_, err := r.s.writeDB.NewRaw(
		`INSERT INTO operations (id, project_id, target_type, target_id, action, state, idempotency_key, requested_by, approved_by,
		 pipeline_json, fencing_token, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		op.ID, op.ProjectID, op.TargetType, op.TargetID, op.Action, string(op.State), op.IdempotencyKey, op.RequestedBy, op.ApprovedBy,
		defaultJSON(op.PipelineJSON, "{}"), op.FencingToken, fmtTime(now), fmtTime(now),
	).Exec(ctx)
	return err
}

const opSelectCols = `id, project_id, target_type, target_id, action, state, idempotency_key, requested_by, approved_by,
	pipeline_json, lease_owner, lease_expires_at, fencing_token, error_code, error_message, started_at, finished_at, created_at, updated_at`

func scanOperationRow(scan func(dest ...any) error) (*domain.Operation, error) {
	var op domain.Operation
	var state, pipeS, cAt, uAt string
	var idem, appBy, owner, leaseExp, errCode, errMsg, startedAt, finAt sql.NullString
	err := scan(
		&op.ID, &op.ProjectID, &op.TargetType, &op.TargetID, &op.Action, &state, &idem, &op.RequestedBy, &appBy,
		&pipeS, &owner, &leaseExp, &op.FencingToken, &errCode, &errMsg, &startedAt, &finAt, &cAt, &uAt,
	)
	if err != nil {
		return nil, err
	}
	op.State = domain.OpState(state)
	op.PipelineJSON = json.RawMessage(pipeS)
	if idem.Valid {
		v := idem.String
		op.IdempotencyKey = &v
	}
	if appBy.Valid {
		v := appBy.String
		op.ApprovedBy = &v
	}
	if owner.Valid {
		v := owner.String
		op.LeaseOwner = &v
	}
	if leaseExp.Valid && leaseExp.String != "" {
		t := parseTime(leaseExp.String)
		op.LeaseExpiresAt = &t
	}
	if errCode.Valid {
		v := errCode.String
		op.ErrorCode = &v
	}
	if errMsg.Valid {
		v := errMsg.String
		op.ErrorMessage = &v
	}
	if startedAt.Valid && startedAt.String != "" {
		t := parseTime(startedAt.String)
		op.StartedAt = &t
	}
	if finAt.Valid && finAt.String != "" {
		t := parseTime(finAt.String)
		op.FinishedAt = &t
	}
	op.CreatedAt, op.UpdatedAt = parseTime(cAt), parseTime(uAt)
	return &op, nil
}

func (r *operationRepo) Get(ctx context.Context, id string) (*domain.Operation, error) {
	row := r.s.readDB.QueryRowContext(ctx, `SELECT `+opSelectCols+` FROM operations WHERE id = ?`, id)
	op, err := scanOperationRow(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFoundErr("operation", id)
	}
	return op, err
}

func (r *operationRepo) ListByProject(ctx context.Context, projectID string, limit int) ([]domain.Operation, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.s.readDB.QueryContext(ctx,
		`SELECT `+opSelectCols+` FROM operations WHERE project_id = ? ORDER BY created_at DESC LIMIT ?`, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Operation
	for rows.Next() {
		op, err := scanOperationRow(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, *op)
	}
	return out, rows.Err()
}

// ClaimNext atomically claims one pending or lease-expired running Operation and increments fencing_token (D2 §2.1).
func (r *operationRepo) ClaimNext(ctx context.Context, workerID string, leaseTTL time.Duration) (*domain.Operation, error) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	exp := now.Add(leaseTTL)

	if r.s.dialect == "postgres" {
		row := r.s.writeDB.QueryRowContext(ctx, `
			WITH candidate AS (
				SELECT id FROM operations
				WHERE (state = 'pending')
				   OR (state = 'running' AND lease_expires_at < ?)
				ORDER BY created_at ASC
				FOR UPDATE SKIP LOCKED
				LIMIT 1
			)
			UPDATE operations o
			SET state            = 'running',
			    lease_owner      = ?,
			    lease_expires_at = ?,
			    fencing_token    = o.fencing_token + 1,
			    started_at       = COALESCE(o.started_at, ?),
			    updated_at       = ?
			FROM candidate c
			WHERE o.id = c.id
			RETURNING o.id`, fmtTime(now), workerID, fmtTime(exp), fmtTime(now), fmtTime(now))
		var id string
		if err := row.Scan(&id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, nil
			}
			return nil, err
		}
		return r.Get(ctx, id)
	}

	// SQLite dialect: BEGIN IMMEDIATE single-writer transaction
	tx, err := r.s.writeDB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var candID string
	err = tx.NewRaw(`
		SELECT id FROM operations
		WHERE (state = 'pending')
		   OR (state = 'running' AND lease_expires_at < ?)
		ORDER BY created_at ASC
		LIMIT 1`, fmtTime(now),
	).Scan(ctx, &candID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	_, err = tx.NewRaw(`
		UPDATE operations
		SET state = 'running',
		    lease_owner = ?,
		    lease_expires_at = ?,
		    fencing_token = fencing_token + 1,
		    started_at = COALESCE(started_at, ?),
		    updated_at = ?
		WHERE id = ?`, workerID, fmtTime(exp), fmtTime(now), fmtTime(now), candID,
	).Exec(ctx)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.Get(ctx, candID)
}

func (r *operationRepo) RenewLease(ctx context.Context, opID, workerID string, fencing int64, leaseTTL time.Duration) (bool, error) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	exp := now.Add(leaseTTL)
	res, err := r.s.writeDB.NewRaw(
		`UPDATE operations SET lease_expires_at = ?, updated_at = ?
		 WHERE id = ? AND lease_owner = ? AND fencing_token = ? AND state = 'running'`,
		fmtTime(exp), fmtTime(now), opID, workerID, fencing,
	).Exec(ctx)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

func (r *operationRepo) Finish(ctx context.Context, opID, workerID string, fencing int64, state domain.OpState, errCode, errMsg string) (bool, error) {
	now := fmtTime(time.Now().UTC())
	var ec, em any
	if errCode != "" {
		ec = errCode
	}
	if errMsg != "" {
		em = errMsg
	}
	res, err := r.s.writeDB.NewRaw(
		`UPDATE operations SET state = ?, error_code = ?, error_message = ?, finished_at = ?, updated_at = ?, lease_expires_at = NULL
		 WHERE id = ? AND lease_owner = ? AND fencing_token = ?`,
		string(state), ec, em, now, now, opID, workerID, fencing,
	).Exec(ctx)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

func (r *operationRepo) TransitionState(ctx context.Context, opID string, from, to domain.OpState, approvedBy *string) (bool, error) {
	if !from.CanTransitionTo(to) {
		return false, sdk.NewError(sdk.CodeEngInvalidTransition, "ENG", http.StatusConflict, sdk.CodeEngInvalidTransition, false,
			fmt.Errorf("illegal op state transition %s -> %s", from, to))
	}
	now := fmtTime(time.Now().UTC())
	res, err := r.s.writeDB.NewRaw(
		`UPDATE operations SET state = ?, approved_by = COALESCE(?, approved_by), updated_at = ?
		 WHERE id = ? AND state = ?`,
		string(to), approvedBy, now, opID, string(from),
	).Exec(ctx)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

func (r *operationRepo) ReleaseLease(ctx context.Context, opID, workerID string, fencing int64) error {
	now := fmtTime(time.Now().UTC().Add(-time.Second))
	_, err := r.s.writeDB.NewRaw(
		`UPDATE operations SET lease_expires_at = ? WHERE id = ? AND lease_owner = ? AND fencing_token = ? AND state = 'running'`,
		now, opID, workerID, fencing,
	).Exec(ctx)
	return err
}

// --- TaskRunRepo (Fencing-guarded writes preventing zombie overwrites, D2 §2.1 #3) ---

type taskRunRepo struct{ s *DBStore }

func (r *taskRunRepo) Upsert(ctx context.Context, tr *domain.TaskRun) error {
	if tr.ID == "" {
		tr.ID = domain.NewID(domain.PrefixTaskRun)
	}
	var started, finished any
	if tr.StartedAt != nil {
		started = fmtTime(*tr.StartedAt)
	}
	if tr.FinishedAt != nil {
		finished = fmtTime(*tr.FinishedAt)
	}
	var driftJ, backupJ any
	if len(tr.DriftSummaryJSON) > 0 {
		driftJ = string(tr.DriftSummaryJSON)
	}
	if len(tr.BackupRefJSON) > 0 {
		backupJ = string(tr.BackupRefJSON)
	}
	_, err := r.s.writeDB.NewRaw(`
		INSERT INTO task_runs (id, operation_id, phase_idx, step_key, host_id, task_type, params_digest, state, attempt, fencing_token,
		 remote_run_dir, remote_pgid, remote_log_offset, drift_summary_json, backup_ref_json, error_code, error_detail, started_at, finished_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (operation_id, phase_idx, step_key) DO UPDATE SET
		 params_digest = excluded.params_digest,
		 state = excluded.state, attempt = excluded.attempt, fencing_token = excluded.fencing_token,
		 remote_run_dir = excluded.remote_run_dir, remote_pgid = excluded.remote_pgid, remote_log_offset = excluded.remote_log_offset,
		 drift_summary_json = excluded.drift_summary_json, backup_ref_json = excluded.backup_ref_json,
		 error_code = excluded.error_code, error_detail = excluded.error_detail,
		 started_at = excluded.started_at, finished_at = excluded.finished_at`,
		tr.ID, tr.OperationID, tr.PhaseIdx, tr.StepKey, tr.HostID, tr.TaskType, tr.ParamsDigest, string(tr.State), tr.Attempt, tr.FencingToken,
		tr.RemoteRunDir, tr.RemotePGID, tr.RemoteLogOffset, driftJ, backupJ, tr.ErrorCode, tr.ErrorDetail, started, finished,
	).Exec(ctx)
	return err
}

func (r *taskRunRepo) ListByOperation(ctx context.Context, opID string) ([]domain.TaskRun, error) {
	rows, err := r.s.readDB.QueryContext(ctx,
		`SELECT id, operation_id, phase_idx, step_key, host_id, task_type, params_digest, state, attempt, fencing_token,
		 remote_run_dir, remote_pgid, remote_log_offset, drift_summary_json, backup_ref_json, error_code, error_detail, started_at, finished_at
		 FROM task_runs WHERE operation_id = ? ORDER BY phase_idx ASC, step_key ASC`, opID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.TaskRun
	for rows.Next() {
		var tr domain.TaskRun
		var state string
		var hostID, runDir, driftS, backupS, errCode, errDet, stAt, finAt sql.NullString
		var pgid sql.NullInt64
		if err := rows.Scan(
			&tr.ID, &tr.OperationID, &tr.PhaseIdx, &tr.StepKey, &hostID, &tr.TaskType, &tr.ParamsDigest, &state,
			&tr.Attempt, &tr.FencingToken, &runDir, &pgid, &tr.RemoteLogOffset, &driftS, &backupS, &errCode, &errDet, &stAt, &finAt,
		); err != nil {
			return nil, err
		}
		tr.State = domain.TaskState(state)
		if hostID.Valid {
			v := hostID.String
			tr.HostID = &v
		}
		if runDir.Valid {
			v := runDir.String
			tr.RemoteRunDir = &v
		}
		if pgid.Valid {
			v := int(pgid.Int64)
			tr.RemotePGID = &v
		}
		if driftS.Valid {
			tr.DriftSummaryJSON = json.RawMessage(driftS.String)
		}
		if backupS.Valid {
			tr.BackupRefJSON = json.RawMessage(backupS.String)
		}
		if errCode.Valid {
			v := errCode.String
			tr.ErrorCode = &v
		}
		if errDet.Valid {
			v := errDet.String
			tr.ErrorDetail = &v
		}
		if stAt.Valid && stAt.String != "" {
			t := parseTime(stAt.String)
			tr.StartedAt = &t
		}
		if finAt.Valid && finAt.String != "" {
			t := parseTime(finAt.String)
			tr.FinishedAt = &t
		}
		out = append(out, tr)
	}
	return out, rows.Err()
}

// UpdateWithFencing enforces the D2 §2.1 #3 Fencing Token barrier so a paused zombie worker
// can NEVER overwrite progress after another replica took over the lease.
func (r *taskRunRepo) UpdateWithFencing(ctx context.Context, tr *domain.TaskRun, workerID string) (bool, error) {
	var fin any
	if tr.FinishedAt != nil {
		fin = fmtTime(*tr.FinishedAt)
	}
	if r.s.dialect == "postgres" {
		res, err := r.s.writeDB.NewRaw(`
			UPDATE task_runs t
			SET state = ?, remote_log_offset = ?, finished_at = ?, error_code = ?, error_detail = ?, fencing_token = ?
			FROM (
				SELECT id FROM operations
				WHERE id = ? AND fencing_token = ? AND lease_owner = ?
				FOR SHARE
			) o
			WHERE t.id = ? AND t.operation_id = o.id AND t.fencing_token <= ?`,
			string(tr.State), tr.RemoteLogOffset, fin, tr.ErrorCode, tr.ErrorDetail, tr.FencingToken,
			tr.OperationID, tr.FencingToken, workerID,
			tr.ID, tr.FencingToken,
		).Exec(ctx)
		if err != nil {
			return false, err
		}
		n, _ := res.RowsAffected()
		return n == 1, nil
	}

	// SQLite: single-writer BEGIN IMMEDIATE ensures atomic verification against operations
	tx, err := r.s.writeDB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	var curFence int64
	var curOwner sql.NullString
	err = tx.NewRaw(`SELECT fencing_token, lease_owner FROM operations WHERE id = ?`, tr.OperationID).Scan(ctx, &curFence, &curOwner)
	if err != nil {
		return false, err
	}
	if curFence != tr.FencingToken || !curOwner.Valid || curOwner.String != workerID {
		return false, nil
	}
	res, err := tx.NewRaw(
		`UPDATE task_runs SET state = ?, remote_log_offset = ?, finished_at = ?, error_code = ?, error_detail = ?, fencing_token = ?
		 WHERE id = ? AND operation_id = ? AND fencing_token <= ?`,
		string(tr.State), tr.RemoteLogOffset, fin, tr.ErrorCode, tr.ErrorDetail, tr.FencingToken,
		tr.ID, tr.OperationID, tr.FencingToken,
	).Exec(ctx)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return n == 1, nil
}

// --- OperationEventRepo (Ordered monotonic seq for lossless SSE, D2 §1.3 & G1 §3.1 #5) ---

type opEventRepo struct{ s *DBStore }

func (r *opEventRepo) Append(ctx context.Context, ev *domain.OperationEvent) (int64, error) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	ev.CreatedAt = now
	payload := defaultJSON(ev.PayloadJSON, "{}")

	tx, err := r.s.writeDB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	if r.s.dialect == "postgres" {
		// Prevent BIGSERIAL out-of-order commit gaps in SSE replay (D2 §1.3, G1 §3.1 #5)
		var dummy int
		if err := tx.NewRaw(`SELECT 1 FROM operations WHERE id = ? FOR UPDATE`, ev.OperationID).Scan(ctx, &dummy); err != nil {
			return 0, err
		}
		var seq int64
		if err := tx.NewRaw(
			`INSERT INTO operation_events (operation_id, task_run_id, event_type, payload_json, created_at)
			 VALUES (?, ?, ?, ?, ?) RETURNING seq`,
			ev.OperationID, ev.TaskRunID, ev.EventType, payload, fmtTime(now),
		).Scan(ctx, &seq); err != nil {
			return 0, err
		}
		// Lightweight 64B NOTIFY pulse inside the same transaction (G1 §3.2)
		pulse, _ := json.Marshal(map[string]any{"op": ev.OperationID, "seq": seq})
		_, _ = tx.NewRaw(`SELECT pg_notify('kinstall_events', ?)`, string(pulse)).Exec(ctx)
		if err := tx.Commit(); err != nil {
			return 0, err
		}
		ev.Seq = seq
		return seq, nil
	}

	res, err := tx.NewRaw(
		`INSERT INTO operation_events (operation_id, task_run_id, event_type, payload_json, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		ev.OperationID, ev.TaskRunID, ev.EventType, payload, fmtTime(now),
	).Exec(ctx)
	if err != nil {
		return 0, err
	}
	seq, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	ev.Seq = seq
	return seq, nil
}

func (r *opEventRepo) ListAfter(ctx context.Context, opID string, afterSeq int64, limit int) ([]domain.OperationEvent, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := r.s.readDB.QueryContext(ctx,
		`SELECT seq, operation_id, task_run_id, event_type, payload_json, created_at
		 FROM operation_events WHERE operation_id = ? AND seq > ? ORDER BY seq ASC LIMIT ?`,
		opID, afterSeq, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.OperationEvent
	for rows.Next() {
		var ev domain.OperationEvent
		var trID sql.NullString
		var payloadS, cAt string
		if err := rows.Scan(&ev.Seq, &ev.OperationID, &trID, &ev.EventType, &payloadS, &cAt); err != nil {
			return nil, err
		}
		if trID.Valid {
			v := trID.String
			ev.TaskRunID = &v
		}
		ev.PayloadJSON = json.RawMessage(payloadS)
		ev.CreatedAt = parseTime(cAt)
		out = append(out, ev)
	}
	return out, rows.Err()
}

// --- LockRepo (Anti-deadlock Stack -> Instance -> Host sorted batch locks, D2 §2.3) ---

type lockRepo struct{ s *DBStore }

func lockKindRank(kind string) int {
	switch kind {
	case "stack":
		return 1
	case "instance":
		return 2
	case "host":
		return 3
	default:
		return 4
	}
}

func (r *lockRepo) AcquireBatch(ctx context.Context, opID string, fencing int64, keys []ports.LockKey, ttl time.Duration) error {
	if len(keys) == 0 {
		return nil
	}
	sorted := make([]ports.LockKey, len(keys))
	copy(sorted, keys)
	sort.Slice(sorted, func(i, j int) bool {
		ri, rj := lockKindRank(sorted[i].Kind), lockKindRank(sorted[j].Kind)
		if ri != rj {
			return ri < rj
		}
		return sorted[i].ID < sorted[j].ID
	})

	now := time.Now().UTC().Truncate(time.Microsecond)
	exp := now.Add(ttl)

	tx, err := r.s.writeDB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// Purge expired locks first
	if _, err := tx.NewRaw(`DELETE FROM resource_locks WHERE expires_at < ?`, fmtTime(now)).Exec(ctx); err != nil {
		return err
	}

	for _, k := range sorted {
		mode := k.Mode
		if mode == "" {
			mode = "exclusive"
		}
		var conflictHolder string
		if mode == "exclusive" {
			err = tx.NewRaw(
				`SELECT holder_op_id FROM resource_locks WHERE resource_type = ? AND resource_id = ? AND holder_op_id <> ? LIMIT 1`,
				k.Kind, k.ID, opID,
			).Scan(ctx, &conflictHolder)
		} else {
			err = tx.NewRaw(
				`SELECT holder_op_id FROM resource_locks WHERE resource_type = ? AND resource_id = ? AND holder_op_id <> ? AND lock_mode = 'exclusive' LIMIT 1`,
				k.Kind, k.ID, opID,
			).Scan(ctx, &conflictHolder)
		}
		if err == nil && conflictHolder != "" {
			return sdk.NewError(sdk.CodeLockConflict, "LOCK", http.StatusConflict, sdk.CodeLockConflict, true,
				fmt.Errorf("%s %s locked by operation %s", k.Kind, k.ID, conflictHolder)).WithArgs(map[string]any{
				"resourceType": k.Kind,
				"resourceId":   k.ID,
				"holderOpId":   conflictHolder,
			})
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}

		_, err = tx.NewRaw(`
			INSERT INTO resource_locks (resource_type, resource_id, lock_mode, holder_op_id, fencing_token, expires_at, acquired_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (resource_type, resource_id, holder_op_id) DO UPDATE SET
			 lock_mode = excluded.lock_mode, fencing_token = excluded.fencing_token, expires_at = excluded.expires_at`,
			k.Kind, k.ID, mode, opID, fencing, fmtTime(exp), fmtTime(now),
		).Exec(ctx)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *lockRepo) RenewByOp(ctx context.Context, opID string, fencing int64, ttl time.Duration) error {
	exp := fmtTime(time.Now().UTC().Add(ttl))
	_, err := r.s.writeDB.NewRaw(
		`UPDATE resource_locks SET expires_at = ? WHERE holder_op_id = ? AND fencing_token = ?`,
		exp, opID, fencing,
	).Exec(ctx)
	return err
}

func (r *lockRepo) ReleaseByOp(ctx context.Context, opID string) error {
	_, err := r.s.writeDB.NewRaw(`DELETE FROM resource_locks WHERE holder_op_id = ?`, opID).Exec(ctx)
	return err
}

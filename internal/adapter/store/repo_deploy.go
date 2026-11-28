package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/pkg/platform/dag"
	"github.com/kinstall/kinstall/pkg/platform/db"
	"github.com/kinstall/kinstall/pkg/platform/id"
)

type SubmitTaskInput struct {
	Key        string         `json:"key"`
	Name       string         `json:"name"`
	TaskType   string         `json:"taskType"`
	NodeID     string         `json:"nodeId"`
	TaskOrder  int            `json:"taskOrder"`
	Disabled   bool           `json:"disabled"`
	MaxRetries int            `json:"maxRetries"`
	Payload    map[string]any `json:"payload"`
}

type SubmitJobParams struct {
	Scope         domain.Scope
	PrincipalID   string
	IdemKey       string
	RequestHash   string
	Name          string
	TriggerType   string
	SpecJSON      string
	Plan          *dag.CompiledPlan
	TasksByKey    map[string]SubmitTaskInput
	ActorUsername string
}

type SubmitJobOutcome struct {
	Snapshot   domain.JobSnapshot
	HTTPStatus int
	Replayed   bool
}

// SubmitJobAtomic implements LiveOps D01, D02, D03:
//   - Deduplicates via deploy_commands (tenant_id, app_id, principal_id, 'submit', idem_key)
//   - Replays identical request_hash; returns 409 IDEMPOTENCY_CONFLICT on mismatched request_hash
//   - Atomically creates deploy_jobs, deploy_tasks, deploy_edges, deploy_timeline (seq 1..N), sys_outbox_events, and deploy_commands in ONE tx!
func (r *Repo) SubmitJobAtomic(ctx context.Context, p SubmitJobParams) (SubmitJobOutcome, error) {
	now := r.Now()
	nowStr := formatTime(now)
	var out SubmitJobOutcome

	err := r.db.WithScopeTx(ctx, dbScope(p.Scope), func(ctx context.Context, tx *db.Tx) error {
		// 1. Check idempotency command ledger (D01/D02)
		var existingHash, existingResp string
		var existingStatus int
		err := tx.QueryRow(ctx,
			`SELECT request_hash, http_status, response_json
			 FROM deploy_commands
			 WHERE tenant_id=? AND app_id=? AND principal_id=? AND operation='submit' AND idem_key=?`,
			p.Scope.TenantID, p.Scope.AppID, p.PrincipalID, p.IdemKey,
		).Scan(&existingHash, &existingStatus, &existingResp)
		if err == nil {
			if existingHash != p.RequestHash {
				return domain.NewError(domain.ErrIdempotencyConflict, "IDEMPOTENCY_CONFLICT", "相同 Idempotency-Key 被用于不同的作业提交请求体", nil)
			}
			var snap domain.JobSnapshot
			if uerr := json.Unmarshal([]byte(existingResp), &snap); uerr != nil {
				return uerr
			}
			out = SubmitJobOutcome{Snapshot: snap, HTTPStatus: existingStatus, Replayed: true}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}

		// 2. Materialize Job, Tasks, Edges, Timeline & Outbox atomically (D03)
		jobID := id.New("job")
		trig := p.TriggerType
		if trig == "" {
			trig = "MANUAL"
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO deploy_jobs (tenant_id, app_id, job_id, project_id, name, trigger_type, state, cancel_requested, version, last_seq, min_retained_seq, spec_json, summary_json, created_by, created_at, updated_at, finished_at)
			 VALUES (?, ?, ?, ?, ?, ?, 'PENDING', 0, 1, 0, 1, ?, '{}', ?, ?, ?, '')`,
			p.Scope.TenantID, p.Scope.AppID, jobID, p.Scope.ProjectID, p.Name, trig, p.SpecJSON, p.ActorUsername, nowStr, nowStr,
		); err != nil {
			return err
		}

		keyToID := make(map[string]string, len(p.Plan.ActiveNodes))
		tasks := make([]domain.DeployTask, 0, len(p.Plan.ActiveNodes))
		for _, n := range p.Plan.ActiveNodes {
			tid := id.New("tsk")
			keyToID[n.Key] = tid
			spec := p.TasksByKey[n.Key]
			tname := spec.Name
			if tname == "" {
				tname = n.Key
			}
			ttype := spec.TaskType
			if ttype == "" {
				ttype = "CHECK"
			}
			inDeg := p.Plan.InDegree[n.Key]
			st := domain.TaskPending
			if inDeg == 0 {
				st = domain.TaskReady
			}
			payload := spec.Payload
			if payload == nil {
				payload = map[string]any{}
			}
			if _, err := tx.Exec(ctx,
				`INSERT INTO deploy_tasks (tenant_id, app_id, job_id, task_id, task_key, name, task_type, node_id, payload_json, state, remaining, lease_epoch, attempt_no, owner, lease_until, action_key, max_retries, last_error, result_json, started_at, finished_at, updated_at)
				 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, 0, '', '', '', ?, '', '{}', '', '', ?)`,
				p.Scope.TenantID, p.Scope.AppID, jobID, tid, n.Key, tname, ttype, spec.NodeID, marshalJSON(payload), string(st), inDeg, spec.MaxRetries, nowStr,
			); err != nil {
				return err
			}
			tasks = append(tasks, domain.DeployTask{
				TenantID: p.Scope.TenantID, AppID: p.Scope.AppID, JobID: jobID, TaskID: tid,
				TaskKey: n.Key, Name: tname, TaskType: ttype, NodeID: spec.NodeID, Payload: payload,
				State: st, Remaining: inDeg, MaxRetries: spec.MaxRetries, UpdatedAt: now,
			})
		}

		edges := make([]domain.DeployEdge, 0, len(p.Plan.ActiveEdges))
		for _, e := range p.Plan.ActiveEdges {
			fromID, toID := keyToID[e.From], keyToID[e.To]
			if _, err := tx.Exec(ctx,
				`INSERT INTO deploy_edges (tenant_id, app_id, job_id, from_task_id, to_task_id, satisfied) VALUES (?, ?, ?, ?, ?, 0)`,
				p.Scope.TenantID, p.Scope.AppID, jobID, fromID, toID,
			); err != nil {
				return err
			}
			edges = append(edges, domain.DeployEdge{
				TenantID: p.Scope.TenantID, AppID: p.Scope.AppID, JobID: jobID,
				FromTaskID: fromID, ToTaskID: toID, Satisfied: false,
			})
		}

		// Timeline event #1: job.created
		var seq int64 = 1
		if err := appendTimelineTx(ctx, tx, p.Scope, jobID, seq, "job.created", "", 0, 0,
			fmt.Sprintf("作业 %q 已提交（%d 个任务，%d 条依赖边）", p.Name, len(tasks), len(edges)),
			marshalJSON(map[string]any{"jobId": jobID, "taskCount": len(tasks), "edgeCount": len(edges)}), nowStr); err != nil {
			return err
		}
		if err := insertOutboxTx(ctx, tx, p.Scope, fmt.Sprintf("job.created:%s", jobID), "deploy.job.created", "deploy_job", jobID,
			marshalJSON(map[string]any{"jobId": jobID, "seq": seq}), nowStr); err != nil {
			return err
		}

		// Timeline + unique outbox for each initially READY task
		for _, k := range p.Plan.InitialReady {
			seq++
			tid := keyToID[k]
			if err := appendTimelineTx(ctx, tx, p.Scope, jobID, seq, "task.ready", tid, 0, 0,
				fmt.Sprintf("任务 %s 前置依赖为 0，已进入 READY 队列", k),
				marshalJSON(map[string]any{"taskId": tid, "taskKey": k}), nowStr); err != nil {
				return err
			}
			if err := insertOutboxTx(ctx, tx, p.Scope, fmt.Sprintf("task.ready:%s:%s", jobID, tid), "deploy.task.ready", "deploy_task", tid,
				marshalJSON(map[string]any{"jobId": jobID, "taskId": tid, "seq": seq}), nowStr); err != nil {
				return err
			}
		}

		if _, err := tx.Exec(ctx,
			`UPDATE deploy_jobs SET last_seq=? WHERE tenant_id=? AND app_id=? AND job_id=?`,
			seq, p.Scope.TenantID, p.Scope.AppID, jobID,
		); err != nil {
			return err
		}

		snap := domain.JobSnapshot{
			Job: domain.DeployJob{
				TenantID: p.Scope.TenantID, AppID: p.Scope.AppID, JobID: jobID, ProjectID: p.Scope.ProjectID,
				Name: p.Name, TriggerType: trig, State: domain.JobPending, Version: 1,
				LastSeq: seq, MinRetainedSeq: 1, SpecJSON: p.SpecJSON, CreatedBy: p.ActorUsername,
				CreatedAt: now, UpdatedAt: now,
			},
			Tasks:   tasks,
			Edges:   edges,
			LastSeq: seq,
		}
		respJSON := marshalJSON(snap)
		if _, err := tx.Exec(ctx,
			`INSERT INTO deploy_commands (tenant_id, app_id, principal_id, operation, idem_key, request_hash, job_id, http_status, response_json, created_at)
			 VALUES (?, ?, ?, 'submit', ?, ?, ?, 201, ?, ?)`,
			p.Scope.TenantID, p.Scope.AppID, p.PrincipalID, p.IdemKey, p.RequestHash, jobID, respJSON, nowStr,
		); err != nil {
			return err
		}
		out = SubmitJobOutcome{Snapshot: snap, HTTPStatus: 201, Replayed: false}
		return nil
	})
	return out, err
}

func (r *Repo) ListDeployJobs(ctx context.Context, scope domain.Scope, projectID string, limit int) ([]domain.DeployJob, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := `SELECT tenant_id, app_id, job_id, project_id, name, trigger_type, state, cancel_requested, version, last_seq, min_retained_seq, spec_json, summary_json, created_by, created_at, updated_at, finished_at
	      FROM deploy_jobs WHERE tenant_id=? AND app_id=?`
	args := []any{scope.TenantID, scope.AppID}
	if projectID != "" {
		q += ` AND project_id=?`
		args = append(args, projectID)
	}
	q += fmt.Sprintf(` ORDER BY created_at DESC LIMIT %d`, limit)
	rows, err := r.db.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.DeployJob
	for rows.Next() {
		j, err := scanJobRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// GetJobSnapshot executes inside a Repeatable-Read snapshot transaction so Job, Tasks, Edges & LastSeq are point-in-time consistent (LiveOps D19).
func (r *Repo) GetJobSnapshot(ctx context.Context, scope domain.Scope, jobID string, includeTimeline bool) (domain.JobSnapshot, error) {
	var snap domain.JobSnapshot
	err := r.db.WithRepeatableReadTx(ctx, dbScope(scope), func(ctx context.Context, tx *db.Tx) error {
		job, err := getJobTx(ctx, tx, scope, jobID)
		if err != nil {
			return err
		}
		tasks, err := listTasksTx(ctx, tx, scope, jobID)
		if err != nil {
			return err
		}
		edges, err := listEdgesTx(ctx, tx, scope, jobID)
		if err != nil {
			return err
		}
		snap = domain.JobSnapshot{
			Job:     job,
			Tasks:   tasks,
			Edges:   edges,
			LastSeq: job.LastSeq,
		}
		if includeTimeline {
			evts, err := listTimelineAfterTx(ctx, tx, scope, jobID, 0, 500)
			if err != nil {
				return err
			}
			snap.Timeline = evts
		}
		return nil
	})
	return snap, err
}

// CancelJobAtomic enforces If-Match version lock + Idempotency-Key dedup + LiveOps D12 cancellation semantics:
//   - Prevents unlocking new downstream tasks while preserving already-running in-flight task outcomes
//   - Marks PENDING & READY tasks CANCELLED immediately
//   - Converges job to CANCELLED immediately if no tasks remain RUNNING/RECONCILING.
func (r *Repo) CancelJobAtomic(ctx context.Context, scope domain.Scope, jobID, principalID, idemKey, reqHash string, expectedVer int64) (domain.JobSnapshot, bool, error) {
	nowStr := r.NowStr()
	var snap domain.JobSnapshot
	var replayed bool

	err := r.db.WithScopeTx(ctx, dbScope(scope), func(ctx context.Context, tx *db.Tx) error {
		if idemKey != "" {
			var prevHash, prevResp string
			err := tx.QueryRow(ctx,
				`SELECT request_hash, response_json FROM deploy_commands
				 WHERE tenant_id=? AND app_id=? AND principal_id=? AND operation='cancel' AND idem_key=?`,
				scope.TenantID, scope.AppID, principalID, idemKey,
			).Scan(&prevHash, &prevResp)
			if err == nil {
				if prevHash != reqHash {
					return domain.NewError(domain.ErrIdempotencyConflict, "IDEMPOTENCY_CONFLICT", "取消操作 Idempotency-Key 负载冲突", nil)
				}
				_ = json.Unmarshal([]byte(prevResp), &snap)
				replayed = true
				return nil
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}

		job, err := getJobTx(ctx, tx, scope, jobID)
		if err != nil {
			return err
		}
		if job.Version != expectedVer {
			return domain.NewError(domain.ErrVersionMismatch, "VERSION_MISMATCH", "作业版本已变更，请刷新后重试取消", map[string]any{"currentVersion": job.Version})
		}
		if job.State.IsTerminal() {
			return domain.NewError(domain.ErrStateConflict, "JOB_ALREADY_TERMINAL", "作业已处于终态 "+string(job.State), nil)
		}

		seq := job.LastSeq + 1
		if _, err := tx.Exec(ctx,
			`UPDATE deploy_tasks SET state='CANCELLED', finished_at=?, updated_at=?
			 WHERE tenant_id=? AND app_id=? AND job_id=? AND state IN ('PENDING','READY')`,
			nowStr, nowStr, scope.TenantID, scope.AppID, jobID,
		); err != nil {
			return err
		}
		if err := appendTimelineTx(ctx, tx, scope, jobID, seq, "job.cancel_requested", "", 0, 0,
			"收到作业取消指令，已阻止未执行任务继续解锁", `{}`, nowStr); err != nil {
			return err
		}

		tasks, err := listTasksTx(ctx, tx, scope, jobID)
		if err != nil {
			return err
		}
		nextState := domain.ReduceJobState(tasks, true)
		finAt := ""
		if nextState.IsTerminal() {
			finAt = nowStr
			seq++
			if err := appendTimelineTx(ctx, tx, scope, jobID, seq, "job.cancelled", "", 0, 0,
				"作业已收敛至 CANCELLED 终态", `{}`, nowStr); err != nil {
				return err
			}
		}

		if _, err := tx.Exec(ctx,
			`UPDATE deploy_jobs SET cancel_requested=1, state=?, version=version+1, last_seq=?, updated_at=?, finished_at=?
			 WHERE tenant_id=? AND app_id=? AND job_id=? AND version=?`,
			string(nextState), seq, nowStr, finAt, scope.TenantID, scope.AppID, jobID, expectedVer,
		); err != nil {
			return err
		}

		job, err = getJobTx(ctx, tx, scope, jobID)
		if err != nil {
			return err
		}
		edges, err := listEdgesTx(ctx, tx, scope, jobID)
		if err != nil {
			return err
		}
		snap = domain.JobSnapshot{Job: job, Tasks: tasks, Edges: edges, LastSeq: job.LastSeq}

		if idemKey != "" {
			if _, err := tx.Exec(ctx,
				`INSERT INTO deploy_commands (tenant_id, app_id, principal_id, operation, idem_key, request_hash, job_id, http_status, response_json, created_at)
				 VALUES (?, ?, ?, 'cancel', ?, ?, ?, 200, ?, ?)`,
				scope.TenantID, scope.AppID, principalID, idemKey, reqHash, jobID, marshalJSON(snap), nowStr,
			); err != nil {
				return err
			}
		}
		return nil
	})
	return snap, replayed, err
}

// ClaimNextReadyTask atomically claims ONE READY task across active non-cancelled jobs (LiveOps D04, D10).
// Increments lease_epoch and attempt_no, generates stable action_key, and records deploy_attempts + timeline.
func (r *Repo) ClaimNextReadyTask(ctx context.Context, owner string, leaseTTL time.Duration) (*domain.DeployTask, error) {
	if leaseTTL <= 0 {
		leaseTTL = 30 * time.Second
	}
	now := r.Now()
	nowStr := formatTime(now)
	leaseUntilStr := formatTime(now.Add(leaseTTL))
	var claimed *domain.DeployTask

	err := r.db.WithTx(ctx, func(ctx context.Context, tx *db.Tx) error {
		var tid, aid, jobID, taskID, taskKey string
		var curEpoch int64
		var curAttempt int
		err := tx.QueryRow(ctx,
			`SELECT t.tenant_id, t.app_id, t.job_id, t.task_id, t.task_key, t.lease_epoch, t.attempt_no
			 FROM deploy_tasks t
			 JOIN deploy_jobs j ON j.tenant_id=t.tenant_id AND j.app_id=t.app_id AND j.job_id=t.job_id
			 WHERE t.state='READY' AND j.cancel_requested=0
			 ORDER BY t.updated_at ASC, t.task_id ASC
			 LIMIT 1`,
		).Scan(&tid, &aid, &jobID, &taskID, &taskKey, &curEpoch, &curAttempt)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}

		nextEpoch := curEpoch + 1
		nextAttempt := curAttempt + 1
		actionKey := fmt.Sprintf("%s:%s:%s:a%d", tid, jobID, taskID, nextAttempt)

		res, err := tx.Exec(ctx,
			`UPDATE deploy_tasks
			 SET state='RUNNING', lease_epoch=?, attempt_no=?, owner=?, lease_until=?, action_key=?, started_at=?, updated_at=?
			 WHERE tenant_id=? AND app_id=? AND job_id=? AND task_id=? AND state='READY' AND lease_epoch=?`,
			nextEpoch, nextAttempt, owner, leaseUntilStr, actionKey, nowStr, nowStr,
			tid, aid, jobID, taskID, curEpoch,
		)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return nil // claimed concurrently by another worker
		}

		if _, err := tx.Exec(ctx,
			`INSERT INTO deploy_attempts (tenant_id, app_id, job_id, task_id, attempt_no, lease_epoch, owner, action_key, state, error_message, started_at, finished_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'RUNNING', '', ?, '')`,
			tid, aid, jobID, taskID, nextAttempt, nextEpoch, owner, actionKey, nowStr,
		); err != nil {
			return err
		}

		scope := domain.Scope{TenantID: tid, AppID: aid}
		job, err := getJobTx(ctx, tx, scope, jobID)
		if err != nil {
			return err
		}
		seq := job.LastSeq + 1
		if err := appendTimelineTx(ctx, tx, scope, jobID, seq, "task.claimed", taskID, nextAttempt, nextEpoch,
			fmt.Sprintf("Worker %s 领取任务 %s (attempt=%d, leaseEpoch=%d)", owner, taskKey, nextAttempt, nextEpoch),
			marshalJSON(map[string]any{"owner": owner, "actionKey": actionKey}), nowStr); err != nil {
			return err
		}

		nextJobState := job.State
		if nextJobState == domain.JobPending {
			nextJobState = domain.JobRunning
		}
		if _, err := tx.Exec(ctx,
			`UPDATE deploy_jobs SET state=?, last_seq=?, updated_at=? WHERE tenant_id=? AND app_id=? AND job_id=?`,
			string(nextJobState), seq, nowStr, tid, aid, jobID,
		); err != nil {
			return err
		}

		task, err := getTaskTx(ctx, tx, scope, jobID, taskID)
		if err != nil {
			return err
		}
		claimed = &task
		return nil
	})
	return claimed, err
}

// RenewTaskLease extends lease_until ONLY if owner and lease_epoch match (LiveOps D17 fencing).
func (r *Repo) RenewTaskLease(ctx context.Context, scope domain.Scope, jobID, taskID, owner string, leaseEpoch int64, leaseTTL time.Duration) error {
	now := r.Now()
	res, err := r.db.Exec(ctx,
		`UPDATE deploy_tasks SET lease_until=?, updated_at=?
		 WHERE tenant_id=? AND app_id=? AND job_id=? AND task_id=? AND state IN ('RUNNING','RECONCILING') AND owner=? AND lease_epoch=?`,
		formatTime(now.Add(leaseTTL)), formatTime(now),
		scope.TenantID, scope.AppID, jobID, taskID, owner, leaseEpoch,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return domain.NewError(domain.ErrStaleLease, "STALE_LEASE", "租约已过期或已被新 epoch 接管", nil)
	}
	return nil
}

// ReconcileExpiredLeases moves RUNNING tasks whose lease_until < now into RECONCILING (LiveOps D05).
func (r *Repo) ReconcileExpiredLeases(ctx context.Context) (int, error) {
	nowStr := r.NowStr()
	var count int
	err := r.db.WithTx(ctx, func(ctx context.Context, tx *db.Tx) error {
		list, err := listExpiredRunningTasksTx(ctx, tx, nowStr)
		if err != nil {
			return err
		}

		for _, e := range list {
			res, err := tx.Exec(ctx,
				`UPDATE deploy_tasks SET state='RECONCILING', updated_at=?
				 WHERE tenant_id=? AND app_id=? AND job_id=? AND task_id=? AND state='RUNNING' AND lease_epoch=?`,
				nowStr, e.tid, e.aid, e.jid, e.taskID, e.epoch,
			)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				continue
			}
			_, _ = tx.Exec(ctx,
				`UPDATE deploy_attempts SET state='RECONCILING'
				 WHERE tenant_id=? AND app_id=? AND job_id=? AND task_id=? AND attempt_no=?`,
				e.tid, e.aid, e.jid, e.taskID, e.attempt,
			)
			scope := domain.Scope{TenantID: e.tid, AppID: e.aid}
			job, err := getJobTx(ctx, tx, scope, e.jid)
			if err != nil {
				return err
			}
			seq := job.LastSeq + 1
			if err := appendTimelineTx(ctx, tx, scope, e.jid, seq, "task.reconciling", e.taskID, e.attempt, e.epoch,
				fmt.Sprintf("任务 %s 租约超时，已转入 RECONCILING 等待接管核对", e.key), `{}`, nowStr); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx,
				`UPDATE deploy_jobs SET last_seq=?, updated_at=? WHERE tenant_id=? AND app_id=? AND job_id=?`,
				seq, nowStr, e.tid, e.aid, e.jid,
			); err != nil {
				return err
			}
			count++
		}
		return nil
	})
	return count, err
}

// TakeoverReconcilingTask advances lease_epoch ONLY while preserving attempt_no & action_key (LiveOps D05, D17).
func (r *Repo) TakeoverReconcilingTask(ctx context.Context, newOwner string, leaseTTL time.Duration) (*domain.DeployTask, error) {
	if leaseTTL <= 0 {
		leaseTTL = 30 * time.Second
	}
	now := r.Now()
	nowStr := formatTime(now)
	leaseUntilStr := formatTime(now.Add(leaseTTL))
	var taken *domain.DeployTask

	err := r.db.WithTx(ctx, func(ctx context.Context, tx *db.Tx) error {
		var tid, aid, jobID, taskID, taskKey string
		var curEpoch int64
		var attemptNo int
		err := tx.QueryRow(ctx,
			`SELECT tenant_id, app_id, job_id, task_id, task_key, lease_epoch, attempt_no
			 FROM deploy_tasks WHERE state='RECONCILING' ORDER BY updated_at ASC LIMIT 1`,
		).Scan(&tid, &aid, &jobID, &taskID, &taskKey, &curEpoch, &attemptNo)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		nextEpoch := curEpoch + 1
		res, err := tx.Exec(ctx,
			`UPDATE deploy_tasks SET state='RUNNING', lease_epoch=?, owner=?, lease_until=?, updated_at=?
			 WHERE tenant_id=? AND app_id=? AND job_id=? AND task_id=? AND state='RECONCILING' AND lease_epoch=?`,
			nextEpoch, newOwner, leaseUntilStr, nowStr, tid, aid, jobID, taskID, curEpoch,
		)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil
		}
		_, _ = tx.Exec(ctx,
			`UPDATE deploy_attempts SET lease_epoch=?, owner=?, state='RUNNING'
			 WHERE tenant_id=? AND app_id=? AND job_id=? AND task_id=? AND attempt_no=?`,
			nextEpoch, newOwner, tid, aid, jobID, taskID, attemptNo,
		)
		scope := domain.Scope{TenantID: tid, AppID: aid}
		job, err := getJobTx(ctx, tx, scope, jobID)
		if err != nil {
			return err
		}
		seq := job.LastSeq + 1
		if err := appendTimelineTx(ctx, tx, scope, jobID, seq, "task.reconcile_takeover", taskID, attemptNo, nextEpoch,
			fmt.Sprintf("Worker %s 接管 RECONCILING 任务 %s (attempt=%d 保持不变, leaseEpoch %d->%d)", newOwner, taskKey, attemptNo, curEpoch, nextEpoch),
			marshalJSON(map[string]any{"owner": newOwner, "prevEpoch": curEpoch, "leaseEpoch": nextEpoch}), nowStr); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`UPDATE deploy_jobs SET last_seq=?, updated_at=? WHERE tenant_id=? AND app_id=? AND job_id=?`,
			seq, nowStr, tid, aid, jobID,
		); err != nil {
			return err
		}
		t, err := getTaskTx(ctx, tx, scope, jobID, taskID)
		if err != nil {
			return err
		}
		taken = &t
		return nil
	})
	return taken, err
}

type ReceiptResult struct {
	Duplicate    bool
	JobID        string
	NewLastSeq   int64
	UnlockedKeys []string
	JobState     domain.JobState
}

// ApplyReceiptAtomic implements LiveOps D06, D07, D08, D09, D12, D15, D17, D18:
//   - Receipt idempotency & conflict detection via deploy_receipts (provider, receipt_id) + digest
//   - Fencing check on stale lease_epoch (D17)
//   - Conditional edge satisfaction (satisfied 0->1), successor remaining decrement, remaining==0 -> READY + unique outbox (D08/D09/D15)
//   - Cancel precedence: preserves in-flight SUCCEEDED fact while blocking downstream unlock (D12)
//   - Permanent failure downstream SKIPPED pruning while independent branches continue (D18)
func (r *Repo) ApplyReceiptAtomic(ctx context.Context, rc domain.DeployReceipt) (ReceiptResult, error) {
	nowStr := r.NowStr()
	scope := domain.Scope{TenantID: rc.TenantID, AppID: rc.AppID}
	var result ReceiptResult
	result.JobID = rc.JobID

	err := r.db.WithScopeTx(ctx, dbScope(scope), func(ctx context.Context, tx *db.Tx) error {
		// 1. Receipt ledger dedup / conflict check (D06 / D07)
		var existingDigest string
		err := tx.QueryRow(ctx,
			`SELECT digest FROM deploy_receipts WHERE tenant_id=? AND app_id=? AND provider=? AND receipt_id=?`,
			rc.TenantID, rc.AppID, rc.Provider, rc.ReceiptID,
		).Scan(&existingDigest)
		if err == nil {
			if existingDigest == rc.Digest {
				job, jerr := getJobTx(ctx, tx, scope, rc.JobID)
				if jerr != nil {
					return jerr
				}
				result.Duplicate = true
				result.NewLastSeq = job.LastSeq
				result.JobState = job.State
				return nil
			}
			return domain.NewError(domain.ErrReceiptConflict, "RECEIPT_CONFLICT", "相同回执 ID 携带了不同摘要，已隔离冲突回执", nil)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}

		// 2. Load current task & job
		task, err := getTaskTx(ctx, tx, scope, rc.JobID, rc.TaskID)
		if err != nil {
			return err
		}
		job, err := getJobTx(ctx, tx, scope, rc.JobID)
		if err != nil {
			return err
		}

		// Fencing check (D17): reject stale local writer epoch when task is actively owned by a higher leaseEpoch
		if rc.LeaseEpoch > 0 && rc.LeaseEpoch < task.LeaseEpoch && task.State == domain.TaskRunning {
			return domain.NewError(domain.ErrStaleLease, "STALE_LEASE", fmt.Sprintf("回执 leaseEpoch=%d 落后于当前租约 %d", rc.LeaseEpoch, task.LeaseEpoch), nil)
		}

		// Record receipt durably
		if _, err := tx.Exec(ctx,
			`INSERT INTO deploy_receipts (tenant_id, app_id, provider, receipt_id, job_id, task_id, attempt_no, lease_epoch, status, digest, payload_json, received_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			rc.TenantID, rc.AppID, rc.Provider, rc.ReceiptID, rc.JobID, rc.TaskID,
			rc.AttemptNo, rc.LeaseEpoch, string(rc.Status), rc.Digest, rc.PayloadJSON, nowStr,
		); err != nil {
			return err
		}

		// If task already reached a terminal state, preserve original terminal fact without re-unlocking.
		if task.State.IsTerminal() {
			result.Duplicate = true
			result.NewLastSeq = job.LastSeq
			result.JobState = job.State
			return nil
		}

		seq := job.LastSeq

		switch rc.Status {
		case domain.TaskSucceeded:
			if _, err := tx.Exec(ctx,
				`UPDATE deploy_tasks SET state='SUCCEEDED', result_json=?, finished_at=?, updated_at=?
				 WHERE tenant_id=? AND app_id=? AND job_id=? AND task_id=?`,
				rc.PayloadJSON, nowStr, nowStr, rc.TenantID, rc.AppID, rc.JobID, rc.TaskID,
			); err != nil {
				return err
			}
			_, _ = tx.Exec(ctx,
				`UPDATE deploy_attempts SET state='SUCCEEDED', finished_at=?
				 WHERE tenant_id=? AND app_id=? AND job_id=? AND task_id=? AND attempt_no=?`,
				nowStr, rc.TenantID, rc.AppID, rc.JobID, rc.TaskID, task.AttemptNo,
			)
			seq++
			if err := appendTimelineTx(ctx, tx, scope, rc.JobID, seq, "task.succeeded", rc.TaskID, task.AttemptNo, task.LeaseEpoch,
				fmt.Sprintf("任务 %s 执行成功 (receipt=%s)", task.TaskKey, rc.ReceiptID), rc.PayloadJSON, nowStr); err != nil {
				return err
			}

			// Unlock successors ONLY if cancel_requested == 0 (D08 / D09 / D12 / D15)
			if !job.CancelRequested {
				succIDs, err := listUnsatisfiedSuccessorsTx(ctx, tx, scope, rc.JobID, rc.TaskID)
				if err != nil {
					return err
				}

				for _, toID := range succIDs {
					eres, err := tx.Exec(ctx,
						`UPDATE deploy_edges SET satisfied=1
						 WHERE tenant_id=? AND app_id=? AND job_id=? AND from_task_id=? AND to_task_id=? AND satisfied=0`,
						rc.TenantID, rc.AppID, rc.JobID, rc.TaskID, toID,
					)
					if err != nil {
						return err
					}
					if n, _ := eres.RowsAffected(); n == 0 {
						continue
					}
					// Decrement remaining on PENDING successor
					if _, err := tx.Exec(ctx,
						`UPDATE deploy_tasks SET remaining = CASE WHEN remaining > 0 THEN remaining - 1 ELSE 0 END, updated_at=?
						 WHERE tenant_id=? AND app_id=? AND job_id=? AND task_id=? AND state='PENDING'`,
						nowStr, rc.TenantID, rc.AppID, rc.JobID, toID,
					); err != nil {
						return err
					}
					succ, err := getTaskTx(ctx, tx, scope, rc.JobID, toID)
					if err != nil {
						return err
					}
					if succ.State == domain.TaskPending && succ.Remaining == 0 {
						if _, err := tx.Exec(ctx,
							`UPDATE deploy_tasks SET state='READY', updated_at=?
							 WHERE tenant_id=? AND app_id=? AND job_id=? AND task_id=? AND state='PENDING' AND remaining=0`,
							nowStr, rc.TenantID, rc.AppID, rc.JobID, toID,
						); err != nil {
							return err
						}
						seq++
						if err := appendTimelineTx(ctx, tx, scope, rc.JobID, seq, "task.ready", toID, 0, 0,
							fmt.Sprintf("前置依赖全部满足，任务 %s 解锁进入 READY", succ.TaskKey),
							marshalJSON(map[string]any{"taskId": toID, "taskKey": succ.TaskKey}), nowStr); err != nil {
							return err
						}
						if err := insertOutboxTx(ctx, tx, scope, fmt.Sprintf("task.ready:%s:%s", rc.JobID, toID), "deploy.task.ready", "deploy_task", toID,
							marshalJSON(map[string]any{"jobId": rc.JobID, "taskId": toID, "seq": seq}), nowStr); err != nil {
							return err
						}
						result.UnlockedKeys = append(result.UnlockedKeys, succ.TaskKey)
					}
				}
			}

		case domain.TaskFailed:
			errMsg := extractErrFromJSON(rc.PayloadJSON)
			// Check retry budget (if not cancelled)
			if !job.CancelRequested && task.AttemptNo <= task.MaxRetries {
				if _, err := tx.Exec(ctx,
					`UPDATE deploy_tasks SET state='READY', last_error=?, updated_at=?
					 WHERE tenant_id=? AND app_id=? AND job_id=? AND task_id=?`,
					errMsg, nowStr, rc.TenantID, rc.AppID, rc.JobID, rc.TaskID,
				); err != nil {
					return err
				}
				_, _ = tx.Exec(ctx,
					`UPDATE deploy_attempts SET state='FAILED', error_message=?, finished_at=?
					 WHERE tenant_id=? AND app_id=? AND job_id=? AND task_id=? AND attempt_no=?`,
					errMsg, nowStr, rc.TenantID, rc.AppID, rc.JobID, rc.TaskID, task.AttemptNo,
				)
				seq++
				if err := appendTimelineTx(ctx, tx, scope, rc.JobID, seq, "task.retry_scheduled", rc.TaskID, task.AttemptNo, task.LeaseEpoch,
					fmt.Sprintf("任务 %s 失败（%s），重新入队等待重试 (%d/%d)", task.TaskKey, errMsg, task.AttemptNo, task.MaxRetries),
					rc.PayloadJSON, nowStr); err != nil {
					return err
				}
			} else {
				// Permanent failure (LiveOps D18): mark task FAILED and prune unreachable downstream tasks as SKIPPED
				if _, err := tx.Exec(ctx,
					`UPDATE deploy_tasks SET state='FAILED', last_error=?, result_json=?, finished_at=?, updated_at=?
					 WHERE tenant_id=? AND app_id=? AND job_id=? AND task_id=?`,
					errMsg, rc.PayloadJSON, nowStr, nowStr, rc.TenantID, rc.AppID, rc.JobID, rc.TaskID,
				); err != nil {
					return err
				}
				_, _ = tx.Exec(ctx,
					`UPDATE deploy_attempts SET state='FAILED', error_message=?, finished_at=?
					 WHERE tenant_id=? AND app_id=? AND job_id=? AND task_id=? AND attempt_no=?`,
					errMsg, nowStr, rc.TenantID, rc.AppID, rc.JobID, rc.TaskID, task.AttemptNo,
				)
				seq++
				if err := appendTimelineTx(ctx, tx, scope, rc.JobID, seq, "task.failed", rc.TaskID, task.AttemptNo, task.LeaseEpoch,
					fmt.Sprintf("任务 %s 永久失败: %s", task.TaskKey, errMsg), rc.PayloadJSON, nowStr); err != nil {
					return err
				}

				skippedIDs, err := pruneDownstreamSkippedTx(ctx, tx, scope, rc.JobID, rc.TaskID, nowStr)
				if err != nil {
					return err
				}
				for _, sid := range skippedIDs {
					seq++
					if err := appendTimelineTx(ctx, tx, scope, rc.JobID, seq, "task.skipped", sid, 0, 0,
						fmt.Sprintf("上游任务 %s 失败，下游任务已标记为 SKIPPED", task.TaskKey), `{}`, nowStr); err != nil {
						return err
					}
				}
			}

		case domain.TaskCancelled:
			if _, err := tx.Exec(ctx,
				`UPDATE deploy_tasks SET state='CANCELLED', finished_at=?, updated_at=?
				 WHERE tenant_id=? AND app_id=? AND job_id=? AND task_id=?`,
				nowStr, nowStr, rc.TenantID, rc.AppID, rc.JobID, rc.TaskID,
			); err != nil {
				return err
			}
			seq++
			if err := appendTimelineTx(ctx, tx, scope, rc.JobID, seq, "task.cancelled", rc.TaskID, task.AttemptNo, task.LeaseEpoch,
				fmt.Sprintf("任务 %s 已确认取消", task.TaskKey), rc.PayloadJSON, nowStr); err != nil {
				return err
			}
		}

		// 3. Recompute Job aggregate convergence state
		tasks, err := listTasksTx(ctx, tx, scope, rc.JobID)
		if err != nil {
			return err
		}
		nextJobState := domain.ReduceJobState(tasks, job.CancelRequested)
		finAt := ""
		if nextJobState.IsTerminal() {
			finAt = nowStr
			seq++
			if err := appendTimelineTx(ctx, tx, scope, rc.JobID, seq, "job.completed", "", 0, 0,
				fmt.Sprintf("作业收敛至终态 %s", nextJobState),
				marshalJSON(map[string]any{"state": nextJobState}), nowStr); err != nil {
				return err
			}
			if err := insertOutboxTx(ctx, tx, scope, fmt.Sprintf("job.completed:%s", rc.JobID), "deploy.job.completed", "deploy_job", rc.JobID,
				marshalJSON(map[string]any{"jobId": rc.JobID, "state": nextJobState, "seq": seq}), nowStr); err != nil {
				return err
			}
		}

		summary := summarizeTasks(tasks)
		if _, err := tx.Exec(ctx,
			`UPDATE deploy_jobs SET state=?, version=version+1, last_seq=?, summary_json=?, updated_at=?, finished_at=?
			 WHERE tenant_id=? AND app_id=? AND job_id=?`,
			string(nextJobState), seq, marshalJSON(summary), nowStr, finAt, rc.TenantID, rc.AppID, rc.JobID,
		); err != nil {
			return err
		}

		result.NewLastSeq = seq
		result.JobState = nextJobState
		return nil
	})
	return result, err
}

func pruneDownstreamSkippedTx(ctx context.Context, tx *db.Tx, scope domain.Scope, jobID, failedTaskID, nowStr string) ([]string, error) {
	edges, err := listEdgesTx(ctx, tx, scope, jobID)
	if err != nil {
		return nil, err
	}
	adj := make(map[string][]string)
	for _, e := range edges {
		adj[e.FromTaskID] = append(adj[e.FromTaskID], e.ToTaskID)
	}
	visited := map[string]bool{failedTaskID: true}
	queue := append([]string(nil), adj[failedTaskID]...)
	var skipped []string
	for len(queue) > 0 {
		u := queue[0]
		queue = queue[1:]
		if visited[u] {
			continue
		}
		visited[u] = true
		res, err := tx.Exec(ctx,
			`UPDATE deploy_tasks SET state='SKIPPED', finished_at=?, updated_at=?
			 WHERE tenant_id=? AND app_id=? AND job_id=? AND task_id=? AND state IN ('PENDING','READY')`,
			nowStr, nowStr, scope.TenantID, scope.AppID, jobID, u,
		)
		if err != nil {
			return nil, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			skipped = append(skipped, u)
		}
		queue = append(queue, adj[u]...)
	}
	sort.Strings(skipped)
	return skipped, nil
}

func (r *Repo) ListTimelineAfter(ctx context.Context, scope domain.Scope, jobID string, afterSeq int64, limit int) ([]domain.TimelineEvent, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := r.db.Query(ctx,
		fmt.Sprintf(`SELECT tenant_id, app_id, job_id, seq, event_type, task_id, attempt_no, lease_epoch, message, payload_json, created_at
		             FROM deploy_timeline WHERE tenant_id=? AND app_id=? AND job_id=? AND seq > ? ORDER BY seq ASC LIMIT %d`, limit),
		scope.TenantID, scope.AppID, jobID, afterSeq,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.TimelineEvent
	for rows.Next() {
		var e domain.TimelineEvent
		var ca string
		if err := rows.Scan(&e.TenantID, &e.AppID, &e.JobID, &e.Seq, &e.EventType, &e.TaskID, &e.AttemptNo, &e.LeaseEpoch, &e.Message, &e.PayloadJSON, &ca); err != nil {
			return nil, err
		}
		e.CreatedAt = parseTime(ca)
		out = append(out, e)
	}
	return out, rows.Err()
}

// PruneTimelineForRetentionTest advances min_retained_seq (used to prove U06/U07 cursor retention window).
func (r *Repo) SetJobMinRetainedSeq(ctx context.Context, scope domain.Scope, jobID string, minSeq int64) error {
	_, err := r.db.Exec(ctx,
		`UPDATE deploy_jobs SET min_retained_seq=? WHERE tenant_id=? AND app_id=? AND job_id=?`,
		minSeq, scope.TenantID, scope.AppID, jobID,
	)
	return err
}

func (r *Repo) ListPendingOutbox(ctx context.Context, limit int) ([]domain.OutboxEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.db.Query(ctx,
		fmt.Sprintf(`SELECT event_id, tenant_id, app_id, dedup_key, topic, aggregate_type, aggregate_id, payload_json, status, published_at, created_at
		             FROM sys_outbox_events WHERE status='PENDING' ORDER BY created_at ASC LIMIT %d`, limit),
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.OutboxEvent
	for rows.Next() {
		var e domain.OutboxEvent
		var pa, ca string
		if err := rows.Scan(&e.EventID, &e.TenantID, &e.AppID, &e.DedupKey, &e.Topic, &e.AggregateType, &e.AggregateID, &e.PayloadJSON, &e.Status, &pa, &ca); err != nil {
			return nil, err
		}
		e.PublishedAt, e.CreatedAt = parseTimePtr(pa), parseTime(ca)
		out = append(out, e)
	}
	return out, rows.Err()
}

func (r *Repo) MarkOutboxPublished(ctx context.Context, eventID string) error {
	_, err := r.db.Exec(ctx, `UPDATE sys_outbox_events SET status='PUBLISHED', published_at=? WHERE event_id=?`, r.NowStr(), eventID)
	return err
}

func (r *Repo) CountOutboxByPrefix(ctx context.Context, scope domain.Scope, dedupPrefix string) (int, error) {
	var c int
	err := r.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM sys_outbox_events WHERE tenant_id=? AND app_id=? AND dedup_key LIKE ?`,
		scope.TenantID, scope.AppID, dedupPrefix+"%",
	).Scan(&c)
	return c, err
}

func (r *Repo) RecordInboxOnce(ctx context.Context, scope domain.Scope, consumer, messageID, digest string) (bool, error) {
	_, err := r.db.Exec(ctx,
		`INSERT INTO sys_inbox_events (tenant_id, app_id, consumer, message_id, digest, processed_at) VALUES (?, ?, ?, ?, ?, ?)`,
		scope.TenantID, scope.AppID, consumer, messageID, digest, r.NowStr(),
	)
	if err != nil {
		if isUniqueViolation(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// ============================================================================
// Internal Tx Helpers
// ============================================================================

func appendTimelineTx(ctx context.Context, tx *db.Tx, scope domain.Scope, jobID string, seq int64, eventType, taskID string, attemptNo int, leaseEpoch int64, message, payloadJSON, nowStr string) error {
	if payloadJSON == "" {
		payloadJSON = "{}"
	}
	_, err := tx.Exec(ctx,
		`INSERT INTO deploy_timeline (tenant_id, app_id, job_id, seq, event_type, task_id, attempt_no, lease_epoch, message, payload_json, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		scope.TenantID, scope.AppID, jobID, seq, eventType, taskID, attemptNo, leaseEpoch, message, payloadJSON, nowStr,
	)
	return err
}

func insertOutboxTx(ctx context.Context, tx *db.Tx, scope domain.Scope, dedupKey, topic, aggType, aggID, payloadJSON, nowStr string) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO sys_outbox_events (event_id, tenant_id, app_id, dedup_key, topic, aggregate_type, aggregate_id, payload_json, status, published_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'PENDING', '', ?)
		 ON CONFLICT(tenant_id, app_id, dedup_key) DO NOTHING`,
		id.New("evt"), scope.TenantID, scope.AppID, dedupKey, topic, aggType, aggID, payloadJSON, nowStr,
	)
	return err
}

func getJobTx(ctx context.Context, tx *db.Tx, scope domain.Scope, jobID string) (domain.DeployJob, error) {
	row := tx.QueryRow(ctx,
		`SELECT tenant_id, app_id, job_id, project_id, name, trigger_type, state, cancel_requested, version, last_seq, min_retained_seq, spec_json, summary_json, created_by, created_at, updated_at, finished_at
		 FROM deploy_jobs WHERE tenant_id=? AND app_id=? AND job_id=?`,
		scope.TenantID, scope.AppID, jobID,
	)
	j, err := scanJobRow(row)
	if err != nil {
		return domain.DeployJob{}, mapNotFound(err, "deploy job not found")
	}
	return j, nil
}

func scanJobRow(s interface{ Scan(...any) error }) (domain.DeployJob, error) {
	var j domain.DeployJob
	var st, ca, ua, fa string
	var cancelReq int
	if err := s.Scan(&j.TenantID, &j.AppID, &j.JobID, &j.ProjectID, &j.Name, &j.TriggerType, &st, &cancelReq, &j.Version, &j.LastSeq, &j.MinRetainedSeq, &j.SpecJSON, &j.SummaryJSON, &j.CreatedBy, &ca, &ua, &fa); err != nil {
		return domain.DeployJob{}, err
	}
	j.State = domain.JobState(st)
	j.CancelRequested = cancelReq != 0
	j.CreatedAt, j.UpdatedAt, j.FinishedAt = parseTime(ca), parseTime(ua), parseTimePtr(fa)
	return j, nil
}

func getTaskTx(ctx context.Context, tx *db.Tx, scope domain.Scope, jobID, taskID string) (domain.DeployTask, error) {
	row := tx.QueryRow(ctx,
		`SELECT tenant_id, app_id, job_id, task_id, task_key, name, task_type, node_id, payload_json, state, remaining, lease_epoch, attempt_no, owner, lease_until, action_key, max_retries, last_error, result_json, started_at, finished_at, updated_at
		 FROM deploy_tasks WHERE tenant_id=? AND app_id=? AND job_id=? AND task_id=?`,
		scope.TenantID, scope.AppID, jobID, taskID,
	)
	t, err := scanTaskRow(row)
	if err != nil {
		return domain.DeployTask{}, mapNotFound(err, "deploy task not found")
	}
	return t, nil
}

func listTasksTx(ctx context.Context, tx *db.Tx, scope domain.Scope, jobID string) ([]domain.DeployTask, error) {
	rows, err := tx.Query(ctx,
		`SELECT tenant_id, app_id, job_id, task_id, task_key, name, task_type, node_id, payload_json, state, remaining, lease_epoch, attempt_no, owner, lease_until, action_key, max_retries, last_error, result_json, started_at, finished_at, updated_at
		 FROM deploy_tasks WHERE tenant_id=? AND app_id=? AND job_id=? ORDER BY task_key ASC`,
		scope.TenantID, scope.AppID, jobID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.DeployTask
	for rows.Next() {
		t, err := scanTaskRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func scanTaskRow(s interface{ Scan(...any) error }) (domain.DeployTask, error) {
	var t domain.DeployTask
	var payloadRaw, st, lu, sa, fa, ua string
	if err := s.Scan(&t.TenantID, &t.AppID, &t.JobID, &t.TaskID, &t.TaskKey, &t.Name, &t.TaskType, &t.NodeID, &payloadRaw, &st, &t.Remaining, &t.LeaseEpoch, &t.AttemptNo, &t.Owner, &lu, &t.ActionKey, &t.MaxRetries, &t.LastError, &t.ResultJSON, &sa, &fa, &ua); err != nil {
		return domain.DeployTask{}, err
	}
	t.State = domain.TaskState(st)
	t.Payload = make(map[string]any)
	unmarshalJSON(payloadRaw, &t.Payload)
	t.LeaseUntil, t.StartedAt, t.FinishedAt, t.UpdatedAt = parseTimePtr(lu), parseTimePtr(sa), parseTimePtr(fa), parseTime(ua)
	return t, nil
}

func listEdgesTx(ctx context.Context, tx *db.Tx, scope domain.Scope, jobID string) ([]domain.DeployEdge, error) {
	rows, err := tx.Query(ctx,
		`SELECT tenant_id, app_id, job_id, from_task_id, to_task_id, satisfied
		 FROM deploy_edges WHERE tenant_id=? AND app_id=? AND job_id=? ORDER BY from_task_id ASC, to_task_id ASC`,
		scope.TenantID, scope.AppID, jobID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.DeployEdge
	for rows.Next() {
		var e domain.DeployEdge
		var sat int
		if err := rows.Scan(&e.TenantID, &e.AppID, &e.JobID, &e.FromTaskID, &e.ToTaskID, &sat); err != nil {
			return nil, err
		}
		e.Satisfied = sat != 0
		out = append(out, e)
	}
	return out, rows.Err()
}

func listTimelineAfterTx(ctx context.Context, tx *db.Tx, scope domain.Scope, jobID string, afterSeq int64, limit int) ([]domain.TimelineEvent, error) {
	rows, err := tx.Query(ctx,
		fmt.Sprintf(`SELECT tenant_id, app_id, job_id, seq, event_type, task_id, attempt_no, lease_epoch, message, payload_json, created_at
		             FROM deploy_timeline WHERE tenant_id=? AND app_id=? AND job_id=? AND seq > ? ORDER BY seq ASC LIMIT %d`, limit),
		scope.TenantID, scope.AppID, jobID, afterSeq,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.TimelineEvent
	for rows.Next() {
		var e domain.TimelineEvent
		var ca string
		if err := rows.Scan(&e.TenantID, &e.AppID, &e.JobID, &e.Seq, &e.EventType, &e.TaskID, &e.AttemptNo, &e.LeaseEpoch, &e.Message, &e.PayloadJSON, &ca); err != nil {
			return nil, err
		}
		e.CreatedAt = parseTime(ca)
		out = append(out, e)
	}
	return out, rows.Err()
}

func extractErrFromJSON(raw string) string {
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err == nil {
		if msg, ok := m["error"].(string); ok && msg != "" {
			return msg
		}
	}
	return "task execution failed"
}

type expiredTaskRow struct {
	tid, aid, jid, taskID, key string
	attempt                    int
	epoch                      int64
}

func listExpiredRunningTasksTx(ctx context.Context, tx *db.Tx, nowStr string) ([]expiredTaskRow, error) {
	rows, err := tx.Query(ctx,
		`SELECT tenant_id, app_id, job_id, task_id, task_key, attempt_no, lease_epoch
		 FROM deploy_tasks
		 WHERE state='RUNNING' AND lease_until <> '' AND lease_until < ?`,
		nowStr,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var list []expiredTaskRow
	for rows.Next() {
		var e expiredTaskRow
		if err := rows.Scan(&e.tid, &e.aid, &e.jid, &e.taskID, &e.key, &e.attempt, &e.epoch); err != nil {
			return nil, err
		}
		list = append(list, e)
	}
	return list, rows.Err()
}

func listUnsatisfiedSuccessorsTx(ctx context.Context, tx *db.Tx, scope domain.Scope, jobID, fromTaskID string) ([]string, error) {
	rows, err := tx.Query(ctx,
		`SELECT to_task_id FROM deploy_edges
		 WHERE tenant_id=? AND app_id=? AND job_id=? AND from_task_id=? AND satisfied=0
		 ORDER BY to_task_id ASC`,
		scope.TenantID, scope.AppID, jobID, fromTaskID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var toID string
		if err := rows.Scan(&toID); err != nil {
			return nil, err
		}
		out = append(out, toID)
	}
	return out, rows.Err()
}

func summarizeTasks(tasks []domain.DeployTask) map[string]int {
	s := map[string]int{"total": len(tasks), "succeeded": 0, "failed": 0, "skipped": 0, "cancelled": 0, "running": 0, "pending": 0}
	for _, t := range tasks {
		switch t.State {
		case domain.TaskSucceeded:
			s["succeeded"]++
		case domain.TaskFailed:
			s["failed"]++
		case domain.TaskSkipped:
			s["skipped"]++
		case domain.TaskCancelled:
			s["cancelled"]++
		case domain.TaskRunning, domain.TaskReconciling:
			s["running"]++
		default:
			s["pending"]++
		}
	}
	return s
}

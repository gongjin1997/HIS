package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"

	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/internal/osadapter"
	"github.com/kinstall/kinstall/internal/ports"
	"github.com/kinstall/kinstall/internal/version"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// HostResolver maps HostSelector and host IDs to live HostView + SSH Target material.
type HostResolver func(ctx context.Context, op *domain.Operation, sel sdk.HostSelector) ([]HostExecBinding, error)

// HostExecBinding pairs a HostView with its connection Target for execution.
type HostExecBinding struct {
	View   sdk.HostView
	Target ports.Target
}

// Worker executes queued Operations with lease heartbeat (10s/30s), monotonic Fencing Token,
// Stack->Instance->Host sorted locking, --resume skip semantics, and schema-version draining (D2 §2, G1 §6).
type Worker struct {
	workerID    string
	store       ports.Store
	locker      ports.Locker
	executor    ports.Executor
	secrets     ports.SecretStore
	media       ports.MediaResolver
	pki         ports.PKIIssuer
	render      ports.Renderer
	eval        ports.Evaluator
	events      ports.EventSink
	audit       ports.AuditRecorder
	osEngine    *osadapter.Engine
	tasks       *TaskRegistry
	hostResolve HostResolver
	logger      *slog.Logger

	leaseTTL       time.Duration
	heartbeatEvery time.Duration
	pollInterval   time.Duration
	draining       atomic.Bool
}

// WorkerOption configures Worker via functional options.
type WorkerOption func(*Worker)

func WithWorkerID(id string) WorkerOption {
	return func(w *Worker) { w.workerID = id }
}

func WithLeaseTiming(ttl, heartbeat time.Duration) WorkerOption {
	return func(w *Worker) {
		w.leaseTTL = ttl
		w.heartbeatEvery = heartbeat
	}
}

func WithHostResolver(fn HostResolver) WorkerOption {
	return func(w *Worker) { w.hostResolve = fn }
}

func NewWorker(
	st ports.Store,
	locker ports.Locker,
	exec ports.Executor,
	sec ports.SecretStore,
	media ports.MediaResolver,
	pkiIssuer ports.PKIIssuer,
	render ports.Renderer,
	eval ports.Evaluator,
	events ports.EventSink,
	audit ports.AuditRecorder,
	osEng *osadapter.Engine,
	logger *slog.Logger,
	opts ...WorkerOption,
) *Worker {
	if logger == nil {
		logger = slog.Default()
	}
	w := &Worker{
		workerID:       domain.NewID("wrk_"),
		store:          st,
		locker:         locker,
		executor:       exec,
		secrets:        sec,
		media:          media,
		pki:            pkiIssuer,
		render:         render,
		eval:           eval,
		events:         events,
		audit:          audit,
		osEngine:       osEng,
		tasks:          NewDefaultTaskRegistry(),
		logger:         logger,
		leaseTTL:       30 * time.Second,
		heartbeatEvery: 10 * time.Second,
		pollInterval:   500 * time.Millisecond,
	}
	for _, o := range opts {
		o(w)
	}
	return w
}

// IsDraining returns true once the worker detects DB schema > MaxCompatibleSchemaVersion or enters shutdown drain (G1 §6.2/§7.3).
func (w *Worker) IsDraining() bool { return w.draining.Load() }

// SetDraining transitions the worker into/out of draining mode so /readyz immediately returns 503 during shutdown.
func (w *Worker) SetDraining(v bool) { w.draining.Store(v) }

// RunClaimLoop continuously claims pending/expired operations until ctx is cancelled or schema ceiling triggers draining.
func (w *Worker) RunClaimLoop(ctx context.Context) error {
	poll := time.NewTicker(w.pollInterval)
	schemaTick := time.NewTicker(5 * time.Second)
	defer poll.Stop()
	defer schemaTick.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-schemaTick.C:
			if ver, err := w.store.SchemaVersion(ctx); err == nil && ver > version.MaxCompatibleSchemaVersion {
				w.logger.Warn("db schema exceeds binary max compatible version; entering draining mode",
					slog.Int64("db_schema", ver),
					slog.Int64("max_compatible", version.MaxCompatibleSchemaVersion))
				w.draining.Store(true)
			}
		case <-poll.C:
			if w.draining.Load() {
				continue
			}
			op, err := w.store.Operations().ClaimNext(ctx, w.workerID, w.leaseTTL)
			if err != nil {
				w.logger.Error("claim operation failed", slog.String("error", err.Error()))
				continue
			}
			if op == nil {
				continue
			}
			_ = w.ExecuteClaimed(ctx, op)
		}
	}
}

// ExecuteClaimed runs an already-claimed Operation under lease renewal and Fencing Token enforcement.
func (w *Worker) ExecuteClaimed(parentCtx context.Context, op *domain.Operation) error {
	opCtx, cancelOp := context.WithCancel(parentCtx)
	defer cancelOp()

	// Lease heartbeat goroutine: if RenewLease returns false (another worker took over), IMMEDIATELY cancel opCtx!
	var leaseLost atomic.Bool
	hbDone := make(chan struct{})
	go func() { //nolint:gosec // G118: heartbeat intentionally uses detached context so final renew check does not fail on cancel race
		defer close(hbDone)
		t := time.NewTicker(w.heartbeatEvery)
		defer t.Stop()
		for {
			select {
			case <-opCtx.Done():
				return
			case <-t.C:
				ok, err := w.store.Operations().RenewLease(context.Background(), op.ID, w.workerID, op.FencingToken, w.leaseTTL)
				if err != nil || !ok {
					leaseLost.Store(true)
					cancelOp()
					return
				}
			}
		}
	}()

	var pl sdk.Pipeline
	if err := json.Unmarshal(op.PipelineJSON, &pl); err != nil {
		cancelOp()
		<-hbDone
		_, _ = w.store.Operations().Finish(context.Background(), op.ID, w.workerID, op.FencingToken, domain.OpFailed, sdk.CodeAPIInvalidRequest, err.Error())
		return err
	}

	w.emit(opCtx, op.ID, nil, "op.state", map[string]any{"state": domain.OpRunning, "fencingToken": op.FencingToken})

	// Acquire Stack/Instance/Host locks if locker configured
	var releaseLocks func(context.Context) error
	if w.locker != nil {
		keys := []ports.LockKey{{Kind: op.TargetType, ID: op.TargetID, Mode: "exclusive"}}
		rel, err := w.locker.AcquireAll(opCtx, op.ID, op.FencingToken, keys, w.leaseTTL*2)
		if err != nil {
			cancelOp()
			<-hbDone
			_, _ = w.store.Operations().Finish(context.Background(), op.ID, w.workerID, op.FencingToken, domain.OpFailed, sdk.ErrorCode(err), err.Error())
			return err
		}
		releaseLocks = rel
		defer func() { _ = releaseLocks(context.Background()) }()
	}

	// Load prior TaskRuns to support --resume skipping already Succeeded/Skipped steps with unchanged digest!
	priorRuns := make(map[string]domain.TaskRun)
	if existing, err := w.store.TaskRuns().ListByOperation(opCtx, op.ID); err == nil {
		for _, tr := range existing {
			priorRuns[fmt.Sprintf("%d|%s", tr.PhaseIdx, tr.StepKey)] = tr
		}
	}

	var mediaPolicy domain.MediaPolicy
	if w.store != nil {
		instID := ""
		if op.TargetType == "instance" {
			instID = op.TargetID
		}
		if mp, err := w.store.MediaPolicies().ResolveEffective(opCtx, op.ProjectID, instID); err == nil && mp != nil {
			mediaPolicy = *mp
		}
	}

	runErr := w.runPipeline(opCtx, op, &pl, priorRuns, mediaPolicy)

	cancelOp()
	<-hbDone

	if leaseLost.Load() {
		return sdk.NewError(sdk.CodeEngLeaseLost, "ENG", http.StatusInternalServerError, sdk.CodeEngLeaseLost, true,
			fmt.Errorf("operation %s lease lost (fencing=%d)", op.ID, op.FencingToken))
	}

	finalState := domain.OpSucceeded
	errCode, errMsg := "", ""
	if runErr != nil {
		if errors.Is(runErr, context.Canceled) {
			finalState = domain.OpCancelled
			errCode = sdk.CodeEngCancelled
		} else {
			finalState = domain.OpFailed
			errCode = sdk.ErrorCode(runErr)
			if errCode == "" {
				errCode = sdk.CodeEngInvalidTransition
			}
		}
		errMsg = runErr.Error()
	}

	_, _ = w.store.Operations().Finish(context.Background(), op.ID, w.workerID, op.FencingToken, finalState, errCode, errMsg)
	w.emit(context.Background(), op.ID, nil, "op.state", map[string]any{"state": finalState, "errorCode": errCode})
	if w.audit != nil {
		res := "success"
		if finalState != domain.OpSucceeded {
			res = "failure"
		}
		_, _ = w.audit.Append(context.Background(), domain.AuditEntry{
			ActorID: op.RequestedBy, ActorType: "system", ProjectID: op.ProjectID,
			Action: "operation." + string(finalState), TargetType: op.TargetType, TargetID: op.TargetID,
			Result: res, ErrCode: errCode, PayloadJSON: fmt.Sprintf(`{"operationId":%q,"action":%q}`, op.ID, op.Action),
		})
	}
	return runErr
}

func (w *Worker) runPipeline(ctx context.Context, op *domain.Operation, pl *sdk.Pipeline, prior map[string]domain.TaskRun, mp domain.MediaPolicy) error {
	for phaseIdx, phase := range pl.Phases {
		if err := ctx.Err(); err != nil {
			return err
		}
		bindings, err := w.resolvePhaseHosts(ctx, op, phase.Hosts)
		if err != nil {
			return err
		}
		if len(bindings) == 0 {
			// Hostless phase (e.g. local helm/wait) runs once against default local target
			bindings = []HostExecBinding{{View: sdk.HostView{ID: "local", Name: "local"}}}
		}

		conc := phase.Concurrency
		if phase.Serial || conc <= 0 {
			conc = 1
		}
		sem := semaphore.NewWeighted(int64(conc))
		eg, egCtx := errgroup.WithContext(ctx)

		for _, hb := range bindings {
			binding := hb
			if err := sem.Acquire(egCtx, 1); err != nil {
				return err
			}
			eg.Go(func() error {
				defer sem.Release(1)
				return w.runPhaseOnHost(egCtx, op, phaseIdx, phase, binding, prior, mp)
			})
		}
		if err := eg.Wait(); err != nil {
			return err
		}
	}
	return nil
}

func (w *Worker) runPhaseOnHost(
	ctx context.Context,
	op *domain.Operation,
	phaseIdx int,
	phase sdk.PhaseSpec,
	binding HostExecBinding,
	prior map[string]domain.TaskRun,
	mp domain.MediaPolicy,
) error {
	sess, err := w.executor.Connect(ctx, binding.Target)
	if err != nil {
		return err
	}
	defer func() { _ = sess.Close() }()

	var adapter ports.OSAdapter
	if w.osEngine != nil && binding.View.Facts.OS.RuleID != "" {
		for i := range w.osEngine.OSMatch.Rules {
			if w.osEngine.OSMatch.Rules[i].ID == binding.View.Facts.OS.RuleID {
				adapter = w.osEngine.AdapterFor(&w.osEngine.OSMatch.Rules[i])
				break
			}
		}
	}

	var secretsMu sync.Mutex
	var dynamicSecrets []string
	rc := &RunContext{
		OpID:        op.ID,
		PhaseName:   phase.Name,
		Host:        binding.View,
		Session:     sess,
		OS:          adapter,
		Render:      w.render,
		Eval:        w.eval,
		Secrets:     w.secrets,
		Media:       w.media,
		MediaPolicy: mp,
		PKI:         w.pki,
		Logger:      w.logger,
		LastApplied: make(map[string]string),
		RegisterSecret: func(raw string) {
			secretsMu.Lock()
			dynamicSecrets = append(dynamicSecrets, raw)
			secretsMu.Unlock()
		},
	}

	for tIdx, tSpec := range phase.Tasks {
		stepKey := tSpec.Key
		if stepKey == "" {
			stepKey = fmt.Sprintf("%s:%d:%s", binding.View.ID, tIdx, tSpec.Type)
		}
		digest := paramsDigest(tSpec)

		// --resume skip check: skip TaskRun that already succeeded/skipped with identical ParamsDigest
		if prev, ok := prior[fmt.Sprintf("%d|%s", phaseIdx, stepKey)]; ok {
			if (prev.State == domain.TaskSucceeded || prev.State == domain.TaskSkipped) && prev.ParamsDigest == digest {
				w.emit(ctx, op.ID, &prev.ID, "task.state", map[string]any{"stepKey": stepKey, "state": domain.TaskSkipped, "resumed": true})
				continue
			}
		}

		// Optional when expression gate
		if tSpec.When != "" && w.eval != nil {
			pass, werr := w.eval.EvalBool(tSpec.When, map[string]any{"host": binding.View, "facts": binding.View.Facts})
			if werr != nil {
				return werr
			}
			if !pass {
				continue
			}
		}

		impl, err := w.tasks.Get(tSpec.Type)
		if err != nil {
			return err
		}

		now := time.Now().UTC()
		var hostIDPtr *string
		if binding.View.ID != "" && binding.View.ID != "local" {
			id := binding.View.ID
			hostIDPtr = &id
		}
		tr := &domain.TaskRun{
			ID:           domain.NewID(domain.PrefixTaskRun),
			OperationID:  op.ID,
			PhaseIdx:     phaseIdx,
			StepKey:      stepKey,
			HostID:       hostIDPtr,
			TaskType:     tSpec.Type,
			ParamsDigest: digest,
			State:        domain.TaskRunning,
			Attempt:      1,
			FencingToken: op.FencingToken,
			StartedAt:    &now,
		}
		rc.TaskRunID = tr.ID
		if err := w.store.TaskRuns().Upsert(ctx, tr); err != nil {
			return err
		}
		w.emit(ctx, op.ID, &tr.ID, "task.state", map[string]any{"stepKey": stepKey, "taskType": tSpec.Type, "host": binding.View.Name, "state": domain.TaskRunning})

		// 1. Idempotent Check() first!
		chk, err := impl.Check(ctx, rc, tSpec)
		if err != nil {
			return w.failTaskRun(ctx, tr, err)
		}
		if chk.Converged {
			fin := time.Now().UTC()
			tr.State = domain.TaskSkipped
			tr.FinishedAt = &fin
			ok, uerr := w.store.TaskRuns().UpdateWithFencing(ctx, tr, w.workerID)
			if uerr != nil {
				return uerr
			}
			if !ok {
				return sdk.NewError(sdk.CodeEngLeaseLost, "ENG", http.StatusInternalServerError, sdk.CodeEngLeaseLost, true, fmt.Errorf("stale fencing token on task %s", tr.ID))
			}
			w.emit(ctx, op.ID, &tr.ID, "task.state", map[string]any{"stepKey": stepKey, "state": domain.TaskSkipped, "summary": chk.Summary})
			continue
		}

		// 2. Apply()
		res, err := impl.Apply(ctx, rc, tSpec)
		if err != nil {
			return w.failTaskRun(ctx, tr, err)
		}
		fin := time.Now().UTC()
		tr.State = domain.TaskSucceeded
		tr.FinishedAt = &fin
		if len(res.BackupRef) > 0 {
			tr.BackupRefJSON, _ = json.Marshal(res.BackupRef)
		}
		ok, uerr := w.store.TaskRuns().UpdateWithFencing(ctx, tr, w.workerID)
		if uerr != nil {
			return uerr
		}
		if !ok {
			return sdk.NewError(sdk.CodeEngLeaseLost, "ENG", http.StatusInternalServerError, sdk.CodeEngLeaseLost, true, fmt.Errorf("stale fencing token on task %s", tr.ID))
		}
		w.emit(ctx, op.ID, &tr.ID, "task.state", map[string]any{"stepKey": stepKey, "state": domain.TaskSucceeded})
	}
	return nil
}

func (w *Worker) failTaskRun(ctx context.Context, tr *domain.TaskRun, err error) error {
	fin := time.Now().UTC()
	tr.State = domain.TaskFailed
	tr.FinishedAt = &fin
	code := sdk.ErrorCode(err)
	if code == "" {
		code = sdk.CodeExecSSHFailed
	}
	detail := err.Error()
	tr.ErrorCode = &code
	tr.ErrorDetail = &detail
	_, _ = w.store.TaskRuns().UpdateWithFencing(context.Background(), tr, w.workerID)
	w.emit(context.Background(), tr.OperationID, &tr.ID, "task.state", map[string]any{"stepKey": tr.StepKey, "state": domain.TaskFailed, "errorCode": code, "detail": detail})
	return err
}

func (w *Worker) resolvePhaseHosts(ctx context.Context, op *domain.Operation, sel sdk.HostSelector) ([]HostExecBinding, error) {
	if w.hostResolve != nil {
		return w.hostResolve(ctx, op, sel)
	}
	return nil, nil
}

func (w *Worker) emit(ctx context.Context, opID string, trID *string, evType string, payload map[string]any) {
	if w.events == nil {
		return
	}
	b, _ := json.Marshal(payload)
	_, _ = w.events.Publish(ctx, domain.OperationEvent{
		OperationID: opID,
		TaskRunID:   trID,
		EventType:   evType,
		PayloadJSON: b,
	})
}

func paramsDigest(spec sdk.TaskSpec) string {
	b, _ := json.Marshal(struct {
		Type       string
		Params     map[string]any
		ManageMode sdk.ManageMode
	}{spec.Type, spec.Params, spec.ManageMode})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

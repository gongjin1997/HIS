package engine

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"al.essio.dev/pkg/shellescape"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"

	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/internal/obs"
	"github.com/kinstall/kinstall/internal/osadapter"
	"github.com/kinstall/kinstall/internal/ports"
	"github.com/kinstall/kinstall/internal/version"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// HostResolver maps HostSelector and host IDs to live HostView + SSH Target material.
type HostResolver func(ctx context.Context, op *domain.Operation, sel sdk.HostSelector) ([]HostExecBinding, error)

// NotifyFunc dispatches operation lifecycle alerts (e.g. operation.failed) to notification channels.
type NotifyFunc func(ctx context.Context, projectID, eventType, severity, dedupKey, summary string)

// HostExecBinding pairs a HostView with its connection Target for execution.
type HostExecBinding struct {
	View   sdk.HostView
	Target ports.Target
}

// Worker executes queued Operations with lease & lock heartbeat (10s/30s), monotonic Fencing Token,
// Stack->Instance->Host sorted locking, rolling batches, scrubbed SSE task.log streaming,
// notify handlers, Class-A LIFO rollback, --resume skip semantics, and post-run state persistence (D2 §2, G1 §6).
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
	notifier    NotifyFunc
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

func WithNotifier(fn NotifyFunc) WorkerOption {
	return func(w *Worker) { w.notifier = fn }
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

// opStateTracker accumulates per-file lastApplied SHAs and scrubbed file snapshots across hosts during an Operation.
type opStateTracker struct {
	mu           sync.Mutex
	shas         map[string]string
	scrubbedText map[string]string
	dirty        bool
}

func newOpStateTracker(inst *domain.Instance) *opStateTracker {
	tr := &opStateTracker{
		shas:         map[string]string{},
		scrubbedText: map[string]string{},
	}
	if inst == nil || len(inst.StateJSON) == 0 {
		return tr
	}
	var st struct {
		LastAppliedShas  map[string]string `json:"lastAppliedShas"`
		LastAppliedFiles map[string]string `json:"lastAppliedFiles"`
	}
	if err := json.Unmarshal(inst.StateJSON, &st); err == nil {
		for k, v := range st.LastAppliedFiles {
			tr.scrubbedText[k] = v
			sum := sha256.Sum256([]byte(v))
			tr.shas[k] = hex.EncodeToString(sum[:])
		}
		for k, v := range st.LastAppliedShas {
			tr.shas[k] = v
		}
	}
	return tr
}

func (s *opStateTracker) snapshotSHAs() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make(map[string]string, len(s.shas))
	for k, v := range s.shas {
		cp[k] = v
	}
	return cp
}

func (s *opStateTracker) record(shas, rawContents map[string]string, secrets []string) {
	if len(shas) == 0 && len(rawContents) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range shas {
		if s.shas[k] != v {
			s.shas[k] = v
			s.dirty = true
		}
	}
	for k, v := range rawContents {
		scrubbed := obs.RedactString(v, secrets...)
		if s.scrubbedText[k] != scrubbed {
			s.scrubbedText[k] = scrubbed
			s.dirty = true
		}
	}
}

// ExecuteClaimed runs an already-claimed Operation under lease & lock renewal and Fencing Token enforcement.
func (w *Worker) ExecuteClaimed(parentCtx context.Context, op *domain.Operation) error {
	opCtx, cancelOp := context.WithCancel(parentCtx)
	defer cancelOp()

	// Lease + resource lock heartbeat goroutine: if RenewLease returns false (another worker took over), IMMEDIATELY cancel opCtx!
	var leaseLost atomic.Bool
	var locksHeld atomic.Bool
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
				if w.locker != nil && locksHeld.Load() {
					_ = w.locker.Renew(context.Background(), op.ID, op.FencingToken, w.leaseTTL*2)
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

	// Load target Instance (when applicable) for Stack lock hierarchy & LastApplied 3-Way Diff baseline
	var targetInst *domain.Instance
	if op.TargetType == "instance" && w.store != nil {
		if inst, gerr := w.store.Instances().Get(opCtx, op.TargetID); gerr == nil {
			targetInst = inst
		}
	}
	stateTrack := newOpStateTracker(targetInst)

	// Pre-resolve phase host bindings once so we can acquire Stack -> Instance -> Host sorted locks BEFORE running any phase!
	phaseBindings := make([][]HostExecBinding, len(pl.Phases))
	defer func() {
		for _, bs := range phaseBindings {
			for i := range bs {
				zeroBindingAuth(&bs[i])
			}
		}
	}()
	for i, ph := range pl.Phases {
		bs, rerr := w.resolvePhaseHosts(opCtx, op, ph.Hosts)
		if rerr != nil {
			cancelOp()
			<-hbDone
			code := sdk.ErrorCode(rerr)
			if code == "" {
				code = sdk.CodeExecSSHFailed
			}
			_, _ = w.store.Operations().Finish(context.Background(), op.ID, w.workerID, op.FencingToken, domain.OpFailed, code, rerr.Error())
			w.emit(context.Background(), op.ID, nil, "op.state", map[string]any{"state": domain.OpFailed, "errorCode": code, "detail": rerr.Error()})
			return rerr
		}
		phaseBindings[i] = bs
	}

	w.emit(opCtx, op.ID, nil, "op.state", map[string]any{"state": domain.OpRunning, "fencingToken": op.FencingToken})

	// Acquire sorted Stack -> Instance -> Host locks (D2 §2.3)
	var releaseLocks func(context.Context) error
	if w.locker != nil {
		keys := buildSortedLockKeys(op, targetInst, phaseBindings)
		rel, err := w.locker.AcquireAll(opCtx, op.ID, op.FencingToken, keys, w.leaseTTL*2)
		if err != nil {
			cancelOp()
			<-hbDone
			_, _ = w.store.Operations().Finish(context.Background(), op.ID, w.workerID, op.FencingToken, domain.OpFailed, sdk.ErrorCode(err), err.Error())
			w.emit(context.Background(), op.ID, nil, "op.state", map[string]any{"state": domain.OpFailed, "errorCode": sdk.ErrorCode(err), "detail": err.Error()})
			return err
		}
		locksHeld.Store(true)
		releaseLocks = rel
		defer func() {
			locksHeld.Store(false)
			_ = releaseLocks(context.Background())
		}()
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

	runErr := w.runPipeline(opCtx, op, &pl, phaseBindings, priorRuns, mediaPolicy, stateTrack)

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

	// Persist Instance/Host convergence state & 3-Way Diff file baseline snapshots
	w.persistOutcome(context.Background(), op, finalState, stateTrack)

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
	if finalState == domain.OpFailed && w.notifier != nil {
		w.notifier(
			context.Background(),
			op.ProjectID,
			"operation.failed",
			"critical",
			fmt.Sprintf("op.failed:%s:%s", op.TargetType, op.TargetID),
			fmt.Sprintf("Operation %s (%s on %s/%s) failed [%s]: %s", op.ID, op.Action, op.TargetType, op.TargetID, errCode, errMsg),
		)
	}
	return runErr
}

func (w *Worker) persistOutcome(ctx context.Context, op *domain.Operation, finalState domain.OpState, stateTrack *opStateTracker) {
	if w.store == nil {
		return
	}
	switch op.TargetType {
	case "instance":
		inst, err := w.store.Instances().Get(ctx, op.TargetID)
		if err != nil || inst == nil {
			return
		}
		stateMap := map[string]any{}
		if len(inst.StateJSON) > 0 {
			_ = json.Unmarshal(inst.StateJSON, &stateMap)
		}
		stateTrack.mu.Lock()
		if stateTrack.dirty || len(stateTrack.shas) > 0 {
			stateMap["lastAppliedShas"] = stateTrack.shas
			if len(stateTrack.scrubbedText) > 0 {
				stateMap["lastAppliedFiles"] = stateTrack.scrubbedText
			}
		}
		stateTrack.mu.Unlock()
		stateMap["lastOperationId"] = op.ID
		stateMap["lastOperationAction"] = op.Action
		stateMap["lastOperationState"] = string(finalState)
		if b, merr := json.Marshal(stateMap); merr == nil {
			inst.StateJSON = b
		}
		switch finalState {
		case domain.OpSucceeded:
			if op.Action == "uninstall" {
				inst.State = "uninstalled"
				_ = w.store.Instances().ReplaceClaims(ctx, inst.ID, nil)
			} else {
				inst.State = "ready"
				if len(inst.DesiredSpecJSON) > 0 {
					inst.LastAppliedJSON = append([]byte(nil), inst.DesiredSpecJSON...)
				}
			}
		case domain.OpFailed:
			inst.State = "failed"
		}
		_ = w.store.Instances().Update(ctx, inst)

	case "host":
		if op.Action == "hostinit" && finalState == domain.OpSucceeded {
			if h, err := w.store.Hosts().Get(ctx, op.TargetID); err == nil && h != nil {
				h.State = domain.HostReady
				_ = w.store.Hosts().Update(ctx, h)
			}
		}

	case "stack":
		st, err := w.store.Stacks().Get(ctx, op.TargetID)
		if err != nil || st == nil {
			return
		}
		switch finalState {
		case domain.OpSucceeded:
			if op.Action == "uninstall" {
				st.State = "uninstalled"
				if members, lerr := w.store.Instances().ListByStack(ctx, st.ID); lerr == nil {
					for i := range members {
						members[i].State = "uninstalled"
						_ = w.store.Instances().ReplaceClaims(ctx, members[i].ID, nil)
						_ = w.store.Instances().Update(ctx, &members[i])
					}
				}
			} else {
				st.State = "ready"
				if members, lerr := w.store.Instances().ListByStack(ctx, st.ID); lerr == nil {
					for i := range members {
						members[i].State = "ready"
						if len(members[i].DesiredSpecJSON) > 0 {
							members[i].LastAppliedJSON = append([]byte(nil), members[i].DesiredSpecJSON...)
						}
						_ = w.store.Instances().Update(ctx, &members[i])
					}
				}
			}
		case domain.OpFailed:
			st.State = "failed"
		}
		_ = w.store.Stacks().Update(ctx, st)
	}
}

func buildSortedLockKeys(op *domain.Operation, inst *domain.Instance, phaseBindings [][]HostExecBinding) []ports.LockKey {
	seen := map[string]ports.LockKey{}
	add := func(kind, id, mode string) {
		if kind == "" || id == "" || id == "local" {
			return
		}
		k := kind + ":" + id
		if cur, ok := seen[k]; ok {
			if mode == "exclusive" && cur.Mode != "exclusive" {
				cur.Mode = "exclusive"
				seen[k] = cur
			}
			return
		}
		seen[k] = ports.LockKey{Kind: kind, ID: id, Mode: mode}
	}

	if inst != nil && inst.StackID != nil && *inst.StackID != "" {
		add("stack", *inst.StackID, "shared")
	}
	add(op.TargetType, op.TargetID, "exclusive")
	for _, bs := range phaseBindings {
		for _, hb := range bs {
			add("host", hb.View.ID, "exclusive")
		}
	}

	rank := func(kind string) int {
		switch kind {
		case "stack":
			return 0
		case "instance":
			return 1
		case "host":
			return 2
		default:
			return 3
		}
	}
	keys := make([]ports.LockKey, 0, len(seen))
	for _, lk := range seen {
		keys = append(keys, lk)
	}
	sort.Slice(keys, func(i, j int) bool {
		ri, rj := rank(keys[i].Kind), rank(keys[j].Kind)
		if ri != rj {
			return ri < rj
		}
		return keys[i].ID < keys[j].ID
	})
	return keys
}

func zeroBindingAuth(hb *HostExecBinding) {
	if hb == nil {
		return
	}
	hb.Target.Auth.Zero()
	for i := range hb.Target.Jumps {
		hb.Target.Jumps[i].Auth.Zero()
	}
}

func (w *Worker) runPipeline(
	ctx context.Context,
	op *domain.Operation,
	pl *sdk.Pipeline,
	phaseBindings [][]HostExecBinding,
	prior map[string]domain.TaskRun,
	mp domain.MediaPolicy,
	stateTrack *opStateTracker,
) error {
	for phaseIdx, phase := range pl.Phases {
		if err := ctx.Err(); err != nil {
			return err
		}
		var bindings []HostExecBinding
		if phaseIdx < len(phaseBindings) {
			bindings = phaseBindings[phaseIdx]
		}
		if len(bindings) == 0 {
			// Hostless phase (e.g. local helm/wait) runs once against default local target
			bindings = []HostExecBinding{{View: sdk.HostView{ID: "local", Name: "local"}}}
		}

		// Rolling wave execution (P1 worker upgrade, D2 §2.2)
		if phase.Rolling != nil && phase.Rolling.MaxUnavailable > 0 {
			batchSize := phase.Rolling.MaxUnavailable
			for start := 0; start < len(bindings); start += batchSize {
				end := start + batchSize
				if end > len(bindings) {
					end = len(bindings)
				}
				wave := bindings[start:end]
				eg, egCtx := errgroup.WithContext(ctx)
				for _, hb := range wave {
					binding := hb
					eg.Go(func() error {
						return w.runPhaseOnHost(egCtx, op, pl, phaseIdx, phase, binding, prior, mp, stateTrack)
					})
				}
				if err := eg.Wait(); err != nil {
					return err
				}
				if end < len(bindings) && phase.Rolling.PauseBetween > 0 {
					select {
					case <-ctx.Done():
						return ctx.Err()
					case <-time.After(phase.Rolling.PauseBetween):
					}
				}
			}
			continue
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
				return w.runPhaseOnHost(egCtx, op, pl, phaseIdx, phase, binding, prior, mp, stateTrack)
			})
		}
		if err := eg.Wait(); err != nil {
			return err
		}
	}
	return nil
}

type appliedStep struct {
	impl    Task
	spec    sdk.TaskSpec
	res     ApplyResult
	stepKey string
}

// sseLineWriter splits incoming stream chunks into lines and emits task.log SSE events.
type sseLineWriter struct {
	mu      sync.Mutex
	buf     bytes.Buffer
	emitted bool
	emitFn  func(line string)
}

func (l *sseLineWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.buf.Write(p)
	for {
		data := l.buf.Bytes()
		idx := bytes.IndexByte(data, '\n')
		if idx < 0 {
			break
		}
		line := strings.TrimRight(string(data[:idx]), "\r")
		l.buf.Next(idx + 1)
		if line != "" {
			l.emitted = true
			l.emitFn(line)
		}
	}
	return len(p), nil
}

func (l *sseLineWriter) Flush() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.buf.Len() > 0 {
		line := strings.TrimRight(l.buf.String(), "\r\n")
		l.buf.Reset()
		if line != "" {
			l.emitted = true
			l.emitFn(line)
		}
	}
}

func (w *Worker) runPhaseOnHost(
	ctx context.Context,
	op *domain.Operation,
	pl *sdk.Pipeline,
	phaseIdx int,
	phase sdk.PhaseSpec,
	binding HostExecBinding,
	prior map[string]domain.TaskRun,
	mp domain.MediaPolicy,
	stateTrack *opStateTracker,
) error {
	sess, err := w.executor.Connect(ctx, binding.Target)
	zeroBindingAuth(&binding)
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
	var activeOutRW, activeErrRW *obs.RedactWriter

	var notifyMu sync.Mutex
	var notifiedHandlers []string
	notifiedSeen := map[string]bool{}

	rc := &RunContext{
		OpID:               op.ID,
		PhaseName:          phase.Name,
		Host:               binding.View,
		Session:            sess,
		OS:                 adapter,
		Render:             w.render,
		Eval:               w.eval,
		Secrets:            w.secrets,
		Media:              w.media,
		MediaPolicy:        mp,
		PKI:                w.pki,
		Logger:             w.logger,
		LastApplied:        stateTrack.snapshotSHAs(),
		LastAppliedContent: make(map[string]string),
		ForceOverride:      pl != nil && pl.ForceOverride,
		Adopt:              pl != nil && pl.Adopt,
		Notify: func(handler string) {
			handler = strings.TrimSpace(handler)
			if handler == "" {
				return
			}
			notifyMu.Lock()
			if !notifiedSeen[handler] {
				notifiedSeen[handler] = true
				notifiedHandlers = append(notifiedHandlers, handler)
			}
			notifyMu.Unlock()
		},
		RegisterSecret: func(raw string) {
			if len(raw) < 3 {
				return
			}
			secretsMu.Lock()
			dynamicSecrets = append(dynamicSecrets, raw)
			if activeOutRW != nil {
				activeOutRW.RegisterSecret(raw)
			}
			if activeErrRW != nil {
				activeErrRW.RegisterSecret(raw)
			}
			secretsMu.Unlock()
		},
	}

	var appliedSteps []appliedStep
	rollbackApplied := func() {
		if len(appliedSteps) == 0 {
			return
		}
		rbCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for i := len(appliedSteps) - 1; i >= 0; i-- {
			step := appliedSteps[i]
			rberr := step.impl.Rollback(rbCtx, rc, step.spec, step.res)
			payload := map[string]any{
				"phase":   phase.Name,
				"step":    step.stepKey,
				"stepKey": step.stepKey,
				"host":    binding.View.Name,
				"status":  "rolled_back",
			}
			if rberr != nil {
				payload["status"] = "rollback_failed"
				payload["error"] = rberr.Error()
			}
			w.emit(rbCtx, op.ID, nil, "task.rollback", payload)
		}
	}

	defer func() {
		secretsMu.Lock()
		secs := append([]string(nil), dynamicSecrets...)
		secretsMu.Unlock()
		stateTrack.record(rc.LastApplied, rc.LastAppliedContent, secs)
	}()

	for tIdx, tSpec := range phase.Tasks {
		baseKey := tSpec.Key
		if baseKey == "" {
			baseKey = fmt.Sprintf("%d:%s", tIdx, tSpec.Type)
		}
		stepKey := baseKey
		if binding.View.ID != "" && binding.View.ID != "local" {
			stepKey = binding.View.ID + ":" + baseKey
		}
		digest := paramsDigest(tSpec)

		// --resume skip check: skip TaskRun that already succeeded/skipped with identical ParamsDigest;
		// on retry of a prior failed/interrupted step, reuse its TaskRun ID and increment Attempt so Upsert+UpdateWithFencing stay consistent!
		trID := domain.NewID(domain.PrefixTaskRun)
		attempt := 1
		if prev, ok := prior[fmt.Sprintf("%d|%s", phaseIdx, stepKey)]; ok {
			if (prev.State == domain.TaskSucceeded || prev.State == domain.TaskSkipped) && prev.ParamsDigest == digest {
				w.emit(ctx, op.ID, &prev.ID, "task.state", map[string]any{
					"phase": phase.Name, "step": stepKey, "stepKey": stepKey,
					"host": binding.View.Name, "state": domain.TaskSkipped, "resumed": true,
				})
				continue
			}
			if prev.ID != "" {
				trID = prev.ID
				attempt = prev.Attempt + 1
			}
		}

		// Optional when expression gate
		if tSpec.When != "" && w.eval != nil {
			pass, werr := w.eval.EvalBool(tSpec.When, map[string]any{"host": binding.View, "facts": binding.View.Facts})
			if werr != nil {
				rollbackApplied()
				return werr
			}
			if !pass {
				continue
			}
		}

		impl, err := w.tasks.Get(tSpec.Type)
		if err != nil {
			rollbackApplied()
			return err
		}

		now := time.Now().UTC()
		var hostIDPtr *string
		if binding.View.ID != "" && binding.View.ID != "local" {
			id := binding.View.ID
			hostIDPtr = &id
		}
		tr := &domain.TaskRun{
			ID:           trID,
			OperationID:  op.ID,
			PhaseIdx:     phaseIdx,
			StepKey:      stepKey,
			HostID:       hostIDPtr,
			TaskType:     tSpec.Type,
			ParamsDigest: digest,
			State:        domain.TaskRunning,
			Attempt:      attempt,
			FencingToken: op.FencingToken,
			StartedAt:    &now,
		}
		rc.TaskRunID = tr.ID
		if err := w.store.TaskRuns().Upsert(ctx, tr); err != nil {
			rollbackApplied()
			return err
		}
		w.emit(ctx, op.ID, &tr.ID, "task.state", map[string]any{
			"phase": phase.Name, "step": stepKey, "stepKey": stepKey,
			"taskType": tSpec.Type, "host": binding.View.Name, "state": domain.TaskRunning,
		})

		// Wire scrubbed live stdout/stderr SSE streams for this task
		outLines := &sseLineWriter{emitFn: func(line string) {
			w.emit(ctx, op.ID, &tr.ID, "task.log", map[string]any{
				"phase": phase.Name, "step": stepKey, "host": binding.View.Name, "stream": "stdout", "line": line,
			})
		}}
		errLines := &sseLineWriter{emitFn: func(line string) {
			w.emit(ctx, op.ID, &tr.ID, "task.log", map[string]any{
				"phase": phase.Name, "step": stepKey, "host": binding.View.Name, "stream": "stderr", "line": line,
			})
		}}
		secretsMu.Lock()
		outRW := obs.NewRedactWriter(outLines, dynamicSecrets...)
		errRW := obs.NewRedactWriter(errLines, dynamicSecrets...)
		activeOutRW = outRW
		activeErrRW = errRW
		secretsMu.Unlock()
		rc.LogStdout = outRW
		rc.LogStderr = errRW

		flushTaskLogs := func(fallbackOutput string) {
			_ = outRW.Close()
			_ = errRW.Close()
			outLines.Flush()
			errLines.Flush()
			secretsMu.Lock()
			activeOutRW = nil
			activeErrRW = nil
			secs := append([]string(nil), dynamicSecrets...)
			secretsMu.Unlock()
			rc.LogStdout = nil
			rc.LogStderr = nil
			if fallbackOutput != "" && !outLines.emitted {
				scrubbed := obs.RedactString(fallbackOutput, secs...)
				for _, ln := range strings.Split(strings.TrimRight(scrubbed, "\n"), "\n") {
					if strings.TrimSpace(ln) != "" {
						w.emit(ctx, op.ID, &tr.ID, "task.log", map[string]any{
							"phase": phase.Name, "step": stepKey, "host": binding.View.Name, "stream": "stdout", "line": ln,
						})
					}
				}
			}
		}

		// 1. Idempotent Check() first!
		chk, err := impl.Check(ctx, rc, tSpec)
		if err != nil {
			flushTaskLogs("")
			rollbackApplied()
			return w.failTaskRun(ctx, phase.Name, binding.View.Name, tr, err)
		}
		if chk.Converged {
			flushTaskLogs("")
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
			w.emit(ctx, op.ID, &tr.ID, "task.state", map[string]any{
				"phase": phase.Name, "step": stepKey, "stepKey": stepKey,
				"host": binding.View.Name, "state": domain.TaskSkipped, "summary": chk.Summary,
			})
			continue
		}

		// 2. Apply()
		res, err := impl.Apply(ctx, rc, tSpec)
		flushTaskLogs(res.Output)
		if err != nil {
			rollbackApplied()
			return w.failTaskRun(ctx, phase.Name, binding.View.Name, tr, err)
		}
		if res.Changed {
			appliedSteps = append(appliedSteps, appliedStep{impl: impl, spec: tSpec, res: res, stepKey: stepKey})
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
		w.emit(ctx, op.ID, &tr.ID, "task.state", map[string]any{
			"phase": phase.Name, "step": stepKey, "stepKey": stepKey,
			"host": binding.View.Name, "state": domain.TaskSucceeded,
		})
	}

	// 3. Execute deduplicated Notify handlers triggered during this phase (e.g. systemd reload/restart)
	for _, handler := range notifiedHandlers {
		var cmdStr string
		if adapter != nil {
			cmdStr = adapter.ServiceCmd(ports.ServiceReload, handler)
		} else {
			cmdStr = fmt.Sprintf("systemctl reload-or-restart %s", shellescape.Quote(handler))
		}
		w.emit(ctx, op.ID, nil, "task.log", map[string]any{
			"phase": phase.Name, "step": "notify:" + handler, "host": binding.View.Name,
			"stream": "stdout", "line": "executing notify handler: " + cmdStr,
		})
		if _, nerr := sess.Run(ctx, ports.Command{Script: cmdStr, Sudo: true}); nerr != nil {
			return fmt.Errorf("notify handler %q failed: %w", handler, nerr)
		}
	}
	return nil
}

func (w *Worker) failTaskRun(_ context.Context, phaseName, hostName string, tr *domain.TaskRun, err error) error {
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
	w.emit(context.Background(), tr.OperationID, &tr.ID, "task.state", map[string]any{
		"phase": phaseName, "step": tr.StepKey, "stepKey": tr.StepKey, "host": hostName,
		"state": domain.TaskFailed, "errorCode": code, "detail": detail,
	})
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
		Template   *sdk.TemplateRef
		Vars       map[string]any
		ManageMode sdk.ManageMode
	}{spec.Type, spec.Params, spec.Template, spec.Vars, spec.ManageMode})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/kinstall/kinstall/internal/adapter/store"
	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/internal/usecase/deploy"
	"github.com/kinstall/kinstall/internal/usecase/nodes"
	"github.com/kinstall/kinstall/pkg/platform/id"
)

// ExecutionResult is the structured outcome produced by an ExecutionProvider for a claimed task.
type ExecutionResult struct {
	Provider  string
	ReceiptID string
	Status    domain.TaskState
	Payload   map[string]any
}

// ExecutionProvider abstracts task execution so control plane never executes local host shell commands.
type ExecutionProvider interface {
	Execute(ctx context.Context, task domain.DeployTask) ExecutionResult
}

// FoundationProvider is the built-in safe LiveOps foundation executor supporting CHECK / SYNC / VERIFY / DELAY / FAIL task modes.
type FoundationProvider struct{}

func (FoundationProvider) Execute(ctx context.Context, task domain.DeployTask) ExecutionResult {
	delayMs := 120
	if raw, ok := task.Payload["delayMs"]; ok {
		switch v := raw.(type) {
		case float64:
			if v >= 0 && v <= 5000 {
				delayMs = int(v)
			}
		case int:
			if v >= 0 && v <= 5000 {
				delayMs = v
			}
		}
	}
	if delayMs > 0 {
		t := time.NewTimer(time.Duration(delayMs) * time.Millisecond)
		select {
		case <-ctx.Done():
			t.Stop()
			return ExecutionResult{
				Provider:  "liveops-foundation",
				ReceiptID: "rcpt-" + task.ActionKey + "-cancel",
				Status:    domain.TaskCancelled,
				Payload:   map[string]any{"reason": "worker context cancelled"},
			}
		case <-t.C:
		}
	}

	if failFlag, ok := task.Payload["simulateFail"].(bool); ok && failFlag {
		return ExecutionResult{
			Provider:  "liveops-foundation",
			ReceiptID: "rcpt-" + task.ActionKey,
			Status:    domain.TaskFailed,
			Payload:   map[string]any{"error": "simulated failure requested by task payload", "taskKey": task.TaskKey},
		}
	}

	return ExecutionResult{
		Provider:  "liveops-foundation",
		ReceiptID: "rcpt-" + task.ActionKey,
		Status:    domain.TaskSucceeded,
		Payload: map[string]any{
			"ok":        true,
			"taskKey":   task.TaskKey,
			"taskType":  task.TaskType,
			"actionKey": task.ActionKey,
			"attemptNo": task.AttemptNo,
		},
	}
}

type Config struct {
	WorkerID          string
	LeaseTTL          time.Duration
	LeaseRenewEvery   time.Duration
	PollInterval      time.Duration
	OutboxInterval    time.Duration
	ReconcileInterval time.Duration
	WatchdogInterval  time.Duration
	Concurrency       int
}

// Runner executes all decoupled LiveOps Worker background loops:
// Outbox Relay, DAG Task Lease Dispatcher, Lease-Expiry Reconciler, and Node Health Watchdog.
type Runner struct {
	cfg      Config
	repo     *store.Repo
	deploy   *deploy.Service
	nodes    *nodes.Service
	provider ExecutionProvider
	logger   *slog.Logger
}

func NewRunner(cfg Config, repo *store.Repo, deploySvc *deploy.Service, nodesSvc *nodes.Service, provider ExecutionProvider, logger *slog.Logger) *Runner {
	if cfg.WorkerID == "" {
		cfg.WorkerID = id.New("wrk")
	}
	if cfg.LeaseTTL <= 0 {
		cfg.LeaseTTL = 30 * time.Second
	}
	if cfg.LeaseRenewEvery <= 0 {
		cfg.LeaseRenewEvery = 10 * time.Second
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 250 * time.Millisecond
	}
	if cfg.OutboxInterval <= 0 {
		cfg.OutboxInterval = 500 * time.Millisecond
	}
	if cfg.ReconcileInterval <= 0 {
		cfg.ReconcileInterval = 5 * time.Second
	}
	if cfg.WatchdogInterval <= 0 {
		cfg.WatchdogInterval = 10 * time.Second
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 4
	}
	if provider == nil {
		provider = FoundationProvider{}
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Runner{
		cfg: cfg, repo: repo, deploy: deploySvc, nodes: nodesSvc, provider: provider, logger: logger,
	}
}

func (r *Runner) Run(ctx context.Context) {
	r.logger.Info("liveops worker loops started", "workerId", r.cfg.WorkerID, "concurrency", r.cfg.Concurrency)
	var wg sync.WaitGroup
	wg.Add(4)
	go func() { defer wg.Done(); r.outboxLoop(ctx) }()
	go func() { defer wg.Done(); r.dispatchLoop(ctx) }()
	go func() { defer wg.Done(); r.reconcileLoop(ctx) }()
	go func() { defer wg.Done(); r.nodeWatchdogLoop(ctx) }()
	wg.Wait()
	r.logger.Info("liveops worker loops stopped", "workerId", r.cfg.WorkerID)
}

func (r *Runner) outboxLoop(ctx context.Context) {
	t := time.NewTicker(r.cfg.OutboxInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			events, err := r.repo.ListPendingOutbox(ctx, 50)
			if err != nil {
				continue
			}
			for _, ev := range events {
				if err := r.repo.MarkOutboxPublished(ctx, ev.EventID); err == nil {
					if ev.AggregateType == "deploy_job" {
						r.deploy.Hub().Notify(ev.AggregateID, 0)
					}
				}
			}
		}
	}
}

func (r *Runner) dispatchLoop(ctx context.Context) {
	sem := make(chan struct{}, r.cfg.Concurrency)
	t := time.NewTicker(r.cfg.PollInterval)
	defer t.Stop()
	var active sync.WaitGroup
	defer active.Wait()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			for {
				select {
				case sem <- struct{}{}:
				default:
					goto nextTick
				}
				task, err := r.deploy.ClaimNextReadyTask(ctx, r.cfg.WorkerID, r.cfg.LeaseTTL)
				if err != nil || task == nil {
					<-sem
					goto nextTick
				}
				active.Add(1)
				go func(tsk domain.DeployTask) {
					defer active.Done()
					defer func() { <-sem }()
					r.executeTask(ctx, tsk)
				}(*task)
			}
		nextTick:
		}
	}
}

func (r *Runner) reconcileLoop(ctx context.Context) {
	t := time.NewTicker(r.cfg.ReconcileInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if n, err := r.deploy.ReconcileExpiredLeases(ctx); err == nil && n > 0 {
				r.logger.Warn("reconciled expired task leases", "count", n, "workerId", r.cfg.WorkerID)
			}
			for {
				taken, err := r.deploy.TakeoverReconcilingTask(ctx, r.cfg.WorkerID, r.cfg.LeaseTTL)
				if err != nil || taken == nil {
					break
				}
				go r.executeTask(ctx, *taken)
			}
		}
	}
}

func (r *Runner) nodeWatchdogLoop(ctx context.Context) {
	t := time.NewTicker(r.cfg.WatchdogInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if n, err := r.nodes.SweepHealth(ctx); err == nil && n > 0 {
				r.logger.Info("updated degraded/offline node health states", "count", n)
			}
		}
	}
}

func (r *Runner) executeTask(ctx context.Context, task domain.DeployTask) {
	scope := domain.Scope{TenantID: task.TenantID, AppID: task.AppID}
	renewCtx, cancelRenew := context.WithCancel(ctx)
	defer cancelRenew()

	go func() {
		ticker := time.NewTicker(r.cfg.LeaseRenewEvery)
		defer ticker.Stop()
		for {
			select {
			case <-renewCtx.Done():
				return
			case <-ticker.C:
				if err := r.deploy.RenewTaskLease(renewCtx, scope, task.JobID, task.TaskID, r.cfg.WorkerID, task.LeaseEpoch, r.cfg.LeaseTTL); err != nil {
					r.logger.Warn("lease renewal fenced", "taskId", task.TaskID, "leaseEpoch", task.LeaseEpoch, "err", err)
					return
				}
			}
		}
	}()

	res := r.provider.Execute(ctx, task)
	payloadBytes, _ := json.Marshal(res.Payload)
	if _, err := r.deploy.ApplyReceipt(ctx, domain.DeployReceipt{
		TenantID:    task.TenantID,
		AppID:       task.AppID,
		Provider:    res.Provider,
		ReceiptID:   res.ReceiptID,
		JobID:       task.JobID,
		TaskID:      task.TaskID,
		AttemptNo:   task.AttemptNo,
		LeaseEpoch:  task.LeaseEpoch,
		Status:      res.Status,
		PayloadJSON: string(payloadBytes),
	}); err != nil {
		r.logger.Error("apply task receipt failed", "jobId", task.JobID, "taskId", task.TaskID, "err", fmt.Sprint(err))
	}
}

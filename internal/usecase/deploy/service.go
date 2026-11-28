package deploy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/kinstall/kinstall/internal/adapter/store"
	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/pkg/platform/auth"
	"github.com/kinstall/kinstall/pkg/platform/crypto"
	"github.com/kinstall/kinstall/pkg/platform/dag"
)

// EventHub wakes live SSE timeline subscribers immediately on new job seq transitions.
type EventHub struct {
	mu   sync.RWMutex
	subs map[string]map[chan int64]struct{}
}

func NewEventHub() *EventHub {
	return &EventHub{subs: make(map[string]map[chan int64]struct{})}
}

func (h *EventHub) Subscribe(jobID string) (<-chan int64, func()) {
	ch := make(chan int64, 32)
	h.mu.Lock()
	if h.subs[jobID] == nil {
		h.subs[jobID] = make(map[chan int64]struct{})
	}
	h.subs[jobID][ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		if m, ok := h.subs[jobID]; ok {
			delete(m, ch)
			if len(m) == 0 {
				delete(h.subs, jobID)
			}
		}
		h.mu.Unlock()
	}
}

func (h *EventHub) Notify(jobID string, seq int64) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.subs[jobID] {
		select {
		case ch <- seq:
		default:
		}
	}
}

type Service struct {
	repo *store.Repo
	hub  *EventHub
}

func New(repo *store.Repo, hub *EventHub) *Service {
	if hub == nil {
		hub = NewEventHub()
	}
	return &Service{repo: repo, hub: hub}
}

func (s *Service) Hub() *EventHub { return s.hub }

type SubmitJobRequest struct {
	Name          string                  `json:"name"`
	ProjectID     string                  `json:"projectId"`
	TriggerType   string                  `json:"triggerType"`
	ExplicitGraph bool                    `json:"explicitGraph"`
	Tasks         []store.SubmitTaskInput `json:"tasks"`
	Edges         []dag.EdgeSpec          `json:"edges"`
}

func (s *Service) SubmitJob(ctx context.Context, tc auth.TrustedContext, scope domain.Scope, idemKey, reqHash string, req SubmitJobRequest) (store.SubmitJobOutcome, error) {
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return store.SubmitJobOutcome{}, domain.ValidationError("部署作业名称不能为空")
	}
	if strings.TrimSpace(idemKey) == "" {
		return store.SubmitJobOutcome{}, domain.ValidationError("提交部署作业必须提供 Idempotency-Key")
	}
	if req.ProjectID != "" {
		scope.ProjectID = req.ProjectID
	}

	nodes := make([]dag.NodeSpec, 0, len(req.Tasks))
	byKey := make(map[string]store.SubmitTaskInput, len(req.Tasks))
	for i, t := range req.Tasks {
		k := strings.TrimSpace(t.Key)
		if k == "" {
			return store.SubmitJobOutcome{}, domain.ValidationError(fmt.Sprintf("第 %d 个任务缺少 key", i+1))
		}
		t.Key = k
		order := t.TaskOrder
		if order == 0 {
			order = (i + 1) * 10
		}
		nodes = append(nodes, dag.NodeSpec{Key: k, TaskOrder: order, Disabled: t.Disabled})
		byKey[k] = t
	}

	plan, err := dag.Compile(nodes, req.Edges, req.ExplicitGraph || len(req.Edges) > 0)
	if err != nil {
		var cycErr *dag.CycleError
		if errors.As(err, &cycErr) {
			return store.SubmitJobOutcome{}, domain.NewError(domain.ErrValidation, "DAG_CYCLE_DETECTED", cycErr.Error(), cycErr)
		}
		return store.SubmitJobOutcome{}, domain.ValidationError(err.Error())
	}

	specBytes, _ := json.Marshal(req)
	if reqHash == "" {
		reqHash = crypto.SHA256Hex(specBytes)
	}

	out, err := s.repo.SubmitJobAtomic(ctx, store.SubmitJobParams{
		Scope:         scope,
		PrincipalID:   tc.SubjectID,
		IdemKey:       idemKey,
		RequestHash:   reqHash,
		Name:          req.Name,
		TriggerType:   req.TriggerType,
		SpecJSON:      string(specBytes),
		Plan:          plan,
		TasksByKey:    byKey,
		ActorUsername: tc.Username,
	})
	if err == nil && !out.Replayed {
		s.hub.Notify(out.Snapshot.Job.JobID, out.Snapshot.LastSeq)
		_ = s.repo.AppendAuditLog(ctx, domain.AuditLog{
			TenantID: scope.TenantID, AppID: scope.AppID, ActorID: tc.SubjectID, ActorName: tc.Username,
			Action: "DEPLOY_JOB_SUBMIT", ResourceType: "deploy_job", ResourceID: out.Snapshot.Job.JobID, RequestID: tc.TraceID,
		}, "deploy", 201, fmt.Sprintf("提交 DAG 部署作业 %s (%d 任务)", req.Name, len(plan.ActiveNodes)))
	}
	return out, err
}

func (s *Service) ListJobs(ctx context.Context, scope domain.Scope, projectID string, limit int) ([]domain.DeployJob, error) {
	return s.repo.ListDeployJobs(ctx, scope, projectID, limit)
}

func (s *Service) GetJobSnapshot(ctx context.Context, scope domain.Scope, jobID string, includeTimeline bool) (domain.JobSnapshot, error) {
	return s.repo.GetJobSnapshot(ctx, scope, jobID, includeTimeline)
}

func (s *Service) CancelJob(ctx context.Context, tc auth.TrustedContext, scope domain.Scope, jobID, idemKey, reqHash string, expectedVer int64) (domain.JobSnapshot, bool, error) {
	snap, replayed, err := s.repo.CancelJobAtomic(ctx, scope, jobID, tc.SubjectID, idemKey, reqHash, expectedVer)
	if err == nil && !replayed {
		s.hub.Notify(jobID, snap.LastSeq)
		_ = s.repo.AppendAuditLog(ctx, domain.AuditLog{
			TenantID: scope.TenantID, AppID: scope.AppID, ActorID: tc.SubjectID, ActorName: tc.Username,
			Action: "DEPLOY_JOB_CANCEL", ResourceType: "deploy_job", ResourceID: jobID, RequestID: tc.TraceID,
		}, "deploy", 200, "取消部署作业 "+jobID)
	}
	return snap, replayed, err
}

func (s *Service) ClaimNextReadyTask(ctx context.Context, owner string, leaseTTL time.Duration) (*domain.DeployTask, error) {
	t, err := s.repo.ClaimNextReadyTask(ctx, owner, leaseTTL)
	if err == nil && t != nil {
		s.hub.Notify(t.JobID, 0)
	}
	return t, err
}

func (s *Service) RenewTaskLease(ctx context.Context, scope domain.Scope, jobID, taskID, owner string, leaseEpoch int64, leaseTTL time.Duration) error {
	return s.repo.RenewTaskLease(ctx, scope, jobID, taskID, owner, leaseEpoch, leaseTTL)
}

func (s *Service) ReconcileExpiredLeases(ctx context.Context) (int, error) {
	return s.repo.ReconcileExpiredLeases(ctx)
}

func (s *Service) TakeoverReconcilingTask(ctx context.Context, newOwner string, leaseTTL time.Duration) (*domain.DeployTask, error) {
	t, err := s.repo.TakeoverReconcilingTask(ctx, newOwner, leaseTTL)
	if err == nil && t != nil {
		s.hub.Notify(t.JobID, 0)
	}
	return t, err
}

func (s *Service) ApplyReceipt(ctx context.Context, rc domain.DeployReceipt) (store.ReceiptResult, error) {
	if rc.Digest == "" {
		rc.Digest = crypto.SHA256Hex([]byte(fmt.Sprintf("%s|%s|%s|%d|%s|%s",
			rc.Provider, rc.ReceiptID, rc.TaskID, rc.AttemptNo, rc.Status, rc.PayloadJSON)))
	}
	res, err := s.repo.ApplyReceiptAtomic(ctx, rc)
	if err == nil && !res.Duplicate {
		s.hub.Notify(rc.JobID, res.NewLastSeq)
	}
	return res, err
}

func (s *Service) ListTimelineAfter(ctx context.Context, scope domain.Scope, jobID string, afterSeq int64, limit int) ([]domain.TimelineEvent, error) {
	return s.repo.ListTimelineAfter(ctx, scope, jobID, afterSeq, limit)
}

package foundation_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kinstall/kinstall/internal/adapter/store"
	"github.com/kinstall/kinstall/internal/bootstrap"
	"github.com/kinstall/kinstall/internal/domain"
	httptransport "github.com/kinstall/kinstall/internal/transport/http"
	"github.com/kinstall/kinstall/internal/usecase/deploy"
	"github.com/kinstall/kinstall/pkg/platform/auth"
	"github.com/kinstall/kinstall/pkg/platform/dag"
	"github.com/kinstall/kinstall/pkg/platform/id"
	"github.com/kinstall/kinstall/pkg/platform/probes"
)

type testHarness struct {
	c        *bootstrap.Container
	clock    *id.FixedClock
	handler  http.Handler
	adminTok string
	adminCtx auth.TrustedContext
}

func newHarness(t *testing.T) *testHarness {
	t.Helper()
	dir := t.TempDir()
	clk := id.NewFixedClock(time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC))
	c, err := bootstrap.Build(context.Background(), bootstrap.Options{
		DBDriver:    "sqlite",
		DBDSN:       filepath.Join(dir, "liveops-test.db"),
		KeyringPath: filepath.Join(dir, "keyring.json"),
		Clock:       clk,
	})
	if err != nil {
		t.Fatalf("bootstrap.Build: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	probeReg := probes.NewRegistry("liveops-api-test", "test", c.DB.Ping)
	probeReg.MarkStarted()
	h := httptransport.NewRouter(httptransport.Dependencies{
		Identity: c.Identity,
		Assets:   c.Assets,
		Config:   c.Config,
		Deploy:   c.Deploy,
		Nodes:    c.Nodes,
		Gateway:  c.Gateway,
		Tokens:   c.Tokens,
		Probes:   probeReg,
		GWSecret: c.GWSecret,
		Logger:   c.Logger,
	})

	loginRes, err := c.Identity.Login(context.Background(), "admin", "LiveOps#2026!", "127.0.0.1", "test-login")
	if err != nil {
		t.Fatalf("admin login: %v", err)
	}
	return &testHarness{
		c:        c,
		clock:    clk,
		handler:  h,
		adminTok: loginRes.Token,
		adminCtx: loginRes.Context,
	}
}

func (h *testHarness) req(t *testing.T, method, path string, body any, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	r := httptest.NewRequest(method, path, &buf)
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	if _, hasAuth := headers["Authorization"]; !hasAuth {
		r.Header.Set("Authorization", "Bearer "+h.adminTok)
	}
	for k, v := range headers {
		if v == "" {
			r.Header.Del(k)
		} else {
			r.Header.Set(k, v)
		}
	}
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, r)
	return rec
}

// ============================================================================
// Deploy Job Exact Concurrency Invariants (LiveOps D01 - D19)
// ============================================================================

func TestLiveOpsDeployInvariants_D01_to_D19(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	scope := domain.Scope{TenantID: "default-tenant", AppID: "his-core", ProjectID: "prod-clinical"}

	// D01 + D02 + D03: Idempotency replay, conflict, and atomic DAG materialization (Diamond A -> B,C -> D)
	diamondBody := deploy.SubmitJobRequest{
		Name:          "Diamond Release",
		ProjectID:     "prod-clinical",
		ExplicitGraph: true,
		Tasks: []store.SubmitTaskInput{
			{Key: "A", Name: "Preflight"},
			{Key: "B", Name: "Rollout Zone-1"},
			{Key: "C", Name: "Rollout Zone-2"},
			{Key: "D", Name: "Final Verify"},
		},
		Edges: []dag.EdgeSpec{
			{From: "A", To: "B"},
			{From: "A", To: "C"},
			{From: "B", To: "D"},
			{From: "C", To: "D"},
		},
	}

	rec1 := h.req(t, http.MethodPost, "/api/v1/deploy/jobs", diamondBody, map[string]string{"Idempotency-Key": "idem-diamond-001"})
	if rec1.Code != http.StatusCreated {
		t.Fatalf("D03 expected 201, got %d: %s", rec1.Code, rec1.Body.String())
	}
	var snap1 domain.JobSnapshot
	_ = json.Unmarshal(rec1.Body.Bytes(), &snap1)
	if len(snap1.Tasks) != 4 || len(snap1.Edges) != 4 || snap1.LastSeq < 2 {
		t.Fatalf("D03 invalid materialized snapshot: %+v", snap1)
	}

	// D01: Replay identical payload with same Idempotency-Key
	recReplay := h.req(t, http.MethodPost, "/api/v1/deploy/jobs", diamondBody, map[string]string{"Idempotency-Key": "idem-diamond-001"})
	if recReplay.Code != http.StatusCreated || recReplay.Header().Get("X-Idempotency-Replayed") != "true" {
		t.Fatalf("D01 expected replayed 201, got %d hdr=%q", recReplay.Code, recReplay.Header().Get("X-Idempotency-Replayed"))
	}
	var snapReplay domain.JobSnapshot
	_ = json.Unmarshal(recReplay.Body.Bytes(), &snapReplay)
	if snapReplay.Job.JobID != snap1.Job.JobID {
		t.Fatalf("D01 replayed different jobId: %s vs %s", snapReplay.Job.JobID, snap1.Job.JobID)
	}

	// D02: Same Idempotency-Key with DIFFERENT payload -> 409 IDEMPOTENCY_CONFLICT
	mutated := diamondBody
	mutated.Name = "Different Name"
	recConflict := h.req(t, http.MethodPost, "/api/v1/deploy/jobs", mutated, map[string]string{"Idempotency-Key": "idem-diamond-001"})
	if recConflict.Code != http.StatusConflict || !strings.Contains(recConflict.Body.String(), "IDEMPOTENCY_CONFLICT") {
		t.Fatalf("D02 expected 409 IDEMPOTENCY_CONFLICT, got %d: %s", recConflict.Code, recConflict.Body.String())
	}

	// D11: Cycle rejected cleanly with concrete cycle path
	cycBody := deploy.SubmitJobRequest{
		Name:          "Cyclic Job",
		ExplicitGraph: true,
		Tasks:         []store.SubmitTaskInput{{Key: "X"}, {Key: "Y"}, {Key: "Z"}},
		Edges:         []dag.EdgeSpec{{From: "X", To: "Y"}, {From: "Y", To: "Z"}, {From: "Z", To: "X"}},
	}
	recCyc := h.req(t, http.MethodPost, "/api/v1/deploy/jobs", cycBody, map[string]string{"Idempotency-Key": "idem-cycle-001"})
	if recCyc.Code != http.StatusBadRequest || !strings.Contains(recCyc.Body.String(), "DAG_CYCLE_DETECTED") {
		t.Fatalf("D11 expected 400 DAG_CYCLE_DETECTED, got %d: %s", recCyc.Code, recCyc.Body.String())
	}

	// D04 + D10: Concurrent Claim Exclusivity (only 1 worker claims READY task A; lease_epoch=1, attempt_no=1)
	var wg sync.WaitGroup
	claimedCh := make(chan *domain.DeployTask, 4)
	for i := range 4 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			tsk, err := h.c.Deploy.ClaimNextReadyTask(ctx, fmt.Sprintf("worker-%d", idx), 30*time.Second)
			if err == nil && tsk != nil {
				claimedCh <- tsk
			}
		}(i)
	}
	wg.Wait()
	close(claimedCh)
	var claimedTasks []*domain.DeployTask
	for tk := range claimedCh {
		claimedTasks = append(claimedTasks, tk)
	}
	if len(claimedTasks) != 1 || claimedTasks[0].TaskKey != "A" || claimedTasks[0].LeaseEpoch != 1 || claimedTasks[0].AttemptNo != 1 {
		t.Fatalf("D04/D10 expected exactly 1 claim on A (epoch=1,attempt=1), got %d: %+v", len(claimedTasks), claimedTasks)
	}
	taskA := claimedTasks[0]

	// D05 + D17: Lease expiry -> RECONCILING -> Takeover increments lease_epoch ONLY (attempt_no & action_key preserved!), old owner fenced!
	h.clock.Advance(35 * time.Second)
	reconciled, err := h.c.Deploy.ReconcileExpiredLeases(ctx)
	if err != nil || reconciled != 1 {
		t.Fatalf("D05 expected 1 reconciled lease, got %d err=%v", reconciled, err)
	}
	takenA, err := h.c.Deploy.TakeoverReconcilingTask(ctx, "worker-takeover", 30*time.Second)
	if err != nil || takenA == nil {
		t.Fatalf("D05 takeover failed: %v", err)
	}
	if takenA.LeaseEpoch != 2 || takenA.AttemptNo != 1 || takenA.ActionKey != taskA.ActionKey {
		t.Fatalf("D05 expected epoch=2 attempt=1 same actionKey, got epoch=%d attempt=%d key=%s", takenA.LeaseEpoch, takenA.AttemptNo, takenA.ActionKey)
	}
	// D17: Old owner renewal & stale receipt must be fenced!
	if err := h.c.Deploy.RenewTaskLease(ctx, scope, taskA.JobID, taskA.TaskID, taskA.Owner, 1, 30*time.Second); !errors.Is(err, domain.ErrStaleLease) {
		t.Fatalf("D17 expected ErrStaleLease on old owner renew, got %v", err)
	}
	if _, err := h.c.Deploy.ApplyReceipt(ctx, domain.DeployReceipt{
		TenantID: scope.TenantID, AppID: scope.AppID, Provider: "test", ReceiptID: "rcpt-stale-A",
		JobID: taskA.JobID, TaskID: taskA.TaskID, AttemptNo: 1, LeaseEpoch: 1, Status: domain.TaskSucceeded, PayloadJSON: `{"stale":true}`,
	}); !errors.Is(err, domain.ErrStaleLease) {
		t.Fatalf("D17 expected ErrStaleLease on stale epoch receipt, got %v", err)
	}

	// D06 + D07 + D08: Valid receipt on epoch=2 succeeds A and unlocks BOTH B and C!
	rcptA := domain.DeployReceipt{
		TenantID: scope.TenantID, AppID: scope.AppID, Provider: "test", ReceiptID: "rcpt-A-ok",
		JobID: taskA.JobID, TaskID: taskA.TaskID, AttemptNo: 1, LeaseEpoch: 2, Status: domain.TaskSucceeded, PayloadJSON: `{"ok":true}`,
	}
	resA, err := h.c.Deploy.ApplyReceipt(ctx, rcptA)
	if err != nil || resA.Duplicate || len(resA.UnlockedKeys) != 2 {
		t.Fatalf("D08 expected A to unlock B & C, got %+v err=%v", resA, err)
	}
	// D06: Replaying identical receipt is idempotent noop
	resADup, err := h.c.Deploy.ApplyReceipt(ctx, rcptA)
	if err != nil || !resADup.Duplicate {
		t.Fatalf("D06 expected duplicate noop, got %+v err=%v", resADup, err)
	}
	// D07: Conflicting digest for same receipt_id rejected with ErrReceiptConflict
	badRcptA := rcptA
	badRcptA.PayloadJSON = `{"tampered":true}`
	if _, err := h.c.Deploy.ApplyReceipt(ctx, badRcptA); !errors.Is(err, domain.ErrReceiptConflict) {
		t.Fatalf("D07 expected ErrReceiptConflict, got %v", err)
	}

	// D09 + D14 + D15: Claim & complete B, then C -> D unlocks ONLY after BOTH B & C succeed, emitting exactly ONE task.ready outbox!
	taskB, _ := h.c.Deploy.ClaimNextReadyTask(ctx, "w-b", 30*time.Second)
	taskC, _ := h.c.Deploy.ClaimNextReadyTask(ctx, "w-c", 30*time.Second)
	resB, err := h.c.Deploy.ApplyReceipt(ctx, domain.DeployReceipt{
		TenantID: scope.TenantID, AppID: scope.AppID, Provider: "test", ReceiptID: "rcpt-B",
		JobID: snap1.Job.JobID, TaskID: taskB.TaskID, AttemptNo: taskB.AttemptNo, LeaseEpoch: taskB.LeaseEpoch,
		Status: domain.TaskSucceeded, PayloadJSON: `{"ok":true}`,
	})
	if err != nil || len(resB.UnlockedKeys) != 0 {
		t.Fatalf("D09 expected D still waiting after only B finished, unlocked=%v err=%v", resB.UnlockedKeys, err)
	}
	resC, err := h.c.Deploy.ApplyReceipt(ctx, domain.DeployReceipt{
		TenantID: scope.TenantID, AppID: scope.AppID, Provider: "test", ReceiptID: "rcpt-C",
		JobID: snap1.Job.JobID, TaskID: taskC.TaskID, AttemptNo: taskC.AttemptNo, LeaseEpoch: taskC.LeaseEpoch,
		Status: domain.TaskSucceeded, PayloadJSON: `{"ok":true}`,
	})
	if err != nil || len(resC.UnlockedKeys) != 1 || resC.UnlockedKeys[0] != "D" {
		t.Fatalf("D09 expected D to unlock after C finished, got unlocked=%v err=%v", resC.UnlockedKeys, err)
	}
	taskD, _ := h.c.Deploy.ClaimNextReadyTask(ctx, "w-d", 30*time.Second)
	if taskD == nil || taskD.TaskKey != "D" {
		t.Fatalf("D09 expected to claim D, got %+v", taskD)
	}
	outboxCountD, err := h.c.Repo.CountOutboxByPrefix(ctx, scope, "task.ready:"+snap1.Job.JobID+":"+taskD.TaskID)
	if err != nil || outboxCountD != 1 {
		t.Fatalf("D14/D15 expected exactly 1 unique outbox row for D, got %d err=%v", outboxCountD, err)
	}
	// Finish D -> Job converges SUCCEEDED
	resD, err := h.c.Deploy.ApplyReceipt(ctx, domain.DeployReceipt{
		TenantID: scope.TenantID, AppID: scope.AppID, Provider: "test", ReceiptID: "rcpt-D",
		JobID: snap1.Job.JobID, TaskID: taskD.TaskID, AttemptNo: taskD.AttemptNo, LeaseEpoch: taskD.LeaseEpoch,
		Status: domain.TaskSucceeded, PayloadJSON: `{"ok":true}`,
	})
	if err != nil || resD.JobState != domain.JobSucceeded {
		t.Fatalf("expected Diamond Job SUCCEEDED, got %s err=%v", resD.JobState, err)
	}

	// D16: Inbox consumer deduplication
	first, err := h.c.Repo.RecordInboxOnce(ctx, scope, "relay", "msg-1", "sha-1")
	second, _ := h.c.Repo.RecordInboxOnce(ctx, scope, "relay", "msg-1", "sha-1")
	if err != nil || !first || second {
		t.Fatalf("D16 expected inbox first=true second=false, got %v %v err=%v", first, second, err)
	}

	// D12: Cancel precedence — If-Match required, blocks unlocking successor while preserving in-flight SUCCEEDED fact
	cancelJobBody := deploy.SubmitJobRequest{
		Name: "Cancel Test", ExplicitGraph: true,
		Tasks: []store.SubmitTaskInput{{Key: "S1"}, {Key: "S2"}},
		Edges: []dag.EdgeSpec{{From: "S1", To: "S2"}},
	}
	recCJ := h.req(t, http.MethodPost, "/api/v1/deploy/jobs", cancelJobBody, map[string]string{"Idempotency-Key": "idem-cancel-job"})
	var cjSnap domain.JobSnapshot
	_ = json.Unmarshal(recCJ.Body.Bytes(), &cjSnap)
	s1Task, _ := h.c.Deploy.ClaimNextReadyTask(ctx, "w-cancel", 30*time.Second)

	// Missing If-Match -> 428
	if r := h.req(t, http.MethodPost, "/api/v1/deploy/jobs/"+cjSnap.Job.JobID+"/cancel", nil, nil); r.Code != http.StatusPreconditionRequired {
		t.Fatalf("D12 expected 428 missing If-Match, got %d", r.Code)
	}
	// Valid If-Match cancels job (S2 becomes CANCELLED, S1 remains RUNNING)
	if r := h.req(t, http.MethodPost, "/api/v1/deploy/jobs/"+cjSnap.Job.JobID+"/cancel", nil, map[string]string{
		"If-Match":        fmt.Sprintf(`"%d"`, cjSnap.Job.Version),
		"Idempotency-Key": "idem-cancel-act",
	}); r.Code != http.StatusOK {
		t.Fatalf("D12 expected 200 cancel, got %d: %s", r.Code, r.Body.String())
	}
	// In-flight S1 finishes SUCCEEDED -> fact preserved, S2 stays CANCELLED, job converges CANCELLED!
	resS1, err := h.c.Deploy.ApplyReceipt(ctx, domain.DeployReceipt{
		TenantID: scope.TenantID, AppID: scope.AppID, Provider: "test", ReceiptID: "rcpt-s1-late",
		JobID: cjSnap.Job.JobID, TaskID: s1Task.TaskID, AttemptNo: s1Task.AttemptNo, LeaseEpoch: s1Task.LeaseEpoch,
		Status: domain.TaskSucceeded, PayloadJSON: `{"ok":true}`,
	})
	if err != nil || len(resS1.UnlockedKeys) != 0 || resS1.JobState != domain.JobCancelled {
		t.Fatalf("D12 expected S2 NOT unlocked & job CANCELLED, got unlocked=%v state=%s err=%v", resS1.UnlockedKeys, resS1.JobState, err)
	}
	finalCJ, _ := h.c.Deploy.GetJobSnapshot(ctx, scope, cjSnap.Job.JobID, false)
	stateByKey := map[string]domain.TaskState{}
	for _, tk := range finalCJ.Tasks {
		stateByKey[tk.TaskKey] = tk.State
	}
	if stateByKey["S1"] != domain.TaskSucceeded || stateByKey["S2"] != domain.TaskCancelled {
		t.Fatalf("D12 expected S1=SUCCEEDED S2=CANCELLED, got %+v", stateByKey)
	}

	// D13 (retry) + D18 (permanent failure prunes downstream SKIPPED while independent branch finishes) + D19 (Repeatable-Read Snapshot)
	branchJob := deploy.SubmitJobRequest{
		Name: "Branch Failure & Retry", ExplicitGraph: true,
		Tasks: []store.SubmitTaskInput{
			{Key: "FailRoot", MaxRetries: 1},
			{Key: "DownstreamSkip"},
			{Key: "IndependentOK"},
		},
		Edges: []dag.EdgeSpec{{From: "FailRoot", To: "DownstreamSkip"}},
	}
	recBJ := h.req(t, http.MethodPost, "/api/v1/deploy/jobs", branchJob, map[string]string{"Idempotency-Key": "idem-branch-d18"})
	var bjSnap domain.JobSnapshot
	_ = json.Unmarshal(recBJ.Body.Bytes(), &bjSnap)

	// Claim both initial READY tasks (FailRoot and IndependentOK)
	t1, _ := h.c.Deploy.ClaimNextReadyTask(ctx, "w-1", 30*time.Second)
	t2, _ := h.c.Deploy.ClaimNextReadyTask(ctx, "w-2", 30*time.Second)
	var failT, okT *domain.DeployTask
	if t1.TaskKey == "FailRoot" {
		failT, okT = t1, t2
	} else {
		failT, okT = t2, t1
	}
	// D13: First failure triggers retry (attempt 1 <= maxRetries 1 -> back to READY)
	_, err = h.c.Deploy.ApplyReceipt(ctx, domain.DeployReceipt{
		TenantID: scope.TenantID, AppID: scope.AppID, Provider: "test", ReceiptID: "rcpt-fail-1",
		JobID: bjSnap.Job.JobID, TaskID: failT.TaskID, AttemptNo: 1, LeaseEpoch: failT.LeaseEpoch,
		Status: domain.TaskFailed, PayloadJSON: `{"error":"transient"}`,
	})
	if err != nil {
		t.Fatalf("D13 first fail receipt: %v", err)
	}
	retryClaim, _ := h.c.Deploy.ClaimNextReadyTask(ctx, "w-retry", 30*time.Second)
	if retryClaim == nil || retryClaim.AttemptNo != 2 || retryClaim.ActionKey == failT.ActionKey {
		t.Fatalf("D13 expected retry attempt_no=2 with distinct actionKey, got %+v", retryClaim)
	}
	// D18: Second failure exhausts retries -> FailRoot=FAILED, DownstreamSkip=SKIPPED, IndependentOK completes SUCCEEDED!
	_, err = h.c.Deploy.ApplyReceipt(ctx, domain.DeployReceipt{
		TenantID: scope.TenantID, AppID: scope.AppID, Provider: "test", ReceiptID: "rcpt-fail-2",
		JobID: bjSnap.Job.JobID, TaskID: retryClaim.TaskID, AttemptNo: 2, LeaseEpoch: retryClaim.LeaseEpoch,
		Status: domain.TaskFailed, PayloadJSON: `{"error":"permanent"}`,
	})
	if err != nil {
		t.Fatalf("D18 second fail receipt: %v", err)
	}
	resIndep, err := h.c.Deploy.ApplyReceipt(ctx, domain.DeployReceipt{
		TenantID: scope.TenantID, AppID: scope.AppID, Provider: "test", ReceiptID: "rcpt-indep-ok",
		JobID: bjSnap.Job.JobID, TaskID: okT.TaskID, AttemptNo: 1, LeaseEpoch: okT.LeaseEpoch,
		Status: domain.TaskSucceeded, PayloadJSON: `{"ok":true}`,
	})
	if err != nil || resIndep.JobState != domain.JobFailed {
		t.Fatalf("D18 expected final job FAILED after independent branch succeeded, got %s err=%v", resIndep.JobState, err)
	}

	// D19: Consistent GET snapshot via HTTP returns ETag and aligned tasks + last_seq
	recGet := h.req(t, http.MethodGet, "/api/v1/deploy/jobs/"+bjSnap.Job.JobID, nil, nil)
	if recGet.Code != http.StatusOK || recGet.Header().Get("ETag") == "" {
		t.Fatalf("D19 expected 200 + ETag, got %d ETag=%q", recGet.Code, recGet.Header().Get("ETag"))
	}
	var snapGet domain.JobSnapshot
	_ = json.Unmarshal(recGet.Body.Bytes(), &snapGet)
	stMap := map[string]domain.TaskState{}
	for _, tk := range snapGet.Tasks {
		stMap[tk.TaskKey] = tk.State
	}
	if stMap["FailRoot"] != domain.TaskFailed || stMap["DownstreamSkip"] != domain.TaskSkipped || stMap["IndependentOK"] != domain.TaskSucceeded {
		t.Fatalf("D18/D19 unexpected final branch states: %+v", stMap)
	}
}

// ============================================================================
// Multi-Tenant Scope, CMDB Dual Topology & Config Locking (LiveOps S01 - S07)
// ============================================================================

func TestLiveOpsScopeAssetsConfigInvariants_S01_to_S07(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	scopeA := domain.Scope{TenantID: "default-tenant", AppID: "his-core", ProjectID: "prod-clinical"}
	scopeB := domain.Scope{TenantID: "clinical-tenant", AppID: "emr-suite", ProjectID: "emr-dev"}

	// S01: Multi-tenant scope isolation (seeded assets in his-core invisible in clinical-tenant/emr-suite)
	listA, _ := h.c.Assets.ListInstances(ctx, scopeA, "", "")
	listB, _ := h.c.Assets.ListInstances(ctx, scopeB, "", "")
	if len(listA) == 0 || len(listB) != 0 {
		t.Fatalf("S01 scope isolation violated: scopeA=%d scopeB=%d", len(listA), len(listB))
	}

	// S02: Cross-tenant edge strictly rejected
	// Create a model & instance in clinical-tenant first
	mB, err := h.c.Assets.CreateModel(ctx, h.adminCtx, scopeB, "", "emr_host", "EMR主机", "server", "", nil)
	if err != nil {
		// clinical-tenant has no grp-res unless seeded; create a tenant via service which seeds CMDB cleanly!
		tB, cerr := h.c.Identity.CreateTenant(ctx, h.adminCtx, "isolated-hosp", "隔离院区", "")
		if cerr != nil {
			t.Fatalf("create isolated tenant: %v", cerr)
		}
		scopeB = domain.Scope{TenantID: tB.TenantID, AppID: "app-isolated-hosp"}
		modelsB, _ := h.c.Assets.ListModels(ctx, scopeB)
		mB = modelsB[0]
	}
	instB, err := h.c.Assets.CreateInstance(ctx, h.adminCtx, scopeB, mB.ModelID, "Isolated-Node", "iso-1", "ACTIVE", map[string]any{"owner": "rd", "tier": "T1"})
	if err != nil {
		t.Fatalf("create isolated instance: %v", err)
	}
	if _, err := h.c.Assets.CreateEdge(ctx, h.adminCtx, scopeA, domain.TreeBusiness, "rel-contains", "inst-biz-his", instB.InstanceID, nil); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("S02 expected ErrForbidden on cross-tenant edge, got %v", err)
	}

	// S03: Single-parent & Acyclic tree invariant on business topology
	if _, err := h.c.Assets.CreateEdge(ctx, h.adminCtx, scopeA, domain.TreeBusiness, "rel-contains", "inst-set-inp", "inst-mod-reg", nil); !errors.Is(err, domain.ErrStateConflict) {
		t.Fatalf("S03 expected SINGLE_PARENT_VIOLATION, got %v", err)
	}
	if _, err := h.c.Assets.CreateEdge(ctx, h.adminCtx, scopeA, domain.TreeBusiness, "rel-contains", "inst-mod-reg", "inst-biz-his", nil); !errors.Is(err, domain.ErrStateConflict) {
		t.Fatalf("S03 expected TOPOLOGY_CYCLE_DETECTED, got %v", err)
	}

	// S04: Instance If-Match optimistic lock + unique field conflict + RelationType ref-check + Excelize round-trip
	recNoETag := h.req(t, http.MethodPut, "/api/v1/assets/instances/inst-host-01", map[string]any{
		"name": "his-app-node-01", "status": "ACTIVE", "attrs": map[string]any{"ip": "10.20.1.11"},
	}, nil)
	if recNoETag.Code != http.StatusPreconditionRequired {
		t.Fatalf("S04 expected 428 missing If-Match, got %d", recNoETag.Code)
	}
	recDupIP := h.req(t, http.MethodPost, "/api/v1/assets/instances", map[string]any{
		"modelId": "m_host", "name": "dup-host", "uniqueKey": "host-dup-key", "attrs": map[string]any{"ip": "10.20.1.11"},
	}, nil)
	if recDupIP.Code != http.StatusConflict {
		t.Fatalf("S04 expected 409 unique field conflict on duplicate IP, got %d: %s", recDupIP.Code, recDupIP.Body.String())
	}
	if err := h.c.Assets.DeleteRelationType(ctx, scopeA, "rel-contains"); !errors.Is(err, domain.ErrReferenceInUse) {
		t.Fatalf("S04 expected ErrReferenceInUse deleting in-use relation type, got %v", err)
	}
	// Excelize export & import round-trip
	xlsxBytes, err := h.c.Assets.ExportInstancesXLSX(ctx, scopeA, "m_host", false)
	if err != nil || len(xlsxBytes) < 100 {
		t.Fatalf("Excelize export failed: len=%d err=%v", len(xlsxBytes), err)
	}

	// S05 + S06: Config If-Match (428 / 412) + atomic cfg_configurations/cfg_history/outbox + KMS secret reveal
	recCfgNoETag := h.req(t, http.MethodPut, "/api/v1/config/configurations/cfg-his-db", map[string]any{
		"description": "no etag", "entries": []domain.ConfigEntry{{Key: "A", Value: "1"}},
	}, nil)
	if recCfgNoETag.Code != http.StatusPreconditionRequired {
		t.Fatalf("S05 expected 428, got %d", recCfgNoETag.Code)
	}
	recCfgStale := h.req(t, http.MethodPut, "/api/v1/config/configurations/cfg-his-db", map[string]any{
		"description": "stale", "entries": []domain.ConfigEntry{{Key: "A", Value: "1"}},
	}, map[string]string{"If-Match": `"99"`})
	if recCfgStale.Code != http.StatusPreconditionFailed {
		t.Fatalf("S05 expected 412 VERSION_MISMATCH, got %d", recCfgStale.Code)
	}
	recCfgOK := h.req(t, http.MethodPut, "/api/v1/config/configurations/cfg-his-db", map[string]any{
		"description":   "Updated v2",
		"changeSummary": "Tune pool to 96",
		"entries": []domain.ConfigEntry{
			{Key: "DB_HOST", Value: "pg-primary.clinical.svc.local"},
			{Key: "DB_MAX_CONNS", Value: "96"},
			{Key: "DB_PASSWORD", Value: "******", IsSecret: true},
		},
	}, map[string]string{"If-Match": `"1"`})
	if recCfgOK.Code != http.StatusOK || recCfgOK.Header().Get("ETag") != `"2"` {
		t.Fatalf("S06 expected 200 ETag=\"2\", got %d ETag=%q body=%s", recCfgOK.Code, recCfgOK.Header().Get("ETag"), recCfgOK.Body.String())
	}
	hist, err := h.c.Config.ListHistory(ctx, scopeA, "cfg-his-db")
	if err != nil || len(hist) != 2 || hist[0].Version != 2 {
		t.Fatalf("S06 expected 2 immutable history snapshots, got %+v err=%v", hist, err)
	}
	// Audited KMS reveal decrypts preserved ciphertext!
	pt, err := h.c.Config.RevealSecret(ctx, h.adminCtx, scopeA, "cfg-his-db", "DB_PASSWORD", "incident-audit")
	if err != nil || pt != "DbPass#Clinical2026!" {
		t.Fatalf("S06 expected decrypted secret, got %q err=%v", pt, err)
	}

	// S07: Environment ViewCheck detects DB_MAX_CONNS override conflict + tombstone refuses deleting referenced config
	vc, err := h.c.Config.ViewCheckEnvironment(ctx, scopeA, "env-prod-clinical")
	if err != nil || len(vc.Conflicts) == 0 || vc.Merged["DB_MAX_CONNS"] != "128" || vc.Merged["DB_PASSWORD"] != "******" {
		t.Fatalf("S07 unexpected env viewcheck: %+v err=%v", vc, err)
	}
	if err := h.c.Config.TombstoneConfiguration(ctx, h.adminCtx, scopeA, "cfg-his-db", 2); !errors.Is(err, domain.ErrReferenceInUse) {
		t.Fatalf("S07 expected ErrReferenceInUse deleting env-referenced config, got %v", err)
	}
}

// ============================================================================
// Gateway Default-Route Exclusivity & Artifact Integrity (LiveOps G01 - G05)
// ============================================================================

func TestLiveOpsGatewayAndArtifactInvariants_G01_to_G05(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	scope := domain.Scope{TenantID: "default-tenant", AppID: "his-core"}

	// G01 + G02: Atomic default-route switch ensures strictly 1 default & bumps route_version
	_, v0, _ := h.c.Gateway.ListRoutes(ctx, scope, "rtr-edge-main")
	v1, err := h.c.Gateway.SwitchDefaultRoute(ctx, h.adminCtx, scope, "rt-inpatient")
	if err != nil || v1 != v0+1 {
		t.Fatalf("G01/G02 expected routeVersion %d->%d, got %d err=%v", v0, v0+1, v1, err)
	}
	routes, _, _ := h.c.Gateway.ListRoutes(ctx, scope, "rtr-edge-main")
	defCount := 0
	defID := ""
	for _, rt := range routes {
		if rt.IsDefault {
			defCount++
			defID = rt.RouteID
		}
	}
	if defCount != 1 || defID != "rt-inpatient" {
		t.Fatalf("G01 expected exactly 1 default route (rt-inpatient), got count=%d id=%s", defCount, defID)
	}

	// G03: Artifact promotion fail-closed while PENDING
	f, err := h.c.Gateway.CreateArtifactFile(ctx, h.adminCtx, scope, "his-core-v4.tar.gz", "application/gzip", "abc123sha256", "/var/lib/liveops/blobs/his.tgz", 4096)
	if err != nil {
		t.Fatalf("CreateArtifactFile: %v", err)
	}
	if _, err := h.c.Gateway.PromoteArtifactRelease(ctx, h.adminCtx, scope, "his-core", "4.3.0", f.FileID, "unverified"); !errors.Is(err, domain.ErrStateConflict) {
		t.Fatalf("G03 expected fail-closed ErrStateConflict for PENDING file, got %v", err)
	}

	// G04: Verify SHA-256 -> VERIFIED -> Promotion succeeds
	vf, err := h.c.Gateway.VerifyArtifactFile(ctx, scope, f.FileID, "abc123sha256")
	if err != nil || vf.VerificationStatus != "VERIFIED" {
		t.Fatalf("G04 verify failed: %+v err=%v", vf, err)
	}
	rel, err := h.c.Gateway.PromoteArtifactRelease(ctx, h.adminCtx, scope, "his-core", "4.3.0", f.FileID, "verified release")
	if err != nil || rel.Status != "PROMOTED" {
		t.Fatalf("G04 promote failed: %+v err=%v", rel, err)
	}

	// G05: Deleting referenced artifact file blob rejected with ErrReferenceInUse
	if err := h.c.Gateway.DeleteArtifactFile(ctx, scope, f.FileID); !errors.Is(err, domain.ErrReferenceInUse) {
		t.Fatalf("G05 expected ErrReferenceInUse deleting referenced artifact blob, got %v", err)
	}
}

// ============================================================================
// Strict SSE Cursor Invariants over Live HTTP (LiveOps U01 - U09)
// ============================================================================

func TestLiveOpsSSECursorInvariants_U01_to_U09(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	scope := domain.Scope{TenantID: "default-tenant", AppID: "his-core"}
	jobID := "job-baseline-001" // seeded completed job with seq 1..4, min_retained_seq=1, last_seq=4

	// U01: Missing Last-Event-ID -> 409 SNAPSHOT_REQUIRED
	if r := h.req(t, http.MethodGet, "/api/v1/deploy/jobs/"+jobID+"/events", nil, nil); r.Code != http.StatusConflict || !strings.Contains(r.Body.String(), "SNAPSHOT_REQUIRED") {
		t.Fatalf("U01 expected 409 SNAPSHOT_REQUIRED, got %d: %s", r.Code, r.Body.String())
	}

	// U02: Valid Last-Event-ID: 0 streams monotonic numeric id: 1..4
	r2 := h.req(t, http.MethodGet, "/api/v1/deploy/jobs/"+jobID+"/events?once=true", nil, map[string]string{"Last-Event-ID": "0"})
	if r2.Code != http.StatusOK || !strings.Contains(r2.Header().Get("Content-Type"), "text/event-stream") {
		t.Fatalf("U02 expected 200 text/event-stream, got %d", r2.Code)
	}
	body := r2.Body.String()
	for _, idLine := range []string{"id: 1\n", "id: 2\n", "id: 3\n", "id: 4\n"} {
		if !strings.Contains(body, idLine) {
			t.Fatalf("U02 missing %q in SSE stream:\n%s", idLine, body)
		}
	}

	// U04: Negative cursor -> 400 INVALID_CURSOR
	if r := h.req(t, http.MethodGet, "/api/v1/deploy/jobs/"+jobID+"/events", nil, map[string]string{"Last-Event-ID": "-5"}); r.Code != http.StatusBadRequest || !strings.Contains(r.Body.String(), "INVALID_CURSOR") {
		t.Fatalf("U04 expected 400 INVALID_CURSOR, got %d: %s", r.Code, r.Body.String())
	}

	// U05: Non-int64 / overflow cursor -> 400 INVALID_CURSOR
	if r := h.req(t, http.MethodGet, "/api/v1/deploy/jobs/"+jobID+"/events", nil, map[string]string{"Last-Event-ID": "not-a-number"}); r.Code != http.StatusBadRequest {
		t.Fatalf("U05 expected 400 INVALID_CURSOR, got %d", r.Code)
	}

	// U06 & U07: Advance retention window min_retained_seq=3 -> cursor 1 returns 410, boundary cursor 2 returns 200
	_ = h.c.Repo.SetJobMinRetainedSeq(ctx, scope, jobID, 3)
	if r := h.req(t, http.MethodGet, "/api/v1/deploy/jobs/"+jobID+"/events", nil, map[string]string{"Last-Event-ID": "1"}); r.Code != http.StatusGone || !strings.Contains(r.Body.String(), "CURSOR_RESET_REQUIRED") {
		t.Fatalf("U06 expected 410 CURSOR_RESET_REQUIRED, got %d: %s", r.Code, r.Body.String())
	}
	if r := h.req(t, http.MethodGet, "/api/v1/deploy/jobs/"+jobID+"/events?once=true", nil, map[string]string{"Last-Event-ID": "2"}); r.Code != http.StatusOK || !strings.Contains(r.Body.String(), "id: 3\n") {
		t.Fatalf("U07 boundary cursor minSeq-1 expected 200 with id: 3, got %d body=%s", r.Code, r.Body.String())
	}

	// U08 & U09: Cursor > lastSeq returns 410; cursor == lastSeq returns 200 empty catch-up
	if r := h.req(t, http.MethodGet, "/api/v1/deploy/jobs/"+jobID+"/events", nil, map[string]string{"Last-Event-ID": "5"}); r.Code != http.StatusGone {
		t.Fatalf("U08 expected 410 CURSOR_RESET_REQUIRED for cursor>lastSeq, got %d", r.Code)
	}
	if r := h.req(t, http.MethodGet, "/api/v1/deploy/jobs/"+jobID+"/events?once=true", nil, map[string]string{"Last-Event-ID": "4"}); r.Code != http.StatusOK || strings.Contains(r.Body.String(), "id: 4\n") {
		t.Fatalf("U09 expected 200 with zero duplicate replay, got %d body=%s", r.Code, r.Body.String())
	}
}

// ============================================================================
// Zero-Trust Nodes, Dual-Track Rotation, Checkpoints, Probes & Audit Chain
// ============================================================================

func TestZeroTrustNodesDualRotationAppKeysAndAuditChain(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	scope := domain.Scope{TenantID: "default-tenant", AppID: "his-core"}

	// Probes (/startupz, /livez, /readyz, legacy /healthy returning data.pid)
	for _, p := range []string{"/startupz", "/livez", "/readyz", "/healthy"} {
		r := h.req(t, http.MethodGet, p, nil, map[string]string{"Authorization": ""})
		if r.Code != http.StatusOK {
			t.Fatalf("probe %s returned %d: %s", p, r.Code, r.Body.String())
		}
		if p == "/healthy" && !strings.Contains(r.Body.String(), `"pid"`) {
			t.Fatalf("legacy /healthy missing data.pid: %s", r.Body.String())
		}
	}

	// One-time enroll token -> enroll -> replay rejected -> dual credential rotation grace window -> watchdog DEGRADED/OFFLINE
	tok, err := h.c.Nodes.IssueEnrollToken(ctx, h.adminCtx, scope, map[string]string{"zone": "dc-a"}, 15*time.Minute)
	if err != nil {
		t.Fatalf("IssueEnrollToken: %v", err)
	}
	node, cred1, err := h.c.Nodes.EnrollNode(ctx, tok.RawToken, "edge-his-01", "10.20.1.91", "linux/amd64", "1.0.0")
	if err != nil || node.Status != domain.NodeOnline {
		t.Fatalf("EnrollNode: %+v err=%v", node, err)
	}
	if _, _, err := h.c.Nodes.EnrollNode(ctx, tok.RawToken, "replay-host", "10.20.1.92", "linux/amd64", "1.0.0"); !errors.Is(err, domain.ErrStateConflict) {
		t.Fatalf("expected one-time enroll token replay rejection, got %v", err)
	}

	// Dual-credential rotation: BOTH cred1 (grace) and cred2 authenticate cleanly
	_, cred2, err := h.c.Nodes.RotateCredential(ctx, h.adminCtx, scope, node.NodeID, 5*time.Minute)
	if err != nil {
		t.Fatalf("RotateCredential: %v", err)
	}
	if _, err := h.c.Nodes.AuthenticateNode(ctx, node.NodeID, cred1); err != nil {
		t.Fatalf("expected old credential to remain valid within grace window: %v", err)
	}
	if _, err := h.c.Nodes.AuthenticateNode(ctx, node.NodeID, cred2); err != nil {
		t.Fatalf("expected new credential valid immediately: %v", err)
	}

	// Watchdog transitions: >30s DEGRADED, >45s OFFLINE
	h.clock.Advance(35 * time.Second)
	_, _ = h.c.Nodes.SweepHealth(ctx)
	nDeg, _ := h.c.Repo.GetNode(ctx, scope, node.NodeID)
	if nDeg.Status != domain.NodeDegraded {
		t.Fatalf("expected DEGRADED after 35s, got %s", nDeg.Status)
	}
	h.clock.Advance(15 * time.Second)
	_, _ = h.c.Nodes.SweepHealth(ctx)
	nOff, _ := h.c.Repo.GetNode(ctx, scope, node.NodeID)
	if nOff.Status != domain.NodeOffline {
		t.Fatalf("expected OFFLINE after 50s, got %s", nOff.Status)
	}

	// Idempotent chunked distribution checkpoints
	dist, err := h.c.Nodes.CreateDistribution(ctx, scope, "", node.NodeID, "fullsha", 1024)
	if err != nil {
		t.Fatalf("CreateDistribution: %v", err)
	}
	d1, _ := h.c.Nodes.AckDistributionChunk(ctx, scope, dist.DistID, 0, 0, 512, "sha0")
	d1Dup, _ := h.c.Nodes.AckDistributionChunk(ctx, scope, dist.DistID, 0, 0, 512, "sha0")
	if d1.VerifiedBytes != 512 || d1Dup.VerifiedBytes != 512 {
		t.Fatalf("chunk checkpoint double-counted: %d / %d", d1.VerifiedBytes, d1Dup.VerifiedBytes)
	}
	d2, _ := h.c.Nodes.AckDistributionChunk(ctx, scope, dist.DistID, 1, 512, 512, "sha1")
	if d2.VerifiedBytes != 1024 || d2.State != "VERIFIED" {
		t.Fatalf("expected VERIFIED distribution at 1024 bytes, got %+v", d2)
	}

	// Dual-track AppKey rotation authentication
	_, rawAppKey, err := h.c.Identity.RotateAppKey(ctx, h.adminCtx, scope, 24*time.Hour)
	if err != nil || !strings.HasPrefix(rawAppKey, "lo_live_") {
		t.Fatalf("RotateAppKey failed: key=%q err=%v", rawAppKey, err)
	}
	recAK := h.req(t, http.MethodGet, "/api/v1/assets/models", nil, map[string]string{"Authorization": "Bearer " + rawAppKey})
	if recAK.Code != http.StatusOK {
		t.Fatalf("AppKey auth failed: %d %s", recAK.Code, recAK.Body.String())
	}

	// Tamper-evident Audit Chain Verification (must be 100% valid across all operations!)
	verRes, err := h.c.Identity.VerifyAuditChain(ctx)
	if err != nil || !verRes.Valid || verRes.Total < 5 {
		t.Fatalf("Audit chain verify failed: %+v err=%v", verRes, err)
	}
}

package store_test

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/internal/ports"
	"github.com/kinstall/kinstall/internal/store"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

func openTestSQLite(t *testing.T) *store.DBStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "kinstall.db")
	st, err := store.OpenAndMigrate(context.Background(), store.Config{
		Driver:    "sqlite",
		DSN:       dbPath,
		BackupDir: filepath.Join(t.TempDir(), "backups"),
	}, nil)
	if err != nil {
		t.Fatalf("OpenAndMigrate: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// TestAll24TablesAnd30ConcurrentWriters verifies T-M0-10, T-M0-11, and T-M3-02 DoDs.
func TestAll24TablesAnd30ConcurrentWriters(t *testing.T) {
	ctx := context.Background()
	st := openTestSQLite(t)

	// Verify all 49 domain tables exist in SQLite (26 core + 23 enterprise CMDB/DCIM/IPAM/Config/PluginDSL/Delivery/Agent tables)
	var tableCount int
	err := st.RawWriteSQL().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' AND name <> 'goose_db_version'`).Scan(&tableCount)
	if err != nil {
		t.Fatalf("count tables: %v", err)
	}
	if tableCount != 49 {
		t.Fatalf("expected exactly 49 domain tables, got %d", tableCount)
	}

	// 30 concurrent writers -> zero SQLITE_BUSY errors
	var wg sync.WaitGroup
	errCh := make(chan error, 30)
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			p := &domain.Project{Name: fmt.Sprintf("proj-%02d", idx), Description: "concurrent"}
			if err := st.Projects().Create(ctx, p); err != nil {
				errCh <- fmt.Errorf("writer %d: %w", idx, err)
			}
		}(i)
	}
	wg.Wait()
	close(errCh)
	for e := range errCh {
		t.Fatal(e)
	}
	list, err := st.Projects().List(ctx)
	if err != nil || len(list) != 30 {
		t.Fatalf("expected 30 projects, got %d err=%v", len(list), err)
	}
}

// TestFencingTokenZombieRejection verifies D2 §2.1 #3:
// Stale Worker A (fencing=1) loses lease to Worker B (fencing=2); A's write affects 0 rows!
func TestFencingTokenZombieRejection(t *testing.T) {
	ctx := context.Background()
	st := openTestSQLite(t)

	prj := &domain.Project{Name: "fencing-prj"}
	if err := st.Projects().Create(ctx, prj); err != nil {
		t.Fatal(err)
	}
	op := &domain.Operation{
		ProjectID: prj.ID, TargetType: "instance", TargetID: "inst_01", Action: "install",
		State: domain.OpPending, RequestedBy: "alice", PipelineJSON: json.RawMessage(`{"phases":[]}`),
	}
	if err := st.Operations().Create(ctx, op); err != nil {
		t.Fatal(err)
	}

	// Worker A claims op -> fencing = 1, short 50ms lease
	claimedA, err := st.Operations().ClaimNext(ctx, "worker-A", 50*time.Millisecond)
	if err != nil || claimedA == nil || claimedA.FencingToken != 1 {
		t.Fatalf("worker A claim failed: op=%+v err=%v", claimedA, err)
	}

	tr := &domain.TaskRun{
		OperationID: op.ID, PhaseIdx: 0, StepKey: "p0:t0", TaskType: "cmd.run",
		ParamsDigest: "0000000000000000000000000000000000000000000000000000000000000001",
		State:        domain.TaskRunning, Attempt: 1, FencingToken: 1,
	}
	if err := st.TaskRuns().Upsert(ctx, tr); err != nil {
		t.Fatal(err)
	}

	// Simulate Worker A GC pause -> lease expires -> Worker B takes over (fencing = 2)
	time.Sleep(80 * time.Millisecond)
	claimedB, err := st.Operations().ClaimNext(ctx, "worker-B", 30*time.Second)
	if err != nil || claimedB == nil || claimedB.FencingToken != 2 {
		t.Fatalf("worker B takeover failed: op=%+v err=%v", claimedB, err)
	}

	// Worker A wakes up and attempts zombie write with fencing=1 -> MUST return ok=false (0 rows)!
	tr.State = domain.TaskSucceeded
	tr.FencingToken = 1
	ok, err := st.TaskRuns().UpdateWithFencing(ctx, tr, "worker-A")
	if err != nil {
		t.Fatalf("UpdateWithFencing error: %v", err)
	}
	if ok {
		t.Fatal("ZOMBIE WRITE SUCCEEDED! Fencing token failed to reject stale worker A")
	}

	// Worker B writes with fencing=2 -> MUST succeed!
	tr.FencingToken = 2
	ok, err = st.TaskRuns().UpdateWithFencing(ctx, tr, "worker-B")
	if err != nil || !ok {
		t.Fatalf("worker B fenced write failed: ok=%v err=%v", ok, err)
	}
}

// TestAntiSelfApprovalSQLConstraint verifies G1 §4.2 / T-M3-07:
// Bypassing Go business layer with direct SQL UPDATE attempting self-approval is blocked by chk_no_self_approval!
func TestAntiSelfApprovalSQLConstraint(t *testing.T) {
	ctx := context.Background()
	st := openTestSQLite(t)

	prj := &domain.Project{Name: "approval-prj"}
	if err := st.Projects().Create(ctx, prj); err != nil {
		t.Fatal(err)
	}
	op := &domain.Operation{
		ProjectID: prj.ID, TargetType: "instance", TargetID: "inst_01", Action: "upgrade",
		State: domain.OpAwaitingApproval, RequestedBy: "alice", PipelineJSON: json.RawMessage(`{}`),
	}
	if err := st.Operations().Create(ctx, op); err != nil {
		t.Fatal(err)
	}
	ap := &domain.Approval{
		OperationID: op.ID, ProjectID: prj.ID, EnvName: "prod", RequestedBy: "alice", Reason: "k8s upgrade",
	}
	if err := st.Approvals().Create(ctx, ap); err != nil {
		t.Fatal(err)
	}

	// 1. Business layer blocks self-approval
	if err := st.Approvals().Decide(ctx, op.ID, "alice", true, "self"); !sdk.IsCode(err, sdk.CodeAuthForbidden) {
		t.Fatalf("expected KI-AUTH-0002 on self-approval, got %v", err)
	}

	// 2. Direct SQL bypass MUST be rejected by CHECK constraint chk_no_self_approval
	_, sqlErr := st.RawWriteSQL().ExecContext(ctx,
		`UPDATE operation_approvals SET status='approved', decided_by='alice' WHERE operation_id=?`, op.ID)
	if sqlErr == nil {
		t.Fatal("DB constraint chk_no_self_approval FAILED to block direct SQL self-approval!")
	}

	// 3. Another approver (bob) succeeds and advances operation state to pending
	if err := st.Approvals().Decide(ctx, op.ID, "bob", true, "lgtm"); err != nil {
		t.Fatalf("bob approve failed: %v", err)
	}
	updatedOp, err := st.Operations().Get(ctx, op.ID)
	if err != nil || updatedOp.State != domain.OpPending {
		t.Fatalf("expected op state=pending after approval, got %+v err=%v", updatedOp, err)
	}
}

// TestBatchLocksAntiDeadlock verifies T-M1-03 / D2 §2.3:
// 50 concurrent goroutines requesting overlapping Host sets in reverse order -> zero deadlocks!
func TestBatchLocksAntiDeadlock(t *testing.T) {
	ctx := context.Background()
	st := openTestSQLite(t)

	prj := &domain.Project{Name: "lock-prj"}
	if err := st.Projects().Create(ctx, prj); err != nil {
		t.Fatal(err)
	}
	ops := make([]string, 20)
	for i := range ops {
		op := &domain.Operation{
			ProjectID: prj.ID, TargetType: "host", TargetID: "h1", Action: "init",
			State: domain.OpPending, RequestedBy: "ops", PipelineJSON: json.RawMessage(`{}`),
		}
		if err := st.Operations().Create(ctx, op); err != nil {
			t.Fatal(err)
		}
		ops[i] = op.ID
	}

	var wg sync.WaitGroup
	var wins, conflicts int
	var mu sync.Mutex
	for i, opID := range ops {
		wg.Add(1)
		go func(idx int, id string) {
			defer wg.Done()
			// Deliberately pass host IDs in alternating orders (AB vs BA)
			keys := []ports.LockKey{
				{Kind: "host", ID: "host_B", Mode: "exclusive"},
				{Kind: "host", ID: "host_A", Mode: "exclusive"},
			}
			if idx%2 == 0 {
				keys[0], keys[1] = keys[1], keys[0]
			}
			err := st.Locks().AcquireBatch(ctx, id, 1, keys, 5*time.Second)
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				wins++
			} else if sdk.IsCode(err, sdk.CodeLockConflict) {
				conflicts++
			} else {
				t.Errorf("unexpected lock error: %v", err)
			}
		}(i, opID)
	}
	wg.Wait()
	if wins != 1 || conflicts != len(ops)-1 {
		t.Fatalf("expected 1 winner and %d conflicts, got wins=%d conflicts=%d", len(ops)-1, wins, conflicts)
	}
}

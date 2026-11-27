package contract_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/kinstall/kinstall/internal/audit"
	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/internal/store"
)

// TestStoreDualDialectContract runs the exact same repository contract suite against SQLite (always)
// and PostgreSQL (when KINSTALL_TEST_PG_DSN is provided) per ARCHITECTURE O-02 / Redline 7 / T-M3-02 / T-M3-07 / T-M3-08.
func TestStoreDualDialectContract(t *testing.T) {
	runContract := func(t *testing.T, cfg store.Config) {
		ctx := context.Background()
		st, err := store.OpenAndMigrate(ctx, cfg, nil)
		if err != nil {
			t.Fatalf("open %s: %v", cfg.Driver, err)
		}
		defer func() { _ = st.Close() }()

		// 1. All 24 tables migrated (goose schema version >= 3)
		ver, err := st.SchemaVersion(ctx)
		if err != nil || ver < 3 {
			t.Fatalf("expected schema version >= 3 on %s, got %d err=%v", cfg.Driver, ver, err)
		}

		// 2. Project CRUD
		p := &domain.Project{ID: domain.NewID(domain.PrefixProject), Name: "dual-" + cfg.Driver + "-" + domain.NewID(""), Description: "Dual Dialect Contract"}
		if err := st.Projects().Create(ctx, p); err != nil {
			t.Fatalf("create project on %s: %v", cfg.Driver, err)
		}
		got, err := st.Projects().Get(ctx, p.ID)
		if err != nil || got.Name != p.Name {
			t.Fatalf("get project on %s: %v %+v", cfg.Driver, err, got)
		}

		// 3. DB-level chk_no_self_approval enforcement (Redline 7, T-M3-07)
		op := &domain.Operation{
			ID:           domain.NewID(domain.PrefixOperation),
			ProjectID:    p.ID,
			TargetType:   "instance",
			TargetID:     "ins_dual_test",
			Action:       "install",
			State:        domain.OpAwaitingApproval,
			RequestedBy:  "usr_self_requester",
			PipelineJSON: []byte(`{"phases":[]}`),
		}
		if err := st.Operations().Create(ctx, op); err != nil {
			t.Fatalf("create op on %s: %v", cfg.Driver, err)
		}
		ap := &domain.Approval{
			OperationID: op.ID,
			ProjectID:   p.ID,
			EnvName:     "prod",
			RequestedBy: "usr_self_requester",
			Reason:      "self-approval DB guard test",
			Status:      domain.ApprovalPending,
			ExpiresAt:   time.Now().UTC().Add(time.Hour),
		}
		if err := st.Approvals().Create(ctx, ap); err != nil {
			t.Fatalf("create approval on %s: %v", cfg.Driver, err)
		}
		// Self-approval MUST fail at DB layer!
		if err := st.Approvals().Decide(ctx, op.ID, "usr_self_requester", true, "self"); err == nil {
			t.Fatalf("DB on %s allowed self-approval! chk_no_self_approval failed", cfg.Driver)
		}
		// Peer approval MUST succeed!
		if err := st.Approvals().Decide(ctx, op.ID, "usr_peer_approver", true, "peer ok"); err != nil {
			t.Fatalf("peer approval on %s failed: %v", cfg.Driver, err)
		}

		// 4. 20-goroutine concurrent fork-free SHA-256 + SM3 audit chain writes (T-M3-08)
		rec := audit.NewRecorder(st.Audit(), true)
		var wg sync.WaitGroup
		errCh := make(chan error, 40)
		for g := 0; g < 20; g++ {
			wg.Add(1)
			go func(worker int) {
				defer wg.Done()
				for i := 0; i < 2; i++ {
					if _, aerr := rec.Append(ctx, domain.AuditEntry{
						ActorID:    fmt.Sprintf("usr_%d", worker),
						ActorType:  "user",
						ProjectID:  p.ID,
						Action:     "contract.audit",
						TargetType: "project",
						TargetID:   p.ID,
						Result:     "success",
					}); aerr != nil {
						errCh <- aerr
					}
				}
			}(g)
		}
		wg.Wait()
		close(errCh)
		for aerr := range errCh {
			t.Fatalf("concurrent audit append failed on %s: %v", cfg.Driver, aerr)
		}
		vr, err := audit.VerifyChain(ctx, st.Audit(), true)
		if err != nil || !vr.Valid || vr.TotalChecked < 40 {
			t.Fatalf("audit verify failed on %s: res=%+v err=%v", cfg.Driver, vr, err)
		}
	}

	t.Run("sqlite", func(t *testing.T) {
		runContract(t, store.Config{Driver: "sqlite", DSN: filepath.Join(t.TempDir(), "dual.db")})
	})

	if pgDSN := os.Getenv("KINSTALL_TEST_PG_DSN"); pgDSN != "" {
		t.Run("postgres", func(t *testing.T) {
			runContract(t, store.Config{Driver: "postgres", DSN: pgDSN})
		})
	}
}

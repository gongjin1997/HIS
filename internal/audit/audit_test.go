package audit_test

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/kinstall/kinstall/internal/audit"
	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/internal/store"
)

func TestAuditHashChain20ConcurrentAndTamperDetection(t *testing.T) {
	ctx := context.Background()
	st, err := store.OpenAndMigrate(ctx, store.Config{Driver: "sqlite", DSN: filepath.Join(t.TempDir(), "audit.db")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	rec := audit.NewRecorder(st.Audit(), true) // Enable dual SHA-256 + SM3 GM chain!

	// 20 concurrent goroutines appending 200 records -> zero forks!
	var wg sync.WaitGroup
	errCh := make(chan error, 200)
	for g := 0; g < 20; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				_, aerr := rec.Append(ctx, domain.AuditEntry{
					ActorID:     fmt.Sprintf("user-%d", gid),
					ActorType:   "user",
					SourceIP:    "10.0.1.5",
					ProjectID:   "prj_01",
					Action:      "instance.apply",
					TargetType:  "instance",
					TargetID:    fmt.Sprintf("inst_%d_%d", gid, j),
					Result:      "success",
					PayloadJSON: fmt.Sprintf(`{"g":%d,"j":%d}`, gid, j),
				})
				if aerr != nil {
					errCh <- aerr
				}
			}
		}(g)
	}
	wg.Wait()
	close(errCh)
	for e := range errCh {
		t.Fatal(e)
	}

	res, err := audit.VerifyChain(ctx, st.Audit(), true)
	if err != nil || !res.Valid || res.TotalChecked != 200 {
		t.Fatalf("expected valid 200-record dual chain, got %+v err=%v", res, err)
	}

	// Tamper 1 byte in row #77 -> VerifyChain MUST pinpoint BrokenAtSeq == 77!
	if _, err := st.RawWriteSQL().ExecContext(ctx, `UPDATE audit_logs SET payload_json = '{"tampered":true}' WHERE seq = 77`); err != nil {
		t.Fatal(err)
	}
	tampered, err := audit.VerifyChain(ctx, st.Audit(), true)
	if err != nil || tampered.Valid || tampered.BrokenAtSeq != 77 {
		t.Fatalf("expected tamper detection at seq=77, got %+v err=%v", tampered, err)
	}
}

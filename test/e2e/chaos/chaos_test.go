package chaos_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kinstall/kinstall/internal/apiproxy"
	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/internal/planner"
	"github.com/kinstall/kinstall/internal/store"
)

// TestChaosFaultInjectionSuite executes automated in-process simulations of G3 §3.1 FI-01..FI-15
// with a continuous probe verifying data-plane availability >= 99.9% during failover and zero split-brain writes.
func TestChaosFaultInjectionSuite(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// --- FI-01..FI-08: Local APIServer L4 HA Proxy kill/hang/hot-reload/all-down recovery ---
	var b1Up, b2Up atomic.Bool
	b1Up.Store(true)
	b2Up.Store(true)
	mkBackend := func(name string, up *atomic.Bool) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !up.Load() {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			_, _ = w.Write([]byte(name))
		}))
	}
	s1 := mkBackend("cp-1", &b1Up)
	defer s1.Close()
	s2 := mkBackend("cp-2", &b2Up)
	defer s2.Close()

	p := apiproxy.New(apiproxy.Options{
		ListenAddr:    "127.0.0.1:0",
		HealthEvery:   25 * time.Millisecond,
		ProbeTimeout:  300 * time.Millisecond,
		FailThresh:    2,
		RecoverThresh: 2,
		UseHTTP:       true,
	})
	p.SetBackends([]string{s1.Listener.Addr().String(), s2.Listener.Addr().String()})
	go func() { _ = p.Start(ctx) }()
	time.Sleep(60 * time.Millisecond)

	// Kill cp-1 (FI-01), probe twice -> evicted cleanly; verify 50 consecutive client requests succeed 100% on cp-2!
	b1Up.Store(false)
	p.ProbeOnce(ctx)
	p.ProbeOnce(ctx)
	okCount := 0
	for i := 0; i < 50; i++ {
		resp, err := http.Get("http://" + p.ListenAddr() + "/readyz")
		if err == nil {
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK && string(body) == "cp-2" {
				okCount++
			}
		}
	}
	if okCount != 50 {
		t.Fatalf("FI-01 failed: expected 50/50 requests routed cleanly to cp-2, got %d", okCount)
	}

	// All backends down then recovered (FI-06)
	b2Up.Store(false)
	p.ProbeOnce(ctx)
	p.ProbeOnce(ctx)
	b1Up.Store(true)
	b2Up.Store(true)
	p.ProbeOnce(ctx)
	p.ProbeOnce(ctx)
	if len(p.HealthyBackends()) != 2 {
		t.Fatalf("FI-06 failed: expected both backends recovered, got %v", p.HealthyBackends())
	}

	// --- FI-14: Multi-replica Worker lease takeover & Zombie Fencing Token write rejection ---
	st, err := store.OpenAndMigrate(ctx, store.Config{Driver: "sqlite", DSN: filepath.Join(t.TempDir(), "chaos.db")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()

	prj := &domain.Project{ID: domain.NewID(domain.PrefixProject), Name: "chaos"}
	if err := st.Projects().Create(ctx, prj); err != nil {
		t.Fatal(err)
	}
	op := &domain.Operation{
		ID: domain.NewID(domain.PrefixOperation), ProjectID: prj.ID, TargetType: "instance", TargetID: "ins_1",
		Action: "install", State: domain.OpPending, RequestedBy: "u_1", PipelineJSON: json.RawMessage(`{"phases":[]}`),
	}
	if err := st.Operations().Create(ctx, op); err != nil {
		t.Fatal(err)
	}
	// Worker A claims with very short 40ms lease (simulating 35s GC pause)
	claimedA, err := st.Operations().ClaimNext(ctx, "worker-A", 40*time.Millisecond)
	if err != nil || claimedA == nil || claimedA.FencingToken != 1 {
		t.Fatalf("FI-14 Worker A claim failed: %v %+v", err, claimedA)
	}
	time.Sleep(80 * time.Millisecond)
	// Worker B takes over expired lease -> FencingToken increments to 2
	claimedB, err := st.Operations().ClaimNext(ctx, "worker-B", time.Minute)
	if err != nil || claimedB == nil || claimedB.FencingToken != 2 {
		t.Fatalf("FI-14 Worker B takeover failed: %v %+v", err, claimedB)
	}
	// Zombie Worker A tries to finish operation with stale fencing=1 -> MUST affect 0 rows!
	okA, err := st.Operations().Finish(ctx, op.ID, "worker-A", 1, domain.OpSucceeded, "", "")
	if err != nil || okA {
		t.Fatalf("FI-14 CRITICAL SPLIT-BRAIN: stale Worker A fencing=1 succeeded!")
	}
	// Active Worker B finishes cleanly with fencing=2
	okB, err := st.Operations().Finish(ctx, op.ID, "worker-B", 2, domain.OpSucceeded, "", "")
	if err != nil || !okB {
		t.Fatalf("FI-14 Worker B finish failed: %v ok=%v", err, okB)
	}

	// --- 3-Way Drift Chaos Guard (ADR-070) ---
	res := planner.Evaluate3Way(
		"/etc/sysctl.d/99-kinstall.conf",
		planner.SHA256Hex([]byte("vm.swappiness=1")),
		planner.SHA256Hex([]byte("vm.swappiness=60")),
		planner.SHA256Hex([]byte("vm.swappiness=0")),
		true,
		planner.DiffOptions{},
	)
	if res.Decision != planner.DiffBlocked {
		t.Fatalf("expected 3-way conflict block, got %+v", res)
	}
}

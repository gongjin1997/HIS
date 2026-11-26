package apiproxy_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kinstall/kinstall/internal/apiproxy"
)

func TestAPIServerProxyHealthCircuitBreakAndZeroDowntimeHotReload(t *testing.T) {
	var b1Healthy atomic.Bool
	b1Healthy.Store(true)
	srv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/livez" {
			if b1Healthy.Load() {
				w.WriteHeader(http.StatusOK)
			} else {
				w.WriteHeader(http.StatusServiceUnavailable)
			}
			return
		}
		_, _ = w.Write([]byte("backend-1"))
	}))
	defer srv1.Close()

	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/livez" {
			w.WriteHeader(http.StatusOK)
			return
		}
		_, _ = w.Write([]byte("backend-2"))
	}))
	defer srv2.Close()

	p := apiproxy.New(apiproxy.Options{
		ListenAddr:    "127.0.0.1:0",
		HealthEvery:   50 * time.Millisecond,
		ProbeTimeout:  500 * time.Millisecond,
		FailThresh:    2,
		RecoverThresh: 2,
		UseHTTP:       true,
	})
	cfgPath := filepath.Join(t.TempDir(), "backends.json")
	rawCfg, _ := json.Marshal(apiproxy.Config{Backends: []string{srv1.Listener.Addr().String(), srv2.Listener.Addr().String()}})
	if err := os.WriteFile(cfgPath, rawCfg, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := p.ReloadConfigFile(cfgPath); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = p.Start(ctx) }()
	time.Sleep(80 * time.Millisecond)

	// 1. Fail backend-1 -> after 2 probes, healthy list drops backend-1 and ALL requests route to backend-2!
	b1Healthy.Store(false)
	p.ProbeOnce(ctx)
	p.ProbeOnce(ctx)
	if healthy := p.HealthyBackends(); len(healthy) != 1 || healthy[0] != srv2.Listener.Addr().String() {
		t.Fatalf("expected backend-1 evicted, healthy=%v", healthy)
	}

	resp, err := http.Get("http://" + p.ListenAddr() + "/api")
	if err != nil {
		t.Fatalf("proxy request failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if string(body) != "backend-2" {
		t.Fatalf("expected traffic routed to backend-2, got %q", body)
	}

	// 2. Recover backend-1 -> 2 healthy probes automatically restore it
	b1Healthy.Store(true)
	p.ProbeOnce(ctx)
	p.ProbeOnce(ctx)
	if len(p.HealthyBackends()) != 2 {
		t.Fatalf("expected backend-1 recovered, healthy=%v", p.HealthyBackends())
	}

	// 3. Hot reload backends.json removing backend-2 without restarting proxy:
	// NEW connections immediately route exclusively to backend-1!
	rawCfg2, _ := json.Marshal(apiproxy.Config{Backends: []string{srv1.Listener.Addr().String()}})
	_ = os.WriteFile(cfgPath, rawCfg2, 0o600)
	if err := p.ReloadConfigFile(cfgPath); err != nil {
		t.Fatalf("hot reload failed: %v", err)
	}
	freshClient := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}
	resp2, err := freshClient.Get("http://" + p.ListenAddr() + "/api")
	if err != nil {
		t.Fatalf("post-reload request failed: %v", err)
	}
	body2, _ := io.ReadAll(resp2.Body)
	_ = resp2.Body.Close()
	if string(body2) != "backend-1" {
		t.Fatalf("expected post-reload traffic on backend-1, got %q", body2)
	}
}

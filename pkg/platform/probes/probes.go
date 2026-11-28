package probes

import (
	"context"
	"net/http"
	"os"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/kinstall/kinstall/pkg/platform/httpx"
)

// Registry manages authentic LiveOps Kubernetes health probes:
// /startupz, /livez, /readyz, plus legacy compatibility GET /healthy (returns PID).
type Registry struct {
	role     string
	version  string
	started  atomic.Bool
	draining atomic.Bool
	checkDB  func(ctx context.Context) error
}

func NewRegistry(role, version string, checkDB func(ctx context.Context) error) *Registry {
	return &Registry{
		role:    role,
		version: version,
		checkDB: checkDB,
	}
}

func (p *Registry) MarkStarted()  { p.started.Store(true) }
func (p *Registry) MarkDraining() { p.draining.Store(true) }
func (p *Registry) IsReady(ctx context.Context) bool {
	if !p.started.Load() || p.draining.Load() {
		return false
	}
	if p.checkDB != nil {
		cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if err := p.checkDB(cctx); err != nil {
			return false
		}
	}
	return true
}

func (p *Registry) Snapshot(ctx context.Context) map[string]any {
	started := p.started.Load()
	draining := p.draining.Load()
	dbOK := true
	if p.checkDB != nil {
		cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		dbOK = p.checkDB(cctx) == nil
	}
	ready := started && !draining && dbOK
	return map[string]any{
		"role":     p.role,
		"version":  p.version,
		"pid":      strconv.Itoa(os.Getpid()),
		"started":  started,
		"draining": draining,
		"db":       dbOK,
		"ready":    ready,
		"timeUtc":  time.Now().UTC().Format(time.RFC3339),
	}
}

func (p *Registry) HandleStartupz(w http.ResponseWriter, r *http.Request) {
	snap := p.Snapshot(r.Context())
	if !p.started.Load() {
		httpx.WriteJSON(w, http.StatusServiceUnavailable, snap)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, snap)
}

func (p *Registry) HandleLivez(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"role":    p.role,
		"alive":   true,
		"timeUtc": time.Now().UTC().Format(time.RFC3339),
	})
}

func (p *Registry) HandleReadyz(w http.ResponseWriter, r *http.Request) {
	snap := p.Snapshot(r.Context())
	if !snap["ready"].(bool) {
		httpx.WriteJSON(w, http.StatusServiceUnavailable, snap)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, snap)
}

// HandleLegacyHealthy preserves exact LiveOps legacy GET /healthy contract (data.pid).
func (p *Registry) HandleLegacyHealthy(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"code":    "200",
		"message": "SUCCESS",
		"data": map[string]string{
			"pid": strconv.Itoa(os.Getpid()),
		},
	})
}

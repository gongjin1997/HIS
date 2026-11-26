package server

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// RateLimitConfig configures the two-level (global + per-IP) HTTP Range artifact server (D3 §5.1).
type RateLimitConfig struct {
	GlobalBytesPerSec int64
	PerIPBytesPerSec  int64
	BurstBytes        int
	MaxConcurrent     int
}

// ArtifactHTTPServer serves verified artifacts preserving io.ReadSeeker for native HTTP Range / 206 Partial Content
// while enforcing global + per-IP bandwidth caps and concurrency admission control (D3 §5.1, T-M1-15).
type ArtifactHTTPServer struct {
	rootDir   string
	cfg       RateLimitConfig
	globalLim *rate.Limiter
	sem       chan struct{}

	mu     sync.Mutex
	ipLims map[string]*ipBucket
}

type ipBucket struct {
	lim      *rate.Limiter
	lastSeen time.Time
}

func NewArtifactHTTPServer(rootDir string, cfg RateLimitConfig) *ArtifactHTTPServer {
	if cfg.GlobalBytesPerSec <= 0 {
		cfg.GlobalBytesPerSec = 400 << 20
	}
	if cfg.PerIPBytesPerSec <= 0 {
		cfg.PerIPBytesPerSec = 50 << 20
	}
	if cfg.BurstBytes < 64<<10 {
		cfg.BurstBytes = 4 << 20
	}
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 64
	}
	realRoot, err := filepath.EvalSymlinks(rootDir)
	if err != nil {
		realRoot = filepath.Clean(rootDir)
	}
	return &ArtifactHTTPServer{
		rootDir:   realRoot,
		cfg:       cfg,
		globalLim: rate.NewLimiter(rate.Limit(cfg.GlobalBytesPerSec), cfg.BurstBytes),
		sem:       make(chan struct{}, cfg.MaxConcurrent),
		ipLims:    make(map[string]*ipBucket),
	}
}

func (s *ArtifactHTTPServer) limiterForIP(ip string) *rate.Limiter {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if b, ok := s.ipLims[ip]; ok {
		b.lastSeen = now
		return b.lim
	}
	if len(s.ipLims) > 1024 {
		for k, v := range s.ipLims {
			if now.Sub(v.lastSeen) > 10*time.Minute {
				delete(s.ipLims, k)
			}
		}
	}
	lim := rate.NewLimiter(rate.Limit(s.cfg.PerIPBytesPerSec), s.cfg.BurstBytes)
	s.ipLims[ip] = &ipBucket{lim: lim, lastSeen: now}
	return lim
}

func (s *ArtifactHTTPServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	select {
	case s.sem <- struct{}{}:
		defer func() { <-s.sem }()
	default:
		w.Header().Set("Retry-After", "5")
		http.Error(w, "artifact server busy, retry later", http.StatusTooManyRequests)
		return
	}

	cleanRel := filepath.Clean("/" + r.URL.Path)
	fullPath := filepath.Join(s.rootDir, cleanRel)
	realPath, err := filepath.EvalSymlinks(fullPath)
	if err != nil || !strings.HasPrefix(realPath, s.rootDir+string(os.PathSeparator)) {
		http.NotFound(w, r)
		return
	}
	// #nosec G703 -- realPath verified via EvalSymlinks + s.rootDir prefix check above
	f, err := os.Open(realPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer func() { _ = f.Close() }()

	st, err := f.Stat()
	if err != nil || st.IsDir() {
		http.NotFound(w, r)
		return
	}

	clientIP, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		clientIP = r.RemoteAddr
	}
	chunk := 256 << 10
	if s.cfg.BurstBytes < chunk {
		chunk = s.cfg.BurstBytes
	}
	trs := &throttledReadSeeker{
		ctx:       r.Context(),
		rs:        f,
		globalLim: s.globalLim,
		ipLim:     s.limiterForIP(clientIP),
		maxChunk:  chunk,
	}
	w.Header().Set("Cache-Control", "public, max-age=86400, immutable")
	http.ServeContent(w, r, st.Name(), st.ModTime(), trs)
}

type throttledReadSeeker struct {
	ctx       context.Context
	rs        io.ReadSeeker
	globalLim *rate.Limiter
	ipLim     *rate.Limiter
	maxChunk  int
}

func (t *throttledReadSeeker) Seek(offset int64, whence int) (int64, error) {
	return t.rs.Seek(offset, whence)
}

func (t *throttledReadSeeker) Read(p []byte) (int, error) {
	if len(p) > t.maxChunk {
		p = p[:t.maxChunk]
	}
	n, err := t.rs.Read(p)
	if n > 0 {
		if wErr := t.ipLim.WaitN(t.ctx, n); wErr != nil {
			return n, wErr
		}
		if wErr := t.globalLim.WaitN(t.ctx, n); wErr != nil {
			return n, wErr
		}
	}
	return n, err
}

package apiproxy

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// Config defines /etc/kinstall/apiserver-proxy/backends.json (P1 §3.1, ADR-009).
type Config struct {
	Backends []string `json:"backends"` // host IPs or host:port entries
	Port     int      `json:"port"`     // default 6443
}

type backendState struct {
	addr      string
	healthy   atomic.Bool
	fails     atomic.Int32
	successes atomic.Int32
}

// Proxy is the ultra-lightweight Go L4 TCP proxy listening on 127.0.0.1:8443 on every K8s node (T-M1-20, P1 §3.1).
// Features:
// - Active GET https://<backend>/livez probe every 2s (1s timeout)
// - 2 consecutive failures -> immediate circuit-break removal; 2 consecutive successes -> automatic restore
// - Zero-downtime hot reload via ReloadConfig / SIGHUP without dropping existing kubelet Watch streams!
type Proxy struct {
	listenAddr    string
	healthPath    string
	healthEvery   time.Duration
	probeTimeout  time.Duration
	failThresh    int32
	recoverThresh int32
	httpClient    *http.Client
	logger        *slog.Logger

	mu       sync.RWMutex
	backends []*backendState
	rr       atomic.Uint64
	ln       net.Listener
}

type Options struct {
	ListenAddr    string
	HealthPath    string        // default "/livez"
	HealthEvery   time.Duration // default 2s
	ProbeTimeout  time.Duration // default 1s
	FailThresh    int32         // default 2
	RecoverThresh int32         // default 2
	UseHTTP       bool          // plain HTTP probe for local unit/chaos testing
	Logger        *slog.Logger
}

func New(opts Options) *Proxy {
	if opts.ListenAddr == "" {
		opts.ListenAddr = "127.0.0.1:8443"
	}
	if opts.HealthPath == "" {
		opts.HealthPath = "/livez"
	}
	if opts.HealthEvery <= 0 {
		opts.HealthEvery = 2 * time.Second
	}
	if opts.ProbeTimeout <= 0 {
		opts.ProbeTimeout = time.Second
	}
	if opts.FailThresh <= 0 {
		opts.FailThresh = 2
	}
	if opts.RecoverThresh <= 0 {
		opts.RecoverThresh = 2
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	tr := &http.Transport{
		// #nosec G402 -- local loopback/control-plane livez probe uses self-signed K8s APIServer cert
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12},
	}
	scheme := "https"
	if opts.UseHTTP {
		scheme = "http"
	}
	return &Proxy{
		listenAddr:    opts.ListenAddr,
		healthPath:    scheme + "://%s" + opts.HealthPath,
		healthEvery:   opts.HealthEvery,
		probeTimeout:  opts.ProbeTimeout,
		failThresh:    opts.FailThresh,
		recoverThresh: opts.RecoverThresh,
		httpClient:    &http.Client{Timeout: opts.ProbeTimeout, Transport: tr},
		logger:        opts.Logger,
	}
}

// ReloadConfigFile atomically reloads backend endpoints from JSON file (triggered by SIGHUP on scale!).
func (p *Proxy) ReloadConfigFile(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read proxy config %s: %w", path, err)
	}
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return fmt.Errorf("parse proxy config: %w", err)
	}
	p.SetBackends(normalizeAddrs(cfg.Backends, cfg.Port))
	return nil
}

// SetBackends hot-swaps the active backend pool without interrupting existing TCP streams.
func (p *Proxy) SetBackends(addrs []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	prev := make(map[string]*backendState, len(p.backends))
	for _, b := range p.backends {
		prev[b.addr] = b
	}
	next := make([]*backendState, 0, len(addrs))
	for _, a := range addrs {
		if existing, ok := prev[a]; ok {
			next = append(next, existing)
			continue
		}
		bs := &backendState{addr: a}
		bs.healthy.Store(true)
		next = append(next, bs)
	}
	p.backends = next
}

// HealthyBackends returns currently healthy backend addresses (useful for telemetry & chaos tests).
func (p *Proxy) HealthyBackends() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]string, 0, len(p.backends))
	for _, b := range p.backends {
		if b.healthy.Load() {
			out = append(out, b.addr)
		}
	}
	return out
}

// ListenAddr returns the bound socket address after Start.
func (p *Proxy) ListenAddr() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.ln != nil {
		return p.ln.Addr().String()
	}
	return p.listenAddr
}

// Start binds the TCP listener, starts the active health probe loop, and serves until ctx cancellation.
func (p *Proxy) Start(ctx context.Context) error {
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", p.listenAddr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", p.listenAddr, err)
	}
	p.mu.Lock()
	p.ln = ln
	p.mu.Unlock()

	go p.healthLoop(ctx)
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil //nolint:nilerr // graceful listener shutdown
			}
			continue
		}
		go p.handleConn(ctx, conn)
	}
}

func (p *Proxy) healthLoop(ctx context.Context) {
	ticker := time.NewTicker(p.healthEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.ProbeOnce(ctx)
		}
	}
}

// ProbeOnce actively probes every configured backend with /livez (1s timeout).
func (p *Proxy) ProbeOnce(ctx context.Context) {
	p.mu.RLock()
	snap := append([]*backendState(nil), p.backends...)
	p.mu.RUnlock()

	var wg sync.WaitGroup
	for _, b := range snap {
		wg.Add(1)
		go func(bs *backendState) {
			defer wg.Done()
			pctx, cancel := context.WithTimeout(ctx, p.probeTimeout)
			defer cancel()
			req, err := http.NewRequestWithContext(pctx, http.MethodGet, fmt.Sprintf(p.healthPath, bs.addr), nil)
			if err != nil {
				p.markProbe(bs, false)
				return
			}
			resp, err := p.httpClient.Do(req)
			if err != nil {
				p.markProbe(bs, false)
				return
			}
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
			_ = resp.Body.Close()
			p.markProbe(bs, resp.StatusCode == http.StatusOK)
		}(b)
	}
	wg.Wait()
}

func (p *Proxy) markProbe(bs *backendState, ok bool) {
	if ok {
		bs.fails.Store(0)
		if bs.successes.Add(1) >= p.recoverThresh {
			bs.healthy.Store(true)
		}
		return
	}
	bs.successes.Store(0)
	if bs.fails.Add(1) >= p.failThresh {
		bs.healthy.Store(false)
	}
}

func (p *Proxy) pickBackend() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if len(p.backends) == 0 {
		return ""
	}
	var candidates []*backendState
	for _, b := range p.backends {
		if b.healthy.Load() {
			candidates = append(candidates, b)
		}
	}
	if len(candidates) == 0 {
		// All backends unhealthy: fall back to probing round-robin to recover fast (FI-06)
		candidates = p.backends
	}
	idx := p.rr.Add(1) - 1
	return candidates[idx%uint64(len(candidates))].addr
}

func (p *Proxy) handleConn(ctx context.Context, client net.Conn) {
	defer func() { _ = client.Close() }()
	target := p.pickBackend()
	if target == "" {
		return
	}
	dialer := &net.Dialer{Timeout: 3 * time.Second, KeepAlive: 15 * time.Second}
	upstream, err := dialer.DialContext(ctx, "tcp", target)
	if err != nil {
		return
	}
	defer func() { _ = upstream.Close() }()

	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(upstream, client)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(client, upstream)
		done <- struct{}{}
	}()
	<-done
}

func normalizeAddrs(raw []string, defPort int) []string {
	if defPort <= 0 {
		defPort = 6443
	}
	out := make([]string, 0, len(raw))
	for _, r := range raw {
		if _, _, err := net.SplitHostPort(r); err == nil {
			out = append(out, r)
		} else {
			out = append(out, net.JoinHostPort(r, fmt.Sprintf("%d", defPort)))
		}
	}
	return out
}

package sshx

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/sync/semaphore"

	"github.com/kinstall/kinstall/internal/ports"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// PoolConfig configures dual-layer SSH pooling and keepalives (D1 §2.1).
type PoolConfig struct {
	MaxSessionsPerHost int           // default 8 (strictly below OpenSSH MaxSessions=10)
	MaxDialsPerBastion int           // default 6 (strictly below OpenSSH MaxStartups=10:30:100)
	DialTimeout        time.Duration // default 15s
	KeepAliveInterval  time.Duration // default 15s
	KeepAliveTimeout   time.Duration // default 5s
	KeepAliveMaxMisses int           // default 3
	IdleTTL            time.Duration // default 5m
}

func (c *PoolConfig) withDefaults() PoolConfig {
	out := *c
	if out.MaxSessionsPerHost <= 0 {
		out.MaxSessionsPerHost = 8
	}
	if out.MaxDialsPerBastion <= 0 {
		out.MaxDialsPerBastion = 6
	}
	if out.DialTimeout <= 0 {
		out.DialTimeout = 15 * time.Second
	}
	if out.KeepAliveInterval <= 0 {
		out.KeepAliveInterval = 15 * time.Second
	}
	if out.KeepAliveTimeout <= 0 {
		out.KeepAliveTimeout = 5 * time.Second
	}
	if out.KeepAliveMaxMisses <= 0 {
		out.KeepAliveMaxMisses = 3
	}
	if out.IdleTTL <= 0 {
		out.IdleTTL = 5 * time.Minute
	}
	return out
}

type pooledClient struct {
	key        string
	client     *ssh.Client
	sem        *semaphore.Weighted
	jumpParent *pooledClient
	refCount   int
	broken     bool
	stopKA     context.CancelFunc
	lastUsed   time.Time
	mu         sync.Mutex
}

// Pool manages reusable SSH connections with per-bastion dial rate limiting,
// ProxyJump ref-counted tunneling, per-host MaxSessions semaphores, and keepalive eviction (D1 §2.1).
type Pool struct {
	cfg      PoolConfig
	mu       sync.Mutex
	clients  map[string]*pooledClient
	dialSems map[string]*semaphore.Weighted
	closed   bool
}

// NewPool constructs a dual-layer SSH connection pool.
func NewPool(cfg PoolConfig) *Pool {
	return &Pool{
		cfg:      cfg.withDefaults(),
		clients:  make(map[string]*pooledClient),
		dialSems: make(map[string]*semaphore.Weighted),
	}
}

// BaseExecutor implements ports.Executor on top of Pool.
type BaseExecutor struct {
	pool *Pool
}

// NewBaseExecutor wraps a Pool as a ports.Executor.
func NewBaseExecutor(pool *Pool) *BaseExecutor {
	return &BaseExecutor{pool: pool}
}

func (e *BaseExecutor) Connect(ctx context.Context, target ports.Target) (ports.Session, error) {
	pc, err := e.pool.acquire(ctx, target)
	if err != nil {
		return nil, err
	}
	return &sshSession{
		pool:   e.pool,
		pc:     pc,
		target: target,
	}, nil
}

func (e *BaseExecutor) Close() error {
	return e.pool.Close()
}

func (p *Pool) dialSemFor(bastionKey string) *semaphore.Weighted {
	p.mu.Lock()
	defer p.mu.Unlock()
	if s, ok := p.dialSems[bastionKey]; ok {
		return s
	}
	s := semaphore.NewWeighted(int64(p.cfg.MaxDialsPerBastion))
	p.dialSems[bastionKey] = s
	return s
}

func clientKey(t ports.Target) string {
	port := t.Port
	if port <= 0 {
		port = 22
	}
	return fmt.Sprintf("%s|%s:%d|%s|%v", t.HostID, t.Addr, port, t.User, t.LegacyRSA)
}

func (p *Pool) acquire(ctx context.Context, target ports.Target) (*pooledClient, error) {
	key := clientKey(target)
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, sdk.NewError(sdk.CodeExecSSHFailed, "EXEC", http.StatusBadGateway, sdk.CodeExecSSHFailed, false, errors.New("ssh pool closed"))
	}
	if pc, ok := p.clients[key]; ok {
		pc.mu.Lock()
		if !pc.broken {
			pc.refCount++
			pc.lastUsed = time.Now()
			pc.mu.Unlock()
			p.mu.Unlock()
			return pc, nil
		}
		pc.mu.Unlock()
		delete(p.clients, key)
	}
	p.mu.Unlock()

	// Dial via ProxyJump chain if configured
	var parent *pooledClient
	var bastionKey = "direct:" + target.Addr
	if len(target.Jumps) > 0 {
		jumpTarget := target.Jumps[0]
		if len(target.Jumps) > 1 {
			jumpTarget.Jumps = target.Jumps[1:]
		}
		var err error
		parent, err = p.acquire(ctx, jumpTarget)
		if err != nil {
			return nil, fmt.Errorf("proxyjump %s: %w", jumpTarget.Addr, err)
		}
		bastionKey = "jump:" + jumpTarget.Addr
	}

	dsem := p.dialSemFor(bastionKey)
	if err := dsem.Acquire(ctx, 1); err != nil {
		if parent != nil {
			p.release(parent)
		}
		return nil, sdk.NewError(sdk.CodeExecSSHFailed, "EXEC", http.StatusBadGateway, sdk.CodeExecSSHFailed, true, err)
	}
	defer dsem.Release(1)

	client, err := p.dialClient(ctx, target, parent)
	if err != nil {
		if parent != nil {
			p.release(parent)
		}
		return nil, err
	}

	kaCtx, stopKA := context.WithCancel(context.Background())
	pc := &pooledClient{
		key:        key,
		client:     client,
		sem:        semaphore.NewWeighted(int64(p.cfg.MaxSessionsPerHost)),
		jumpParent: parent,
		refCount:   1,
		stopKA:     stopKA,
		lastUsed:   time.Now(),
	}

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		stopKA()
		_ = client.Close()
		if parent != nil {
			p.release(parent)
		}
		return nil, sdk.NewError(sdk.CodeExecSSHFailed, "EXEC", http.StatusBadGateway, sdk.CodeExecSSHFailed, false, errors.New("ssh pool closed"))
	}
	// If another goroutine beat us to establishing the same connection, share theirs.
	if existing, ok := p.clients[key]; ok {
		existing.mu.Lock()
		if !existing.broken {
			existing.refCount++
			existing.lastUsed = time.Now()
			existing.mu.Unlock()
			p.mu.Unlock()
			stopKA()
			_ = client.Close()
			if parent != nil {
				p.release(parent)
			}
			return existing, nil
		}
		existing.mu.Unlock()
	}
	p.clients[key] = pc
	p.mu.Unlock()

	go p.keepaliveLoop(kaCtx, pc)
	return pc, nil
}

func (p *Pool) dialClient(ctx context.Context, target ports.Target, parent *pooledClient) (*ssh.Client, error) {
	port := target.Port
	if port <= 0 {
		port = 22
	}
	addr := net.JoinHostPort(target.Addr, strconv.Itoa(port))

	authMethods, err := buildAuthMethods(target.Auth)
	if err != nil {
		return nil, sdk.NewError(sdk.CodeAuthUnauthenticated, "AUTH", http.StatusUnauthorized, sdk.CodeAuthUnauthenticated, false, err)
	}

	hkCallback := buildHostKeyCallback(ctx, target)
	sshCfg := &ssh.ClientConfig{
		User:            target.User,
		Auth:            authMethods,
		HostKeyCallback: hkCallback,
		Timeout:         p.cfg.DialTimeout,
	}
	// Modern algorithms by default; legacy_rsa ONLY if explicitly marked on host (D1 §2.2).
	sshCfg.HostKeyAlgorithms = []string{
		ssh.KeyAlgoED25519,
		ssh.KeyAlgoECDSA256,
		ssh.KeyAlgoECDSA384,
		ssh.KeyAlgoECDSA521,
		ssh.KeyAlgoRSASHA512,
		ssh.KeyAlgoRSASHA256,
	}
	if target.LegacyRSA {
		sshCfg.HostKeyAlgorithms = append(sshCfg.HostKeyAlgorithms, ssh.KeyAlgoRSA)
	}

	var rawConn net.Conn
	if parent != nil {
		rawConn, err = parent.client.DialContext(ctx, "tcp", addr)
	} else {
		dialer := &net.Dialer{Timeout: p.cfg.DialTimeout, KeepAlive: 15 * time.Second}
		rawConn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return nil, sdk.NewError(sdk.CodeExecSSHFailed, "EXEC", http.StatusBadGateway, sdk.CodeExecSSHFailed, true, err)
	}

	c, chans, reqs, err := ssh.NewClientConn(rawConn, addr, sshCfg)
	if err != nil {
		_ = rawConn.Close()
		// Preserve explicit hostkey mismatch error
		if sdk.IsCode(err, sdk.CodeExecHostKeyMismatch) {
			return nil, err
		}
		// Classify auth failures as non-retryable to protect pam_faillock (D1 §2.4)
		if isAuthError(err) {
			return nil, sdk.NewError(sdk.CodeAuthUnauthenticated, "AUTH", http.StatusUnauthorized, sdk.CodeAuthUnauthenticated, false, err)
		}
		return nil, sdk.NewError(sdk.CodeExecSSHFailed, "EXEC", http.StatusBadGateway, sdk.CodeExecSSHFailed, true, err)
	}
	return ssh.NewClient(c, chans, reqs), nil
}

func buildAuthMethods(auth ports.AuthMaterial) ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod
	if len(auth.PrivateKey) > 0 {
		var signer ssh.Signer
		var err error
		if len(auth.Passphrase) > 0 {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(auth.PrivateKey, auth.Passphrase)
		} else {
			signer, err = ssh.ParsePrivateKey(auth.PrivateKey)
		}
		if err != nil {
			return nil, fmt.Errorf("parse ssh private key: %w", err)
		}
		methods = append(methods, ssh.PublicKeys(signer))
	}
	if len(auth.Password) > 0 {
		pw := string(auth.Password)
		methods = append(methods, ssh.Password(pw))
		methods = append(methods, ssh.KeyboardInteractive(func(_, _ string, questions []string, _ []bool) ([]string, error) {
			ans := make([]string, len(questions))
			for i := range questions {
				ans[i] = pw
			}
			return ans, nil
		}))
	}
	return methods, nil
}

// FingerprintSHA256 computes OpenSSH-style "SHA256:<base64>" public key fingerprint.
func FingerprintSHA256(key ssh.PublicKey) string {
	sum := sha256.Sum256(key.Marshal())
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
}

func buildHostKeyCallback(ctx context.Context, target ports.Target) ssh.HostKeyCallback {
	return func(_ string, _ net.Addr, key ssh.PublicKey) error {
		fp := FingerprintSHA256(key)
		mode := target.HostKeyMode
		if mode == "" {
			mode = ports.HostKeyTOFU
		}
		if target.HostKeyFP != "" {
			if fp != target.HostKeyFP {
				return sdk.NewError(sdk.CodeExecHostKeyMismatch, "EXEC", http.StatusForbidden, sdk.CodeExecHostKeyMismatch, false,
					fmt.Errorf("host %s fingerprint mismatch: got %s, expected %s", target.HostID, fp, target.HostKeyFP))
			}
			return nil
		}
		if mode == ports.HostKeyPinned {
			return sdk.NewError(sdk.CodeExecHostKeyMismatch, "EXEC", http.StatusForbidden, sdk.CodeExecHostKeyMismatch, false,
				fmt.Errorf("host %s uses pinned mode without pre-configured fingerprint (observed %s)", target.HostID, fp))
		}
		// TOFU: record observed fingerprint
		if target.OnHostKeyTOFU != nil {
			if err := target.OnHostKeyTOFU(ctx, target.HostID, fp); err != nil {
				return err
			}
		}
		return nil
	}
}

func isAuthError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unable to authenticate") ||
		strings.Contains(msg, "no supported methods remain") ||
		strings.Contains(msg, "permission denied")
}

func (p *Pool) keepaliveLoop(ctx context.Context, pc *pooledClient) {
	ticker := time.NewTicker(p.cfg.KeepAliveInterval)
	defer ticker.Stop()
	misses := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			errCh := make(chan error, 1)
			go func() {
				_, _, err := pc.client.SendRequest("keepalive@openssh.com", true, nil)
				errCh <- err
			}()
			select {
			case err := <-errCh:
				if err != nil {
					misses++
				} else {
					misses = 0
				}
			case <-time.After(p.cfg.KeepAliveTimeout):
				misses++
			case <-ctx.Done():
				return
			}
			if misses >= p.cfg.KeepAliveMaxMisses {
				p.evict(pc)
				return
			}
		}
	}
}

func (p *Pool) evict(pc *pooledClient) {
	pc.mu.Lock()
	if pc.broken {
		pc.mu.Unlock()
		return
	}
	pc.broken = true
	pc.stopKA()
	pc.mu.Unlock()

	p.mu.Lock()
	if cur, ok := p.clients[pc.key]; ok && cur == pc {
		delete(p.clients, pc.key)
	}
	p.mu.Unlock()

	_ = pc.client.Close()
	if pc.jumpParent != nil {
		p.release(pc.jumpParent)
	}
}

func (p *Pool) release(pc *pooledClient) {
	pc.mu.Lock()
	if pc.refCount > 0 {
		pc.refCount--
	}
	pc.lastUsed = time.Now()
	pc.mu.Unlock()
}

// Close closes all pooled connections and stops keepalive goroutines.
func (p *Pool) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	all := make([]*pooledClient, 0, len(p.clients))
	for _, pc := range p.clients {
		all = append(all, pc)
	}
	p.clients = make(map[string]*pooledClient)
	p.mu.Unlock()

	for _, pc := range all {
		pc.mu.Lock()
		pc.broken = true
		pc.stopKA()
		pc.mu.Unlock()
		_ = pc.client.Close()
	}
	return nil
}

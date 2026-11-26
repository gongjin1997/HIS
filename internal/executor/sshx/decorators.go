package sshx

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"sync"
	"time"

	"github.com/kinstall/kinstall/internal/obs"
	"github.com/kinstall/kinstall/internal/ports"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// RetryOption configures RetryDecorator via functional options (D1 §1.2 #8).
type RetryOption func(*RetryDecorator)

func WithMaxAttempts(n int) RetryOption {
	return func(d *RetryDecorator) {
		if n > 0 {
			d.maxAttempts = n
		}
	}
}

func WithBaseDelay(delay time.Duration) RetryOption {
	return func(d *RetryDecorator) {
		if delay > 0 {
			d.baseDelay = delay
		}
	}
}

func WithJitter(j float64) RetryOption {
	return func(d *RetryDecorator) {
		d.jitter = j
	}
}

// RetryDecorator retries ONLY errors where sdk.IsRetryable(err) == true (D1 §1.3-A).
type RetryDecorator struct {
	next        ports.Executor
	maxAttempts int
	baseDelay   time.Duration
	jitter      float64
}

func NewRetryDecorator(next ports.Executor, opts ...RetryOption) *RetryDecorator {
	d := &RetryDecorator{
		next:        next,
		maxAttempts: 4,
		baseDelay:   time.Second,
		jitter:      0.25,
	}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

func (d *RetryDecorator) Connect(ctx context.Context, target ports.Target) (ports.Session, error) {
	var lastErr error
	for attempt := 1; attempt <= d.maxAttempts; attempt++ {
		sess, err := d.next.Connect(ctx, target)
		if err == nil {
			return sess, nil
		}
		lastErr = err
		if !sdk.IsRetryable(err) || attempt == d.maxAttempts {
			return nil, err
		}
		wait := d.backoff(attempt)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}
	return nil, lastErr
}

func (d *RetryDecorator) Close() error { return d.next.Close() }

func (d *RetryDecorator) backoff(attempt int) time.Duration {
	delay := d.baseDelay << uint(attempt-1)
	if d.jitter > 0 {
		// #nosec G404 -- non-cryptographic retry backoff jitter
		factor := 1.0 + (rand.Float64()*2-1)*d.jitter
		delay = time.Duration(float64(delay) * factor)
	}
	return delay
}

// CircuitBreakerDecorator immediately trips a per-host circuit breaker on non-retryable auth/sudo/hostkey errors
// so pam_faillock never locks out ops accounts (D1 §2.4).
type CircuitBreakerDecorator struct {
	next    ports.Executor
	mu      sync.Mutex
	tripped map[string]error
}

func NewCircuitBreakerDecorator(next ports.Executor) *CircuitBreakerDecorator {
	return &CircuitBreakerDecorator{
		next:    next,
		tripped: make(map[string]error),
	}
}

func (c *CircuitBreakerDecorator) Connect(ctx context.Context, target ports.Target) (ports.Session, error) {
	c.mu.Lock()
	if err, ok := c.tripped[target.HostID]; ok {
		c.mu.Unlock()
		return nil, sdk.NewError(sdk.CodeAuthUnauthenticated, "AUTH", http.StatusForbidden, sdk.CodeAuthUnauthenticated, false,
			fmt.Errorf("circuit breaker tripped for host %s (protecting pam_faillock): %w", target.HostID, err))
	}
	c.mu.Unlock()

	sess, err := c.next.Connect(ctx, target)
	if err != nil {
		if isAuthTripError(err) {
			c.mu.Lock()
			c.tripped[target.HostID] = err
			c.mu.Unlock()
		}
		return nil, err
	}
	return &cbSession{next: sess, hostID: target.HostID, cb: c}, nil
}

func (c *CircuitBreakerDecorator) Reset(hostID string) {
	c.mu.Lock()
	delete(c.tripped, hostID)
	c.mu.Unlock()
}

func (c *CircuitBreakerDecorator) Close() error { return c.next.Close() }

func isAuthTripError(err error) bool {
	return sdk.IsCode(err, sdk.CodeAuthUnauthenticated) ||
		sdk.IsCode(err, sdk.CodeExecHostKeyMismatch) ||
		sdk.IsCode(err, sdk.CodeExecSudoFailed)
}

type cbSession struct {
	next   ports.Session
	hostID string
	cb     *CircuitBreakerDecorator
}

func (s *cbSession) Run(ctx context.Context, cmd ports.Command) (ports.Result, error) {
	res, err := s.next.Run(ctx, cmd)
	if err != nil && isAuthTripError(err) {
		s.cb.mu.Lock()
		s.cb.tripped[s.hostID] = err
		s.cb.mu.Unlock()
	}
	return res, err
}

func (s *cbSession) Upload(ctx context.Context, r io.Reader, dst string, mode fs.FileMode, owner string) (string, error) {
	return s.next.Upload(ctx, r, dst, mode, owner)
}
func (s *cbSession) Download(ctx context.Context, src string, w io.Writer) error {
	return s.next.Download(ctx, src, w)
}
func (s *cbSession) Stat(ctx context.Context, p string) (*ports.RemoteFileInfo, error) {
	return s.next.Stat(ctx, p)
}
func (s *cbSession) Close() error { return s.next.Close() }

// RedactingAuditDecorator wraps command stdout/stderr with obs.RedactWriter and logs execution summaries (D1 §1.3-A).
type RedactingAuditDecorator struct {
	next    ports.Executor
	logger  *slog.Logger
	secrets func() []string
}

func NewRedactingAuditDecorator(next ports.Executor, logger *slog.Logger, secretsFn func() []string) *RedactingAuditDecorator {
	return &RedactingAuditDecorator{next: next, logger: logger, secrets: secretsFn}
}

func (r *RedactingAuditDecorator) Connect(ctx context.Context, target ports.Target) (ports.Session, error) {
	sess, err := r.next.Connect(ctx, target)
	if err != nil {
		if r.logger != nil {
			r.logger.WarnContext(ctx, "ssh connect failed",
				slog.String("host_id", target.HostID),
				slog.String("err_code", sdk.ErrorCode(err)))
		}
		return nil, err
	}
	var initSecrets []string
	if len(target.Auth.Password) > 0 {
		initSecrets = append(initSecrets, string(target.Auth.Password))
	}
	if len(target.Auth.SudoPassword) > 0 {
		initSecrets = append(initSecrets, string(target.Auth.SudoPassword))
	}
	if len(target.Auth.Passphrase) > 0 {
		initSecrets = append(initSecrets, string(target.Auth.Passphrase))
	}
	return &redactingSession{next: sess, hostID: target.HostID, logger: r.logger, extraSecrets: initSecrets, secretsFn: r.secrets}, nil
}

func (r *RedactingAuditDecorator) Close() error { return r.next.Close() }

type redactingSession struct {
	next         ports.Session
	hostID       string
	logger       *slog.Logger
	extraSecrets []string
	secretsFn    func() []string
}

func (s *redactingSession) allSecrets() []string {
	out := append([]string(nil), s.extraSecrets...)
	if s.secretsFn != nil {
		out = append(out, s.secretsFn()...)
	}
	return out
}

func (s *redactingSession) Run(ctx context.Context, cmd ports.Command) (ports.Result, error) {
	sec := s.allSecrets()
	var rwOut, rwErr *obs.RedactWriter
	if cmd.Stdout != nil {
		rwOut = obs.NewRedactWriter(cmd.Stdout, sec...)
		cmd.Stdout = rwOut
	}
	if cmd.Stderr != nil {
		rwErr = obs.NewRedactWriter(cmd.Stderr, sec...)
		cmd.Stderr = rwErr
	}
	res, err := s.next.Run(ctx, cmd)
	if rwOut != nil {
		_ = rwOut.Close()
	}
	if rwErr != nil {
		_ = rwErr.Close()
	}
	res.Stdout = obs.RedactString(res.Stdout, sec...)
	res.Stderr = obs.RedactString(res.Stderr, sec...)
	if s.logger != nil {
		s.logger.DebugContext(ctx, "ssh command finished",
			slog.String("host_id", s.hostID),
			slog.Int("exit_code", res.ExitCode),
			slog.Int64("duration_ms", res.Duration.Milliseconds()))
	}
	return res, err
}

func (s *redactingSession) Upload(ctx context.Context, r io.Reader, dst string, mode fs.FileMode, owner string) (string, error) {
	return s.next.Upload(ctx, r, dst, mode, owner)
}
func (s *redactingSession) Download(ctx context.Context, src string, w io.Writer) error {
	return s.next.Download(ctx, src, w)
}
func (s *redactingSession) Stat(ctx context.Context, p string) (*ports.RemoteFileInfo, error) {
	return s.next.Stat(ctx, p)
}
func (s *redactingSession) Close() error { return s.next.Close() }

// BuildExecutor assembles the canonical D1 §1.3-A decorator chain:
// BaseExecutor -> RetryDecorator -> CircuitBreakerDecorator -> RedactingAuditDecorator.
func BuildExecutor(pool *Pool, logger *slog.Logger) ports.Executor {
	var exec ports.Executor = NewBaseExecutor(pool)
	exec = NewRetryDecorator(exec, WithMaxAttempts(4), WithJitter(0.25))
	exec = NewCircuitBreakerDecorator(exec)
	exec = NewRedactingAuditDecorator(exec, logger, nil)
	return exec
}

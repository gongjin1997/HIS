package sshx_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gssh "github.com/gliderlabs/ssh"
	"golang.org/x/crypto/ssh"

	"github.com/kinstall/kinstall/internal/executor/sshx"
	"github.com/kinstall/kinstall/internal/ports"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

func startFakeSSHServer(t *testing.T, expectedPass string, authAttempts *atomic.Int32) (string, int, string, func()) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	fp := sshx.FingerprintSHA256(signer.PublicKey())

	srv := &gssh.Server{
		HostSigners: []gssh.Signer{signer},
		PasswordHandler: func(_ gssh.Context, password string) bool {
			if authAttempts != nil {
				authAttempts.Add(1)
			}
			return password == expectedPass
		},
		Handler: func(s gssh.Session) {
			rawCmd := s.RawCommand()
			c := exec.Command("sh", "-c", rawCmd)
			c.Stdin = s
			c.Stdout = s
			c.Stderr = s.Stderr()
			if err := c.Run(); err != nil {
				var ee *exec.ExitError
				if errors.As(err, &ee) {
					_ = s.Exit(ee.ExitCode())
					return
				}
				_ = s.Exit(1)
				return
			}
			_ = s.Exit(0)
		},
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = srv.Serve(ln) }()

	host, portStr, _ := net.SplitHostPort(ln.Addr().String())
	var port int
	_, _ = fmt.Sscanf(portStr, "%d", &port)
	return host, port, fp, func() {
		_ = srv.Close()
		_ = ln.Close()
	}
}

func TestSSHPoolConcurrencyHostKeyCircuitBreakerStdinUploadAndAsyncDetached(t *testing.T) {
	var attempts atomic.Int32
	host, port, fp, stop := startFakeSSHServer(t, "testpass123", &attempts)
	defer stop()

	tmpDir := t.TempDir()
	pool := sshx.NewPool(sshx.PoolConfig{
		MaxSessionsPerHost: 8,
		MaxDialsPerBastion: 6,
		KeepAliveInterval:  30 * time.Second,
	})
	defer pool.Close()

	execChain := sshx.BuildExecutor(pool, nil)
	ctx := context.Background()

	// 1. TOFU captures fingerprint and 20 concurrent channels succeed under MaxSessions=8 semaphore
	var tofuFP string
	target := ports.Target{
		HostID:      "host_01TEST",
		Addr:        host,
		Port:        port,
		User:        "ops",
		Auth:        ports.AuthMaterial{Password: []byte("testpass123")},
		HostKeyMode: ports.HostKeyTOFU,
		SudoMode:    ports.SudoNone,
		WorkDir:     tmpDir,
		OnHostKeyTOFU: func(_ context.Context, _ string, observed string) error {
			tofuFP = observed
			return nil
		},
	}
	sess, err := execChain.Connect(ctx, target)
	if err != nil {
		t.Fatalf("TOFU connect failed: %v", err)
	}
	if tofuFP != fp {
		t.Fatalf("TOFU fingerprint mismatch: got %s, want %s", tofuFP, fp)
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			res, rerr := sess.Run(ctx, ports.Command{Script: fmt.Sprintf("echo hello-%d", idx)})
			if rerr != nil || !strings.Contains(res.Stdout, fmt.Sprintf("hello-%d", idx)) {
				errCh <- fmt.Errorf("goroutine %d failed: err=%w out=%q", idx, rerr, res.Stdout)
			}
		}(i)
	}
	wg.Wait()
	close(errCh)
	for e := range errCh {
		t.Fatal(e)
	}

	// 2. SFTP disabled on gliderlabs/ssh -> automatically falls back to stdin streaming + atomic rename + SHA-256!
	uploadDst := filepath.Join(tmpDir, "nested", "payload.bin")
	payload := bytes.Repeat([]byte("kinstall-atomic-transfer-"), 4096)
	wantSum := sha256.Sum256(payload)
	gotSHA, err := sess.Upload(ctx, bytes.NewReader(payload), uploadDst, 0o640, "")
	if err != nil {
		t.Fatalf("Upload fallback failed: %v", err)
	}
	if gotSHA != hex.EncodeToString(wantSum[:]) {
		t.Fatalf("SHA mismatch: got %s", gotSHA)
	}
	diskBytes, err := os.ReadFile(uploadDst)
	if err != nil || !bytes.Equal(diskBytes, payload) {
		t.Fatalf("uploaded file mismatch: err=%v len=%d", err, len(diskBytes))
	}

	// 3. Async Detached execution (runs in background, writes rc, streams stdout)
	var liveOut bytes.Buffer
	asyncRes, err := sess.Run(ctx, ports.Command{
		Script:    "echo async-line-1; sleep 0.2; echo async-line-2",
		Async:     true,
		TaskRunID: "tr_01ASYNC",
		Stdout:    &liveOut,
	})
	if err != nil {
		t.Fatalf("Async Detached failed: %v", err)
	}
	if !strings.Contains(asyncRes.Stdout, "async-line-1") || !strings.Contains(asyncRes.Stdout, "async-line-2") {
		t.Fatalf("unexpected async stdout: %q", asyncRes.Stdout)
	}
	if !strings.Contains(liveOut.String(), "async-line-2") {
		t.Fatalf("streaming stdout missing line: %q", liveOut.String())
	}
	_ = sess.Close()

	// 4. HostKey mismatch must fail immediately with KI-EXEC-0002 and Retryable=false
	badFPTarget := target
	badFPTarget.HostID = "host_02BADFP"
	badFPTarget.HostKeyMode = ports.HostKeyPinned
	badFPTarget.HostKeyFP = "SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	if _, err := execChain.Connect(ctx, badFPTarget); !sdk.IsCode(err, sdk.CodeExecHostKeyMismatch) {
		t.Fatalf("expected KI-EXEC-0002, got %v", err)
	}

	// 5. Wrong password MUST NOT retry (0 retries -> exactly 1 server auth attempt) and trips circuit breaker (D1 §2.4)
	attempts.Store(0)
	badAuthTarget := target
	badAuthTarget.HostID = "host_03BADAUTH"
	badAuthTarget.Auth = ports.AuthMaterial{Password: []byte("wrong-pass")}
	badAuthTarget.HostKeyFP = fp
	if _, err := execChain.Connect(ctx, badAuthTarget); !sdk.IsCode(err, sdk.CodeAuthUnauthenticated) {
		t.Fatalf("expected KI-AUTH-0001, got %v", err)
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("auth error was retried! expected 1 attempt (0 retries), got %d", got)
	}
	// Second call must be blocked immediately by circuit breaker without hitting server!
	if _, err := execChain.Connect(ctx, badAuthTarget); err == nil {
		t.Fatal("expected circuit breaker to reject second call")
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("circuit breaker failed to prevent second auth attempt! got %d", got)
	}
	_ = io.Discard
}

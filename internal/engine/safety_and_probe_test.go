package engine

import (
	"bytes"
	"context"
	"io"
	"io/fs"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kinstall/kinstall/internal/ports"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

type stubProbeSession struct {
	runFn func(ctx context.Context, cmd ports.Command) (ports.Result, error)
}

func (s *stubProbeSession) Run(ctx context.Context, cmd ports.Command) (ports.Result, error) {
	return s.runFn(ctx, cmd)
}
func (s *stubProbeSession) Upload(context.Context, io.Reader, string, fs.FileMode, string) (string, error) {
	return "", nil
}
func (s *stubProbeSession) Download(context.Context, string, io.Writer) error { return nil }
func (s *stubProbeSession) Stat(context.Context, string) (*ports.RemoteFileInfo, error) {
	return &ports.RemoteFileInfo{}, nil
}
func (s *stubProbeSession) Close() error { return nil }

func TestValidateCommandSafety(t *testing.T) {
	blocked := []string{
		"rm -rf /",
		"rm -rf /*",
		"rm -fr --no-preserve-root /etc",
		":(){ :|:& };:",
		"dd if=/dev/zero of=/dev/sda bs=1M",
		"chmod -R 777 /",
		"mkfs.ext4 /dev/sdb1",
		"echo boom; > /dev/vda",
	}
	for _, cmd := range blocked {
		if err := ValidateCommandSafety(cmd); err == nil {
			t.Fatalf("expected high-risk command %q to be blocked", cmd)
		}
	}

	allowed := []string{
		"rm -rf /var/lib/kinstall/tmp/stage",
		"systemctl restart postgresql",
		"chmod 0600 /etc/kinstall/pg.key",
		"mkdir -p /data/valkey && chown valkey:valkey /data/valkey",
	}
	for _, cmd := range allowed {
		if err := ValidateCommandSafety(cmd); err != nil {
			t.Fatalf("expected safe command %q to be allowed, got %v", cmd, err)
		}
	}
}

func TestValidateTargetPathAndCollisionFreeBackup(t *testing.T) {
	blocked := []string{
		"",
		"relative/path.conf",
		"/etc/kinstall/../../etc/shadow",
		"/etc/shadow",
		"/etc/passwd",
		"/etc/sudoers",
		"/boot/grub/grub.cfg",
		"/proc/sys/vm/swappiness",
		"/sys/kernel/mm/transparent_hugepage/enabled",
		"/dev/sda",
	}
	for _, p := range blocked {
		if err := ValidateTargetPath(p); err == nil {
			t.Fatalf("expected target path %q to be blocked", p)
		}
	}

	allowed := []string{
		"/etc/kinstall/postgresql/postgresql.conf",
		"/etc/hosts",
		"/etc/systemd/system/valkey.service",
		"/data/compose/docker-compose.yml",
	}
	for _, p := range allowed {
		if err := ValidateTargetPath(p); err != nil {
			t.Fatalf("expected target path %q to be allowed, got %v", p, err)
		}
	}

	b1 := collisionFreeBackupPath("op_123", "/etc/app1/config.yaml")
	b2 := collisionFreeBackupPath("op_123", "/etc/app2/config.yaml")
	if b1 == b2 {
		t.Fatalf("expected distinct backup paths for same basename in different dirs, both got %s", b1)
	}
	if !strings.HasPrefix(b1, "/var/lib/kinstall/backups/op_123/") {
		t.Fatalf("unexpected backup prefix: %s", b1)
	}
}

func TestWaitTCPAndHTTPProbeOverRemoteSSHSession(t *testing.T) {
	var tcpCalls atomic.Int32
	sess := &stubProbeSession{
		runFn: func(_ context.Context, cmd ports.Command) (ports.Result, error) {
			n := tcpCalls.Add(1)
			if strings.Contains(cmd.Script, "5432") {
				// Fail on attempt 1, succeed on attempts 2 and 3 (testing successThreshold=2)
				if n == 1 {
					return ports.Result{ExitCode: 1}, nil
				}
				return ports.Result{ExitCode: 0}, nil
			}
			if strings.Contains(cmd.Script, "https://127.0.0.1:6443/readyz") {
				return ports.Result{ExitCode: 0}, nil
			}
			return ports.Result{ExitCode: 1}, nil
		},
	}

	rc := &RunContext{
		Host:    sdk.HostView{ID: "hst_remote_01", Name: "pg-node-01", Address: "10.20.1.11"},
		Session: sess,
	}

	tcpTask := &WaitTCPTask{}
	tcpSpec := sdk.WaitTCP("127.0.0.1:5432", 2*time.Second).
		WithParam("initialDelayMs", 10).
		WithParam("intervalMs", 10).
		WithParam("successThreshold", 2)

	res, err := tcpTask.Apply(context.Background(), rc, tcpSpec)
	if err != nil {
		t.Fatalf("expected remote SSH TCP probe to succeed, got error: %v", err)
	}
	if res.Output != "reachable" {
		t.Fatalf("unexpected output: %q", res.Output)
	}
	if got := tcpCalls.Load(); got != 3 {
		t.Fatalf("expected 3 SSH probe calls (1 fail + 2 consecutive passes), got %d", got)
	}

	httpTask := &WaitHTTPTask{}
	httpSpec := sdk.WaitHTTP("https://127.0.0.1:6443/readyz", 2*time.Second).WithParam("intervalMs", 10)
	chk, err := httpTask.Check(context.Background(), rc, httpSpec)
	if err != nil || !chk.Converged {
		t.Fatalf("expected remote SSH HTTP probe Check to converge, got converged=%v err=%v", chk.Converged, err)
	}
}

func TestCmdRunBlocksDangerousScript(t *testing.T) {
	called := false
	rc := &RunContext{
		Session: &stubProbeSession{
			runFn: func(_ context.Context, _ ports.Command) (ports.Result, error) {
				called = true
				return ports.Result{}, nil
			},
		},
	}
	task := &CmdRunTask{}
	_, err := task.Apply(context.Background(), rc, sdk.CmdRun("rm -rf /", ""))
	if err == nil {
		t.Fatal("expected CmdRunTask.Apply to block rm -rf /")
	}
	if called {
		t.Fatal("remote session should NEVER be called when safety guard blocks command")
	}
	// Ensure buffer import unused check doesn't happen
	_ = bytes.NewReader(nil)
}

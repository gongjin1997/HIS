package inventory_test

import (
	"context"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kinstall/kinstall/internal/inventory"
)

func TestDiscoverSubnetAndSSHKeyDistribution(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		for {
			c, aerr := ln.Accept()
			if aerr != nil {
				return
			}
			_, _ = c.Write([]byte("SSH-2.0-OpenSSH_9.9_kinstall-test\r\n"))
			_ = c.Close()
		}
	}()

	port := ln.Addr().(*net.TCPAddr).Port
	found, err := inventory.DiscoverSubnet(context.Background(), "127.0.0.1/32", port, time.Second, 4)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(found) != 1 || !found[0].Reachable || !strings.HasPrefix(found[0].Banner, "SSH-2.0-OpenSSH") {
		t.Fatalf("expected 1 reachable SSH banner host, got %+v", found)
	}

	script, err := inventory.BuildSSHKeyDistributeScript("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITestKey ops@kinstall")
	if err != nil || !strings.Contains(script, "authorized_keys") {
		t.Fatalf("expected valid key script, got %q err=%v", script, err)
	}
	if _, err := inventory.BuildSSHKeyDistributeScript("ssh-ed25519 bad;rm -rf /"); err == nil {
		t.Fatalf("expected shell injection key to be rejected")
	}
}

func TestFactsSchedulerGocronV2(t *testing.T) {
	var ticks atomic.Int32
	sched, err := inventory.NewFactsScheduler(40*time.Millisecond, nil, func(_ context.Context) {
		ticks.Add(1)
	})
	if err != nil {
		t.Fatalf("new scheduler: %v", err)
	}
	sched.Start()
	deadline := time.Now().Add(2 * time.Second)
	for ticks.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if err := sched.Shutdown(); err != nil {
		t.Fatalf("shutdown scheduler: %v", err)
	}
	if ticks.Load() < 2 {
		t.Fatalf("expected >= 2 gocron/v2 ticks, got %d", ticks.Load())
	}
}

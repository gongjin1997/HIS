package domain_test

import (
	"strings"
	"testing"

	"github.com/kinstall/kinstall/internal/domain"
)

func TestOpStateMachine(t *testing.T) {
	valid := [][2]domain.OpState{
		{domain.OpAwaitingApproval, domain.OpPending},
		{domain.OpAwaitingApproval, domain.OpRejected},
		{domain.OpAwaitingApproval, domain.OpCancelled},
		{domain.OpPending, domain.OpRunning},
		{domain.OpPending, domain.OpCancelled},
		{domain.OpRunning, domain.OpRunning},
		{domain.OpRunning, domain.OpSucceeded},
		{domain.OpRunning, domain.OpFailed},
		{domain.OpRunning, domain.OpCancelled},
		{domain.OpFailed, domain.OpPending},
	}
	for _, pair := range valid {
		if !pair[0].CanTransitionTo(pair[1]) {
			t.Errorf("expected transition %s -> %s to be allowed", pair[0], pair[1])
		}
	}

	invalid := [][2]domain.OpState{
		{domain.OpSucceeded, domain.OpRunning},
		{domain.OpSucceeded, domain.OpPending},
		{domain.OpRejected, domain.OpPending},
		{domain.OpCancelled, domain.OpRunning},
		{domain.OpAwaitingApproval, domain.OpRunning},
	}
	for _, pair := range invalid {
		if pair[0].CanTransitionTo(pair[1]) {
			t.Errorf("expected transition %s -> %s to be forbidden", pair[0], pair[1])
		}
	}
}

func TestPrefixedULIDAndDNS1123(t *testing.T) {
	id := domain.NewID(domain.PrefixHost)
	if !strings.HasPrefix(id, "host_") || len(id) > 48 {
		t.Fatalf("invalid generated id: %q", id)
	}
	if !domain.ValidateDNS1123("node-x86-01") {
		t.Fatal("valid DNS-1123 rejected")
	}
	for _, bad := range []string{"", "Node_01", "-bad", "bad-", "a.b"} {
		if domain.ValidateDNS1123(bad) {
			t.Fatalf("invalid DNS-1123 accepted: %q", bad)
		}
	}
}

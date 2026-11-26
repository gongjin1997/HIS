package planner_test

import (
	"strings"
	"testing"

	"github.com/kinstall/kinstall/internal/planner"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

func TestEvaluate3WayAll6ScenariosAndCreateOnly(t *testing.T) {
	const (
		hA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		hB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		hC = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	)

	// Scenario 1: base == live == desired -> Skipped
	r1 := planner.Evaluate3Way("/etc/x", hA, hA, hA, true, planner.DiffOptions{})
	if r1.Decision != planner.DiffSkipped || r1.Scenario != 1 {
		t.Fatalf("scenario 1 mismatch: %+v", r1)
	}

	// Scenario 2: base == live != desired -> Update
	r2 := planner.Evaluate3Way("/etc/x", hA, hA, hB, true, planner.DiffOptions{})
	if r2.Decision != planner.DiffUpdate || r2.Scenario != 2 {
		t.Fatalf("scenario 2 mismatch: %+v", r2)
	}

	// Scenario 3: pure external drift -> blocked KI-DIFF-0001; --adopt converges; --force-override updates
	r3 := planner.Evaluate3Way("/etc/x", hA, hB, hA, true, planner.DiffOptions{})
	if r3.Decision != planner.DiffBlocked || !sdk.IsCode(r3.Err, sdk.CodeDiffExternalDrift) {
		t.Fatalf("scenario 3 expected KI-DIFF-0001, got %+v", r3)
	}
	r3Adopt := planner.Evaluate3Way("/etc/x", hA, hB, hA, true, planner.DiffOptions{Adopt: true})
	if r3Adopt.Decision != planner.DiffConverged {
		t.Fatalf("scenario 3 adopt expected converged, got %+v", r3Adopt)
	}

	// Scenario 4: 3-way conflict -> blocked KI-DIFF-0002; --force-override updates
	r4 := planner.Evaluate3Way("/etc/x", hA, hB, hC, true, planner.DiffOptions{})
	if r4.Decision != planner.DiffBlocked || !sdk.IsCode(r4.Err, sdk.CodeDiffThreeWayConflict) {
		t.Fatalf("scenario 4 expected KI-DIFF-0002, got %+v", r4)
	}
	r4Force := planner.Evaluate3Way("/etc/x", hA, hB, hC, true, planner.DiffOptions{ForceOverride: true})
	if r4Force.Decision != planner.DiffUpdate {
		t.Fatalf("scenario 4 force expected update, got %+v", r4Force)
	}

	// Scenario 5: already converged remotely -> Converged (no overwrite)
	r5 := planner.Evaluate3Way("/etc/x", hA, hB, hB, true, planner.DiffOptions{})
	if r5.Decision != planner.DiffConverged || r5.Scenario != 5 {
		t.Fatalf("scenario 5 mismatch: %+v", r5)
	}

	// Scenario 6: unmanaged existing file -> blocked KI-DIFF-0003 unless adoptExisting=true
	r6 := planner.Evaluate3Way("/etc/x", "", hB, hA, true, planner.DiffOptions{})
	if r6.Decision != planner.DiffBlocked || !sdk.IsCode(r6.Err, sdk.CodeDiffUnmanagedExists) {
		t.Fatalf("scenario 6 expected KI-DIFF-0003, got %+v", r6)
	}
	r6Adopt := planner.Evaluate3Way("/etc/x", "", hB, hA, true, planner.DiffOptions{AdoptExisting: true})
	if r6Adopt.Decision != planner.DiffUpdate {
		t.Fatalf("scenario 6 adoptExisting expected update, got %+v", r6Adopt)
	}

	// create-only mode (Sentinel self-mutated sentinel.conf) -> ALWAYS Skipped when exists!
	rCO := planner.Evaluate3Way("/etc/sentinel.conf", hA, hB, hC, true, planner.DiffOptions{ManageMode: sdk.ManageModeCreateOnly})
	if rCO.Decision != planner.DiffSkipped {
		t.Fatalf("create-only must skip existing file, got %+v", rCO)
	}

	// managed-block preserves surrounding user config
	orig := "127.0.0.1 localhost\n# custom user line\n"
	m1 := planner.MergeManagedBlock(orig, "k8s-hosts", "10.0.1.10 master-1")
	m2 := planner.MergeManagedBlock(m1, "k8s-hosts", "10.0.1.10 master-1\n10.0.1.11 master-2")
	if !strings.Contains(m2, "# custom user line") || !strings.Contains(m2, "master-2") || strings.Count(m2, "# BEGIN kinstall:k8s-hosts") != 1 {
		t.Fatalf("managed-block merge broken:\n%s", m2)
	}
}

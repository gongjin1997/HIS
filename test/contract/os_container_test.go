package contract_test

import (
	"os"
	"os/exec"
	"testing"

	"github.com/kinstall/kinstall/internal/osadapter"
)

// TestRealContainerPOSIXFactsContract executes the 1-RTT zero-Python POSIX Facts collector
// inside real Linux containers (Debian/Alpine) when KINSTALL_TEST_DOCKER=1 (T-M0-09, G3 §2).
func TestRealContainerPOSIXFactsContract(t *testing.T) {
	if os.Getenv("KINSTALL_TEST_DOCKER") != "1" {
		t.Skip("set KINSTALL_TEST_DOCKER=1 to run live Docker OS Facts contract tests")
	}
	eng, err := osadapter.LoadEmbedded()
	if err != nil {
		t.Fatalf("load osadapter: %v", err)
	}
	images := []struct {
		image      string
		wantFamily string
	}{
		{"python:3.11-slim-bookworm", "debian-like"},
		{"python:3.12-slim", "debian-like"},
	}
	for _, tc := range images {
		t.Run(tc.image, func(t *testing.T) {
			cmd := exec.Command("docker", "run", "--rm", tc.image, "sh", "-c", osadapter.FactsCollectorScript)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("docker run %s failed: %v\n%s", tc.image, err, string(out))
			}
			facts, err := osadapter.ParseFactsOutput(string(out), "127.0.0.1", eng)
			if err != nil {
				t.Fatalf("parse facts from %s: %v\nraw:\n%s", tc.image, err, string(out))
			}
			if facts.OS.Family != tc.wantFamily {
				t.Fatalf("expected OS family %q, got %+v", tc.wantFamily, facts.OS)
			}
			if facts.Arch == "" || facts.PageSize <= 0 {
				t.Fatalf("missing arch/pageSize from real container facts: %+v", facts)
			}
			t.Logf("Container %s => rule=%s family=%s arch=%s pageSize=%d cgroup=v%d kernel=%s",
				tc.image, facts.OS.RuleID, facts.OS.Family, facts.Arch, facts.PageSize, facts.CgroupVer, facts.Kernel)
		})
	}
}

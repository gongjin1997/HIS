package sdktest

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// FakeBOM is an in-memory BOMView implementation for pure unit testing of plugins.
type FakeBOM struct {
	Rev       string
	Artifacts []sdk.BOMArtifact
	Images    map[string]string
}

func (b *FakeBOM) Revision() string {
	if b.Rev == "" {
		return "2026.11.3"
	}
	return b.Rev
}

func (b *FakeBOM) HasArtifact(name, arch string, pageSize int) bool {
	_, ok := b.GetArtifact(name, arch, pageSize)
	return ok
}

func (b *FakeBOM) GetArtifact(name, arch string, pageSize int) (sdk.BOMArtifact, bool) {
	for _, a := range b.Artifacts {
		if a.Name != name {
			continue
		}
		if a.Arch != "" && arch != "" && a.Arch != arch {
			continue
		}
		if pageSize > 0 && len(a.PageSizes) > 0 {
			matched := false
			for _, ps := range a.PageSizes {
				if ps == pageSize {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		return a, true
	}
	return sdk.BOMArtifact{}, false
}

func (b *FakeBOM) ResolveImage(logical string) (string, bool) {
	if b.Images == nil {
		return "", false
	}
	v, ok := b.Images[logical]
	return v, ok
}

// FakeClaimsTracker detects port, path, and Docker subnet collisions in unit tests.
type FakeClaimsTracker struct {
	ports   map[string]map[string]bool
	dirs    map[string]map[string]bool
	subnets map[string]map[string]bool
}

func NewFakeClaimsTracker() *FakeClaimsTracker {
	return &FakeClaimsTracker{
		ports:   make(map[string]map[string]bool),
		dirs:    make(map[string]map[string]bool),
		subnets: make(map[string]map[string]bool),
	}
}

func (c *FakeClaimsTracker) Check(hostID string, claims sdk.HostRoleClaims) error {
	if c.ports[hostID] == nil {
		c.ports[hostID] = make(map[string]bool)
		c.dirs[hostID] = make(map[string]bool)
		c.subnets[hostID] = make(map[string]bool)
	}
	for _, p := range claims.Ports {
		proto := p.Proto
		if proto == "" {
			proto = "tcp"
		}
		k := fmt.Sprintf("%d/%s", p.Port, proto)
		if c.ports[hostID][k] {
			return fmt.Errorf("port %s already claimed on host %s", k, hostID)
		}
		c.ports[hostID][k] = true
	}
	for _, d := range claims.Dirs {
		if c.dirs[hostID][d] {
			return fmt.Errorf("dir %s already claimed on host %s", d, hostID)
		}
		c.dirs[hostID][d] = true
	}
	for _, s := range claims.Subnets {
		if c.subnets[hostID][s] {
			return fmt.Errorf("subnet %s already claimed on host %s", s, hostID)
		}
		c.subnets[hostID][s] = true
	}
	return nil
}

// AssertGolden compares actual bytes against a golden file; when update is true, overwrites it.
func AssertGolden(t *testing.T, goldenPath string, actual []byte, update bool) {
	t.Helper()
	if update {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("mkdir golden dir: %v", err)
		}
		if err := os.WriteFile(goldenPath, actual, 0o644); err != nil {
			t.Fatalf("write golden file: %v", err)
		}
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s: %v", goldenPath, err)
	}
	if string(actual) != string(want) {
		t.Fatalf("golden mismatch for %s:\n--- want ---\n%s\n--- got ---\n%s", goldenPath, string(want), string(actual))
	}
}

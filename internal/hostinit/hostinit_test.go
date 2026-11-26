package hostinit_test

import (
	"testing"

	"github.com/kinstall/kinstall/internal/hostinit"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

func Test22Modules7ProfilesConflictAndDrift(t *testing.T) {
	mods := hostinit.AllModules()
	if len(mods) != 22 {
		t.Fatalf("expected 22 hostinit modules, got %d", len(mods))
	}
	if mods[0].Name != "backup" || mods[len(mods)-1].Name != "verify" {
		t.Fatalf("modules must start with backup and end with verify, got %s..%s", mods[0].Name, mods[len(mods)-1].Name)
	}

	profiles := hostinit.BuiltinProfiles()
	if len(profiles) != 7 {
		t.Fatalf("expected 7 built-in profiles, got %d", len(profiles))
	}

	// Compatible stack: k8s-node + search -> succeeds
	merged, err := hostinit.MergeProfiles([]string{"k8s-node", "search"})
	if err != nil {
		t.Fatalf("compatible stack failed: %v", err)
	}
	if merged.Sysctls["vm.max_map_count"] != "1048576" || merged.Sysctls["vm.overcommit_memory"] != "1" {
		t.Fatalf("unexpected merged sysctls: %+v", merged.Sysctls)
	}

	// Hard mutually-exclusive stack: k8s-node (overcommit=1) + database-pg (overcommit=2) -> MUST block!
	if _, err := hostinit.MergeProfiles([]string{"k8s-node", "database-pg"}); !sdk.IsCode(err, sdk.CodeInitVerifyFailed) {
		t.Fatalf("expected KI-INIT-0001 on conflicting profiles, got %v", err)
	}

	// Drift check catches manual vm.swappiness change (T-M1-08)
	cacheProf, _ := hostinit.MergeProfiles([]string{"cache"})
	drifts := hostinit.CheckHostDrift(cacheProf, sdk.FactsView{SwapMB: 0}, map[string]string{"vm.swappiness": "60"}, "always madvise [never]")
	if len(drifts) != 1 || drifts[0].Key != "vm.swappiness" {
		t.Fatalf("expected vm.swappiness drift finding, got %+v", drifts)
	}
}

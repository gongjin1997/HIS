package valkey_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
	"github.com/kinstall/kinstall/pkg/sdk/v1alpha1/sdktest"
	"github.com/kinstall/kinstall/plugins/valkey"
)

func TestValkeyGuardrailsRingAntiAffinityAndSentinelCreateOnly(t *testing.T) {
	p := valkey.New()
	ctx := context.Background()

	// 1. COW memory ceiling formula: 0.65 with persistence vs 0.80 cache-only
	if got := valkey.ComputeMaxMemoryMB(16384, 2048, "aof-rdb"); got != 9318 {
		t.Fatalf("expected 9318 MiB (0.65x), got %d", got)
	}
	if got := valkey.ComputeMaxMemoryMB(16384, 2048, "none"); got != 11468 {
		t.Fatalf("expected 11468 MiB (0.80x), got %d", got)
	}

	// 2. Deterministic Ring Anti-Affinity across 3 AZs (AZ1->AZ2, AZ2->AZ3, AZ3->AZ1)
	azHosts := []sdk.HostView{
		{ID: "h1", Name: "vk-1", Address: "10.0.1.1", FaultZone: "az-1", Facts: sdk.FactsView{Arch: "arm64", PageSize: 65536, GlibcVersion: "2.28", MemMB: 32768}},
		{ID: "h2", Name: "vk-2", Address: "10.0.2.1", FaultZone: "az-2", Facts: sdk.FactsView{Arch: "arm64", PageSize: 65536, GlibcVersion: "2.28", MemMB: 32768}},
		{ID: "h3", Name: "vk-3", Address: "10.0.3.1", FaultZone: "az-3", Facts: sdk.FactsView{Arch: "arm64", PageSize: 65536, GlibcVersion: "2.28", MemMB: 32768}},
	}
	pairs, err := valkey.PlanClusterRingAntiAffinity(azHosts, 6379)
	if err != nil || len(pairs) != 3 {
		t.Fatalf("ring anti-affinity failed: %v %+v", err, pairs)
	}
	for _, pair := range pairs {
		if pair.MasterHost == pair.ReplicaHost || pair.MasterZone == pair.ReplicaZone {
			t.Fatalf("anti-affinity violated: %+v", pair)
		}
	}

	// 3. CentOS 7 glibc 2.17 on host-binary MUST be rejected with KI-OS-0003
	oldHost := sdk.HostView{ID: "h-c7", Name: "c7", Address: "10.0.0.9", Facts: sdk.FactsView{Arch: "amd64", PageSize: 4096, GlibcVersion: "2.17", MemMB: 8192}}
	specStandalone, _ := json.Marshal(map[string]any{"topology": "standalone", "port": 6379})
	repOld, _ := p.Validate(ctx, &sdk.ValidateRequest{
		Instance: sdk.InstanceView{ID: "ins1", Name: "vk-old", Product: "valkey", ProductVersion: "9.1.4", DeployTarget: sdk.TargetHostBinary, Spec: specStandalone},
		Hosts:    []sdk.HostView{oldHost},
	})
	if !repOld.HasBlocking() || repOld.Findings[0].Code != sdk.CodeOSGlibcTooOld {
		t.Fatalf("expected KI-OS-0003 for glibc 2.17 host-binary, got %+v", repOld.Findings)
	}

	// 4. 64K host with BOM missing 64K artifact MUST block with KI-PLAN-0012
	bom4KOnly := &sdktest.FakeBOM{Artifacts: []sdk.BOMArtifact{{Name: "valkey", Arch: "arm64", PageSizes: []int{4096}}}}
	specCluster, _ := json.Marshal(map[string]any{"topology": "cluster", "port": 6379})
	rep64K, _ := p.Validate(ctx, &sdk.ValidateRequest{
		Instance: sdk.InstanceView{ID: "ins2", Name: "vk-64k", Product: "valkey", ProductVersion: "9.1.4", DeployTarget: sdk.TargetHostBinary, Spec: specCluster},
		Hosts:    azHosts,
		BOM:      bom4KOnly,
	})
	if !rep64K.HasBlocking() || rep64K.Findings[0].Code != sdk.CodePlanArchPageMismatch {
		t.Fatalf("expected KI-PLAN-0012 when 64K artifact missing, got %+v", rep64K.Findings)
	}

	// 5. Sentinel plan MUST mark sentinel.conf as ManageModeCreateOnly & Cluster bootstrap MUST use REDISCLI_AUTH (never -a)
	specSentinel, _ := json.Marshal(map[string]any{"topology": "replication-sentinel", "port": 6379})
	planSent, err := p.Plan(ctx, &sdk.PlanRequest{
		Instance: sdk.InstanceView{ID: "ins3", Name: "vk-sent", Product: "valkey", ProductVersion: "9.1.4", DeployTarget: sdk.TargetHostBinary, Spec: specSentinel},
		Hosts:    azHosts,
	})
	if err != nil {
		t.Fatalf("sentinel plan failed: %v", err)
	}
	foundCreateOnly := false
	for _, ph := range planSent.Pipeline.Phases {
		for _, tk := range ph.Tasks {
			if tk.Key == "vk-sentinel-conf" && tk.ManageMode == sdk.ManageModeCreateOnly {
				foundCreateOnly = true
			}
		}
	}
	if !foundCreateOnly {
		t.Fatalf("sentinel.conf MUST use ManageModeCreateOnly")
	}

	planClus, err := p.Plan(ctx, &sdk.PlanRequest{
		Instance: sdk.InstanceView{ID: "ins4", Name: "vk-clus", Product: "valkey", ProductVersion: "9.1.4", DeployTarget: sdk.TargetHostBinary, Spec: specCluster},
		Hosts:    azHosts,
	})
	if err != nil {
		t.Fatalf("cluster plan failed: %v", err)
	}
	for _, ph := range planClus.Pipeline.Phases {
		for _, tk := range ph.Tasks {
			if sc, ok := tk.Params["script"].(string); ok {
				if strings.Contains(sc, " -a ") {
					t.Fatalf("CRITICAL: valkey-cli must NEVER pass password via -a flag: %s", sc)
				}
			}
		}
	}

	// 6. Redis red-list license blocked unless explicitly accepted
	repRed, _ := p.Validate(ctx, &sdk.ValidateRequest{
		Instance: sdk.InstanceView{ID: "ins5", Name: "r8", Product: "redis", ProductVersion: "8.0.0", DeployTarget: sdk.TargetHostContainer, Spec: specStandalone},
		Hosts:    []sdk.HostView{oldHost},
	})
	if !repRed.HasBlocking() {
		t.Fatalf("expected Redis 8.x without acceptRedLicense to be blocked")
	}
}

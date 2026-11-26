package kubernetes_test

import (
	"context"
	"encoding/json"
	"testing"

	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
	"github.com/kinstall/kinstall/pkg/sdk/v1alpha1/sdktest"
	"github.com/kinstall/kinstall/plugins/kubernetes"
)

func TestKubernetesPluginValidationMatrixAndGoldenPipeline(t *testing.T) {
	p := kubernetes.New()
	ctx := context.Background()

	// 1. Calico + kube-proxy LCD matrix: all modern -> nftables/NFT; one legacy Kylin V10 4.19 -> unified iptables/Iptables!
	modernHosts := []sdk.HostView{
		{ID: "h1", Name: "cp-1", Address: "10.0.0.1", Roles: []string{"control-plane"}, Facts: sdk.FactsView{Kernel: "5.14.0-427", NFTVersion: "1.0.9", Arch: "amd64", PageSize: 4096, CgroupVer: 2, MemMB: 32768}},
		{ID: "h2", Name: "cp-2", Address: "10.0.0.2", Roles: []string{"control-plane"}, Facts: sdk.FactsView{Kernel: "6.6.0-28", NFTVersion: "1.0.9", Arch: "arm64", PageSize: 65536, CgroupVer: 2, MemMB: 32768}},
		{ID: "h3", Name: "cp-3", Address: "10.0.0.3", Roles: []string{"control-plane"}, Facts: sdk.FactsView{Kernel: "6.8.0-45", NFTVersion: "1.0.9", Arch: "amd64", PageSize: 4096, CgroupVer: 2, MemMB: 32768}},
	}
	pm, cm := kubernetes.ResolveProxyAndCalicoMode(modernHosts)
	if pm != "nftables" || cm != "NFT" {
		t.Fatalf("expected nftables/NFT on modern cluster, got %s/%s", pm, cm)
	}
	mixedWithKylinV10 := append([]sdk.HostView(nil), modernHosts...)
	mixedWithKylinV10[1].Facts.Kernel = "4.19.90-52"
	pm2, cm2 := kubernetes.ResolveProxyAndCalicoMode(mixedWithKylinV10)
	if pm2 != "iptables" || cm2 != "Iptables" {
		t.Fatalf("expected unified iptables/Iptables downgrade when any node has 4.19 kernel, got %s/%s", pm2, cm2)
	}

	// 2. ADR-033 exact node memory reservation formula
	resv := kubernetes.ComputeNodeReservation(65536, 0, 110, true)
	// kubeReserved = min(255+11*110=1465, 16384)=1465; sys=1024+512=1536; evict=min(0.02*65536=1310,1024)=1024
	if resv.KubeReservedMi != 1465 || resv.SystemReservedMi != 1536 || resv.EvictionHardMi != 1024 {
		t.Fatalf("ADR-033 reservation mismatch: %+v", resv)
	}
	if resv.OSReserveMi != 1465+1536+1024 {
		t.Fatalf("OSReserveMi mismatch: %d", resv.OSReserveMi)
	}

	// 3. Cross-minor upgrade jump 1.35 -> 1.37 MUST be blocked; 1.36 -> 1.37 allowed
	if err := kubernetes.ValidateUpgradePath("1.35.4", "1.37.2"); err == nil {
		t.Fatalf("expected 1.35 -> 1.37 jump to fail")
	}
	if err := kubernetes.ValidateUpgradePath("1.36.3", "1.37.2"); err != nil {
		t.Fatalf("expected 1.36 -> 1.37 sequential upgrade to pass: %v", err)
	}

	// 4. Validate + Plan produces 10-phase install pipeline with 127.0.0.1:8443 local proxy
	bom := &sdktest.FakeBOM{
		Rev: "2026.11.3",
		Artifacts: []sdk.BOMArtifact{
			{Name: "kubernetes-server", Arch: "amd64", PageSizes: []int{4096}},
			{Name: "kubernetes-server", Arch: "arm64", PageSizes: []int{4096, 65536}},
		},
	}
	specJSON, _ := json.Marshal(map[string]any{"clusterName": "his-k8s", "controlPlaneEndpoint": "10.0.0.10:6443", "csiSelinuxMount": true})
	inst := sdk.InstanceView{ID: "ins_k8s1", Product: "kubernetes", ProductVersion: "1.37.2", DeployTarget: sdk.TargetHostBinary, Spec: specJSON}
	rep, err := p.Validate(ctx, &sdk.ValidateRequest{Instance: inst, Hosts: modernHosts, BOM: bom})
	if err != nil || rep.HasBlocking() {
		t.Fatalf("validate failed: err=%v findings=%+v", err, rep.Findings)
	}
	plan, err := p.Plan(ctx, &sdk.PlanRequest{Instance: inst, Hosts: modernHosts, BOM: bom, Operation: "install"})
	if err != nil {
		t.Fatalf("plan failed: %v", err)
	}
	if len(plan.Pipeline.Phases) != 10 {
		t.Fatalf("expected 10-phase production K8s install pipeline, got %d", len(plan.Pipeline.Phases))
	}

	// 5. StateMigrator v1 -> v2
	toRev, nextState, err := p.MigrateState(1, json.RawMessage(`{"cluster":"his"}`))
	if err != nil || toRev != 2 || len(nextState) == 0 {
		t.Fatalf("StateMigrator failed: rev=%d err=%v state=%s", toRev, err, nextState)
	}
}

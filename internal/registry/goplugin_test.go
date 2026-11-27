package registry_test

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/kinstall/kinstall/internal/registry"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

func TestHashicorpGoPluginGRPCAndCrashIsolation(t *testing.T) {
	ctx := context.Background()
	binPath := filepath.Join(t.TempDir(), "external-csi-plugin")
	build := exec.Command("go", "build", "-o", binPath, "../../examples/external-csi-plugin")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build external plugin example: %v\n%s", err, string(out))
	}

	comp, err := registry.LoadGoPlugin(ctx, binPath)
	if err != nil {
		t.Fatalf("LoadGoPlugin failed: %v", err)
	}
	defer comp.Kill()

	if comp.Meta().Name != "vendor-csi-external" || comp.Meta().SDKVersion != sdk.SDKAPIVersion {
		t.Fatalf("unexpected meta: %+v", comp.Meta())
	}
	rep, err := comp.Validate(ctx, &sdk.ValidateRequest{Instance: sdk.InstanceView{Name: "csi-prod"}})
	if err != nil || rep.HasBlocking() {
		t.Fatalf("validate failed: %+v err=%v", rep, err)
	}
	plan, err := comp.Plan(ctx, &sdk.PlanRequest{Instance: sdk.InstanceView{Name: "csi-prod"}, Operation: "install"})
	if err != nil || plan.Pipeline == nil || len(plan.Pipeline.Phases) != 1 {
		t.Fatalf("plan failed: %+v err=%v", plan, err)
	}
	st, err := comp.Status(ctx, &sdk.StatusRequest{Instance: sdk.InstanceView{Name: "csi-prod"}})
	if err != nil || st.Health != "healthy" {
		t.Fatalf("status failed: %+v err=%v", st, err)
	}

	// Crash/kill isolation (T-M5-22 DoD): killing the child plugin process MUST NOT crash host and subsequent RPC returns clean error
	comp.Kill()
	if _, err := comp.Status(ctx, &sdk.StatusRequest{Instance: sdk.InstanceView{Name: "csi-prod"}}); err == nil {
		t.Fatalf("expected RPC error after killing external plugin subprocess")
	}
}

// Example external CSI / HSM process plugin using hashicorp/go-plugin v1.8.0 over local gRPC (T-M5-22, ADR-044/061).
// Runs in a completely isolated subprocess: receives ONLY read-only InstanceView/HostView and returns ONLY PipelineSpec.
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/kinstall/kinstall/internal/registry"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

type vendorCSIPlugin struct{}

func (v *vendorCSIPlugin) Meta(_ context.Context) (sdk.PluginMeta, error) {
	return sdk.PluginMeta{
		Name:        "vendor-csi-external",
		Version:     "1.0.0",
		SDKVersion:  sdk.SDKAPIVersion,
		Type:        sdk.PluginComponent,
		Description: "Example out-of-process gRPC vendor CSI / HSM plugin via hashicorp/go-plugin v1.8.0",
		Products: []sdk.ProductRange{{
			Product: "vendor-csi", Versions: ">=1.0.0", DefaultVer: "1.0.0",
			Targets: []sdk.DeployTarget{sdk.TargetKubernetes}, LicenseTier: "green",
		}},
		Platforms:  sdk.Platforms{Arch: []string{"amd64", "arm64"}, PageSizes: []int{4096, 65536}},
		Operations: []string{"install", "uninstall"},
		ParamSchema: map[string]any{
			"title": "Vendor CSI Driver (External gRPC Plugin)",
			"properties": map[string]any{
				"storageClassName": map[string]any{"type": "string", "default": "vendor-nvme-sc", "x-kinstall-ui": map[string]any{"group": "basic", "order": 1, "label": "StorageClass 名称"}},
				"selinuxMount":     map[string]any{"type": "boolean", "default": false, "x-kinstall-ui": map[string]any{"group": "security", "order": 2, "label": "SELinuxMount 支持 (ADR-032)"}},
			},
		},
		OutputSchema: map[string]string{"storageClass": "string"},
	}, nil
}

func (v *vendorCSIPlugin) Validate(_ context.Context, _ *sdk.ValidateRequest) (*sdk.ValidationReport, error) {
	return sdk.NewValidationReport(), nil
}

func (v *vendorCSIPlugin) Plan(_ context.Context, req *sdk.PlanRequest) (*sdk.PlanResult, error) {
	pb := sdk.NewPipelineBuilder()
	pb.Phase(sdk.PhaseSpec{
		Name: "vendor-csi-deploy",
		Tasks: []sdk.TaskSpec{
			sdk.HelmRelease(sdk.HelmReleaseSpec{
				ReleaseName: "vendor-csi-" + req.Instance.Name,
				Namespace:   "kube-system",
				ChartRef:    "charts/vendor-csi-1.0.0.tgz",
				Version:     "1.0.0",
				Atomic:      true,
				Wait:        true,
			}).WithKey("vendor-csi-helm"),
		},
	})
	return &sdk.PlanResult{
		Summary:  fmt.Sprintf("External gRPC vendor-csi %s (%s)", req.Instance.Name, req.Operation),
		Pipeline: pb.Build(),
		Outputs:  map[string]any{"storageClass": "vendor-nvme-sc"},
	}, nil
}

func (v *vendorCSIPlugin) Status(_ context.Context, _ *sdk.StatusRequest) (*sdk.InstanceStatus, error) {
	return &sdk.InstanceStatus{Health: "healthy", Message: "vendor-csi external gRPC ready", CheckedAt: time.Now().UTC()}, nil
}

func main() {
	registry.ServeExternalGRPCPlugin(&vendorCSIPlugin{})
}

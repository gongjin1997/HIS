package registry_test

import (
	"context"
	"testing"

	"github.com/kinstall/kinstall/internal/registry"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

type stubComp struct{ meta sdk.PluginMeta }

func (s stubComp) Meta() sdk.PluginMeta { return s.meta }
func (s stubComp) Validate(context.Context, *sdk.ValidateRequest) (*sdk.ValidationReport, error) {
	return sdk.NewValidationReport(), nil
}
func (s stubComp) Plan(context.Context, *sdk.PlanRequest) (*sdk.PlanResult, error) {
	return &sdk.PlanResult{}, nil
}
func (s stubComp) Operation(string) (sdk.Operation, bool) { return nil, false }
func (s stubComp) Status(context.Context, *sdk.StatusRequest) (*sdk.InstanceStatus, error) {
	return &sdk.InstanceStatus{Health: "healthy"}, nil
}

func TestRegistryFreezeAndCompat(t *testing.T) {
	r := registry.New()
	err := r.RegisterComponent(stubComp{meta: sdk.PluginMeta{
		Name: "valkey", Version: "0.2.0", SDKVersion: sdk.SDKAPIVersion, Type: sdk.PluginComponent,
		Products: []sdk.ProductRange{{Product: "valkey", Versions: ">=9.0.0, <10.0.0", Targets: []sdk.DeployTarget{sdk.TargetHostBinary, sdk.TargetHostContainer}}},
	}})
	if err != nil {
		t.Fatalf("RegisterComponent: %v", err)
	}
	r.Freeze()
	if err := r.RegisterComponent(stubComp{meta: sdk.PluginMeta{Name: "other", SDKVersion: sdk.SDKAPIVersion}}); err == nil {
		t.Fatal("expected error registering after Freeze")
	}
	if _, err := r.Resolve("valkey", sdk.TargetHostBinary, "9.1.4"); err != nil {
		t.Fatalf("expected resolve success: %v", err)
	}
	if _, err := r.Resolve("valkey", sdk.TargetKubernetes, "9.1.4"); !sdk.IsCode(err, sdk.CodePluginIncompatible) {
		t.Fatalf("expected KI-PLG-0001 for unsupported target, got %v", err)
	}
	if _, err := r.Resolve("valkey", sdk.TargetHostBinary, "8.0.0"); !sdk.IsCode(err, sdk.CodePluginIncompatible) {
		t.Fatalf("expected KI-PLG-0001 for version out of range, got %v", err)
	}
}

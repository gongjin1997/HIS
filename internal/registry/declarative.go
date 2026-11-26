package registry

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"sigs.k8s.io/yaml"

	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// DeclarativeManifest defines an M5 YAML/OCI declarative plugin strictly constrained to the 15 built-in Tasks (T-M5-09, ADR-022/069).
type DeclarativeManifest struct {
	APIVersion string                  `json:"apiVersion"`
	Kind       string                  `json:"kind"`
	Meta       sdk.PluginMeta          `json:"meta"`
	Operations map[string]sdk.Pipeline `json:"operations"`
	Outputs    map[string]any          `json:"outputs,omitempty"`
}

// allowedBuiltinTaskTypes freezes the exact 15 platform Tasks available to declarative plugins.
var allowedBuiltinTaskTypes = map[string]bool{
	"file.sync": true, "template.render": true, "pkg.ensure": true, "binary.install": true,
	"systemd.unit": true, "sysctl.apply": true, "kmod.ensure": true, "cmd.run": true,
	"wait.tcp": true, "wait.http": true, "cert.issue": true, "helm.release": true,
	"docker.ensure": true, "image.load": true, "compose.sync": true,
}

// LoadDeclarativeYAML parses, validates (15-task sandbox), and wraps a declarative YAML/OCI plugin as sdk.Component.
func LoadDeclarativeYAML(raw []byte) (sdk.Component, error) {
	var m DeclarativeManifest
	if err := yaml.Unmarshal(raw, &m); err != nil {
		return nil, sdk.NewError(sdk.CodePluginIncompatible, "PLG", http.StatusUnprocessableEntity, sdk.CodePluginIncompatible, false, fmt.Errorf("parse declarative plugin: %w", err))
	}
	if m.Meta.Name == "" || len(m.Meta.Products) == 0 {
		return nil, sdk.NewError(sdk.CodePluginIncompatible, "PLG", http.StatusUnprocessableEntity, sdk.CodePluginIncompatible, false, fmt.Errorf("declarative plugin missing meta.name or products"))
	}
	if m.Meta.SDKVersion == "" {
		m.Meta.SDKVersion = sdk.SDKAPIVersion
	}
	if m.Meta.Type == "" {
		m.Meta.Type = sdk.PluginComponent
	}
	for opName, pl := range m.Operations {
		for _, ph := range pl.Phases {
			for _, tk := range ph.Tasks {
				if !allowedBuiltinTaskTypes[tk.Type] {
					return nil, sdk.NewError(sdk.CodeEngUnknownTask, "PLG", http.StatusUnprocessableEntity, sdk.CodeEngUnknownTask, false,
						fmt.Errorf("declarative plugin %s op %s references disallowed task type %q", m.Meta.Name, opName, tk.Type))
				}
			}
		}
	}
	return &declarativeComponent{manifest: m}, nil
}

type declarativeComponent struct {
	manifest DeclarativeManifest
}

func (d *declarativeComponent) Meta() sdk.PluginMeta { return d.manifest.Meta }
func (d *declarativeComponent) Validate(_ context.Context, _ *sdk.ValidateRequest) (*sdk.ValidationReport, error) {
	return sdk.NewValidationReport(), nil
}
func (d *declarativeComponent) Plan(_ context.Context, req *sdk.PlanRequest) (*sdk.PlanResult, error) {
	opName := req.Operation
	if opName == "" {
		opName = "install"
	}
	pl, ok := d.manifest.Operations[opName]
	if !ok {
		return nil, sdk.NewError(sdk.CodeAPIInvalidRequest, "PLG", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, fmt.Errorf("declarative plugin %s has no operation %q", d.manifest.Meta.Name, opName))
	}
	cp := pl
	return &sdk.PlanResult{
		Summary:  fmt.Sprintf("Declarative plugin %s (%s)", d.manifest.Meta.Name, opName),
		Pipeline: &cp,
		Outputs:  d.manifest.Outputs,
	}, nil
}
func (d *declarativeComponent) Operation(name string) (sdk.Operation, bool) {
	pl, ok := d.manifest.Operations[name]
	if !ok {
		return nil, false
	}
	cp := pl
	return &declarativeOp{name: name, pl: &cp}, true
}
func (d *declarativeComponent) Status(_ context.Context, _ *sdk.StatusRequest) (*sdk.InstanceStatus, error) {
	return &sdk.InstanceStatus{Health: "healthy", Message: "declarative plugin active", CheckedAt: time.Now().UTC()}, nil
}

type declarativeOp struct {
	name string
	pl   *sdk.Pipeline
}

func (o *declarativeOp) Name() string   { return o.name }
func (o *declarativeOp) Mutating() bool { return o.name != "status" && o.name != "backup" }
func (o *declarativeOp) Preflight(_ context.Context, _ *sdk.OpRequest) (*sdk.ValidationReport, error) {
	return sdk.NewValidationReport(), nil
}
func (o *declarativeOp) Build(_ context.Context, _ *sdk.OpRequest) (*sdk.Pipeline, error) {
	return o.pl, nil
}

package v1alpha1

import (
	"context"
	"encoding/json"
)

// ClaimsChecker validates whether proposed host claims collide with existing claims.
type ClaimsChecker func(hostID string, claims HostRoleClaims) error

// ValidateRequest carries read-only context for Component.Validate.
type ValidateRequest struct {
	Instance    InstanceView
	Hosts       []HostView
	BOM         BOMView
	CheckClaims ClaimsChecker
}

// PlanRequest carries read-only context for Component.Plan.
type PlanRequest struct {
	Instance    InstanceView
	Hosts       []HostView
	BOM         BOMView
	Operation   string
	CheckClaims ClaimsChecker
}

// StatusRequest carries read-only context for Component.Status.
type StatusRequest struct {
	Instance InstanceView
	Hosts    []HostView
}

// OpRequest carries read-only context for Operation.Preflight and Operation.Build.
type OpRequest struct {
	Instance   InstanceView
	Hosts      []HostView
	BOM        BOMView
	Params     map[string]any
	Extensions []Extension
}

// ExtRequest carries read-only context for Extension hooks.
type ExtRequest struct {
	Instance InstanceView
	Hosts    []HostView
	BOM      BOMView
	Params   map[string]any
}

// Contribution is returned by an Extension to enrich a Component pipeline.
type Contribution struct {
	Phases       []PhaseSpec
	TemplateVars map[string]any
	Warnings     []Finding
}

// Component is the core interface implemented by every product plugin (P1-P10).
// Plugins MUST be side-effect free and MUST NOT execute SSH or DB calls directly.
type Component interface {
	Meta() PluginMeta
	Validate(ctx context.Context, req *ValidateRequest) (*ValidationReport, error)
	Plan(ctx context.Context, req *PlanRequest) (*PlanResult, error)
	Operation(name string) (Operation, bool)
	Status(ctx context.Context, req *StatusRequest) (*InstanceStatus, error)
}

// Operation describes a lifecycle action (install, scale, upgrade, backup, restore, reconfigure, uninstall).
type Operation interface {
	Name() string
	Mutating() bool
	Preflight(ctx context.Context, req *OpRequest) (*ValidationReport, error)
	Build(ctx context.Context, req *OpRequest) (*Pipeline, error)
}

// Extension implements a component extension point (e.g. kubernetes.cri, kubernetes.cni, kubernetes.csi).
type Extension interface {
	Meta() PluginMeta
	Point() string
	HostRequirements(ctx context.Context, req *ExtRequest) (*HostRequirements, error)
	Contribute(ctx context.Context, req *ExtRequest) (*Contribution, error)
}

// StateMigrator is an optional interface implemented by plugins that evolve instances.state_json across versions (G1 §6.2).
type StateMigrator interface {
	MigrateState(fromRev int, raw json.RawMessage) (toRev int, next json.RawMessage, err error)
}

// Constructor is exported as New() by every built-in plugin and registered explicitly in cmd/kinstall/plugins.go.
type Constructor func() any

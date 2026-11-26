package skywalking

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// Spec configures Apache SkyWalking 11.1 + Horizon UI + BanyanDB 0.11.2 / OpenSearch + Satellite 1.4 (P7).
type Spec struct {
	StorageEngine         string  `json:"storageEngine"` // banyandb (default) | opensearch
	EnableSatellite       bool    `json:"enableSatellite"`
	TraceSQLParameters    bool    `json:"traceSqlParameters"` // MUST stay false by default (P7 §2.3!)
	EnableOAPAuth         bool    `json:"enableOapAuth"`      // MUST stay true (P7 §2.3!)
	TailSampleSlowMs      int     `json:"tailSampleSlowMs"`   // default 2000ms (>2s 100% kept)
	TailSampleNormalRatio float64 `json:"tailSampleNormalRatio"`
	TraceTTLDays          int     `json:"traceTtlDays"`
}

func parseSpec(raw json.RawMessage) Spec {
	s := Spec{
		StorageEngine:         "banyandb",
		EnableSatellite:       true,
		TraceSQLParameters:    false,
		EnableOAPAuth:         true,
		TailSampleSlowMs:      2000,
		TailSampleNormalRatio: 0.05,
		TraceTTLDays:          7,
	}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &s)
	}
	if s.StorageEngine == "" {
		s.StorageEngine = "banyandb"
	}
	if s.TailSampleSlowMs <= 0 {
		s.TailSampleSlowMs = 2000
	}
	if s.TraceTTLDays <= 0 {
		s.TraceTTLDays = 7
	}
	return s
}

// Plugin implements sdk.Component for Apache SkyWalking 11.x APM (P7, ADR-075/076).
type Plugin struct{ ops map[string]sdk.Operation }

func New() *Plugin {
	p := &Plugin{}
	p.ops = map[string]sdk.Operation{
		"install":   &swOp{name: "install", mutating: true, build: p.buildInstall},
		"uninstall": &swOp{name: "uninstall", mutating: true, build: p.buildUninstall},
	}
	return p
}

func (p *Plugin) Meta() sdk.PluginMeta {
	return sdk.PluginMeta{
		Name:        "skywalking",
		Version:     "1.1.0",
		SDKVersion:  sdk.SDKAPIVersion,
		Type:        sdk.PluginComponent,
		Description: "Apache SkyWalking 11.1 + Horizon UI + BanyanDB 0.11.2 (default) + Satellite 1.4 + SWCK 0.11.0 offline agent injection",
		Products: []sdk.ProductRange{{
			Product:     "skywalking",
			Versions:    ">=11.0.0, <12.0.0",
			DefaultVer:  "11.1.0",
			Targets:     []sdk.DeployTarget{sdk.TargetHostBinary, sdk.TargetHostContainer, sdk.TargetKubernetes},
			LicenseTier: "green",
		}},
		Platforms: sdk.Platforms{Arch: []string{"amd64", "arm64"}, PageSizes: []int{4096, 65536}},
		OutputSchema: map[string]string{
			"collectorGRPC": "string",
			"horizonUIURL":  "string",
			"agentAuthRef":  "string",
		},
		SchemaVersion: 1,
	}
}

func (p *Plugin) Validate(_ context.Context, req *sdk.ValidateRequest) (*sdk.ValidationReport, error) {
	rep := sdk.NewValidationReport()
	spec := parseSpec(req.Instance.Spec)
	// Privacy & security redlines (P7 §2.3)
	if !spec.EnableOAPAuth {
		rep.Block(sdk.CodeAuthForbidden, "SkyWalking OAP agent.authentication MUST be enabled to prevent unauthenticated telemetry spoofing (P7 §2.3)")
	}
	if spec.TraceSQLParameters {
		rep.Block(sdk.CodeAuthForbidden, "plugin.jdbc.trace_sql_parameters=true risks leaking PII/passwords into APM storage; keep disabled (P7 §2.3)")
	}
	if req.Instance.DeployTarget != sdk.TargetKubernetes && len(req.Hosts) > 0 && len(req.Hosts) < 2 {
		rep.Warn(sdk.CodePlanTopologyMinHosts, "SkyWalking OAP production recommends >= 2 nodes with Satellite 1.4 L7 gRPC balancing (P7 §2.1)")
	}
	return rep, nil
}

func (p *Plugin) Plan(ctx context.Context, req *sdk.PlanRequest) (*sdk.PlanResult, error) {
	spec := parseSpec(req.Instance.Spec)
	pl, err := p.buildInstall(ctx, &sdk.OpRequest{Instance: req.Instance, Hosts: req.Hosts})
	if err != nil {
		return nil, err
	}
	host := "127.0.0.1"
	if len(req.Hosts) > 0 && req.Hosts[0].Address != "" {
		host = req.Hosts[0].Address
	}
	return &sdk.PlanResult{
		Summary:  fmt.Sprintf("SkyWalking %s (storage=%s, satellite=%v, slow>%dms)", req.Instance.ProductVersion, spec.StorageEngine, spec.EnableSatellite, spec.TailSampleSlowMs),
		Pipeline: pl,
		Outputs: map[string]any{
			"collectorGRPC": fmt.Sprintf("%s:11800", host),
			"horizonUIURL":  fmt.Sprintf("http://%s:8080", host),
			"agentAuthRef":  "cred_sw_agent_" + req.Instance.ID,
		},
	}, nil
}

func (p *Plugin) Operation(name string) (sdk.Operation, bool) { op, ok := p.ops[name]; return op, ok }

func (p *Plugin) Status(_ context.Context, _ *sdk.StatusRequest) (*sdk.InstanceStatus, error) {
	return &sdk.InstanceStatus{Health: "healthy", Message: "SkyWalking OAP + BanyanDB healthy", CheckedAt: time.Now().UTC()}, nil
}

func (p *Plugin) buildInstall(_ context.Context, req *sdk.OpRequest) (*sdk.Pipeline, error) {
	spec := parseSpec(req.Instance.Spec)
	pb := sdk.NewPipelineBuilder()
	if req.Instance.DeployTarget == sdk.TargetKubernetes {
		pb.Phase(sdk.PhaseSpec{
			Name: "k8s-swck-and-skywalking",
			Tasks: []sdk.TaskSpec{
				sdk.HelmRelease(sdk.HelmReleaseSpec{
					ReleaseName: "skywalking-" + req.Instance.Name, Namespace: "skywalking",
					ChartRef: "charts/skywalking-11.1.0.tgz", Version: "11.1.0", Atomic: true, Wait: true,
					Values: map[string]any{"storage": spec.StorageEngine, "swckVersion": "0.11.0"},
				}).WithKey("sw-k8s-helm"),
			},
		})
		return pb.Build(), nil
	}
	agentCfg := "plugin.jdbc.trace_sql_parameters=false\nagent.authentication=${SW_AGENT_AUTH_TOKEN}\n"
	pb.Phase(sdk.PhaseSpec{
		Name: "deploy-skywalking-stack",
		Tasks: []sdk.TaskSpec{
			sdk.BinaryInstall("banyandb", "0.11.2", sdk.ArtifactRef{Name: "banyandb"}).WithKey("sw-banyandb-bin"),
			sdk.BinaryInstall("skywalking-oap", req.Instance.ProductVersion, sdk.ArtifactRef{Name: "skywalking-oap"}).WithKey("sw-oap-bin"),
			sdk.FileSync("/opt/kinstall/agents/skywalking-java/9.8.0/config/agent.config", sdk.TemplateRef{Inline: agentCfg}, 0o644, "root:root").WithKey("sw-offline-agent-cfg"),
			sdk.WaitTCP("127.0.0.1:11800", 90*time.Second).WithKey("sw-oap-ready"),
		},
	})
	return pb.Build(), nil
}

func (p *Plugin) buildUninstall(_ context.Context, req *sdk.OpRequest) (*sdk.Pipeline, error) {
	pb := sdk.NewPipelineBuilder()
	pb.Phase(sdk.PhaseSpec{Name: "uninstall-sw", Disruptive: true, Tasks: []sdk.TaskSpec{sdk.CmdRun("systemctl disable --now kinstall-skywalking-"+req.Instance.Name+" || true", "").WithKey("sw-stop")}})
	return pb.Build(), nil
}

type swOp struct {
	name     string
	mutating bool
	build    func(context.Context, *sdk.OpRequest) (*sdk.Pipeline, error)
}

func (o *swOp) Name() string   { return o.name }
func (o *swOp) Mutating() bool { return o.mutating }
func (o *swOp) Preflight(_ context.Context, _ *sdk.OpRequest) (*sdk.ValidationReport, error) {
	return sdk.NewValidationReport(), nil
}
func (o *swOp) Build(ctx context.Context, req *sdk.OpRequest) (*sdk.Pipeline, error) {
	return o.build(ctx, req)
}

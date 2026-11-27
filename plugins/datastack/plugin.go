package datastack

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// GenericServiceSpec defines a container service inside the generic-app stack (P10 §5, R2).
type GenericServiceSpec struct {
	Name       string            `json:"name"`
	Image      string            `json:"image"`
	Ports      []string          `json:"ports,omitempty"`
	Mounts     []sdk.MountSpec   `json:"mounts,omitempty"`
	Privileged bool              `json:"privileged,omitempty"`
	EnvSecrets map[string]string `json:"envSecretRefs,omitempty"`
}

// Spec configures P10 data/AI/office/monitoring/generic-app components.
type Spec struct {
	Port                 int                  `json:"port"`
	AttuPort             int                  `json:"attuPort"` // Milvus Attu defaults to 19001 (avoids R2 SonarQube 9000 clash!)
	AuthorizationEnabled bool                 `json:"authorizationEnabled"`
	AcceptRedLicense     bool                 `json:"acceptRedLicense"`
	ArbiterMode          bool                 `json:"arbiterMode,omitempty"`  // MongoDB forbids 2+Arbiter
	Neo4jOnlinePlugins   bool                 `json:"neo4jOnlinePlugins"`     // forbidden offline (R2)
	OnlyOfficeMode       string               `json:"onlyOfficeMode"`         // docs (default) | legacy-workspace
	AllowPrivileged      bool                 `json:"allowPrivileged"`        // requires four-eyes approval
	MountDockerSock      bool                 `json:"mountDockerSock"`        // strictly forbidden
	RancherEngine        string               `json:"rancherEngine"`          // rke2 | kinstall (blocks EOL rke1)
	DockerSubnet         string               `json:"dockerSubnet,omitempty"` // generic-app subnet claim
	SharedNetworkRef     string               `json:"sharedNetworkRef,omitempty"`
	FirewallAllowCIDRs   []string             `json:"firewallAllowCIDRs,omitempty"`
	Services             []GenericServiceSpec `json:"services,omitempty"`
}

func parseSpec(raw json.RawMessage, prod string) Spec {
	s := Spec{
		Port:                 19530,
		AttuPort:             19001,
		AuthorizationEnabled: true,
		OnlyOfficeMode:       "docs",
		RancherEngine:        "kinstall",
	}
	switch prod {
	case "mongodb":
		s.Port = 27017
	case "neo4j":
		s.Port = 7687
	case "onlyoffice":
		s.Port = 8088
	case "zabbix":
		s.Port = 10051
	case "generic-app":
		s.Port = 8080
	}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &s)
	}
	if s.AttuPort <= 0 {
		s.AttuPort = 19001
	}
	return s
}

// Plugin implements sdk.Component for P10 (Milvus, MongoDB, Neo4j, OnlyOffice, Rancher, Zabbix 7, generic-app).
type Plugin struct{ ops map[string]sdk.Operation }

func New() *Plugin {
	p := &Plugin{}
	p.ops = map[string]sdk.Operation{
		"install":   &dsOp{name: "install", mutating: true, build: p.buildInstall},
		"uninstall": &dsOp{name: "uninstall", mutating: true, build: p.buildUninstall},
	}
	return p
}

func (p *Plugin) Meta() sdk.PluginMeta {
	return sdk.PluginMeta{
		Name:        "datastack-apps",
		Version:     "1.2.0",
		SDKVersion:  sdk.SDKAPIVersion,
		Type:        sdk.PluginComponent,
		Description: "P10 AI/Data/Collab/App Ecosystem: Milvus 2.6.26/3.0.5 (CVE-2026-26190 guard + Attu 19001), MongoDB 8.0, Neo4j offline APOC, OnlyOffice 10.0, Rancher 2.14, Zabbix 7, generic-app",
		Products: []sdk.ProductRange{
			{Product: "milvus", Versions: ">=2.6.26, <4.0.0", DefaultVer: "2.6.26", Targets: []sdk.DeployTarget{sdk.TargetHostContainer, sdk.TargetKubernetes}, LicenseTier: "green"},
			{Product: "mongodb", Versions: ">=8.0.0, <9.0.0", DefaultVer: "8.0.37", Targets: []sdk.DeployTarget{sdk.TargetHostBinary, sdk.TargetHostContainer, sdk.TargetKubernetes}, LicenseTier: "red"},
			{Product: "neo4j", Versions: ">=5.26.0, <7.0.0", DefaultVer: "5.26.0", Targets: []sdk.DeployTarget{sdk.TargetHostBinary, sdk.TargetHostContainer}, LicenseTier: "red"},
			{Product: "onlyoffice", Versions: ">=9.0.0, <12.0.0", DefaultVer: "10.0.0", Targets: []sdk.DeployTarget{sdk.TargetHostContainer, sdk.TargetKubernetes}, LicenseTier: "red"},
			{Product: "rancher", Versions: ">=2.12.0, <3.0.0", DefaultVer: "2.14.2", Targets: []sdk.DeployTarget{sdk.TargetKubernetes}, LicenseTier: "green"},
			{Product: "zabbix", Versions: ">=7.0.0, <8.0.0", DefaultVer: "7.4.0", Targets: []sdk.DeployTarget{sdk.TargetHostBinary, sdk.TargetHostContainer}, LicenseTier: "red"},
			{Product: "generic-app", Versions: ">=1.0.0, <10.0.0", DefaultVer: "1.0.0", Targets: []sdk.DeployTarget{sdk.TargetHostContainer}, LicenseTier: "green"},
		},
		Platforms:  sdk.Platforms{Arch: []string{"amd64", "arm64"}, PageSizes: []int{4096, 65536}},
		Operations: []string{"install", "uninstall"},
		ParamSchema: map[string]any{
			"title": "数据/AI/办公与应用栈参数 (Milvus / MongoDB / Neo4j / OnlyOffice / Zabbix / generic-app)",
			"properties": map[string]any{
				"port":                 map[string]any{"type": "integer", "default": 19530, "minimum": 80, "maximum": 65535, "x-kinstall-ui": map[string]any{"group": "network", "order": 1, "label": "主服务端口"}},
				"attuPort":             map[string]any{"type": "integer", "default": 19001, "minimum": 1024, "maximum": 65535, "x-kinstall-ui": map[string]any{"group": "network", "order": 2, "label": "Milvus Attu 端口（默认 19001 避开 Sonar 9000）"}},
				"authorizationEnabled": map[string]any{"type": "boolean", "default": true, "x-kinstall-ui": map[string]any{"group": "security", "order": 3, "label": "强制鉴权（防 CVE-2026-26190）"}},
				"dockerSubnet":         map[string]any{"type": "string", "default": "172.28.0.0/24", "x-kinstall-ui": map[string]any{"group": "network", "order": 4, "widget": "cidr-allocator", "label": "Docker 自定义网桥子网（防撞网预检）"}},
				"onlyOfficeMode":       map[string]any{"type": "string", "enum": []string{"docs", "legacy-workspace"}, "default": "docs", "x-kinstall-ui": map[string]any{"group": "basic", "order": 5, "label": "OnlyOffice 形态"}},
				"acceptRedLicense":     map[string]any{"type": "boolean", "default": false, "x-kinstall-ui": map[string]any{"group": "advanced", "order": 6, "label": "确认红灯许可证（SSPLv1/AGPLv3 客户自建）", "riskWarning": "MongoDB/Neo4j/OnlyOffice/Zabbix 属于红灯协议，禁止进入公共离线交付包"}},
			},
		},
		OutputSchema:  map[string]string{"endpoint": "string", "attuUrl": "string"},
		SchemaVersion: 1,
	}
}

func (p *Plugin) Validate(_ context.Context, req *sdk.ValidateRequest) (*sdk.ValidationReport, error) {
	rep := sdk.NewValidationReport()
	prod := strings.ToLower(req.Instance.Product)
	spec := parseSpec(req.Instance.Spec, prod)

	// Red-list license guard (ADR-059)
	if (prod == "mongodb" || prod == "neo4j" || prod == "onlyoffice" || prod == "zabbix") && !spec.AcceptRedLicense {
		rep.Block(sdk.CodeImgForbiddenRegistry, fmt.Sprintf("%s uses a red-list license (SSPLv1/GPLv3/AGPLv3); set acceptRedLicense=true for customer-side build (ADR-059)", prod))
	}

	switch prod {
	case "milvus":
		if req.Instance.ProductVersion != "" && sdk.DottedVersionLess(req.Instance.ProductVersion, "2.6.26") {
			rep.Block(sdk.CodePluginIncompatible, fmt.Sprintf("Milvus %s is vulnerable to CVE-2026-26190 (CVSS 9.8 auth bypass); upgrade to >= 2.6.26 or >= 3.0.5 (P10 §1)", req.Instance.ProductVersion))
		}
		if !spec.AuthorizationEnabled {
			rep.Block(sdk.CodeAuthForbidden, "Milvus common.security.authorizationEnabled MUST be true (P10 §1)")
		}
		if req.CheckClaims != nil && req.Instance.DeployTarget != sdk.TargetKubernetes {
			for _, h := range req.Hosts {
				if err := req.CheckClaims(h.ID, sdk.HostRoleClaims{
					Ports: []sdk.PortClaim{{Port: spec.Port, Proto: "tcp"}, {Port: spec.AttuPort, Proto: "tcp"}},
				}); err != nil {
					rep.BlockHost(h.ID, sdk.CodeHostClaimConflict, err.Error())
				}
			}
		}
	case "mongodb":
		if spec.ArbiterMode {
			rep.Block(sdk.CodePlanTopologyMinHosts, "MongoDB forbids 2-data + Arbiter topology; deploy >= 3 full PSS nodes (P10 §2)")
		}
		if req.Instance.DeployTarget != sdk.TargetKubernetes && len(req.Hosts) > 0 && len(req.Hosts) < 3 {
			rep.Block(sdk.CodePlanTopologyMinHosts, fmt.Sprintf("MongoDB Replica Set requires >= 3 PSS hosts, got %d", len(req.Hosts)))
		}
	case "neo4j":
		if spec.Neo4jOnlinePlugins {
			rep.Block(sdk.CodeSrcOfflineStrictDenied, "Neo4j forbids NEO4J_PLUGINS online GitHub fetch; mount offline apoc-core.jar to /plugins (P10 §3)")
		}
	case "onlyoffice":
		if spec.MountDockerSock {
			rep.Block(sdk.CodeAuthForbidden, "mounting /var/run/docker.sock into OnlyOffice is strictly forbidden (P10 §4)")
		}
		if spec.OnlyOfficeMode == "legacy-workspace" && !spec.AllowPrivileged {
			rep.Block(sdk.CodeAuthForbidden, "OnlyOffice Legacy Workspace requires isolated VM + four-eyes approval with allowPrivileged=true; default to non-privileged Mode A (docs)")
		}
	case "rancher":
		if strings.EqualFold(spec.RancherEngine, "rke1") {
			rep.Block(sdk.CodePluginIncompatible, "RKE1 is EOL and forbidden; use RKE2 or kinstall pure-binary K8s (P10 §4)")
		}
	case "generic-app":
		for _, svc := range spec.Services {
			if svc.Privileged && !spec.AllowPrivileged {
				rep.Block(sdk.CodeAuthForbidden, fmt.Sprintf("generic-app service %q sets privileged=true without explicit allowPrivileged approval (P10 §5)", svc.Name))
			}
			for _, pt := range svc.Ports {
				if strings.HasPrefix(pt, "0.0.0.0:") {
					rep.Block(sdk.CodeAuthForbidden, fmt.Sprintf("generic-app service %q exposes %s on 0.0.0.0; bind to host primaryIP instead (P10 §5)", svc.Name, pt))
				}
			}
			for _, m := range svc.Mounts {
				if m.Type != "file" && m.Type != "dir" {
					rep.Block(sdk.CodeAPIInvalidRequest, fmt.Sprintf("generic-app mount %s must explicitly declare type=file|dir (P10 §5)", m.Src))
				}
			}
		}
		if req.CheckClaims != nil && spec.DockerSubnet != "" {
			for _, h := range req.Hosts {
				if err := req.CheckClaims(h.ID, sdk.HostRoleClaims{Subnets: []string{spec.DockerSubnet}}); err != nil {
					rep.BlockHost(h.ID, sdk.CodeHostClaimConflict, err.Error())
				}
			}
		}
	}
	return rep, nil
}

func (p *Plugin) Plan(ctx context.Context, req *sdk.PlanRequest) (*sdk.PlanResult, error) {
	prod := strings.ToLower(req.Instance.Product)
	spec := parseSpec(req.Instance.Spec, prod)
	pl, err := p.buildInstall(ctx, &sdk.OpRequest{Instance: req.Instance, Hosts: req.Hosts})
	if err != nil {
		return nil, err
	}
	host := "127.0.0.1"
	if len(req.Hosts) > 0 && req.Hosts[0].Address != "" {
		host = req.Hosts[0].Address
	}
	return &sdk.PlanResult{
		Summary:  fmt.Sprintf("%s %s (%s)", prod, req.Instance.ProductVersion, req.Instance.DeployTarget),
		Pipeline: pl,
		Outputs: map[string]any{
			"endpoint": fmt.Sprintf("%s:%d", host, spec.Port),
			"attuUrl":  fmt.Sprintf("http://%s:%d", host, spec.AttuPort),
		},
	}, nil
}

func (p *Plugin) Operation(name string) (sdk.Operation, bool) { op, ok := p.ops[name]; return op, ok }

func (p *Plugin) Status(_ context.Context, req *sdk.StatusRequest) (*sdk.InstanceStatus, error) {
	return &sdk.InstanceStatus{Health: "healthy", Message: req.Instance.Product + " healthy", CheckedAt: time.Now().UTC()}, nil
}

func (p *Plugin) buildInstall(_ context.Context, req *sdk.OpRequest) (*sdk.Pipeline, error) {
	prod := strings.ToLower(req.Instance.Product)
	spec := parseSpec(req.Instance.Spec, prod)
	pb := sdk.NewPipelineBuilder()

	if req.Instance.DeployTarget == sdk.TargetKubernetes {
		pb.Phase(sdk.PhaseSpec{
			Name: "k8s-" + prod,
			Tasks: []sdk.TaskSpec{
				sdk.HelmRelease(sdk.HelmReleaseSpec{
					ReleaseName: prod + "-" + req.Instance.Name, Namespace: prod,
					ChartRef: fmt.Sprintf("charts/%s-%s.tgz", prod, req.Instance.ProductVersion), Atomic: true, Wait: true,
				}).WithKey(prod + "-helm"),
			},
		})
		return pb.Build(), nil
	}

	var mounts []sdk.MountSpec
	for _, svc := range spec.Services {
		mounts = append(mounts, svc.Mounts...)
	}
	if len(mounts) == 0 {
		mounts = []sdk.MountSpec{{Src: fmt.Sprintf("/data/%s/%s", prod, req.Instance.Name), Dst: "/data", Type: "dir", Mode: 0o750}}
	}
	pb.Phase(sdk.PhaseSpec{
		Name: "deploy-" + prod,
		Tasks: []sdk.TaskSpec{
			sdk.DockerEnsure(spec.FirewallAllowCIDRs).WithKey(prod + "-docker"),
			sdk.ComposeSync(prod+"-"+req.Instance.Name, fmt.Sprintf("/opt/kinstall/compose/%s-%s/docker-compose.yml", prod, req.Instance.Name), mounts...).
				WithParam("allowPrivileged", spec.AllowPrivileged).
				WithKey(prod + "-compose"),
		},
	})
	return pb.Build(), nil
}

func (p *Plugin) buildUninstall(_ context.Context, req *sdk.OpRequest) (*sdk.Pipeline, error) {
	pb := sdk.NewPipelineBuilder()
	pb.Phase(sdk.PhaseSpec{Name: "uninstall-" + req.Instance.Product, Disruptive: true, Tasks: []sdk.TaskSpec{sdk.CmdRun("systemctl disable --now kinstall-compose-"+req.Instance.Product+"-"+req.Instance.Name+" || true", "").WithKey("ds-stop")}})
	return pb.Build(), nil
}

type dsOp struct {
	name     string
	mutating bool
	build    func(context.Context, *sdk.OpRequest) (*sdk.Pipeline, error)
}

func (o *dsOp) Name() string   { return o.name }
func (o *dsOp) Mutating() bool { return o.mutating }
func (o *dsOp) Preflight(_ context.Context, _ *sdk.OpRequest) (*sdk.ValidationReport, error) {
	return sdk.NewValidationReport(), nil
}
func (o *dsOp) Build(ctx context.Context, req *sdk.OpRequest) (*sdk.Pipeline, error) {
	return o.build(ctx, req)
}

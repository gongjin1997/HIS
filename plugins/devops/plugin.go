package devops

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// Spec configures DevOps delivery stack components (P8: Harbor, GitLab/Forgejo, Jenkins, SonarQube, Reposilite, Verdaccio, Nginx).
type Spec struct {
	Port           int    `json:"port"`
	ExternalValkey bool   `json:"externalValkey"` // required for Harbor on arm64-64K (harbor#24445)
	ExternalPG     bool   `json:"externalPG"`
	JDKVersion     int    `json:"jdkVersion"` // Jenkins 2.580+ requires JDK 21+
	UseJCasC       bool   `json:"useJCasC"`
	EnableTLCP     bool   `json:"enableTlcp"` // Nginx Tongsuo GM/T 0024 dual-cert mode
	UpstreamURL    string `json:"upstreamUrl,omitempty"`
}

func parseSpec(raw json.RawMessage, prod string) Spec {
	defPort := 8080
	switch prod {
	case "harbor":
		defPort = 443
	case "gitlab", "forgejo":
		defPort = 80
	case "sonarqube":
		defPort = 9000
	case "reposilite":
		defPort = 8081
	case "verdaccio":
		defPort = 4873
	case "nginx":
		defPort = 443
	}
	s := Spec{Port: defPort, JDKVersion: 21, UseJCasC: true}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &s)
	}
	if s.Port <= 0 {
		s.Port = defPort
	}
	if s.JDKVersion <= 0 {
		s.JDKVersion = 21
	}
	return s
}

// Plugin implements sdk.Component for the P8 Enterprise DevOps Delivery Stack.
type Plugin struct{ ops map[string]sdk.Operation }

func New() *Plugin {
	p := &Plugin{}
	p.ops = map[string]sdk.Operation{
		"install":   &devOp{name: "install", mutating: true, build: p.buildInstall},
		"reload":    &devOp{name: "reload", mutating: true, build: p.buildReload},
		"uninstall": &devOp{name: "uninstall", mutating: true, build: p.buildUninstall},
	}
	return p
}

func (p *Plugin) Meta() sdk.PluginMeta {
	return sdk.PluginMeta{
		Name:        "devops-stack",
		Version:     "1.0.0",
		SDKVersion:  sdk.SDKAPIVersion,
		Type:        sdk.PluginComponent,
		Description: "P8 DevOps Stack: Harbor 2.16 (64K external Valkey guard), GitLab 19 / Forgejo, Jenkins 2.580 JCasC+JDK21, SonarQube 26.x, Reposilite, Verdaccio, Nginx 1.28 / Tongsuo",
		Products: []sdk.ProductRange{
			{Product: "harbor", Versions: ">=2.14.0, <3.0.0", DefaultVer: "2.16.0", Targets: []sdk.DeployTarget{sdk.TargetHostContainer, sdk.TargetKubernetes}, LicenseTier: "green"},
			{Product: "gitlab", Versions: ">=18.0.0, <21.0.0", DefaultVer: "19.5.6", Targets: []sdk.DeployTarget{sdk.TargetHostContainer, sdk.TargetKubernetes}, LicenseTier: "yellow"},
			{Product: "forgejo", Versions: ">=9.0.0, <20.0.0", DefaultVer: "11.0.0", Targets: []sdk.DeployTarget{sdk.TargetHostBinary, sdk.TargetHostContainer, sdk.TargetKubernetes}, LicenseTier: "green"},
			{Product: "jenkins", Versions: ">=2.500.0, <3.0.0", DefaultVer: "2.580.3", Targets: []sdk.DeployTarget{sdk.TargetHostBinary, sdk.TargetHostContainer, sdk.TargetKubernetes}, LicenseTier: "green"},
			{Product: "sonarqube", Versions: ">=25.0.0, <28.0.0", DefaultVer: "26.4.0", Targets: []sdk.DeployTarget{sdk.TargetHostContainer, sdk.TargetKubernetes}, LicenseTier: "yellow"},
			{Product: "reposilite", Versions: ">=3.5.0, <5.0.0", DefaultVer: "3.5.20", Targets: []sdk.DeployTarget{sdk.TargetHostBinary, sdk.TargetHostContainer}, LicenseTier: "green"},
			{Product: "verdaccio", Versions: ">=6.0.0, <8.0.0", DefaultVer: "6.1.0", Targets: []sdk.DeployTarget{sdk.TargetHostBinary, sdk.TargetHostContainer}, LicenseTier: "green"},
			{Product: "nginx", Versions: ">=1.26.0, <2.0.0", DefaultVer: "1.28.0", Targets: []sdk.DeployTarget{sdk.TargetHostBinary, sdk.TargetHostContainer}, LicenseTier: "green"},
		},
		Platforms:  sdk.Platforms{Arch: []string{"amd64", "arm64"}, PageSizes: []int{4096, 65536}},
		Operations: []string{"install", "reload", "uninstall"},
		ParamSchema: map[string]any{
			"title": "DevOps 交付栈组件参数 (Harbor / GitLab / Jenkins / Sonar / Nginx)",
			"properties": map[string]any{
				"port":           map[string]any{"type": "integer", "default": 8080, "minimum": 80, "maximum": 65535, "x-kinstall-ui": map[string]any{"group": "network", "order": 1, "label": "服务端口（Harbor 443 / Sonar 9000）"}},
				"externalValkey": map[string]any{"type": "boolean", "default": true, "x-kinstall-ui": map[string]any{"group": "storage", "order": 2, "label": "外置 64K Valkey（Harbor 鲲鹏必选）"}},
				"externalPG":     map[string]any{"type": "boolean", "default": true, "x-kinstall-ui": map[string]any{"group": "storage", "order": 3, "label": "复用外置高可用 PostgreSQL"}},
				"jdkVersion":     map[string]any{"type": "integer", "default": 21, "minimum": 17, "maximum": 25, "x-kinstall-ui": map[string]any{"group": "basic", "order": 4, "label": "隔离运行时 JDK 版本 (Jenkins ≥ 21)"}},
				"useJCasC":       map[string]any{"type": "boolean", "default": true, "x-kinstall-ui": map[string]any{"group": "basic", "order": 5, "label": "启用 Jenkins JCasC 声明式配置"}},
				"enableTlcp":     map[string]any{"type": "boolean", "default": false, "x-kinstall-ui": map[string]any{"group": "security", "order": 6, "label": "启用 Tongsuo TLCP 国密双证书"}},
				"upstreamUrl":    map[string]any{"type": "string", "default": "http://127.0.0.1:8080", "x-kinstall-ui": map[string]any{"group": "advanced", "order": 7, "label": "Nginx 反代上游地址"}},
			},
		},
		OutputSchema:  map[string]string{"url": "string", "product": "string"},
		SchemaVersion: 1,
	}
}

func (p *Plugin) Validate(_ context.Context, req *sdk.ValidateRequest) (*sdk.ValidationReport, error) {
	rep := sdk.NewValidationReport()
	prod := strings.ToLower(req.Instance.Product)
	spec := parseSpec(req.Instance.Spec, prod)

	for _, h := range req.Hosts {
		switch prod {
		case "harbor":
			// P8 §1: 64K page hosts MUST externalize P2 64K Valkey to avoid goharbor/harbor#24445 embedded Redis crash!
			if h.Facts.PageSize == 65536 && !spec.ExternalValkey {
				rep.BlockHost(h.ID, sdk.CodePlanArchPageMismatch, "Harbor on arm64-64K host MUST set externalValkey=true pointing to P2 64K Valkey (goharbor/harbor#24445)")
			}
		case "gitlab":
			if h.Facts.MemMB > 0 && h.Facts.MemMB < 8192 {
				rep.Warn(sdk.CodeAPIInvalidRequest, fmt.Sprintf("host %s has < 8 GiB RAM (%d MiB); switch from GitLab CE to lightweight Forgejo (P8 §1)", h.Name, h.Facts.MemMB))
			}
		case "jenkins":
			if spec.JDKVersion < 21 {
				rep.Block(sdk.CodePluginIncompatible, fmt.Sprintf("Jenkins 2.580+ requires runtime/jdk >= 21 (got JDK %d, P8 §1)", spec.JDKVersion))
			}
		}
		if req.CheckClaims != nil && req.Instance.DeployTarget != sdk.TargetKubernetes {
			if err := req.CheckClaims(h.ID, sdk.HostRoleClaims{
				Ports: []sdk.PortClaim{{Port: spec.Port, Proto: "tcp"}},
				Dirs:  []string{fmt.Sprintf("/data/%s/%s", prod, req.Instance.Name)},
			}); err != nil {
				rep.BlockHost(h.ID, sdk.CodeHostClaimConflict, err.Error())
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
		Summary:  fmt.Sprintf("DevOps %s %s (%s:%d)", prod, req.Instance.ProductVersion, host, spec.Port),
		Pipeline: pl,
		Outputs:  map[string]any{"url": fmt.Sprintf("http://%s:%d", host, spec.Port), "product": prod},
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

	switch prod {
	case "sonarqube":
		// R2 exact sonar.properties filename (fixing old-install.md sonar.properies typo!) + sysctl
		sonarProps := "sonar.web.port=9000\nsonar.search.javaOpts=-Xmx2G -Xms2G\n"
		pb.Phase(sdk.PhaseSpec{
			Name: "sonarqube-tune-and-deploy",
			Tasks: []sdk.TaskSpec{
				sdk.SysctlApply("94-kinstall-sonarqube.conf", map[string]string{"vm.max_map_count": "524288", "fs.file-max": "131072"}).WithKey("sq-sysctl"),
				sdk.FileSync(fmt.Sprintf("/etc/kinstall/sonarqube/%s/sonar.properties", req.Instance.Name), sdk.TemplateRef{Inline: sonarProps}, 0o640, "root:root").WithKey("sq-props"),
			},
		})
	case "nginx":
		// P8 §1: proxy_buffering off for SSE + mandatory `nginx -t` precheck before reload!
		ngxConf := fmt.Sprintf("server {\n  listen %d;\n  location / {\n    proxy_pass %s;\n    proxy_buffering off;\n    proxy_set_header X-Accel-Buffering no;\n  }\n}\n", spec.Port, firstNonEmpty(spec.UpstreamURL, "http://127.0.0.1:8080"))
		pb.Phase(sdk.PhaseSpec{
			Name: "deploy-nginx",
			Tasks: []sdk.TaskSpec{
				sdk.FileSync(fmt.Sprintf("/etc/nginx/conf.d/%s.conf", req.Instance.Name), sdk.TemplateRef{Inline: ngxConf}, 0o644, "root:root").WithKey("ngx-conf"),
				sdk.CmdRun("nginx -t && systemctl reload nginx", "").WithKey("ngx-test-and-reload"),
			},
		})
	case "jenkins":
		jcasc := "jenkins:\n  systemMessage: \"Managed by kinstall JCasC (JDK 21 + 9 detached offline plugins)\"\n"
		pb.Phase(sdk.PhaseSpec{
			Name: "deploy-jenkins-jcasc",
			Tasks: []sdk.TaskSpec{
				sdk.BinaryInstall("jenkins", req.Instance.ProductVersion, sdk.ArtifactRef{Name: "jenkins"}).WithKey("jks-bin"),
				sdk.FileSync(fmt.Sprintf("/etc/kinstall/jenkins/%s/casc.yaml", req.Instance.Name), sdk.TemplateRef{Inline: jcasc}, 0o640, "root:jenkins").WithKey("jks-jcasc"),
			},
		})
	default:
		pb.Phase(sdk.PhaseSpec{
			Name: "deploy-" + prod,
			Tasks: []sdk.TaskSpec{
				sdk.DockerEnsure(nil).WithKey(prod + "-docker"),
				sdk.ComposeSync(prod+"-"+req.Instance.Name, fmt.Sprintf("/opt/kinstall/compose/%s-%s/docker-compose.yml", prod, req.Instance.Name),
					sdk.MountSpec{Src: fmt.Sprintf("/data/%s/%s", prod, req.Instance.Name), Dst: "/data", Type: "dir", Mode: 0o750},
				).WithKey(prod + "-compose"),
			},
		})
	}
	return pb.Build(), nil
}

func (p *Plugin) buildReload(_ context.Context, _ *sdk.OpRequest) (*sdk.Pipeline, error) {
	pb := sdk.NewPipelineBuilder()
	pb.Phase(sdk.PhaseSpec{Name: "nginx-safe-reload", Tasks: []sdk.TaskSpec{sdk.CmdRun("nginx -t && systemctl reload nginx", "").WithKey("ngx-reload")}})
	return pb.Build(), nil
}

func (p *Plugin) buildUninstall(_ context.Context, req *sdk.OpRequest) (*sdk.Pipeline, error) {
	pb := sdk.NewPipelineBuilder()
	pb.Phase(sdk.PhaseSpec{Name: "uninstall-" + req.Instance.Product, Disruptive: true, Tasks: []sdk.TaskSpec{sdk.CmdRun("systemctl disable --now kinstall-"+req.Instance.Product+"-"+req.Instance.Name+" || true", "").WithKey("dev-stop")}})
	return pb.Build(), nil
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

type devOp struct {
	name     string
	mutating bool
	build    func(context.Context, *sdk.OpRequest) (*sdk.Pipeline, error)
}

func (o *devOp) Name() string   { return o.name }
func (o *devOp) Mutating() bool { return o.mutating }
func (o *devOp) Preflight(_ context.Context, _ *sdk.OpRequest) (*sdk.ValidationReport, error) {
	return sdk.NewValidationReport(), nil
}
func (o *devOp) Build(ctx context.Context, req *sdk.OpRequest) (*sdk.Pipeline, error) {
	return o.build(ctx, req)
}

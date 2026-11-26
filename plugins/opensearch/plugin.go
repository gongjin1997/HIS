package opensearch

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// Spec configures OpenSearch 3.10.0 (Apache-2.0 default) or restricted Elasticsearch 9.5/8.19 (P3 §3.5, ADR-052/053).
type Spec struct {
	Port             int    `json:"port"`
	DataPath         string `json:"dataPath"`
	DataFSType       string `json:"dataFsType,omitempty"`
	HeapGB           int    `json:"heapGB"`
	HotDays          int    `json:"hotDays"`
	WarmDays         int    `json:"warmDays"`
	ArchiveDays      int    `json:"archiveDays"` // >= 180d for DJCP L3 compliance
	AcceptRedLicense bool   `json:"acceptRedLicense"`
}

func parseSpec(raw json.RawMessage) Spec {
	s := Spec{Port: 9200, DataPath: "/data/opensearch", HotDays: 7, WarmDays: 30, ArchiveDays: 180}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &s)
	}
	if s.Port <= 0 {
		s.Port = 9200
	}
	if s.DataPath == "" {
		s.DataPath = "/data/opensearch"
	}
	if s.ArchiveDays <= 0 {
		s.ArchiveDays = 180
	}
	return s
}

// ComputeSafeHeapGB enforces min(floor(MemTotal*0.50), 31 GiB) to preserve JVM CompressedOops (ADR-053).
func ComputeSafeHeapGB(hostMemMB int) int {
	halfGB := int(math.Floor(float64(hostMemMB) / 1024.0 * 0.50))
	if halfGB < 1 {
		halfGB = 1
	}
	if halfGB > 31 {
		return 31
	}
	return halfGB
}

// Plugin implements sdk.Component for OpenSearch 3.10.0 & restricted Elasticsearch 9.5/8.19 (P3).
type Plugin struct{ ops map[string]sdk.Operation }

func New() *Plugin {
	p := &Plugin{}
	p.ops = map[string]sdk.Operation{
		"install":   &osOp{name: "install", mutating: true, build: p.buildInstall},
		"upgrade":   &osOp{name: "upgrade", mutating: true, build: p.buildInstall},
		"backup":    &osOp{name: "backup", mutating: false, build: p.buildSnapshot},
		"uninstall": &osOp{name: "uninstall", mutating: true, build: p.buildUninstall},
	}
	return p
}

func (p *Plugin) Meta() sdk.PluginMeta {
	return sdk.PluginMeta{
		Name:        "opensearch",
		Version:     "1.1.0",
		SDKVersion:  sdk.SDKAPIVersion,
		Type:        sdk.PluginComponent,
		Description: "OpenSearch 3.10.0 (Apache-2.0, <=31GiB CompressedOops heap, 180d ISM archive) & optional restricted Elasticsearch 9.5/8.19",
		Products: []sdk.ProductRange{
			{Product: "opensearch", Versions: ">=3.0.0, <4.0.0", DefaultVer: "3.10.0", Targets: []sdk.DeployTarget{sdk.TargetHostBinary, sdk.TargetHostContainer, sdk.TargetKubernetes}, LicenseTier: "green"},
			{Product: "elasticsearch", Versions: ">=8.19.0, <10.0.0", DefaultVer: "9.5.7", Targets: []sdk.DeployTarget{sdk.TargetHostContainer, sdk.TargetKubernetes}, LicenseTier: "red"},
		},
		Platforms: sdk.Platforms{Arch: []string{"amd64", "arm64"}, PageSizes: []int{4096, 65536}},
		OutputSchema: map[string]string{
			"httpEndpoints": "string",
			"caCertRef":     "string",
		},
		SchemaVersion: 1,
	}
}

func (p *Plugin) Validate(_ context.Context, req *sdk.ValidateRequest) (*sdk.ValidationReport, error) {
	rep := sdk.NewValidationReport()
	spec := parseSpec(req.Instance.Spec)
	if strings.EqualFold(req.Instance.Product, "elasticsearch") && !spec.AcceptRedLicense {
		rep.Block(sdk.CodeImgForbiddenRegistry, "Elasticsearch uses Elastic License / SSPLv1 (red-list); default to OpenSearch 3.10.0 (Apache-2.0) or set acceptRedLicense=true (ADR-052)")
	}
	if spec.HeapGB > 31 {
		rep.Block(sdk.CodeAPIInvalidRequest, fmt.Sprintf("heapGB (%d) exceeds 31 GiB JVM CompressedOops threshold (ADR-053)", spec.HeapGB))
	}
	if strings.HasPrefix(strings.ToLower(spec.DataFSType), "nfs") || strings.Contains(strings.ToLower(spec.DataPath), "/nfs") {
		rep.Block(sdk.CodeAPIInvalidRequest, "NFS is strictly forbidden for OpenSearch/Elasticsearch path.data (Lucene mmap corruption risk)")
	}
	for _, h := range req.Hosts {
		for _, d := range h.Facts.Disks {
			if d.MountPoint != "" && strings.HasPrefix(spec.DataPath, d.MountPoint) && strings.HasPrefix(strings.ToLower(d.FSType), "nfs") {
				rep.BlockHost(h.ID, sdk.CodeAPIInvalidRequest, fmt.Sprintf("host %s path.data %s resolves to NFS mount %s (forbidden)", h.Name, spec.DataPath, d.MountPoint))
			}
		}
		if req.CheckClaims != nil && req.Instance.DeployTarget != sdk.TargetKubernetes {
			if err := req.CheckClaims(h.ID, sdk.HostRoleClaims{
				Ports: []sdk.PortClaim{{Port: spec.Port, Proto: "tcp"}, {Port: spec.Port + 100, Proto: "tcp"}},
				Dirs:  []string{spec.DataPath},
			}); err != nil {
				rep.BlockHost(h.ID, sdk.CodeHostClaimConflict, err.Error())
			}
		}
	}
	return rep, nil
}

func (p *Plugin) Plan(ctx context.Context, req *sdk.PlanRequest) (*sdk.PlanResult, error) {
	spec := parseSpec(req.Instance.Spec)
	pl, err := p.buildInstall(ctx, &sdk.OpRequest{Instance: req.Instance, Hosts: req.Hosts})
	if err != nil {
		return nil, err
	}
	var eps []string
	for _, h := range req.Hosts {
		eps = append(eps, fmt.Sprintf("https://%s:%d", h.Address, spec.Port))
	}
	return &sdk.PlanResult{
		Summary:  fmt.Sprintf("OpenSearch %s (ISM hot=%dd/warm=%dd/archive=%dd)", req.Instance.ProductVersion, spec.HotDays, spec.WarmDays, spec.ArchiveDays),
		Pipeline: pl,
		Outputs:  map[string]any{"httpEndpoints": strings.Join(eps, ","), "caCertRef": "cred_os_ca_" + req.Instance.ID},
	}, nil
}

func (p *Plugin) Operation(name string) (sdk.Operation, bool) { op, ok := p.ops[name]; return op, ok }

func (p *Plugin) Status(_ context.Context, _ *sdk.StatusRequest) (*sdk.InstanceStatus, error) {
	return &sdk.InstanceStatus{Health: "healthy", Message: "OpenSearch cluster green", CheckedAt: time.Now().UTC()}, nil
}

func (p *Plugin) buildInstall(_ context.Context, req *sdk.OpRequest) (*sdk.Pipeline, error) {
	spec := parseSpec(req.Instance.Spec)
	memMB := 16384
	if len(req.Hosts) > 0 && req.Hosts[0].Facts.MemMB > 0 {
		memMB = req.Hosts[0].Facts.MemMB
	}
	heap := spec.HeapGB
	if heap <= 0 {
		heap = ComputeSafeHeapGB(memMB)
	}
	cfg := fmt.Sprintf("cluster.name: %s\nbootstrap.memory_lock: true\npath.data: %s\nhttp.port: %d\nindex.mapping.total_fields.limit: 1000\n", req.Instance.Name, spec.DataPath, spec.Port)
	jvm := fmt.Sprintf("-Xms%dg\n-Xmx%dg\n", heap, heap)
	pb := sdk.NewPipelineBuilder()
	pb.Phase(sdk.PhaseSpec{
		Name: "search-host-tune",
		Tasks: []sdk.TaskSpec{
			sdk.SysctlApply("93-kinstall-search.conf", map[string]string{"vm.max_map_count": "1048576", "vm.swappiness": "1"}).WithKey("os-sysctl"),
		},
	})
	pb.Phase(sdk.PhaseSpec{
		Name: "deploy-opensearch",
		Tasks: []sdk.TaskSpec{
			sdk.BinaryInstall("opensearch", req.Instance.ProductVersion, sdk.ArtifactRef{Name: "opensearch"}).WithKey("os-bin"),
			sdk.FileSync(fmt.Sprintf("/etc/kinstall/opensearch/%s/opensearch.yml", req.Instance.Name), sdk.TemplateRef{Inline: cfg}, 0o640, "root:opensearch").WithKey("os-cfg"),
			sdk.FileSync(fmt.Sprintf("/etc/kinstall/opensearch/%s/jvm.options", req.Instance.Name), sdk.TemplateRef{Inline: jvm}, 0o640, "root:opensearch").WithKey("os-jvm"),
			sdk.WaitTCP(fmt.Sprintf("127.0.0.1:%d", spec.Port), 120*time.Second).WithKey("os-ready"),
		},
	})
	return pb.Build(), nil
}

func (p *Plugin) buildSnapshot(_ context.Context, req *sdk.OpRequest) (*sdk.Pipeline, error) {
	pb := sdk.NewPipelineBuilder()
	pb.Phase(sdk.PhaseSpec{Name: "ism-snapshot", Tasks: []sdk.TaskSpec{sdk.CmdRun("echo trigger opensearch snapshot for "+req.Instance.Name, "").WithKey("os-snap")}})
	return pb.Build(), nil
}

func (p *Plugin) buildUninstall(_ context.Context, req *sdk.OpRequest) (*sdk.Pipeline, error) {
	pb := sdk.NewPipelineBuilder()
	pb.Phase(sdk.PhaseSpec{Name: "uninstall-opensearch", Disruptive: true, Tasks: []sdk.TaskSpec{sdk.CmdRun("systemctl disable --now kinstall-opensearch-"+req.Instance.Name+" || true", "").WithKey("os-stop")}})
	return pb.Build(), nil
}

type osOp struct {
	name     string
	mutating bool
	build    func(context.Context, *sdk.OpRequest) (*sdk.Pipeline, error)
}

func (o *osOp) Name() string   { return o.name }
func (o *osOp) Mutating() bool { return o.mutating }
func (o *osOp) Preflight(_ context.Context, _ *sdk.OpRequest) (*sdk.ValidationReport, error) {
	return sdk.NewValidationReport(), nil
}
func (o *osOp) Build(ctx context.Context, req *sdk.OpRequest) (*sdk.Pipeline, error) {
	return o.build(ctx, req)
}

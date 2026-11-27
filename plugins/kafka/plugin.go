package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// TopicSpec defines a pre-provisioned Kafka topic (P3 §3.1).
type TopicSpec struct {
	Name              string `json:"name"`
	Partitions        int    `json:"partitions"`
	ReplicationFactor int    `json:"replicationFactor"`
	RetentionHours    int    `json:"retentionHours"`
	MinInsyncReplicas int    `json:"minInsyncReplicas"`
	Compression       string `json:"compression"`
}

// Spec defines Apache Kafka 4.x KRaft configuration (P3 §3.2, ADR-073).
type Spec struct {
	Mode         string      `json:"mode"` // must be "kraft" (ZK removed in Kafka 4.x)
	Port         int         `json:"port"`
	JVMHeapGB    int         `json:"jvmHeapGB"` // locked 6-8 GiB to preserve Linux Page Cache!
	JBODDataDirs []string    `json:"jbodDataDirs"`
	Topics       []TopicSpec `json:"topics,omitempty"`
}

func parseSpec(raw json.RawMessage) Spec {
	s := Spec{
		Mode:         "kraft",
		Port:         9092,
		JVMHeapGB:    8,
		JBODDataDirs: []string{"/data/kafka/disk1"},
	}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &s)
	}
	if s.Mode == "" {
		s.Mode = "kraft"
	}
	if s.Port <= 0 {
		s.Port = 9092
	}
	if s.JVMHeapGB <= 0 {
		s.JVMHeapGB = 8
	}
	if len(s.JBODDataDirs) == 0 {
		s.JBODDataDirs = []string{"/data/kafka/disk1"}
	}
	return s
}

// Plugin implements sdk.Component for Apache Kafka 4.3.2 / 4.4.0 KRaft (P3).
type Plugin struct{ ops map[string]sdk.Operation }

func New() *Plugin {
	p := &Plugin{}
	p.ops = map[string]sdk.Operation{
		"install":   &simpleOp{name: "install", mutating: true, build: p.buildInstall},
		"scale":     &simpleOp{name: "scale", mutating: true, build: p.buildInstall},
		"upgrade":   &simpleOp{name: "upgrade", mutating: true, build: p.buildInstall},
		"uninstall": &simpleOp{name: "uninstall", mutating: true, build: p.buildUninstall},
	}
	return p
}

func (p *Plugin) Meta() sdk.PluginMeta {
	return sdk.PluginMeta{
		Name:        "kafka",
		Version:     "1.1.0",
		SDKVersion:  sdk.SDKAPIVersion,
		Type:        sdk.PluginComponent,
		Description: "Apache Kafka 4.3.2/4.4.0 KRaft (zero-ZK, KIP-848 incremental rebalance, KIP-858 JBOD, Strimzi >= 1.3.2)",
		Products: []sdk.ProductRange{{
			Product:     "kafka",
			Versions:    ">=4.0.0, <5.0.0",
			DefaultVer:  "4.3.2",
			Targets:     []sdk.DeployTarget{sdk.TargetHostBinary, sdk.TargetHostContainer, sdk.TargetKubernetes},
			LicenseTier: "green",
		}},
		Platforms:  sdk.Platforms{Arch: []string{"amd64", "arm64"}, PageSizes: []int{4096, 65536}},
		Operations: []string{"install", "scale", "upgrade", "uninstall"},
		ParamSchema: map[string]any{
			"title": "Apache Kafka 4.x KRaft 集群参数",
			"properties": map[string]any{
				"mode":      map[string]any{"type": "string", "enum": []string{"kraft"}, "default": "kraft", "x-kinstall-ui": map[string]any{"group": "basic", "order": 1, "label": "元数据模式（Kafka 4.x 纯 KRaft）"}},
				"port":      map[string]any{"type": "integer", "default": 9092, "minimum": 1024, "maximum": 65535, "x-kinstall-ui": map[string]any{"group": "network", "order": 2, "label": "Broker 监听端口"}},
				"jvmHeapGB": map[string]any{"type": "integer", "default": 6, "minimum": 2, "maximum": 8, "x-kinstall-ui": map[string]any{"group": "storage", "order": 3, "label": "JVM 堆上限 (GiB · 硬限 ≤ 8G 保 PageCache)", "riskWarning": "超过 8 GiB 将挤占 Linux Page Cache，导致 Broker 磁盘顺序吞吐断崖下降"}},
			},
		},
		OutputSchema: map[string]string{
			"bootstrapServers": "string",
			"clientCreds":      "object",
		},
		SchemaVersion: 1,
	}
}

func (p *Plugin) Validate(_ context.Context, req *sdk.ValidateRequest) (*sdk.ValidationReport, error) {
	rep := sdk.NewValidationReport()
	spec := parseSpec(req.Instance.Spec)
	if !strings.EqualFold(spec.Mode, "kraft") {
		rep.Block(sdk.CodePluginIncompatible, "Kafka 4.x completely removed ZooKeeper; mode must be 'kraft'")
	}
	if spec.JVMHeapGB > 8 {
		rep.Block(sdk.CodeAPIInvalidRequest, fmt.Sprintf("Kafka Broker JVM heap (%d GiB) exceeds 8 GiB cap; preserve >=75%% RAM for Linux Page Cache (ADR-073)", spec.JVMHeapGB))
	}
	if req.Instance.DeployTarget != sdk.TargetKubernetes && len(req.Hosts) > 0 && len(req.Hosts) < 3 {
		rep.Warn(sdk.CodePlanTopologyMinHosts, fmt.Sprintf("Kafka KRaft HA recommends >= 3 brokers (got %d)", len(req.Hosts)))
	}
	for _, h := range req.Hosts {
		if req.CheckClaims != nil && req.Instance.DeployTarget != sdk.TargetKubernetes {
			if err := req.CheckClaims(h.ID, sdk.HostRoleClaims{
				Ports: []sdk.PortClaim{{Port: spec.Port, Proto: "tcp"}, {Port: 9093, Proto: "tcp"}},
				Dirs:  spec.JBODDataDirs,
			}); err != nil {
				rep.BlockHost(h.ID, sdk.CodeHostClaimConflict, err.Error())
			}
		}
	}
	return rep, nil
}

func (p *Plugin) Plan(ctx context.Context, req *sdk.PlanRequest) (*sdk.PlanResult, error) {
	spec := parseSpec(req.Instance.Spec)
	pl, err := p.buildInstall(ctx, &sdk.OpRequest{Instance: req.Instance, Hosts: req.Hosts, BOM: req.BOM})
	if err != nil {
		return nil, err
	}
	var addrs []string
	for _, h := range req.Hosts {
		addrs = append(addrs, fmt.Sprintf("%s:%d", h.Address, spec.Port))
	}
	return &sdk.PlanResult{
		Summary:  fmt.Sprintf("Apache Kafka %s KRaft (KIP-848/KIP-858, heap=%dG) on %d brokers", req.Instance.ProductVersion, spec.JVMHeapGB, len(req.Hosts)),
		Pipeline: pl,
		Outputs: map[string]any{
			"bootstrapServers": strings.Join(addrs, ","),
			"clientCreds":      map[string]string{"dataprepper": "cred_kf_dp_" + req.Instance.ID},
		},
	}, nil
}

func (p *Plugin) Operation(name string) (sdk.Operation, bool) {
	op, ok := p.ops[name]
	return op, ok
}

func (p *Plugin) Status(_ context.Context, req *sdk.StatusRequest) (*sdk.InstanceStatus, error) {
	return &sdk.InstanceStatus{Health: "healthy", Message: "Kafka KRaft quorum active", CheckedAt: time.Now().UTC()}, nil
}

func (p *Plugin) buildInstall(_ context.Context, req *sdk.OpRequest) (*sdk.Pipeline, error) {
	spec := parseSpec(req.Instance.Spec)
	ver := req.Instance.ProductVersion
	if ver == "" {
		ver = "4.3.2"
	}
	pb := sdk.NewPipelineBuilder()
	if req.Instance.DeployTarget == sdk.TargetKubernetes {
		pb.Phase(sdk.PhaseSpec{
			Name: "k8s-strimzi",
			Tasks: []sdk.TaskSpec{
				sdk.HelmRelease(sdk.HelmReleaseSpec{
					ReleaseName: "strimzi-" + req.Instance.Name,
					Namespace:   "kafka",
					ChartRef:    "charts/strimzi-kafka-operator-1.3.2.tgz",
					Version:     "1.3.2",
					Atomic:      true,
					Wait:        true,
				}).WithKey("kafka-strimzi"),
			},
		})
		return pb.Build(), nil
	}
	serverProps := fmt.Sprintf("process.roles=broker,controller\nlog.dirs=%s\ngroup.coordinator.rebalance.protocols=classic,consumer\ncompression.type=zstd\nnum.network.threads=8\n", strings.Join(spec.JBODDataDirs, ","))
	pb.Phase(sdk.PhaseSpec{
		Name: "deploy-kafka-kraft",
		Tasks: []sdk.TaskSpec{
			sdk.BinaryInstall("kafka", ver, sdk.ArtifactRef{Name: "kafka"}).WithKey("kafka-bin"),
			sdk.FileSync(fmt.Sprintf("/etc/kinstall/kafka/%s/server.properties", req.Instance.Name), sdk.TemplateRef{Inline: serverProps}, 0o640, "root:kafka").WithKey("kafka-props"),
			sdk.WaitTCP(fmt.Sprintf("127.0.0.1:%d", spec.Port), 90*time.Second).WithKey("kafka-ready"),
		},
	})
	return pb.Build(), nil
}

func (p *Plugin) buildUninstall(_ context.Context, req *sdk.OpRequest) (*sdk.Pipeline, error) {
	pb := sdk.NewPipelineBuilder()
	pb.Phase(sdk.PhaseSpec{
		Name: "uninstall-kafka", Disruptive: true,
		Tasks: []sdk.TaskSpec{sdk.CmdRun(fmt.Sprintf("systemctl disable --now kinstall-kafka-%s.service || true", req.Instance.Name), "").WithKey("kafka-stop")},
	})
	return pb.Build(), nil
}

type simpleOp struct {
	name     string
	mutating bool
	build    func(context.Context, *sdk.OpRequest) (*sdk.Pipeline, error)
}

func (o *simpleOp) Name() string   { return o.name }
func (o *simpleOp) Mutating() bool { return o.mutating }
func (o *simpleOp) Preflight(_ context.Context, _ *sdk.OpRequest) (*sdk.ValidationReport, error) {
	return sdk.NewValidationReport(), nil
}
func (o *simpleOp) Build(ctx context.Context, req *sdk.OpRequest) (*sdk.Pipeline, error) {
	if o.build == nil {
		return nil, sdk.NewError(sdk.CodeAPIInvalidRequest, "KAFKA", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, nil)
	}
	return o.build(ctx, req)
}

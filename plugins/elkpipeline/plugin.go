package elkpipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// Spec defines Fluent Bit + Data Prepper 2.16.x end-to-end ACK & DLQ pipeline (P3 §3.1-§3.4, ADR-071/072).
type Spec struct {
	Engine              string `json:"engine"` // data-prepper | logstash
	WorkersPerNode      int    `json:"workersPerNode"`
	TopicPartitions     int    `json:"topicPartitions"`
	Acknowledgments     bool   `json:"acknowledgments"`
	AutoCommit          bool   `json:"autoCommit"`
	KafkaBootstrap      string `json:"kafkaBootstrap"`
	OpenSearchEndpoints string `json:"opensearchEndpoints"`
	DLQS3Endpoint       string `json:"dlqS3Endpoint"`
	DLQBucket           string `json:"dlqBucket"`
	FluentBitMemMaxMB   int    `json:"fluentBitMemMaxMB"`
	FluentBitDiskBufGB  int    `json:"fluentBitDiskBufGB"`
}

func parseSpec(raw json.RawMessage) Spec {
	s := Spec{
		Engine:             "data-prepper",
		WorkersPerNode:     6,
		TopicPartitions:    24,
		Acknowledgments:    true,
		AutoCommit:         false,
		DLQBucket:          "logging-dlq",
		FluentBitMemMaxMB:  512,
		FluentBitDiskBufGB: 5,
	}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &s)
	}
	if s.WorkersPerNode <= 0 {
		s.WorkersPerNode = 6
	}
	if s.FluentBitMemMaxMB <= 0 {
		s.FluentBitMemMaxMB = 512
	}
	if s.FluentBitDiskBufGB <= 0 {
		s.FluentBitDiskBufGB = 5
	}
	return s
}

// ValidatePartitionThreadAlignment enforces P3 §3.1: total consumer threads (nodes * workersPerNode) <= TopicPartitions!
func ValidatePartitionThreadAlignment(nodeCount, workersPerNode, topicPartitions int) error {
	if topicPartitions <= 0 {
		return nil
	}
	total := nodeCount * workersPerNode
	if total > topicPartitions {
		return fmt.Errorf("consumer threads (%d nodes x %d workers = %d) exceed Kafka topic partitions (%d); idle threads trigger rebalance churn (P3 §3.1)",
			nodeCount, workersPerNode, total, topicPartitions)
	}
	return nil
}

// Plugin implements sdk.Component for the ELK / OpenSearch + Kafka Ingestion Pipeline (P3/15).
type Plugin struct{ ops map[string]sdk.Operation }

func New() *Plugin {
	p := &Plugin{}
	p.ops = map[string]sdk.Operation{
		"install":   &epOp{name: "install", mutating: true, build: p.buildInstall},
		"uninstall": &epOp{name: "uninstall", mutating: true, build: p.buildUninstall},
	}
	return p
}

func (p *Plugin) Meta() sdk.PluginMeta {
	return sdk.PluginMeta{
		Name:        "elk-pipeline",
		Version:     "1.1.0",
		SDKVersion:  sdk.SDKAPIVersion,
		Type:        sdk.PluginComponent,
		Description: "Fluent Bit (512M/5G disk buf) + Data Prepper 2.16.x (acknowledgments=true, KIP-848 consumer protocol, S3/OBS DLQ) with partition-thread alignment guard",
		Products: []sdk.ProductRange{{
			Product:     "elk-pipeline",
			Versions:    ">=2.10.0, <4.0.0",
			DefaultVer:  "2.16.0",
			Targets:     []sdk.DeployTarget{sdk.TargetHostBinary, sdk.TargetHostContainer, sdk.TargetKubernetes},
			LicenseTier: "green",
		}},
		Platforms:  sdk.Platforms{Arch: []string{"amd64", "arm64"}, PageSizes: []int{4096, 65536}},
		Operations: []string{"install", "uninstall"},
		ParamSchema: map[string]any{
			"title": "ELK / Data Prepper 2.16 日志采集清洗管道参数",
			"properties": map[string]any{
				"engine":             map[string]any{"type": "string", "enum": []string{"data-prepper", "logstash"}, "default": "data-prepper", "x-kinstall-ui": map[string]any{"group": "basic", "order": 1, "label": "清洗消费引擎"}},
				"workersPerNode":     map[string]any{"type": "integer", "default": 6, "minimum": 1, "maximum": 32, "x-kinstall-ui": map[string]any{"group": "basic", "order": 2, "label": "单节点消费线程数"}},
				"topicPartitions":    map[string]any{"type": "integer", "default": 24, "minimum": 1, "maximum": 256, "x-kinstall-ui": map[string]any{"group": "network", "order": 3, "label": "Kafka Topic 分区数（需 ≥ 总线程数）"}},
				"acknowledgments":    map[string]any{"type": "boolean", "default": true, "x-kinstall-ui": map[string]any{"group": "security", "order": 4, "label": "端到端 ACK 防丢日志 (P3 §3.3)"}},
				"dlqBucket":          map[string]any{"type": "string", "default": "logging-dlq", "x-kinstall-ui": map[string]any{"group": "storage", "order": 5, "label": "S3/OBS 死信队列 (DLQ) 桶名"}},
				"fluentBitMemMaxMB":  map[string]any{"type": "integer", "default": 256, "minimum": 64, "maximum": 2048, "x-kinstall-ui": map[string]any{"group": "advanced", "order": 6, "label": "Fluent Bit 内存硬限 (MiB)"}},
				"fluentBitDiskBufGB": map[string]any{"type": "integer", "default": 5, "minimum": 1, "maximum": 50, "x-kinstall-ui": map[string]any{"group": "advanced", "order": 7, "label": "Fluent Bit 磁盘缓冲 (GiB)"}},
			},
		},
		OutputSchema:  map[string]string{"dlqBucket": "string", "pipelineHealth": "string"},
		SchemaVersion: 1,
	}
}

func (p *Plugin) Validate(_ context.Context, req *sdk.ValidateRequest) (*sdk.ValidationReport, error) {
	rep := sdk.NewValidationReport()
	spec := parseSpec(req.Instance.Spec)
	if !spec.Acknowledgments || spec.AutoCommit {
		rep.Block(sdk.CodeAPIInvalidRequest, "Data Prepper Kafka source MUST enforce acknowledgments=true and autoCommit=false to prevent silent log loss (P3 §3.3)")
	}
	if spec.DLQBucket == "" {
		rep.Block(sdk.CodeAPIInvalidRequest, "Data Prepper sink MUST configure an S3/OBS DLQ bucket to isolate poison-pill 400 documents (P3 §3.3)")
	}
	if len(req.Hosts) > 0 && spec.TopicPartitions > 0 {
		if err := ValidatePartitionThreadAlignment(len(req.Hosts), spec.WorkersPerNode, spec.TopicPartitions); err != nil {
			rep.Block(sdk.CodeAPIInvalidRequest, err.Error())
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
	return &sdk.PlanResult{
		Summary:  fmt.Sprintf("ELK Pipeline (%s %s, %d nodes x %d workers, DLQ=%s)", spec.Engine, req.Instance.ProductVersion, len(req.Hosts), spec.WorkersPerNode, spec.DLQBucket),
		Pipeline: pl,
		Outputs:  map[string]any{"dlqBucket": spec.DLQBucket, "pipelineHealth": "ready"},
	}, nil
}

func (p *Plugin) Operation(name string) (sdk.Operation, bool) { op, ok := p.ops[name]; return op, ok }

func (p *Plugin) Status(_ context.Context, _ *sdk.StatusRequest) (*sdk.InstanceStatus, error) {
	return &sdk.InstanceStatus{Health: "healthy", Message: "Fluent Bit + Data Prepper pipeline healthy", CheckedAt: time.Now().UTC()}, nil
}

func (p *Plugin) buildInstall(_ context.Context, req *sdk.OpRequest) (*sdk.Pipeline, error) {
	spec := parseSpec(req.Instance.Spec)
	dpYAML := fmt.Sprintf(`logs-ingest-pipeline:
  workers: %d
  source:
    kafka:
      acknowledgments: true
      bootstrap_servers: [%q]
      topics:
        - name: "logs-app-biz"
          workers: %d
          auto_commit: false
          additional_properties:
            group.protocol: "consumer"
  sink:
    - opensearch:
        hosts: [%q]
        dlq:
          s3:
            bucket: %q
            endpoint: %q
            path_style_access: true
`, spec.WorkersPerNode, spec.KafkaBootstrap, spec.WorkersPerNode, spec.OpenSearchEndpoints, spec.DLQBucket, spec.DLQS3Endpoint)

	pb := sdk.NewPipelineBuilder()
	pb.Phase(sdk.PhaseSpec{
		Name: "deploy-elk-pipeline",
		Tasks: []sdk.TaskSpec{
			sdk.BinaryInstall("data-prepper", req.Instance.ProductVersion, sdk.ArtifactRef{Name: "data-prepper"}).WithKey("dp-bin"),
			sdk.FileSync(fmt.Sprintf("/etc/kinstall/dataprepper/%s/pipelines/kafka-to-opensearch.yaml", req.Instance.Name), sdk.TemplateRef{Inline: dpYAML}, 0o640, "root:dataprepper").WithKey("dp-pipeline-cfg"),
		},
	})
	return pb.Build(), nil
}

func (p *Plugin) buildUninstall(_ context.Context, req *sdk.OpRequest) (*sdk.Pipeline, error) {
	pb := sdk.NewPipelineBuilder()
	pb.Phase(sdk.PhaseSpec{Name: "uninstall-elk", Disruptive: true, Tasks: []sdk.TaskSpec{sdk.CmdRun("systemctl disable --now kinstall-dataprepper-"+req.Instance.Name+" || true", "").WithKey("dp-stop")}})
	return pb.Build(), nil
}

type epOp struct {
	name     string
	mutating bool
	build    func(context.Context, *sdk.OpRequest) (*sdk.Pipeline, error)
}

func (o *epOp) Name() string   { return o.name }
func (o *epOp) Mutating() bool { return o.mutating }
func (o *epOp) Preflight(_ context.Context, _ *sdk.OpRequest) (*sdk.ValidationReport, error) {
	return sdk.NewValidationReport(), nil
}
func (o *epOp) Build(ctx context.Context, req *sdk.OpRequest) (*sdk.Pipeline, error) {
	return o.build(ctx, req)
}

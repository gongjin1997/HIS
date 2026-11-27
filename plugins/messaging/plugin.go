package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// Spec configures P9 business messaging & coordination components (RocketMQ 5.5.1, Seata 2.6.0, ZooKeeper 3.9.7).
type Spec struct {
	Port              int    `json:"port"`
	JVMHeapGB         int    `json:"jvmHeapGB"`
	BrokerIP1         string `json:"brokerIP1,omitempty"`
	StoreMode         string `json:"storeMode,omitempty"` // seata: raft (default) | db
	ConsolePassword   string `json:"consolePassword,omitempty"`
	DataDir           string `json:"dataDir,omitempty"`
	DataLogDir        string `json:"dataLogDir,omitempty"` // ZK separate WAL disk (P9 §3)
	EnableACL         bool   `json:"enableAcl"`
	FourLetterAllowed string `json:"fourLetterAllowed,omitempty"`
}

func parseSpec(raw json.RawMessage, prod string) Spec {
	s := Spec{
		Port:              9876,
		JVMHeapGB:         4,
		StoreMode:         "raft",
		DataDir:           "/data/" + prod + "/data",
		DataLogDir:        "/data/" + prod + "/wal",
		EnableACL:         true,
		FourLetterAllowed: "ruok,mntr,stat",
	}
	switch prod {
	case "seata":
		s.Port = 8091
	case "zookeeper":
		s.Port = 2181
	}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &s)
	}
	return s
}

// Plugin implements sdk.Component for P9 (RocketMQ 5.5.1, Seata 2.6.0, ZooKeeper 3.9.7 — all Apache-2.0).
type Plugin struct{ ops map[string]sdk.Operation }

func New() *Plugin {
	p := &Plugin{}
	p.ops = map[string]sdk.Operation{
		"install":   &msgOp{name: "install", mutating: true, build: p.buildInstall},
		"uninstall": &msgOp{name: "uninstall", mutating: true, build: p.buildUninstall},
	}
	return p
}

func (p *Plugin) Meta() sdk.PluginMeta {
	return sdk.PluginMeta{
		Name:        "messaging-tx",
		Version:     "1.0.0",
		SDKVersion:  sdk.SDKAPIVersion,
		Type:        sdk.PluginComponent,
		Description: "P9 Business Messaging & Distributed TX: RocketMQ 5.5.1 (DLedger + brokerIP1), Seata 2.6.0 (Raft + NamingServer), ZooKeeper 3.9.7 (legacy compat)",
		Products: []sdk.ProductRange{
			{Product: "rocketmq", Versions: ">=5.3.0, <6.0.0", DefaultVer: "5.5.1", Targets: []sdk.DeployTarget{sdk.TargetHostBinary, sdk.TargetHostContainer, sdk.TargetKubernetes}, LicenseTier: "green"},
			{Product: "seata", Versions: ">=2.5.0, <3.0.0", DefaultVer: "2.6.0", Targets: []sdk.DeployTarget{sdk.TargetHostBinary, sdk.TargetHostContainer, sdk.TargetKubernetes}, LicenseTier: "green"},
			{Product: "zookeeper", Versions: ">=3.9.0, <4.0.0", DefaultVer: "3.9.7", Targets: []sdk.DeployTarget{sdk.TargetHostBinary, sdk.TargetHostContainer}, LicenseTier: "green"},
		},
		Platforms:  sdk.Platforms{Arch: []string{"amd64", "arm64"}, PageSizes: []int{4096, 65536}},
		Operations: []string{"install", "uninstall"},
		ParamSchema: map[string]any{
			"title": "业务消息与分布式事务参数 (RocketMQ / Seata / ZooKeeper)",
			"properties": map[string]any{
				"port":       map[string]any{"type": "integer", "default": 9876, "minimum": 1024, "maximum": 65535, "x-kinstall-ui": map[string]any{"group": "network", "order": 1, "label": "监听端口"}},
				"jvmHeapGB":  map[string]any{"type": "integer", "default": 4, "minimum": 1, "maximum": 8, "x-kinstall-ui": map[string]any{"group": "storage", "order": 2, "label": "JVM 堆上限 (GiB · 硬限 ≤ 8G)"}},
				"brokerIP1":  map[string]any{"type": "string", "default": "", "x-kinstall-ui": map[string]any{"group": "network", "order": 3, "label": "RocketMQ 显式宿主机 IP（防注册 172.x）"}},
				"storeMode":  map[string]any{"type": "string", "enum": []string{"raft", "db"}, "default": "raft", "x-kinstall-ui": map[string]any{"group": "basic", "order": 4, "label": "Seata 事务存储模式"}},
				"dataDir":    map[string]any{"type": "string", "default": "/data/messaging/data", "x-kinstall-ui": map[string]any{"group": "storage", "order": 5, "label": "快照数据目录"}},
				"dataLogDir": map[string]any{"type": "string", "default": "/data/messaging/wal", "x-kinstall-ui": map[string]any{"group": "storage", "order": 6, "label": "独立 WAL 日志盘（ZK 必分离）"}},
				"enableAcl":  map[string]any{"type": "boolean", "default": true, "x-kinstall-ui": map[string]any{"group": "security", "order": 7, "label": "开启访问控制 ACL"}},
			},
		},
		OutputSchema:  map[string]string{"endpoints": "string", "authSecretRef": "string"},
		SchemaVersion: 1,
	}
}

func (p *Plugin) Validate(_ context.Context, req *sdk.ValidateRequest) (*sdk.ValidationReport, error) {
	rep := sdk.NewValidationReport()
	prod := strings.ToLower(req.Instance.Product)
	spec := parseSpec(req.Instance.Spec, prod)

	switch prod {
	case "rocketmq":
		if spec.JVMHeapGB > 8 {
			rep.Block(sdk.CodeAPIInvalidRequest, fmt.Sprintf("RocketMQ broker heap (%dG) exceeds 8 GiB cap; keep RAM for CommitLog Page Cache (P9 §1.2)", spec.JVMHeapGB))
		}
		if req.Instance.DeployTarget == sdk.TargetHostContainer && spec.BrokerIP1 == "" {
			for _, h := range req.Hosts {
				if h.Address == "" && h.Facts.PrimaryIP4 == "" {
					rep.BlockHost(h.ID, sdk.CodeAPIInvalidRequest, "RocketMQ host-container requires explicit host primaryIP/brokerIP1 so Docker 172.x bridge IP is never registered to NameServer (R2)")
				}
			}
		}
	case "seata":
		if strings.EqualFold(spec.ConsolePassword, "seata") {
			rep.Block(sdk.CodeAuthForbidden, "Seata forbids default weak password 'seata'; supply high-entropy secret (P9 §2)")
		}
		if strings.EqualFold(spec.StoreMode, "raft") && req.Instance.DeployTarget != sdk.TargetKubernetes && len(req.Hosts) > 0 && (len(req.Hosts) < 3 || len(req.Hosts)%2 == 0) {
			rep.Block(sdk.CodePlanTopologyMinHosts, fmt.Sprintf("Seata Raft mode requires odd >= 3 nodes for consensus, got %d", len(req.Hosts)))
		}
	case "zookeeper":
		if len(req.Hosts) > 0 && (len(req.Hosts) < 3 || len(req.Hosts)%2 == 0) {
			rep.Block(sdk.CodePlanTopologyMinHosts, fmt.Sprintf("ZooKeeper requires odd >= 3 nodes (2 nodes strictly forbidden, P9 §3), got %d", len(req.Hosts)))
		}
		if spec.DataDir == spec.DataLogDir {
			rep.Block(sdk.CodeAPIInvalidRequest, "ZooKeeper MUST separate dataLogDir (WAL) from dataDir (snapshot) to avoid election timeouts (P9 §3)")
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
	var eps []string
	for _, h := range req.Hosts {
		eps = append(eps, fmt.Sprintf("%s:%d", h.Address, spec.Port))
	}
	return &sdk.PlanResult{
		Summary:  fmt.Sprintf("%s %s across %d nodes", prod, req.Instance.ProductVersion, len(req.Hosts)),
		Pipeline: pl,
		Outputs:  map[string]any{"endpoints": strings.Join(eps, ","), "authSecretRef": "cred_" + prod + "_" + req.Instance.ID},
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
	hostIP := "127.0.0.1"
	if len(req.Hosts) > 0 && req.Hosts[0].Address != "" {
		hostIP = req.Hosts[0].Address
	}
	switch prod {
	case "rocketmq":
		bip := spec.BrokerIP1
		if bip == "" {
			bip = hostIP
		}
		cfg := fmt.Sprintf("brokerIP1=%s\nenableControllerMode=true\naclEnable=true\ndiskMaxUsedSpaceRatio=85\n", bip)
		pb.Phase(sdk.PhaseSpec{
			Name: "deploy-rocketmq",
			Tasks: []sdk.TaskSpec{
				sdk.BinaryInstall("rocketmq", req.Instance.ProductVersion, sdk.ArtifactRef{Name: "rocketmq"}).WithKey("rmq-bin"),
				sdk.FileSync(fmt.Sprintf("/etc/kinstall/rocketmq/%s/broker.conf", req.Instance.Name), sdk.TemplateRef{Inline: cfg}, 0o640, "root:rocketmq").WithKey("rmq-broker-conf"),
			},
		})
	case "seata":
		cfg := fmt.Sprintf("seata.store.mode=%s\nserver.port=%d\n", spec.StoreMode, spec.Port)
		pb.Phase(sdk.PhaseSpec{
			Name: "deploy-seata",
			Tasks: []sdk.TaskSpec{
				sdk.BinaryInstall("seata", req.Instance.ProductVersion, sdk.ArtifactRef{Name: "seata"}).WithKey("seata-bin"),
				sdk.FileSync(fmt.Sprintf("/etc/kinstall/seata/%s/application.properties", req.Instance.Name), sdk.TemplateRef{Inline: cfg}, 0o640, "root:seata").WithKey("seata-cfg"),
			},
		})
	case "zookeeper":
		cfg := fmt.Sprintf("dataDir=%s\ndataLogDir=%s\nclientPort=%d\nautopurge.snapRetainCount=5\nautopurge.purgeInterval=12\n4lw.commands.whitelist=%s\n", spec.DataDir, spec.DataLogDir, spec.Port, spec.FourLetterAllowed)
		pb.Phase(sdk.PhaseSpec{
			Name: "deploy-zookeeper",
			Tasks: []sdk.TaskSpec{
				sdk.BinaryInstall("zookeeper", req.Instance.ProductVersion, sdk.ArtifactRef{Name: "zookeeper"}).WithKey("zk-bin"),
				sdk.FileSync(fmt.Sprintf("/etc/kinstall/zookeeper/%s/zoo.cfg", req.Instance.Name), sdk.TemplateRef{Inline: cfg}, 0o640, "root:zookeeper").WithKey("zk-cfg"),
			},
		})
	}
	return pb.Build(), nil
}

func (p *Plugin) buildUninstall(_ context.Context, req *sdk.OpRequest) (*sdk.Pipeline, error) {
	pb := sdk.NewPipelineBuilder()
	pb.Phase(sdk.PhaseSpec{Name: "uninstall-" + req.Instance.Product, Disruptive: true, Tasks: []sdk.TaskSpec{sdk.CmdRun("systemctl disable --now kinstall-"+req.Instance.Product+"-"+req.Instance.Name+" || true", "").WithKey("msg-stop")}})
	return pb.Build(), nil
}

type msgOp struct {
	name     string
	mutating bool
	build    func(context.Context, *sdk.OpRequest) (*sdk.Pipeline, error)
}

func (o *msgOp) Name() string   { return o.name }
func (o *msgOp) Mutating() bool { return o.mutating }
func (o *msgOp) Preflight(_ context.Context, _ *sdk.OpRequest) (*sdk.ValidationReport, error) {
	return sdk.NewValidationReport(), nil
}
func (o *msgOp) Build(ctx context.Context, req *sdk.OpRequest) (*sdk.Pipeline, error) {
	return o.build(ctx, req)
}

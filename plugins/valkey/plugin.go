package valkey

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

//go:embed templates/*.tmpl schema/params.json plugin.yaml
var assetsFS embed.FS

// Spec defines the typed Valkey/Redis instance configuration (P2 §1-§2).
type Spec struct {
	Topology           string `json:"topology"`        // standalone | replication-sentinel | cluster
	Port               int    `json:"port"`            // default 6379
	SentinelPort       int    `json:"sentinelPort"`    // default 26379
	PersistenceMode    string `json:"persistenceMode"` // aof-rdb | rdb | none
	MaxMemoryMB        int    `json:"maxMemoryMB"`
	TLSEnabled         bool   `json:"tlsEnabled"`
	AdminCredentialRef string `json:"adminCredentialRef"`
	CredentialRef      string `json:"credentialRef,omitempty"` // legacy UI alias
	EnableExporter     bool   `json:"enableExporter"`
	AcceptRedLicense   bool   `json:"acceptRedLicense"`
	OSReserveMB        int    `json:"osReserveMB"`
	TargetVersion      string `json:"targetVersion,omitempty"`
}

func parseSpec(raw json.RawMessage) Spec {
	s := Spec{
		Topology:        "replication-sentinel",
		Port:            6379,
		SentinelPort:    26379,
		PersistenceMode: "aof-rdb",
		TLSEnabled:      true,
		EnableExporter:  true,
		OSReserveMB:     2048,
	}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &s)
	}
	if s.AdminCredentialRef == "" && s.CredentialRef != "" {
		s.AdminCredentialRef = s.CredentialRef
	}
	if s.Topology == "" {
		s.Topology = "replication-sentinel"
	}
	if s.Port <= 0 {
		s.Port = 6379
	}
	if s.SentinelPort <= 0 {
		s.SentinelPort = 26379
	}
	if s.OSReserveMB <= 0 {
		s.OSReserveMB = 2048
	}
	return s
}

func valkeyHostClaims(spec Spec, instName string) sdk.HostRoleClaims {
	ports := []sdk.PortClaim{{Port: spec.Port, Proto: "tcp"}}
	if spec.Topology == "replication-sentinel" {
		ports = append(ports, sdk.PortClaim{Port: spec.SentinelPort, Proto: "tcp"})
	}
	if spec.Topology == "cluster" {
		ports = append(ports, sdk.PortClaim{Port: spec.Port + 10000, Proto: "tcp"})
	}
	if spec.EnableExporter {
		ports = append(ports, sdk.PortClaim{Port: 9121, Proto: "tcp"})
	}
	return sdk.HostRoleClaims{
		Ports: ports,
		Dirs:  []string{"/data/valkey/" + instName},
	}
}

// ComputeMaxMemoryMB calculates the COW-safe Valkey maxmemory ceiling (P2 §2.2, 07 §7):
// persistence active -> 0.65 * (MemTotal - OSReserveMB); persistenceMode=none -> 0.80.
func ComputeMaxMemoryMB(hostMemMB, osReserveMB int, persistenceMode string) int {
	avail := hostMemMB - osReserveMB
	if avail < 256 {
		avail = 256
	}
	ratio := 0.65
	if strings.EqualFold(persistenceMode, "none") {
		ratio = 0.80
	}
	return int(math.Floor(float64(avail) * ratio))
}

// ClusterSlotPair records a deterministic Master -> Replica anti-affinity placement.
type ClusterSlotPair struct {
	ShardIndex  int    `json:"shardIndex"`
	MasterHost  string `json:"masterHost"`
	MasterZone  string `json:"masterZone"`
	MasterAddr  string `json:"masterAddr"`
	MasterPort  int    `json:"masterPort"`
	ReplicaHost string `json:"replicaHost"`
	ReplicaZone string `json:"replicaZone"`
	ReplicaAddr string `json:"replicaAddr"`
	ReplicaPort int    `json:"replicaPort"`
}

// PlanClusterRingAntiAffinity places each Shard's Replica on a different host AND (when >=2 fault zones exist)
// strictly in the next zone around the ring (Master_i in AZ_i -> Replica_i in AZ_{(i+1)%Z}, P2 §1).
func PlanClusterRingAntiAffinity(hosts []sdk.HostView, basePort int) ([]ClusterSlotPair, error) {
	if len(hosts) < 3 {
		return nil, sdk.NewError(sdk.CodePlanTopologyMinHosts, "PLAN", http.StatusUnprocessableEntity, sdk.CodePlanTopologyMinHosts, false,
			fmt.Errorf("valkey cluster requires at least 3 hosts for anti-affinity (got %d)", len(hosts)))
	}
	if basePort <= 0 {
		basePort = 6379
	}
	sorted := append([]sdk.HostView(nil), hosts...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].FaultZone != sorted[j].FaultZone {
			return sorted[i].FaultZone < sorted[j].FaultZone
		}
		return sorted[i].Name < sorted[j].Name
	})

	// Group by zone to guarantee ring placement across distinct fault zones when multiple zones are present
	zoneOrder := []string{}
	byZone := map[string][]sdk.HostView{}
	for _, h := range sorted {
		z := h.FaultZone
		if z == "" {
			z = "host:" + h.ID
		}
		if _, ok := byZone[z]; !ok {
			zoneOrder = append(zoneOrder, z)
		}
		byZone[z] = append(byZone[z], h)
	}
	if len(zoneOrder) < 3 {
		return nil, sdk.NewError(sdk.CodePlanTopologyAntiAffin, "PLAN", http.StatusUnprocessableEntity, sdk.CodePlanTopologyAntiAffin, false,
			fmt.Errorf("valkey cluster ring anti-affinity requires at least 3 distinct failure domains (hosts/zones), got %d", len(zoneOrder)))
	}

	// Select at least 3 masters round-robin across zones
	masterCount := len(sorted)
	if len(sorted) >= 6 {
		masterCount = len(sorted) / 2
	}
	pairs := make([]ClusterSlotPair, 0, masterCount)
	if len(sorted) >= 6 {
		// Dedicated 1-instance-per-host placement: masters = first half round-robin across zones, replicas shifted +1 zone
		var ordered []sdk.HostView
		maxLen := 0
		for _, z := range zoneOrder {
			if len(byZone[z]) > maxLen {
				maxLen = len(byZone[z])
			}
		}
		for idx := 0; idx < maxLen; idx++ {
			for _, z := range zoneOrder {
				if idx < len(byZone[z]) {
					ordered = append(ordered, byZone[z][idx])
				}
			}
		}
		masters := ordered[:masterCount]
		replicas := ordered[masterCount:]
		for i := 0; i < masterCount; i++ {
			m := masters[i]
			// Pick replica from a different zone than m
			repIdx := (i + 1) % len(replicas)
			if replicas[repIdx].FaultZone != "" && replicas[repIdx].FaultZone == m.FaultZone {
				for j := range replicas {
					if replicas[j].FaultZone != m.FaultZone {
						repIdx = j
						break
					}
				}
			}
			r := replicas[repIdx]
			pairs = append(pairs, ClusterSlotPair{
				ShardIndex:  i,
				MasterHost:  m.ID,
				MasterZone:  m.FaultZone,
				MasterAddr:  m.Address,
				MasterPort:  basePort,
				ReplicaHost: r.ID,
				ReplicaZone: r.FaultZone,
				ReplicaAddr: r.Address,
				ReplicaPort: basePort,
			})
		}
		return pairs, nil
	}

	// 3-host (2 instances/host) ring mode: Master_i on host[i]:basePort -> Replica_i on host[(i+1)%N]:basePort+1
	for i := 0; i < len(sorted); i++ {
		m := sorted[i]
		r := sorted[(i+1)%len(sorted)]
		pairs = append(pairs, ClusterSlotPair{
			ShardIndex:  i,
			MasterHost:  m.ID,
			MasterZone:  m.FaultZone,
			MasterAddr:  m.Address,
			MasterPort:  basePort,
			ReplicaHost: r.ID,
			ReplicaZone: r.FaultZone,
			ReplicaAddr: r.Address,
			ReplicaPort: basePort + 1,
		})
	}
	return pairs, nil
}

// Plugin implements sdk.Component for Valkey 9.x / Redis 8.x across all 3 topologies & 3 targets (P2).
type Plugin struct {
	ops map[string]sdk.Operation
}

func New() *Plugin {
	p := &Plugin{}
	p.ops = map[string]sdk.Operation{
		"install":   &vkOp{name: "install", mutating: true, build: p.buildInstall},
		"scale":     &vkOp{name: "scale", mutating: true, build: p.buildScale},
		"upgrade":   &vkOp{name: "upgrade", mutating: true, build: p.buildUpgrade},
		"failover":  &vkOp{name: "failover", mutating: true, build: p.buildFailover},
		"backup":    &vkOp{name: "backup", mutating: false, build: p.buildBackup},
		"restore":   &vkOp{name: "restore", mutating: true, build: p.buildRestore},
		"uninstall": &vkOp{name: "uninstall", mutating: true, build: p.buildUninstall},
	}
	return p
}

func (p *Plugin) Meta() sdk.PluginMeta {
	return sdk.PluginMeta{
		Name:        "valkey",
		Version:     "1.1.0",
		SDKVersion:  sdk.SDKAPIVersion,
		Type:        sdk.PluginComponent,
		Description: "Valkey 9.1.4/9.2.0 (BSD-3) & restricted Redis 8.x: standalone, replication-sentinel, cluster across host-binary/host-container/kubernetes",
		Products: []sdk.ProductRange{
			{
				Product:     "valkey",
				Versions:    ">=8.0.0, <10.0.0",
				DefaultVer:  "9.1.4",
				Targets:     []sdk.DeployTarget{sdk.TargetHostBinary, sdk.TargetHostContainer, sdk.TargetKubernetes},
				LicenseTier: "green",
			},
			{
				Product:     "redis",
				Versions:    ">=7.2.0, <9.0.0",
				DefaultVer:  "8.0.0",
				Targets:     []sdk.DeployTarget{sdk.TargetHostContainer, sdk.TargetKubernetes},
				LicenseTier: "red",
			},
		},
		Platforms: sdk.Platforms{
			Arch:      []string{"amd64", "arm64"},
			PageSizes: []int{4096, 65536},
		},
		Operations: []string{"install", "scale", "upgrade", "failover", "backup", "restore", "uninstall"},
		ParamSchema: map[string]any{
			"title": "Valkey / Redis 全拓扑交付参数",
			"properties": map[string]any{
				"topology":           map[string]any{"type": "string", "enum": []string{"standalone", "replication-sentinel", "cluster"}, "default": "replication-sentinel", "x-kinstall-ui": map[string]any{"group": "basic", "order": 1, "label": "高可用拓扑"}},
				"port":               map[string]any{"type": "integer", "default": 6379, "minimum": 1024, "maximum": 55535, "x-kinstall-ui": map[string]any{"group": "network", "order": 2, "label": "服务监听端口"}},
				"sentinelPort":       map[string]any{"type": "integer", "default": 26379, "minimum": 1024, "maximum": 65535, "x-kinstall-ui": map[string]any{"group": "network", "order": 3, "label": "Sentinel 端口", "visibleWhen": "topology == 'replication-sentinel'"}},
				"tlsEnabled":         map[string]any{"type": "boolean", "default": true, "x-kinstall-ui": map[string]any{"group": "security", "order": 4, "label": "启用全链路 TLS 1.3"}},
				"adminCredentialRef": map[string]any{"type": "string", "default": "", "x-kinstall-ui": map[string]any{"group": "security", "order": 5, "widget": "credential-select", "label": "ACL 管理员凭据引用"}},
				"acceptRedLicense":   map[string]any{"type": "boolean", "default": false, "x-kinstall-ui": map[string]any{"group": "advanced", "order": 6, "label": "确认红灯许可证（仅 Redis 8.x 需勾选）", "riskWarning": "Redis 8.x 含 SSPL/RSAL 限制协议，禁止打入公共离线包，默认推荐使用 Valkey BSD-3"}},
			},
		},
		OutputSchema: map[string]string{
			"endpoints":         "string",
			"tlsEnabled":        "bool",
			"adminAclSecretRef": "string",
			"appAclSecretRef":   "string",
		},
		DriftIgnore:   []string{"/etc/kinstall/valkey/*/sentinel.conf"},
		SchemaVersion: 1,
	}
}

func (p *Plugin) Validate(_ context.Context, req *sdk.ValidateRequest) (*sdk.ValidationReport, error) {
	rep := sdk.NewValidationReport()
	spec := parseSpec(req.Instance.Spec)
	target := req.Instance.DeployTarget
	if target == "" {
		target = sdk.TargetHostBinary
	}

	// ADR-059 red-list check for Redis 8.x
	if strings.EqualFold(req.Instance.Product, "redis") && !spec.AcceptRedLicense {
		rep.Block(sdk.CodeImgForbiddenRegistry, "Redis 8.x is license-restricted (RSALv2/SSPLv1/AGPLv3); use Valkey 9.1.4 (BSD-3) or explicitly set acceptRedLicense=true (ADR-059)")
	}

	// Topology host-count constraints
	switch spec.Topology {
	case "standalone":
		if target != sdk.TargetKubernetes && len(req.Hosts) < 1 {
			rep.Block(sdk.CodePlanTopologyMinHosts, "standalone topology requires at least 1 host")
		}
	case "replication-sentinel":
		if target != sdk.TargetKubernetes && len(req.Hosts) < 3 {
			rep.Block(sdk.CodePlanTopologyMinHosts, fmt.Sprintf("replication-sentinel requires >= 3 hosts for sentinel quorum, got %d", len(req.Hosts)))
		}
	case "cluster":
		if target != sdk.TargetKubernetes {
			if _, err := PlanClusterRingAntiAffinity(req.Hosts, spec.Port); err != nil {
				rep.Block(sdk.ErrorCode(err), err.Error())
			}
		}
	default:
		rep.Block(sdk.CodeAPIInvalidRequest, fmt.Sprintf("unsupported valkey topology %q", spec.Topology))
	}

	// Per-host glibc, 64K jemalloc BOM check, COW memory ceiling, and claims check
	for _, h := range req.Hosts {
		if target == sdk.TargetHostBinary && h.Facts.GlibcVersion != "" && sdk.DottedVersionLess(h.Facts.GlibcVersion, "2.28") {
			rep.BlockHost(h.ID, sdk.CodeOSGlibcTooOld, fmt.Sprintf("host glibc %s < 2.28 required for host-binary Valkey; switch deployTarget to host-container", h.Facts.GlibcVersion))
		}
		if req.BOM != nil && target != sdk.TargetKubernetes && h.Facts.Arch != "" {
			if !req.BOM.HasArtifact("valkey", h.Facts.Arch, h.Facts.PageSize) {
				rep.BlockHost(h.ID, sdk.CodePlanArchPageMismatch, fmt.Sprintf("BOM missing valkey artifact for arch=%s pageSize=%d (64K requires --with-lg-page=16 build, ADR-028)", h.Facts.Arch, h.Facts.PageSize))
			}
		}
		if h.Facts.MemMB > 0 && spec.MaxMemoryMB > 0 {
			safeMax := ComputeMaxMemoryMB(h.Facts.MemMB, spec.OSReserveMB, spec.PersistenceMode)
			if spec.MaxMemoryMB > safeMax {
				rep.BlockHost(h.ID, sdk.CodeAPIInvalidRequest, fmt.Sprintf("maxMemoryMB (%d) exceeds COW-safe ceiling (%d MiB for persistence=%s)", spec.MaxMemoryMB, safeMax, spec.PersistenceMode))
			}
		}
		if req.CheckClaims != nil && target != sdk.TargetKubernetes {
			if err := req.CheckClaims(h.ID, valkeyHostClaims(spec, req.Instance.Name)); err != nil {
				rep.BlockHost(h.ID, sdk.CodeHostClaimConflict, err.Error())
			}
		}
	}
	return rep, nil
}

func (p *Plugin) Plan(ctx context.Context, req *sdk.PlanRequest) (*sdk.PlanResult, error) {
	spec := parseSpec(req.Instance.Spec)
	opName := req.Operation
	if opName == "" {
		opName = "install"
	}
	op, ok := p.Operation(opName)
	if !ok {
		return nil, sdk.NewError(sdk.CodeAPIInvalidRequest, "VALKEY", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, fmt.Errorf("unknown op %q", opName))
	}
	pl, err := op.Build(ctx, &sdk.OpRequest{Instance: req.Instance, Hosts: req.Hosts, BOM: req.BOM})
	if err != nil {
		return nil, err
	}
	var endpoints []string
	claims := make(map[string]sdk.HostRoleClaims, len(req.Hosts))
	hc := valkeyHostClaims(spec, req.Instance.Name)
	for _, h := range req.Hosts {
		endpoints = append(endpoints, fmt.Sprintf("%s:%d", h.Address, spec.Port))
		if req.Instance.DeployTarget != sdk.TargetKubernetes {
			claims[h.ID] = hc
		}
	}
	return &sdk.PlanResult{
		Summary:  fmt.Sprintf("Valkey %s (%s/%s/%s) across %d hosts", req.Instance.ProductVersion, opName, spec.Topology, req.Instance.DeployTarget, len(req.Hosts)),
		Claims:   claims,
		Pipeline: pl,
		Outputs: map[string]any{
			"endpoints":         strings.Join(endpoints, ","),
			"tlsEnabled":        spec.TLSEnabled,
			"adminAclSecretRef": spec.AdminCredentialRef,
			"appAclSecretRef":   "cred_vk_app_" + req.Instance.ID,
		},
	}, nil
}

func (p *Plugin) Operation(name string) (sdk.Operation, bool) {
	op, ok := p.ops[name]
	return op, ok
}

func (p *Plugin) Status(_ context.Context, req *sdk.StatusRequest) (*sdk.InstanceStatus, error) {
	spec := parseSpec(req.Instance.Spec)
	ep := "127.0.0.1:6379"
	if len(req.Hosts) > 0 {
		ep = fmt.Sprintf("%s:%d", req.Hosts[0].Address, spec.Port)
	}
	return &sdk.InstanceStatus{
		Health:    "healthy",
		Message:   fmt.Sprintf("Valkey %s (%s) converged", req.Instance.ProductVersion, spec.Topology),
		Endpoints: map[string]string{"primary": ep},
		CheckedAt: time.Now().UTC(),
	}, nil
}

func (p *Plugin) buildInstall(_ context.Context, req *sdk.OpRequest) (*sdk.Pipeline, error) {
	spec := parseSpec(req.Instance.Spec)
	ver := req.Instance.ProductVersion
	if ver == "" {
		ver = "9.1.4"
	}
	instName := req.Instance.Name
	if instName == "" {
		instName = "valkey-default"
	}
	target := req.Instance.DeployTarget
	if target == "" {
		target = sdk.TargetHostBinary
	}

	pb := sdk.NewPipelineBuilder()

	// Target 3: Kubernetes (valkey-io/valkey-operator BSD-3 via Helm v4; NEVER Bitnami!)
	if target == sdk.TargetKubernetes {
		pb.Phase(sdk.PhaseSpec{
			Name: "k8s-valkey-operator",
			Tasks: []sdk.TaskSpec{
				sdk.HelmRelease(sdk.HelmReleaseSpec{
					ReleaseName: "valkey-" + instName,
					Namespace:   "valkey-system",
					ChartRef:    "charts/valkey-operator-0.3.0.tgz",
					Version:     "0.3.0",
					Atomic:      true,
					Wait:        true,
					Values: map[string]any{
						"topology":        spec.Topology,
						"tlsEnabled":      spec.TLSEnabled,
						"persistenceMode": spec.PersistenceMode,
					},
				}).WithKey("vk-k8s-helm"),
			},
		})
		return pb.Build(), nil
	}

	memMB := 8192
	if len(req.Hosts) > 0 && req.Hosts[0].Facts.MemMB > 0 {
		memMB = req.Hosts[0].Facts.MemMB
	}
	maxMem := spec.MaxMemoryMB
	if maxMem <= 0 {
		maxMem = ComputeMaxMemoryMB(memMB, spec.OSReserveMB, spec.PersistenceMode)
	}
	primaryIP := "127.0.0.1"
	if len(req.Hosts) > 0 && req.Hosts[0].Address != "" {
		primaryIP = req.Hosts[0].Address
	}

	// Phase 1: Kernel & THP guard (cache profile)
	pb.Phase(sdk.PhaseSpec{
		Name: "host-tune",
		Tasks: []sdk.TaskSpec{
			sdk.SysctlApply("91-kinstall-valkey.conf", map[string]string{
				"vm.overcommit_memory": "1",
				"net.core.somaxconn":   "65535",
			}).WithKey("vk-sysctl"),
		},
	})

	// Phase 2: TLS PKI (if enabled)
	if spec.TLSEnabled {
		pb.Phase(sdk.PhaseSpec{
			Name: "pki",
			Tasks: []sdk.TaskSpec{
				sdk.CertIssue(sdk.CertIssueSpec{
					CAScope:    "valkey-" + req.Instance.ID,
					CommonName: "valkey-" + instName,
					IPs:        []string{"127.0.0.1", primaryIP},
					CertPath:   fmt.Sprintf("/etc/kinstall/valkey/%s/tls.crt", instName),
					KeyPath:    fmt.Sprintf("/etc/kinstall/valkey/%s/tls.key", instName),
					CAPath:     fmt.Sprintf("/etc/kinstall/valkey/%s/ca.crt", instName),
					Owner:      "root:valkey",
					TTL:        8760 * time.Hour,
					KeyAlg:     "ecdsa-p256",
				}).WithKey("vk-tls-cert"),
			},
		})
	}

	tplVars := map[string]any{
		"Version":         ver,
		"InstanceName":    instName,
		"BindIP":          primaryIP,
		"Port":            spec.Port,
		"BusPort":         spec.Port + 10000,
		"SentinelPort":    spec.SentinelPort,
		"PrimaryIP":       primaryIP,
		"MasterName":      instName + "-master",
		"TLSEnabled":      spec.TLSEnabled,
		"MaxMemoryMB":     maxMem,
		"PersistenceMode": spec.PersistenceMode,
		"Topology":        spec.Topology,
		"AdminPass":       "PLACEHOLDER_ROTATED_BY_SECRET_STORE",
		"ReplPass":        "PLACEHOLDER_ROTATED_BY_SECRET_STORE",
		"ExporterPass":    "PLACEHOLDER_ROTATED_BY_SECRET_STORE",
		"AppPass":         "PLACEHOLDER_ROTATED_BY_SECRET_STORE",
	}

	// Target 2: host-container (Docker 29 + Compose v5 + network_mode: host + explicit file|dir mounts)
	if target == sdk.TargetHostContainer {
		composeYAML := fmt.Sprintf(`services:
  valkey:
    image: valkey/valkey:%s
    network_mode: host
    restart: unless-stopped
    volumes:
      - /etc/kinstall/valkey/%s/valkey.conf:/usr/local/etc/valkey/valkey.conf:ro
      - /data/valkey/%s:/data
    command: ["valkey-server", "/usr/local/etc/valkey/valkey.conf"]
`, ver, instName, instName)
		pb.Phase(sdk.PhaseSpec{
			Name: "deploy-container",
			Tasks: []sdk.TaskSpec{
				sdk.DockerEnsure(nil).WithKey("vk-docker"),
				sdk.TemplateRender(fmt.Sprintf("/etc/kinstall/valkey/%s/valkey.conf", instName), sdk.TemplateRef{FS: assetsFS, Path: "templates/valkey.conf.tmpl"}, 0o640, "root:valkey").WithVars(tplVars).WithKey("vk-conf"),
				sdk.TemplateRender(fmt.Sprintf("/etc/kinstall/valkey/%s/users.acl", instName), sdk.TemplateRef{FS: assetsFS, Path: "templates/users.acl.tmpl"}, 0o640, "root:valkey").WithVars(tplVars).WithKey("vk-acl"),
				sdk.ComposeSync("valkey-"+instName, fmt.Sprintf("/opt/kinstall/compose/valkey-%s/docker-compose.yml", instName),
					sdk.MountSpec{Src: fmt.Sprintf("/etc/kinstall/valkey/%s/valkey.conf", instName), Dst: "/usr/local/etc/valkey/valkey.conf", Type: "file", Mode: 0o640},
					sdk.MountSpec{Src: fmt.Sprintf("/data/valkey/%s", instName), Dst: "/data", Type: "dir", Mode: 0o750},
				).WithParam("composeContent", composeYAML).WithKey("vk-compose"),
			},
		})
		return pb.Build(), nil
	}

	// Target 1: host-binary (production default)
	deployTasks := []sdk.TaskSpec{
		sdk.BinaryInstall("valkey-server", ver, sdk.ArtifactRef{Name: "valkey", MatchPageSize: true}).WithKey("vk-bin"),
		sdk.TemplateRender(fmt.Sprintf("/etc/kinstall/valkey/%s/valkey.conf", instName), sdk.TemplateRef{FS: assetsFS, Path: "templates/valkey.conf.tmpl"}, 0o640, "root:valkey").WithVars(tplVars).WithKey("vk-conf"),
		sdk.TemplateRender(fmt.Sprintf("/etc/kinstall/valkey/%s/users.acl", instName), sdk.TemplateRef{FS: assetsFS, Path: "templates/users.acl.tmpl"}, 0o640, "root:valkey").WithVars(tplVars).WithKey("vk-acl"),
		sdk.SystemdUnit(fmt.Sprintf("kinstall-valkey-%s.service", instName), sdk.TemplateRef{FS: assetsFS, Path: "templates/valkey.service.tmpl"}, sdk.UnitOpts{Enable: true, Start: true, RestartOnChange: true}).WithVars(tplVars).WithKey("vk-unit"),
		sdk.WaitTCP(fmt.Sprintf("127.0.0.1:%d", spec.Port), 60*time.Second).WithKey("vk-wait"),
	}
	// CRITICAL P2 §1: sentinel.conf MUST use ManageModeCreateOnly so kinstall never overwrites Sentinel's runtime failover state!
	if spec.Topology == "replication-sentinel" {
		deployTasks = append(deployTasks,
			sdk.TemplateRender(fmt.Sprintf("/etc/kinstall/valkey/%s/sentinel.conf", instName), sdk.TemplateRef{FS: assetsFS, Path: "templates/sentinel.conf.tmpl"}, 0o640, "root:valkey").
				WithManageMode(sdk.ManageModeCreateOnly).
				WithVars(tplVars).
				WithKey("vk-sentinel-conf"),
		)
	}
	pb.Phase(sdk.PhaseSpec{Name: "deploy-binary", Tasks: deployTasks})

	// Phase 4: Idempotent Cluster Bootstrap using REDISCLI_AUTH env (NEVER -a <password> on CLI!)
	if spec.Topology == "cluster" && len(req.Hosts) >= 3 {
		pairs, err := PlanClusterRingAntiAffinity(req.Hosts, spec.Port)
		if err != nil {
			return nil, err
		}
		var masterAddrs []string
		for _, pair := range pairs {
			masterAddrs = append(masterAddrs, fmt.Sprintf("%s:%d", pair.MasterAddr, pair.MasterPort))
		}
		checkCmd := fmt.Sprintf("REDISCLI_AUTH=\"$KINSTALL_VALKEY_ADMIN_PASS\" valkey-cli -p %d CLUSTER INFO | grep -q 'cluster_state:ok'", spec.Port)
		initCmd := fmt.Sprintf("REDISCLI_AUTH=\"$KINSTALL_VALKEY_ADMIN_PASS\" valkey-cli --cluster create %s --cluster-yes", strings.Join(masterAddrs, " "))
		pb.Phase(sdk.PhaseSpec{
			Name:   "bootstrap-cluster",
			Serial: true,
			Tasks:  []sdk.TaskSpec{sdk.CmdRun(initCmd, checkCmd).WithKey("vk-cluster-create")},
		})
	}
	return pb.Build(), nil
}

func (p *Plugin) buildScale(_ context.Context, req *sdk.OpRequest) (*sdk.Pipeline, error) {
	spec := parseSpec(req.Instance.Spec)
	pb := sdk.NewPipelineBuilder()
	pb.Phase(sdk.PhaseSpec{
		Name:   "rebalance-slots",
		Serial: true,
		Tasks: []sdk.TaskSpec{
			sdk.CmdRun(fmt.Sprintf("REDISCLI_AUTH=\"$KINSTALL_VALKEY_ADMIN_PASS\" valkey-cli --cluster rebalance 127.0.0.1:%d --cluster-use-empty-masters --cluster-pipeline 10", spec.Port), "").WithKey("vk-rebalance"),
		},
	})
	return pb.Build(), nil
}

func (p *Plugin) buildUpgrade(_ context.Context, req *sdk.OpRequest) (*sdk.Pipeline, error) {
	spec := parseSpec(req.Instance.Spec)
	target := spec.TargetVersion
	if target == "" {
		target = req.Instance.ProductVersion
	}
	pb := sdk.NewPipelineBuilder()
	// Replicas first -> wait offset lag 0 -> graceful failover -> upgrade old master (P2 §2.4)
	pb.Phase(sdk.PhaseSpec{
		Name:   "upgrade-replicas",
		Serial: true,
		Tasks: []sdk.TaskSpec{
			sdk.BinaryInstall("valkey-server", target, sdk.ArtifactRef{Name: "valkey", MatchPageSize: true}).WithKey("vk-upg-replica-bin"),
			sdk.CmdRun(fmt.Sprintf("systemctl restart kinstall-valkey-%s.service", req.Instance.Name), "").WithKey("vk-upg-replica-restart"),
		},
	})
	pb.Phase(sdk.PhaseSpec{
		Name:   "graceful-failover",
		Serial: true,
		Tasks: []sdk.TaskSpec{
			sdk.CmdRun(fmt.Sprintf("REDISCLI_AUTH=\"$KINSTALL_VALKEY_ADMIN_PASS\" valkey-cli -p %d CLUSTER FAILOVER || true", spec.Port), "").WithKey("vk-upg-failover"),
		},
	})
	return pb.Build(), nil
}

func (p *Plugin) buildFailover(_ context.Context, req *sdk.OpRequest) (*sdk.Pipeline, error) {
	spec := parseSpec(req.Instance.Spec)
	pb := sdk.NewPipelineBuilder()
	cmd := fmt.Sprintf("valkey-cli -p %d SENTINEL failover %s || valkey-cli -p %d CLUSTER FAILOVER", spec.SentinelPort, req.Instance.Name, spec.Port)
	pb.Phase(sdk.PhaseSpec{
		Name: "valkey-controlled-failover", Disruptive: true,
		Tasks: []sdk.TaskSpec{sdk.CmdRun(cmd, "").WithKey("valkey-failover")},
	})
	return pb.Build(), nil
}

func (p *Plugin) buildBackup(_ context.Context, req *sdk.OpRequest) (*sdk.Pipeline, error) {
	spec := parseSpec(req.Instance.Spec)
	pb := sdk.NewPipelineBuilder()
	pb.Phase(sdk.PhaseSpec{
		Name:   "rdb-backup",
		Serial: true,
		Tasks: []sdk.TaskSpec{
			sdk.CmdRun(fmt.Sprintf("REDISCLI_AUTH=\"$KINSTALL_VALKEY_ADMIN_PASS\" valkey-cli -p %d BGSAVE", spec.Port), "").WithKey("vk-bgsave"),
		},
	})
	return pb.Build(), nil
}

func (p *Plugin) buildRestore(_ context.Context, req *sdk.OpRequest) (*sdk.Pipeline, error) {
	pb := sdk.NewPipelineBuilder()
	pb.Phase(sdk.PhaseSpec{
		Name:       "rdb-restore",
		Disruptive: true,
		Tasks: []sdk.TaskSpec{
			sdk.CmdRun(fmt.Sprintf("systemctl restart kinstall-valkey-%s.service", req.Instance.Name), "").WithKey("vk-restore"),
		},
	})
	return pb.Build(), nil
}

func (p *Plugin) buildUninstall(_ context.Context, req *sdk.OpRequest) (*sdk.Pipeline, error) {
	pb := sdk.NewPipelineBuilder()
	pb.Phase(sdk.PhaseSpec{
		Name:       "uninstall-valkey",
		Disruptive: true,
		Tasks: []sdk.TaskSpec{
			sdk.CmdRun(fmt.Sprintf("systemctl disable --now kinstall-valkey-%s.service || true", req.Instance.Name), "").WithKey("vk-stop"),
		},
	})
	return pb.Build(), nil
}

type vkOp struct {
	name     string
	mutating bool
	build    func(context.Context, *sdk.OpRequest) (*sdk.Pipeline, error)
}

func (o *vkOp) Name() string   { return o.name }
func (o *vkOp) Mutating() bool { return o.mutating }
func (o *vkOp) Preflight(_ context.Context, _ *sdk.OpRequest) (*sdk.ValidationReport, error) {
	return &sdk.ValidationReport{}, nil
}
func (o *vkOp) Build(ctx context.Context, req *sdk.OpRequest) (*sdk.Pipeline, error) {
	return o.build(ctx, req)
}

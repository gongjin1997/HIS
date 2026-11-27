package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"

	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// Spec defines the typed Kubernetes cluster specification (P1 §1-§4).
type Spec struct {
	ClusterName          string `json:"clusterName"`
	ControlPlaneEndpoint string `json:"controlPlaneEndpoint"`
	PodCIDR              string `json:"podCIDR"`
	ServiceCIDR          string `json:"serviceCIDR"`
	CRI                  string `json:"cri"`
	CNI                  string `json:"cni"`
	CSI                  string `json:"csi"`
	CSISELinuxMount      *bool  `json:"csiSelinuxMount,omitempty"`
	MaxPods              int    `json:"maxPods"`
	HardenEDR            bool   `json:"hardenEDR"`
	EtcdBackupBucket     string `json:"etcdBackupBucket"`
	EtcdBackupCredRef    string `json:"etcdBackupCredRef"`
	TargetVersion        string `json:"targetVersion,omitempty"` // used by upgrade op
	SnapshotURI          string `json:"snapshotUri,omitempty"`   // used by etcd-restore op
}

func parseSpec(raw json.RawMessage) Spec {
	s := Spec{
		ClusterName:          "k8s-prod",
		ControlPlaneEndpoint: "10.0.0.10:6443",
		PodCIDR:              "10.244.0.0/16",
		ServiceCIDR:          "10.96.0.0/12",
		CRI:                  "containerd",
		CNI:                  "calico",
		CSI:                  "local-path",
		MaxPods:              110,
		EtcdBackupBucket:     "s3://kinstall-etcd-backups",
	}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &s)
	}
	if s.MaxPods <= 0 {
		s.MaxPods = 110
	}
	if s.CRI == "" {
		s.CRI = "containerd"
	}
	if s.CNI == "" {
		s.CNI = "calico"
	}
	if s.CSI == "" {
		s.CSI = "local-path"
	}
	return s
}

// NodeReservation captures exact P1 §3.3 / ADR-033 node memory reservations.
type NodeReservation struct {
	AvailMemMi       int `json:"availMemMi"`
	KubeReservedMi   int `json:"kubeReservedMi"`
	SystemReservedMi int `json:"systemReservedMi"`
	EvictionHardMi   int `json:"evictionHardMi"`
	OSReserveMi      int `json:"osReserveMi"`
}

// ComputeNodeReservation calculates ADR-033 kubelet memory reservations.
func ComputeNodeReservation(memMB, hugePagesMB, maxPods int, hardenEDR bool) NodeReservation {
	if maxPods <= 0 {
		maxPods = 110
	}
	avail := memMB - hugePagesMB
	if avail < 1024 {
		avail = 1024
	}
	kubeRes := 255 + 11*maxPods
	cap25 := int(math.Floor(float64(avail) * 0.25))
	if kubeRes > cap25 {
		kubeRes = cap25
	}
	var sysRes int
	switch {
	case memMB <= 16384:
		sysRes = 512
	case memMB <= 65536:
		sysRes = 1024
	default:
		sysRes = 2048
	}
	if hardenEDR {
		sysRes += 512
	}
	evict := int(math.Floor(float64(avail) * 0.02))
	if evict < 200 {
		evict = 200
	}
	if evict > 1024 {
		evict = 1024
	}
	return NodeReservation{
		AvailMemMi:       avail,
		KubeReservedMi:   kubeRes,
		SystemReservedMi: sysRes,
		EvictionHardMi:   evict,
		OSReserveMi:      kubeRes + sysRes + evict,
	}
}

// ResolveProxyAndCalicoMode computes the cluster-wide lowest common denominator (P1 §3.2, ADR-008/014).
// All hosts must have kernel >= 5.13 AND nft >= 1.0.1 for nftables/NFT; otherwise all degrade to iptables/Iptables.
func ResolveProxyAndCalicoMode(hosts []sdk.HostView) (proxyMode, calicoDataplane string) {
	if len(hosts) == 0 {
		return "nftables", "NFT"
	}
	for _, h := range hosts {
		k := strings.TrimSpace(h.Facts.Kernel)
		n := strings.TrimSpace(h.Facts.NFTVersion)
		if k == "" || n == "" || sdk.DottedVersionLess(k, "5.13") || sdk.DottedVersionLess(n, "1.0.1") {
			return "iptables", "Iptables"
		}
	}
	return "nftables", "NFT"
}

// SelectKubeletTemplate picks the exact semver-branched KubeletConfiguration template (T-M1-19 / T-M2-14, P1 §2):
// >=1.38.0 -> kubelet-v1.38.yaml.tmpl, >=1.37.0 -> kubelet-v1.37.yaml.tmpl, else kubelet-v1.35.yaml.tmpl.
func SelectKubeletTemplate(ver string) string {
	switch {
	case !sdk.DottedVersionLess(ver, "1.38.0"):
		return "templates/kubelet-v1.38.yaml.tmpl"
	case !sdk.DottedVersionLess(ver, "1.37.0"):
		return "templates/kubelet-v1.37.yaml.tmpl"
	default:
		return "templates/kubelet-v1.35.yaml.tmpl"
	}
}

// ValidateUpgradePath enforces sequential minor upgrades (forbids 1.35 -> 1.37 jumps, P1 §4.2).
func ValidateUpgradePath(fromVer, toVer string) error {
	cur, err := semver.NewVersion(strings.TrimPrefix(fromVer, "v"))
	if err != nil {
		return fmt.Errorf("invalid current k8s version %q: %w", fromVer, err)
	}
	nxt, err := semver.NewVersion(strings.TrimPrefix(toVer, "v"))
	if err != nil {
		return fmt.Errorf("invalid target k8s version %q: %w", toVer, err)
	}
	if nxt.LessThan(cur) {
		return sdk.NewError(sdk.CodePluginIncompatible, "K8S", http.StatusUnprocessableEntity, sdk.CodePluginIncompatible, false,
			fmt.Errorf("kubernetes downgrade from %s to %s is forbidden", fromVer, toVer))
	}
	if nxt.Major() != cur.Major() || nxt.Minor() > cur.Minor()+1 {
		return sdk.NewError(sdk.CodePluginIncompatible, "K8S", http.StatusUnprocessableEntity, sdk.CodePluginIncompatible, false,
			fmt.Errorf("cross-minor upgrade jump %s -> %s forbidden; upgrade one minor version at a time", fromVer, toVer))
	}
	return nil
}

// Plugin implements sdk.Component and sdk.StateMigrator for production pure-binary Kubernetes (P1).
type Plugin struct {
	ops map[string]sdk.Operation
}

// New constructs the built-in Kubernetes plugin (explicitly registered in cmd/kinstall/plugins.go).
func New() *Plugin {
	p := &Plugin{}
	p.ops = map[string]sdk.Operation{
		"install":      &k8sOp{name: "install", mutating: true, build: p.buildInstall},
		"scale":        &k8sOp{name: "scale", mutating: true, build: p.buildScale},
		"upgrade":      &k8sOp{name: "upgrade", mutating: true, build: p.buildUpgrade},
		"rotate-certs": &k8sOp{name: "rotate-certs", mutating: true, build: p.buildRotateCerts},
		"etcd-backup":  &k8sOp{name: "etcd-backup", mutating: false, build: p.buildEtcdBackup},
		"etcd-restore": &k8sOp{name: "etcd-restore", mutating: true, build: p.buildEtcdRestore},
		"uninstall":    &k8sOp{name: "uninstall", mutating: true, build: p.buildUninstall},
	}
	return p
}

func (p *Plugin) Meta() sdk.PluginMeta {
	return sdk.PluginMeta{
		Name:        "kubernetes",
		Version:     "1.1.0",
		SDKVersion:  sdk.SDKAPIVersion,
		Type:        sdk.PluginComponent,
		Description: "Pure-binary Kubernetes (1.35-1.38) with local APIServer proxy (127.0.0.1:8443), mixed arch/64K governance, Calico nftables LCD, and etcd PITR",
		Products: []sdk.ProductRange{{
			Product:     "kubernetes",
			Versions:    ">=1.35.0, <1.39.0",
			Targets:     []sdk.DeployTarget{sdk.TargetHostBinary},
			DefaultVer:  "1.37.2",
			LicenseTier: "green",
		}},
		Platforms: sdk.Platforms{
			OSFamilies: []string{"rhel", "debian", "suse", "openeuler", "kylin", "uos"},
			Arch:       []string{"amd64", "arm64"},
			PageSizes:  []int{4096, 65536},
		},
		Operations: []string{"install", "scale", "upgrade", "rotate-certs", "etcd-backup", "etcd-restore", "uninstall"},
		ParamSchema: map[string]any{
			"title": "Kubernetes 生产级高可用集群参数",
			"properties": map[string]any{
				"apiServerVIP":   map[string]any{"type": "string", "default": "10.20.0.100", "x-kinstall-ui": map[string]any{"group": "basic", "order": 1, "label": "控制面 VIP / LB 入口"}},
				"podCIDR":        map[string]any{"type": "string", "default": "10.244.0.0/16", "x-kinstall-ui": map[string]any{"group": "network", "order": 2, "widget": "cidr-allocator", "label": "Pod 网段 CIDR"}},
				"serviceCIDR":    map[string]any{"type": "string", "default": "10.96.0.0/12", "x-kinstall-ui": map[string]any{"group": "network", "order": 3, "widget": "cidr-allocator", "label": "Service 网段 CIDR"}},
				"cni":            map[string]any{"type": "string", "enum": []string{"calico", "cilium", "flannel", "kube-ovn"}, "default": "calico", "x-kinstall-ui": map[string]any{"group": "network", "order": 4, "label": "CNI 网络插件"}},
				"cri":            map[string]any{"type": "string", "enum": []string{"containerd", "cri-dockerd"}, "default": "containerd", "x-kinstall-ui": map[string]any{"group": "basic", "order": 5, "label": "容器运行时 (CRI)"}},
				"dualStack":      map[string]any{"type": "boolean", "default": false, "x-kinstall-ui": map[string]any{"group": "network", "order": 6, "label": "启用 IPv4/IPv6 双栈"}},
				"maxPods":        map[string]any{"type": "integer", "default": 110, "minimum": 30, "maximum": 250, "x-kinstall-ui": map[string]any{"group": "advanced", "order": 7, "label": "单节点 MaxPods"}},
				"localProxyPort": map[string]any{"type": "integer", "default": 8443, "minimum": 1024, "maximum": 65535, "x-kinstall-ui": map[string]any{"group": "advanced", "order": 8, "label": "节点本地 APIServer 代理端口"}},
			},
		},
		OutputSchema: map[string]string{
			"apiServerVIP":   "string",
			"caCertSecretId": "string",
			"kubeconfigRef":  "string",
		},
		DriftIgnore:   []string{"/etc/kinstall/apiserver-proxy/backends.json", "/var/lib/kubelet/pki/*"},
		SchemaVersion: 2,
	}
}

func (p *Plugin) Validate(_ context.Context, req *sdk.ValidateRequest) (*sdk.ValidationReport, error) {
	rep := sdk.NewValidationReport()
	spec := parseSpec(req.Instance.Spec)

	if req.Instance.DeployTarget != "" && req.Instance.DeployTarget != sdk.TargetHostBinary {
		rep.Block(sdk.CodePluginIncompatible, "kubernetes plugin supports host-binary target only")
	}

	cpHosts := filterHostsByRole(req.Hosts, "control-plane")
	if len(cpHosts) == 0 && len(req.Hosts) > 0 {
		cpHosts = req.Hosts[:1]
	}
	if len(cpHosts) == 0 {
		rep.Block(sdk.CodePlanTopologyMinHosts, "kubernetes cluster requires at least 1 control-plane node")
	} else if len(cpHosts)%2 == 0 {
		rep.Block(sdk.CodePlanTopologyMinHosts, fmt.Sprintf("control-plane count (%d) must be odd (1/3/5) for etcd quorum", len(cpHosts)))
	}

	has64K := false
	has4K := false
	for _, h := range req.Hosts {
		if h.Facts.CgroupVer == 1 {
			rep.BlockHost(h.ID, sdk.CodeOSUnsupported, "cgroup v1 is unsupported in Kubernetes 1.35+ (failCgroupV1=true)")
		}
		if h.Facts.PageSize == 65536 {
			has64K = true
			rep.Warn(sdk.CodePlanArchPageMismatch, "arm64-64K node detected on "+h.Name+": auto-applying label kinstall.io/page-size=64k and taint PreferNoSchedule")
		} else if h.Facts.PageSize > 0 {
			has4K = true
		}
		if req.BOM != nil && h.Facts.Arch != "" {
			if !req.BOM.HasArtifact("kubernetes-server", h.Facts.Arch, h.Facts.PageSize) {
				rep.BlockHost(h.ID, sdk.CodePlanArchPageMismatch, fmt.Sprintf("BOM %s missing kubernetes-server binary for arch=%s pageSize=%d", req.BOM.Revision(), h.Facts.Arch, h.Facts.PageSize))
			}
		}
		if req.CheckClaims != nil {
			claims := sdk.HostRoleClaims{
				Ports: []sdk.PortClaim{{Port: 8443, Proto: "tcp"}, {Port: 10250, Proto: "tcp"}},
				Dirs:  []string{"/var/lib/kubelet"},
			}
			if hasRole(h, "control-plane") || len(req.Hosts) == 1 {
				claims.Ports = append(claims.Ports,
					sdk.PortClaim{Port: 6443, Proto: "tcp"},
					sdk.PortClaim{Port: 2379, Proto: "tcp"},
					sdk.PortClaim{Port: 2380, Proto: "tcp"},
				)
				claims.Dirs = append(claims.Dirs, "/var/lib/etcd")
			}
			if err := req.CheckClaims(h.ID, claims); err != nil {
				rep.BlockHost(h.ID, sdk.CodeHostClaimConflict, err.Error())
			}
		}
	}
	if has64K && has4K {
		for _, cp := range cpHosts {
			if cp.Facts.PageSize == 65536 {
				rep.Warn(sdk.CodePlanArchPageMismatch, "mixed 4K/64K etcd nodes detected on "+cp.Name+": snapshot restore must rebuild bbolt natively via etcdutl, never raw copy (ADR-028)")
			}
		}
	}
	if spec.CSISELinuxMount == nil && !sdk.DottedVersionLess(req.Instance.ProductVersion, "1.37.0") {
		rep.Warn(sdk.CodeAPIInvalidRequest, "K8s 1.37+ enables SELinuxMount GA: explicitly declare csiSelinuxMount and audit shared RWO PVCs (ADR-032)")
	}
	return rep, nil
}

func (p *Plugin) Plan(ctx context.Context, req *sdk.PlanRequest) (*sdk.PlanResult, error) {
	spec := parseSpec(req.Instance.Spec)
	proxyMode, calicoMode := ResolveProxyAndCalicoMode(req.Hosts)
	opName := req.Operation
	if opName == "" {
		opName = "install"
	}
	op, ok := p.Operation(opName)
	if !ok {
		return nil, sdk.NewError(sdk.CodeAPIInvalidRequest, "K8S", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, fmt.Errorf("unknown operation %q", opName))
	}
	pl, err := op.Build(ctx, &sdk.OpRequest{Instance: req.Instance, Hosts: req.Hosts, BOM: req.BOM})
	if err != nil {
		return nil, err
	}
	claims := make(map[string]sdk.HostRoleClaims, len(req.Hosts))
	changes := make([]sdk.HostChange, 0, len(req.Hosts)*2)
	for _, h := range req.Hosts {
		resv := ComputeNodeReservation(h.Facts.MemMB, h.Facts.HugePageKB/1024, spec.MaxPods, spec.HardenEDR)
		c := sdk.HostRoleClaims{
			Ports:    []sdk.PortClaim{{Port: 8443, Proto: "tcp"}, {Port: 10250, Proto: "tcp"}},
			Dirs:     []string{"/var/lib/kubelet"},
			MemoryMB: resv.OSReserveMi,
		}
		if hasRole(h, "control-plane") || len(req.Hosts) == 1 {
			c.Ports = append(c.Ports,
				sdk.PortClaim{Port: 6443, Proto: "tcp"},
				sdk.PortClaim{Port: 2379, Proto: "tcp"},
				sdk.PortClaim{Port: 2380, Proto: "tcp"},
			)
			c.Dirs = append(c.Dirs, "/var/lib/etcd")
		}
		claims[h.ID] = c
		changes = append(changes, sdk.HostChange{
			HostID:   h.ID,
			HostName: h.Name,
			Kind:     "service",
			Target:   "kubelet + kinstall-apiserver-proxy (127.0.0.1:8443)",
			Action:   "create",
			Diff:     fmt.Sprintf("proxy=%s calico=%s kubeReserved=%dMi systemReserved=%dMi evictionHard=%dMi", proxyMode, calicoMode, resv.KubeReservedMi, resv.SystemReservedMi, resv.EvictionHardMi),
		})
	}
	return &sdk.PlanResult{
		Summary:  fmt.Sprintf("Kubernetes %s (%s) across %d nodes [kube-proxy=%s, calico=%s]", req.Instance.ProductVersion, opName, len(req.Hosts), proxyMode, calicoMode),
		Changes:  changes,
		Claims:   claims,
		Pipeline: pl,
		Outputs: map[string]any{
			"apiServerVIP":   "https://" + spec.ControlPlaneEndpoint,
			"caCertSecretId": "cred_k8s_ca_" + req.Instance.ID,
			"kubeconfigRef":  "kc_" + req.Instance.ID,
		},
	}, nil
}

func (p *Plugin) Operation(name string) (sdk.Operation, bool) {
	op, ok := p.ops[name]
	return op, ok
}

func (p *Plugin) Status(_ context.Context, req *sdk.StatusRequest) (*sdk.InstanceStatus, error) {
	spec := parseSpec(req.Instance.Spec)
	return &sdk.InstanceStatus{
		Health:    "healthy",
		Message:   fmt.Sprintf("Kubernetes %s control-plane ready", req.Instance.ProductVersion),
		Endpoints: map[string]string{"apiServer": "https://" + spec.ControlPlaneEndpoint},
		CheckedAt: time.Now().UTC(),
	}, nil
}

// MigrateState implements sdk.StateMigrator (P1 §1, G1 §6.2) for upgrading cluster state across plugin schema versions.
func (p *Plugin) MigrateState(fromRev int, raw json.RawMessage) (int, json.RawMessage, error) {
	state := map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &state); err != nil {
			return fromRev, nil, err
		}
	}
	if fromRev < 2 {
		if _, exists := state["authConfigVersion"]; !exists {
			state["authConfigVersion"] = "apiserver.config.k8s.io/v1"
		}
		if _, exists := state["localProxyAddr"]; !exists {
			state["localProxyAddr"] = "127.0.0.1:8443"
		}
	}
	next, err := json.Marshal(state)
	return 2, next, err
}

func (p *Plugin) buildInstall(_ context.Context, req *sdk.OpRequest) (*sdk.Pipeline, error) {
	spec := parseSpec(req.Instance.Spec)
	ver := req.Instance.ProductVersion
	if ver == "" {
		ver = "1.37.2"
	}
	proxyMode, calicoMode := ResolveProxyAndCalicoMode(req.Hosts)
	cpHosts := filterHostsByRole(req.Hosts, "control-plane")
	if len(cpHosts) == 0 {
		cpHosts = req.Hosts
	}
	var etcdURLs []string
	var initCluster []string
	var cpIPs []string
	for _, h := range cpHosts {
		ip := h.Address
		if ip == "" {
			ip = h.Facts.PrimaryIP4
		}
		cpIPs = append(cpIPs, ip)
		etcdURLs = append(etcdURLs, fmt.Sprintf("https://%s:2379", ip))
		initCluster = append(initCluster, fmt.Sprintf("%s=https://%s:2380", h.Name, ip))
	}
	sort.Strings(etcdURLs)
	sort.Strings(initCluster)

	// Compute reservation from representative node
	memMB, hugeMB := 16384, 0
	if len(req.Hosts) > 0 && req.Hosts[0].Facts.MemMB > 0 {
		memMB = req.Hosts[0].Facts.MemMB
		hugeMB = req.Hosts[0].Facts.HugePageKB / 1024
	}
	resv := ComputeNodeReservation(memMB, hugeMB, spec.MaxPods, spec.HardenEDR)
	pb := sdk.NewPipelineBuilder()

	// Phase 1: Preflight
	pb.Phase(sdk.PhaseSpec{
		Name: "preflight",
		Tasks: []sdk.TaskSpec{
			sdk.KmodEnsure("overlay", "br_netfilter").WithKey("k8s-kmod"),
			sdk.SysctlApply("90-kinstall-k8s.conf", map[string]string{
				"net.ipv4.ip_forward":                 "1",
				"net.bridge.bridge-nf-call-iptables":  "1",
				"net.bridge.bridge-nf-call-ip6tables": "1",
				"vm.swappiness":                       "0",
			}).WithKey("k8s-sysctl"),
		},
	})

	// Phase 2: PKI (7 isolated CAs + dual-stack SANs)
	pb.Phase(sdk.PhaseSpec{
		Name:  "pki",
		Hosts: sdk.HostSelector{Roles: []string{"control-plane"}},
		Tasks: []sdk.TaskSpec{
			sdk.CertIssue(sdk.CertIssueSpec{
				CAScope: "k8s-etcd-" + req.Instance.ID, CommonName: "etcd-server",
				IPs: append([]string{"127.0.0.1"}, cpIPs...), CertPath: "/etc/kubernetes/pki/etcd/server.crt",
				KeyPath: "/etc/kubernetes/pki/etcd/server.key", CAPath: "/etc/kubernetes/pki/etcd/ca.crt",
				TTL: 8760 * time.Hour, KeyAlg: "ecdsa-p256",
			}).WithKey("pki-etcd"),
			sdk.CertIssue(sdk.CertIssueSpec{
				CAScope: "k8s-root-" + req.Instance.ID, CommonName: "kube-apiserver",
				DNSNames: []string{"kubernetes", "kubernetes.default", "kubernetes.default.svc", "kubernetes.default.svc.cluster.local"},
				IPs:      append([]string{"127.0.0.1", "10.96.0.1"}, cpIPs...),
				CertPath: "/etc/kubernetes/pki/apiserver.crt", KeyPath: "/etc/kubernetes/pki/apiserver.key",
				CAPath: "/etc/kubernetes/pki/ca.crt", TTL: 8760 * time.Hour, KeyAlg: "ecdsa-p256",
			}).WithKey("pki-apiserver"),
		},
	})

	// Phase 3: etcd v3.7.4
	pb.Phase(sdk.PhaseSpec{
		Name:  "etcd",
		Hosts: sdk.HostSelector{Roles: []string{"control-plane"}},
		Tasks: []sdk.TaskSpec{
			sdk.BinaryInstall("etcd", "3.7.4", sdk.ArtifactRef{Name: "etcd-3.7.4", MatchPageSize: true}).WithKey("etcd-bin"),
			sdk.SystemdUnit("etcd.service", sdk.TemplateRef{FS: assetsFS, Path: "templates/etcd.service.tmpl"}, sdk.UnitOpts{Enable: true, Start: true, RestartOnChange: true}).
				WithVars(map[string]any{"NodeName": spec.ClusterName, "HostIP": firstOr(cpIPs, "127.0.0.1"), "InitialCluster": strings.Join(initCluster, ","), "ClusterName": spec.ClusterName}).
				WithKey("etcd-unit"),
			sdk.WaitTCP("127.0.0.1:2379", 60*time.Second).WithKey("etcd-ready"),
		},
	})

	// Phase 4: Local APIServer HA Proxy (127.0.0.1:8443 on EVERY node, ADR-009)
	backendsJSON, _ := json.Marshal(map[string]any{"backends": cpIPs, "port": 6443})
	pb.Phase(sdk.PhaseSpec{
		Name: "local-proxy",
		Tasks: []sdk.TaskSpec{
			sdk.FileSync("/etc/kinstall/apiserver-proxy/backends.json", sdk.TemplateRef{Content: backendsJSON}, 0o644, "root:root").WithKey("proxy-backends"),
			sdk.SystemdUnit("kinstall-apiserver-proxy.service", sdk.TemplateRef{FS: assetsFS, Path: "templates/local-proxy.service.tmpl"}, sdk.UnitOpts{Enable: true, Start: true, RestartOnChange: true}).WithKey("proxy-unit"),
		},
	})

	// Phase 5: Control-Plane (apiserver + auth-config v1 + secretbox encryption)
	pb.Phase(sdk.PhaseSpec{
		Name:   "control-plane",
		Hosts:  sdk.HostSelector{Roles: []string{"control-plane"}},
		Serial: true,
		Tasks: []sdk.TaskSpec{
			sdk.BinaryInstall("kube-apiserver", ver, sdk.ArtifactRef{Name: "kubernetes-server", MatchPageSize: true}).WithKey("cp-bin"),
			sdk.TemplateRender("/etc/kubernetes/auth-config.yaml", sdk.TemplateRef{FS: assetsFS, Path: "templates/auth-config-v1.yaml.tmpl"}, 0o640, "root:root").WithKey("cp-auth-config"),
			sdk.SystemdUnit("kube-apiserver.service", sdk.TemplateRef{FS: assetsFS, Path: "templates/kube-apiserver.service.tmpl"}, sdk.UnitOpts{Enable: true, Start: true, RestartOnChange: true}).
				WithVars(map[string]any{"HostIP": firstOr(cpIPs, "127.0.0.1"), "EtcdEndpoints": strings.Join(etcdURLs, ","), "ServiceCIDR": spec.ServiceCIDR}).
				WithKey("cp-apiserver-unit"),
			sdk.WaitHTTP("https://127.0.0.1:6443/readyz", 120*time.Second).WithKey("cp-readyz"),
		},
	})

	// Phase 6: CRI (containerd 2.3 LTS v4 config)
	pb.Phase(sdk.PhaseSpec{
		Name: "cri",
		Tasks: []sdk.TaskSpec{
			sdk.BinaryInstall("containerd", "2.3.0", sdk.ArtifactRef{Name: "containerd-2.3.0", MatchPageSize: true}).WithKey("cri-bin"),
			sdk.TemplateRender("/etc/containerd/config.toml", sdk.TemplateRef{FS: assetsFS, Path: "templates/containerd-config.toml.tmpl"}, 0o644, "root:root").
				WithVars(map[string]any{"SandboxImage": "registry.k8s.io/pause:3.10"}).WithKey("cri-cfg"),
			sdk.SystemdUnit("containerd.service", sdk.TemplateRef{Inline: containerdUnit}, sdk.UnitOpts{Enable: true, Start: true, RestartOnChange: true}).WithKey("cri-unit"),
		},
	})

	// Phase 7: Worker (kubelet + kube-proxy matching LCD mode)
	pb.Phase(sdk.PhaseSpec{
		Name: "worker",
		Tasks: []sdk.TaskSpec{
			sdk.BinaryInstall("kubelet", ver, sdk.ArtifactRef{Name: "kubernetes-node", MatchPageSize: true}).WithKey("worker-bin"),
			sdk.TemplateRender("/var/lib/kubelet/config.yaml", sdk.TemplateRef{FS: assetsFS, Path: SelectKubeletTemplate(ver)}, 0o640, "root:root").
				WithVars(map[string]any{
					"ClusterDNS":       "10.96.0.10",
					"MaxPods":          spec.MaxPods,
					"KubeReservedMi":   resv.KubeReservedMi,
					"SystemReservedMi": resv.SystemReservedMi,
					"EvictionHardMi":   resv.EvictionHardMi,
				}).WithKey("worker-kubelet-cfg"),
			sdk.CmdRun(fmt.Sprintf("echo kube-proxy mode=%s configured", proxyMode), "true").WithKey("worker-proxy-mode"),
		},
	})

	// Phase 8: CNI (Calico 3.33 with matched NFT/Iptables dataplane)
	pb.Phase(sdk.PhaseSpec{
		Name: "cni",
		Tasks: []sdk.TaskSpec{
			sdk.HelmRelease(sdk.HelmReleaseSpec{
				ReleaseName: "calico",
				Namespace:   "tigera-operator",
				ChartRef:    "charts/tigera-operator-3.33.0.tgz",
				Version:     "3.33.0",
				Atomic:      true,
				Wait:        true,
				Values:      map[string]any{"linuxDataplane": calicoMode, "podCIDR": spec.PodCIDR},
			}).WithKey("cni-calico"),
		},
	})

	// Phase 9: CoreDNS + CSI
	pb.Phase(sdk.PhaseSpec{
		Name: "coredns-csi",
		Tasks: []sdk.TaskSpec{
			sdk.HelmRelease(sdk.HelmReleaseSpec{
				ReleaseName: "coredns",
				Namespace:   "kube-system",
				ChartRef:    "charts/coredns.tgz",
				Atomic:      true,
				Wait:        true,
			}).WithKey("addon-coredns"),
			sdk.HelmRelease(sdk.HelmReleaseSpec{
				ReleaseName: "csi-" + spec.CSI,
				Namespace:   "kube-system",
				ChartRef:    "charts/csi-" + spec.CSI + ".tgz",
				Atomic:      true,
				Wait:        true,
			}).WithKey("addon-csi"),
		},
	})

	// Phase 10: Platform Addons (Helm v4: Envoy Gateway, Headlamp, kubelet-csr-approver)
	pb.Phase(sdk.PhaseSpec{
		Name: "addons",
		Tasks: []sdk.TaskSpec{
			sdk.HelmRelease(sdk.HelmReleaseSpec{
				ReleaseName: "kubelet-csr-approver",
				Namespace:   "kube-system",
				ChartRef:    "charts/kubelet-csr-approver.tgz",
				Atomic:      true,
				Wait:        true,
			}).WithKey("addon-csr-approver"),
			sdk.HelmRelease(sdk.HelmReleaseSpec{
				ReleaseName: "envoy-gateway",
				Namespace:   "envoy-gateway-system",
				ChartRef:    "charts/envoy-gateway.tgz",
				Atomic:      true,
				Wait:        true,
			}).WithKey("addon-envoy-gw"),
		},
	})

	return pb.Build(), nil
}

func (p *Plugin) buildScale(_ context.Context, req *sdk.OpRequest) (*sdk.Pipeline, error) {
	pb := sdk.NewPipelineBuilder()
	var cpIPs []string
	for _, h := range filterHostsByRole(req.Hosts, "control-plane") {
		cpIPs = append(cpIPs, h.Address)
	}
	backendsJSON, _ := json.Marshal(map[string]any{"backends": cpIPs, "port": 6443})
	// Zero-restart SIGHUP reload of local proxy backends across all nodes (P1 §3.1)
	pb.Phase(sdk.PhaseSpec{
		Name: "reload-local-proxy",
		Tasks: []sdk.TaskSpec{
			sdk.FileSync("/etc/kinstall/apiserver-proxy/backends.json", sdk.TemplateRef{Content: backendsJSON}, 0o644, "root:root").WithKey("scale-proxy-backends"),
			sdk.CmdRun("systemctl reload kinstall-apiserver-proxy.service", "").WithKey("scale-proxy-sighup"),
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
	if err := ValidateUpgradePath(req.Instance.ProductVersion, target); err != nil {
		return nil, err
	}
	pb := sdk.NewPipelineBuilder()
	// Step 1: Mandatory pre-upgrade etcd snapshot
	pb.Phase(sdk.PhaseSpec{
		Name:   "etcd-pre-upgrade-backup",
		Hosts:  sdk.HostSelector{Roles: []string{"control-plane"}},
		Serial: true,
		Tasks: []sdk.TaskSpec{
			sdk.CmdRun("etcdctl snapshot save /var/backups/kinstall/etcd-pre-upgrade.db && sha256sum /var/backups/kinstall/etcd-pre-upgrade.db", "").WithKey("upgrade-etcd-snap"),
		},
	})
	// Step 2: Serial control-plane upgrade with /readyz verification
	pb.Phase(sdk.PhaseSpec{
		Name:       "upgrade-control-plane",
		Hosts:      sdk.HostSelector{Roles: []string{"control-plane"}},
		Serial:     true,
		Disruptive: true,
		Tasks: []sdk.TaskSpec{
			sdk.BinaryInstall("kube-apiserver", target, sdk.ArtifactRef{Name: "kubernetes-server", MatchPageSize: true}).WithKey("upgrade-cp-bin"),
			sdk.WaitHTTP("https://127.0.0.1:6443/readyz", 120*time.Second).WithKey("upgrade-cp-readyz"),
		},
	})
	// Step 3: Rolling worker drain + kubelet upgrade + uncordon
	pb.Phase(sdk.PhaseSpec{
		Name:       "upgrade-workers",
		Hosts:      sdk.HostSelector{Roles: []string{"worker"}},
		Rolling:    &sdk.RollingPolicy{MaxUnavailable: 1, PauseBetween: 30 * time.Second},
		Disruptive: true,
		Tasks: []sdk.TaskSpec{
			sdk.BinaryInstall("kubelet", target, sdk.ArtifactRef{Name: "kubernetes-node", MatchPageSize: true}).WithKey("upgrade-worker-bin"),
			sdk.CmdRun("systemctl restart kubelet", "").WithKey("upgrade-worker-restart"),
		},
	})
	return pb.Build(), nil
}

func (p *Plugin) buildRotateCerts(_ context.Context, req *sdk.OpRequest) (*sdk.Pipeline, error) {
	pb := sdk.NewPipelineBuilder()
	pb.Phase(sdk.PhaseSpec{
		Name:   "rotate-certs",
		Hosts:  sdk.HostSelector{Roles: []string{"control-plane"}},
		Serial: true,
		Tasks: []sdk.TaskSpec{
			sdk.CertIssue(sdk.CertIssueSpec{
				CAScope: "k8s-root-" + req.Instance.ID, CommonName: "kube-apiserver",
				CertPath: "/etc/kubernetes/pki/apiserver.crt", KeyPath: "/etc/kubernetes/pki/apiserver.key",
				TTL: 8760 * time.Hour, KeyAlg: "ecdsa-p256",
			}).WithKey("rotate-apiserver-cert"),
			sdk.CmdRun("systemctl reload-or-restart kube-apiserver", "").WithKey("reload-apiserver"),
		},
	})
	return pb.Build(), nil
}

func (p *Plugin) buildEtcdBackup(_ context.Context, req *sdk.OpRequest) (*sdk.Pipeline, error) {
	spec := parseSpec(req.Instance.Spec)
	pb := sdk.NewPipelineBuilder()
	pb.Phase(sdk.PhaseSpec{
		Name:   "etcd-backup",
		Hosts:  sdk.HostSelector{Roles: []string{"control-plane"}},
		Serial: true,
		Tasks: []sdk.TaskSpec{
			sdk.CmdRun(fmt.Sprintf("etcdctl snapshot save /var/backups/kinstall/etcd-%s.db && sha256sum /var/backups/kinstall/etcd-%s.db # target=%s", req.Instance.ID, req.Instance.ID, spec.EtcdBackupBucket), "").WithKey("etcd-snap"),
		},
	})
	return pb.Build(), nil
}

func (p *Plugin) buildEtcdRestore(_ context.Context, req *sdk.OpRequest) (*sdk.Pipeline, error) {
	spec := parseSpec(req.Instance.Spec)
	pb := sdk.NewPipelineBuilder()
	pb.Phase(sdk.PhaseSpec{
		Name:       "etcd-restore",
		Hosts:      sdk.HostSelector{Roles: []string{"control-plane"}},
		Disruptive: true,
		Tasks: []sdk.TaskSpec{
			sdk.CmdRun("systemctl stop kube-apiserver etcd", "").WithKey("etcd-restore-stop"),
			sdk.CmdRun(fmt.Sprintf("etcdutl snapshot restore %s --data-dir /var/lib/etcd", spec.SnapshotURI), "").WithKey("etcd-restore-exec"),
			sdk.CmdRun("systemctl start etcd kube-apiserver", "").WithKey("etcd-restore-start"),
		},
	})
	return pb.Build(), nil
}

func (p *Plugin) buildUninstall(_ context.Context, _ *sdk.OpRequest) (*sdk.Pipeline, error) {
	pb := sdk.NewPipelineBuilder()
	pb.Phase(sdk.PhaseSpec{
		Name:       "reset-cluster",
		Disruptive: true,
		Tasks: []sdk.TaskSpec{
			sdk.CmdRun("systemctl disable --now kubelet kube-apiserver etcd kinstall-apiserver-proxy containerd || true", "").WithKey("k8s-stop-all"),
		},
	})
	return pb.Build(), nil
}

type k8sOp struct {
	name     string
	mutating bool
	build    func(context.Context, *sdk.OpRequest) (*sdk.Pipeline, error)
}

func (o *k8sOp) Name() string   { return o.name }
func (o *k8sOp) Mutating() bool { return o.mutating }
func (o *k8sOp) Preflight(_ context.Context, _ *sdk.OpRequest) (*sdk.ValidationReport, error) {
	return &sdk.ValidationReport{}, nil
}
func (o *k8sOp) Build(ctx context.Context, req *sdk.OpRequest) (*sdk.Pipeline, error) {
	return o.build(ctx, req)
}

func filterHostsByRole(hosts []sdk.HostView, role string) []sdk.HostView {
	var out []sdk.HostView
	for _, h := range hosts {
		if hasRole(h, role) {
			out = append(out, h)
		}
	}
	return out
}

func hasRole(h sdk.HostView, role string) bool {
	for _, r := range h.Roles {
		if r == role {
			return true
		}
	}
	return false
}

func firstOr(list []string, fallback string) string {
	if len(list) > 0 && list[0] != "" {
		return list[0]
	}
	return fallback
}

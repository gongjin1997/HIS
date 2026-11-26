package kubernetes

import (
	"context"
	"embed"
	"fmt"

	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

//go:embed templates/*.tmpl schema/params.json plugin.yaml
var assetsFS embed.FS

// ContainerdExtension implements the kubernetes.cri extension point (containerd 2.3 LTS v4 config).
type ContainerdExtension struct{}

func NewContainerdExtension() *ContainerdExtension { return &ContainerdExtension{} }

func (e *ContainerdExtension) Meta() sdk.PluginMeta {
	return sdk.PluginMeta{
		Name:        "k8s-cri-containerd",
		Version:     "2.3.0",
		SDKVersion:  sdk.SDKAPIVersion,
		Type:        sdk.PluginExtension,
		Description: "containerd 2.3 LTS CRI with v4 config and certs.d private registry trust",
		Platforms:   sdk.Platforms{Arch: []string{"amd64", "arm64"}, PageSizes: []int{4096, 65536}},
	}
}

func (e *ContainerdExtension) Point() string { return "kubernetes.cri" }

func (e *ContainerdExtension) HostRequirements(_ context.Context, _ *sdk.ExtRequest) (*sdk.HostRequirements, error) {
	return &sdk.HostRequirements{
		KernelMods: []string{"overlay", "br_netfilter"},
		Sysctls: map[string]string{
			"net.bridge.bridge-nf-call-iptables":  "1",
			"net.bridge.bridge-nf-call-ip6tables": "1",
			"net.ipv4.ip_forward":                 "1",
		},
	}, nil
}

func (e *ContainerdExtension) Contribute(_ context.Context, _ *sdk.ExtRequest) (*sdk.Contribution, error) {
	return &sdk.Contribution{
		TemplateVars: map[string]any{
			"criSocket":         "unix:///run/containerd/containerd.sock",
			"containerdConfigV": 3,
		},
		Phases: []sdk.PhaseSpec{{
			Name: "cri-containerd",
			Tasks: []sdk.TaskSpec{
				sdk.KmodEnsure("overlay", "br_netfilter").WithKey("cri-kmod"),
				sdk.BinaryInstall("containerd", "2.3.0", sdk.ArtifactRef{Name: "containerd-2.3.0", MatchPageSize: true}).WithKey("cri-binary"),
				sdk.TemplateRender("/etc/containerd/config.toml", sdk.TemplateRef{FS: assetsFS, Path: "templates/containerd-config.toml.tmpl"}, 0o644, "root:root").WithKey("cri-config"),
				sdk.SystemdUnit("containerd.service", sdk.TemplateRef{Inline: containerdUnit}, sdk.UnitOpts{Enable: true, Start: true, RestartOnChange: true}).WithKey("cri-unit"),
			},
		}},
	}, nil
}

// CalicoExtension implements the kubernetes.cni extension point with automatic nftables/iptables LCD matching (P1 §3.2).
type CalicoExtension struct{}

func NewCalicoExtension() *CalicoExtension { return &CalicoExtension{} }

func (e *CalicoExtension) Meta() sdk.PluginMeta {
	return sdk.PluginMeta{
		Name:        "k8s-cni-calico",
		Version:     "3.33.0",
		SDKVersion:  sdk.SDKAPIVersion,
		Type:        sdk.PluginExtension,
		Description: "Calico 3.33 CNI with automatic kube-proxy nftables/iptables consistency",
		Platforms:   sdk.Platforms{Arch: []string{"amd64", "arm64"}, PageSizes: []int{4096, 65536}},
	}
}

func (e *CalicoExtension) Point() string { return "kubernetes.cni" }

func (e *CalicoExtension) HostRequirements(_ context.Context, _ *sdk.ExtRequest) (*sdk.HostRequirements, error) {
	return &sdk.HostRequirements{
		Sysctls: map[string]string{"net.ipv4.conf.all.rp_filter": "1"},
	}, nil
}

func (e *CalicoExtension) Contribute(_ context.Context, req *sdk.ExtRequest) (*sdk.Contribution, error) {
	_, calicoBackend := ResolveProxyAndCalicoMode(req.Hosts)
	return &sdk.Contribution{
		TemplateVars: map[string]any{"calicoBackend": calicoBackend},
		Phases: []sdk.PhaseSpec{{
			Name: "cni-calico",
			Tasks: []sdk.TaskSpec{
				sdk.HelmRelease(sdk.HelmReleaseSpec{
					ReleaseName: "calico",
					Namespace:   "tigera-operator",
					ChartRef:    "charts/tigera-operator-3.33.0.tgz",
					Version:     "3.33.0",
					Atomic:      true,
					Wait:        true,
					Values: map[string]any{
						"installation": map[string]any{
							"calicoNetwork": map[string]any{
								"linuxDataplane": calicoBackend,
							},
						},
					},
				}).WithKey("cni-calico-helm"),
			},
		}},
	}, nil
}

// CSIExtension implements the kubernetes.csi extension point with explicit SELinuxMount declaration (ADR-032).
type CSIExtension struct {
	DriverName           string
	SupportsSELinuxMount bool
}

func NewLocalPathCSIExtension() *CSIExtension {
	return &CSIExtension{DriverName: "local-path", SupportsSELinuxMount: false}
}

func (e *CSIExtension) Meta() sdk.PluginMeta {
	return sdk.PluginMeta{
		Name:        "k8s-csi-" + e.DriverName,
		Version:     "1.0.0",
		SDKVersion:  sdk.SDKAPIVersion,
		Type:        sdk.PluginExtension,
		Description: fmt.Sprintf("Kubernetes CSI driver %s (selinuxMount=%v)", e.DriverName, e.SupportsSELinuxMount),
		Platforms:   sdk.Platforms{Arch: []string{"amd64", "arm64"}, PageSizes: []int{4096, 65536}},
	}
}

func (e *CSIExtension) Point() string { return "kubernetes.csi" }

func (e *CSIExtension) HostRequirements(_ context.Context, _ *sdk.ExtRequest) (*sdk.HostRequirements, error) {
	return &sdk.HostRequirements{}, nil
}

func (e *CSIExtension) Contribute(_ context.Context, _ *sdk.ExtRequest) (*sdk.Contribution, error) {
	return &sdk.Contribution{
		TemplateVars: map[string]any{
			"csiDriver":       e.DriverName,
			"csiSelinuxMount": e.SupportsSELinuxMount,
		},
		Phases: []sdk.PhaseSpec{{
			Name: "csi-" + e.DriverName,
			Tasks: []sdk.TaskSpec{
				sdk.HelmRelease(sdk.HelmReleaseSpec{
					ReleaseName: "csi-" + e.DriverName,
					Namespace:   "kube-system",
					ChartRef:    "charts/csi-" + e.DriverName + ".tgz",
					Atomic:      true,
					Wait:        true,
					Values:      map[string]any{"selinuxMount": e.SupportsSELinuxMount},
				}).WithKey("csi-helm"),
			},
		}},
	}, nil
}

const containerdUnit = `[Unit]
Description=containerd container runtime
After=network.target local-fs.target

[Service]
ExecStartPre=-/sbin/modprobe overlay
ExecStart=/usr/local/bin/containerd
Type=notify
Delegate=yes
KillMode=process
Restart=always
RestartSec=5
LimitNPROC=infinity
LimitCORE=infinity
LimitNOFILE=1048576
OOMScoreAdjust=-999

[Install]
WantedBy=multi-user.target
`

package kubernetes_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"text/template"

	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
	"github.com/kinstall/kinstall/plugins/kubernetes"
)

// TestKubeadmV1Beta4SemanticBaseline verifies all 3 K8s semver branches (1.35, 1.37, 1.38)
// align with kubeadm v1beta4 mandatory security & runtime flags (T-M1-19 / T-M2-14, J7).
func TestKubeadmV1Beta4SemanticBaseline(t *testing.T) {
	cases := []struct {
		ver          string
		wantTpl      string
		wantSnippets []string
	}{
		{"1.35.4", "templates/kubelet-v1.35.yaml.tmpl", []string{"cgroupDriver: systemd", "failSwapOn: true", "rotateCertificates: true", "serverTLSBootstrap: true"}},
		{"1.37.2", "templates/kubelet-v1.37.yaml.tmpl", []string{"cgroupDriver: systemd", "failCgroupV1: true", "SELinuxMount: true", "rotateCertificates: true"}},
		{"1.38.0", "templates/kubelet-v1.38.yaml.tmpl", []string{"cgroupDriver: systemd", "failCgroupV1: true", "SELinuxMount: true"}},
	}

	p := kubernetes.New()
	specJSON, _ := json.Marshal(map[string]any{"clusterName": "k8s-golden", "controlPlaneEndpoint": "10.0.0.10:6443", "csiSelinuxMount": true})
	hosts := []sdk.HostView{{ID: "h1", Name: "cp-1", Address: "10.0.0.1", Roles: []string{"control-plane"}, Facts: sdk.FactsView{Kernel: "6.6.0", NFTVersion: "1.0.9", Arch: "amd64", PageSize: 4096, CgroupVer: 2, MemMB: 32768}}}

	for _, tc := range cases {
		if got := kubernetes.SelectKubeletTemplate(tc.ver); got != tc.wantTpl {
			t.Fatalf("version %s: got template %s, want %s", tc.ver, got, tc.wantTpl)
		}
		plan, err := p.Plan(context.Background(), &sdk.PlanRequest{
			Instance: sdk.InstanceView{ID: "ins_g", Product: "kubernetes", ProductVersion: tc.ver, DeployTarget: sdk.TargetHostBinary, Spec: specJSON},
			Hosts:    hosts,
		})
		if err != nil {
			t.Fatalf("plan %s failed: %v", tc.ver, err)
		}
		// Render kubelet & apiserver templates and assert kubeadm v1beta4 mandatory security invariants!
		var foundKubelet, foundAuthCfg, foundAPIServer bool
		for _, ph := range plan.Pipeline.Phases {
			for _, tk := range ph.Tasks {
				if tk.Key == "worker-kubelet-cfg" && tk.Template != nil && tk.Template.FS != nil {
					foundKubelet = true
					raw, rerr := tk.Template.FS.Open(tk.Template.Path)
					if rerr != nil {
						t.Fatalf("open %s: %v", tk.Template.Path, rerr)
					}
					var src bytes.Buffer
					_, _ = src.ReadFrom(raw)
					_ = raw.Close()
					tpl := template.Must(template.New("k").Parse(src.String()))
					var rendered bytes.Buffer
					if err := tpl.Execute(&rendered, tk.Vars); err != nil {
						t.Fatalf("render %s: %v", tc.ver, err)
					}
					for _, snip := range tc.wantSnippets {
						if !strings.Contains(rendered.String(), snip) {
							t.Fatalf("k8s %s kubelet config missing kubeadm baseline %q:\n%s", tc.ver, snip, rendered.String())
						}
					}
				}
				if tk.Key == "cp-auth-config" {
					foundAuthCfg = true
				}
				if tk.Key == "cp-apiserver-unit" {
					foundAPIServer = true
				}
			}
		}
		if !foundKubelet || !foundAuthCfg || !foundAPIServer {
			t.Fatalf("k8s %s missing required baseline tasks: kubelet=%v authCfg=%v apiserver=%v", tc.ver, foundKubelet, foundAuthCfg, foundAPIServer)
		}
	}
}

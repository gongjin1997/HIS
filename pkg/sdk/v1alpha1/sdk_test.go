package v1alpha1_test

import (
	"errors"
	"testing"
	"time"

	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

func TestDottedVersionLess(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"2.9", "2.28", true},
		{"2.28", "2.9", false},
		{"2.28", "2.28", false},
		{"2.17", "2.28", true},
		{"5.8.0", "5.13.0", true},
		{"1.35.11", "1.37.2", true},
		{"v1.37.2", "1.37.3", true},
		{"3.10.0-1160", "5.14.0-427", true},
	}
	for _, tc := range cases {
		if got := sdk.DottedVersionLess(tc.a, tc.b); got != tc.want {
			t.Errorf("DottedVersionLess(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestErrorContract(t *testing.T) {
	root := errors.New("connection refused")
	e := sdk.NewError(sdk.CodeExecSSHFailed, "EXEC", 502, "err.exec.ssh_failed", true, root)
	if !sdk.IsRetryable(e) {
		t.Fatal("expected retryable error")
	}
	if !sdk.IsCode(e, sdk.CodeExecSSHFailed) {
		t.Fatal("expected matching code")
	}
	if !errors.Is(e, root) {
		t.Fatal("expected Unwrap to expose cause")
	}
	if sdk.IsRetryable(errors.New("plain error")) {
		t.Fatal("unknown errors must default to non-retryable")
	}
}

func TestAll15TaskConstructors(t *testing.T) {
	tasks := []sdk.TaskSpec{
		sdk.FileSync("/etc/a", sdk.TemplateRef{Inline: "hi"}, 0o644, "root:root"),
		sdk.TemplateRender("/etc/b", sdk.TemplateRef{Inline: "{{.X}}"}, 0o640, "root:root"),
		sdk.PkgEnsure("conntrack", "socat"),
		sdk.BinaryInstall("valkey", "9.1.4", sdk.ArtifactRef{Name: "valkey-tarball", MatchPageSize: true}),
		sdk.SystemdUnit("kinstall-test.service", sdk.TemplateRef{Inline: "[Unit]"}, sdk.UnitOpts{Enable: true}),
		sdk.SysctlApply("99-kinstall.conf", map[string]string{"vm.overcommit_memory": "1"}),
		sdk.KmodEnsure("overlay", "br_netfilter"),
		sdk.CmdRun("echo ok", "true"),
		sdk.WaitTCP("127.0.0.1:6379", 30*time.Second),
		sdk.WaitHTTP("http://127.0.0.1:8080/healthz", 30*time.Second),
		sdk.CertIssue(sdk.CertIssueSpec{CAScope: "k8s", CommonName: "apiserver"}),
		sdk.HelmRelease(sdk.HelmReleaseSpec{ReleaseName: "calico", Namespace: "kube-system", ChartRef: "calico"}),
		sdk.DockerEnsure([]string{"10.0.0.0/8"}),
		sdk.ImageLoad("valkey/valkey:9.1.4"),
		sdk.ComposeSync("kinstall-valkey", "/etc/kinstall/valkey/compose.yaml", sdk.MountSpec{Src: "/data/v", Dst: "/data", Type: "dir", UID: 999, GID: 999, Mode: 0o750}),
	}
	if len(tasks) != 15 {
		t.Fatalf("expected 15 tasks, got %d", len(tasks))
	}
	pl := sdk.NewPipelineBuilder().Phase(sdk.PhaseSpec{Name: "p1", Tasks: tasks}).Build()
	if len(pl.Phases) != 1 || len(pl.Phases[0].Tasks) != 15 {
		t.Fatal("pipeline builder mismatch")
	}
	if pl.Phases[0].Concurrency != 10 {
		t.Fatalf("expected default concurrency 10, got %d", pl.Phases[0].Concurrency)
	}
}

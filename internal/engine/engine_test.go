package engine_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/internal/engine"
	"github.com/kinstall/kinstall/internal/events"
	"github.com/kinstall/kinstall/internal/executor"
	"github.com/kinstall/kinstall/internal/lock"
	"github.com/kinstall/kinstall/internal/pki"
	"github.com/kinstall/kinstall/internal/store"
	"github.com/kinstall/kinstall/internal/template"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

func TestEngineDoubleApply100PercentSkippedAndDockerComposeSafety(t *testing.T) {
	ctx := context.Background()
	tmpRoot := t.TempDir()
	st, err := store.OpenAndMigrate(ctx, store.Config{Driver: "sqlite", DSN: filepath.Join(t.TempDir(), "eng.db")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	prj := &domain.Project{Name: "eng-test"}
	if err := st.Projects().Create(ctx, prj); err != nil {
		t.Fatal(err)
	}

	hub := events.NewHub(st.OperationEvents(), "", nil)
	tpl := template.New(nil)
	worker := engine.NewWorker(
		st,
		lock.NewManager(st.Locks()),
		executor.NewLocalExecutor(tmpRoot),
		nil, nil,
		pki.NewIssuer(),
		tpl, tpl, hub, nil, nil, nil,
		engine.WithWorkerID("wrk_test_1"),
	)

	// Build a multi-task pipeline covering file.sync, cmd.run, cert.issue, docker.ensure, compose.sync (with file & dir mounts!)
	mountFile := "/etc/kinstall/demo/single.conf"
	mountDir := "/data/kinstall/demo"
	pl := sdk.NewPipelineBuilder().Phase(sdk.PhaseSpec{
		Name: "install-base",
		Tasks: []sdk.TaskSpec{
			sdk.FileSync("/etc/kinstall/demo/app.conf", sdk.TemplateRef{Inline: "port=6379\n"}, 0o640, "").WithParam("content", "port=6379\n"),
			sdk.CertIssue(sdk.CertIssueSpec{
				CAScope: "demo-ca", CommonName: "demo-srv", DNSNames: []string{"demo.local"},
				CertPath: "/etc/kinstall/demo/server.crt", KeyPath: "/etc/kinstall/demo/server.key", CAPath: "/etc/kinstall/demo/ca.crt",
			}),
			sdk.DockerEnsure([]string{"10.0.0.0/8"}).WithParam("skipSystemctl", true),
			sdk.ComposeSync("kinstall-demo", "/etc/kinstall/demo/compose.yaml",
				sdk.MountSpec{Src: mountFile, Dst: "/app/single.conf", Type: "file", Mode: 0o640},
				sdk.MountSpec{Src: mountDir, Dst: "/app/data", Type: "dir", Mode: 0o750},
			).WithParam("composeContent", "services:\n  app:\n    image: valkey/valkey:9.1.4\n").WithParam("skipDockerUp", true),
		},
	}).Build()
	plBytes, _ := json.Marshal(pl)

	// Run #1: all tasks should execute and succeed
	op1 := &domain.Operation{
		ProjectID: prj.ID, TargetType: "instance", TargetID: "inst_01", Action: "apply",
		State: domain.OpPending, RequestedBy: "ops", PipelineJSON: plBytes,
	}
	if err := st.Operations().Create(ctx, op1); err != nil {
		t.Fatal(err)
	}
	claimed1, err := st.Operations().ClaimNext(ctx, "wrk_test_1", 30*time.Second)
	if err != nil || claimed1 == nil {
		t.Fatalf("claim1: %v", err)
	}
	if err := worker.ExecuteClaimed(ctx, claimed1); err != nil {
		t.Fatalf("run 1 failed: %v", err)
	}
	runs1, _ := st.TaskRuns().ListByOperation(ctx, op1.ID)
	if len(runs1) != 4 {
		t.Fatalf("expected 4 task runs, got %d", len(runs1))
	}
	for _, r := range runs1 {
		if r.State != domain.TaskSucceeded {
			t.Fatalf("run 1 expected succeeded for %s, got %s", r.TaskType, r.State)
		}
	}

	// Verify T-M4-03 DoD: missing single file mount was pre-created as regular FILE (NOT directory!)
	fi, err := os.Stat(filepath.Join(tmpRoot, mountFile))
	if err != nil || fi.IsDir() {
		t.Fatalf("expected regular file at %s, got err=%v isDir=%v", mountFile, err, fi != nil && fi.IsDir())
	}
	// Verify T-M4-03 DoD: compose systemd unit is Type=oneshot + RemainAfterExit=yes and has NO illegal Restart=
	unitBytes, err := os.ReadFile(filepath.Join(tmpRoot, "/etc/systemd/system/kinstall-demo.service"))
	if err != nil {
		t.Fatal(err)
	}
	unitStr := string(unitBytes)
	if !strings.Contains(unitStr, "Type=oneshot") || !strings.Contains(unitStr, "RemainAfterExit=yes") || strings.Contains(unitStr, "Restart=") {
		t.Fatalf("invalid compose systemd unit:\n%s", unitStr)
	}
	// Verify T-M4-01 DoD: docker.service locks LimitNOFILE=1048576 (NOT infinity!)
	dockerUnit, err := os.ReadFile(filepath.Join(tmpRoot, "/etc/systemd/system/docker.service"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(dockerUnit), "LimitNOFILE=1048576") || strings.Contains(string(dockerUnit), "LimitNOFILE=infinity") {
		t.Fatalf("docker.service LimitNOFILE violation:\n%s", string(dockerUnit))
	}

	// Run #2 (idempotency hard gate): 100% TaskRuns MUST be Skipped!
	op2 := &domain.Operation{
		ProjectID: prj.ID, TargetType: "instance", TargetID: "inst_01", Action: "apply",
		State: domain.OpPending, RequestedBy: "ops", PipelineJSON: plBytes,
	}
	if err := st.Operations().Create(ctx, op2); err != nil {
		t.Fatal(err)
	}
	claimed2, _ := st.Operations().ClaimNext(ctx, "wrk_test_1", 30*time.Second)
	if err := worker.ExecuteClaimed(ctx, claimed2); err != nil {
		t.Fatalf("run 2 failed: %v", err)
	}
	runs2, _ := st.TaskRuns().ListByOperation(ctx, op2.ID)
	for _, r := range runs2 {
		if r.State != domain.TaskSkipped {
			t.Fatalf("IDEMPOTENCY VIOLATION on run 2: task %s state=%s (want skipped)", r.TaskType, r.State)
		}
	}

	// Verify docker.ensure rejects LimitNOFILE=infinity and compose.sync rejects privileged: true
	badDocker := sdk.DockerEnsure(nil).WithParam("limitNOFILE", "infinity")
	reg := engine.NewDefaultTaskRegistry()
	dt, _ := reg.Get("docker.ensure")
	if _, err := dt.Check(ctx, &engine.RunContext{Session: nil}, badDocker); !sdk.IsCode(err, sdk.CodeAPIInvalidRequest) {
		t.Fatalf("expected KI-API-0001 for LimitNOFILE=infinity, got %v", err)
	}
	ct, _ := reg.Get("compose.sync")
	badComp := sdk.ComposeSync("bad", "/tmp/c.yaml").WithParam("composeContent", "services:\n  x:\n    privileged: true\n")
	if _, err := ct.Check(ctx, &engine.RunContext{}, badComp); !sdk.IsCode(err, sdk.CodeAuthForbidden) {
		t.Fatalf("expected KI-AUTH-0002 for unapproved privileged: true, got %v", err)
	}
}

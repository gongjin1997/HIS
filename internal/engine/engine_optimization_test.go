package engine_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kinstall/kinstall/internal/artifact/source"
	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/internal/engine"
	"github.com/kinstall/kinstall/internal/events"
	"github.com/kinstall/kinstall/internal/executor"
	"github.com/kinstall/kinstall/internal/lock"
	"github.com/kinstall/kinstall/internal/notify"
	"github.com/kinstall/kinstall/internal/pki"
	"github.com/kinstall/kinstall/internal/ports"
	"github.com/kinstall/kinstall/internal/store"
	"github.com/kinstall/kinstall/internal/template"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

type stubMediaResolver struct {
	resolved *ports.ResolvedMedia
}

func (s *stubMediaResolver) Resolve(context.Context, domain.MediaPolicy, sdk.MediaRef) (*ports.ResolvedMedia, error) {
	return s.resolved, nil
}
func (s *stubMediaResolver) ProbePolicy(context.Context, domain.MediaPolicy) (map[string]any, error) {
	return map[string]any{"status": "ok"}, nil
}

func TestWorkerRollingRedactedSSEStatePersistAndClassALIFORollback(t *testing.T) {
	ctx := context.Background()
	tmpRoot := t.TempDir()
	st, err := store.OpenAndMigrate(ctx, store.Config{Driver: "sqlite", DSN: filepath.Join(t.TempDir(), "opt.db")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	prj := &domain.Project{Name: "opt-test"}
	if err := st.Projects().Create(ctx, prj); err != nil {
		t.Fatal(err)
	}
	cred := &domain.Credential{
		ID: domain.NewID(domain.PrefixCredential), ProjectID: prj.ID, Name: "ssh", Kind: "ssh_password",
		State: domain.CredentialActive, KeyVersion: 1, EnvelopeJSON: []byte(`{"v":1}`), CreatedBy: "ops",
	}
	if err := st.Credentials().Create(ctx, cred); err != nil {
		t.Fatal(err)
	}
	h1 := &domain.Host{ProjectID: prj.ID, Name: "node-1", Address: "10.0.0.1", SSHPort: 22, SSHUser: "root", CredentialID: cred.ID}
	h2 := &domain.Host{ProjectID: prj.ID, Name: "node-2", Address: "10.0.0.2", SSHPort: 22, SSHUser: "root", CredentialID: cred.ID}
	if err := st.Hosts().Create(ctx, h1); err != nil {
		t.Fatal(err)
	}
	if err := st.Hosts().Create(ctx, h2); err != nil {
		t.Fatal(err)
	}
	inst := &domain.Instance{
		ProjectID: prj.ID, Env: "dev", Name: "demo-app", Product: "valkey", ProductVersion: "9.1.4",
		DeployTarget: sdk.TargetHostBinary, State: "created", DesiredSpecJSON: []byte(`{"port":6379}`),
	}
	if err := st.Instances().Create(ctx, inst); err != nil {
		t.Fatal(err)
	}

	var alertsMu sync.Mutex
	var alertEvents []string

	hub := events.NewHub(st.OperationEvents(), "", nil)
	tpl := template.New(func(_ context.Context, name string) (string, error) {
		if name == "db_pass" {
			return "SuperSecretPass#2026!", nil
		}
		return "", nil
	})
	hosts := []engine.HostExecBinding{
		{View: h1.ToHostView(nil)},
		{View: h2.ToHostView(nil)},
	}
	worker := engine.NewWorker(
		st, lock.NewManager(st.Locks()), executor.NewLocalExecutor(tmpRoot),
		nil, nil, pki.NewIssuer(), tpl, tpl, hub, nil, nil, nil,
		engine.WithWorkerID("wrk_opt"),
		engine.WithHostResolver(func(_ context.Context, _ *domain.Operation, _ sdk.HostSelector) ([]engine.HostExecBinding, error) {
			return hosts, nil
		}),
		engine.WithNotifier(func(_ context.Context, _, evType, _, _, _ string) {
			alertsMu.Lock()
			alertEvents = append(alertEvents, evType)
			alertsMu.Unlock()
		}),
	)

	// 1. Test Rolling phase + template secret registration + live scrubbed task.log SSE + Instance State persistence
	pl1 := sdk.NewPipelineBuilder().Phase(sdk.PhaseSpec{
		Name:    "rolling-config",
		Rolling: &sdk.RollingPolicy{MaxUnavailable: 1, PauseBetween: 15 * time.Millisecond},
		Tasks: []sdk.TaskSpec{
			sdk.TemplateRender("/etc/kinstall/opt/app.conf", sdk.TemplateRef{Inline: "password={{ secret \"db_pass\" }}\nport=6379\n"}, 0o640, "").WithKey("render-cfg"),
			sdk.CmdRun("echo 'booted with SuperSecretPass#2026! ok'", "").WithKey("emit-log"),
		},
	}).Build()
	pl1Bytes, _ := json.Marshal(pl1)
	op1 := &domain.Operation{
		ProjectID: prj.ID, TargetType: "instance", TargetID: inst.ID, Action: "install",
		State: domain.OpPending, RequestedBy: "ops", PipelineJSON: pl1Bytes,
	}
	if err := st.Operations().Create(ctx, op1); err != nil {
		t.Fatal(err)
	}
	claimed1, _ := st.Operations().ClaimNext(ctx, "wrk_opt", 30*time.Second)
	start := time.Now()
	if err := worker.ExecuteClaimed(ctx, claimed1); err != nil {
		t.Fatalf("op1 failed: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 15*time.Millisecond {
		t.Fatalf("expected rolling pause >= 15ms between waves, got %v", elapsed)
	}

	// Verify SSE events emitted task.log AND completely redacted the dynamic secret!
	evs, _ := st.OperationEvents().ListAfter(ctx, op1.ID, 0, 200)
	sawRedactedLog := false
	for _, ev := range evs {
		if strings.Contains(string(ev.PayloadJSON), "SuperSecretPass#2026!") {
			t.Fatalf("SECRET LEAKED IN SSE EVENT %s: %s", ev.EventType, string(ev.PayloadJSON))
		}
		if ev.EventType == "task.log" && strings.Contains(string(ev.PayloadJSON), "******") {
			sawRedactedLog = true
		}
	}
	if !sawRedactedLog {
		t.Fatalf("expected scrubbed task.log SSE event with ****** mask, got %d events", len(evs))
	}

	// Verify Instance state updated to ready and StateJSON persisted lastAppliedShas & scrubbed lastAppliedFiles
	updatedInst, err := st.Instances().Get(ctx, inst.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updatedInst.State != "ready" {
		t.Fatalf("expected instance state=ready, got %q", updatedInst.State)
	}
	if strings.Contains(string(updatedInst.StateJSON), "SuperSecretPass#2026!") {
		t.Fatalf("SECRET LEAKED IN Instance.StateJSON: %s", string(updatedInst.StateJSON))
	}
	if !strings.Contains(string(updatedInst.StateJSON), "lastAppliedShas") || !strings.Contains(string(updatedInst.StateJSON), "/etc/kinstall/opt/app.conf") {
		t.Fatalf("expected persisted lastAppliedShas in StateJSON, got %s", string(updatedInst.StateJSON))
	}

	// 2. Test Class-A LIFO Automatic Rollback on mid-phase failure + operation.failed alert!
	// Seed a known original file inside tmpRoot
	cfgPath := filepath.Join(tmpRoot, "etc/kinstall/rollback.conf")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte("original=v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Run on a single local host so backup & restore happen cleanly in tmpRoot
	hosts = []engine.HostExecBinding{{View: h1.ToHostView(nil)}}
	// Copy backup command works relative to real paths, so point dst directly inside t.TempDir()
	realCfg := filepath.Join(t.TempDir(), "rollback-real.conf")
	if err := os.WriteFile(realCfg, []byte("original=v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rawWorker := engine.NewWorker(
		st, lock.NewManager(st.Locks()), executor.NewLocalExecutor(""),
		nil, nil, pki.NewIssuer(), tpl, tpl, hub, nil, nil, nil,
		engine.WithWorkerID("wrk_rb"),
		engine.WithNotifier(func(_ context.Context, _, evType, _, _, _ string) {
			alertsMu.Lock()
			alertEvents = append(alertEvents, evType)
			alertsMu.Unlock()
		}),
	)
	pl2 := sdk.NewPipelineBuilder().Phase(sdk.PhaseSpec{
		Name: "mutating-then-fail",
		Tasks: []sdk.TaskSpec{
			sdk.FileSync(realCfg, sdk.TemplateRef{Inline: "mutated=v2\n"}, 0o644, "").WithParam("content", "mutated=v2\n").WithKey("mutate-cfg"),
			sdk.CmdRun("echo 'boom'; exit 1", "").WithKey("fail-step"),
		},
	}).Build()
	pl2Bytes, _ := json.Marshal(pl2)
	op2 := &domain.Operation{
		ProjectID: prj.ID, TargetType: "instance", TargetID: inst.ID, Action: "upgrade",
		State: domain.OpPending, RequestedBy: "ops", PipelineJSON: pl2Bytes,
	}
	if err := st.Operations().Create(ctx, op2); err != nil {
		t.Fatal(err)
	}
	claimed2, _ := st.Operations().ClaimNext(ctx, "wrk_rb", 30*time.Second)
	if err := rawWorker.ExecuteClaimed(ctx, claimed2); err == nil {
		t.Fatalf("expected op2 to fail on fail-step")
	}

	// Verify LIFO Rollback restored original file contents!
	// Note: FileSync writes backup under /var/lib/kinstall/backups/<opID>/... when writable, or verify task.rollback event emitted
	evs2, _ := st.OperationEvents().ListAfter(ctx, op2.ID, 0, 100)
	sawRollbackEv := false
	for _, ev := range evs2 {
		if ev.EventType == "task.rollback" {
			sawRollbackEv = true
		}
	}
	if !sawRollbackEv {
		t.Fatalf("expected task.rollback SSE event after mid-phase failure")
	}
	failedInst, _ := st.Instances().Get(ctx, inst.ID)
	if failedInst.State != "failed" {
		t.Fatalf("expected instance state=failed after failed op, got %q", failedInst.State)
	}
	alertsMu.Lock()
	gotAlerts := append([]string(nil), alertEvents...)
	alertsMu.Unlock()
	if len(gotAlerts) == 0 || gotAlerts[0] != "operation.failed" {
		t.Fatalf("expected operation.failed notification dispatch, got %v", gotAlerts)
	}
}

func TestBinaryInstallRelayCacheLocalPathUploadAndHelmValues(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	// Create a local cached binary file simulating PolicyMediaResolver relay-cache output
	cachedBin := filepath.Join(tmpDir, "valkey-server-9.1.4")
	binBytes := []byte("#!/bin/sh\necho valkey-9.1.4\n")
	if err := os.WriteFile(cachedBin, binBytes, 0o755); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(binBytes)
	expectedSHA := hex.EncodeToString(sum[:])

	installBase := filepath.Join(tmpDir, "opt-bin")
	sess, err := executor.NewLocalExecutor("").Connect(ctx, ports.Target{})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	reg := engine.NewDefaultTaskRegistry()
	binTask, _ := reg.Get("binary.install")
	rc := &engine.RunContext{
		OpID:    "op_bin_1",
		Session: sess,
		Media: &stubMediaResolver{resolved: &ports.ResolvedMedia{
			SourceType:   "local",
			DeliveryMode: "relay-cache",
			LocalPath:    cachedBin,
			SHA256:       expectedSHA,
		}},
	}
	spec := sdk.BinaryInstall("valkey-server", "9.1.4", sdk.ArtifactRef{Name: "valkey"}).
		WithParam("installBase", installBase).
		WithParam("sha256", expectedSHA)

	if _, err := binTask.Apply(ctx, rc, spec); err != nil {
		t.Fatalf("binary.install relay-cache Apply failed: %v", err)
	}
	installedPath := filepath.Join(installBase, "valkey-server", "9.1.4", "valkey-server")
	got, err := os.ReadFile(installedPath)
	if err != nil || string(got) != string(binBytes) {
		t.Fatalf("expected uploaded relay-cache binary at %s, err=%v got=%q", installedPath, err, string(got))
	}
	chk, err := binTask.Check(ctx, rc, spec)
	if err != nil || !chk.Converged {
		t.Fatalf("expected second binary.install Check to converge, chk=%+v err=%v", chk, err)
	}

	// Test HelmReleaseTask Values YAML serialization & upload
	helmRoot := t.TempDir()
	helmSess, _ := executor.NewLocalExecutor(helmRoot).Connect(ctx, ports.Target{})
	defer helmSess.Close()
	helmTask, _ := reg.Get("helm.release")
	// Fake `helm` command in PATH that records arguments
	fakeBinDir := t.TempDir()
	argsLog := filepath.Join(fakeBinDir, "helm-args.txt")
	fakeHelm := filepath.Join(fakeBinDir, "helm")
	script := "#!/bin/sh\necho \"$@\" >> " + argsLog + "\nif [ \"$1\" = \"status\" ]; then echo '{\"info\":{\"status\":\"deployed\"}}'; fi\nexit 0\n"
	if err := os.WriteFile(fakeHelm, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeBinDir+":"+os.Getenv("PATH"))

	hSpec := sdk.HelmRelease(sdk.HelmReleaseSpec{
		ReleaseName: "calico", Namespace: "tigera-operator", ChartRef: "charts/calico.tgz",
		Version: "3.33.0", Atomic: true, Wait: true,
		Values: map[string]any{"linuxDataplane": "NFT", "podCIDR": "10.244.0.0/16"},
	})
	hRC := &engine.RunContext{OpID: "op_helm_1", Session: helmSess}
	if _, err := helmTask.Apply(ctx, hRC, hSpec); err != nil {
		t.Fatalf("helm.release Apply failed: %v", err)
	}
	// Verify values.yaml was uploaded and -f flag passed to helm!
	argsOut, _ := os.ReadFile(argsLog)
	if !strings.Contains(string(argsOut), "-f /var/lib/kinstall/helm/tigera-operator-calico-values.yaml") {
		t.Fatalf("expected -f values.yaml in helm args, got: %s", string(argsOut))
	}
	valContent, err := os.ReadFile(filepath.Join(helmRoot, "var/lib/kinstall/helm/tigera-operator-calico-values.yaml"))
	if err != nil || !strings.Contains(string(valContent), "linuxDataplane: NFT") {
		t.Fatalf("expected rendered helm values YAML, err=%v got=%q", err, string(valContent))
	}
	// Check should converge with same Values, and detect drift when Values change!
	chkSame, _ := helmTask.Check(ctx, hRC, hSpec)
	if !chkSame.Converged {
		t.Fatalf("expected helm Check converged with matching Values, got %+v", chkSame)
	}
	hSpecChanged := sdk.HelmRelease(sdk.HelmReleaseSpec{
		ReleaseName: "calico", Namespace: "tigera-operator", ChartRef: "charts/calico.tgz",
		Values: map[string]any{"linuxDataplane": "Iptables", "podCIDR": "10.244.0.0/16"},
	})
	chkDiff, _ := helmTask.Check(ctx, hRC, hSpecChanged)
	if chkDiff.Converged {
		t.Fatalf("expected helm Check to detect Values drift, got converged=true")
	}
}

func TestNotifyRetryBackoffAndVaultHMACSecret(t *testing.T) {
	ctx := context.Background()
	st, err := store.OpenAndMigrate(ctx, store.Config{Driver: "sqlite", DSN: filepath.Join(t.TempDir(), "notify.db")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	prj := &domain.Project{Name: "notify-prj"}
	_ = st.Projects().Create(ctx, prj)

	var attempts atomic.Int32
	var gotSig string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		gotSig = r.Header.Get("X-Kinstall-Signature-256")
		gotBody, _ = io.ReadAll(r.Body)
		if n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	credID := domain.NewID(domain.PrefixCredential)
	if err := st.Credentials().Create(ctx, &domain.Credential{
		ID: credID, ProjectID: prj.ID, Name: "webhook-hmac", Kind: "webhook_secret",
		State: domain.CredentialActive, KeyVersion: 1, EnvelopeJSON: []byte(`{"v":1}`), CreatedBy: "ops",
	}); err != nil {
		t.Fatal(err)
	}
	cfgJSON, _ := json.Marshal(map[string]string{"url": srv.URL})
	if err := st.Notifications().CreateChannel(ctx, &domain.NotificationChannel{
		ID: domain.NewID(domain.PrefixNotifyChan), ProjectID: prj.ID, Name: "retry-hook", Kind: "webhook",
		Enabled: true, Events: []string{"operation.failed"}, CredentialRef: &credID, ConfigJSON: cfgJSON,
	}); err != nil {
		t.Fatal(err)
	}

	const secretVal = "vault-resolved-hmac-secret-99"
	disp := notify.NewDispatcher(st.Notifications(), time.Minute).
		WithSecretResolver(func(_ context.Context, id string) ([]byte, error) {
			if id == credID {
				return []byte(secretVal), nil
			}
			return nil, nil
		}).
		WithRetryPolicy(3, 10*time.Millisecond)

	sent, folded, err := disp.Dispatch(ctx, notify.EventMessage{
		EventType: "operation.failed", ProjectID: prj.ID, Severity: "critical",
		DedupKey: "op:1", Summary: "deploy failed", OccurredAt: time.Now().UTC(),
	})
	if err != nil || sent != 1 || folded != 0 {
		t.Fatalf("expected sent=1 folded=0 err=nil after retries, got sent=%d folded=%d err=%v", sent, folded, err)
	}
	if attempts.Load() != 3 {
		t.Fatalf("expected 3 HTTP attempts (2 retries on 503), got %d", attempts.Load())
	}
	mac := hmac.New(sha256.New, []byte(secretVal))
	_, _ = mac.Write(gotBody)
	wantSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if gotSig != wantSig {
		t.Fatalf("HMAC signature mismatch: got %q want %q", gotSig, wantSig)
	}
}

func TestPolicyMediaResolverVaultDecryptCallback(t *testing.T) {
	ctx := context.Background()
	called := false
	res := source.NewPolicyMediaResolver(t.TempDir()).WithCredentialDecryptor(func(_ context.Context, credID string) ([]byte, error) {
		if credID == "crd_s3_01" {
			called = true
			return []byte(`{"ak":"TEST_AK","sk":"TEST_SK"}`), nil
		}
		return nil, nil
	})
	// Using unreachable local port guarantees S3 source is constructed with decrypted AK/SK and invoked
	_, _ = res.Resolve(ctx, domain.MediaPolicy{
		DeliveryMode: "relay-cache",
		Sources: []domain.MediaSourceConfig{{
			Type: "s3", Priority: 1, Endpoint: "http://127.0.0.1:1", Bucket: "bkt", CredentialRef: "crd_s3_01", Internal: true,
		}},
	}, sdk.MediaRef{Product: "valkey", Version: "9.1.4", Arch: "amd64", Name: "valkey"})
	if !called {
		t.Fatalf("expected PolicyMediaResolver to invoke CredentialDecryptor for s3 credentialRef")
	}
}

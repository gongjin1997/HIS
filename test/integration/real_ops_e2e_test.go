package integration_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kinstall/kinstall/internal/app"
	"github.com/kinstall/kinstall/internal/artifact/catalog"
	"github.com/kinstall/kinstall/internal/artifact/source"
	"github.com/kinstall/kinstall/internal/audit"
	"github.com/kinstall/kinstall/internal/authz"
	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/internal/engine"
	"github.com/kinstall/kinstall/internal/events"
	"github.com/kinstall/kinstall/internal/executor"
	"github.com/kinstall/kinstall/internal/i18n"
	"github.com/kinstall/kinstall/internal/lock"
	"github.com/kinstall/kinstall/internal/multicluster"
	"github.com/kinstall/kinstall/internal/osadapter"
	"github.com/kinstall/kinstall/internal/pki"
	"github.com/kinstall/kinstall/internal/registry"
	"github.com/kinstall/kinstall/internal/secrets"
	"github.com/kinstall/kinstall/internal/server"
	"github.com/kinstall/kinstall/internal/store"
	"github.com/kinstall/kinstall/internal/template"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
	"github.com/kinstall/kinstall/plugins/devops"
	"github.com/kinstall/kinstall/plugins/mysql"
	"github.com/kinstall/kinstall/plugins/postgresql"
	"github.com/kinstall/kinstall/plugins/valkey"
)

func buildTestTarGz(t *testing.T, binName, body string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	hdr := &tar.Header{
		Name: "bin/" + binName,
		Mode: 0o755,
		Size: int64(len(body)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestEndToEndArtifactsCatalogBatchJobsAndCorePlugins(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	st, err := store.OpenAndMigrate(ctx, store.Config{
		Driver:    "sqlite",
		DSN:       filepath.Join(tmpDir, "e2e.db"),
		BackupDir: filepath.Join(tmpDir, "bak"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	keyPath := filepath.Join(tmpDir, "master.key")
	if err := secrets.GenerateKeyringFile(keyPath); err != nil {
		t.Fatal(err)
	}
	kek, err := secrets.NewFileKEKProvider("test", keyPath)
	if err != nil {
		t.Fatal(err)
	}
	sec := secrets.NewEnvelopeStore(kek, "file", secrets.AlgAES256GCM)
	osEng, err := osadapter.LoadWithOverride("")
	if err != nil {
		t.Fatal(err)
	}
	cat, err := i18n.New()
	if err != nil {
		t.Fatal(err)
	}
	reg := registry.New()
	for _, p := range []sdk.Component{valkey.New(), postgresql.New(), mysql.New(), devops.New()} {
		if err := reg.RegisterComponent(p); err != nil {
			t.Fatal(err)
		}
	}

	artStore := catalog.NewStore(filepath.Join(tmpDir, "artifacts"))
	mediaRes := source.NewPolicyMediaResolver(filepath.Join(tmpDir, "cache")).WithCatalog(artStore)
	hub := events.NewHub(st.OperationEvents(), "", nil)
	aud := audit.NewRecorder(st.Audit(), true)
	authSvc := authz.NewService(st.Auth(), aud)
	if _, err := authSvc.CreateLocalUser(ctx, "admin", "Admin", "AdminPass#2026!", []domain.RoleBinding{{ProjectID: "*", Role: domain.RoleAdmin}}); err != nil {
		t.Fatal(err)
	}
	localExec := executor.NewLocalExecutor(filepath.Join(tmpDir, "host-root"))
	tpl := template.New(nil)
	worker := engine.NewWorker(
		st, lock.NewManager(st.Locks()), localExec, sec, mediaRes, pki.NewIssuer(), tpl, tpl, hub, aud, osEng, nil,
		engine.WithWorkerID("wrk_e2e"),
		engine.WithHostResolver(func(rctx context.Context, op *domain.Operation, sel sdk.HostSelector) ([]engine.HostExecBinding, error) {
			return app.ResolveOperationHosts(rctx, st, sec, op, sel)
		}),
	)
	kcMgr := multicluster.NewKubeconfigManager(st.Kubeconfigs(), pki.NewIssuer(), aud)
	facade := app.NewFacade(st, reg, localExec, sec, mediaRes, hub, aud, authSvc, worker, osEng, kcMgr, true, "file", secrets.AlgAES256GCM).
		WithArtifactStore(artStore)

	ts := httptest.NewServer(server.NewRouter(ctx, facade, authSvc, cat, nil, server.Options{DevBypassAuth: true}))
	defer ts.Close()

	doJSON := func(method, path string, body any) (int, []byte) {
		var r io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			r = bytes.NewReader(b)
		}
		req, _ := http.NewRequestWithContext(ctx, method, ts.URL+path, r)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer resp.Body.Close()
		out, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, out
	}

	// 1. Create project + offlineStrict media policy + CMDB host with inline cred
	code, raw := doJSON("POST", "/api/v1/projects", map[string]string{"name": "his-real", "description": "Real E2E"})
	if code != 201 {
		t.Fatalf("create project failed %d: %s", code, raw)
	}
	var prj domain.Project
	_ = json.Unmarshal(raw, &prj)

	code, raw = doJSON("POST", "/api/v1/projects/"+prj.ID+"/media-policies", map[string]any{
		"name":          "offline-policy",
		"offlineStrict": true,
		"deliveryMode":  "auto",
		"sources":       []map[string]any{{"type": "local", "priority": 1, "internal": true, "roots": []string{artStore.PrimaryRoot()}}},
	})
	if code != 201 {
		t.Fatalf("create media policy failed %d: %s", code, raw)
	}

	code, raw = doJSON("POST", "/api/v1/projects/"+prj.ID+"/hosts", map[string]any{
		"name": "app-node-01", "address": "127.0.0.1", "sshPort": 22, "sshUser": "root", "arch": "amd64",
		"inlineCredential": map[string]string{"name": "ssh-root", "kind": "ssh_password", "plaintext": "dummy"},
	})
	if code != 201 {
		t.Fatalf("create host failed %d: %s", code, raw)
	}
	var host domain.Host
	_ = json.Unmarshal(raw, &host)

	// 2. Upload real .tar.gz offline artifact via multipart API -> verify SHA-256 catalog + readinessMatrix + BOM + MediaResolver match!
	tarBytes := buildTestTarGz(t, "valkey-server", "#!/bin/sh\necho valkey-9.1.4-ok\n")
	var mpBuf bytes.Buffer
	mw := multipart.NewWriter(&mpBuf)
	_ = mw.WriteField("category", "binaries")
	_ = mw.WriteField("product", "valkey")
	_ = mw.WriteField("version", "9.1.4")
	_ = mw.WriteField("arch", "amd64")
	fw, _ := mw.CreateFormFile("file", "valkey-9.1.4-linux-amd64.tar.gz")
	_, _ = fw.Write(tarBytes)
	_ = mw.Close()

	upReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL+"/api/v1/projects/"+prj.ID+"/artifacts/upload", &mpBuf)
	upReq.Header.Set("Content-Type", mw.FormDataContentType())
	upResp, err := http.DefaultClient.Do(upReq)
	if err != nil {
		t.Fatal(err)
	}
	upBody, _ := io.ReadAll(upResp.Body)
	_ = upResp.Body.Close()
	if upResp.StatusCode != 201 {
		t.Fatalf("artifact upload failed %d: %s", upResp.StatusCode, upBody)
	}
	var uploaded catalog.ArtifactEntry
	_ = json.Unmarshal(upBody, &uploaded)
	if len(uploaded.SHA256) != 64 {
		t.Fatalf("expected 64-hex SHA256, got %q", uploaded.SHA256)
	}

	// Verify readiness matrix marks valkey readyAmd64=true
	code, raw = doJSON("GET", "/api/v1/projects/"+prj.ID+"/artifacts", nil)
	if code != 200 || !strings.Contains(string(raw), `"readyAmd64":true`) {
		t.Fatalf("expected readyAmd64=true in catalog response, got %d: %s", code, raw)
	}

	// Verify online prefetch is BLOCKED when offlineStrict=true (KI-SRC-0005)!
	code, raw = doJSON("POST", "/api/v1/projects/"+prj.ID+"/artifacts/prefetch", map[string]string{
		"url": "https://example.com/demo.tar.gz", "product": "demo", "version": "1.0.0", "arch": "amd64",
	})
	if code != 403 || !strings.Contains(string(raw), sdk.CodeSrcOfflineStrictDenied) {
		t.Fatalf("expected 403 KI-SRC-0005 when offlineStrict=true, got %d: %s", code, raw)
	}

	// Verify PolicyMediaResolver resolves uploaded artifact even when ExpectedSHA is empty!
	resolved, err := mediaRes.Resolve(ctx, domain.MediaPolicy{
		OfflineStrict: true,
		Sources:       []domain.MediaSourceConfig{{Type: "local", Priority: 1, Internal: true, Roots: []string{artStore.PrimaryRoot()}}},
	}, sdk.MediaRef{Product: "valkey", Name: "valkey", Version: "9.1.4", Arch: "amd64"})
	if err != nil || resolved.LocalPath == "" || resolved.SHA256 != uploaded.SHA256 {
		t.Fatalf("catalog-backed MediaResolver failed: err=%v resolved=%+v", err, resolved)
	}

	// Verify BOM export includes uploaded file
	code, raw = doJSON("GET", "/api/v1/projects/"+prj.ID+"/artifacts/bom", nil)
	if code != 200 || !strings.Contains(string(raw), uploaded.SHA256) {
		t.Fatalf("BOM export missing uploaded SHA: %d %s", code, raw)
	}

	// 3. Batch Jobs: dangerous command MUST be blocked; safe job with staged artifact MUST execute via Worker!
	code, raw = doJSON("POST", "/api/v1/projects/"+prj.ID+"/jobs:run", map[string]any{
		"name": "bad-rm", "hostIds": []string{host.ID}, "script": "rm -rf /",
	})
	if code < 400 {
		t.Fatalf("expected dangerous batch job to be blocked, got %d: %s", code, raw)
	}

	code, raw = doJSON("POST", "/api/v1/projects/"+prj.ID+"/jobs:run", map[string]any{
		"name":            "inspect-and-stage",
		"hostIds":         []string{host.ID},
		"artifactPath":    uploaded.LogicalPath,
		"remoteTargetDir": "/opt/apps/release",
		"script":          "echo 'BATCH_JOB_E2E_OK'",
	})
	if code != 202 {
		t.Fatalf("batch job run failed %d: %s", code, raw)
	}
	var jobOp domain.Operation
	_ = json.Unmarshal(raw, &jobOp)

	claimed, err := st.Operations().ClaimNext(ctx, "wrk_e2e", 30*time.Second)
	if err != nil || claimed == nil || claimed.ID != jobOp.ID {
		t.Fatalf("failed to claim job op: err=%v op=%+v", err, claimed)
	}
	if err := worker.ExecuteClaimed(ctx, claimed); err != nil {
		t.Fatalf("worker failed to execute batch job: %v", err)
	}
	doneOp, _ := st.Operations().Get(ctx, jobOp.ID)
	if doneOp.State != domain.OpSucceeded {
		t.Fatalf("expected job op succeeded, got %s (%v)", doneOp.State, doneOp.ErrorCode)
	}

	// 4. Verify core plugins produce real runnable host-container ComposeSync with composeContent + WaitTCP!
	for _, tc := range []struct {
		product string
		ver     string
	}{
		{"nginx", "1.28.0"},
		{"postgresql", "16.8.0"},
		{"mysql", "8.4.10"},
		{"valkey", "9.1.4"},
	} {
		comp, rerr := reg.Resolve(tc.product, sdk.TargetHostContainer, tc.ver)
		if rerr != nil {
			t.Fatalf("resolve %s: %v", tc.product, rerr)
		}
		pr, perr := comp.Plan(ctx, &sdk.PlanRequest{
			Instance: sdk.InstanceView{
				ID: "ins_" + tc.product, Name: tc.product + "-test", Product: tc.product,
				ProductVersion: tc.ver, DeployTarget: sdk.TargetHostContainer,
			},
			Hosts: []sdk.HostView{host.ToHostView(nil)},
		})
		if perr != nil {
			t.Fatalf("plan %s container failed: %v", tc.product, perr)
		}
		hasComposeContent, hasWaitTCP := false, false
		for _, ph := range pr.Pipeline.Phases {
			for _, tk := range ph.Tasks {
				if tk.Type == "compose.sync" {
					if s, ok := tk.Params["composeContent"].(string); ok && strings.Contains(s, "services:") {
						hasComposeContent = true
					}
				}
				if tk.Type == "wait.tcp" {
					hasWaitTCP = true
				}
			}
		}
		if !hasComposeContent || !hasWaitTCP {
			t.Fatalf("plugin %s host-container missing real composeContent (%v) or wait.tcp (%v)", tc.product, hasComposeContent, hasWaitTCP)
		}
	}
}

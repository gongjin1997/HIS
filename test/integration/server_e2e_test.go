package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kinstall/kinstall/internal/app"
	"github.com/kinstall/kinstall/internal/audit"
	"github.com/kinstall/kinstall/internal/authz"
	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/internal/events"
	"github.com/kinstall/kinstall/internal/i18n"
	"github.com/kinstall/kinstall/internal/multicluster"
	"github.com/kinstall/kinstall/internal/obs"
	"github.com/kinstall/kinstall/internal/osadapter"
	"github.com/kinstall/kinstall/internal/pki"
	"github.com/kinstall/kinstall/internal/registry"
	"github.com/kinstall/kinstall/internal/secrets"
	"github.com/kinstall/kinstall/internal/server"
	"github.com/kinstall/kinstall/internal/store"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
	"github.com/kinstall/kinstall/plugins/kafka"
	"github.com/kinstall/kinstall/plugins/objectstorage"
	"github.com/kinstall/kinstall/plugins/opensearch"
	"github.com/kinstall/kinstall/plugins/postgresql"
	"github.com/kinstall/kinstall/plugins/valkey"
)

func setupTestServer(t *testing.T) (http.Handler, *authz.Service, func()) {
	t.Helper()
	ctx := context.Background()
	tmp := t.TempDir()
	logger := obs.NewLogger("json", "error")

	st, err := store.OpenAndMigrate(ctx, store.Config{Driver: "sqlite", DSN: filepath.Join(tmp, "test.db")}, logger)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	keyPath := filepath.Join(tmp, "master.key")
	if err := secrets.GenerateKeyringFile(keyPath); err != nil {
		t.Fatalf("gen key: %v", err)
	}
	kek, err := secrets.NewFileKEKProvider("test-kek", keyPath)
	if err != nil {
		t.Fatalf("kek: %v", err)
	}
	sec := secrets.NewEnvelopeStore(kek, "file", secrets.AlgAES256GCM)
	osEng, err := osadapter.LoadEmbedded()
	if err != nil {
		t.Fatalf("osadapter: %v", err)
	}
	cat, err := i18n.New()
	if err != nil {
		t.Fatalf("i18n: %v", err)
	}
	reg := registry.New()
	for _, c := range []sdk.Component{valkey.New(), postgresql.New(), kafka.New(), opensearch.New(), objectstorage.New()} {
		if err := reg.RegisterComponent(c); err != nil {
			t.Fatalf("register plugin: %v", err)
		}
	}
	reg.Freeze()
	evHub := events.NewHub(st.OperationEvents(), "", logger)
	audRec := audit.NewRecorder(st.Audit(), true)
	authSvc := authz.NewService(st.Auth(), audRec)
	kcMgr := multicluster.NewKubeconfigManager(st.Kubeconfigs(), pki.NewIssuer(), audRec)
	facade := app.NewFacade(st, reg, nil, sec, nil, evHub, audRec, authSvc, nil, osEng, kcMgr, true, "file", secrets.AlgAES256GCM)

	if _, err := authSvc.CreateLocalUser(ctx, "admin1", "Admin One", "Pass#2026!", []domain.RoleBinding{{ProjectID: "*", Role: domain.RoleAdmin}}); err != nil {
		t.Fatalf("create admin1: %v", err)
	}
	if _, err := authSvc.CreateLocalUser(ctx, "admin2", "Admin Two", "Pass#2026!", []domain.RoleBinding{{ProjectID: "*", Role: domain.RoleAdmin}}); err != nil {
		t.Fatalf("create admin2: %v", err)
	}

	router := server.NewRouter(ctx, facade, authSvc, cat, logger, server.Options{})
	return router, authSvc, func() { _ = st.Close() }
}

func doJSON(t *testing.T, h http.Handler, method, path, tok string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		r = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, r)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestFullControlPlaneEndToEnd(t *testing.T) {
	router, _, cleanup := setupTestServer(t)
	defer cleanup()

	// 1. Login as admin1 and admin2
	login := func(u, p string) string {
		rec := doJSON(t, router, "POST", "/api/v1/auth/login", "", map[string]string{"username": u, "password": p})
		if rec.Code != http.StatusOK {
			t.Fatalf("login %s failed: %d %s", u, rec.Code, rec.Body.String())
		}
		var out struct{ Token string }
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return out.Token
	}
	tok1 := login("admin1", "Pass#2026!")
	tok2 := login("admin2", "Pass#2026!")

	// 2. Create project
	rec := doJSON(t, router, "POST", "/api/v1/projects", tok1, map[string]string{"name": "prod-his", "description": "HIS Core"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create project: %d %s", rec.Code, rec.Body.String())
	}
	var prj struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &prj)

	// 3. Credentials vault: reject empty secret, create with "secret" key, list (must redact EnvelopeJSON), rewrap
	rec = doJSON(t, router, "POST", "/api/v1/projects/"+prj.ID+"/credentials", tok1, map[string]string{"name": "empty-bad", "kind": "ssh_password"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty credential should be rejected with 400, got %d %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, router, "POST", "/api/v1/projects/"+prj.ID+"/credentials", tok1, map[string]string{"name": "ssh-root", "kind": "ssh_password", "secret": "TopSecret#99"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create cred: %d %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, router, "GET", "/api/v1/projects/"+prj.ID+"/credentials", tok1, nil)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "TopSecret") {
		t.Fatalf("list creds failed or leaked secret: %d %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, router, "POST", "/api/v1/projects/"+prj.ID+"/secrets:rewrap", tok1, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"rewrapped":1`) {
		t.Fatalf("rewrap failed: %d %s", rec.Code, rec.Body.String())
	}

	// 4. Bulk host import CSV (dryRun + commit) + baseline check + 22-module hostinit
	csvData := "name,address,port,user,zone\nnode-01,10.20.1.11,22,root,az-1\nnode-02,10.20.1.12,22,root,az-2\n"
	rec = doJSON(t, router, "POST", "/api/v1/projects/"+prj.ID+"/hosts:import", tok1, map[string]any{"format": "csv", "data": csvData, "dryRun": false})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"imported":2`) {
		t.Fatalf("hosts import failed: %d %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, router, "GET", "/api/v1/projects/"+prj.ID+"/hosts", tok1, nil)
	var hostList struct{ Items []struct{ ID string } }
	_ = json.Unmarshal(rec.Body.Bytes(), &hostList)
	if len(hostList.Items) != 2 {
		t.Fatalf("expected 2 hosts, got %d", len(hostList.Items))
	}
	hostID := hostList.Items[0].ID
	rec = doJSON(t, router, "POST", "/api/v1/projects/"+prj.ID+"/hosts/"+hostID+":check", tok1, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("host check failed: %d %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, router, "POST", "/api/v1/projects/"+prj.ID+"/hosts/"+hostID+":init", tok1, map[string]any{"profiles": []string{"base", "cache"}})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("host init failed: %d %s", rec.Code, rec.Body.String())
	}
	// Multi-profile baseline check AFTER hosts:init (profile="base,cache") must succeed (no 422)!
	rec = doJSON(t, router, "POST", "/api/v1/projects/"+prj.ID+"/hosts/"+hostID+":check", tok1, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("multi-profile host check failed after init: %d %s", rec.Code, rec.Body.String())
	}
	// External CMDB sync: create new host (resolving default credential!) AND update existing host zone!
	rec = doJSON(t, router, "POST", "/api/v1/projects/"+prj.ID+"/cmdb:sync", tok1, map[string]any{
		"hosts": []map[string]any{
			{"name": "cmdb-sync-01", "address": "10.20.9.11", "faultZone": "az-9"},
			{"name": "node-02", "address": "10.20.1.12", "faultZone": "az-updated"},
		},
	})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"created":1`) || !strings.Contains(rec.Body.String(), `"updated":1`) {
		t.Fatalf("cmdb sync failed: %d %s", rec.Code, rec.Body.String())
	}

	// 5. Plugin ParamSchema lookup + Instance Create + Plan (real rendered valkey.conf 3-Way Diff + DriftIgnore!) + Prod Apply -> Four-Eyes Approval
	rec = doJSON(t, router, "GET", "/api/v1/plugins/valkey", tok1, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "paramSchema") {
		t.Fatalf("plugin schema failed: %d %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, router, "POST", "/api/v1/projects/"+prj.ID+"/instances", tok1, map[string]any{
		"env": "prod", "name": "valkey-prod", "product": "valkey", "productVersion": "9.1.4", "deployTarget": "host-binary",
		"desiredSpec": map[string]any{"topology": "standalone", "port": 6379},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create instance failed: %d %s", rec.Code, rec.Body.String())
	}
	var inst struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &inst)

	rec = doJSON(t, router, "POST", "/api/v1/projects/"+prj.ID+"/instances/"+inst.ID+":plan", tok1, map[string]any{"action": "install"})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "valkey.conf") {
		t.Fatalf("plan failed or did not render real valkey.conf configDiffs: %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "sentinel.conf") {
		t.Fatalf("DriftIgnore O-08 violated: sentinel.conf should be ignored in configDiffs")
	}
	var planResp struct{ Plan struct{ ID string } }
	_ = json.Unmarshal(rec.Body.Bytes(), &planResp)

	rec = doJSON(t, router, "POST", "/api/v1/projects/"+prj.ID+"/instances/"+inst.ID+":apply", tok1, map[string]any{
		"planId": planResp.Plan.ID, "action": "install",
	})
	if rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), "awaiting_approval") {
		t.Fatalf("prod apply should await approval: %d %s", rec.Code, rec.Body.String())
	}
	var opResp struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &opResp)

	// Cross-instance host port collision protection: second Valkey on port 6379 MUST be blocked by persisted instance_host_claims!
	rec = doJSON(t, router, "POST", "/api/v1/projects/"+prj.ID+"/instances", tok1, map[string]any{
		"env": "dev", "name": "valkey-clash", "product": "valkey", "productVersion": "9.1.4", "deployTarget": "host-binary",
		"desiredSpec": map[string]any{"topology": "standalone", "port": 6379},
	})
	var clashInst struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &clashInst)
	rec = doJSON(t, router, "POST", "/api/v1/projects/"+prj.ID+"/instances/"+clashInst.ID+":plan", tok1, map[string]any{"action": "install"})
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), sdk.CodeHostClaimConflict) {
		t.Fatalf("expected KI-HOST-0001 port collision on overlapping Valkey 6379, got %d %s", rec.Code, rec.Body.String())
	}
	_ = doJSON(t, router, "DELETE", "/api/v1/projects/"+prj.ID+"/instances/"+clashInst.ID, tok1, nil)

	// PostgreSQL PITR targetTime parameter pass-through into Plan pipeline!
	rec = doJSON(t, router, "POST", "/api/v1/projects/"+prj.ID+"/instances", tok1, map[string]any{
		"env": "dev", "name": "pg-pitr", "product": "postgresql", "productVersion": "18.7.0", "deployTarget": "host-binary",
		"desiredSpec": map[string]any{"topology": "patroni-ha", "port": 5432},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create pg instance failed: %d %s", rec.Code, rec.Body.String())
	}
	var pgInst struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &pgInst)
	rec = doJSON(t, router, "POST", "/api/v1/projects/"+prj.ID+"/instances/"+pgInst.ID+":plan", tok1, map[string]any{
		"action": "restore", "params": map[string]any{"targetTime": "2026-11-27T12:34:56Z"},
	})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "2026-11-27T12:34:56Z") {
		t.Fatalf("pg PITR targetTime not baked into plan pipeline: %d %s", rec.Code, rec.Body.String())
	}
	_ = doJSON(t, router, "DELETE", "/api/v1/projects/"+prj.ID+"/instances/"+pgInst.ID, tok1, nil)

	// Self-approval by admin1 MUST be rejected (ADR-037)!
	rec = doJSON(t, router, "POST", "/api/v1/projects/"+prj.ID+"/operations/"+opResp.ID+":approve", tok1, map[string]string{"note": "self"})
	if rec.Code == http.StatusOK {
		t.Fatalf("self-approval must be blocked!")
	}
	// Peer approval by admin2 MUST succeed!
	rec = doJSON(t, router, "POST", "/api/v1/projects/"+prj.ID+"/operations/"+opResp.ID+":approve", tok2, map[string]string{"note": "peer ok"})
	if rec.Code != http.StatusOK {
		t.Fatalf("peer approval failed: %d %s", rec.Code, rec.Body.String())
	}

	// 6. Stack live validation + create + apply + reverse uninstall
	stackYAML := "apiVersion: kinstall.io/v1alpha1\nkind: Stack\nmetadata:\n  name: logging\nspec:\n  instances:\n    - id: s3\n      product: object-storage\n      productVersion: \"4.54.0\"\n      deployTarget: host-binary\n    - id: os\n      product: opensearch\n      productVersion: \"3.10.0\"\n      deployTarget: host-binary\n      dependsOn: [s3]\n"
	rec = doJSON(t, router, "POST", "/api/v1/projects/"+prj.ID+"/stacks:validate", tok1, map[string]string{"specYaml": stackYAML})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"valid":true`) || !strings.Contains(rec.Body.String(), `"nodes"`) {
		t.Fatalf("stack validate failed: %d %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, router, "POST", "/api/v1/projects/"+prj.ID+"/stacks", tok1, map[string]string{"env": "dev", "name": "logging", "specYaml": stackYAML})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create stack failed: %d %s", rec.Code, rec.Body.String())
	}
	var stResp struct{ Stack struct{ ID string } }
	_ = json.Unmarshal(rec.Body.Bytes(), &stResp)
	rec = doJSON(t, router, "POST", "/api/v1/projects/"+prj.ID+"/stacks/"+stResp.Stack.ID+":apply", tok1, nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("apply stack failed: %d %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, router, "POST", "/api/v1/projects/"+prj.ID+"/stacks/"+stResp.Stack.ID+":uninstall", tok1, nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("uninstall stack failed: %d %s", rec.Code, rec.Body.String())
	}

	// 7. Notifications, GitOps export/diff, Support Bundle, Compliance, Users, API Tokens, Audit verify
	rec = doJSON(t, router, "POST", "/api/v1/projects/"+prj.ID+"/notification-channels", tok1, map[string]any{
		"name": "ops-hook", "kind": "webhook", "enabled": true, "events": []string{"approval.requested"}, "config": map[string]string{"url": "http://127.0.0.1:9/noop"},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create notification channel failed: %d %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, router, "GET", "/api/v1/projects/"+prj.ID+"/notification-channels", tok1, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "ops-hook") {
		t.Fatalf("list notification channels failed: %d %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, router, "GET", "/api/v1/projects/"+prj.ID+"/gitops/export", tok1, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "valkey-prod") {
		t.Fatalf("gitops export failed: %d %s", rec.Code, rec.Body.String())
	}
	gitopsYAML := rec.Body.String()
	rec = doJSON(t, router, "POST", "/api/v1/projects/"+prj.ID+"/gitops:diff", tok1, map[string]string{"desiredYaml": gitopsYAML})
	if rec.Code != http.StatusOK {
		t.Fatalf("gitops diff failed: %d %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, router, "POST", "/api/v1/projects/"+prj.ID+"/support-bundle", tok1, nil)
	if rec.Code != http.StatusOK || rec.Body.Len() < 30 {
		t.Fatalf("support bundle failed: %d", rec.Code)
	}
	rec = doJSON(t, router, "GET", "/api/v1/projects/"+prj.ID+"/compliance:download", tok1, nil)
	if rec.Code != http.StatusOK || rec.Header().Get("X-Evidence-SHA256") == "" {
		t.Fatalf("compliance download failed: %d", rec.Code)
	}
	rec = doJSON(t, router, "GET", "/api/v1/users", tok1, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "admin1") {
		t.Fatalf("list users failed: %d %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, router, "POST", "/api/v1/auth/tokens", tok1, map[string]any{"name": "ci-bot", "scopes": []string{"*"}, "ttlDays": 30})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create api token failed: %d %s", rec.Code, rec.Body.String())
	}
	var tokResp struct {
		Record struct{ ID string } `json:"record"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &tokResp)
	rec = doJSON(t, router, "GET", "/api/v1/auth/tokens", tok1, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "ci-bot") {
		t.Fatalf("list api tokens failed: %d %s", rec.Code, rec.Body.String())
	}
	if tokResp.Record.ID != "" {
		rec = doJSON(t, router, "DELETE", "/api/v1/auth/tokens/"+tokResp.Record.ID, tok1, nil)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("revoke api token failed: %d %s", rec.Code, rec.Body.String())
		}
	}
	rec = doJSON(t, router, "DELETE", "/api/v1/projects/"+prj.ID+"/instances/"+inst.ID, tok1, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete instance failed: %d %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, router, "DELETE", "/api/v1/projects/"+prj.ID+"/hosts/"+hostID, tok1, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete host failed: %d %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, router, "POST", "/api/v1/audit:verify", tok1, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"valid":true`) {
		t.Fatalf("audit verify failed: %d %s", rec.Code, rec.Body.String())
	}
}

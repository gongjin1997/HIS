package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gssh "github.com/gliderlabs/ssh"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/kinstall/kinstall/internal/catalog"
	"github.com/kinstall/kinstall/internal/cmdb"
	"github.com/kinstall/kinstall/internal/config"
	"github.com/kinstall/kinstall/internal/delivery"
	"github.com/kinstall/kinstall/internal/execplane"
	"github.com/kinstall/kinstall/internal/iam"
	"github.com/kinstall/kinstall/internal/server"
	"github.com/kinstall/kinstall/migrations"
	"github.com/kinstall/kinstall/pkg/platform/auth"
	"github.com/kinstall/kinstall/pkg/platform/crypto"
	"github.com/kinstall/kinstall/pkg/platform/db"
	"github.com/kinstall/kinstall/pkg/platform/eventbus"
	"github.com/xuri/excelize/v2"
)

type testHarness struct {
	ts      *httptest.Server
	token   string
	wsID    string
	dataDir string
}

func newHarness(t *testing.T) *testHarness {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	codec, err := crypto.LoadOrCreateKeyring(filepath.Join(dir, "keyring.json"))
	if err != nil {
		t.Fatalf("keyring: %v", err)
	}
	store, err := db.Open(ctx, db.Config{Driver: "sqlite", DSN: filepath.Join(dir, "kinstall.db")})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	mfs, _ := migrations.ForDialect(store.Dialect())
	if err := store.Migrate(ctx, mfs); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	authMgr := auth.NewManager(codec.SigningKey("jwt"), time.Hour)
	broker := eventbus.NewLogBroker(500)
	iamSvc := iam.NewService(store, codec, authMgr)
	wsID, err := iamSvc.Bootstrap(ctx)
	if err != nil {
		t.Fatalf("iam bootstrap: %v", err)
	}
	cmdbSvc := cmdb.NewService(store)
	if err := cmdbSvc.EnsureSeeded(ctx, wsID); err != nil {
		t.Fatalf("seed cmdb: %v", err)
	}
	cfgSvc := config.NewService(store, codec, iamSvc)
	if err := cfgSvc.EnsureSeeded(ctx, wsID); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	catSvc := catalog.NewService(store, codec, iamSvc, dir)
	if err := catSvc.EnsureSeeded(ctx, wsID); err != nil {
		t.Fatalf("seed catalog: %v", err)
	}
	execSvc := execplane.NewService(store, cmdbSvc, iamSvc, catSvc, broker)
	delSvc := delivery.NewService(store, cmdbSvc, execSvc, broker)
	if err := delSvc.EnsureSeededPlugins(ctx, wsID); err != nil {
		t.Fatalf("seed plugins: %v", err)
	}

	srv := server.New(server.Config{
		Version:     "2.0.0-test",
		DefaultWSID: wsID,
		Logger:      slog.Default(),
		AuthMgr:     authMgr,
		Broker:      broker,
		IAM:         iamSvc,
		CMDB:        cmdbSvc,
		ConfigSvc:   cfgSvc,
		Catalog:     catSvc,
		Exec:        execSvc,
		Delivery:    delSvc,
		PingDB:      store.Ping,
	})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	h := &testHarness{ts: ts, wsID: wsID, dataDir: dir}
	// Login as bootstrap admin
	resp := h.doJSON(t, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": iam.DefaultAdminUser,
		"password": iam.DefaultAdminPassword,
	}, http.StatusOK)
	h.token = resp["token"].(string)
	return h
}

func (h *testHarness) doJSON(t *testing.T, method, path string, body any, wantStatus int) map[string]any {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, h.ts.URL+path, r)
	if err != nil {
		t.Fatalf("new req: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if h.token != "" {
		req.Header.Set("Authorization", "Bearer "+h.token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != wantStatus {
		t.Fatalf("%s %s: got HTTP %d want %d, body=%s", method, path, resp.StatusCode, wantStatus, string(raw))
	}
	out := make(map[string]any)
	_ = json.Unmarshal(raw, &out)
	return out
}

func TestEndToEndCMDBExcelDCIMIPAMConfigCatalog(t *testing.T) {
	h := newHarness(t)

	// 1. Overview & RBAC enforcement check
	ov := h.doJSON(t, http.MethodGet, "/api/v1/overview", nil, http.StatusOK)
	if ov["totalModels"].(float64) < 11 || ov["totalPlugins"].(float64) < 10 {
		t.Fatalf("unexpected seeded overview: %+v", ov)
	}

	// Create viewer user & verify default-deny RBAC blocks write
	h.doJSON(t, http.MethodPost, "/api/v1/iam/users", map[string]any{
		"username": "readonly1", "displayName": "Viewer", "role": "viewer", "password": "Viewer#2026!",
	}, http.StatusCreated)
	viewerLogin := h.doJSON(t, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": "readonly1", "password": "Viewer#2026!",
	}, http.StatusOK)
	vReq, _ := http.NewRequest(http.MethodPost, h.ts.URL+"/api/v1/cmdb/models", strings.NewReader(`{"code":"m_evil","name":"evil"}`))
	vReq.Header.Set("Authorization", "Bearer "+viewerLogin["token"].(string))
	vResp, _ := http.DefaultClient.Do(vReq)
	if vResp.StatusCode != http.StatusForbidden {
		t.Fatalf("viewer must get 403 on cmdb:write, got %d", vResp.StatusCode)
	}

	// 2. Native Excelize .xlsx template + round-trip batch import
	tplReq, _ := http.NewRequest(http.MethodGet, h.ts.URL+"/api/v1/cmdb/models/m_host/excel/template", nil)
	tplReq.Header.Set("Authorization", "Bearer "+h.token)
	tplResp, err := http.DefaultClient.Do(tplReq)
	if err != nil || tplResp.StatusCode != http.StatusOK {
		t.Fatalf("excel template download failed: %v", err)
	}
	tplBytes, _ := io.ReadAll(tplResp.Body)
	_ = tplResp.Body.Close()

	ef, err := excelize.OpenReader(bytes.NewReader(tplBytes))
	if err != nil {
		t.Fatalf("parse downloaded template xlsx: %v", err)
	}
	sheet := ef.GetSheetName(0)
	_ = ef.SetSheetRow(sheet, "A3", &[]any{"10.20.10.99", "excel-host-99", "uat", "active", "SRE", "imported", "22", "root", "auto", "10.20.10.99", "prod-core", "Ubuntu 24.04", "amd64", "8", "32", "SN-99"})
	var uploadXLSX bytes.Buffer
	_ = ef.Write(&uploadXLSX)
	_ = ef.Close()

	var mpBuf bytes.Buffer
	mw := multipart.NewWriter(&mpBuf)
	fw, _ := mw.CreateFormFile("file", "hosts.xlsx")
	_, _ = fw.Write(uploadXLSX.Bytes())
	_ = mw.Close()
	impReq, _ := http.NewRequest(http.MethodPost, h.ts.URL+"/api/v1/cmdb/models/m_host/excel/import", &mpBuf)
	impReq.Header.Set("Authorization", "Bearer "+h.token)
	impReq.Header.Set("Content-Type", mw.FormDataContentType())
	impResp, err := http.DefaultClient.Do(impReq)
	if err != nil || impResp.StatusCode != http.StatusOK {
		t.Fatalf("excel import failed: %v", err)
	}
	var impRes cmdb.ImportResult
	_ = json.NewDecoder(impResp.Body).Decode(&impRes)
	_ = impResp.Body.Close()
	if impRes.Created != 1 || impRes.Failed != 0 {
		t.Fatalf("unexpected excel import result: %+v", impRes)
	}

	// 3. DCIM 42U Rack U-overlap protection & IPAM CIDR auto-allocator
	racks := h.doJSON(t, http.MethodGet, "/api/v1/cmdb/dcim/racks", nil, http.StatusOK)["items"].([]any)
	rackID := racks[0].(map[string]any)["id"].(string)
	// Existing seed has device at 28U-29U -> mounting 29U-30U must return 409 conflict!
	h.doJSON(t, http.MethodPost, "/api/v1/cmdb/dcim/racks/"+rackID+"/units", map[string]any{
		"deviceName": "overlap-srv", "uStart": 29, "uSize": 2, "face": "front",
	}, http.StatusConflict)
	// Mounting free slot 10U-11U must succeed!
	h.doJSON(t, http.MethodPost, "/api/v1/cmdb/dcim/racks/"+rackID+"/units", map[string]any{
		"deviceName": "free-slot-srv", "uStart": 10, "uSize": 2, "face": "front", "powerW": 450,
	}, http.StatusCreated)

	pfxs := h.doJSON(t, http.MethodGet, "/api/v1/cmdb/ipam/prefixes", nil, http.StatusOK)["items"].([]any)
	pfxID := pfxs[0].(map[string]any)["id"].(string)
	alloc := h.doJSON(t, http.MethodPost, "/api/v1/cmdb/ipam/prefixes/"+pfxID+"/allocate", map[string]any{
		"dnsName": "auto-alloc.his.local", "owner": "SRE",
	}, http.StatusCreated)
	if !strings.HasPrefix(alloc["ip"].(string), "10.20.10.") {
		t.Fatalf("unexpected auto-allocated IP: %+v", alloc)
	}

	// 4. Multi-Env Config Center: KMS mask -> audited reveal -> diff -> publish -> rollback -> safe cross-env clone
	nss := h.doJSON(t, http.MethodGet, "/api/v1/config/namespaces?env=prod", nil, http.StatusOK)["items"].([]any)
	prodNSID := nss[0].(map[string]any)["id"].(string)
	items := h.doJSON(t, http.MethodGet, "/api/v1/config/namespaces/"+prodNSID+"/items", nil, http.StatusOK)["items"].([]any)
	foundMasked := false
	for _, rawIt := range items {
		it := rawIt.(map[string]any)
		if it["key"] == "DB_PASSWORD" && it["value"] == config.MaskedValue {
			foundMasked = true
		}
	}
	if !foundMasked {
		t.Fatal("DB_PASSWORD must be masked in list response")
	}
	rev := h.doJSON(t, http.MethodPost, "/api/v1/config/namespaces/"+prodNSID+"/reveal", map[string]string{
		"key": "DB_PASSWORD", "reason": "automated verification test",
	}, http.StatusOK)
	if rev["value"] != "HisProd#Pg2026!Secure" {
		t.Fatalf("unexpected revealed secret: %v", rev["value"])
	}
	// Modify item -> publish v2 -> rollback to v1
	h.doJSON(t, http.MethodPost, "/api/v1/config/namespaces/"+prodNSID+"/items", map[string]any{
		"key": "JVM_XMX", "value": "8192m", "isSecret": false,
	}, http.StatusOK)
	h.doJSON(t, http.MethodPost, "/api/v1/config/namespaces/"+prodNSID+"/publish", map[string]string{"comment": "bump heap"}, http.StatusCreated)
	rb := h.doJSON(t, http.MethodPost, "/api/v1/config/namespaces/"+prodNSID+"/rollback", map[string]int{"targetVersion": 1}, http.StatusOK)
	if rb["version"].(float64) != 3 {
		t.Fatalf("expected monotonic rollback release v3, got %v", rb["version"])
	}
	cloneRes := h.doJSON(t, http.MethodPost, "/api/v1/config/namespaces/"+prodNSID+"/clone", map[string]string{
		"targetEnv": "sit", "targetApp": "his-core", "targetName": "application",
	}, http.StatusCreated)
	if cloneRes["blankedSecrets"].(float64) < 2 {
		t.Fatalf("expected cross-env clone to blank secrets, got %+v", cloneRes)
	}

	// 5. CAS Artifact Dedup + Ed25519 Verify + Pure-Go OCI Arch-Mismatch Fail-Closed!
	img, _ := mutate.ConfigFile(empty.Image, &v1.ConfigFile{Architecture: "amd64", OS: "linux"})
	ociPath := filepath.Join(t.TempDir(), "app-amd64.tar")
	ref, _ := name.ParseReference("his.local/demo:v1")
	if err := tarball.WriteToFile(ociPath, ref, img); err != nil {
		t.Fatalf("create oci tar: %v", err)
	}
	ociRaw, _ := os.ReadFile(ociPath)

	var oBuf bytes.Buffer
	omw := multipart.NewWriter(&oBuf)
	_ = omw.WriteField("name", "his-demo-amd64.tar")
	_ = omw.WriteField("version", "1.0.0")
	_ = omw.WriteField("kind", "oci_tar")
	_ = omw.WriteField("arch", "amd64")
	ofw, _ := omw.CreateFormFile("file", "his-demo-amd64.tar")
	_, _ = ofw.Write(ociRaw)
	_ = omw.Close()
	upReq, _ := http.NewRequest(http.MethodPost, h.ts.URL+"/api/v1/catalog/artifacts", &oBuf)
	upReq.Header.Set("Authorization", "Bearer "+h.token)
	upReq.Header.Set("Content-Type", omw.FormDataContentType())
	upResp, _ := http.DefaultClient.Do(upReq)
	var upArt catalog.Artifact
	_ = json.NewDecoder(upResp.Body).Decode(&upArt)
	_ = upResp.Body.Close()

	ver := h.doJSON(t, http.MethodPost, "/api/v1/catalog/artifacts/"+upArt.ID+"/verify", nil, http.StatusOK)
	if !ver["verified"].(bool) {
		t.Fatalf("expected Ed25519 verify true: %+v", ver)
	}
	// Pushing amd64 OCI tar with expectedArch=arm64 MUST fail-closed with 412 ARCH_MISMATCH!
	h.doJSON(t, http.MethodPost, "/api/v1/catalog/artifacts/"+upArt.ID+"/oci-push", map[string]any{
		"targetRef": "127.0.0.1:5000/his/demo:v1", "expectedArch": "arm64", "insecureTls": true,
	}, http.StatusPreconditionFailed)

	// 6. Audit Hash Chain integrity must remain 100% valid across all operations!
	verifyAudit := h.doJSON(t, http.MethodPost, "/api/v1/audit/verify", nil, http.StatusOK)
	if !verifyAudit["valid"].(bool) || verifyAudit["totalCount"].(float64) < 5 {
		t.Fatalf("audit hash chain invalid: %+v", verifyAudit)
	}
}

func TestEndToEndRealSSHDeliveryAndOutboundAgent(t *testing.T) {
	h := newHarness(t)

	// Start real in-process SSH server (gliderlabs/ssh) for real SSH deployment test!
	sshLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen ssh: %v", err)
	}
	sshPort := sshLn.Addr().(*net.TCPAddr).Port
	sshSrv := &gssh.Server{
		Handler: func(s gssh.Session) {
			_, _ = io.WriteString(s, "REAL_SSH_EXECUTED: "+s.RawCommand()+"\n")
			_ = s.Exit(0)
		},
		PasswordHandler: func(ctx gssh.Context, password string) bool {
			return password == "test-ssh-pass"
		},
	}
	go func() { _ = sshSrv.Serve(sshLn) }()
	t.Cleanup(func() { _ = sshSrv.Close() })

	// Start local HTTP health target for real DIALING_TEST!
	dialSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"healthy":true}`))
	}))
	t.Cleanup(dialSrv.Close)

	// Create encrypted SSH credential & Unified Host-as-CI pointing to our live SSH server!
	cred := h.doJSON(t, http.MethodPost, "/api/v1/iam/credentials", map[string]any{
		"name": "e2e-ssh-cred", "kind": "ssh_password", "username": "root", "secret": "test-ssh-pass",
	}, http.StatusCreated)
	hostCI := h.doJSON(t, http.MethodPost, "/api/v1/cmdb/cis", map[string]any{
		"modelCode": "m_host",
		"uniqueKey": "127.0.0.1",
		"name":      "e2e-ssh-target-01",
		"env":       "prod",
		"data":      map[string]any{"ip": "127.0.0.1", "zone": "prod-core"},
		"host": map[string]any{
			"ip": "127.0.0.1", "sshPort": sshPort, "sshUser": "root",
			"credentialId": cred["id"].(string), "execMode": "ssh",
		},
	}, http.StatusCreated)
	hostID := hostCI["id"].(string)

	// Verify Kahn DAG cycle detection rejects cyclic graph!
	h.doJSON(t, http.MethodPost, "/api/v1/delivery/releases", map[string]any{
		"name": "cyclic-release", "env": "prod", "mode": "DAG",
		"items": []map[string]any{
			{"nodeKey": "a", "pluginCode": "postgresql", "serviceName": "a", "dependsOn": []string{"b"}},
			{"nodeKey": "b", "pluginCode": "valkey", "serviceName": "b", "dependsOn": []string{"a"}},
		},
	}, http.StatusBadRequest)

	// Create 2-node DAG release with pauseAfter on wave 1 + real SSH execution + real HTTP dial check!
	rel := h.doJSON(t, http.MethodPost, "/api/v1/delivery/releases", map[string]any{
		"name": "HIS Core Real SSH Release",
		"env":  "prod",
		"mode": "DAG",
		"globalParams": map[string]string{
			"CPU_CORES": "2", "MEM_GB": "4",
		},
		"items": []map[string]any{
			{
				"nodeKey": "pg-primary", "pluginCode": "postgresql", "serviceName": "his-pg-live",
				"targetHostCiId": hostID, "batchIndex": 0, "pauseAfter": true,
				"dialUrl": dialSrv.URL,
			},
			{
				"nodeKey": "his-app", "pluginCode": "his-java-app", "serviceName": "his-app-live",
				"targetHostCiId": hostID, "batchIndex": 1, "dependsOn": []string{"pg-primary"},
				"dialUrl": dialSrv.URL,
			},
		},
	}, http.StatusCreated)
	relID := rel["id"].(string)

	// Execute wave 1 -> MUST stop cleanly at PAUSED!
	exec1 := h.doJSON(t, http.MethodPost, "/api/v1/delivery/releases/"+relID+"/execute", nil, http.StatusOK)
	if exec1["status"] != "PAUSED" {
		t.Fatalf("expected PAUSED after pauseAfter breakpoint, got %+v", exec1["status"])
	}
	// Resume -> MUST complete wave 2 and reach SUCCEEDED + auto-sync CIs into CMDB!
	exec2 := h.doJSON(t, http.MethodPost, "/api/v1/delivery/releases/"+relID+"/execute", nil, http.StatusOK)
	if exec2["status"] != "SUCCEEDED" {
		t.Fatalf("expected SUCCEEDED after resume, got %+v", exec2)
	}
	items := exec2["items"].([]any)
	syncedCI := items[0].(map[string]any)["syncedCiId"].(string)
	if syncedCI == "" {
		t.Fatal("expected deployed database to auto-sync CI in CMDB")
	}
	// Verify runs_on lineage edge exists from syncedCI -> hostID!
	lineage := h.doJSON(t, http.MethodGet, "/api/v1/cmdb/cis/"+syncedCI+"/lineage?depth=2", nil, http.StatusOK)
	if len(lineage["edges"].([]any)) == 0 {
		t.Fatalf("expected runs_on lineage edge for auto-synced CI %s", syncedCI)
	}
	// Verify Rollback transitions cleanly to ROLLED_BACK!
	rb := h.doJSON(t, http.MethodPost, "/api/v1/delivery/releases/"+relID+"/rollback", nil, http.StatusOK)
	if rb["status"] != "ROLLED_BACK" {
		t.Fatalf("expected ROLLED_BACK, got %v", rb["status"])
	}

	// =========================================================================
	// Real Outbound Zero-Trust Go Agent Live Enrollment + Delegated Execution!
	// =========================================================================
	enr := h.doJSON(t, http.MethodPost, "/api/v1/ops/enroll-tokens", map[string]any{
		"label": "e2e-edge-agent", "zone": "prod-core", "ttlMin": 15,
	}, http.StatusCreated)

	agentCtx, cancelAgent := context.WithCancel(context.Background())
	agentDone := make(chan error, 1)
	go func() {
		agentDone <- execplane.RunEdgeAgent(agentCtx, execplane.EdgeAgentConfig{
			ServerURL:    h.ts.URL,
			EnrollToken:  enr["plainToken"].(string),
			Zone:         "prod-core",
			Hostname:     "live-edge-agent-01",
			IdentityFile: filepath.Join(t.TempDir(), "agent.json"),
			Version:      "2.0.0-test",
		}, slog.Default())
	}()

	// Wait up to 5s for outbound agent to enroll and heartbeat ONLINE.
	var agentID, agentHostCIID string
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		list := h.doJSON(t, http.MethodGet, "/api/v1/ops/agents", nil, http.StatusOK)["items"].([]any)
		if len(list) > 0 {
			a := list[0].(map[string]any)
			if a["status"] == "ONLINE" && a["hostCiId"].(string) != "" {
				agentID = a["id"].(string)
				agentHostCIID = a["hostCiId"].(string)
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if agentID == "" {
		cancelAgent()
		t.Fatal("outbound edge agent did not come ONLINE within 5s")
	}

	// Delegate CIDR TCP Probe directly to our live Edge Agent targeting our live HTTP server port!
	dialPort := dialSrv.Listener.Addr().(*net.TCPAddr).Port
	probeRes := h.doJSON(t, http.MethodPost, "/api/v1/ops/tcp-probe", map[string]any{
		"cidr":      "127.0.0.1/32",
		"ports":     []int{dialPort},
		"agentId":   agentID,
		"timeoutMs": 1000,
	}, http.StatusOK)
	if probeRes["executor"] != "agent:"+agentID || len(probeRes["items"].([]any)) != 1 {
		t.Fatalf("expected delegated agent TCP probe to discover port %d, got %+v", dialPort, probeRes)
	}

	// Trigger whitelist host fact collection via the live Agent!
	colRes := h.doJSON(t, http.MethodPost, "/api/v1/cmdb/cis/"+agentHostCIID+"/collect", nil, http.StatusOK)
	hostObj := colRes["host"].(map[string]any)
	if hostObj["cpuCores"].(float64) < 1 {
		t.Fatalf("expected whitelist collector via agent to populate cpuCores, got %+v", hostObj)
	}

	// Revoke agent and stop cleanly.
	h.doJSON(t, http.MethodPost, "/api/v1/ops/agents/"+agentID+"/revoke", nil, http.StatusOK)
	cancelAgent()
	<-agentDone
}

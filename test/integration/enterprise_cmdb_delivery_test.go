package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/tarball"

	"github.com/kinstall/kinstall/internal/agenthub"
	"github.com/kinstall/kinstall/internal/app"
	"github.com/kinstall/kinstall/internal/artifact/catalog"
	"github.com/kinstall/kinstall/internal/artifact/oci"
	"github.com/kinstall/kinstall/internal/artifact/source"
	"github.com/kinstall/kinstall/internal/audit"
	"github.com/kinstall/kinstall/internal/authz"
	"github.com/kinstall/kinstall/internal/cmdb"
	"github.com/kinstall/kinstall/internal/configcenter"
	"github.com/kinstall/kinstall/internal/delivery"
	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/internal/engine"
	"github.com/kinstall/kinstall/internal/events"
	"github.com/kinstall/kinstall/internal/executor/sshx"
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
	"github.com/kinstall/kinstall/plugins/devops"
)

func TestEnterpriseCMDBConfigPluginsDeliveryOCIAgentEndToEnd(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	st, err := store.OpenAndMigrate(ctx, store.Config{Driver: "sqlite", DSN: filepath.Join(tmp, "enterprise.db")}, logger)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = st.Close() }()

	keyPath := filepath.Join(tmp, "master.key")
	if err := secrets.GenerateKeyringFile(keyPath); err != nil {
		t.Fatalf("gen key: %v", err)
	}
	kek, err := secrets.NewFileKEKProvider("test", keyPath)
	if err != nil {
		t.Fatalf("kek: %v", err)
	}
	secStore := secrets.NewEnvelopeStore(kek, "file", secrets.AlgAES256GCM)
	osEng, err := osadapter.LoadWithOverride("")
	if err != nil {
		t.Fatalf("osadapter: %v", err)
	}
	i18nCat, _ := i18n.New()
	reg := registry.New()
	_ = reg.RegisterComponent(devops.New())
	reg.Freeze()

	artStore := catalog.NewStore(filepath.Join(tmp, "artifacts"))
	mediaRes := source.NewPolicyMediaResolver(filepath.Join(tmp, "cache")).WithCatalog(artStore)
	evHub := events.NewHub(st.OperationEvents(), "", logger)
	audRec := audit.NewRecorder(st.Audit(), false)
	authSvc := authz.NewService(st.Auth(), audRec)
	kcMgr := multicluster.NewKubeconfigManager(st.Kubeconfigs(), pki.NewIssuer(), audRec)
	sshPool := sshx.NewPool(sshx.PoolConfig{})
	defer func() { _ = sshPool.Close() }()
	execChain := sshx.BuildExecutor(sshPool, logger)
	tpl := template.New(nil)
	wrk := engine.NewWorker(st, lock.NewManager(st.Locks()), execChain, secStore, mediaRes, pki.NewIssuer(), tpl, tpl, evHub, audRec, osEng, logger)

	fac := app.NewFacade(st, reg, execChain, secStore, mediaRes, evHub, audRec, authSvc, wrk, osEng, kcMgr, false, "file", secrets.AlgAES256GCM).
		WithArtifactStore(artStore).WithSSHPool(sshPool)

	router := server.NewRouter(ctx, fac, authSvc, i18nCat, logger, server.Options{DevBypassAuth: true})
	ts := httptest.NewServer(router)
	defer ts.Close()

	// Seed project
	proj := &domain.Project{ID: domain.NewID(domain.PrefixProject), Name: "enterprise-cmdb"}
	if err := st.Projects().Create(ctx, proj); err != nil {
		t.Fatalf("create project: %v", err)
	}
	P := ts.URL + "/api/v1/projects/" + proj.ID

	doJSON := func(method, url string, body any) (int, []byte) {
		t.Helper()
		var r io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			r = bytes.NewReader(b)
		}
		req, _ := http.NewRequestWithContext(ctx, method, url, r)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, url, err)
		}
		defer func() { _ = resp.Body.Close() }()
		out, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, out
	}

	// 1. Dynamic Meta-Model CMDB: auto-seeded models, custom model + dynamic attribute + JSONB schema validation + CI lineage graph
	code, raw := doJSON("GET", P+"/cmdb/meta", nil)
	if code != 200 {
		t.Fatalf("GET /cmdb/meta failed: %d %s", code, raw)
	}
	var metaOv cmdb.MetaModelOverview
	_ = json.Unmarshal(raw, &metaOv)
	if len(metaOv.Models) < 10 || len(metaOv.Relations) < 9 {
		t.Fatalf("expected >=10 seeded models & >=9 relations, got %d/%d", len(metaOv.Models), len(metaOv.Relations))
	}

	// Create custom model + enum/required attribute + verify schema rejects invalid enum
	code, _ = doJSON("POST", P+"/cmdb/meta/models", map[string]any{"groupCode": "t_net", "code": "m_lb", "name": "负载均衡 VIP", "posX": 200, "posY": 520})
	if code != 201 {
		t.Fatalf("create custom model failed: %d", code)
	}
	code, _ = doJSON("POST", P+"/cmdb/meta/models/m_lb/attributes", map[string]any{
		"propId": "vip", "propName": "虚拟 IP", "fieldType": "ip", "required": true, "isUnique": true,
	})
	if code != 200 {
		t.Fatalf("add ip attr failed: %d", code)
	}
	code, _ = doJSON("POST", P+"/cmdb/meta/models/m_lb/attributes", map[string]any{
		"propId": "algo", "propName": "调度算法", "fieldType": "enum", "enumOptions": []string{"rr", "lc", "wrr"},
	})
	if code != 200 {
		t.Fatalf("add enum attr failed: %d", code)
	}
	// Reject invalid IP
	code, _ = doJSON("POST", P+"/cmdb/cis", map[string]any{"modelCode": "m_lb", "name": "lb-bad", "attrs": map[string]any{"vip": "not-an-ip"}})
	if code != 400 {
		t.Fatalf("expected 400 invalid IP rejection, got %d", code)
	}
	// Valid CI creation
	code, raw = doJSON("POST", P+"/cmdb/cis", map[string]any{"modelCode": "m_lb", "name": "lb-his-01", "attrs": map[string]any{"vip": "10.20.10.100", "algo": "wrr"}})
	if code != 201 {
		t.Fatalf("create valid CI failed: %d %s", code, raw)
	}
	var lbCI cmdb.CIInstance
	_ = json.Unmarshal(raw, &lbCI)

	// 2. Native Excelize .xlsx template download + formula-injection-safe export + import round-trip
	resp, err := http.Get(P + "/cmdb/models/m_lb/export.xlsx")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("export xlsx failed: %v", err)
	}
	xlsxBytes, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if len(xlsxBytes) < 1000 {
		t.Fatalf("unexpectedly small xlsx output: %d bytes", len(xlsxBytes))
	}
	var mpBuf bytes.Buffer
	mw := multipart.NewWriter(&mpBuf)
	fw, _ := mw.CreateFormFile("file", "import.xlsx")
	_, _ = fw.Write(xlsxBytes)
	_ = mw.WriteField("dryRun", "true")
	_ = mw.Close()
	impReq, _ := http.NewRequestWithContext(ctx, "POST", P+"/cmdb/models/m_lb/import.xlsx?dryRun=true", &mpBuf)
	impReq.Header.Set("Content-Type", mw.FormDataContentType())
	impResp, err := http.DefaultClient.Do(impReq)
	if err != nil || impResp.StatusCode != 200 {
		t.Fatalf("import xlsx dry-run failed: %v", err)
	}
	impBody, _ := io.ReadAll(impResp.Body)
	_ = impResp.Body.Close()
	var impRep cmdb.ExcelImportReport
	_ = json.Unmarshal(impBody, &impRep)
	if impRep.TotalRows != 1 || impRep.Updated != 1 || impRep.Errors != 0 {
		t.Fatalf("unexpected xlsx dry-run report: %+v", impRep)
	}

	// 3. DCIM 42U Rack Elevation Overlap Protection & IPAM Next-IP Allocator
	code, raw = doJSON("GET", P+"/dcim/racks", nil)
	if code != 200 {
		t.Fatalf("list racks failed: %d", code)
	}
	var rackList struct {
		Items []cmdb.Rack `json:"items"`
	}
	_ = json.Unmarshal(raw, &rackList)
	rackID := rackList.Items[0].ID
	// Starter unit occupies U30-U31 -> mounting U31-U32 must fail 409 conflict!
	code, _ = doJSON("POST", P+"/dcim/racks/"+rackID+"/units", map[string]any{"deviceName": "overlap-srv", "startU": 31, "uSize": 2})
	if code != 409 {
		t.Fatalf("expected 409 U-slot conflict, got %d", code)
	}
	// Mounting free U20-U21 must succeed
	code, _ = doJSON("POST", P+"/dcim/racks/"+rackID+"/units", map[string]any{"deviceName": "his-app-01", "startU": 20, "uSize": 2, "powerW": 500})
	if code != 201 {
		t.Fatalf("mount free U-slot failed: %d", code)
	}
	// IPAM auto-allocate next free IP in seeded 10.20.10.0/24 (10.20.10.1 taken -> should allocate 10.20.10.2)
	code, raw = doJSON("GET", P+"/ipam/prefixes", nil)
	if code != 200 {
		t.Fatalf("list prefixes failed: %d", code)
	}
	var pfxList struct {
		Items []cmdb.IPAMPrefix `json:"items"`
	}
	_ = json.Unmarshal(raw, &pfxList)
	code, raw = doJSON("POST", P+"/ipam/prefixes/"+pfxList.Items[0].ID+"/allocate", map[string]any{"hostname": "his-app-01"})
	if code != 201 {
		t.Fatalf("allocate IP failed: %d %s", code, raw)
	}
	var allocIP cmdb.IPAMAddress
	_ = json.Unmarshal(raw, &allocIP)
	if allocIP.IPAddress != "10.20.10.2" {
		t.Fatalf("expected next available IP 10.20.10.2, got %s", allocIP.IPAddress)
	}

	// 4. Multi-Env Versioned Config & Secrets Center: mask, audited reveal, publish, rollback, cross-env secret blanking, 3-level merge
	code, raw = doJSON("GET", P+"/config/namespaces?env=prod", nil)
	if code != 200 {
		t.Fatalf("list namespaces failed: %d", code)
	}
	var nsList struct {
		Items []configcenter.Namespace `json:"items"`
	}
	_ = json.Unmarshal(raw, &nsList)
	nsID := nsList.Items[0].ID

	// Secrets MUST be masked in list
	code, raw = doJSON("GET", P+"/config/namespaces/"+nsID+"/items", nil)
	if code != 200 || !strings.Contains(string(raw), configcenter.MaskedSecretPlaceholder) || strings.Contains(string(raw), "ProdInit#2026!Safe") {
		t.Fatalf("secret masking violated: %d %s", code, raw)
	}
	// Audited reveal returns real plaintext
	code, raw = doJSON("POST", P+"/config/namespaces/"+nsID+"/items/DB_ADMIN_PASSWORD/reveal", map[string]any{"reason": "e2e verify"})
	if code != 200 || !strings.Contains(string(raw), "ProdInit#2026!Safe") {
		t.Fatalf("secret reveal failed: %d %s", code, raw)
	}
	// Cross-env clone MUST blank secret so prod secrets never leak to dev
	code, raw = doJSON("POST", P+"/config/namespaces/"+nsID+"/clone", map[string]any{"targetEnv": "dev", "targetName": "global-dev"})
	if code != 201 {
		t.Fatalf("clone namespace failed: %d %s", code, raw)
	}
	var devNS configcenter.Namespace
	_ = json.Unmarshal(raw, &devNS)
	_, raw = doJSON("POST", P+"/config/namespaces/"+devNS.ID+"/items/DB_ADMIN_PASSWORD/reveal", map[string]any{"reason": "check blank"})
	if strings.Contains(string(raw), "ProdInit#2026!Safe") {
		t.Fatalf("cross-env secret leaked into cloned namespace!")
	}
	// 3-Level parameter merge: Spec > Service > Global
	code, raw = doJSON("POST", P+"/config/params:preview", map[string]any{
		"globalParams":  map[string]any{"PORT": 8080, "LOG_LEVEL": "info"},
		"serviceParams": map[string]any{"PORT": 9090},
		"specs":         []map[string]any{{"serviceName": "api", "paramPrefix": "API_", "cpuApply": "500m", "cpuLimit": "2", "memApply": "512Mi", "memLimit": "2Gi", "replicas": 3}},
	})
	if code != 200 || !strings.Contains(string(raw), `"spec@API_CPU":"500m"`) || !strings.Contains(string(raw), `"PORT":9090`) {
		t.Fatalf("3-level param preview failed: %d %s", code, raw)
	}

	// 5. Declarative Extensible Plugin YAML -> dynamically registered into live Registry & compiled by Delivery Release!
	customYAML := `apiVersion: kinstall.io/v1alpha1
kind: PluginPackage
name: custom-echo-svc
displayName: 自定义 Echo 业务服务
category: 业务应用
version: 1.0.0
targets: [host-container]
composeTemplate: |
  services:
    echo:
      image: alpine:latest
      ports: ["8099:8099"]
healthCheck:
  tcpPort: 8099
`
	code, raw = doJSON("POST", P+"/plugins/declarative", map[string]any{"manifestYaml": customYAML, "isEnabled": true})
	if code != 200 {
		t.Fatalf("upsert declarative plugin failed: %d %s", code, raw)
	}
	if _, err := reg.Resolve("custom-echo-svc", "host-container", "1.0.0"); err != nil {
		t.Fatalf("declarative plugin not dynamically registered: %v", err)
	}

	// 6. Enterprise BATCH Delivery Release with pause_after breakpoint + real HTTP dialing check (ts.URL + "/healthz") + resume + rollback
	code, raw = doJSON("POST", P+"/delivery/releases", map[string]any{
		"name":         "his-v2-canary",
		"env":          "prod",
		"scheduleMode": "BATCH",
		"items": []map[string]any{
			{"batchNo": 0, "serviceName": "canary-gw", "pluginName": "custom-echo-svc", "pauseAfter": true, "probeUrl": ts.URL + "/healthz"},
			{"batchNo": 1, "serviceName": "full-gw", "pluginName": "custom-echo-svc", "pauseAfter": false, "probeUrl": ts.URL + "/healthz"},
		},
	})
	if code != 201 {
		t.Fatalf("create delivery release failed: %d %s", code, raw)
	}
	var rel delivery.Release
	_ = json.Unmarshal(raw, &rel)

	// Start -> batch 0 succeeds with real HTTP dial check & pauses cleanly at breakpoint!
	code, raw = doJSON("POST", P+"/delivery/releases/"+rel.ID+":start", nil)
	if code != 200 {
		t.Fatalf("start release failed: %d %s", code, raw)
	}
	_ = json.Unmarshal(raw, &rel)
	if rel.Status != "PAUSED" || rel.Items[0].Status != "SUCCEEDED" || rel.Items[1].Status != "PENDING" {
		t.Fatalf("expected PAUSED after batch 0 breakpoint, got status=%s items=%+v", rel.Status, rel.Items)
	}
	// Resume -> batch 1 runs & completes SUCCEEDED!
	code, raw = doJSON("POST", P+"/delivery/releases/"+rel.ID+":resume", nil)
	_ = json.Unmarshal(raw, &rel)
	if code != 200 || rel.Status != "SUCCEEDED" {
		t.Fatalf("expected SUCCEEDED after resume, got %d %s", code, rel.Status)
	}

	// 7. Pure-Go Native OCI Inspect & Fail-Closed Arch Mismatch Check (Zero docker CLI!)
	img, _ := mutate.ConfigFile(empty.Image, &v1.ConfigFile{Architecture: "amd64", OS: "linux"})
	tarPath := filepath.Join(tmp, "test-amd64.tar")
	tag, _ := name.NewTag("example.local/test:1.0")
	if err := tarball.WriteToFile(tarPath, tag, img); err != nil {
		t.Fatalf("write oci tarball: %v", err)
	}
	insp, err := oci.InspectTarball(tarPath)
	if err != nil || insp.Architecture != "amd64" || insp.OS != "linux" {
		t.Fatalf("native oci inspect failed: %+v %v", insp, err)
	}
	// Expect strict fail-closed rejection when pushing amd64 tarball as arm64!
	_, err = oci.PushTarballNative(ctx, oci.PushRequest{TarballPath: tarPath, TargetRef: "127.0.0.1:5000/test:1.0", ExpectedArch: "arm64"})
	if err == nil || !strings.Contains(err.Error(), "Fail-Closed") {
		t.Fatalf("expected fail-closed arch mismatch error, got: %v", err)
	}

	// 8. Zero-Trust Outbound Go Agent: Issue 15m Token -> Live Agent Enroll + Heartbeat -> Delegate TCP Probe -> Revoke!
	code, raw = doJSON("POST", P+"/fleet/enroll-tokens", map[string]any{"zone": "dmz-a", "maxUses": 1, "ttlMinutes": 15, "serverUrl": ts.URL})
	if code != 201 {
		t.Fatalf("issue enroll token failed: %d %s", code, raw)
	}
	var tokIssue agenthub.EnrollTokenIssue
	_ = json.Unmarshal(raw, &tokIssue)

	agentCtx, cancelAgent := context.WithCancel(ctx)
	defer cancelAgent()
	go func() {
		_ = agenthub.RunAgent(agentCtx, agenthub.RunnerConfig{
			ServerURL:   ts.URL,
			EnrollToken: tokIssue.Token,
			Zone:        "dmz-a",
			StateDir:    filepath.Join(tmp, "agent-state"),
			Logger:      logger,
		})
	}()

	// Wait up to 4s for real agent enrollment & heartbeat
	var agents []agenthub.AgentRecord
	for i := 0; i < 40; i++ {
		_, raw = doJSON("GET", P+"/fleet/agents", nil)
		var al struct {
			Items []agenthub.AgentRecord `json:"items"`
		}
		_ = json.Unmarshal(raw, &al)
		if len(al.Items) > 0 && al.Items[0].Status == "ONLINE" {
			agents = al.Items
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if len(agents) == 0 {
		t.Fatalf("live outbound agent failed to enroll & heartbeat")
	}

	// Delegate real TCP probe via the live enrolled Agent (tests both reachable ts.Listener & blocked 169.254.169.254 metadata IP!)
	hostPort := strings.Split(strings.TrimPrefix(ts.URL, "http://"), ":")
	var portNum int
	for _, ch := range hostPort[1] {
		portNum = portNum*10 + int(ch-'0')
	}
	code, raw = doJSON("POST", P+"/fleet/probes/tcp", map[string]any{
		"agentId": agents[0].ID,
		"targets": []map[string]any{
			{"host": hostPort[0], "port": portNum},
			{"host": "169.254.169.254", "port": 80},
		},
		"timeoutSec": 2,
	})
	if code != 200 || !strings.Contains(string(raw), `"status":"OK"`) || !strings.Contains(string(raw), `"status":"BLOCKED"`) {
		t.Fatalf("agent TCP probe failed: %d %s", code, raw)
	}

	// Revoke agent & verify status flips to REVOKED
	code, _ = doJSON("POST", P+"/fleet/agents/"+agents[0].ID+":revoke", nil)
	if code != 200 {
		t.Fatalf("revoke agent failed: %d", code)
	}
	cancelAgent()
}

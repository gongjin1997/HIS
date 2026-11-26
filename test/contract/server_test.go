package contract_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"sigs.k8s.io/yaml"

	"github.com/kinstall/kinstall/internal/app"
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
	"github.com/kinstall/kinstall/internal/notify"
	"github.com/kinstall/kinstall/internal/osadapter"
	"github.com/kinstall/kinstall/internal/pki"
	"github.com/kinstall/kinstall/internal/registry"
	"github.com/kinstall/kinstall/internal/secrets"
	"github.com/kinstall/kinstall/internal/server"
	"github.com/kinstall/kinstall/internal/store"
	"github.com/kinstall/kinstall/internal/template"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

type stubValkeyComponent struct{}

func (c *stubValkeyComponent) Meta() sdk.PluginMeta {
	return sdk.PluginMeta{
		Name:       "valkey",
		Version:    "1.0.0",
		SDKVersion: sdk.SDKAPIVersion,
		Type:       sdk.PluginComponent,
		Products: []sdk.ProductRange{{
			Product:    "valkey",
			Versions:   ">=8.0.0",
			Targets:    []sdk.DeployTarget{sdk.TargetHostBinary, sdk.TargetHostContainer},
			DefaultVer: "9.0.3",
		}},
		Platforms: sdk.Platforms{Arch: []string{"amd64", "arm64"}, PageSizes: []int{4096, 65536}},
	}
}
func (c *stubValkeyComponent) Validate(_ context.Context, _ *sdk.ValidateRequest) (*sdk.ValidationReport, error) {
	return &sdk.ValidationReport{}, nil
}
func (c *stubValkeyComponent) Plan(_ context.Context, _ *sdk.PlanRequest) (*sdk.PlanResult, error) {
	pb := sdk.NewPipelineBuilder().Phase(sdk.PhaseSpec{
		Name:  "prepare",
		Tasks: []sdk.TaskSpec{sdk.CmdRun("echo valkey-ok", "")},
	})
	return &sdk.PlanResult{
		Summary:  "Install Valkey 9.0.3",
		Pipeline: pb.Build(),
	}, nil
}
func (c *stubValkeyComponent) Operation(_ string) (sdk.Operation, bool) { return nil, false }
func (c *stubValkeyComponent) Status(_ context.Context, _ *sdk.StatusRequest) (*sdk.InstanceStatus, error) {
	return &sdk.InstanceStatus{Health: "healthy"}, nil
}

func setupTestServer(t *testing.T) (*httptest.Server, *store.DBStore, *events.Hub, *authz.Service) {
	t.Helper()
	ctx := context.Background()
	tmp := t.TempDir()

	st, err := store.OpenAndMigrate(ctx, store.Config{Driver: "sqlite", DSN: filepath.Join(tmp, "kinstall.db")}, nil)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	kekPath := filepath.Join(tmp, "master.key")
	if err := secrets.GenerateKeyringFile(kekPath); err != nil {
		t.Fatalf("gen keyring: %v", err)
	}
	kek, err := secrets.NewFileKEKProvider("test-kek", kekPath)
	if err != nil {
		t.Fatalf("kek: %v", err)
	}
	secMgr := secrets.NewEnvelopeStore(kek, "file", secrets.AlgSM4GCM)

	cat, err := i18n.New()
	if err != nil {
		t.Fatalf("i18n: %v", err)
	}
	osEng, err := osadapter.LoadEmbedded()
	if err != nil {
		t.Fatalf("osadapter: %v", err)
	}
	reg := registry.New()
	if err := reg.RegisterComponent(&stubValkeyComponent{}); err != nil {
		t.Fatalf("register component: %v", err)
	}
	reg.Freeze()

	evHub := events.NewHub(st.OperationEvents(), "", nil)
	audRec := audit.NewRecorder(st.Audit(), true)
	authSvc := authz.NewService(st.Auth(), audRec)
	mediaRes := source.NewPolicyMediaResolver(filepath.Join(tmp, "cache"))
	pkiIssuer := pki.NewIssuer()
	kcMgr := multicluster.NewKubeconfigManager(st.Kubeconfigs(), pkiIssuer, audRec)
	tplEngine := template.New(nil)
	locker := lock.NewManager(st.Locks())
	localExec := &executor.LocalExecutor{RootDir: tmp}
	worker := engine.NewWorker(st, locker, localExec, secMgr, mediaRes, pkiIssuer, tplEngine, tplEngine, evHub, audRec, osEng, nil)

	facade := app.NewFacade(st, reg, localExec, secMgr, mediaRes, evHub, audRec, authSvc, worker, osEng, kcMgr, true, "file", "sm4-gcm")
	handler := server.NewRouter(ctx, facade, authSvc, cat, nil, server.Options{})
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	return ts, st, evHub, authSvc
}

func doJSON(t *testing.T, method, url, token string, headers map[string]string, body any) (*http.Response, []byte) {
	t.Helper()
	var r io.Reader
	if body != nil {
		if raw, ok := body.([]byte); ok {
			r = bytes.NewReader(raw)
		} else {
			b, err := json.Marshal(body)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			r = bytes.NewReader(b)
		}
	}
	req, err := http.NewRequest(method, url, r)
	if err != nil {
		t.Fatalf("new req: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do %s %s: %v", method, url, err)
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return resp, raw
}

func TestControlPlaneEndToEndContracts(t *testing.T) {
	ts, st, evHub, authSvc := setupTestServer(t)
	ctx := context.Background()

	// Seed two users: admin (requester) and reviewer (approver)
	adminBindings := []domain.RoleBinding{{ProjectID: "*", Role: domain.RoleAdmin}}
	if _, err := authSvc.CreateLocalUser(ctx, "admin", "Admin User", "SuperAdmin#2026!", adminBindings); err != nil {
		t.Fatalf("create admin: %v", err)
	}
	if _, err := authSvc.CreateLocalUser(ctx, "reviewer", "Reviewer User", "Reviewer#2026!", adminBindings); err != nil {
		t.Fatalf("create reviewer: %v", err)
	}

	// 1. Health, Ready, OpenAPI, Embedded UI CSP
	resp, body := doJSON(t, http.MethodGet, ts.URL+"/healthz", "", nil, nil)
	if resp.StatusCode != 200 || !strings.Contains(string(body), `"status":"ok"`) {
		t.Fatalf("/healthz unexpected: %d %s", resp.StatusCode, body)
	}
	resp, body = doJSON(t, http.MethodGet, ts.URL+"/api/v1/openapi.yaml", "", nil, nil)
	if resp.StatusCode != 200 || !strings.Contains(string(body), "openapi: 3.1.0") {
		t.Fatalf("openapi.yaml unexpected: %d", resp.StatusCode)
	}
	resp, _ = doJSON(t, http.MethodGet, ts.URL+"/", "", nil, nil)
	if resp.StatusCode != 200 || !strings.Contains(resp.Header.Get("Content-Security-Policy"), "default-src 'self'") {
		t.Fatalf("embedded UI CSP missing: %d %q", resp.StatusCode, resp.Header.Get("Content-Security-Policy"))
	}

	// 2. Bilingual RFC 9457 ProblemDetails on unauthenticated access
	respZH, rawZH := doJSON(t, http.MethodGet, ts.URL+"/api/v1/projects", "", map[string]string{"Accept-Language": "zh-CN"}, nil)
	respEN, rawEN := doJSON(t, http.MethodGet, ts.URL+"/api/v1/projects", "", map[string]string{"Accept-Language": "en-US"}, nil)
	if respZH.StatusCode != 401 || respEN.StatusCode != 401 {
		t.Fatalf("expected 401, got zh=%d en=%d", respZH.StatusCode, respEN.StatusCode)
	}
	if !strings.Contains(respZH.Header.Get("Content-Type"), "application/problem+json") {
		t.Fatalf("expected RFC 9457 problem+json, got %s", respZH.Header.Get("Content-Type"))
	}
	if string(rawZH) == string(rawEN) {
		t.Fatalf("expected bilingual titles to differ: zh=%s en=%s", rawZH, rawEN)
	}

	// 3. Login + 5-fail lockout on a sacrificial account -> 423 Locked
	_, _ = authSvc.CreateLocalUser(ctx, "lockme", "Lock Test", "LockMe#2026!", nil)
	for i := 0; i < 5; i++ {
		r, _ := doJSON(t, http.MethodPost, ts.URL+"/api/v1/auth/login", "", nil, map[string]string{"username": "lockme", "password": "wrong"})
		if r.StatusCode != 401 && r.StatusCode != 423 {
			t.Fatalf("attempt %d: unexpected status %d", i, r.StatusCode)
		}
	}
	rLock, rawLock := doJSON(t, http.MethodPost, ts.URL+"/api/v1/auth/login", "", nil, map[string]string{"username": "lockme", "password": "LockMe#2026!"})
	if rLock.StatusCode != 423 || !strings.Contains(string(rawLock), sdk.CodeAuthAccountLocked) {
		t.Fatalf("expected 423 lockout, got %d %s", rLock.StatusCode, rawLock)
	}

	// Login admin & reviewer
	rAdmin, rawAdmin := doJSON(t, http.MethodPost, ts.URL+"/api/v1/auth/login", "", nil, map[string]string{"username": "admin", "password": "SuperAdmin#2026!"})
	if rAdmin.StatusCode != 200 {
		t.Fatalf("admin login failed: %d %s", rAdmin.StatusCode, rawAdmin)
	}
	var adminLogin struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rawAdmin, &adminLogin)
	rRev, rawRev := doJSON(t, http.MethodPost, ts.URL+"/api/v1/auth/login", "", nil, map[string]string{"username": "reviewer", "password": "Reviewer#2026!"})
	if rRev.StatusCode != 200 {
		t.Fatalf("reviewer login failed: %d %s", rRev.StatusCode, rawRev)
	}
	var revLogin struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rawRev, &revLogin)

	// 4. Idempotency-Key: 10 concurrent identical POSTs -> ONLY 1 project created (winner 201, racing 409 KI-API-0004 or replayed 201); follow-up replay -> identical 201; mismatched body -> 409 KI-API-0002; >4 MiB -> 413
	const idemKey = "01JIDEMTEST000000000000001"
	var wg sync.WaitGroup
	var mu sync.Mutex
	seenIDs := map[string]bool{}
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			r, b := doJSON(t, http.MethodPost, ts.URL+"/api/v1/projects", adminLogin.Token,
				map[string]string{"Idempotency-Key": idemKey},
				map[string]string{"name": "his-prod", "description": "Hospital Core"})
			switch r.StatusCode {
			case http.StatusCreated:
				var p domain.Project
				_ = json.Unmarshal(b, &p)
				mu.Lock()
				seenIDs[p.ID] = true
				mu.Unlock()
			case http.StatusConflict:
				if !strings.Contains(string(b), sdk.CodeAPIIdempotencyProgress) {
					t.Errorf("concurrent req %d got unexpected 409: %s", idx, b)
				}
			default:
				t.Errorf("concurrent req %d got unexpected status %d: %s", idx, r.StatusCode, b)
			}
		}(i)
	}
	wg.Wait()
	if len(seenIDs) != 1 {
		t.Fatalf("expected exactly 1 created project ID across 10 concurrent calls, got %v", seenIDs)
	}
	var projID string
	for id := range seenIDs {
		projID = id
	}
	// Completed replay MUST return 201 with exact same project ID!
	rReplay, bReplay := doJSON(t, http.MethodPost, ts.URL+"/api/v1/projects", adminLogin.Token,
		map[string]string{"Idempotency-Key": idemKey},
		map[string]string{"name": "his-prod", "description": "Hospital Core"})
	var replayProj domain.Project
	_ = json.Unmarshal(bReplay, &replayProj)
	if rReplay.StatusCode != 201 || replayProj.ID != projID {
		t.Fatalf("idempotency replay failed: status=%d got=%s want=%s", rReplay.StatusCode, replayProj.ID, projID)
	}
	// Verify DB has only 1 project
	projs, _ := st.Projects().List(ctx)
	if len(projs) != 1 {
		t.Fatalf("expected 1 project in DB after 11 idempotent POSTs, got %d", len(projs))
	}
	// Mismatched body on same Idempotency-Key -> 409 KI-API-0002
	r409, b409 := doJSON(t, http.MethodPost, ts.URL+"/api/v1/projects", adminLogin.Token,
		map[string]string{"Idempotency-Key": idemKey},
		map[string]string{"name": "different-name", "description": "mismatch"})
	if r409.StatusCode != 409 || !strings.Contains(string(b409), sdk.CodeAPIIdempotencyConflict) {
		t.Fatalf("expected 409 KI-API-0002, got %d %s", r409.StatusCode, b409)
	}
	// >4 MiB body -> 413
	huge := bytes.Repeat([]byte("a"), 4*1024*1024+128)
	r413, _ := doJSON(t, http.MethodPost, ts.URL+"/api/v1/projects", adminLogin.Token,
		map[string]string{"Idempotency-Key": "01JIDEMHUGE000000000000002"}, huge)
	if r413.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 for >4MiB idempotent body, got %d", r413.StatusCode)
	}

	// 5. Media Policy CRUD + ETag/If-Match 412 + :test probe
	localMediaDir := t.TempDir()
	rMP, bMP := doJSON(t, http.MethodPost, ts.URL+"/api/v1/projects/"+projID+"/media-policies", adminLogin.Token, nil, domain.MediaPolicy{
		Name:          "prod-airgap-policy",
		OfflineStrict: true,
		DeliveryMode:  "relay-cache",
		Sources: []domain.MediaSourceConfig{
			{Type: "local", Priority: 1, Internal: true, Roots: []string{localMediaDir}},
			{Type: "online", Priority: 2, Internal: false, BaseURL: "https://example.invalid"},
		},
	})
	if rMP.StatusCode != 201 || rMP.Header.Get("ETag") != `"v-1"` {
		t.Fatalf("create media policy unexpected: %d etag=%q body=%s", rMP.StatusCode, rMP.Header.Get("ETag"), bMP)
	}
	var mp domain.MediaPolicy
	_ = json.Unmarshal(bMP, &mp)

	// Stale If-Match -> 412 KI-API-0003
	r412, b412 := doJSON(t, http.MethodPut, ts.URL+"/api/v1/projects/"+projID+"/media-policies/"+mp.ID, adminLogin.Token,
		map[string]string{"If-Match": `"v-99"`}, mp)
	if r412.StatusCode != 412 || !strings.Contains(string(b412), sdk.CodeAPIETagConflict) {
		t.Fatalf("expected 412 KI-API-0003, got %d %s", r412.StatusCode, b412)
	}
	// Valid If-Match -> 200 + ETag "v-2"
	mp.DeliveryMode = "direct-presigned"
	rUpd, bUpd := doJSON(t, http.MethodPut, ts.URL+"/api/v1/projects/"+projID+"/media-policies/"+mp.ID, adminLogin.Token,
		map[string]string{"If-Match": `"v-1"`}, mp)
	if rUpd.StatusCode != 200 || rUpd.Header.Get("ETag") != `"v-2"` {
		t.Fatalf("update media policy failed: %d etag=%q body=%s", rUpd.StatusCode, rUpd.Header.Get("ETag"), bUpd)
	}
	// :test connectivity probe -> local=ok, online=blocked with KI-SRC-0005
	rTest, bTest := doJSON(t, http.MethodPost, ts.URL+"/api/v1/projects/"+projID+"/media-policies/"+mp.ID+":test", adminLogin.Token, nil, nil)
	if rTest.StatusCode != 200 || !strings.Contains(string(bTest), sdk.CodeSrcOfflineStrictDenied) {
		t.Fatalf("media policy probe unexpected: %d %s", rTest.StatusCode, bTest)
	}

	// 6. Prod Instance -> :plan -> :apply (Four-Eyes awaiting_approval -> self-approve 403 -> reviewer approve 200)
	rInst, bInst := doJSON(t, http.MethodPost, ts.URL+"/api/v1/projects/"+projID+"/instances", adminLogin.Token, nil, domain.Instance{
		Env:             "prod",
		Name:            "valkey-core",
		Product:         "valkey",
		ProductVersion:  "9.0.3",
		DeployTarget:    sdk.TargetHostBinary,
		DesiredSpecJSON: json.RawMessage(`{"topology":"sentinel","port":6379}`),
	})
	if rInst.StatusCode != 201 {
		t.Fatalf("create instance failed: %d %s", rInst.StatusCode, bInst)
	}
	var inst domain.Instance
	_ = json.Unmarshal(bInst, &inst)

	rPlan, bPlan := doJSON(t, http.MethodPost, ts.URL+"/api/v1/projects/"+projID+"/instances/"+inst.ID+":plan", adminLogin.Token, nil, map[string]any{"action": "install"})
	if rPlan.StatusCode != 200 {
		t.Fatalf("plan failed: %d %s", rPlan.StatusCode, bPlan)
	}
	var planResp struct {
		Plan domain.Plan `json:"plan"`
	}
	_ = json.Unmarshal(bPlan, &planResp)

	rApply, bApply := doJSON(t, http.MethodPost, ts.URL+"/api/v1/projects/"+projID+"/instances/"+inst.ID+":apply", adminLogin.Token, nil, map[string]any{
		"planId": planResp.Plan.ID,
		"action": "install",
	})
	if rApply.StatusCode != 202 {
		t.Fatalf("apply failed: %d %s", rApply.StatusCode, bApply)
	}
	var op domain.Operation
	_ = json.Unmarshal(bApply, &op)
	if op.State != domain.OpAwaitingApproval {
		t.Fatalf("expected prod install to enter awaiting_approval, got %s", op.State)
	}

	// Self-approval MUST fail with 403 KI-AUTH-0002
	rSelf, bSelf := doJSON(t, http.MethodPost, ts.URL+"/api/v1/projects/"+projID+"/operations/"+op.ID+":approve", adminLogin.Token, nil, map[string]string{"note": "self"})
	if rSelf.StatusCode != 403 || !strings.Contains(string(bSelf), sdk.CodeAuthForbidden) {
		t.Fatalf("self-approval MUST be blocked with 403 KI-AUTH-0002, got %d %s", rSelf.StatusCode, bSelf)
	}
	// Second reviewer approves -> 200
	rRevAp, bRevAp := doJSON(t, http.MethodPost, ts.URL+"/api/v1/projects/"+projID+"/operations/"+op.ID+":approve", revLogin.Token, nil, map[string]string{"note": "LGTM"})
	if rRevAp.StatusCode != 200 {
		t.Fatalf("reviewer approval failed: %d %s", rRevAp.StatusCode, bRevAp)
	}

	// 7. Lossless SSE with Last-Event-ID resume & terminal operation.done WITHOUT id: line!
	for i := 1; i <= 5; i++ {
		_, err := evHub.Publish(ctx, domain.OperationEvent{
			OperationID: op.ID,
			EventType:   "task.log",
			PayloadJSON: json.RawMessage(fmt.Sprintf(`{"line":%d}`, i)),
		})
		if err != nil {
			t.Fatalf("publish %d: %v", i, err)
		}
	}
	sseReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/projects/"+projID+"/operations/"+op.ID+"/events", nil)
	sseReq.Header.Set("Authorization", "Bearer "+adminLogin.Token)
	sseReq.Header.Set("Last-Event-ID", "2")
	sseResp, err := http.DefaultClient.Do(sseReq)
	if err != nil {
		t.Fatalf("sse connect: %v", err)
	}
	defer func() { _ = sseResp.Body.Close() }()
	if sseResp.StatusCode != 200 || !strings.Contains(sseResp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("sse unexpected status/ct: %d %s", sseResp.StatusCode, sseResp.Header.Get("Content-Type"))
	}

	go func() {
		time.Sleep(120 * time.Millisecond)
		_, _ = evHub.Publish(ctx, domain.OperationEvent{
			OperationID: op.ID,
			EventType:   "task.log",
			PayloadJSON: json.RawMessage(`{"line":6}`),
		})
		claimed, _ := st.Operations().ClaimNext(ctx, "worker-test", time.Minute)
		if claimed != nil {
			_, _ = st.Operations().Finish(ctx, claimed.ID, "worker-test", claimed.FencingToken, domain.OpSucceeded, "", "")
			evHub.BroadcastAllResync()
		}
	}()

	var gotIDs []string
	var sawDone bool
	var doneHadID bool
	scanner := bufio.NewScanner(sseResp.Body)
	var curID, curEvent string
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "id: "):
			curID = strings.TrimPrefix(line, "id: ")
		case strings.HasPrefix(line, "event: "):
			curEvent = strings.TrimPrefix(line, "event: ")
		case line == "":
			if curEvent == "operation.done" {
				sawDone = true
				doneHadID = (curID != "")
			} else if curID != "" {
				gotIDs = append(gotIDs, curID)
			}
			curID, curEvent = "", ""
		}
	}
	if !sawDone {
		t.Fatalf("SSE never received terminal operation.done event")
	}
	if doneHadID {
		t.Fatalf("CRITICAL SSE violation: operation.done frame MUST NOT include an `id:` line!")
	}
	if len(gotIDs) < 4 {
		t.Fatalf("expected resumed SSE events after id 2, got %v", gotIDs)
	}
	for i := 1; i < len(gotIDs); i++ {
		if gotIDs[i] <= gotIDs[i-1] {
			t.Fatalf("SSE sequence not strictly monotonic: %v", gotIDs)
		}
	}

	// 8. Multi-Cluster Kubeconfig Issuance (cluster-admin capped at 4h, NEVER system:masters!) & Revoke
	rKC, bKC := doJSON(t, http.MethodPost, ts.URL+"/api/v1/projects/"+projID+"/clusters/"+inst.ID+"/kubeconfigs", adminLogin.Token, nil, map[string]any{
		"k8sRole":  "cluster-admin",
		"ttlHours": 24,
	})
	if rKC.StatusCode != 201 {
		t.Fatalf("issue kubeconfig failed: %d %s", rKC.StatusCode, bKC)
	}
	var kcIssued multicluster.IssuedKubeconfig
	_ = json.Unmarshal(bKC, &kcIssued)
	if dur := time.Until(kcIssued.Record.ExpiresAt); dur > 4*time.Hour+time.Minute {
		t.Fatalf("cluster-admin TTL exceeded 4h hard cap: %v", dur)
	}
	var kcDoc struct {
		Users []struct {
			User struct {
				ClientCertData string `json:"client-certificate-data"`
			} `json:"user"`
		} `json:"users"`
	}
	if err := yaml.Unmarshal([]byte(kcIssued.KubeconfigYAML), &kcDoc); err != nil || len(kcDoc.Users) == 0 {
		t.Fatalf("parse kubeconfig yaml: %v", err)
	}
	certPEM, err := base64.StdEncoding.DecodeString(kcDoc.Users[0].User.ClientCertData)
	if err != nil {
		t.Fatalf("decode cert: %v", err)
	}
	blk, _ := pem.Decode(certPEM)
	if blk == nil {
		t.Fatalf("empty PEM block from client-certificate-data=%q", kcDoc.Users[0].User.ClientCertData)
	}
	x509Cert, err := x509.ParseCertificate(blk.Bytes)
	if err != nil {
		t.Fatalf("parse x509: %v", err)
	}
	for _, org := range x509Cert.Subject.Organization {
		if org == "system:masters" {
			t.Fatalf("CRITICAL SECURITY VIOLATION: kubeconfig used system:masters!")
		}
	}
	if len(x509Cert.Subject.Organization) != 1 || x509Cert.Subject.Organization[0] != "kinstall:cluster-admin" {
		t.Fatalf("expected O=kinstall:cluster-admin, got %v", x509Cert.Subject.Organization)
	}
	rRevKC, _ := doJSON(t, http.MethodDelete, ts.URL+"/api/v1/projects/"+projID+"/clusters/kubeconfigs/"+kcIssued.Record.ID, adminLogin.Token, nil, nil)
	if rRevKC.StatusCode != 204 {
		t.Fatalf("revoke kubeconfig expected 204, got %d", rRevKC.StatusCode)
	}

	// 9. Audit Hash-Chain Verify & DJCP-L3/GM-0054 Compliance Evidence Report
	rVer, bVer := doJSON(t, http.MethodPost, ts.URL+"/api/v1/audit:verify", adminLogin.Token, nil, nil)
	if rVer.StatusCode != 200 || !strings.Contains(string(bVer), `"valid":true`) {
		t.Fatalf("audit verify failed: %d %s", rVer.StatusCode, bVer)
	}
	rComp, bComp := doJSON(t, http.MethodPost, ts.URL+"/api/v1/projects/"+projID+"/compliance:report", adminLogin.Token, nil, nil)
	if rComp.StatusCode != 200 || !strings.Contains(string(bComp), "DJCP-2.0-Level3") || !strings.Contains(string(bComp), "enable_ntls on") {
		t.Fatalf("compliance report unexpected: %d %s", rComp.StatusCode, bComp)
	}

	// 10. Notification Storm Folding (100 identical alerts in 1 min -> 1 HMAC-signed webhook delivery, 99 folded!)
	var webhookHits atomic.Int32
	var validHMAC atomic.Bool
	const hmacSecret = "webhook-secret-2026"
	hookSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		webhookHits.Add(1)
		payload, _ := io.ReadAll(r.Body)
		mac := hmac.New(sha256.New, []byte(hmacSecret))
		_, _ = mac.Write(payload)
		wantSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))
		if r.Header.Get("X-Kinstall-Signature-256") == wantSig {
			validHMAC.Store(true)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer hookSrv.Close()

	cfgJSON, _ := json.Marshal(map[string]string{"url": hookSrv.URL, "hmacSecret": hmacSecret})
	if err := st.Notifications().CreateChannel(ctx, &domain.NotificationChannel{
		ProjectID:  projID,
		Name:       "ops-webhook",
		Kind:       "webhook",
		ConfigJSON: cfgJSON,
		Events:     []string{"operation.failed"},
		Enabled:    true,
	}); err != nil {
		t.Fatalf("create notify channel: %v", err)
	}
	disp := notify.NewDispatcher(st.Notifications(), 5*time.Minute)
	totalSent, totalFolded := 0, 0
	for i := 0; i < 100; i++ {
		s, f, err := disp.Dispatch(ctx, notify.EventMessage{
			EventType:  "operation.failed",
			ProjectID:  projID,
			Severity:   "critical",
			DedupKey:   "inst-valkey-core-failed",
			Summary:    "Valkey install failed on node-1",
			OccurredAt: time.Now().UTC(),
		})
		if err != nil {
			t.Fatalf("dispatch %d: %v", i, err)
		}
		totalSent += s
		totalFolded += f
	}
	if totalSent != 1 || totalFolded != 99 || webhookHits.Load() != 1 || !validHMAC.Load() {
		t.Fatalf("storm folding failed: sent=%d folded=%d hits=%d hmac=%v", totalSent, totalFolded, webhookHits.Load(), validHMAC.Load())
	}
}

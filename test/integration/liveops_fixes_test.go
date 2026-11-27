package integration_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kinstall/kinstall/internal/app"
	"github.com/kinstall/kinstall/internal/audit"
	"github.com/kinstall/kinstall/internal/authz"
	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/internal/engine"
	"github.com/kinstall/kinstall/internal/events"
	"github.com/kinstall/kinstall/internal/lock"
	"github.com/kinstall/kinstall/internal/obs"
	"github.com/kinstall/kinstall/internal/osadapter"
	"github.com/kinstall/kinstall/internal/registry"
	"github.com/kinstall/kinstall/internal/secrets"
	"github.com/kinstall/kinstall/internal/store"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
	"github.com/kinstall/kinstall/plugins/postgresql"
	"github.com/kinstall/kinstall/plugins/valkey"
)

func TestLiveOpsInspiredLogicFixesEndToEnd(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	logger := obs.NewLogger("json", "error")

	st, err := store.OpenAndMigrate(ctx, store.Config{Driver: "sqlite", DSN: filepath.Join(tmp, "liveops.db")}, logger)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = st.Close() }()

	keyPath := filepath.Join(tmp, "master.key")
	if err := secrets.GenerateKeyringFile(keyPath); err != nil {
		t.Fatalf("gen key: %v", err)
	}
	kek, _ := secrets.NewFileKEKProvider("test-kek", keyPath)
	sec := secrets.NewEnvelopeStore(kek, "file", secrets.AlgAES256GCM)
	osEng, _ := osadapter.LoadEmbedded()
	evHub := events.NewHub(st.OperationEvents(), "", logger)
	audRec := audit.NewRecorder(st.Audit(), true)
	authSvc := authz.NewService(st.Auth(), audRec)
	reg := registry.New()
	_ = reg.RegisterComponent(postgresql.New())
	_ = reg.RegisterComponent(valkey.New())

	worker := engine.NewWorker(st, lock.NewManager(st.Locks()), nil, sec, nil, nil, nil, nil, evHub, audRec, osEng, logger)
	facade := app.NewFacade(st, reg, nil, sec, nil, evHub, audRec, authSvc, worker, osEng, nil, true, "file", secrets.AlgAES256GCM)

	admin := domain.Actor{
		ID:           "usr_admin",
		Username:     "admin",
		Type:         "user",
		RoleBindings: []domain.RoleBinding{{ProjectID: "*", Role: domain.RoleAdmin}},
	}

	prj, err := facade.CreateProject(ctx, admin, "his-core", "HIS Core Integration Test")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	cred, err := facade.CreateCredential(ctx, admin, prj.ID, "ssh-root", "ssh_password", []byte("Secret#2026"))
	if err != nil {
		t.Fatalf("create credential: %v", err)
	}

	// Create 2 hosts sharing a cloned machine-id; host-1 has live port 5432 occupied + sysctl/THP drift; host-2 is unprobed.
	h1, err := facade.CreateHost(ctx, admin, &domain.Host{
		ProjectID: prj.ID, Name: "db-node-01", Address: "10.30.1.11", SSHPort: 22, SSHUser: "root",
		CredentialID: cred.ID, FaultZone: "az-1", InitProfile: "database-pg", Arch: "amd64", PageSize: 4096,
	})
	if err != nil {
		t.Fatalf("create h1: %v", err)
	}
	if err := st.Hosts().UpdateFacts(ctx, h1.ID, sdk.FactsView{
		Hostname:   "db-node-01",
		Arch:       "amd64",
		PageSize:   4096,
		MemMB:      16384,
		MachineID:  "cloned-mid-001",
		THPEnabled: "[always] madvise never",
		Sysctls: map[string]string{
			"vm.overcommit_memory": "0",
			"vm.swappiness":        "60",
		},
		ListeningPorts: []sdk.ListeningPort{
			{Proto: "tcp", Address: "0.0.0.0", Port: 5432, Process: "legacy-pg"},
			{Proto: "tcp", Address: "0.0.0.0", Port: 6379, Process: "unmanaged-redis"},
		},
	}); err != nil {
		t.Fatalf("update h1 facts: %v", err)
	}

	h2, err := facade.CreateHost(ctx, admin, &domain.Host{
		ProjectID: prj.ID, Name: "db-node-02", Address: "10.30.1.12", SSHPort: 22, SSHUser: "root",
		CredentialID: cred.ID, FaultZone: "az-2", Arch: "amd64", PageSize: 4096,
	})
	if err != nil {
		t.Fatalf("create h2: %v", err)
	}

	// 1. P1-2: CheckHostBaseline MUST detect live sysctl & THP drift on h1 and unprobed warning on h2!
	drifts1, err := facade.CheckHostBaseline(ctx, admin, prj.ID, h1.ID)
	if err != nil {
		t.Fatalf("check h1 baseline: %v", err)
	}
	foundSysctl, foundTHP := false, false
	for _, d := range drifts1 {
		if d.Category == "sysctl" && d.Key == "vm.overcommit_memory" {
			foundSysctl = true
		}
		if d.Category == "thp" {
			foundTHP = true
		}
	}
	if !foundSysctl || !foundTHP {
		t.Fatalf("expected both sysctl and thp drift findings on h1, got: %#v", drifts1)
	}
	drifts2, err := facade.CheckHostBaseline(ctx, admin, prj.ID, h2.ID)
	if err != nil {
		t.Fatalf("check h2 baseline: %v", err)
	}
	if len(drifts2) == 0 || drifts2[0].Category != "facts" {
		t.Fatalf("expected unprobed facts warning for h2, got: %#v", drifts2)
	}

	// 2. P1-3: ImportHosts Dry-Run MUST catch duplicate DB name, duplicate DB address:port, and missing credential!
	csvData := "name,address,port,user,credential\ndb-node-01,10.30.1.99,22,root,ssh-root\nnew-node-02,10.30.1.12,22,root,ssh-root\nnew-node-03,10.30.1.98,22,root,missing-cred\nvalid-node-04,10.30.1.97,22,root,ssh-root\n"
	impRes, count, err := facade.ImportHosts(ctx, admin, prj.ID, "csv", []byte(csvData), true)
	if err != nil {
		t.Fatalf("import dry-run: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 imported in dry-run, got %d", count)
	}
	if len(impRes.Errors) != 3 {
		t.Fatalf("expected 3 DB/credential errors in dry-run, got %d: %#v", len(impRes.Errors), impRes.Errors)
	}
	if len(impRes.Rows) != 1 || impRes.Rows[0].Name != "valid-node-04" {
		t.Fatalf("expected only valid-node-04 in valid preview rows, got %#v", impRes.Rows)
	}

	// Give h2 the SAME cloned machine-id to also test P1-4 duplicate machine-id pre-flight check!
	if err := st.Hosts().UpdateFacts(ctx, h2.ID, sdk.FactsView{
		Hostname: "db-node-02", Arch: "amd64", PageSize: 4096, MemMB: 16384, MachineID: "cloned-mid-001",
	}); err != nil {
		t.Fatalf("update h2 facts: %v", err)
	}

	// 3. P1-4 + P0-3: PlanInstance emits live port-in-use & duplicate machine-id warnings;
	//    prod uninstall awaits approval WITHOUT clearing claims; forceOverride=true propagates into PipelineJSON!
	inst, err := facade.CreateInstance(ctx, admin, &domain.Instance{
		ProjectID: prj.ID, Env: "prod", Name: "pg-main", Product: "postgresql",
		DeployTarget: sdk.TargetHostBinary, ProductVersion: "18.3",
		DesiredSpecJSON: json.RawMessage(`{"port":5432,"sharedBuffersMB":1024}`),
	})
	if err != nil {
		t.Fatalf("create pg instance: %v", err)
	}
	// Seed an active claim on h1 so we verify prod uninstall does NOT wipe it prior to approval!
	if err := st.Instances().ReplaceClaims(ctx, inst.ID, []domain.HostClaim{
		{InstanceID: inst.ID, HostID: h1.ID, Role: "primary", Ports: []sdk.PortClaim{{Port: 5432, Proto: "tcp"}}},
	}); err != nil {
		t.Fatalf("seed claim: %v", err)
	}
	// Plan a fresh instance on port 5432 to trigger KI-PREFLIGHT-PORT-IN-USE (since inst owns 5432, create a 2nd instance on port 5432? No, CheckClaims would conflict; test port 5432 live warning on an unowned port using valkey on port 5432!).
	vkInst, err := facade.CreateInstance(ctx, admin, &domain.Instance{
		ProjectID: prj.ID, Env: "dev", Name: "vk-portcheck", Product: "valkey",
		DeployTarget: sdk.TargetHostBinary, ProductVersion: "9.1.4",
		DesiredSpecJSON: json.RawMessage(`{"topology":"standalone","port":6379}`),
	})
	if err != nil {
		t.Fatalf("create valkey instance: %v", err)
	}
	vkPlan, _, err := facade.PlanInstance(ctx, admin, prj.ID, vkInst.ID, "install", nil, nil)
	if err != nil {
		t.Fatalf("plan valkey: %v", err)
	}
	var vkDiff app.PlanDiffEnvelope
	_ = json.Unmarshal(vkPlan.DiffJSON, &vkDiff)
	foundDupMID, foundPortInUse := false, false
	for _, w := range vkDiff.Warnings {
		if w.Code == "KI-PREFLIGHT-DUPLICATE-MACHINE-ID" {
			foundDupMID = true
		}
		if w.Code == "KI-PREFLIGHT-PORT-IN-USE" {
			foundPortInUse = true
		}
	}
	if !foundDupMID || !foundPortInUse {
		t.Fatalf("expected both KI-PREFLIGHT-DUPLICATE-MACHINE-ID and KI-PREFLIGHT-PORT-IN-USE warnings, got %#v", vkDiff.Warnings)
	}

	// Apply valkey with forceOverride=true and verify PipelineJSON carries forceOverride: true!
	vkOp, err := facade.ApplyInstance(ctx, admin, prj.ID, vkInst.ID, vkPlan.ID, "install", true, false, nil)
	if err != nil {
		t.Fatalf("apply valkey forceOverride: %v", err)
	}
	var parsedPipe sdk.Pipeline
	if err := json.Unmarshal(vkOp.PipelineJSON, &parsedPipe); err != nil {
		t.Fatalf("unmarshal pipeline: %v", err)
	}
	if !parsedPipe.ForceOverride {
		t.Fatal("expected ForceOverride=true to be propagated into Operation.PipelineJSON")
	}

	// Prod uninstall MUST enter awaiting_approval and MUST NOT wipe existing claims before approval!
	pgUnPlan, _, err := facade.PlanInstance(ctx, admin, prj.ID, inst.ID, "uninstall", nil, nil)
	if err != nil {
		t.Fatalf("plan pg uninstall: %v", err)
	}
	pgUnOp, err := facade.ApplyInstance(ctx, admin, prj.ID, inst.ID, pgUnPlan.ID, "uninstall", false, false, nil)
	if err != nil {
		t.Fatalf("apply pg uninstall: %v", err)
	}
	if pgUnOp.State != domain.OpAwaitingApproval {
		t.Fatalf("expected prod uninstall to await approval, got %s", pgUnOp.State)
	}
	claimsAfter, err := st.Instances().ListClaimsByHost(ctx, h1.ID)
	if err != nil || len(claimsAfter) == 0 {
		t.Fatalf("CRITICAL REGRESSION: prod uninstall cleared host claims BEFORE approval! claims=%#v", claimsAfter)
	}

	// 4. P0-2: Real ApplyStack compiles layered plugin pipelines (not stub echo!) & creates stack member instances!
	stackYAML := `apiVersion: kinstall.io/v1alpha1
kind: Stack
metadata:
  name: his-mini-stack
spec:
  instances:
    - id: db
      product: postgresql
      productVersion: "18.3"
      deployTarget: host-binary
      spec:
        port: 5433
    - id: cache
      product: valkey
      productVersion: "9.1.4"
      deployTarget: host-binary
      dependsOn: [db]
      spec:
        port: 6380
`
	stackObj, _, err := facade.CreateStack(ctx, admin, prj.ID, "dev", "his-mini-stack", stackYAML)
	if err != nil {
		t.Fatalf("create stack: %v", err)
	}
	stackOp, err := facade.ApplyStack(ctx, admin, prj.ID, stackObj.ID, "install")
	if err != nil {
		t.Fatalf("apply stack: %v", err)
	}
	var stackPipe sdk.Pipeline
	if err := json.Unmarshal(stackOp.PipelineJSON, &stackPipe); err != nil {
		t.Fatalf("unmarshal stack pipeline: %v", err)
	}
	if len(stackPipe.Phases) < 2 {
		t.Fatalf("expected >= 2 real compiled plugin phases in stack pipeline, got %d", len(stackPipe.Phases))
	}
	if !strings.HasPrefix(stackPipe.Phases[0].Name, "L0/db/") {
		t.Fatalf("expected layer-0 phase prefix L0/db/, got %q", stackPipe.Phases[0].Name)
	}
	members, err := st.Instances().ListByStack(ctx, stackObj.ID)
	if err != nil || len(members) != 2 {
		t.Fatalf("expected 2 reconciled stack member instances, got %d (err=%v)", len(members), err)
	}
}

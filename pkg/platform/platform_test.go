package platform_test

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kinstall/kinstall/migrations"
	"github.com/kinstall/kinstall/pkg/platform/audit"
	"github.com/kinstall/kinstall/pkg/platform/auth"
	"github.com/kinstall/kinstall/pkg/platform/crypto"
	"github.com/kinstall/kinstall/pkg/platform/dag"
	"github.com/kinstall/kinstall/pkg/platform/db"
	"github.com/kinstall/kinstall/pkg/platform/eventbus"
	"github.com/kinstall/kinstall/pkg/platform/id"
	"github.com/kinstall/kinstall/pkg/platform/netguard"
	"github.com/kinstall/kinstall/pkg/platform/safefs"
	"github.com/kinstall/kinstall/pkg/platform/safety"
)

func TestFoundationCryptoEnvelopeAndSigning(t *testing.T) {
	dir := t.TempDir()
	kr := filepath.Join(dir, "keyring.json")
	codec, err := crypto.LoadOrCreateKeyring(kr)
	if err != nil {
		t.Fatalf("LoadOrCreateKeyring: %v", err)
	}
	// Reload from disk to ensure persistence.
	codec2, err := crypto.LoadOrCreateKeyring(kr)
	if err != nil {
		t.Fatalf("reload keyring: %v", err)
	}

	for _, alg := range []string{crypto.AlgAES256GCM, crypto.AlgSM4GCM} {
		ct, err := codec.EncryptWithAlg("super-secret-db-pass-2026!", alg)
		if err != nil {
			t.Fatalf("encrypt %s: %v", alg, err)
		}
		if !crypto.IsEncrypted(ct) || !strings.Contains(ct, ":"+alg+":") {
			t.Fatalf("unexpected envelope format: %s", ct)
		}
		pt, err := codec2.Decrypt(ct)
		if err != nil || pt != "super-secret-db-pass-2026!" {
			t.Fatalf("decrypt %s failed: pt=%q err=%v", alg, pt, err)
		}
		// Tamper detection.
		if _, err := codec2.Decrypt(ct[:len(ct)-2] + "aa"); err == nil {
			t.Fatalf("expected tamper error for %s", alg)
		}
	}

	sig := codec.SignBytes([]byte("artifact-sha256-digest"))
	if !codec2.VerifyBytes([]byte("artifact-sha256-digest"), sig) {
		t.Fatal("ed25519 verify failed")
	}
	if codec2.VerifyBytes([]byte("tampered"), sig) {
		t.Fatal("ed25519 verify should fail on tampered payload")
	}

	hash, err := crypto.HashPassword("Kinstall#2026!")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !crypto.VerifyPassword("Kinstall#2026!", hash) || crypto.VerifyPassword("wrong", hash) {
		t.Fatal("Argon2id password verification mismatch")
	}
}

func TestFoundationDBMigrationsAndRebind(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := db.Open(ctx, db.Config{Driver: "sqlite", DSN: dbPath})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer st.Close()

	mfs, err := migrations.ForDialect(st.Dialect())
	if err != nil {
		t.Fatalf("ForDialect: %v", err)
	}
	if err := st.Migrate(ctx, mfs); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	// Verify WAL split writer/reader works cleanly.
	now := time.Now().UTC().Format(time.RFC3339)
	wsID := id.New("ws")
	if _, err := st.Exec(ctx, `INSERT INTO workspaces(id, code, name, description, created_at, updated_at) VALUES(?,?,?,?,?,?)`,
		wsID, "core", "Core Ops", "Primary workspace", now, now); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	var got string
	if err := st.QueryRow(ctx, `SELECT name FROM workspaces WHERE id = ?`, wsID).Scan(&got); err != nil || got != "Core Ops" {
		t.Fatalf("read workspace failed: got=%q err=%v", got, err)
	}
}

func TestFoundationAuthRBAC(t *testing.T) {
	codec, _ := crypto.NewEphemeralCodec("")
	mgr := auth.NewManager(codec.SigningKey("jwt"), time.Hour)
	tok, _, err := mgr.Issue(auth.Principal{
		UserID:      id.New("usr"),
		Username:    "ops1",
		DisplayName: "Operator 1",
		Role:        auth.RoleOperator,
		WorkspaceID: "ws_1",
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	p, err := mgr.Verify(tok)
	if err != nil || p.Username != "ops1" || p.Role != auth.RoleOperator {
		t.Fatalf("Verify failed: p=%+v err=%v", p, err)
	}
	if !auth.HasPermission(auth.RoleOperator, auth.PermCMDBWrite) {
		t.Fatal("operator must have cmdb:write")
	}
	if auth.HasPermission(auth.RoleOperator, auth.PermIAMWrite) {
		t.Fatal("operator must NOT have iam:write")
	}
	if auth.HasPermission(auth.RoleViewer, auth.PermConfigReveal) {
		t.Fatal("viewer must NOT have config:reveal")
	}
	if !auth.HasPermission(auth.RoleAuditor, auth.PermAuditVerify) {
		t.Fatal("auditor must have audit:verify")
	}
}

func TestFoundationNetguardSafeFSDAGAuditSafety(t *testing.T) {
	// SSRF / cloud metadata block
	if err := netguard.ValidateIP(net.ParseIP("169.254.169.254"), true); err == nil {
		t.Fatal("cloud metadata IP must be blocked")
	}
	ips, err := netguard.ExpandCIDR("10.20.30.0/29", 64)
	if err != nil || len(ips) != 6 {
		t.Fatalf("ExpandCIDR failed: len=%d err=%v", len(ips), err)
	}
	if _, err := netguard.ExpandCIDR("169.254.169.0/29", 64); err == nil {
		t.Fatal("metadata CIDR must be rejected")
	}

	// SafeFS anti-ZipSlip
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	_ = tw.WriteHeader(&tar.Header{Name: "../../etc/evil", Mode: 0o600, Size: 4, Typeflag: tar.TypeReg})
	_, _ = tw.Write([]byte("evil"))
	_ = tw.Close()
	if _, err := safefs.SafeExtractTar(&buf, t.TempDir(), safefs.DefaultLimits()); err == nil {
		t.Fatal("expected zip-slip tar extraction to fail")
	}

	// Kahn DAG + Cycle Detection
	plan, err := dag.Sort([]dag.Node{
		{ID: "app", DependsOn: []string{"pg", "valkey"}},
		{ID: "pg"},
		{ID: "valkey"},
	})
	if err != nil || len(plan.Layers) != 2 || len(plan.Layers[0]) != 2 || plan.Layers[1][0] != "app" {
		t.Fatalf("unexpected DAG plan: %+v err=%v", plan, err)
	}
	_, cerr := dag.Sort([]dag.Node{
		{ID: "a", DependsOn: []string{"b"}},
		{ID: "b", DependsOn: []string{"a"}},
	})
	var ce *dag.CycleError
	if !errors.As(cerr, &ce) || len(ce.Nodes) != 2 {
		t.Fatalf("expected CycleError with 2 nodes, got %v", cerr)
	}

	// Audit RedactingHandler + Hash Chain
	var logBuf bytes.Buffer
	logger := slog.New(audit.NewRedactingHandler(slog.NewTextHandler(&logBuf, nil)))
	logger.Info("login", "username", "admin", "password", "SuperSecret123", "api_token", "ki_pat_xyz")
	if strings.Contains(logBuf.String(), "SuperSecret123") || strings.Contains(logBuf.String(), "ki_pat_xyz") {
		t.Fatalf("redacting logger leaked secret: %s", logBuf.String())
	}

	e1 := audit.Entry{ID: "aud_1", WorkspaceID: "ws_1", Actor: "admin", Action: "login", TargetType: "user", Status: "ok", PrevHash: "GENESIS", CreatedAt: "2026-11-27T00:00:00Z"}
	e1.Hash = audit.ComputeHash(e1)
	e2 := audit.Entry{ID: "aud_2", WorkspaceID: "ws_1", Actor: "admin", Action: "reveal", TargetType: "config", Status: "ok", PrevHash: e1.Hash, CreatedAt: "2026-11-27T00:01:00Z"}
	e2.Hash = audit.ComputeHash(e2)
	if ok, _, reason := audit.VerifyChain([]audit.Entry{e1, e2}); !ok {
		t.Fatalf("VerifyChain failed: %s", reason)
	}
	e2.Detail = "tampered"
	if ok, idx, _ := audit.VerifyChain([]audit.Entry{e1, e2}); ok || idx != 1 {
		t.Fatalf("expected tamper detection at idx 1, got ok=%v idx=%d", ok, idx)
	}

	// Shell Safety Guardrail
	if err := safety.CheckCommand("rm -rf /"); err == nil {
		t.Fatal("rm -rf / must be blocked")
	}
	if err := safety.CheckCommand("uname -a && df -h"); err != nil {
		t.Fatalf("safe command rejected: %v", err)
	}

	// EventBus LogBroker
	broker := eventbus.NewLogBroker(10)
	broker.Publish("job_1", "stdout", "info", "line 1")
	hist, ch, closed, cancel := broker.Subscribe("job_1", 0)
	defer cancel()
	if len(hist) != 1 || closed {
		t.Fatalf("unexpected subscribe history: len=%d closed=%v", len(hist), closed)
	}
	broker.Complete("job_1", "SUCCEEDED", "done")
	got := <-ch
	if !got.Done || got.Status != "SUCCEEDED" {
		t.Fatalf("unexpected terminal log event: %+v", got)
	}
}

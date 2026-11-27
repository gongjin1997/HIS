package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kinstall/kinstall/internal/app"
	"github.com/kinstall/kinstall/internal/audit"
	"github.com/kinstall/kinstall/internal/authz"
	"github.com/kinstall/kinstall/internal/cli"
	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/internal/i18n"
	"github.com/kinstall/kinstall/internal/obs"
	"github.com/kinstall/kinstall/internal/registry"
	"github.com/kinstall/kinstall/internal/server"
	"github.com/kinstall/kinstall/internal/store"
)

// TestCLILocalAndServerModeParity verifies local Facade mode and --server REST client mode
// produce identical behavior (T-M3-03 DoD).
func TestCLILocalAndServerModeParity(t *testing.T) {
	ctx := context.Background()
	st, err := store.OpenAndMigrate(ctx, store.Config{Driver: "sqlite", DSN: filepath.Join(t.TempDir(), "cli.db")}, nil)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = st.Close() }()

	cat, _ := i18n.New()
	audRec := audit.NewRecorder(st.Audit(), true)
	authSvc := authz.NewService(st.Auth(), audRec)
	facade := app.NewFacade(st, registry.New(), nil, nil, nil, nil, audRec, authSvc, nil, nil, nil, true, "file", "aes-256-gcm")

	u, err := authSvc.CreateLocalUser(ctx, "cliadmin", "CLI Admin", "Pass#2026!", []domain.RoleBinding{{ProjectID: "*", Role: domain.RoleAdmin}})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	rawTok, _, err := authSvc.CreateAPIToken(ctx, u.ID, nil, "cli-test", []string{"*"}, 0)
	if err != nil {
		t.Fatalf("create token: %v", err)
	}

	router := server.NewRouter(ctx, facade, authSvc, cat, obs.NewLogger("json", "error"), server.Options{})
	ts := httptest.NewServer(router)
	defer ts.Close()

	runCLI := func(args ...string) string {
		t.Helper()
		var buf bytes.Buffer
		cmd := cli.NewRootCmd(cli.Deps{Facade: facade, Out: &buf})
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			t.Fatalf("cli %v failed: %v", args, err)
		}
		return buf.String()
	}

	// Create project via local mode, list via BOTH local and --server modes and compare JSON!
	localCreated := runCLI("-o", "json", "project", "create", "parity-prj", "--desc", "Parity Check")
	if !strings.Contains(localCreated, "parity-prj") {
		t.Fatalf("local project create failed: %s", localCreated)
	}
	localList := runCLI("-o", "json", "project", "list")
	remoteList := runCLI("--server", ts.URL, "--token", rawTok, "-o", "json", "project", "list")

	var a, b []domain.Project
	_ = json.Unmarshal([]byte(localList), &a)
	_ = json.Unmarshal([]byte(remoteList), &b)
	if len(a) != 1 || len(b) != 1 || a[0].ID != b[0].ID || a[0].Name != b[0].Name {
		t.Fatalf("local vs --server parity mismatch: local=%+v remote=%+v", a, b)
	}

	// Verify remote --server mode creates second project via REST
	remoteCreated := runCLI("--server", ts.URL, "--token", rawTok, "-o", "json", "project", "create", "remote-prj")
	if !strings.Contains(remoteCreated, "remote-prj") {
		t.Fatalf("remote project create failed: %s", remoteCreated)
	}
	// Verify server HTTP 200 on OIDC config endpoint
	resp, err := http.Get(ts.URL + "/api/v1/auth/oidc/config")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("oidc config failed: %v", err)
	}
	_ = resp.Body.Close()
}

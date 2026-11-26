package authz_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kinstall/kinstall/internal/authz"
	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/internal/store"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

func TestArgon2LockoutTokensAndRBAC(t *testing.T) {
	ctx := context.Background()
	st, err := store.OpenAndMigrate(ctx, store.Config{Driver: "sqlite", DSN: filepath.Join(t.TempDir(), "auth.db")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	svc := authz.NewService(st.Auth(), nil)
	u, err := svc.CreateLocalUser(ctx, "alice", "Alice", "StrongPass!2026", []domain.RoleBinding{
		{ProjectID: "prj_1", Role: domain.RoleOperator},
	})
	if err != nil {
		t.Fatal(err)
	}

	// 4 wrong logins -> 401; 5th wrong login -> 423 Locked (KI-AUTH-0003)!
	for i := 1; i <= 4; i++ {
		if _, _, err := svc.LoginLocal(ctx, "alice", "wrong", "10.0.0.1", "test"); !sdk.IsCode(err, sdk.CodeAuthUnauthenticated) {
			t.Fatalf("attempt %d expected KI-AUTH-0001, got %v", i, err)
		}
	}
	if _, _, err := svc.LoginLocal(ctx, "alice", "wrong", "10.0.0.1", "test"); !sdk.IsCode(err, sdk.CodeAuthAccountLocked) {
		t.Fatalf("attempt 5 expected 423 KI-AUTH-0003, got %v", err)
	}
	// Even with correct password while locked -> still 423!
	if _, _, err := svc.LoginLocal(ctx, "alice", "StrongPass!2026", "10.0.0.1", "test"); !sdk.IsCode(err, sdk.CodeAuthAccountLocked) {
		t.Fatalf("expected locked account to block valid password, got %v", err)
	}

	// Scoped API token format ki_<8>_<32> + verify + revoke
	plainTok, meta, err := svc.CreateAPIToken(ctx, u.ID, nil, "ci-deploy", []string{"instance.apply"}, 30*24*time.Hour)
	if err != nil || !strings.HasPrefix(plainTok, "ki_") {
		t.Fatalf("CreateAPIToken: %v %q", err, plainTok)
	}
	actor, err := svc.AuthenticateBearer(ctx, plainTok, "10.0.0.2")
	if err != nil || actor.Username != "alice" {
		t.Fatalf("AuthenticateBearer failed: %+v err=%v", actor, err)
	}
	// RBAC checks: operator can apply, cannot approve or read audit
	if err := authz.Authorize(*actor, "prj_1", "instance.apply"); err != nil {
		t.Fatalf("operator should apply: %v", err)
	}
	if err := authz.Authorize(*actor, "prj_1", "operation.approve"); !sdk.IsCode(err, sdk.CodeAuthForbidden) {
		t.Fatalf("operator must NOT approve: %v", err)
	}
	if err := authz.Authorize(*actor, "prj_1", "audit.read"); !sdk.IsCode(err, sdk.CodeAuthForbidden) {
		t.Fatalf("operator must NOT read audit: %v", err)
	}
	_ = st.Auth().RevokeAPIToken(ctx, meta.ID)
	if _, err := svc.AuthenticateBearer(ctx, plainTok, "10.0.0.2"); !sdk.IsCode(err, sdk.CodeAuthUnauthenticated) {
		t.Fatalf("revoked token must fail auth, got %v", err)
	}
}

package contract_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/internal/store"
)

// TestStoreDualDialectContract runs the exact same repository contract suite against SQLite (always)
// and PostgreSQL 18 (when KINSTALL_TEST_PG_DSN is provided in CI) per ARCHITECTURE O-02 / Redline 7.
func TestStoreDualDialectContract(t *testing.T) {
	runContract := func(t *testing.T, cfg store.Config) {
		ctx := context.Background()
		st, err := store.OpenAndMigrate(ctx, cfg, nil)
		if err != nil {
			t.Fatalf("open %s: %v", cfg.Driver, err)
		}
		defer func() { _ = st.Close() }()

		ver, err := st.SchemaVersion(ctx)
		if err != nil || ver < 3 {
			t.Fatalf("expected schema version >= 3 on %s, got %d err=%v", cfg.Driver, ver, err)
		}

		p := &domain.Project{ID: domain.NewID(domain.PrefixProject), Name: "dual-" + cfg.Driver, Description: "Dual Dialect Contract"}
		if err := st.Projects().Create(ctx, p); err != nil {
			t.Fatalf("create project on %s: %v", cfg.Driver, err)
		}
		got, err := st.Projects().Get(ctx, p.ID)
		if err != nil || got.Name != p.Name {
			t.Fatalf("get project on %s: %v %+v", cfg.Driver, err, got)
		}
		if err := st.Projects().Delete(ctx, p.ID); err != nil {
			t.Fatalf("delete project on %s: %v", cfg.Driver, err)
		}
	}

	t.Run("sqlite", func(t *testing.T) {
		runContract(t, store.Config{Driver: "sqlite", DSN: filepath.Join(t.TempDir(), "dual.db")})
	})

	if pgDSN := os.Getenv("KINSTALL_TEST_PG_DSN"); pgDSN != "" {
		t.Run("postgres", func(t *testing.T) {
			runContract(t, store.Config{Driver: "postgres", DSN: pgDSN})
		})
	}
}

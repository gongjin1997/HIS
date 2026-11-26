package store

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	_ "modernc.org/sqlite"

	"github.com/kinstall/kinstall/internal/ports"
	"github.com/kinstall/kinstall/internal/version"
	"github.com/kinstall/kinstall/migrations"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// Config defines database connection parameters for SQLite (CLI) or PostgreSQL 18 (HA Server).
type Config struct {
	Driver    string // sqlite | postgres
	DSN       string // file path for sqlite (or ":memory:") or postgres:// URL
	BackupDir string // optional pre-upgrade VACUUM INTO backup directory for SQLite
}

// DBStore implements ports.Store with separated read/write pools on SQLite and pgxpool on PostgreSQL (D2 §1.1-§1.2).
type DBStore struct {
	dialect  string
	writeSQL *sql.DB
	readSQL  *sql.DB
	writeDB  *bun.DB
	readDB   *bun.DB
	logger   *slog.Logger
}

const pgMigrateAdvisoryLockKey int64 = 0x6b696e7374616c01

// OpenAndMigrate opens the store, snapshots SQLite before migration, runs embedded forward-only
// goose migrations (serialized via pg_advisory_lock on PG), and enforces MaxCompatibleSchemaVersion (G1 §6).
func OpenAndMigrate(ctx context.Context, cfg Config, logger *slog.Logger) (*DBStore, error) {
	if logger == nil {
		logger = slog.Default()
	}
	driver := strings.ToLower(strings.TrimSpace(cfg.Driver))
	if driver == "" {
		driver = "sqlite"
	}

	switch driver {
	case "sqlite":
		return openSQLite(ctx, cfg, logger)
	case "postgres", "postgresql":
		return openPostgres(ctx, cfg, logger)
	default:
		return nil, sdk.NewError(sdk.CodeDBUnavailableOrSchema, "DB", http.StatusServiceUnavailable, sdk.CodeDBUnavailableOrSchema, false,
			fmt.Errorf("unsupported store driver %q", driver))
	}
}

func openSQLite(ctx context.Context, cfg Config, logger *slog.Logger) (*DBStore, error) {
	path := cfg.DSN
	if path == "" {
		path = ":memory:"
	}
	isMem := path == ":memory:" || strings.Contains(path, "mode=memory")

	var writeDSN, readDSN string
	if isMem {
		writeDSN = "file:kinstall_mem?mode=memory&cache=shared&_txlock=immediate&_pragma=foreign_keys(ON)&_pragma=busy_timeout(10000)"
		readDSN = writeDSN
	} else {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return nil, fmt.Errorf("mkdir sqlite dir: %w", err)
		}
		absPath, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		u := (&url.URL{Scheme: "file", Path: absPath}).String()
		// All connection-scoped PRAGMAs delivered via DSN per D2 §1.1
		writeDSN = u + "?_txlock=immediate&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(10000)&_pragma=cache_size(-64000)&_pragma=temp_store(MEMORY)"
		readDSN = u + "?_pragma=query_only(ON)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(10000)&_pragma=cache_size(-64000)&_pragma=temp_store(MEMORY)"
	}

	writeSQL, err := sql.Open("sqlite", writeDSN)
	if err != nil {
		return nil, sdk.NewError(sdk.CodeDBUnavailableOrSchema, "DB", http.StatusServiceUnavailable, sdk.CodeDBUnavailableOrSchema, true, err)
	}
	// Physical pool separation: 1 writer avoids SQLITE_BUSY under high concurrency (D2 §1.1)
	writeSQL.SetMaxOpenConns(1)
	writeSQL.SetMaxIdleConns(1)
	if err := writeSQL.PingContext(ctx); err != nil {
		_ = writeSQL.Close()
		return nil, sdk.NewError(sdk.CodeDBUnavailableOrSchema, "DB", http.StatusServiceUnavailable, sdk.CodeDBUnavailableOrSchema, true, err)
	}

	// Pre-upgrade consistent snapshot if existing file DB has data (G1 §6.2)
	if !isMem && cfg.BackupDir != "" {
		if fi, serr := os.Stat(path); serr == nil && fi.Size() > 0 {
			if mkErr := os.MkdirAll(cfg.BackupDir, 0o750); mkErr == nil {
				bkPath := filepath.Join(cfg.BackupDir, fmt.Sprintf("pre-upgrade-%s.db", time.Now().UTC().Format("20060102T150405Z")))
				_, _ = writeSQL.ExecContext(ctx, "VACUUM INTO ?", bkPath)
			}
		}
	}

	// Run forward-only goose migrations BEFORE opening readDB so WAL initialization never races (D2 §1.1 #3)
	if err := runGooseMigrations(ctx, writeSQL, "sqlite3", "sqlite"); err != nil {
		_ = writeSQL.Close()
		return nil, err
	}

	var readSQL *sql.DB
	if isMem {
		readSQL = writeSQL
	} else {
		readSQL, err = sql.Open("sqlite", readDSN)
		if err != nil {
			_ = writeSQL.Close()
			return nil, sdk.NewError(sdk.CodeDBUnavailableOrSchema, "DB", http.StatusServiceUnavailable, sdk.CodeDBUnavailableOrSchema, true, err)
		}
		readSQL.SetMaxOpenConns(4)
		readSQL.SetMaxIdleConns(4)
		if err := readSQL.PingContext(ctx); err != nil {
			_ = readSQL.Close()
			_ = writeSQL.Close()
			return nil, sdk.NewError(sdk.CodeDBUnavailableOrSchema, "DB", http.StatusServiceUnavailable, sdk.CodeDBUnavailableOrSchema, true, err)
		}
	}

	s := &DBStore{
		dialect:  "sqlite",
		writeSQL: writeSQL,
		readSQL:  readSQL,
		writeDB:  bun.NewDB(writeSQL, sqlitedialect.New()),
		readDB:   bun.NewDB(readSQL, sqlitedialect.New()),
		logger:   logger,
	}
	if err := s.checkSchemaCeiling(ctx); err != nil {
		_ = s.Close()
		return nil, err
	}
	return s, nil
}

func openPostgres(ctx context.Context, cfg Config, logger *slog.Logger) (*DBStore, error) {
	sqldb, err := sql.Open("pgx", cfg.DSN)
	if err != nil {
		return nil, sdk.NewError(sdk.CodeDBUnavailableOrSchema, "DB", http.StatusServiceUnavailable, sdk.CodeDBUnavailableOrSchema, true, err)
	}
	sqldb.SetMaxOpenConns(25)
	sqldb.SetMaxIdleConns(4)
	sqldb.SetConnMaxLifetime(30 * time.Minute)
	if err := sqldb.PingContext(ctx); err != nil {
		_ = sqldb.Close()
		return nil, sdk.NewError(sdk.CodeDBUnavailableOrSchema, "DB", http.StatusServiceUnavailable, sdk.CodeDBUnavailableOrSchema, true, err)
	}

	// Serialize schema migration across HA replicas using pg_advisory_lock (G1 §6.1)
	conn, err := sqldb.Conn(ctx)
	if err != nil {
		_ = sqldb.Close()
		return nil, err
	}
	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", pgMigrateAdvisoryLockKey); err != nil {
		_ = conn.Close()
		_ = sqldb.Close()
		return nil, err
	}
	migErr := runGooseMigrations(ctx, sqldb, "postgres", "postgres")
	_, _ = conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", pgMigrateAdvisoryLockKey)
	_ = conn.Close()
	if migErr != nil {
		_ = sqldb.Close()
		return nil, migErr
	}

	bunDB := bun.NewDB(sqldb, pgdialect.New())
	s := &DBStore{
		dialect:  "postgres",
		writeSQL: sqldb,
		readSQL:  sqldb,
		writeDB:  bunDB,
		readDB:   bunDB,
		logger:   logger,
	}
	if err := s.checkSchemaCeiling(ctx); err != nil {
		_ = s.Close()
		return nil, err
	}
	return s, nil
}

func runGooseMigrations(ctx context.Context, db *sql.DB, gooseDialect, dir string) error {
	sub, err := migrations.FS.ReadDir(dir)
	if err != nil || len(sub) == 0 {
		return fmt.Errorf("migrations dir %s missing: %w", dir, err)
	}
	provider, err := goose.NewProvider(goose.Dialect(gooseDialect), db, migrations.FS)
	if err != nil {
		// Fall back to sub-FS scoped to dir
		return runGooseSubFS(ctx, db, gooseDialect, dir)
	}
	_ = provider
	return runGooseSubFS(ctx, db, gooseDialect, dir)
}

func runGooseSubFS(ctx context.Context, db *sql.DB, gooseDialect, dir string) error {
	subFS, err := subFSFor(dir)
	if err != nil {
		return err
	}
	p, err := goose.NewProvider(goose.Dialect(gooseDialect), db, subFS)
	if err != nil {
		return sdk.NewError(sdk.CodeDBUnavailableOrSchema, "DB", http.StatusServiceUnavailable, sdk.CodeDBUnavailableOrSchema, false, err)
	}
	if _, err := p.Up(ctx); err != nil {
		return sdk.NewError(sdk.CodeDBUnavailableOrSchema, "DB", http.StatusServiceUnavailable, sdk.CodeDBUnavailableOrSchema, false, err)
	}
	return nil
}

func (s *DBStore) checkSchemaCeiling(ctx context.Context) error {
	ver, err := s.SchemaVersion(ctx)
	if err != nil {
		return err
	}
	if ver > version.MaxCompatibleSchemaVersion {
		return sdk.NewError(sdk.CodeDBUnavailableOrSchema, "DB", http.StatusServiceUnavailable, sdk.CodeDBUnavailableOrSchema, false,
			fmt.Errorf("db schema version %d exceeds binary max compatible version %d", ver, version.MaxCompatibleSchemaVersion))
	}
	return nil
}

// Dialect returns "sqlite" or "postgres".
func (s *DBStore) Dialect() string { return s.dialect }

// SchemaVersion queries goose_db_version for the latest applied migration version.
func (s *DBStore) SchemaVersion(ctx context.Context) (int64, error) {
	var ver int64
	err := s.readDB.NewRaw("SELECT COALESCE(MAX(version_id), 0) FROM goose_db_version WHERE is_applied = ?", true).Scan(ctx, &ver)
	if err != nil {
		return 0, sdk.NewError(sdk.CodeDBUnavailableOrSchema, "DB", http.StatusServiceUnavailable, sdk.CodeDBUnavailableOrSchema, true, err)
	}
	return ver, nil
}

// RawWriteSQL exposes the underlying write *sql.DB solely for low-level contract tests.
func (s *DBStore) RawWriteSQL() *sql.DB { return s.writeSQL }

// Close shuts down write and read pools cleanly.
func (s *DBStore) Close() error {
	var firstErr error
	if s.readSQL != nil && s.readSQL != s.writeSQL {
		if err := s.readSQL.Close(); err != nil {
			firstErr = err
		}
	}
	if s.writeSQL != nil {
		if err := s.writeSQL.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (s *DBStore) Projects() ports.ProjectRepo               { return &projectRepo{s: s} }
func (s *DBStore) Credentials() ports.CredentialRepo         { return &credentialRepo{s: s} }
func (s *DBStore) Hosts() ports.HostRepo                     { return &hostRepo{s: s} }
func (s *DBStore) Stacks() ports.StackRepo                   { return &stackRepo{s: s} }
func (s *DBStore) Instances() ports.InstanceRepo             { return &instanceRepo{s: s} }
func (s *DBStore) Operations() ports.OperationRepo           { return &operationRepo{s: s} }
func (s *DBStore) TaskRuns() ports.TaskRunRepo               { return &taskRunRepo{s: s} }
func (s *DBStore) OperationEvents() ports.OperationEventRepo { return &opEventRepo{s: s} }
func (s *DBStore) Locks() ports.LockRepo                     { return &lockRepo{s: s} }
func (s *DBStore) Audit() ports.AuditRepo                    { return &auditRepo{s: s} }
func (s *DBStore) Approvals() ports.ApprovalRepo             { return &approvalRepo{s: s} }
func (s *DBStore) MediaPolicies() ports.MediaPolicyRepo      { return &mediaPolicyRepo{s: s} }
func (s *DBStore) Auth() ports.AuthRepo                      { return &authRepo{s: s} }
func (s *DBStore) Idempotency() ports.IdempotencyRepo        { return &idempotencyRepo{s: s} }
func (s *DBStore) Plans() ports.PlanRepo                     { return &planRepo{s: s} }
func (s *DBStore) StateBackups() ports.StateBackupRepo       { return &stateBackupRepo{s: s} }
func (s *DBStore) Notifications() ports.NotificationRepo     { return &notificationRepo{s: s} }
func (s *DBStore) Kubeconfigs() ports.KubeconfigRepo         { return &kubeconfigRepo{s: s} }

func fmtTime(t time.Time) string {
	return t.UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano)
}

func parseTime(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05.999999-07", "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

func notFoundErr(resource, id string) error {
	return sdk.NewError(sdk.CodeDBNotFound, "DB", http.StatusNotFound, sdk.CodeDBNotFound, false,
		fmt.Errorf("%s %q not found", resource, id))
}

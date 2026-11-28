package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"hash/fnv"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

type Dialect string

const (
	DialectSQLite   Dialect = "sqlite"
	DialectPostgres Dialect = "postgres"
)

// Scope represents the mandatory LiveOps multi-tenant isolation key (tenant_id, app_id).
type Scope struct {
	TenantID string
	AppID    string
}

type Config struct {
	Driver string // "sqlite" | "postgres"
	DSN    string
}

// Store provides dual-dialect SQLite WAL (1-writer / 4-reader split) & PostgreSQL 16 (RLS + advisory locks).
type Store struct {
	dialect Dialect
	writer  *sql.DB
	reader  *sql.DB
}

func Open(ctx context.Context, cfg Config) (*Store, error) {
	drv := strings.ToLower(strings.TrimSpace(cfg.Driver))
	if drv == "" {
		drv = string(DialectSQLite)
	}
	switch Dialect(drv) {
	case DialectSQLite:
		return openSQLite(ctx, cfg.DSN)
	case DialectPostgres:
		return openPostgres(ctx, cfg.DSN)
	default:
		return nil, fmt.Errorf("unsupported db driver %q", cfg.Driver)
	}
}

func openSQLite(ctx context.Context, dsn string) (*Store, error) {
	if strings.TrimSpace(dsn) == "" {
		dsn = ":memory:"
	}
	isMem := dsn == ":memory:" || strings.Contains(dsn, "mode=memory")
	var writerDSN, readerDSN string
	if isMem {
		writerDSN = "file::memory:?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
		readerDSN = writerDSN
	} else {
		if err := os.MkdirAll(filepath.Dir(dsn), 0o750); err != nil {
			return nil, fmt.Errorf("mkdir sqlite dir: %w", err)
		}
		base := "file:" + dsn + "?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=cache_size(-32000)"
		writerDSN = base + "&_txlock=immediate"
		readerDSN = base + "&mode=ro"
	}

	writer, err := sql.Open("sqlite", writerDSN)
	if err != nil {
		return nil, fmt.Errorf("open sqlite writer: %w", err)
	}
	writer.SetMaxOpenConns(1)
	writer.SetMaxIdleConns(1)
	if err := writer.PingContext(ctx); err != nil {
		_ = writer.Close()
		return nil, fmt.Errorf("ping sqlite writer: %w", err)
	}
	if !isMem {
		if _, err := writer.ExecContext(ctx, `PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON;`); err != nil {
			_ = writer.Close()
			return nil, err
		}
	}

	reader := writer
	if !isMem {
		r, err := sql.Open("sqlite", readerDSN)
		if err != nil {
			_ = writer.Close()
			return nil, fmt.Errorf("open sqlite reader: %w", err)
		}
		r.SetMaxOpenConns(4)
		r.SetMaxIdleConns(4)
		if err := r.PingContext(ctx); err != nil {
			_ = r.Close()
			_ = writer.Close()
			return nil, err
		}
		reader = r
	}
	return &Store{dialect: DialectSQLite, writer: writer, reader: reader}, nil
}

func openPostgres(ctx context.Context, dsn string) (*Store, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, errors.New("postgres dsn is required")
	}
	pool, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	pool.SetMaxOpenConns(24)
	pool.SetMaxIdleConns(6)
	pool.SetConnMaxLifetime(30 * time.Minute)
	if err := pool.PingContext(ctx); err != nil {
		_ = pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return &Store{dialect: DialectPostgres, writer: pool, reader: pool}, nil
}

func (s *Store) Dialect() Dialect { return s.dialect }

func (s *Store) Migrate(ctx context.Context, migrationsFS fs.FS) error {
	gd := goose.DialectSQLite3
	if s.dialect == DialectPostgres {
		gd = goose.DialectPostgres
	}
	provider, err := goose.NewProvider(gd, s.writer, migrationsFS)
	if err != nil {
		return fmt.Errorf("init goose provider: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}

func (s *Store) Rebind(query string) string {
	if s.dialect != DialectPostgres {
		return query
	}
	var b strings.Builder
	b.Grow(len(query) + 16)
	idx := 1
	inSingle := false
	for i := 0; i < len(query); i++ {
		ch := query[i]
		if ch == '\'' {
			inSingle = !inSingle
			b.WriteByte(ch)
			continue
		}
		if ch == '?' && !inSingle {
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(idx))
			idx++
			continue
		}
		b.WriteByte(ch)
	}
	return b.String()
}

// Tx wraps *sql.Tx with automatic dialect rebinding & Postgres RLS scope enforcement.
type Tx struct {
	store *Store
	tx    *sql.Tx
	scope Scope
}

func (t *Tx) Scope() Scope     { return t.scope }
func (t *Tx) Dialect() Dialect { return t.store.dialect }

func (t *Tx) Exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return t.tx.ExecContext(ctx, t.store.Rebind(query), args...)
}

func (t *Tx) Query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return t.tx.QueryContext(ctx, t.store.Rebind(query), args...)
}

func (t *Tx) QueryRow(ctx context.Context, query string, args ...any) *sql.Row {
	return t.tx.QueryRowContext(ctx, t.store.Rebind(query), args...)
}

// TryAdvisoryLock acquires a non-blocking transaction-scoped advisory lock on PostgreSQL
// (SQLite single-writer transactions are already serialized cleanly).
func (t *Tx) TryAdvisoryLock(ctx context.Context, key string) (bool, error) {
	if t.store.dialect != DialectPostgres {
		return true, nil
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(key))
	var ok bool
	err := t.tx.QueryRowContext(ctx, "SELECT pg_try_advisory_xact_lock($1)", int64(h.Sum64())).Scan(&ok) // #nosec G115 -- deterministic hash key for PG advisory lock
	return ok, err
}

// WithTx executes fn inside a writer transaction with automatic rollback on error or panic.
func (s *Store) WithTx(ctx context.Context, fn func(ctx context.Context, tx *Tx) error) error {
	return s.withTxOptions(ctx, Scope{}, nil, fn)
}

// WithScopeTx executes fn inside a tenant/app-scoped transaction, injecting SET LOCAL app.tenant_id / app.app_id on PostgreSQL.
func (s *Store) WithScopeTx(ctx context.Context, scope Scope, fn func(ctx context.Context, tx *Tx) error) error {
	if strings.TrimSpace(scope.TenantID) == "" {
		return errors.New("tenant_id is required for scoped transaction")
	}
	return s.withTxOptions(ctx, scope, nil, fn)
}

// WithRepeatableReadTx executes fn inside a consistent snapshot read transaction (for LiveOps D19 job snapshot reads).
func (s *Store) WithRepeatableReadTx(ctx context.Context, scope Scope, fn func(ctx context.Context, tx *Tx) error) error {
	opts := &sql.TxOptions{ReadOnly: true}
	if s.dialect == DialectPostgres {
		opts.Isolation = sql.LevelRepeatableRead
	}
	return s.withTxOptions(ctx, scope, opts, fn)
}

func (s *Store) withTxOptions(ctx context.Context, scope Scope, opts *sql.TxOptions, fn func(ctx context.Context, tx *Tx) error) error {
	pool := s.writer
	if opts != nil && opts.ReadOnly {
		pool = s.reader
	}
	rawTx, err := pool.BeginTx(ctx, opts)
	if err != nil {
		return err
	}
	wtx := &Tx{store: s, tx: rawTx, scope: scope}
	defer func() {
		if p := recover(); p != nil {
			_ = rawTx.Rollback()
			panic(p)
		}
	}()
	if s.dialect == DialectPostgres && scope.TenantID != "" {
		if _, err := rawTx.ExecContext(ctx, "SELECT set_config('app.tenant_id', $1, true)", scope.TenantID); err != nil {
			_ = rawTx.Rollback()
			return fmt.Errorf("inject rls tenant_id: %w", err)
		}
		if scope.AppID != "" {
			if _, err := rawTx.ExecContext(ctx, "SELECT set_config('app.app_id', $1, true)", scope.AppID); err != nil {
				_ = rawTx.Rollback()
				return fmt.Errorf("inject rls app_id: %w", err)
			}
		}
	}
	if err := fn(ctx, wtx); err != nil {
		_ = rawTx.Rollback()
		return err
	}
	return rawTx.Commit()
}

func (s *Store) Exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return s.writer.ExecContext(ctx, s.Rebind(query), args...)
}

func (s *Store) Query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return s.reader.QueryContext(ctx, s.Rebind(query), args...)
}

func (s *Store) QueryRow(ctx context.Context, query string, args ...any) *sql.Row {
	return s.reader.QueryRowContext(ctx, s.Rebind(query), args...)
}

func (s *Store) Ping(ctx context.Context) error { return s.writer.PingContext(ctx) }

func (s *Store) Close() error {
	var e1, e2 error
	if s.reader != nil && s.reader != s.writer {
		e1 = s.reader.Close()
	}
	if s.writer != nil {
		e2 = s.writer.Close()
	}
	return errors.Join(e1, e2)
}

// SafeOrderBy maps requested sort column through a strict allow-list to eliminate ORDER BY SQL injection.
func SafeOrderBy(field, dir string, allow map[string]string, fallback string) string {
	col, ok := allow[strings.ToLower(strings.TrimSpace(field))]
	if !ok {
		return fallback
	}
	if strings.EqualFold(strings.TrimSpace(dir), "asc") {
		return col + " ASC"
	}
	return col + " DESC"
}

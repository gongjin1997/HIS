package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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

// Dialect represents the active SQL database engine.
type Dialect string

const (
	DialectSQLite   Dialect = "sqlite"
	DialectPostgres Dialect = "postgres"
)

// Config controls database startup.
type Config struct {
	Driver string // "sqlite" or "postgres"
	DSN    string // file path (or ":memory:") for sqlite; postgres URL for postgres
}

// Store wraps dual-dialect SQLite (split writer/reader WAL pools) or PostgreSQL pgx/v5 pools.
type Store struct {
	dialect Dialect
	writer  *sql.DB
	reader  *sql.DB
}

// Open initializes the database pools according to cfg.
func Open(ctx context.Context, cfg Config) (*Store, error) {
	driver := strings.ToLower(strings.TrimSpace(cfg.Driver))
	if driver == "" {
		driver = string(DialectSQLite)
	}
	switch Dialect(driver) {
	case DialectSQLite:
		return openSQLite(ctx, cfg.DSN)
	case DialectPostgres:
		return openPostgres(ctx, cfg.DSN)
	default:
		return nil, fmt.Errorf("unsupported database driver %q", cfg.Driver)
	}
}

func openSQLite(ctx context.Context, dsn string) (*Store, error) {
	if strings.TrimSpace(dsn) == "" {
		dsn = ":memory:"
	}
	isMem := dsn == ":memory:" || strings.Contains(dsn, "mode=memory")
	var writerDSN, readerDSN string
	if isMem {
		// Shared-cache in-memory SQLite lets writer and reader share one isolated test DB.
		writerDSN = "file:kinstall_mem?mode=memory&cache=shared&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
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
	writer.SetConnMaxLifetime(0)

	if err := writer.PingContext(ctx); err != nil {
		_ = writer.Close()
		return nil, fmt.Errorf("ping sqlite writer: %w", err)
	}
	if !isMem {
		// Ensure WAL file is initialized before opening read-only pool.
		if _, err := writer.ExecContext(ctx, `PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON;`); err != nil {
			_ = writer.Close()
			return nil, fmt.Errorf("init sqlite pragmas: %w", err)
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
		r.SetConnMaxLifetime(0)
		if err := r.PingContext(ctx); err != nil {
			_ = r.Close()
			_ = writer.Close()
			return nil, fmt.Errorf("ping sqlite reader: %w", err)
		}
		reader = r
	}

	return &Store{
		dialect: DialectSQLite,
		writer:  writer,
		reader:  reader,
	}, nil
}

func openPostgres(ctx context.Context, dsn string) (*Store, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, errors.New("postgres dsn is required")
	}
	pool, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	pool.SetMaxOpenConns(25)
	pool.SetMaxIdleConns(5)
	pool.SetConnMaxLifetime(30 * time.Minute)
	if err := pool.PingContext(ctx); err != nil {
		_ = pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return &Store{
		dialect: DialectPostgres,
		writer:  pool,
		reader:  pool,
	}, nil
}

// Migrate runs forward Goose SQL migrations embedded in migrationsFS.
func (s *Store) Migrate(ctx context.Context, migrationsFS fs.FS) error {
	var gd goose.Dialect
	switch s.dialect {
	case DialectSQLite:
		gd = goose.DialectSQLite3
	case DialectPostgres:
		gd = goose.DialectPostgres
	default:
		return fmt.Errorf("unsupported migration dialect %q", s.dialect)
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

// Dialect returns the active SQL dialect.
func (s *Store) Dialect() Dialect {
	return s.dialect
}

// Rebind translates '?' parameter placeholders to '$1, $2, ...' on PostgreSQL.
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

// Exec runs a write statement on the writer pool.
func (s *Store) Exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return s.writer.ExecContext(ctx, s.Rebind(query), args...)
}

// Query runs a read statement on the reader pool.
func (s *Store) Query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return s.reader.QueryContext(ctx, s.Rebind(query), args...)
}

// QueryRow runs a single-row read statement on the reader pool.
func (s *Store) QueryRow(ctx context.Context, query string, args ...any) *sql.Row {
	return s.reader.QueryRowContext(ctx, s.Rebind(query), args...)
}

// Tx wraps *sql.Tx with automatic dialect rebinding.
type Tx struct {
	store *Store
	tx    *sql.Tx
}

// Exec executes a statement inside the transaction.
func (t *Tx) Exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return t.tx.ExecContext(ctx, t.store.Rebind(query), args...)
}

// Query runs a query inside the transaction.
func (t *Tx) Query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return t.tx.QueryContext(ctx, t.store.Rebind(query), args...)
}

// QueryRow runs a single-row query inside the transaction.
func (t *Tx) QueryRow(ctx context.Context, query string, args ...any) *sql.Row {
	return t.tx.QueryRowContext(ctx, t.store.Rebind(query), args...)
}

// WithTx executes fn inside a writer transaction and rolls back automatically on error or panic.
func (s *Store) WithTx(ctx context.Context, fn func(ctx context.Context, tx *Tx) error) error {
	rawTx, err := s.writer.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	wtx := &Tx{store: s, tx: rawTx}
	defer func() {
		if p := recover(); p != nil {
			_ = rawTx.Rollback()
			panic(p)
		}
	}()
	if err := fn(ctx, wtx); err != nil {
		_ = rawTx.Rollback()
		return err
	}
	return rawTx.Commit()
}

// Ping verifies database connectivity.
func (s *Store) Ping(ctx context.Context) error {
	return s.writer.PingContext(ctx)
}

// Close closes both database pools cleanly.
func (s *Store) Close() error {
	var err1, err2 error
	if s.reader != nil && s.reader != s.writer {
		err1 = s.reader.Close()
	}
	if s.writer != nil {
		err2 = s.writer.Close()
	}
	return errors.Join(err1, err2)
}

// SafeOrderBy maps user-requested sort keys through an explicit allow-list to eliminate SQL injection.
func SafeOrderBy(requested string, allow map[string]string, fallback string) string {
	key := strings.ToLower(strings.TrimSpace(requested))
	if expr, ok := allow[key]; ok {
		return expr
	}
	return fallback
}

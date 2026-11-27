package ports

import (
	"context"
	"database/sql"
)

// QueryExec provides dialect-agnostic parameterized SQL access implemented solely by internal/store.DBStore,
// keeping github.com/uptrace/bun strictly confined to internal/store (R5).
type QueryExec interface {
	ExecRaw(ctx context.Context, query string, args ...any) (sql.Result, error)
	ScanRaw(ctx context.Context, query string, args []any, dest ...any) error
	QueryRaw(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// QueryExecProvider is optionally implemented by Store adapters supporting modular domain services.
type QueryExecProvider interface {
	WriteQE() QueryExec
	ReadQE() QueryExec
}

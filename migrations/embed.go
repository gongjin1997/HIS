package migrations

import (
	"embed"
	"io/fs"

	"github.com/kinstall/kinstall/pkg/platform/db"
)

//go:embed sqlite/*.sql postgres/*.sql
var rawFS embed.FS

// ForDialect returns the embedded Goose SQL migration filesystem for the selected DB dialect.
func ForDialect(d db.Dialect) (fs.FS, error) {
	if d == db.DialectPostgres {
		return fs.Sub(rawFS, "postgres")
	}
	return fs.Sub(rawFS, "sqlite")
}

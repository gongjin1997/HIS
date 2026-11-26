package migrations

import "embed"

// FS embeds forward-only goose SQL migrations for both SQLite and PostgreSQL (D2 §1.3, G1 §6.3).
//
//go:embed sqlite/*.sql postgres/*.sql
var FS embed.FS

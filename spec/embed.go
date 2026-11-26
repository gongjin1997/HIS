package spec

import "embed"

// FS embeds the single-source-of-truth OS matching rules, logical package map,
// and golden test fixtures (ADR-029).
//
//go:embed osmatch.yaml pkgmap.yaml osmatch_golden.yaml
var FS embed.FS

package api

import "embed"

// SpecFS embeds api/openapi.yaml for serving /api/v1/openapi.yaml (05 §1.3).
//
//go:embed openapi.yaml
var SpecFS embed.FS

//go:build !noui

package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var rawDist embed.FS

// DistFS exposes the built Vue 3 SPA assets embedded into the single Go binary (ADR-023, WEB_DESIGN §4.3).
func DistFS() fs.FS {
	sub, err := fs.Sub(rawDist, "dist")
	if err != nil {
		return nil
	}
	return sub
}

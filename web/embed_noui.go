//go:build noui

package web

import "io/fs"

// DistFS returns nil when built with -tags=noui for minimal CLI-only binaries (05 §1.3).
func DistFS() fs.FS { return nil }

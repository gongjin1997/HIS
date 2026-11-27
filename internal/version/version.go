package version

import "runtime"

// Build-time variables injected via -ldflags -X.
var (
	Version = "v0.1.0-dev"
	Commit  = "unknown"
	Date    = "unknown"
)

// MaxCompatibleSchemaVersion is the highest goose schema version this binary can safely write to (G1 §6.2).
const MaxCompatibleSchemaVersion int64 = 4

// Info represents structured build & compatibility metadata.
type Info struct {
	Version                    string `json:"version" yaml:"version"`
	Commit                     string `json:"commit" yaml:"commit"`
	Date                       string `json:"date" yaml:"date"`
	GoVersion                  string `json:"goVersion" yaml:"goVersion"`
	OS                         string `json:"os" yaml:"os"`
	Arch                       string `json:"arch" yaml:"arch"`
	MaxCompatibleSchemaVersion int64  `json:"maxCompatibleSchemaVersion" yaml:"maxCompatibleSchemaVersion"`
}

// Get returns the current binary version metadata.
func Get() Info {
	return Info{
		Version:                    Version,
		Commit:                     Commit,
		Date:                       Date,
		GoVersion:                  runtime.Version(),
		OS:                         runtime.GOOS,
		Arch:                       runtime.GOARCH,
		MaxCompatibleSchemaVersion: MaxCompatibleSchemaVersion,
	}
}

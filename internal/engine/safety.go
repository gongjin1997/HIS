package engine

import (
	"fmt"
	"net/http"
	"path"
	"regexp"
	"strings"

	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

type riskRule struct {
	name    string
	pattern *regexp.Regexp
}

// commandRiskRules implements LiveOps-inspired high-risk script guards (ScriptRiskCheckService)
// blocking catastrophic unguarded destructive commands before remote sudo execution.
var commandRiskRules = []riskRule{
	{
		name:    "root-recursive-delete",
		pattern: regexp.MustCompile(`(?i)\brm\s+[^\n;|&]*-[a-z]*r[a-z]*f[^\n;|&]*\s+/(?:\s|\*|$|;|&&|\|\|)|rm\s+[^\n;|&]*--no-preserve-root`),
	},
	{
		name:    "fork-bomb",
		pattern: regexp.MustCompile(`:\(\)\s*\{\s*:\s*\|\s*:\s*&\s*\}\s*;`),
	},
	{
		name:    "raw-block-device-overwrite",
		pattern: regexp.MustCompile(`(?i)(?:\bdd\b[^\n;|&]*\bof=/dev/(?:sd|vd|xvd|nvme|hd)[a-z0-9]*|(?:^|[;&|]\s*)>\s*/dev/(?:sd|vd|xvd|nvme|hd)[a-z0-9]*)`),
	},
	{
		name:    "root-chmod-777",
		pattern: regexp.MustCompile(`(?i)\bchmod\s+[^\n;|&]*-R[^\n;|&]*\s+777\s+/(?:\s|$|;|&&|\|\|)`),
	},
	{
		name:    "raw-disk-format",
		pattern: regexp.MustCompile(`(?i)\bmkfs(?:\.[a-z0-9]+)?\s+[^\n;|&]*/dev/(?:sd|vd|xvd|nvme|hd)[a-z0-9]*`),
	},
}

// ValidateCommandSafety rejects catastrophic destructive shell commands prior to remote execution.
func ValidateCommandSafety(script string) error {
	trimmed := strings.TrimSpace(script)
	if trimmed == "" {
		return nil
	}
	for _, rule := range commandRiskRules {
		if rule.pattern.MatchString(trimmed) {
			return sdk.NewError(sdk.CodeAPIInvalidRequest, "SAFETY", http.StatusUnprocessableEntity, sdk.CodeAPIInvalidRequest, false,
				fmt.Errorf("blocked high-risk shell command by safety guard (%s)", rule.name))
		}
	}
	return nil
}

var protectedSystemFiles = map[string]bool{
	"/etc/shadow":          true,
	"/etc/gshadow":         true,
	"/etc/passwd":          true,
	"/etc/group":           true,
	"/etc/sudoers":         true,
	"/boot/grub/grub.cfg":  true,
	"/boot/grub2/grub.cfg": true,
	"/etc/fstab":           true,
}

var protectedSystemPrefixes = []string{
	"/proc/",
	"/sys/",
	"/dev/",
}

// ValidateTargetPath enforces LiveOps-inspired remote path safety rules (ProjectPathRules):
// rejects relative paths, path traversal (`..`), and critical OS credential/virtual filesystems.
func ValidateTargetPath(dst string) error {
	if strings.TrimSpace(dst) == "" {
		return sdk.NewError(sdk.CodeAPIInvalidRequest, "SAFETY", http.StatusUnprocessableEntity, sdk.CodeAPIInvalidRequest, false,
			fmt.Errorf("target file path cannot be empty"))
	}
	if !strings.HasPrefix(dst, "/") {
		return sdk.NewError(sdk.CodeAPIInvalidRequest, "SAFETY", http.StatusUnprocessableEntity, sdk.CodeAPIInvalidRequest, false,
			fmt.Errorf("target file path %q must be absolute", dst))
	}
	for _, seg := range strings.Split(dst, "/") {
		if seg == ".." {
			return sdk.NewError(sdk.CodeAPIInvalidRequest, "SAFETY", http.StatusUnprocessableEntity, sdk.CodeAPIInvalidRequest, false,
				fmt.Errorf("target file path %q contains forbidden traversal segment '..'", dst))
		}
	}
	cleaned := path.Clean(dst)
	if cleaned == "/" || protectedSystemFiles[cleaned] {
		return sdk.NewError(sdk.CodeAPIInvalidRequest, "SAFETY", http.StatusUnprocessableEntity, sdk.CodeAPIInvalidRequest, false,
			fmt.Errorf("target file path %q is a protected system file", cleaned))
	}
	for _, pfx := range protectedSystemPrefixes {
		if strings.HasPrefix(cleaned, pfx) || cleaned == strings.TrimSuffix(pfx, "/") {
			return sdk.NewError(sdk.CodeAPIInvalidRequest, "SAFETY", http.StatusUnprocessableEntity, sdk.CodeAPIInvalidRequest, false,
				fmt.Errorf("target file path %q targets protected kernel/device filesystem %s", cleaned, pfx))
		}
	}
	return nil
}

// collisionFreeBackupPath generates a deterministic per-operation backup path preserving full directory structure
// so modifying two identically named files in different directories never overwrites the earlier backup.
func collisionFreeBackupPath(opID, dst string) string {
	rel := strings.TrimPrefix(path.Clean(dst), "/")
	if rel == "" {
		rel = "rootfile"
	}
	return path.Join("/var/lib/kinstall/backups", opID, rel+".bak")
}

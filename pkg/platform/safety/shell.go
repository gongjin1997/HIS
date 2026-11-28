package safety

import (
	"fmt"
	"regexp"
	"strings"
)

type rule struct {
	name string
	re   *regexp.Regexp
}

var blockedShellRules = []rule{
	{name: "recursive root deletion", re: regexp.MustCompile(`(?i)\brm\s+-[a-z]*r[a-z]*f[a-z]*\s+(/(\*|\s|$)|/\.\.)|\brm\s+-[a-z]*f[a-z]*r[a-z]*\s+(/(\*|\s|$)|/\.\.)`)},
	{name: "filesystem format (mkfs)", re: regexp.MustCompile(`(?i)\bmkfs(\.[a-z0-9]+)?\b`)},
	{name: "raw block device write (dd)", re: regexp.MustCompile(`(?i)\bdd\b[^\n]*\bof=/dev/(sd|vd|xvd|nvme|hd|mmcblk)`)},
	{name: "block device truncation", re: regexp.MustCompile(`>\s*/dev/(sd|vd|xvd|nvme|hd)`)},
	{name: "fork bomb", re: regexp.MustCompile(`:\(\)\s*\{\s*:\s*\|\s*:\s*&\s*\}\s*;\s*:`)},
	{name: "recursive world-writable root", re: regexp.MustCompile(`(?i)\bchmod\s+(-R|--recursive)\s+777\s+/(\s|$)`)},
	{name: "reverse shell /dev/tcp", re: regexp.MustCompile(`(?i)/dev/tcp/[0-9a-zA-Z.-]+/[0-9]+`)},
	{name: "shadow file exfiltration", re: regexp.MustCompile(`(?i)\b(cat|head|tail|base64|curl|nc|scp)\b[^\n]*/etc/shadow`)},
	{name: "untrusted remote script pipe", re: regexp.MustCompile(`(?i)\b(curl|wget)\b[^\n|]+\|\s*(ba)?sh\b`)},
}

// CheckCommand enforces execution safety guardrails on ad-hoc shell commands.
func CheckCommand(cmd string) error {
	trimmed := strings.TrimSpace(cmd)
	if trimmed == "" {
		return fmt.Errorf("command cannot be empty")
	}
	for _, r := range blockedShellRules {
		if r.re.MatchString(trimmed) {
			return fmt.Errorf("command blocked by safety guardrail (%s)", r.name)
		}
	}
	return nil
}

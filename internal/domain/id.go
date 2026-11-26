package domain

import (
	"crypto/rand"
	"regexp"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
)

// Canonical prefixed ULID prefixes (05 §1.4, D2 §1.3).
// Every ID fits comfortably within VARCHAR(48).
const (
	PrefixProject     = "prj_"
	PrefixCredential  = "cred_"
	PrefixHost        = "host_"
	PrefixStack       = "stack_"
	PrefixInstance    = "inst_"
	PrefixClaim       = "claim_"
	PrefixPlan        = "plan_"
	PrefixOperation   = "op_"
	PrefixTaskRun     = "tr_"
	PrefixUser        = "usr_"
	PrefixSession     = "sess_"
	PrefixAPIToken    = "tok_"
	PrefixMediaPolicy = "mp_"
	PrefixStateBackup = "bk_"
	PrefixNotifyChan  = "nc_"
	PrefixNotifyDeliv = "nd_"
	PrefixKubeconfig  = "kc_"
)

// NewID generates a time-ordered prefixed ULID using crypto/rand entropy.
func NewID(prefix string) string {
	id := ulid.MustNew(ulid.Timestamp(time.Now().UTC()), rand.Reader)
	return prefix + id.String()
}

var dns1123Re = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`)

// ValidateDNS1123 validates host/instance/project names per 03 §5.6.1.
func ValidateDNS1123(name string) bool {
	if len(name) == 0 || len(name) > 63 {
		return false
	}
	return dns1123Re.MatchString(name)
}

// NormalizeDNS1123 lowercases and trims whitespace.
func NormalizeDNS1123(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

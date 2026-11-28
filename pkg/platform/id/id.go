package id

import (
	crand "crypto/rand"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

var (
	entropyMu sync.Mutex
	entropy   io.Reader
)

func getEntropy() io.Reader {
	if entropy == nil {
		entropy = ulid.Monotonic(crand.Reader, 0)
	}
	return entropy
}

// Raw returns a lowercase monotonic ULID string.
func Raw() string {
	entropyMu.Lock()
	defer entropyMu.Unlock()
	v := ulid.MustNew(ulid.Timestamp(time.Now().UTC()), getEntropy())
	return strings.ToLower(v.String())
}

// New returns a prefixed monotonic ULID (e.g. "ci_01j...").
func New(prefix string) string {
	p := strings.TrimSuffix(strings.TrimSpace(prefix), "_")
	if p == "" {
		return Raw()
	}
	return p + "_" + Raw()
}

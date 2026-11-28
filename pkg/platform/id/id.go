package id

import (
	crand "crypto/rand"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

// Clock abstracts time retrieval for deterministic testing and lease/cursor invariants.
type Clock interface {
	Now() time.Time
}

// RealClock returns wall-clock UTC time.
type RealClock struct{}

func (RealClock) Now() time.Time { return time.Now().UTC() }

// FixedClock provides a controllable clock for deterministic unit & invariant tests.
type FixedClock struct {
	mu  sync.Mutex
	cur time.Time
}

func NewFixedClock(t time.Time) *FixedClock {
	return &FixedClock{cur: t.UTC()}
}

func (c *FixedClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cur
}

func (c *FixedClock) Advance(d time.Duration) time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cur = c.cur.Add(d)
	return c.cur
}

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

// Raw returns a lowercase monotonic ULID opaque identifier.
func Raw() string {
	entropyMu.Lock()
	defer entropyMu.Unlock()
	return strings.ToLower(ulid.MustNew(ulid.Timestamp(time.Now().UTC()), getEntropy()).String())
}

// New returns a domain-prefixed opaque ID (e.g. "ten_", "app_", "job_", "cfg_").
func New(prefix string) string {
	p := strings.TrimSuffix(strings.TrimSpace(prefix), "_")
	if p == "" {
		return Raw()
	}
	return p + "_" + Raw()
}

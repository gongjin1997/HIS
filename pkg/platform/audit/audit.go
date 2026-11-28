package audit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
)

const RedactedPlaceholder = "***REDACTED***"

var sensitiveKeywords = []string{
	"password", "passwd", "secret", "token", "private_key", "privatekey",
	"passphrase", "authorization", "cookie", "master_key", "api_key", "apikey",
	"credential", "cipher",
}

// IsSensitiveKey reports whether a structured log attribute key may hold secret material.
func IsSensitiveKey(key string) bool {
	k := strings.ToLower(strings.TrimSpace(key))
	for _, kw := range sensitiveKeywords {
		if strings.Contains(k, kw) {
			return true
		}
	}
	return false
}

// RedactingHandler wraps any slog.Handler to automatically scrub sensitive attributes.
type RedactingHandler struct {
	inner slog.Handler
}

// NewRedactingHandler returns an slog.Handler that masks secrets before emission.
func NewRedactingHandler(inner slog.Handler) *RedactingHandler {
	return &RedactingHandler{inner: inner}
}

func (h *RedactingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *RedactingHandler) Handle(ctx context.Context, r slog.Record) error {
	clean := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	r.Attrs(func(a slog.Attr) bool {
		clean.AddAttrs(redactAttr(a))
		return true
	})
	return h.inner.Handle(ctx, clean)
}

func (h *RedactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		out[i] = redactAttr(a)
	}
	return &RedactingHandler{inner: h.inner.WithAttrs(out)}
}

func (h *RedactingHandler) WithGroup(name string) slog.Handler {
	return &RedactingHandler{inner: h.inner.WithGroup(name)}
}

func redactAttr(a slog.Attr) slog.Attr {
	if IsSensitiveKey(a.Key) {
		return slog.String(a.Key, RedactedPlaceholder)
	}
	if a.Value.Kind() == slog.KindGroup {
		g := a.Value.Group()
		next := make([]slog.Attr, len(g))
		for i, sub := range g {
			next[i] = redactAttr(sub)
		}
		return slog.Attr{Key: a.Key, Value: slog.GroupValue(next...)}
	}
	return a
}

// Entry represents one tamper-evident row in the append-only SHA-256 audit hash chain.
type Entry struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	Actor       string `json:"actor"`
	ActorRole   string `json:"actorRole"`
	Action      string `json:"action"`
	TargetType  string `json:"targetType"`
	TargetID    string `json:"targetId"`
	Status      string `json:"status"`
	Detail      string `json:"detail"`
	SourceIP    string `json:"sourceIp"`
	PrevHash    string `json:"prevHash"`
	Hash        string `json:"hash"`
	CreatedAt   string `json:"createdAt"`
}

// ComputeHash computes the canonical SHA-256 digest linking e to e.PrevHash.
func ComputeHash(e Entry) string {
	canonical := fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s|%s|%s|%s|%s",
		e.PrevHash,
		e.ID,
		e.CreatedAt,
		e.WorkspaceID,
		e.Actor,
		e.ActorRole,
		e.Action,
		e.TargetType,
		e.TargetID,
		e.Status,
		e.Detail,
	)
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])
}

// VerifyChain validates the entire ordered (oldest -> newest) audit chain.
// Returns (true, -1, "") when intact, or (false, index, reason) on tampering.
func VerifyChain(entries []Entry) (bool, int, string) {
	prev := "GENESIS"
	for i, e := range entries {
		if e.PrevHash != prev {
			return false, i, fmt.Sprintf("broken prevHash link at index %d (id=%s)", i, e.ID)
		}
		expected := ComputeHash(e)
		if e.Hash != expected {
			return false, i, fmt.Sprintf("hash mismatch at index %d (id=%s)", i, e.ID)
		}
		prev = e.Hash
	}
	return true, -1, ""
}

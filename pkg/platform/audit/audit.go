package audit

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
)

const Redacted = "***REDACTED***"

var sensitiveSubstrings = []string{
	"password", "passwd", "secret", "token", "private_key", "privatekey",
	"passphrase", "authorization", "cookie", "apikey", "api_key", "appkey",
	"ak", "sk", "credential", "cipher",
}

// RedactingHandler wraps any slog.Handler to automatically scrub sensitive keys before emission.
type RedactingHandler struct {
	next slog.Handler
}

func NewRedactingHandler(next slog.Handler) *RedactingHandler {
	return &RedactingHandler{next: next}
}

func (h *RedactingHandler) Enabled(ctx context.Context, lvl slog.Level) bool {
	return h.next.Enabled(ctx, lvl)
}

func (h *RedactingHandler) Handle(ctx context.Context, r slog.Record) error {
	clean := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	r.Attrs(func(a slog.Attr) bool {
		clean.AddAttrs(redactAttr(a))
		return true
	})
	return h.next.Handle(ctx, clean)
}

func (h *RedactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		out[i] = redactAttr(a)
	}
	return &RedactingHandler{next: h.next.WithAttrs(out)}
}

func (h *RedactingHandler) WithGroup(name string) slog.Handler {
	return &RedactingHandler{next: h.next.WithGroup(name)}
}

func redactAttr(a slog.Attr) slog.Attr {
	k := strings.ToLower(strings.TrimSpace(a.Key))
	for _, sub := range sensitiveSubstrings {
		if k == sub || strings.Contains(k, sub) && len(sub) > 2 {
			return slog.String(a.Key, Redacted)
		}
	}
	if a.Value.Kind() == slog.KindGroup {
		g := a.Value.Group()
		out := make([]slog.Attr, len(g))
		for i, sub := range g {
			out[i] = redactAttr(sub)
		}
		return slog.Attr{Key: a.Key, Value: slog.GroupValue(out...)}
	}
	return a
}

// Record represents a tamper-evident append-only audit row linked via HMAC-SHA256 + prev_hash.
type Record struct {
	ID            string `json:"id"`
	TenantID      string `json:"tenantId"`
	AppID         string `json:"appId"`
	ActorID       string `json:"actorId"`
	ActorUsername string `json:"actorUsername"`
	ClientIP      string `json:"clientIp"`
	TraceID       string `json:"traceId"`
	Module        string `json:"module"`
	Action        string `json:"action"`
	ResourceType  string `json:"resourceType"`
	ResourceID    string `json:"resourceId"`
	HTTPStatus    int    `json:"httpStatus"`
	Summary       string `json:"summary"`
	PrevHash      string `json:"prevHash"`
	HMACSignature string `json:"hmacSignature"`
	OccurredAt    string `json:"occurredAt"`
}

func ComputeSignature(secret []byte, r Record) string {
	canonical := fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s|%s|%s|%s|%s|%d|%s|%s",
		r.PrevHash, r.ID, r.OccurredAt, r.TenantID, r.AppID,
		r.ActorID, r.ActorUsername, r.TraceID, r.Module, r.Action,
		r.ResourceType, r.HTTPStatus, r.ResourceID, r.Summary,
	)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(canonical))
	return hex.EncodeToString(mac.Sum(nil))
}

func VerifyChain(secret []byte, records []Record) (valid bool, brokenAt int, reason string) {
	prev := "GENESIS"
	for i, r := range records {
		if r.PrevHash != prev {
			return false, i, fmt.Sprintf("broken prevHash at index %d (id=%s)", i, r.ID)
		}
		expected := ComputeSignature(secret, r)
		if !hmac.Equal([]byte(expected), []byte(r.HMACSignature)) {
			return false, i, fmt.Sprintf("hmac tamper detected at index %d (id=%s)", i, r.ID)
		}
		prev = r.HMACSignature
	}
	return true, -1, ""
}

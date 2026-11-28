package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/kinstall/kinstall/pkg/platform/id"
)

const DefaultMaxJSONBytes = 10 << 20 // 10 MiB

type traceCtxKey struct{}

// TraceIDFromContext returns the request trace ID.
func TraceIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(traceCtxKey{}).(string); ok && v != "" {
		return v
	}
	return ""
}

// TraceMiddleware injects an X-Request-Id trace identifier onto every request/response.
func TraceMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tid := strings.TrimSpace(r.Header.Get("X-Request-Id"))
		if tid == "" || len(tid) > 64 {
			tid = id.New("req")
		}
		w.Header().Set("X-Request-Id", tid)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), traceCtxKey{}, tid)))
	})
}

// SecurityHeaders applies strict enterprise security headers (zero external CDN allowed).
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		h.Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data: blob:; font-src 'self' data:; "+
				"connect-src 'self' ws: wss:; frame-ancestors 'none'; base-uri 'self'")
		next.ServeHTTP(w, r)
	})
}

// ErrorBody is the canonical error JSON envelope.
type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`
	TraceID string `json:"traceId,omitempty"`
}

// ListBody wraps collection responses with total count.
type ListBody[T any] struct {
	Items []T `json:"items"`
	Total int `json:"total"`
}

// WriteJSON serializes v as UTF-8 JSON.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// WriteList serializes a collection response, guaranteeing a non-null JSON array.
func WriteList[T any](w http.ResponseWriter, items []T, total int) {
	if items == nil {
		items = make([]T, 0)
	}
	WriteJSON(w, http.StatusOK, ListBody[T]{Items: items, Total: total})
}

// WriteError emits a structured JSON error response.
func WriteError(w http.ResponseWriter, status int, code, message string, detail ...string) {
	body := ErrorBody{
		Code:    code,
		Message: message,
		TraceID: w.Header().Get("X-Request-Id"),
	}
	if len(detail) > 0 {
		body.Detail = strings.Join(detail, "; ")
	}
	WriteJSON(w, status, body)
}

// DecodeStrictJSON decodes a bounded JSON request body while rejecting trailing garbage.
func DecodeStrictJSON(w http.ResponseWriter, r *http.Request, dst any, maxBytes int64) error {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxJSONBytes
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			return errors.New("请求体不能为空")
		}
		return fmt.Errorf("JSON 解析失败: %w", err)
	}
	return nil
}

// SSEWriter streams Server-Sent Events cleanly over HTTP/1.1 or HTTP/2.
type SSEWriter struct {
	w http.ResponseWriter
	f http.Flusher
}

// NewSSEWriter prepares SSE headers and returns a flusher-backed writer.
func NewSSEWriter(w http.ResponseWriter) (*SSEWriter, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, errors.New("streaming unsupported by http writer")
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream; charset=utf-8")
	h.Set("Cache-Control", "no-cache, no-transform")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	return &SSEWriter{w: w, f: flusher}, nil
}

// Send writes one SSE frame and flushes immediately.
func (s *SSEWriter) Send(eventID, eventName string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if eventID != "" {
		if _, err := fmt.Fprintf(s.w, "id: %s\n", eventID); err != nil {
			return err
		}
	}
	if eventName != "" {
		if _, err := fmt.Fprintf(s.w, "event: %s\n", eventName); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(s.w, "data: %s\n\n", raw); err != nil {
		return err
	}
	s.f.Flush()
	return nil
}

// Ping emits an SSE keepalive comment.
func (s *SSEWriter) Ping() error {
	if _, err := fmt.Fprint(s.w, ": keepalive\n\n"); err != nil {
		return err
	}
	s.f.Flush()
	return nil
}

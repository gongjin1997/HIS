package httpx

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/kinstall/kinstall/pkg/platform/id"
)

const DefaultMaxJSONBytes = 10 << 20 // 10 MiB

type traceCtxKey struct{}

func TraceIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(traceCtxKey{}).(string); ok {
		return v
	}
	return ""
}

func TraceMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tid := strings.TrimSpace(r.Header.Get("X-Trace-Id"))
		if tid == "" {
			tid = strings.TrimSpace(r.Header.Get("X-Request-Id"))
		}
		if tid == "" || len(tid) > 64 {
			tid = id.New("tr")
		}
		w.Header().Set("X-Trace-Id", tid)
		w.Header().Set("X-Request-Id", tid)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), traceCtxKey{}, tid)))
	})
}

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

// ErrorBody matches the LiveOps Go v1 standard error contract (03-新旧协议与演进.md).
type ErrorBody struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	TraceID string         `json:"traceId,omitempty"`
	Details map[string]any `json:"details,omitempty"`
}

// ListBody matches the LiveOps Go v1 cursor-paginated response contract.
type ListBody[T any] struct {
	Items      []T     `json:"items"`
	NextCursor *string `json:"nextCursor"`
	Total      int     `json:"total"`
}

func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func WriteList[T any](w http.ResponseWriter, items []T, nextCursor *string, total int) {
	if items == nil {
		items = make([]T, 0)
	}
	WriteJSON(w, http.StatusOK, ListBody[T]{Items: items, NextCursor: nextCursor, Total: total})
}

func WriteError(w http.ResponseWriter, status int, code, message string, details map[string]any) {
	WriteJSON(w, status, ErrorBody{
		Code:    code,
		Message: message,
		TraceID: w.Header().Get("X-Trace-Id"),
		Details: details,
	})
}

// SetVersionETag writes a strong ETag header (`"<version>"`) for optimistic concurrency.
func SetVersionETag(w http.ResponseWriter, version int64) {
	w.Header().Set("ETag", fmt.Sprintf(`"%d"`, version))
}

// ParseIfMatch parses a mandatory strong ETag from If-Match (`"7"` -> 7).
// Returns status 428 PRECONDITION_REQUIRED when missing or 400 BAD_REQUEST when malformed.
func ParseIfMatch(r *http.Request) (version int64, status int, code string, err error) {
	raw := strings.TrimSpace(r.Header.Get("If-Match"))
	if raw == "" {
		return 0, http.StatusPreconditionRequired, "PRECONDITION_REQUIRED", errors.New("写操作必须携带 If-Match 强版本头")
	}
	cleaned := strings.Trim(raw, `"`)
	if strings.HasPrefix(raw, "W/") {
		return 0, http.StatusBadRequest, "INVALID_IF_MATCH", errors.New("不支持弱 ETag，请提供强版本号")
	}
	v, perr := strconv.ParseInt(cleaned, 10, 64)
	if perr != nil || v < 0 {
		return 0, http.StatusBadRequest, "INVALID_IF_MATCH", errors.New("If-Match 版本格式无效")
	}
	return v, 0, "", nil
}

// ParseIdempotencyKey validates the 1-128 printable ASCII Idempotency-Key header.
func ParseIdempotencyKey(r *http.Request, required bool) (string, error) {
	k := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if k == "" {
		k = strings.TrimSpace(r.Header.Get("X-Idempotency-Key"))
	}
	if k == "" {
		if required {
			return "", errors.New("缺少必填请求头 Idempotency-Key")
		}
		return "", nil
	}
	if len(k) > 128 {
		return "", errors.New("Idempotency-Key 长度不得超过 128 字符")
	}
	for i := 0; i < len(k); i++ {
		if k[i] < 0x21 || k[i] > 0x7e {
			return "", errors.New("Idempotency-Key 仅允许可见 ASCII 字符")
		}
	}
	return k, nil
}

// DecodeStrictJSON reads bounded JSON, rejects unknown top-level fields & trailing tokens, and returns raw canonical digest bytes.
func DecodeStrictJSON(w http.ResponseWriter, r *http.Request, dst any, maxBytes int64) (string, error) {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxJSONBytes
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return "", fmt.Errorf("读取请求体失败: %w", err)
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return "", errors.New("请求体不能为空")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return "", fmt.Errorf("JSON 参数校验失败: %w", err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return "", errors.New("JSON 请求体包含非法尾随内容")
	}
	// Canonicalize digest by re-encoding decoded value (Go json.Marshal sorts map keys deterministically).
	canon, err := json.Marshal(dst)
	if err != nil {
		canon = raw
	}
	sum := sha256.Sum256(canon)
	return hex.EncodeToString(sum[:]), nil
}

// ValidateSSECursor implements LiveOps Go v1 U01-U09 strict SSE cursor validation:
//   - Missing cursor -> 409 SNAPSHOT_REQUIRED
//   - Negative or non-int64 / overflow -> 400 INVALID_CURSOR
//   - Outside [minSeq-1, lastSeq] -> 410 CURSOR_RESET_REQUIRED
func ValidateSSECursor(rawCursor string, minSeq, lastSeq int64) (cursor int64, status int, code, msg string) {
	raw := strings.TrimSpace(rawCursor)
	if raw == "" {
		return 0, http.StatusConflict, "SNAPSHOT_REQUIRED", "订阅事件流前必须先拉取快照并携带 Last-Event-ID 游标"
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || v < 0 {
		return 0, http.StatusBadRequest, "INVALID_CURSOR", "游标必须为非负 64 位整数"
	}
	// Empty retention window: minSeq = lastSeq + 1, valid cursor is lastSeq (0 for empty job).
	if minSeq <= 0 {
		minSeq = lastSeq + 1
	}
	lower := minSeq - 1
	if lower < 0 {
		lower = 0
	}
	if v < lower || v > lastSeq {
		return 0, http.StatusGone, "CURSOR_RESET_REQUIRED", "事件游标超出保留窗口或领先当前水位，请重新拉取快照"
	}
	return v, http.StatusOK, "", ""
}

// SSEWriter streams Server-Sent Events with numeric seq IDs and id-less keepalive heartbeats.
type SSEWriter struct {
	w http.ResponseWriter
	f http.Flusher
}

func NewSSEWriter(w http.ResponseWriter) (*SSEWriter, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, errors.New("http writer does not support streaming")
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

func (s *SSEWriter) SendEvent(seq int64, eventName string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if seq > 0 {
		if _, err := fmt.Fprintf(s.w, "id: %d\n", seq); err != nil {
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

// Heartbeat writes an id-less SSE keepalive frame so business seq is never polluted (U03).
func (s *SSEWriter) Heartbeat() error {
	if _, err := fmt.Fprint(s.w, ": heartbeat\n\n"); err != nil {
		return err
	}
	s.f.Flush()
	return nil
}

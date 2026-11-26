package server

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
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/kinstall/kinstall/internal/authz"
	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/internal/i18n"
	"github.com/kinstall/kinstall/internal/ports"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

type ctxKey string

const (
	ctxKeyTraceID ctxKey = "trace_id"
	ctxKeyActor   ctxKey = "actor"
)

// ProblemDetails implements RFC 9457 bilingual error responses (G1 §2.1, 05 §5.2).
type ProblemDetails struct {
	Type       string  `json:"type"`
	Title      string  `json:"title"`
	Status     int     `json:"status"`
	Detail     string  `json:"detail"`
	Code       string  `json:"code"`
	Instance   string  `json:"instance"`
	TraceID    string  `json:"traceId"`
	Suggestion *string `json:"suggestion"`
}

func TraceIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyTraceID).(string)
	return v
}

func ActorFromContext(ctx context.Context) domain.Actor {
	v, _ := ctx.Value(ctxKeyActor).(domain.Actor)
	return v
}

func traceIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tid := r.Header.Get("X-Trace-ID")
		if tid == "" {
			tid = domain.NewID("trc_")
		}
		w.Header().Set("X-Trace-ID", tid)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKeyTraceID, tid)))
	})
}

func writeProblem(w http.ResponseWriter, r *http.Request, cat *i18n.Catalog, err error) {
	status := http.StatusInternalServerError
	code := "KI-API-0001"
	detail := err.Error()
	var ce *sdk.Error
	if errors.As(err, &ce) {
		if ce.HTTPStatus > 0 {
			status = ce.HTTPStatus
		}
		if ce.Code != "" {
			code = ce.Code
		}
	}
	lang := r.Header.Get("Accept-Language")
	title := code
	var sugPtr *string
	if cat != nil {
		title = cat.Translate(lang, code, nil)
		if s := cat.Translate(lang, code+".suggestion", nil); s != "" && s != code+".suggestion" {
			sugPtr = &s
		}
	}
	pd := ProblemDetails{
		Type:       "https://kinstall.io/errors/" + strings.ToLower(code),
		Title:      title,
		Status:     status,
		Detail:     detail,
		Code:       code,
		Instance:   r.URL.Path,
		TraceID:    TraceIDFromContext(r.Context()),
		Suggestion: sugPtr,
	}
	w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(pd)
}

func authMiddleware(authSvc *authz.Service, cat *i18n.Catalog, devBypass bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var rawTok string
			if hdr := r.Header.Get("Authorization"); strings.HasPrefix(hdr, "Bearer ") {
				rawTok = strings.TrimSpace(strings.TrimPrefix(hdr, "Bearer "))
			} else if ck, err := r.Cookie("kinstall_session"); err == nil {
				rawTok = ck.Value
			}
			if rawTok == "" {
				if devBypass {
					actor := domain.Actor{
						ID: "usr_dev_admin", Username: "admin", Type: "system", SourceIP: r.RemoteAddr,
						RoleBindings: []domain.RoleBinding{{ProjectID: "*", Role: domain.RoleAdmin}},
					}
					next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKeyActor, actor)))
					return
				}
				writeProblem(w, r, cat, sdk.NewError(sdk.CodeAuthUnauthenticated, "AUTH", http.StatusUnauthorized, sdk.CodeAuthUnauthenticated, false, fmt.Errorf("missing credentials")))
				return
			}
			actor, err := authSvc.AuthenticateBearer(r.Context(), rawTok, r.RemoteAddr)
			if err != nil {
				writeProblem(w, r, cat, err)
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKeyActor, *actor)))
		})
	}
}

type respRecorder struct {
	http.ResponseWriter
	status int
	buf    bytes.Buffer
}

func (r *respRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *respRecorder) Write(p []byte) (int, error) {
	_, _ = r.buf.Write(p)
	return r.ResponseWriter.Write(p)
}

// idempotencyMiddleware implements G1 §2.2:
// - http.MaxBytesReader(4 MiB) -> 413 Request Entity Too Large
// - PK (project_id, actor_id, key_hash) 24h dedup
// - Same key + different body/endpoint -> 409 KI-API-0002
// - In-progress duplicate -> 409 Retry-After: 1
// - context.WithoutCancel prevents stuck in_progress on early client disconnect
// - 5xx aborts key to allow safe retry
func idempotencyMiddleware(repo ports.IdempotencyRepo, cat *i18n.Catalog) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
			if repo == nil || key == "" || (r.Method != http.MethodPost && r.Method != http.MethodPut && r.Method != http.MethodPatch) {
				next.ServeHTTP(w, r)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
			rawBody, err := io.ReadAll(r.Body)
			if err != nil {
				var maxErr *http.MaxBytesError
				if errors.As(err, &maxErr) {
					w.WriteHeader(http.StatusRequestEntityTooLarge)
					return
				}
				writeProblem(w, r, cat, err)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(rawBody))

			reqSum := sha256.Sum256(rawBody)
			reqHash := hex.EncodeToString(reqSum[:])
			keySum := sha256.Sum256([]byte(key))
			keyHash := hex.EncodeToString(keySum[:])

			actorID := ActorFromContext(r.Context()).ID
			if actorID == "" {
				actorID = "anonymous"
			}
			projectID := chi.URLParam(r, "projectId")
			if projectID == "" {
				projectID = "_"
			}
			endpoint := r.Method + " " + r.URL.Path

			existing, created, err := repo.Acquire(r.Context(), domain.IdempotencyRecord{
				ProjectID:   projectID,
				ActorID:     actorID,
				KeyHash:     keyHash,
				Endpoint:    endpoint,
				RequestHash: reqHash,
				ExpiresAt:   time.Now().UTC().Add(24 * time.Hour),
			})
			if err != nil {
				writeProblem(w, r, cat, err)
				return
			}
			if !created {
				if existing.RequestHash != reqHash || existing.Endpoint != endpoint {
					writeProblem(w, r, cat, sdk.NewError(sdk.CodeAPIIdempotencyConflict, "API", http.StatusConflict, sdk.CodeAPIIdempotencyConflict, false,
						fmt.Errorf("Idempotency-Key reused with different request payload or endpoint")))
					return
				}
				if existing.Status == "in_progress" {
					w.Header().Set("Retry-After", "1")
					writeProblem(w, r, cat, sdk.NewError(sdk.CodeAPIIdempotencyProgress, "API", http.StatusConflict, sdk.CodeAPIIdempotencyProgress, true,
						fmt.Errorf("request with same Idempotency-Key still in progress")))
					return
				}
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.Header().Set("X-Idempotency-Replayed", "true")
				w.WriteHeader(existing.ResponseCode)
				_, _ = w.Write([]byte(existing.ResponseBody))
				return
			}

			rec := &respRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)

			persistCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
			defer cancel()
			if rec.status >= 500 {
				_ = repo.Abort(persistCtx, projectID, actorID, keyHash)
				return
			}
			_ = repo.Complete(persistCtx, projectID, actorID, keyHash, rec.status, rec.buf.String())
		})
	}
}

func formatETag(version int64) string {
	return fmt.Sprintf(`"v-%d"`, version)
}

func checkIfMatch(r *http.Request, currentVersion int64) error {
	hdr := strings.TrimSpace(r.Header.Get("If-Match"))
	if hdr == "" || hdr == "*" {
		return nil
	}
	want := formatETag(currentVersion)
	if hdr != want {
		return sdk.NewError(sdk.CodeAPIETagConflict, "API", http.StatusPreconditionFailed, sdk.CodeAPIETagConflict, true,
			fmt.Errorf("If-Match %s does not match current ETag %s", hdr, want))
	}
	return nil
}

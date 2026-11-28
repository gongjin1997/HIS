package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/kinstall/kinstall/pkg/platform/httpx"
)

// LiveOps Default-Deny Permission Codes (aligned with 02.1 API Spec & 03-新旧协议与演进).
const (
	PermIAMRead          = "iam:read"
	PermIAMWrite         = "iam:rbac:admin"
	PermIAMAppAdmin      = "iam:app:admin"
	PermIAMAuditRead     = "iam:audit:read"
	PermIAMAuditVerify   = "iam:audit:verify"
	PermAssetsRead       = "assets:read"
	PermAssetsWrite      = "assets:write"
	PermConfigRead       = "config:read"
	PermConfigWrite      = "config:write"
	PermConfigReveal     = "config:secret:reveal"
	PermDeployJobSubmit  = "deploy.job.submit"
	PermDeployJobRead    = "deploy.job.read"
	PermDeployJobObserve = "deploy.job.observe"
	PermDeployJobCancel  = "deploy.job.cancel"
	PermNodesRead        = "nodes:read"
	PermNodesAdmin       = "nodes:admin"
	PermGatewayRead      = "gateway:route:read"
	PermGatewayWrite     = "gateway:route:write"
	PermGatewayMonitor   = "gateway:monitor:read"
	PermPlatformAdmin    = "platform:admin"
)

// TrustedContext represents verified server-side identity & multi-tenant scope (NEVER trusted from raw client headers).
type TrustedContext struct {
	TenantID     string              `json:"tenantId"`
	AppID        string              `json:"appId"`
	ProjectID    string              `json:"projectId"`
	SubjectID    string              `json:"subjectId"`
	Username     string              `json:"username"`
	DisplayName  string              `json:"displayName"`
	Roles        []string            `json:"roles"`
	Perms        map[string]struct{} `json:"-"`
	PermList     []string            `json:"permissions"`
	ScopeVersion int64               `json:"scopeVersion"`
	AuthKind     string              `json:"authKind"` // "session" | "appkey" | "gateway_hmac"
	TraceID      string              `json:"traceId"`
}

func (t TrustedContext) HasPerm(perm string) bool {
	if t.Perms == nil {
		return false
	}
	if _, wild := t.Perms["*"]; wild {
		return true
	}
	_, ok := t.Perms[perm]
	return ok
}

type ctxKey struct{}

// WithContext injects TrustedContext into ctx.
func WithContext(ctx context.Context, tc TrustedContext) context.Context {
	return context.WithValue(ctx, ctxKey{}, tc)
}

// FromContext retrieves the verified TrustedContext from ctx.
func FromContext(ctx context.Context) (TrustedContext, bool) {
	tc, ok := ctx.Value(ctxKey{}).(TrustedContext)
	return tc, ok && tc.SubjectID != ""
}

type sessionClaims struct {
	TenantID     string   `json:"tid"`
	AppID        string   `json:"aid"`
	ProjectID    string   `json:"pid"`
	SubjectID    string   `json:"sub"`
	Username     string   `json:"usr"`
	DisplayName  string   `json:"name"`
	Roles        []string `json:"roles"`
	ScopeVersion int64    `json:"sv"`
	IssuedAt     int64    `json:"iat"`
	ExpiresAt    int64    `json:"exp"`
}

// TokenManager signs and verifies stateless HMAC-SHA256 session tokens.
type TokenManager struct {
	signingKey []byte
	ttl        time.Duration
}

func NewTokenManager(signingKey []byte, ttl time.Duration) *TokenManager {
	if ttl <= 0 {
		ttl = 8 * time.Hour
	}
	return &TokenManager{signingKey: append([]byte(nil), signingKey...), ttl: ttl}
}

func (m *TokenManager) Issue(tc TrustedContext) (string, time.Time, error) {
	now := time.Now().UTC()
	exp := now.Add(m.ttl)
	hdr := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	raw, err := json.Marshal(sessionClaims{
		TenantID:     tc.TenantID,
		AppID:        tc.AppID,
		ProjectID:    tc.ProjectID,
		SubjectID:    tc.SubjectID,
		Username:     tc.Username,
		DisplayName:  tc.DisplayName,
		Roles:        tc.Roles,
		ScopeVersion: tc.ScopeVersion,
		IssuedAt:     now.Unix(),
		ExpiresAt:    exp.Unix(),
	})
	if err != nil {
		return "", time.Time{}, err
	}
	payload := base64.RawURLEncoding.EncodeToString(raw)
	sig := m.sign(hdr + "." + payload)
	return hdr + "." + payload + "." + sig, exp, nil
}

func (m *TokenManager) Verify(token string) (*TrustedContext, error) {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 3 {
		return nil, errors.New("malformed session token")
	}
	if !hmac.Equal([]byte(m.sign(parts[0]+"."+parts[1])), []byte(parts[2])) {
		return nil, errors.New("invalid token signature")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, errors.New("invalid token payload")
	}
	var c sessionClaims
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, errors.New("invalid token claims")
	}
	if time.Now().UTC().Unix() > c.ExpiresAt {
		return nil, errors.New("session token expired")
	}
	return &TrustedContext{
		TenantID:     c.TenantID,
		AppID:        c.AppID,
		ProjectID:    c.ProjectID,
		SubjectID:    c.SubjectID,
		Username:     c.Username,
		DisplayName:  c.DisplayName,
		Roles:        c.Roles,
		ScopeVersion: c.ScopeVersion,
		AuthKind:     "session",
	}, nil
}

func (m *TokenManager) sign(input string) string {
	mac := hmac.New(sha256.New, m.signingKey)
	mac.Write([]byte(input))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// SignGatewayHeaders generates X-LiveOps-Gw-Sig over trusted internal identity headers.
func SignGatewayHeaders(gwSecret []byte, tenantID, appID, subjectID, username, permsCSV string) string {
	mac := hmac.New(sha256.New, gwSecret)
	mac.Write([]byte(tenantID + "|" + appID + "|" + subjectID + "|" + username + "|" + permsCSV))
	return hex.EncodeToString(mac.Sum(nil))
}

// ScopeResolver loads live authoritative permissions & scopeVersion for an authenticated subject
// so role revocations take effect immediately (zero stale-token authorization window!).
type ScopeResolver func(ctx context.Context, subjectID, reqTenantID, reqAppID, reqProjectID string) (*TrustedContext, error)

// AppKeyResolver authenticates dual-track hashed AppKeys ("lo_live_...").
type AppKeyResolver func(ctx context.Context, rawKey string) (*TrustedContext, error)

// Authenticate middleware validates Bearer JWT / HttpOnly Cookie / AppKey / Gateway HMAC signature,
// strips spoofed client headers, and resolves live authoritative RBAC permissions.
func Authenticate(mgr *TokenManager, gwSecret []byte, scopeRes ScopeResolver, appKeyRes AppKeyResolver) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 1. Check trusted internal gateway signature first (if present).
			if gwSig := strings.TrimSpace(r.Header.Get("X-LiveOps-Gw-Sig")); gwSig != "" && len(gwSecret) > 0 {
				tid := r.Header.Get("X-LiveOps-Tenant-Id")
				aid := r.Header.Get("X-LiveOps-App-Id")
				sub := r.Header.Get("X-LiveOps-User-Id")
				usr := r.Header.Get("X-LiveOps-Username")
				permsRaw := r.Header.Get("X-LiveOps-Perms")
				expected := SignGatewayHeaders(gwSecret, tid, aid, sub, usr, permsRaw)
				if !hmac.Equal([]byte(expected), []byte(gwSig)) {
					httpx.WriteError(w, http.StatusUnauthorized, "AUTH_SIG_INVALID", "网关内部签名校验失败", nil)
					return
				}
				pset, plist := parsePermsCSV(permsRaw)
				tc := TrustedContext{
					TenantID: tid, AppID: aid, ProjectID: r.Header.Get("X-LiveOps-Project-Id"),
					SubjectID: sub, Username: usr, DisplayName: usr,
					Perms: pset, PermList: plist, AuthKind: "gateway_hmac",
					TraceID: httpx.TraceIDFromContext(r.Context()),
				}
				next.ServeHTTP(w, r.WithContext(WithContext(r.Context(), tc)))
				return
			}

			// 2. Extract Bearer token, query token (for EventSource/WS), or HttpOnly cookie.
			raw := extractCredential(r)
			if raw == "" {
				httpx.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "缺少有效认证凭据，请先登录", nil)
				return
			}

			var (
				tc  *TrustedContext
				err error
			)
			if strings.HasPrefix(raw, "lo_live_") && appKeyRes != nil {
				tc, err = appKeyRes(r.Context(), raw)
			} else {
				tc, err = mgr.Verify(raw)
				if err == nil && tc != nil && scopeRes != nil {
					reqTenant := strings.TrimSpace(r.Header.Get("X-Tenant-Id"))
					if reqTenant == "" {
						reqTenant = tc.TenantID
					}
					reqApp := strings.TrimSpace(r.Header.Get("X-App-Id"))
					if reqApp == "" {
						reqApp = tc.AppID
					}
					reqProj := strings.TrimSpace(r.Header.Get("X-Project-Id"))
					if reqProj == "" {
						reqProj = tc.ProjectID
					}
					tc, err = scopeRes(r.Context(), tc.SubjectID, reqTenant, reqApp, reqProj)
				}
			}
			if err != nil || tc == nil {
				httpx.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "认证凭据无效或已吊销", nil)
				return
			}
			tc.TraceID = httpx.TraceIDFromContext(r.Context())
			next.ServeHTTP(w, r.WithContext(WithContext(r.Context(), *tc)))
		})
	}
}

// RequirePerm enforces LiveOps Default-Deny RBAC: missing context -> 401, missing permission -> 403.
func RequirePerm(permCode string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tc, ok := FromContext(r.Context())
			if !ok {
				httpx.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "未认证请求", nil)
				return
			}
			if !tc.HasPerm(permCode) {
				httpx.WriteError(w, http.StatusForbidden, "AUTH_FORBIDDEN", "当前身份无权执行该操作", map[string]any{
					"requiredPermission": permCode,
					"scopeVersion":       tc.ScopeVersion,
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func extractCredential(r *http.Request) string {
	authz := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(authz) > 7 && strings.EqualFold(authz[:7], "Bearer ") {
		return strings.TrimSpace(authz[7:])
	}
	if q := strings.TrimSpace(r.URL.Query().Get("access_token")); q != "" {
		return q
	}
	if c, err := r.Cookie("liveops_session"); err == nil && strings.TrimSpace(c.Value) != "" {
		return strings.TrimSpace(c.Value)
	}
	return ""
}

func parsePermsCSV(csv string) (map[string]struct{}, []string) {
	set := make(map[string]struct{})
	list := make([]string, 0)
	for _, p := range strings.Split(csv, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			if _, exists := set[p]; !exists {
				set[p] = struct{}{}
				list = append(list, p)
			}
		}
	}
	return set, list
}

// EvaluateProgressiveLockout computes the LiveOps login failure lockout policy (3->30s, 6->180s, 9->DISABLED).
func EvaluateProgressiveLockout(failCount int, now time.Time) (status string, lockedUntil time.Time) {
	switch {
	case failCount >= 9:
		return "DISABLED", time.Time{}
	case failCount >= 6:
		return "LOCKED", now.Add(180 * time.Second)
	case failCount >= 3:
		return "LOCKED", now.Add(30 * time.Second)
	default:
		return "ACTIVE", time.Time{}
	}
}

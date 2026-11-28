package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/kinstall/kinstall/pkg/platform/httpx"
)

const (
	RoleAdmin    = "admin"
	RoleOperator = "operator"
	RoleViewer   = "viewer"
	RoleAuditor  = "auditor"
)

// Permission constants follow resource:action naming.
const (
	PermCMDBRead        = "cmdb:read"
	PermCMDBWrite       = "cmdb:write"
	PermConfigRead      = "config:read"
	PermConfigWrite     = "config:write"
	PermConfigReveal    = "config:reveal"
	PermDeliveryRead    = "delivery:read"
	PermDeliveryWrite   = "delivery:write"
	PermDeliveryExecute = "delivery:execute"
	PermOpsRead         = "ops:read"
	PermOpsExecute      = "ops:execute"
	PermCatalogRead     = "catalog:read"
	PermCatalogWrite    = "catalog:write"
	PermIAMRead         = "iam:read"
	PermIAMWrite        = "iam:write"
	PermAuditRead       = "audit:read"
	PermAuditVerify     = "audit:verify"
)

// Principal represents the authenticated caller inside request context.
type Principal struct {
	UserID      string `json:"userId"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	Role        string `json:"role"`
	WorkspaceID string `json:"workspaceId"`
	TokenType   string `json:"tokenType"` // "session" or "pat"
}

type claims struct {
	Sub         string `json:"sub"`
	Username    string `json:"usr"`
	DisplayName string `json:"name"`
	Role        string `json:"role"`
	WorkspaceID string `json:"ws"`
	IssuedAt    int64  `json:"iat"`
	ExpiresAt   int64  `json:"exp"`
}

// Manager issues and validates stateless HMAC-SHA256 tokens.
type Manager struct {
	signingKey []byte
	ttl        time.Duration
}

// NewManager creates an auth token manager with a 24h default session TTL.
func NewManager(signingKey []byte, ttl time.Duration) *Manager {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	cp := make([]byte, len(signingKey))
	copy(cp, signingKey)
	return &Manager{signingKey: cp, ttl: ttl}
}

// Issue signs a compact HMAC-SHA256 JWT token for p.
func (m *Manager) Issue(p Principal) (string, time.Time, error) {
	now := time.Now().UTC()
	exp := now.Add(m.ttl)
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payloadBytes, err := json.Marshal(claims{
		Sub:         p.UserID,
		Username:    p.Username,
		DisplayName: p.DisplayName,
		Role:        p.Role,
		WorkspaceID: p.WorkspaceID,
		IssuedAt:    now.Unix(),
		ExpiresAt:   exp.Unix(),
	})
	if err != nil {
		return "", time.Time{}, err
	}
	payload := base64.RawURLEncoding.EncodeToString(payloadBytes)
	signingInput := header + "." + payload
	sig := m.sign(signingInput)
	return signingInput + "." + sig, exp, nil
}

// Verify parses and validates a compact token string.
func (m *Manager) Verify(token string) (*Principal, error) {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 3 {
		return nil, errors.New("malformed token")
	}
	expectedSig := m.sign(parts[0] + "." + parts[1])
	if !hmac.Equal([]byte(expectedSig), []byte(parts[2])) {
		return nil, errors.New("invalid token signature")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, errors.New("invalid token payload")
	}
	var c claims
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, errors.New("invalid token claims")
	}
	if time.Now().UTC().Unix() > c.ExpiresAt {
		return nil, errors.New("token expired")
	}
	return &Principal{
		UserID:      c.Sub,
		Username:    c.Username,
		DisplayName: c.DisplayName,
		Role:        c.Role,
		WorkspaceID: c.WorkspaceID,
		TokenType:   "session",
	}, nil
}

func (m *Manager) sign(input string) string {
	mac := hmac.New(sha256.New, m.signingKey)
	mac.Write([]byte(input))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// HasPermission implements default-deny RBAC authorization.
func HasPermission(role, perm string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case RoleAdmin:
		return true
	case RoleOperator:
		switch perm {
		case PermCMDBRead, PermCMDBWrite,
			PermConfigRead, PermConfigWrite, PermConfigReveal,
			PermDeliveryRead, PermDeliveryWrite, PermDeliveryExecute,
			PermOpsRead, PermOpsExecute,
			PermCatalogRead, PermCatalogWrite,
			PermIAMRead, PermAuditRead:
			return true
		default:
			return false
		}
	case RoleAuditor:
		switch perm {
		case PermCMDBRead, PermConfigRead, PermDeliveryRead, PermOpsRead, PermCatalogRead,
			PermIAMRead, PermAuditRead, PermAuditVerify:
			return true
		default:
			return false
		}
	case RoleViewer:
		switch perm {
		case PermCMDBRead, PermConfigRead, PermDeliveryRead, PermOpsRead, PermCatalogRead:
			return true
		default:
			return false
		}
	default:
		return false
	}
}

type ctxKey struct{}

// WithPrincipal stores p in ctx.
func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

// PrincipalFromContext retrieves the authenticated principal from ctx.
func PrincipalFromContext(ctx context.Context) (*Principal, bool) {
	p, ok := ctx.Value(ctxKey{}).(*Principal)
	return p, ok && p != nil
}

// PATLookup resolves personal API tokens ("ki_pat_...") into Principals.
type PATLookup func(ctx context.Context, rawToken string) (*Principal, error)

// Authenticate middleware validates Authorization Bearer or ?token= (for SSE/WebSocket).
func Authenticate(mgr *Manager, patLookup PATLookup) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := extractBearer(r)
			if raw == "" {
				httpx.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "缺少认证令牌，请先登录")
				return
			}
			var (
				p   *Principal
				err error
			)
			if strings.HasPrefix(raw, "ki_pat_") && patLookup != nil {
				p, err = patLookup(r.Context(), raw)
			} else {
				p, err = mgr.Verify(raw)
			}
			if err != nil || p == nil {
				httpx.WriteError(w, http.StatusUnauthorized, "INVALID_TOKEN", "令牌无效或已过期，请重新登录")
				return
			}
			// Allow callers with workspace header to scope requests cleanly.
			if ws := strings.TrimSpace(r.Header.Get("X-Workspace-Id")); ws != "" {
				pCopy := *p
				pCopy.WorkspaceID = ws
				p = &pCopy
			}
			next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), p)))
		})
	}
}

// RequirePerm enforces default-deny permission checks on an endpoint.
func RequirePerm(perm string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, ok := PrincipalFromContext(r.Context())
			if !ok {
				httpx.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "未认证请求")
				return
			}
			if !HasPermission(p.Role, perm) {
				httpx.WriteError(w, http.StatusForbidden, "FORBIDDEN", "当前角色无权执行此操作", "missing permission: "+perm)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func extractBearer(r *http.Request) string {
	authz := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(authz) > 7 && strings.EqualFold(authz[:7], "Bearer ") {
		return strings.TrimSpace(authz[7:])
	}
	if q := strings.TrimSpace(r.URL.Query().Get("token")); q != "" {
		return q
	}
	return ""
}

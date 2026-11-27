package authz

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"

	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/internal/ports"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// OWASP 2026 Argon2id parameter baseline (G1 §4.1).
const (
	argonTime    uint32 = 3
	argonMemory  uint32 = 64 * 1024 // 64 MiB
	argonThreads uint8  = 4
	argonKeyLen  uint32 = 32
	argonSaltLen        = 16

	MaxFailedLogins = 5
	LockoutDuration = 15 * time.Minute
	MaxTokenTTL     = 365 * 24 * time.Hour
	DefaultTokenTTL = 90 * 24 * time.Hour
)

// HashPassword produces an Argon2id PHC string using OWASP 2026 baseline parameters.
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

// VerifyPassword compares password against a PHC Argon2id hash in constant time.
func VerifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var m, t uint32
	var p uint8
	for _, kv := range strings.Split(parts[3], ",") {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			return false
		}
		n, err := strconv.ParseUint(v, 10, 32)
		if err != nil {
			return false
		}
		switch k {
		case "m":
			m = uint32(n)
		case "t":
			t = uint32(n)
		case "p":
			if n > 255 {
				return false
			}
			p = uint8(n)
		}
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(want) == 0 {
		return false
	}
	// #nosec G115 -- keyLen bounded by stored hash length (<=64 bytes)
	got := argon2.IDKey([]byte(password), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

const base62Alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

func randomBase62(n int) (string, error) {
	out := make([]byte, n)
	maxVal := big.NewInt(int64(len(base62Alphabet)))
	for i := range out {
		v, err := rand.Int(rand.Reader, maxVal)
		if err != nil {
			return "", err
		}
		out[i] = base62Alphabet[v.Int64()]
	}
	return string(out), nil
}

// GeneratePKCEChallenge computes S256 PKCE verifier and challenge for OIDC Auth Code flow (G1 §4.1).
func GeneratePKCEChallenge() (verifier, challenge string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	verifier = base64.RawURLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	return verifier, challenge, nil
}

// Service implements three-in-one auth (OIDC + Argon2id local + scoped API tokens) and RBAC (G1 §4).
type Service struct {
	repo  ports.AuthRepo
	audit ports.AuditRecorder
	oidc  *OIDCProvider
}

func NewService(repo ports.AuthRepo, audit ports.AuditRecorder) *Service {
	return &Service{repo: repo, audit: audit}
}

// ConfigureOIDC attaches an active OIDC PKCE provider for SSO login (T-M3-06).
func (s *Service) ConfigureOIDC(p *OIDCProvider) { s.oidc = p }

// OIDC returns the configured OIDC provider, or nil if OIDC SSO is disabled.
func (s *Service) OIDC() *OIDCProvider { return s.oidc }

// RevokeSessionByToken immediately revokes a server-side session token on logout (G1 §4.1).
func (s *Service) RevokeSessionByToken(ctx context.Context, rawToken string) error {
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" || strings.HasPrefix(rawToken, "ki_") {
		return nil
	}
	sum := sha256.Sum256([]byte(rawToken))
	sess, err := s.repo.GetSessionByTokenHash(ctx, hex.EncodeToString(sum[:]))
	if err != nil {
		return nil //nolint:nilerr // logout is idempotent when session is already revoked or missing
	}
	return s.repo.RevokeSession(ctx, sess.ID)
}

// CreateLocalUser creates a local account with Argon2id hashed password.
func (s *Service) CreateLocalUser(ctx context.Context, username, displayName, password string, bindings []domain.RoleBinding) (*domain.User, error) {
	hash, err := HashPassword(password)
	if err != nil {
		return nil, err
	}
	u := &domain.User{
		ID:           domain.NewID(domain.PrefixUser),
		Username:     username,
		DisplayName:  displayName,
		AuthSource:   "local",
		PasswordHash: hash,
		RoleBindings: bindings,
	}
	if err := s.repo.CreateUser(ctx, u); err != nil {
		return nil, err
	}
	return u, nil
}

// LoginLocal verifies Argon2id credentials, enforces cross-replica 5-failure 15-minute 423 lockout, and creates a session.
func (s *Service) LoginLocal(ctx context.Context, username, password, sourceIP, userAgent string) (sessionToken string, user *domain.User, err error) {
	u, err := s.repo.GetUserByUsername(ctx, username)
	if err != nil {
		return "", nil, sdk.NewError(sdk.CodeAuthUnauthenticated, "AUTH", http.StatusUnauthorized, sdk.CodeAuthUnauthenticated, false, fmt.Errorf("invalid credentials"))
	}
	if u.Disabled {
		return "", nil, sdk.NewError(sdk.CodeAuthForbidden, "AUTH", http.StatusForbidden, sdk.CodeAuthForbidden, false, fmt.Errorf("account disabled"))
	}
	now := time.Now().UTC()
	if u.LockedUntil != nil && u.LockedUntil.After(now) {
		return "", nil, sdk.NewError(sdk.CodeAuthAccountLocked, "AUTH", http.StatusLocked, sdk.CodeAuthAccountLocked, false,
			fmt.Errorf("account locked until %s", u.LockedUntil.Format(time.RFC3339)))
	}
	if !VerifyPassword(password, u.PasswordHash) {
		attempts, lockedUntil, _ := s.repo.RecordLoginFailure(ctx, u.ID, MaxFailedLogins, LockoutDuration)
		if s.audit != nil {
			_, _ = s.audit.Append(ctx, domain.AuditEntry{
				ActorID: u.ID, ActorType: "user", SourceIP: sourceIP,
				Action: "auth.login", TargetType: "user", TargetID: u.ID, Result: "denied",
				ErrCode: sdk.CodeAuthUnauthenticated, PayloadJSON: fmt.Sprintf(`{"attempts":%d}`, attempts),
			})
		}
		if lockedUntil != nil {
			return "", nil, sdk.NewError(sdk.CodeAuthAccountLocked, "AUTH", http.StatusLocked, sdk.CodeAuthAccountLocked, false,
				fmt.Errorf("account locked after %d failed attempts", attempts))
		}
		return "", nil, sdk.NewError(sdk.CodeAuthUnauthenticated, "AUTH", http.StatusUnauthorized, sdk.CodeAuthUnauthenticated, false, fmt.Errorf("invalid credentials"))
	}
	_ = s.repo.ResetLoginFailures(ctx, u.ID)

	rawTok, err := randomBase62(40)
	if err != nil {
		return "", nil, err
	}
	sum := sha256.Sum256([]byte(rawTok))
	sess := &domain.UserSession{
		ID:        domain.NewID(domain.PrefixSession),
		UserID:    u.ID,
		TokenHash: hex.EncodeToString(sum[:]),
		SourceIP:  sourceIP,
		UserAgent: userAgent,
		ExpiresAt: now.Add(12 * time.Hour),
	}
	if err := s.repo.CreateSession(ctx, sess); err != nil {
		return "", nil, err
	}
	return rawTok, u, nil
}

// CreateAPIToken generates a scoped short-lived token (ki_<8>_<32>), storing ONLY SHA-256 hash in DB (G1 §4.1).
func (s *Service) CreateAPIToken(ctx context.Context, userID string, projectID *string, name string, scopes []string, ttl time.Duration) (plaintext string, meta *domain.APIToken, err error) {
	if ttl <= 0 {
		ttl = DefaultTokenTTL
	}
	if ttl > MaxTokenTTL {
		return "", nil, sdk.NewError(sdk.CodeAPIInvalidRequest, "API", http.StatusUnprocessableEntity, sdk.CodeAPIInvalidRequest, false,
			fmt.Errorf("API token TTL exceeds hard maximum of 365 days"))
	}
	pfx, err := randomBase62(8)
	if err != nil {
		return "", nil, err
	}
	sec, err := randomBase62(32)
	if err != nil {
		return "", nil, err
	}
	full := fmt.Sprintf("ki_%s_%s", pfx, sec)
	sum := sha256.Sum256([]byte(full))
	tok := &domain.APIToken{
		ID:          domain.NewID(domain.PrefixAPIToken),
		UserID:      userID,
		ProjectID:   projectID,
		Name:        name,
		TokenPrefix: "ki_" + pfx,
		TokenHash:   hex.EncodeToString(sum[:]),
		Scopes:      scopes,
		ExpiresAt:   time.Now().UTC().Add(ttl),
	}
	if err := s.repo.CreateAPIToken(ctx, tok); err != nil {
		return "", nil, err
	}
	return full, tok, nil
}

// AuthenticateBearer resolves either an API token (ki_...) or a session token into an authenticated domain.Actor.
func (s *Service) AuthenticateBearer(ctx context.Context, rawToken, sourceIP string) (*domain.Actor, error) {
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" {
		return nil, sdk.NewError(sdk.CodeAuthUnauthenticated, "AUTH", http.StatusUnauthorized, sdk.CodeAuthUnauthenticated, false, fmt.Errorf("missing bearer token"))
	}
	sum := sha256.Sum256([]byte(rawToken))
	hash := hex.EncodeToString(sum[:])
	now := time.Now().UTC()

	if strings.HasPrefix(rawToken, "ki_") {
		tok, err := s.repo.GetAPITokenByHash(ctx, hash)
		if err != nil || tok.RevokedAt != nil || tok.ExpiresAt.Before(now) {
			return nil, sdk.NewError(sdk.CodeAuthUnauthenticated, "AUTH", http.StatusUnauthorized, sdk.CodeAuthUnauthenticated, false, fmt.Errorf("invalid or expired API token"))
		}
		u, err := s.repo.GetUserByID(ctx, tok.UserID)
		if err != nil || u.Disabled {
			return nil, sdk.NewError(sdk.CodeAuthUnauthenticated, "AUTH", http.StatusUnauthorized, sdk.CodeAuthUnauthenticated, false, fmt.Errorf("token owner unavailable"))
		}
		return &domain.Actor{ID: u.ID, Username: u.Username, Type: "token", SourceIP: sourceIP, RoleBindings: u.RoleBindings, Scopes: tok.Scopes}, nil
	}

	sess, err := s.repo.GetSessionByTokenHash(ctx, hash)
	if err != nil || sess.RevokedAt != nil || sess.ExpiresAt.Before(now) {
		return nil, sdk.NewError(sdk.CodeAuthUnauthenticated, "AUTH", http.StatusUnauthorized, sdk.CodeAuthUnauthenticated, false, fmt.Errorf("invalid or expired session"))
	}
	u, err := s.repo.GetUserByID(ctx, sess.UserID)
	if err != nil || u.Disabled {
		return nil, sdk.NewError(sdk.CodeAuthUnauthenticated, "AUTH", http.StatusUnauthorized, sdk.CodeAuthUnauthenticated, false, fmt.Errorf("session user unavailable"))
	}
	return &domain.Actor{ID: u.ID, Username: u.Username, Type: "user", SourceIP: sourceIP, RoleBindings: u.RoleBindings}, nil
}

// Authorize evaluates the 5-role RBAC permission matrix (05 §4.3).
// action examples: "platform.admin", "project.write", "host.write", "instance.apply", "operation.approve", "audit.read", "resource.read".
func Authorize(actor domain.Actor, projectID, action string) error {
	if actor.Type == "cli" || actor.Type == "system" {
		return nil
	}
	best := effectiveRole(actor.RoleBindings, projectID)
	if best == "" {
		return sdk.NewError(sdk.CodeAuthForbidden, "AUTH", http.StatusForbidden, sdk.CodeAuthForbidden, false,
			fmt.Errorf("actor %s has no role in project %s", actor.Username, projectID))
	}
	allowed := false
	switch action {
	case "platform.admin":
		allowed = best == domain.RoleAdmin
	case "project.write", "operation.approve":
		allowed = best == domain.RoleAdmin || best == domain.RoleProjectAdmin
	case "host.write", "instance.apply", "instance.write", "stack.write", "media.write":
		allowed = best == domain.RoleAdmin || best == domain.RoleProjectAdmin || best == domain.RoleOperator
	case "audit.read":
		allowed = best == domain.RoleAdmin || best == domain.RoleProjectAdmin || best == domain.RoleAuditor
	case "resource.read":
		allowed = true
	default:
		allowed = best == domain.RoleAdmin
	}
	if !allowed {
		return sdk.NewError(sdk.CodeAuthForbidden, "AUTH", http.StatusForbidden, sdk.CodeAuthForbidden, false,
			fmt.Errorf("role %s denied action %s on project %s", best, action, projectID))
	}
	return nil
}

func roleRank(r domain.Role) int {
	switch r {
	case domain.RoleAdmin:
		return 5
	case domain.RoleProjectAdmin:
		return 4
	case domain.RoleOperator:
		return 3
	case domain.RoleAuditor:
		return 2
	case domain.RoleViewer:
		return 1
	default:
		return 0
	}
}

func effectiveRole(bindings []domain.RoleBinding, projectID string) domain.Role {
	var best domain.Role
	for _, b := range bindings {
		if b.Role == domain.RoleAdmin || b.ProjectID == "*" || b.ProjectID == projectID {
			if roleRank(b.Role) > roleRank(best) {
				best = b.Role
			}
		}
	}
	return best
}

// RequiresApproval checks whether an action triggers the mandatory four-eyes approval gate (G1 §4.2, 05 §4.3).
func RequiresApproval(env, action string, forceOverride, skipVerify bool) bool {
	if forceOverride || skipVerify {
		return true
	}
	switch strings.ToLower(action) {
	case "etcd-restore", "ca-rotate", "uninstall", "upgrade":
		return true
	}
	return strings.EqualFold(env, "prod") && (action == "apply" || action == "install" || action == "scale")
}

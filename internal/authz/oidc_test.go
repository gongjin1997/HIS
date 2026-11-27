package authz_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"

	"github.com/kinstall/kinstall/internal/authz"
	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/internal/store"
)

func TestOIDCPKCES256EndToEndWithMockIdP(t *testing.T) {
	ctx := context.Background()
	st, err := store.OpenAndMigrate(ctx, store.Config{Driver: "sqlite", DSN: filepath.Join(t.TempDir(), "oidc.db")}, nil)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = st.Close() }()

	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa key: %v", err)
	}
	jwkPub := jose.JSONWebKey{Key: &rsaKey.PublicKey, KeyID: "kid-1", Algorithm: string(jose.RS256), Use: "sig"}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: rsaKey}, (&jose.SignerOptions{}).WithHeader("kid", "kid-1").WithType("JWT"))
	if err != nil {
		t.Fatalf("signer: %v", err)
	}

	var expectedChallenge, issuedNonce string
	var idpURL string
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                 idpURL,
			"authorization_endpoint": idpURL + "/authorize",
			"token_endpoint":         idpURL + "/token",
			"jwks_uri":               idpURL + "/jwks",
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{jwkPub}})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		verifier := r.FormValue("code_verifier")
		sum := sha256.Sum256([]byte(verifier))
		gotChallenge := base64.RawURLEncoding.EncodeToString(sum[:])
		if gotChallenge != expectedChallenge {
			http.Error(w, "pkce challenge mismatch", http.StatusBadRequest)
			return
		}
		claimsBytes, _ := json.Marshal(map[string]any{
			"iss":                idpURL,
			"sub":                "corp-sub-42",
			"aud":                "kinstall-web",
			"exp":                time.Now().Add(10 * time.Minute).Unix(),
			"iat":                time.Now().Unix(),
			"nonce":              issuedNonce,
			"preferred_username": "alice.ops",
			"name":               "Alice Ops",
			"groups":             []string{"platform-admins"},
		})
		obj, _ := signer.Sign(claimsBytes)
		compact, _ := obj.CompactSerialize()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "mock-access",
			"token_type":   "Bearer",
			"expires_in":   600,
			"id_token":     compact,
		})
	})
	idp := httptest.NewServer(mux)
	defer idp.Close()
	idpURL = idp.URL

	svc := authz.NewService(st.Auth(), nil)
	prov, err := authz.NewOIDCProvider(ctx, authz.OIDCConfig{
		IssuerURL:   idpURL,
		ClientID:    "kinstall-web",
		RedirectURL: "http://localhost:8080/api/v1/auth/oidc/callback",
		AdminGroup:  "platform-admins",
	}, idp.Client())
	if err != nil {
		t.Fatalf("new oidc provider: %v", err)
	}
	svc.ConfigureOIDC(prov)

	// 1. BeginAuth returns S256 PKCE URL
	authURL, state, err := prov.BeginAuth()
	if err != nil {
		t.Fatalf("begin auth: %v", err)
	}
	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("parse auth url: %v", err)
	}
	if u.Query().Get("code_challenge_method") != "S256" {
		t.Fatalf("expected S256 PKCE method")
	}
	expectedChallenge = u.Query().Get("code_challenge")
	issuedNonce = u.Query().Get("nonce")

	// 2. CompleteAuth exchanges code+verifier, verifies RS256 ID token + nonce, JIT-provisions admin user
	sessTok, user, err := prov.CompleteAuth(ctx, svc, state, "auth-code-123", "10.0.0.5", "ChromeTest")
	if err != nil {
		t.Fatalf("complete auth: %v", err)
	}
	if user.AuthSource != "oidc" || user.Username != "alice.ops" {
		t.Fatalf("unexpected OIDC user: %+v", user)
	}
	if len(user.RoleBindings) == 0 || user.RoleBindings[0].Role != domain.RoleAdmin {
		t.Fatalf("expected admin group mapping, got %+v", user.RoleBindings)
	}

	// 3. Replay of consumed state MUST fail
	if _, _, err := prov.CompleteAuth(ctx, svc, state, "auth-code-123", "10.0.0.5", "ChromeTest"); err == nil {
		t.Fatalf("replayed OIDC state must be rejected")
	}

	// 4. Session authenticates via AuthenticateBearer and stops working after RevokeSessionByToken
	actor, err := svc.AuthenticateBearer(ctx, sessTok, "10.0.0.5")
	if err != nil || actor.Username != "alice.ops" {
		t.Fatalf("authenticate OIDC session failed: %v %+v", err, actor)
	}
	if err := svc.RevokeSessionByToken(ctx, sessTok); err != nil {
		t.Fatalf("revoke session: %v", err)
	}
	if _, err := svc.AuthenticateBearer(ctx, sessTok, "10.0.0.5"); err == nil {
		t.Fatalf("revoked OIDC session must be rejected")
	}
}

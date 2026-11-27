package authz

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/kinstall/kinstall/internal/domain"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// OIDCConfig configures Authorization Code + PKCE S256 OIDC SSO (G1 §4.1, T-M3-06).
type OIDCConfig struct {
	IssuerURL    string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Scopes       []string
	DefaultRole  domain.Role
	AdminGroup   string
}

type oidcPendingState struct {
	verifier  string
	nonce     string
	expiresAt time.Time
}

// OIDCProvider manages OIDC discovery, PKCE S256 state verification, ID Token verification, and JIT provisioning.
type OIDCProvider struct {
	cfg        OIDCConfig
	provider   *oidc.Provider
	verifier   *oidc.IDTokenVerifier
	oauth2Cfg  oauth2.Config
	httpClient *http.Client

	mu      sync.Mutex
	pending map[string]oidcPendingState
}

// NewOIDCProvider discovers issuer metadata via coreos/go-oidc/v3 and prepares an OIDC PKCE provider.
func NewOIDCProvider(ctx context.Context, cfg OIDCConfig, hc *http.Client) (*OIDCProvider, error) {
	if strings.TrimSpace(cfg.IssuerURL) == "" || strings.TrimSpace(cfg.ClientID) == "" {
		return nil, fmt.Errorf("oidc issuerURL and clientID are required")
	}
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}
	if len(cfg.Scopes) == 0 {
		cfg.Scopes = []string{oidc.ScopeOpenID, "profile", "email"}
	}
	if cfg.DefaultRole == "" {
		cfg.DefaultRole = domain.RoleViewer
	}
	pCtx := oidc.ClientContext(ctx, hc)
	prov, err := oidc.NewProvider(pCtx, cfg.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("oidc discover %s: %w", cfg.IssuerURL, err)
	}
	return &OIDCProvider{
		cfg:        cfg,
		provider:   prov,
		verifier:   prov.Verifier(&oidc.Config{ClientID: cfg.ClientID}),
		httpClient: hc,
		oauth2Cfg: oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURL,
			Endpoint:     prov.Endpoint(),
			Scopes:       cfg.Scopes,
		},
		pending: make(map[string]oidcPendingState),
	}, nil
}

// Issuer returns the configured OIDC issuer URL.
func (p *OIDCProvider) Issuer() string { return p.cfg.IssuerURL }

// BeginAuth starts an OIDC Authorization Code + PKCE S256 login flow.
func (p *OIDCProvider) BeginAuth() (authURL, state string, err error) {
	verifier, challenge, err := GeneratePKCEChallenge()
	if err != nil {
		return "", "", err
	}
	state, err = randomBase62(32)
	if err != nil {
		return "", "", err
	}
	nonce, err := randomBase62(32)
	if err != nil {
		return "", "", err
	}
	now := time.Now().UTC()
	p.mu.Lock()
	for k, v := range p.pending {
		if now.After(v.expiresAt) {
			delete(p.pending, k)
		}
	}
	p.pending[state] = oidcPendingState{
		verifier:  verifier,
		nonce:     nonce,
		expiresAt: now.Add(10 * time.Minute),
	}
	p.mu.Unlock()

	authURL = p.oauth2Cfg.AuthCodeURL(
		state,
		oauth2.SetAuthURLParam("code_challenge", challenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
		oidc.Nonce(nonce),
	)
	return authURL, state, nil
}

// CompleteAuth exchanges the authorization code with the PKCE code_verifier, verifies the ID Token signature & nonce,
// JIT-provisions or loads the user, and creates a revocable DB UserSession.
func (p *OIDCProvider) CompleteAuth(ctx context.Context, svc *Service, state, code, sourceIP, userAgent string) (string, *domain.User, error) {
	p.mu.Lock()
	entry, ok := p.pending[state]
	delete(p.pending, state) // single-use state prevents replay attacks
	p.mu.Unlock()

	if !ok || time.Now().UTC().After(entry.expiresAt) {
		return "", nil, sdk.NewError(sdk.CodeAuthUnauthenticated, "AUTH", http.StatusUnauthorized, sdk.CodeAuthUnauthenticated, false,
			fmt.Errorf("invalid or expired OIDC state"))
	}
	if strings.TrimSpace(code) == "" {
		return "", nil, sdk.NewError(sdk.CodeAuthUnauthenticated, "AUTH", http.StatusUnauthorized, sdk.CodeAuthUnauthenticated, false,
			fmt.Errorf("missing OIDC authorization code"))
	}

	cctx := context.WithValue(ctx, oauth2.HTTPClient, p.httpClient)
	tok, err := p.oauth2Cfg.Exchange(cctx, code, oauth2.SetAuthURLParam("code_verifier", entry.verifier))
	if err != nil {
		return "", nil, sdk.NewError(sdk.CodeAuthUnauthenticated, "AUTH", http.StatusUnauthorized, sdk.CodeAuthUnauthenticated, false,
			fmt.Errorf("oidc token exchange failed: %w", err))
	}
	rawID, _ := tok.Extra("id_token").(string)
	if rawID == "" {
		return "", nil, sdk.NewError(sdk.CodeAuthUnauthenticated, "AUTH", http.StatusUnauthorized, sdk.CodeAuthUnauthenticated, false,
			fmt.Errorf("oidc response missing id_token"))
	}
	idTok, err := p.verifier.Verify(oidc.ClientContext(ctx, p.httpClient), rawID)
	if err != nil {
		return "", nil, sdk.NewError(sdk.CodeAuthUnauthenticated, "AUTH", http.StatusUnauthorized, sdk.CodeAuthUnauthenticated, false,
			fmt.Errorf("oidc id_token verification failed: %w", err))
	}
	if idTok.Nonce != entry.nonce {
		return "", nil, sdk.NewError(sdk.CodeAuthUnauthenticated, "AUTH", http.StatusUnauthorized, sdk.CodeAuthUnauthenticated, false,
			fmt.Errorf("oidc nonce mismatch"))
	}

	var claims struct {
		Sub               string   `json:"sub"`
		PreferredUsername string   `json:"preferred_username"`
		Email             string   `json:"email"`
		Name              string   `json:"name"`
		Groups            []string `json:"groups"`
	}
	if err := idTok.Claims(&claims); err != nil {
		return "", nil, err
	}
	if claims.Sub == "" {
		claims.Sub = idTok.Subject
	}

	u, err := svc.repo.GetUserByOIDC(ctx, p.cfg.IssuerURL, claims.Sub)
	if err != nil {
		uname := claims.PreferredUsername
		if uname == "" {
			uname = claims.Email
		}
		if uname == "" {
			uname = "oidc-" + claims.Sub
		}
		dname := claims.Name
		if dname == "" {
			dname = uname
		}
		role := p.cfg.DefaultRole
		if p.cfg.AdminGroup != "" {
			for _, g := range claims.Groups {
				if g == p.cfg.AdminGroup {
					role = domain.RoleAdmin
					break
				}
			}
		}
		u = &domain.User{
			ID:           domain.NewID(domain.PrefixUser),
			Username:     uname,
			DisplayName:  dname,
			AuthSource:   "oidc",
			OIDCIssuer:   p.cfg.IssuerURL,
			OIDCSubject:  claims.Sub,
			RoleBindings: []domain.RoleBinding{{ProjectID: "*", Role: role}},
		}
		if cerr := svc.repo.CreateUser(ctx, u); cerr != nil {
			return "", nil, cerr
		}
	}
	if u.Disabled {
		return "", nil, sdk.NewError(sdk.CodeAuthForbidden, "AUTH", http.StatusForbidden, sdk.CodeAuthForbidden, false, fmt.Errorf("account disabled"))
	}

	rawTok, err := randomBase62(40)
	if err != nil {
		return "", nil, err
	}
	sum := sha256.Sum256([]byte(rawTok))
	now := time.Now().UTC()
	sess := &domain.UserSession{
		ID:        domain.NewID(domain.PrefixSession),
		UserID:    u.ID,
		TokenHash: hex.EncodeToString(sum[:]),
		SourceIP:  sourceIP,
		UserAgent: userAgent,
		ExpiresAt: now.Add(12 * time.Hour),
	}
	if err := svc.repo.CreateSession(ctx, sess); err != nil {
		return "", nil, err
	}
	if svc.audit != nil {
		_, _ = svc.audit.Append(ctx, domain.AuditEntry{
			ActorID: u.ID, ActorType: "user", SourceIP: sourceIP,
			Action: "auth.oidc_login", TargetType: "user", TargetID: u.ID, Result: "success",
			PayloadJSON: fmt.Sprintf(`{"issuer":%q,"sub":%q}`, p.cfg.IssuerURL, claims.Sub),
		})
	}
	return rawTok, u, nil
}

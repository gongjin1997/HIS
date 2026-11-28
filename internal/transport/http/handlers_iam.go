package httptransport

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/pkg/platform/httpx"
)

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if _, err := httpx.DecodeStrictJSON(w, r, &req, 64<<10); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error(), nil)
		return
	}
	res, err := s.d.Identity.Login(r.Context(), req.Username, req.Password, clientIP(r), httpx.TraceIDFromContext(r.Context()))
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{ // #nosec G124 -- Secure enabled dynamically under TLS; local control-plane supports HTTP
		Name:     "liveops_session",
		Value:    res.Token,
		Path:     "/",
		Expires:  res.ExpiresAt,
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})
	httpx.WriteJSON(w, http.StatusOK, res)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{ // #nosec G124 -- Secure enabled dynamically under TLS
		Name:     "liveops_session",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})
	httpx.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	tc, scope := ctxScope(r)
	tenants, _ := s.d.Identity.ListTenants(r.Context())
	apps, _ := s.d.Identity.ListApps(r.Context(), scope.TenantID)
	projects, _ := s.d.Identity.ListProjects(r.Context(), scope)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"context":  tc,
		"tenants":  tenants,
		"apps":     apps,
		"projects": projects,
	})
}

func (s *Server) handleListTenants(w http.ResponseWriter, r *http.Request) {
	items, err := s.d.Identity.ListTenants(r.Context())
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.WriteList(w, items, nil, len(items))
}

func (s *Server) handleCreateTenant(w http.ResponseWriter, r *http.Request) {
	tc, _ := ctxScope(r)
	var req struct {
		Code        string `json:"code"`
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if _, err := httpx.DecodeStrictJSON(w, r, &req, 64<<10); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error(), nil)
		return
	}
	t, err := s.d.Identity.CreateTenant(r.Context(), tc, req.Code, req.Name, req.Description)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, t)
}

func (s *Server) handleListApps(w http.ResponseWriter, r *http.Request) {
	_, scope := ctxScope(r)
	items, err := s.d.Identity.ListApps(r.Context(), scope.TenantID)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.WriteList(w, items, nil, len(items))
}

func (s *Server) handleCreateApp(w http.ResponseWriter, r *http.Request) {
	tc, scope := ctxScope(r)
	var req struct {
		Code        string `json:"code"`
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if _, err := httpx.DecodeStrictJSON(w, r, &req, 64<<10); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error(), nil)
		return
	}
	a, err := s.d.Identity.CreateApp(r.Context(), tc, scope.TenantID, req.Code, req.Name, req.Description)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, a)
}

func (s *Server) handleListAppKeys(w http.ResponseWriter, r *http.Request) {
	_, scope := ctxScope(r)
	items, err := s.d.Identity.ListAppKeys(r.Context(), scope)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.WriteList(w, items, nil, len(items))
}

func (s *Server) handleRotateAppKey(w http.ResponseWriter, r *http.Request) {
	tc, scope := ctxScope(r)
	var req struct {
		GraceMinutes int `json:"graceMinutes"`
	}
	_ = r.Body.Close() // optional body
	grace := 24 * time.Hour
	if req.GraceMinutes > 0 {
		grace = time.Duration(req.GraceMinutes) * time.Minute
	}
	ak, raw, err := s.d.Identity.RotateAppKey(r.Context(), tc, scope, grace)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{
		"key":       ak,
		"plaintext": raw,
	})
}

func (s *Server) handleRevokeAppKey(w http.ResponseWriter, r *http.Request) {
	tc, scope := ctxScope(r)
	keyID := chi.URLParam(r, "keyId")
	if err := s.d.Identity.RevokeAppKey(r.Context(), tc, scope, keyID); err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]bool{"revoked": true})
}

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	_, scope := ctxScope(r)
	items, err := s.d.Identity.ListProjects(r.Context(), scope)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.WriteList(w, items, nil, len(items))
}

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	tc, scope := ctxScope(r)
	var req struct {
		Code        string `json:"code"`
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if _, err := httpx.DecodeStrictJSON(w, r, &req, 64<<10); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error(), nil)
		return
	}
	p, err := s.d.Identity.CreateProject(r.Context(), tc, scope, req.Code, req.Name, req.Description)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.SetVersionETag(w, p.PermissionVersion)
	httpx.WriteJSON(w, http.StatusCreated, p)
}

func (s *Server) handleListProjectRoles(w http.ResponseWriter, r *http.Request) {
	_, scope := ctxScope(r)
	projectID := chi.URLParam(r, "projectId")
	items, permVer, err := s.d.Identity.ListProjectRoles(r.Context(), scope, projectID)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.SetVersionETag(w, permVer)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"items":             items,
		"permissionVersion": permVer,
	})
}

func (s *Server) handleReplaceProjectRolePerms(w http.ResponseWriter, r *http.Request) {
	tc, scope := ctxScope(r)
	expectedVer, st, code, err := httpx.ParseIfMatch(r)
	if err != nil {
		httpx.WriteError(w, st, code, err.Error(), nil)
		return
	}
	var req struct {
		Permissions []string `json:"permissions"`
	}
	if _, err := httpx.DecodeStrictJSON(w, r, &req, 64<<10); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error(), nil)
		return
	}
	projectID := chi.URLParam(r, "projectId")
	roleID := chi.URLParam(r, "roleId")
	newVer, err := s.d.Identity.ReplaceProjectRolePerms(r.Context(), tc, scope, projectID, roleID, expectedVer, req.Permissions)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.SetVersionETag(w, newVer)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"permissionVersion": newVer})
}

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	_, scope := ctxScope(r)
	items, err := s.d.Identity.ListUsers(r.Context(), scope.TenantID)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.WriteList(w, items, nil, len(items))
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	tc, scope := ctxScope(r)
	var req struct {
		Username    string `json:"username"`
		DisplayName string `json:"displayName"`
		Email       string `json:"email"`
		Password    string `json:"password"`
		RoleID      string `json:"roleId"`
	}
	if _, err := httpx.DecodeStrictJSON(w, r, &req, 64<<10); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error(), nil)
		return
	}
	u, err := s.d.Identity.CreateUser(r.Context(), tc, scope.TenantID, req.Username, req.DisplayName, req.Email, req.Password, req.RoleID)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, u)
}

func (s *Server) handleListRoles(w http.ResponseWriter, r *http.Request) {
	_, scope := ctxScope(r)
	items, err := s.d.Identity.ListRoles(r.Context(), scope.TenantID)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.WriteList(w, items, nil, len(items))
}

func (s *Server) handleListRoutePolicies(w http.ResponseWriter, r *http.Request) {
	_, scope := ctxScope(r)
	items, err := s.d.Identity.ListRoutePolicies(r.Context(), scope.TenantID)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.WriteList(w, items, nil, len(items))
}

func (s *Server) handleUpsertRoutePolicy(w http.ResponseWriter, r *http.Request) {
	tc, scope := ctxScope(r)
	var req struct {
		Method       string `json:"method"`
		PathPattern  string `json:"pathPattern"`
		RequiredPerm string `json:"requiredPerm"`
		ScopeLevel   string `json:"scopeLevel"`
		Description  string `json:"description"`
		Enabled      bool   `json:"enabled"`
	}
	if _, err := httpx.DecodeStrictJSON(w, r, &req, 64<<10); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error(), nil)
		return
	}
	p, err := s.d.Identity.UpsertRoutePolicy(r.Context(), tc, scope.TenantID, req.Method, req.PathPattern, req.RequiredPerm, req.ScopeLevel, req.Description, req.Enabled)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, p)
}

func (s *Server) handleListAuditLogs(w http.ResponseWriter, r *http.Request) {
	_, scope := ctxScope(r)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	tenantFilter := scope.TenantID
	if r.URL.Query().Get("allTenants") == "true" {
		tenantFilter = ""
	}
	items, err := s.d.Identity.ListAuditLogs(r.Context(), tenantFilter, limit)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.WriteList(w, items, nil, len(items))
}

func (s *Server) handleVerifyAuditChain(w http.ResponseWriter, r *http.Request) {
	res, err := s.d.Identity.VerifyAuditChain(r.Context())
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, res)
}

// Keep unused import check satisfied if needed
var _ = domain.Scope{}

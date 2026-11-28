package httptransport

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/pkg/platform/httpx"
)

func (s *Server) handleListConfigs(w http.ResponseWriter, r *http.Request) {
	_, scope := ctxScope(r)
	proj := r.URL.Query().Get("projectId")
	items, err := s.d.Config.ListConfigurations(r.Context(), scope, proj)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.WriteList(w, items, nil, len(items))
}

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	_, scope := ctxScope(r)
	c, err := s.d.Config.GetConfiguration(r.Context(), scope, chi.URLParam(r, "cfgId"))
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.SetVersionETag(w, c.Version)
	httpx.WriteJSON(w, http.StatusOK, c)
}

func (s *Server) handleCreateConfig(w http.ResponseWriter, r *http.Request) {
	tc, scope := ctxScope(r)
	var req struct {
		ProjectID   string               `json:"projectId"`
		Name        string               `json:"name"`
		Format      string               `json:"format"`
		Description string               `json:"description"`
		Entries     []domain.ConfigEntry `json:"entries"`
	}
	if _, err := httpx.DecodeStrictJSON(w, r, &req, 1<<20); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error(), nil)
		return
	}
	c, err := s.d.Config.CreateConfiguration(r.Context(), tc, scope, req.ProjectID, req.Name, req.Format, req.Description, req.Entries)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.SetVersionETag(w, c.Version)
	httpx.WriteJSON(w, http.StatusCreated, c)
}

func (s *Server) handleUpdateConfig(w http.ResponseWriter, r *http.Request) {
	tc, scope := ctxScope(r)
	expectedVer, st, code, err := httpx.ParseIfMatch(r)
	if err != nil {
		httpx.WriteError(w, st, code, err.Error(), nil)
		return
	}
	var req struct {
		Description   string               `json:"description"`
		ChangeSummary string               `json:"changeSummary"`
		Entries       []domain.ConfigEntry `json:"entries"`
	}
	if _, err := httpx.DecodeStrictJSON(w, r, &req, 1<<20); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error(), nil)
		return
	}
	c, err := s.d.Config.UpdateConfiguration(r.Context(), tc, scope, chi.URLParam(r, "cfgId"), expectedVer, req.Description, req.ChangeSummary, req.Entries)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.SetVersionETag(w, c.Version)
	httpx.WriteJSON(w, http.StatusOK, c)
}

func (s *Server) handleDeleteConfig(w http.ResponseWriter, r *http.Request) {
	tc, scope := ctxScope(r)
	expectedVer, st, code, err := httpx.ParseIfMatch(r)
	if err != nil {
		httpx.WriteError(w, st, code, err.Error(), nil)
		return
	}
	if err := s.d.Config.TombstoneConfiguration(r.Context(), tc, scope, chi.URLParam(r, "cfgId"), expectedVer); err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (s *Server) handleListConfigHistory(w http.ResponseWriter, r *http.Request) {
	_, scope := ctxScope(r)
	items, err := s.d.Config.ListHistory(r.Context(), scope, chi.URLParam(r, "cfgId"))
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.WriteList(w, items, nil, len(items))
}

func (s *Server) handleRevealConfigSecret(w http.ResponseWriter, r *http.Request) {
	tc, scope := ctxScope(r)
	var req struct {
		Key    string `json:"key"`
		Reason string `json:"reason"`
	}
	if _, err := httpx.DecodeStrictJSON(w, r, &req, 64<<10); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error(), nil)
		return
	}
	pt, err := s.d.Config.RevealSecret(r.Context(), tc, scope, chi.URLParam(r, "cfgId"), req.Key, req.Reason)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"key":       req.Key,
		"plaintext": pt,
		"audited":   true,
	})
}

func (s *Server) handleListEnvs(w http.ResponseWriter, r *http.Request) {
	_, scope := ctxScope(r)
	items, err := s.d.Config.ListEnvironments(r.Context(), scope, r.URL.Query().Get("projectId"))
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.WriteList(w, items, nil, len(items))
}

func (s *Server) handleCreateEnv(w http.ResponseWriter, r *http.Request) {
	tc, scope := ctxScope(r)
	var req struct {
		ProjectID   string             `json:"projectId"`
		Code        string             `json:"code"`
		Name        string             `json:"name"`
		Description string             `json:"description"`
		Members     []domain.EnvMember `json:"members"`
	}
	if _, err := httpx.DecodeStrictJSON(w, r, &req, 256<<10); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error(), nil)
		return
	}
	env, err := s.d.Config.CreateEnvironment(r.Context(), tc, scope, req.ProjectID, req.Code, req.Name, req.Description, req.Members)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.SetVersionETag(w, env.Version)
	httpx.WriteJSON(w, http.StatusCreated, env)
}

func (s *Server) handleUpdateEnvMembers(w http.ResponseWriter, r *http.Request) {
	tc, scope := ctxScope(r)
	expectedVer, st, code, err := httpx.ParseIfMatch(r)
	if err != nil {
		httpx.WriteError(w, st, code, err.Error(), nil)
		return
	}
	var req struct {
		Members []domain.EnvMember `json:"members"`
	}
	if _, err := httpx.DecodeStrictJSON(w, r, &req, 256<<10); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error(), nil)
		return
	}
	env, err := s.d.Config.UpdateEnvironmentMembers(r.Context(), tc, scope, chi.URLParam(r, "envId"), expectedVer, req.Members)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.SetVersionETag(w, env.Version)
	httpx.WriteJSON(w, http.StatusOK, env)
}

func (s *Server) handleViewCheckEnv(w http.ResponseWriter, r *http.Request) {
	_, scope := ctxScope(r)
	res, err := s.d.Config.ViewCheckEnvironment(r.Context(), scope, chi.URLParam(r, "envId"))
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, res)
}

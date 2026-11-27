package server

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/kinstall/kinstall/internal/domain"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

func (s *Server) handleGetPlugin(w http.ResponseWriter, r *http.Request) {
	meta, err := s.app.GetPluginByProduct(chi.URLParam(r, "product"))
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, meta)
}

func (s *Server) handleListCredentials(w http.ResponseWriter, r *http.Request) {
	items, err := s.app.ListCredentials(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"))
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleDeleteCredential(w http.ResponseWriter, r *http.Request) {
	if err := s.app.DeleteCredential(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"), chi.URLParam(r, "credId")); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleImportHosts(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Format string `json:"format"` // csv | yaml | xlsx
		Data   string `json:"data"`
		DryRun bool   `json:"dryRun"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "API", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	res, imported, err := s.app.ImportHosts(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"), body.Format, []byte(body.Data), body.DryRun)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"result": res, "imported": imported, "dryRun": body.DryRun})
}

func (s *Server) handleDeleteHost(w http.ResponseWriter, r *http.Request) {
	if err := s.app.DeleteHost(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"), chi.URLParam(r, "hostId")); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleCheckHostBaseline(w http.ResponseWriter, r *http.Request) {
	drifts, err := s.app.CheckHostBaseline(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"), chi.URLParam(r, "hostId"))
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"drifts": drifts, "compliant": len(drifts) == 0})
}

func (s *Server) handleInitHost(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Profiles []string `json:"profiles"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	op, err := s.app.InitHost(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"), chi.URLParam(r, "hostId"), body.Profiles)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	w.Header().Set("Location", "/api/v1/projects/"+op.ProjectID+"/operations/"+op.ID)
	writeJSON(w, http.StatusAccepted, op)
}

func (s *Server) handleDeleteInstance(w http.ResponseWriter, r *http.Request) {
	if err := s.app.DeleteInstance(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"), chi.URLParam(r, "instanceId")); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleValidateStack(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SpecYAML string `json:"specYaml"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "API", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	res, err := s.app.ValidateStackYAML(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"), body.SpecYAML)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"dag": res.DAG, "nodes": res.Nodes, "valid": true})
}

func (s *Server) handleApplyStack(w http.ResponseWriter, r *http.Request) {
	op, err := s.app.ApplyStack(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"), chi.URLParam(r, "stackId"), "install")
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	w.Header().Set("Location", "/api/v1/projects/"+op.ProjectID+"/operations/"+op.ID)
	writeJSON(w, http.StatusAccepted, op)
}

func (s *Server) handleUninstallStack(w http.ResponseWriter, r *http.Request) {
	op, err := s.app.ApplyStack(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"), chi.URLParam(r, "stackId"), "uninstall")
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	w.Header().Set("Location", "/api/v1/projects/"+op.ProjectID+"/operations/"+op.ID)
	writeJSON(w, http.StatusAccepted, op)
}

func (s *Server) handleListNotificationChannels(w http.ResponseWriter, r *http.Request) {
	items, err := s.app.ListNotificationChannels(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"))
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleCreateNotificationChannel(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name    string            `json:"name"`
		Kind    string            `json:"kind"` // webhook | email | wecom | dingtalk | feishu
		Enabled bool              `json:"enabled"`
		Events  []string          `json:"events"`
		Config  map[string]string `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "API", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	cfgJ, _ := json.Marshal(body.Config)
	ch, err := s.app.CreateNotificationChannel(r.Context(), ActorFromContext(r.Context()), &domain.NotificationChannel{
		ProjectID:  chi.URLParam(r, "projectId"),
		Name:       body.Name,
		Kind:       body.Kind,
		Enabled:    body.Enabled,
		Events:     body.Events,
		ConfigJSON: cfgJ,
	})
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusCreated, ch)
}

func (s *Server) handleTestNotificationChannels(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Summary string `json:"summary"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	sent, folded, err := s.app.TestNotificationDispatch(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"), body.Summary)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"sent": sent, "folded": folded})
}

func (s *Server) handleGitOpsExport(w http.ResponseWriter, r *http.Request) {
	raw, err := s.app.ExportGitOpsYAML(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"))
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	// #nosec G705 -- YAML export with application/yaml and nosniff header
	_, _ = w.Write(raw)
}

func (s *Server) handleGitOpsDiff(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DesiredYAML string `json:"desiredYaml"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "API", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	entries, code, err := s.app.DiffGitOpsYAML(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"), []byte(body.DesiredYAML))
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": entries, "exitCode": code})
}

func (s *Server) handleSupportBundle(w http.ResponseWriter, r *http.Request) {
	raw, _, err := s.app.CollectSupportBundle(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"))
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", `attachment; filename="kinstall-support-bundle.tar.gz"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	// #nosec G705 -- binary gzip attachment with X-Content-Type-Options: nosniff
	_, _ = w.Write(raw)
}

func (s *Server) handleComplianceDownload(w http.ResponseWriter, r *http.Request) {
	_, raw, digest, err := s.app.GenerateComplianceReport(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"))
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", `attachment; filename="kinstall-compliance-evidence.tar.gz"`)
	w.Header().Set("X-Evidence-SHA256", digest)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	// #nosec G705 -- binary gzip attachment with X-Content-Type-Options: nosniff
	_, _ = w.Write(raw)
}

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	items, err := s.app.ListUsers(r.Context(), ActorFromContext(r.Context()))
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username     string               `json:"username"`
		DisplayName  string               `json:"displayName"`
		Password     string               `json:"password"`
		RoleBindings []domain.RoleBinding `json:"roleBindings"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "API", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	u, err := s.app.CreateUser(r.Context(), ActorFromContext(r.Context()), body.Username, body.DisplayName, body.Password, body.RoleBindings)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusCreated, u)
}

func (s *Server) handleListAPITokens(w http.ResponseWriter, r *http.Request) {
	items, err := s.app.ListAPITokens(r.Context(), ActorFromContext(r.Context()))
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleRevokeAPIToken(w http.ResponseWriter, r *http.Request) {
	if err := s.app.RevokeAPIToken(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "tokenId")); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleOIDCConfig(w http.ResponseWriter, _ *http.Request) {
	p := s.authSvc.OIDC()
	if p == nil {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": true, "issuer": p.Issuer()})
}

func (s *Server) handleOIDCLogin(w http.ResponseWriter, r *http.Request) {
	p := s.authSvc.OIDC()
	if p == nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "AUTH", http.StatusNotImplemented, sdk.CodeAPIInvalidRequest, false, nil))
		return
	}
	authURL, state, err := p.BeginAuth()
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	if r.URL.Query().Get("mode") == "json" {
		writeJSON(w, http.StatusOK, map[string]string{"authUrl": authURL, "state": state})
		return
	}
	http.Redirect(w, r, authURL, http.StatusFound)
}

func (s *Server) handleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	p := s.authSvc.OIDC()
	if p == nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "AUTH", http.StatusNotImplemented, sdk.CodeAPIInvalidRequest, false, nil))
		return
	}
	tok, user, err := p.CompleteAuth(r.Context(), s.authSvc, r.URL.Query().Get("state"), r.URL.Query().Get("code"), r.RemoteAddr, r.UserAgent())
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	if strings.Contains(r.Header.Get("Accept"), "application/json") {
		writeJSON(w, http.StatusOK, map[string]any{"token": tok, "user": user})
		return
	}
	http.Redirect(w, r, "/login?oidc_token="+url.QueryEscape(tok), http.StatusFound)
}

func (s *Server) handleDiscoverHosts(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CIDR string `json:"cidr"`
		Port int    `json:"port"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "API", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	items, err := s.app.DiscoverHostsSubnet(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"), body.CIDR, body.Port)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if raw := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "); raw != "" {
		_ = s.authSvc.RevokeSessionByToken(r.Context(), raw)
	}
	w.WriteHeader(http.StatusNoContent)
}

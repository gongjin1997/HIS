package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/kinstall/kinstall/internal/app"
	"github.com/kinstall/kinstall/internal/domain"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

func (s *Server) handleDownloadHostTemplate(w http.ResponseWriter, r *http.Request) {
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "xlsx"
	}
	raw, ct, filename, err := s.app.ExportHostsTemplate(format)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	// #nosec G705 -- binary xlsx or csv download with attachment and nosniff headers
	_, _ = w.Write(raw)
}

func (s *Server) handleExportHosts(w http.ResponseWriter, r *http.Request) {
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "xlsx"
	}
	raw, ct, filename, err := s.app.ExportHostsLedger(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"), format)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	// #nosec G705 -- binary xlsx or csv download with attachment and nosniff headers
	_, _ = w.Write(raw)
}

func (s *Server) handleUpdateHost(w http.ResponseWriter, r *http.Request) {
	var patch app.HostUpdatePatch
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "API", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	updated, err := s.app.UpdateHost(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"), chi.URLParam(r, "hostId"), patch)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	w.Header().Set("ETag", formatETag(updated.Version))
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleBatchHosts(w http.ResponseWriter, r *http.Request) {
	var req app.BatchHostActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "API", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	res, err := s.app.BatchOperateHosts(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"), req)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleCMDBOverview(w http.ResponseWriter, r *http.Request) {
	ov, err := s.app.GetCMDBOverview(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"))
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, ov)
}

func (s *Server) handleHostImpact(w http.ResponseWriter, r *http.Request) {
	imp, err := s.app.GetHostImpact(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"), chi.URLParam(r, "hostId"))
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, imp)
}

func (s *Server) handleListDynamicGroups(w http.ResponseWriter, r *http.Request) {
	items, err := s.app.ListDynamicGroups(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"))
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleCreateDynamicGroup(w http.ResponseWriter, r *http.Request) {
	var g domain.CMDBDynamicGroup
	if err := json.NewDecoder(r.Body).Decode(&g); err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "API", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	g.ProjectID = chi.URLParam(r, "projectId")
	created, err := s.app.CreateDynamicGroup(r.Context(), ActorFromContext(r.Context()), &g)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleUpdateDynamicGroup(w http.ResponseWriter, r *http.Request) {
	var g domain.CMDBDynamicGroup
	if err := json.NewDecoder(r.Body).Decode(&g); err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "API", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	g.ID = chi.URLParam(r, "groupId")
	g.ProjectID = chi.URLParam(r, "projectId")
	updated, err := s.app.UpdateDynamicGroup(r.Context(), ActorFromContext(r.Context()), &g)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleDeleteDynamicGroup(w http.ResponseWriter, r *http.Request) {
	if err := s.app.DeleteDynamicGroup(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"), chi.URLParam(r, "groupId")); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePreviewDynamicGroup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Rules []domain.CMDBGroupRule `json:"rules"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "API", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	matched, err := s.app.PreviewDynamicGroup(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"), body.Rules)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": matched, "count": len(matched)})
}

func (s *Server) handleListCMDBSyncSources(w http.ResponseWriter, r *http.Request) {
	items, err := s.app.ListCMDBSyncSources(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"))
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleCreateCMDBSyncSource(w http.ResponseWriter, r *http.Request) {
	var src domain.CMDBSyncSource
	if err := json.NewDecoder(r.Body).Decode(&src); err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "API", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	src.ProjectID = chi.URLParam(r, "projectId")
	created, err := s.app.CreateCMDBSyncSource(r.Context(), ActorFromContext(r.Context()), &src)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleDeleteCMDBSyncSource(w http.ResponseWriter, r *http.Request) {
	if err := s.app.DeleteCMDBSyncSource(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"), chi.URLParam(r, "sourceId")); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRunCMDBSyncSource(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DryRun bool `json:"dryRun"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	res, err := s.app.RunCMDBSyncSource(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"), chi.URLParam(r, "sourceId"), body.DryRun)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

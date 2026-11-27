package server

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/kinstall/kinstall/internal/app"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	if err := s.app.DeleteProject(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId")); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleGetInstanceDetail(w http.ResponseWriter, r *http.Request) {
	detail, err := s.app.GetInstanceDetail(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"), chi.URLParam(r, "instanceId"))
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) handleUpdateInstance(w http.ResponseWriter, r *http.Request) {
	var patch app.InstanceUpdatePatch
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "API", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	updated, err := s.app.UpdateInstance(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"), chi.URLParam(r, "instanceId"), patch)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	w.Header().Set("ETag", formatETag(updated.Version))
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleUpdateStack(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Env      string `json:"env"`
		Name     string `json:"name"`
		SpecYAML string `json:"specYaml"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "API", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	st, dag, err := s.app.UpdateStack(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"), chi.URLParam(r, "stackId"), body.Env, body.Name, body.SpecYAML)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"stack": st, "dag": dag})
}

func (s *Server) handleDeleteStack(w http.ResponseWriter, r *http.Request) {
	if err := s.app.DeleteStack(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"), chi.URLParam(r, "stackId")); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleCancelOperation(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if err := s.app.CancelOperation(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"), chi.URLParam(r, "opId"), body.Reason); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

func (s *Server) handleDeleteNotificationChannel(w http.ResponseWriter, r *http.Request) {
	if err := s.app.DeleteNotificationChannel(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"), chi.URLParam(r, "channelId")); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

package server

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

func (s *Server) handleProbeInstance(w http.ResponseWriter, r *http.Request) {
	rep, err := s.app.ProbeInstanceLive(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"), chi.URLParam(r, "instanceId"))
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

func (s *Server) handleControlInstance(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "API", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	rep, err := s.app.ControlInstanceService(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"), chi.URLParam(r, "instanceId"), body.Action)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

func (s *Server) handleInstanceLogs(w http.ResponseWriter, r *http.Request) {
	lines, _ := strconv.Atoi(r.URL.Query().Get("lines"))
	res, err := s.app.FetchInstanceLogs(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"), chi.URLParam(r, "instanceId"), r.URL.Query().Get("hostId"), lines)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/kinstall/kinstall/internal/authz"
	"github.com/kinstall/kinstall/internal/delivery"
	"github.com/kinstall/kinstall/internal/plugindsl"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

func (s *Server) requirePluginDSL(w http.ResponseWriter, r *http.Request) *plugindsl.Service {
	svc := s.app.PluginDSL()
	if svc == nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeDBUnavailableOrSchema, "PLGDSL", http.StatusServiceUnavailable, sdk.CodeDBUnavailableOrSchema, false, fmt.Errorf("plugin dsl service unavailable")))
		return nil
	}
	return svc
}

func (s *Server) requireDelivery(w http.ResponseWriter, r *http.Request) *delivery.Service {
	svc := s.app.Delivery()
	if svc == nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeDBUnavailableOrSchema, "DELIV", http.StatusServiceUnavailable, sdk.CodeDBUnavailableOrSchema, false, fmt.Errorf("delivery service unavailable")))
		return nil
	}
	return svc
}

// --- Declarative Extensible Plugin Packages ---

func (s *Server) handleListDeclarativePlugins(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	if err := authz.Authorize(ActorFromContext(r.Context()), pid, "resource.read"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requirePluginDSL(w, r)
	if svc == nil {
		return
	}
	items, err := svc.List(r.Context(), pid)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleUpsertDeclarativePlugin(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	actor := ActorFromContext(r.Context())
	if err := authz.Authorize(actor, pid, "instance.write"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requirePluginDSL(w, r)
	if svc == nil {
		return
	}
	var body struct {
		ManifestYAML string `json:"manifestYaml"`
		IsEnabled    *bool  `json:"isEnabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "PLGDSL", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	enabled := true
	if body.IsEnabled != nil {
		enabled = *body.IsEnabled
	}
	saved, err := svc.Upsert(r.Context(), pid, actor.Username, body.ManifestYAML, enabled)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	s.app.RecordAuditEvent(r.Context(), pid, actor.Username, "plugin.declarative.upsert", "declarative_plugin", saved.Name, map[string]any{
		"version": saved.Version, "enabled": saved.IsEnabled,
	})
	writeJSON(w, http.StatusOK, saved)
}

func (s *Server) handleDeleteDeclarativePlugin(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	if err := authz.Authorize(ActorFromContext(r.Context()), pid, "instance.write"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requirePluginDSL(w, r)
	if svc == nil {
		return
	}
	if err := svc.Delete(r.Context(), pid, chi.URLParam(r, "pluginId")); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": chi.URLParam(r, "pluginId")})
}

// --- Enterprise Batch / M1 / M2 / M3 Delivery Pipelines ---

func (s *Server) handleListDeliveryReleases(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	if err := authz.Authorize(ActorFromContext(r.Context()), pid, "resource.read"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireDelivery(w, r)
	if svc == nil {
		return
	}
	items, err := svc.ListReleases(r.Context(), pid)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleCreateDeliveryRelease(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	actor := ActorFromContext(r.Context())
	if err := authz.Authorize(actor, pid, "instance.write"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireDelivery(w, r)
	if svc == nil {
		return
	}
	var req delivery.Release
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "DELIV", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	created, err := svc.CreateRelease(r.Context(), pid, actor.Username, &req)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	s.app.RecordAuditEvent(r.Context(), pid, actor.Username, "delivery.release.create", "delivery_release", created.ID, map[string]any{
		"name": created.Name, "mode": created.ScheduleMode, "items": len(created.Items),
	})
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleStartOrResumeDeliveryRelease(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	actor := ActorFromContext(r.Context())
	if err := authz.Authorize(actor, pid, "instance.write"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireDelivery(w, r)
	if svc == nil {
		return
	}
	rel, err := svc.StartOrResume(r.Context(), pid, chi.URLParam(r, "releaseId"))
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	s.app.RecordAuditEvent(r.Context(), pid, actor.Username, "delivery.release.advance", "delivery_release", rel.ID, map[string]any{
		"status": rel.Status, "currentBatch": rel.CurrentBatch,
	})
	writeJSON(w, http.StatusOK, rel)
}

func (s *Server) handleRollbackDeliveryRelease(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	actor := ActorFromContext(r.Context())
	if err := authz.Authorize(actor, pid, "instance.write"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireDelivery(w, r)
	if svc == nil {
		return
	}
	rel, err := svc.RollbackRelease(r.Context(), pid, chi.URLParam(r, "releaseId"))
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	s.app.RecordAuditEvent(r.Context(), pid, actor.Username, "delivery.release.rollback", "delivery_release", rel.ID, map[string]any{
		"status": rel.Status,
	})
	writeJSON(w, http.StatusOK, rel)
}

func (s *Server) handleCancelDeliveryRelease(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	actor := ActorFromContext(r.Context())
	if err := authz.Authorize(actor, pid, "instance.write"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireDelivery(w, r)
	if svc == nil {
		return
	}
	rel, err := svc.CancelRelease(r.Context(), pid, chi.URLParam(r, "releaseId"))
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, rel)
}

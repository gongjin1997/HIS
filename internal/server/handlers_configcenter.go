package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/kinstall/kinstall/internal/authz"
	"github.com/kinstall/kinstall/internal/configcenter"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

func (s *Server) requireConfigCenter(w http.ResponseWriter, r *http.Request) *configcenter.Service {
	svc := s.app.ConfigCenter()
	if svc == nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeDBUnavailableOrSchema, "CFG", http.StatusServiceUnavailable, sdk.CodeDBUnavailableOrSchema, false, fmt.Errorf("config center unavailable")))
		return nil
	}
	return svc
}

func (s *Server) handleListConfigNamespaces(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	if err := authz.Authorize(ActorFromContext(r.Context()), pid, "resource.read"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireConfigCenter(w, r)
	if svc == nil {
		return
	}
	items, err := svc.ListNamespaces(r.Context(), pid, r.URL.Query().Get("env"))
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleCreateConfigNamespace(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	if err := authz.Authorize(ActorFromContext(r.Context()), pid, "instance.write"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireConfigCenter(w, r)
	if svc == nil {
		return
	}
	var ns configcenter.Namespace
	if err := json.NewDecoder(r.Body).Decode(&ns); err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "CFG", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	created, err := svc.CreateNamespace(r.Context(), pid, &ns)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleDeleteConfigNamespace(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	if err := authz.Authorize(ActorFromContext(r.Context()), pid, "instance.write"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireConfigCenter(w, r)
	if svc == nil {
		return
	}
	if err := svc.DeleteNamespace(r.Context(), pid, chi.URLParam(r, "nsId")); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": chi.URLParam(r, "nsId")})
}

func (s *Server) handleListConfigItems(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	if err := authz.Authorize(ActorFromContext(r.Context()), pid, "resource.read"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireConfigCenter(w, r)
	if svc == nil {
		return
	}
	items, err := svc.ListItems(r.Context(), chi.URLParam(r, "nsId"))
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleUpsertConfigItem(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	actor := ActorFromContext(r.Context())
	if err := authz.Authorize(actor, pid, "instance.write"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireConfigCenter(w, r)
	if svc == nil {
		return
	}
	var item configcenter.ConfigItem
	if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "CFG", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	saved, err := svc.UpsertItem(r.Context(), chi.URLParam(r, "nsId"), actor.Username, &item)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

func (s *Server) handleDeleteConfigItem(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	if err := authz.Authorize(ActorFromContext(r.Context()), pid, "instance.write"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireConfigCenter(w, r)
	if svc == nil {
		return
	}
	if err := svc.DeleteItem(r.Context(), chi.URLParam(r, "nsId"), chi.URLParam(r, "key")); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": chi.URLParam(r, "key")})
}

// handleRevealConfigSecret enforces privileged authorization & writes a tamper-evident audit entry before revealing secret plaintext.
func (s *Server) handleRevealConfigSecret(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	actor := ActorFromContext(r.Context())
	if err := authz.Authorize(actor, pid, "credential.write"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireConfigCenter(w, r)
	if svc == nil {
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	nsID := chi.URLParam(r, "nsId")
	key := chi.URLParam(r, "key")
	val, err := svc.RevealSecret(r.Context(), nsID, key)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	s.app.RecordAuditEvent(r.Context(), pid, actor.Username, "config.secret.reveal", "cfg_item", nsID+"/"+key, map[string]any{
		"reason": body.Reason,
		"key":    key,
	})
	writeJSON(w, http.StatusOK, map[string]any{"key": key, "value": val, "audited": true})
}

func (s *Server) handleDiffConfigNamespace(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	if err := authz.Authorize(ActorFromContext(r.Context()), pid, "resource.read"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireConfigCenter(w, r)
	if svc == nil {
		return
	}
	diffs, err := svc.DiffDraftVsLatest(r.Context(), chi.URLParam(r, "nsId"))
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": diffs})
}

func (s *Server) handlePublishConfigRelease(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	actor := ActorFromContext(r.Context())
	if err := authz.Authorize(actor, pid, "instance.write"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireConfigCenter(w, r)
	if svc == nil {
		return
	}
	var body struct {
		Comment     string `json:"comment"`
		ORCTicketNo string `json:"orcTicketNo"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	rel, err := svc.PublishRelease(r.Context(), chi.URLParam(r, "nsId"), actor.Username, body.Comment, body.ORCTicketNo)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	s.app.RecordAuditEvent(r.Context(), pid, actor.Username, "config.release.publish", "cfg_namespace", rel.NamespaceID, map[string]any{
		"releaseVer": rel.ReleaseVer, "etag": rel.ETag, "orcTicketNo": rel.ORCTicketNo,
	})
	writeJSON(w, http.StatusCreated, rel)
}

func (s *Server) handleListConfigReleases(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	if err := authz.Authorize(ActorFromContext(r.Context()), pid, "resource.read"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireConfigCenter(w, r)
	if svc == nil {
		return
	}
	rels, err := svc.ListReleases(r.Context(), chi.URLParam(r, "nsId"))
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": rels})
}

func (s *Server) handleRollbackConfigRelease(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	actor := ActorFromContext(r.Context())
	if err := authz.Authorize(actor, pid, "instance.write"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireConfigCenter(w, r)
	if svc == nil {
		return
	}
	var body struct {
		TargetVer int    `json:"targetVer"`
		Comment   string `json:"comment"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "CFG", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	rel, err := svc.RollbackRelease(r.Context(), chi.URLParam(r, "nsId"), actor.Username, body.TargetVer, body.Comment)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	s.app.RecordAuditEvent(r.Context(), pid, actor.Username, "config.release.rollback", "cfg_namespace", rel.NamespaceID, map[string]any{
		"targetVer": body.TargetVer, "newReleaseVer": rel.ReleaseVer,
	})
	writeJSON(w, http.StatusOK, rel)
}

func (s *Server) handleCloneConfigNamespace(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	actor := ActorFromContext(r.Context())
	if err := authz.Authorize(actor, pid, "instance.write"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireConfigCenter(w, r)
	if svc == nil {
		return
	}
	var body struct {
		TargetEnv  string `json:"targetEnv"`
		TargetName string `json:"targetName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "CFG", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	cloned, err := svc.CloneNamespace(r.Context(), pid, chi.URLParam(r, "nsId"), body.TargetEnv, body.TargetName, actor.Username)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusCreated, cloned)
}

func (s *Server) handlePreviewMergedParams(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	if err := authz.Authorize(ActorFromContext(r.Context()), pid, "resource.read"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireConfigCenter(w, r)
	if svc == nil {
		return
	}
	var body struct {
		NamespaceID   string                        `json:"namespaceId"`
		GlobalParams  map[string]any                `json:"globalParams"`
		ServiceParams map[string]any                `json:"serviceParams"`
		Specs         []configcenter.K8sServiceSpec `json:"specs"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "CFG", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	global := make(map[string]any)
	if body.NamespaceID != "" {
		if m, err := svc.ExportGlobalParamsMap(r.Context(), body.NamespaceID); err == nil {
			for k, v := range m {
				global[k] = v
			}
		}
	}
	for k, v := range body.GlobalParams {
		global[k] = v
	}
	merged, trace, normSpecs, err := configcenter.ResolveEffectiveParams(global, body.ServiceParams, body.Specs)
	if err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "CFG", http.StatusUnprocessableEntity, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"effectiveParams": merged,
		"trace":           trace,
		"normalizedSpecs": normSpecs,
	})
}

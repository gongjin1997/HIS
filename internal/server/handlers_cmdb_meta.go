package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/kinstall/kinstall/internal/authz"
	"github.com/kinstall/kinstall/internal/cmdb"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

func (s *Server) requireCMDB(w http.ResponseWriter, r *http.Request) *cmdb.Service {
	svc := s.app.CMDB()
	if svc == nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeDBUnavailableOrSchema, "CMDB", http.StatusServiceUnavailable, sdk.CodeDBUnavailableOrSchema, false, fmt.Errorf("cmdb service unavailable")))
		return nil
	}
	return svc
}

// --- Overview & Meta-Model Engine ---

func (s *Server) handleCMDBOverviewStats(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	if err := authz.Authorize(ActorFromContext(r.Context()), pid, "resource.read"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireCMDB(w, r)
	if svc == nil {
		return
	}
	st, err := svc.GetOverviewStats(r.Context(), pid)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleGetMetaModels(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	if err := authz.Authorize(ActorFromContext(r.Context()), pid, "resource.read"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireCMDB(w, r)
	if svc == nil {
		return
	}
	ov, err := svc.GetMetaModelOverview(r.Context(), pid)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, ov)
}

func (s *Server) handleCreateModelGroup(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	if err := authz.Authorize(ActorFromContext(r.Context()), pid, "host.write"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireCMDB(w, r)
	if svc == nil {
		return
	}
	var body struct {
		Code      string `json:"code"`
		Name      string `json:"name"`
		SortOrder int    `json:"sortOrder"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "CMDB", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	g, err := svc.CreateModelGroup(r.Context(), pid, body.Code, body.Name, body.SortOrder)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusCreated, g)
}

func (s *Server) handleCreateModel(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	if err := authz.Authorize(ActorFromContext(r.Context()), pid, "host.write"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireCMDB(w, r)
	if svc == nil {
		return
	}
	var m cmdb.Model
	if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "CMDB", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	created, err := svc.CreateModel(r.Context(), pid, &m)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleUpdateModelPosition(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	if err := authz.Authorize(ActorFromContext(r.Context()), pid, "host.write"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireCMDB(w, r)
	if svc == nil {
		return
	}
	var body struct {
		PosX float64 `json:"posX"`
		PosY float64 `json:"posY"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "CMDB", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	if err := svc.UpdateModelPosition(r.Context(), pid, chi.URLParam(r, "modelCode"), body.PosX, body.PosY); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleDeleteModel(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	if err := authz.Authorize(ActorFromContext(r.Context()), pid, "host.write"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireCMDB(w, r)
	if svc == nil {
		return
	}
	if err := svc.DeleteModel(r.Context(), pid, chi.URLParam(r, "modelCode")); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": chi.URLParam(r, "modelCode")})
}

func (s *Server) handleUpsertModelAttribute(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	if err := authz.Authorize(ActorFromContext(r.Context()), pid, "host.write"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireCMDB(w, r)
	if svc == nil {
		return
	}
	var a cmdb.ModelAttribute
	if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "CMDB", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	saved, err := svc.UpsertModelAttribute(r.Context(), pid, chi.URLParam(r, "modelCode"), &a)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

func (s *Server) handleDeleteModelAttribute(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	if err := authz.Authorize(ActorFromContext(r.Context()), pid, "host.write"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireCMDB(w, r)
	if svc == nil {
		return
	}
	if err := svc.DeleteModelAttribute(r.Context(), pid, chi.URLParam(r, "attrId")); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": chi.URLParam(r, "attrId")})
}

func (s *Server) handleCreateModelRelation(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	if err := authz.Authorize(ActorFromContext(r.Context()), pid, "host.write"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireCMDB(w, r)
	if svc == nil {
		return
	}
	var rel cmdb.ModelRelation
	if err := json.NewDecoder(r.Body).Decode(&rel); err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "CMDB", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	created, err := svc.CreateModelRelation(r.Context(), pid, &rel)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleDeleteModelRelation(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	if err := authz.Authorize(ActorFromContext(r.Context()), pid, "host.write"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireCMDB(w, r)
	if svc == nil {
		return
	}
	if err := svc.DeleteModelRelation(r.Context(), pid, chi.URLParam(r, "relId")); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": chi.URLParam(r, "relId")})
}

// --- Generic CI Instances, Lineage Graph, History & Excelize .xlsx ---

func (s *Server) handleListCIInstances(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	if err := authz.Authorize(ActorFromContext(r.Context()), pid, "resource.read"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireCMDB(w, r)
	if svc == nil {
		return
	}
	items, err := svc.ListCIInstances(r.Context(), pid, r.URL.Query().Get("modelCode"), r.URL.Query().Get("q"))
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleCreateCIInstance(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	actor := ActorFromContext(r.Context())
	if err := authz.Authorize(actor, pid, "host.write"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireCMDB(w, r)
	if svc == nil {
		return
	}
	var ci cmdb.CIInstance
	if err := json.NewDecoder(r.Body).Decode(&ci); err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "CMDB", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	created, err := svc.CreateCIInstance(r.Context(), pid, actor.Username, &ci)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleUpdateCIInstance(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	actor := ActorFromContext(r.Context())
	if err := authz.Authorize(actor, pid, "host.write"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireCMDB(w, r)
	if svc == nil {
		return
	}
	var patch cmdb.CIInstance
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "CMDB", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	updated, err := svc.UpdateCIInstance(r.Context(), pid, chi.URLParam(r, "ciId"), actor.Username, &patch)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleDeleteCIInstance(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	actor := ActorFromContext(r.Context())
	if err := authz.Authorize(actor, pid, "host.write"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireCMDB(w, r)
	if svc == nil {
		return
	}
	if err := svc.DeleteCIInstance(r.Context(), pid, chi.URLParam(r, "ciId"), actor.Username); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": chi.URLParam(r, "ciId")})
}

func (s *Server) handleGetCILineageGraph(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	if err := authz.Authorize(ActorFromContext(r.Context()), pid, "resource.read"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireCMDB(w, r)
	if svc == nil {
		return
	}
	depth, _ := strconv.Atoi(r.URL.Query().Get("depth"))
	g, err := svc.GetCILineageGraph(r.Context(), pid, chi.URLParam(r, "ciId"), depth)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, g)
}

func (s *Server) handleListCIHistory(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	if err := authz.Authorize(ActorFromContext(r.Context()), pid, "resource.read"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireCMDB(w, r)
	if svc == nil {
		return
	}
	items, err := svc.ListCIHistory(r.Context(), pid, r.URL.Query().Get("ciId"), 50)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleCreateCIRelation(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	if err := authz.Authorize(ActorFromContext(r.Context()), pid, "host.write"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireCMDB(w, r)
	if svc == nil {
		return
	}
	var rel cmdb.CIRelation
	if err := json.NewDecoder(r.Body).Decode(&rel); err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "CMDB", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	created, err := svc.CreateCIRelation(r.Context(), pid, &rel)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleDeleteCIRelation(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	if err := authz.Authorize(ActorFromContext(r.Context()), pid, "host.write"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireCMDB(w, r)
	if svc == nil {
		return
	}
	if err := svc.DeleteCIRelation(r.Context(), pid, chi.URLParam(r, "relId")); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": chi.URLParam(r, "relId")})
}

func (s *Server) handleExportModelTemplateXLSX(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	if err := authz.Authorize(ActorFromContext(r.Context()), pid, "resource.read"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireCMDB(w, r)
	if svc == nil {
		return
	}
	data, fname, err := svc.ExportModelTemplateXLSX(r.Context(), pid, chi.URLParam(r, "modelCode"))
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, fname))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (s *Server) handleExportInstancesXLSX(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	if err := authz.Authorize(ActorFromContext(r.Context()), pid, "resource.read"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireCMDB(w, r)
	if svc == nil {
		return
	}
	data, fname, err := svc.ExportInstancesXLSX(r.Context(), pid, chi.URLParam(r, "modelCode"))
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, fname))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (s *Server) handleImportInstancesXLSX(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	actor := ActorFromContext(r.Context())
	if err := authz.Authorize(actor, pid, "host.write"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireCMDB(w, r)
	if svc == nil {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32<<20)
	// #nosec G120 -- request body explicitly capped at 32 MiB via http.MaxBytesReader above
	if err := r.ParseMultipartForm(16 << 20); err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "CMDB", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	f, _, err := r.FormFile("file")
	if err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "CMDB", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, fmt.Errorf("missing xlsx file field")))
		return
	}
	defer func() { _ = f.Close() }()
	dryRun := r.FormValue("dryRun") == "true" || r.URL.Query().Get("dryRun") == "true"
	rep, err := svc.ImportInstancesXLSX(r.Context(), pid, chi.URLParam(r, "modelCode"), actor.Username, f, dryRun)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

// --- Dual Topology, Host Bindings, Service Templates & Whitelist Collectors ---

func (s *Server) handleListTopologyTree(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	if err := authz.Authorize(ActorFromContext(r.Context()), pid, "resource.read"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireCMDB(w, r)
	if svc == nil {
		return
	}
	nodes, err := svc.ListTopologyTree(r.Context(), pid, r.URL.Query().Get("kind"))
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": nodes})
}

func (s *Server) handleCreateTopologyNode(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	if err := authz.Authorize(ActorFromContext(r.Context()), pid, "host.write"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireCMDB(w, r)
	if svc == nil {
		return
	}
	var node cmdb.TopologyNode
	if err := json.NewDecoder(r.Body).Decode(&node); err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "CMDB", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	created, err := svc.CreateTopologyNode(r.Context(), pid, &node)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleDeleteTopologyNode(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	if err := authz.Authorize(ActorFromContext(r.Context()), pid, "host.write"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireCMDB(w, r)
	if svc == nil {
		return
	}
	if err := svc.DeleteTopologyNode(r.Context(), pid, chi.URLParam(r, "nodeId")); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": chi.URLParam(r, "nodeId")})
}

func (s *Server) handleListTopologyNodeHosts(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	if err := authz.Authorize(ActorFromContext(r.Context()), pid, "resource.read"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireCMDB(w, r)
	if svc == nil {
		return
	}
	hostIDs, err := svc.ListBoundHostIDs(r.Context(), pid, chi.URLParam(r, "nodeId"))
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"hostIds": hostIDs})
}

func (s *Server) handleBindTopologyHost(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	if err := authz.Authorize(ActorFromContext(r.Context()), pid, "host.write"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireCMDB(w, r)
	if svc == nil {
		return
	}
	var body struct {
		HostID string `json:"hostId"`
		Action string `json:"action"` // bind | unbind
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "CMDB", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	nid := chi.URLParam(r, "nodeId")
	var err error
	if body.Action == "unbind" {
		err = svc.UnbindHostFromTopoNode(r.Context(), pid, body.HostID, nid)
	} else {
		err = svc.BindHostToTopoNode(r.Context(), pid, body.HostID, nid)
	}
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleApplyServiceTemplate(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	if err := authz.Authorize(ActorFromContext(r.Context()), pid, "host.write"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireCMDB(w, r)
	if svc == nil {
		return
	}
	var body struct {
		TemplateID string `json:"templateId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "CMDB", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	if err := svc.ApplyServiceTemplateToNode(r.Context(), pid, chi.URLParam(r, "nodeId"), body.TemplateID); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "applied"})
}

func (s *Server) handleListServiceTemplates(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	if err := authz.Authorize(ActorFromContext(r.Context()), pid, "resource.read"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireCMDB(w, r)
	if svc == nil {
		return
	}
	items, err := svc.ListServiceTemplates(r.Context(), pid)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleCreateServiceTemplate(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	if err := authz.Authorize(ActorFromContext(r.Context()), pid, "host.write"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireCMDB(w, r)
	if svc == nil {
		return
	}
	var tpl cmdb.ServiceTemplate
	if err := json.NewDecoder(r.Body).Decode(&tpl); err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "CMDB", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	created, err := svc.CreateServiceTemplate(r.Context(), pid, &tpl)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleDeleteServiceTemplate(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	if err := authz.Authorize(ActorFromContext(r.Context()), pid, "host.write"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireCMDB(w, r)
	if svc == nil {
		return
	}
	if err := svc.DeleteServiceTemplate(r.Context(), pid, chi.URLParam(r, "tplId")); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": chi.URLParam(r, "tplId")})
}

func (s *Server) handleListWhitelistCollectors(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"items": cmdb.WhitelistCollectors()})
}

func (s *Server) handleSyncHostsToCMDB(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	actor := ActorFromContext(r.Context())
	if err := authz.Authorize(actor, pid, "host.write"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireCMDB(w, r)
	if svc == nil {
		return
	}
	hosts, err := s.app.ListHosts(r.Context(), actor, pid)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	synced := 0
	for i := range hosts {
		if _, serr := svc.SyncHostToCI(r.Context(), pid, actor.Username, &hosts[i]); serr == nil {
			synced++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"syncedHosts": synced, "totalHosts": len(hosts)})
}

// --- DCIM Racks & IPAM Prefixes/Addresses ---

func (s *Server) handleListRacks(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	if err := authz.Authorize(ActorFromContext(r.Context()), pid, "resource.read"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireCMDB(w, r)
	if svc == nil {
		return
	}
	items, err := svc.ListRacks(r.Context(), pid)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleCreateRack(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	if err := authz.Authorize(ActorFromContext(r.Context()), pid, "host.write"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireCMDB(w, r)
	if svc == nil {
		return
	}
	var rack cmdb.Rack
	if err := json.NewDecoder(r.Body).Decode(&rack); err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "DCIM", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	created, err := svc.CreateRack(r.Context(), pid, &rack)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleDeleteRack(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	if err := authz.Authorize(ActorFromContext(r.Context()), pid, "host.write"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireCMDB(w, r)
	if svc == nil {
		return
	}
	if err := svc.DeleteRack(r.Context(), pid, chi.URLParam(r, "rackId")); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": chi.URLParam(r, "rackId")})
}

func (s *Server) handleMountRackUnit(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	if err := authz.Authorize(ActorFromContext(r.Context()), pid, "host.write"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireCMDB(w, r)
	if svc == nil {
		return
	}
	var u cmdb.RackUnit
	if err := json.NewDecoder(r.Body).Decode(&u); err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "DCIM", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	mounted, err := svc.MountRackUnit(r.Context(), pid, chi.URLParam(r, "rackId"), &u)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusCreated, mounted)
}

func (s *Server) handleUnmountRackUnit(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	if err := authz.Authorize(ActorFromContext(r.Context()), pid, "host.write"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireCMDB(w, r)
	if svc == nil {
		return
	}
	if err := svc.UnmountRackUnit(r.Context(), pid, chi.URLParam(r, "unitId")); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": chi.URLParam(r, "unitId")})
}

func (s *Server) handleListIPAMPrefixes(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	if err := authz.Authorize(ActorFromContext(r.Context()), pid, "resource.read"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireCMDB(w, r)
	if svc == nil {
		return
	}
	items, err := svc.ListPrefixes(r.Context(), pid)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleCreateIPAMPrefix(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	if err := authz.Authorize(ActorFromContext(r.Context()), pid, "host.write"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireCMDB(w, r)
	if svc == nil {
		return
	}
	var p cmdb.IPAMPrefix
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "IPAM", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	created, err := svc.CreatePrefix(r.Context(), pid, &p)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleDeleteIPAMPrefix(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	if err := authz.Authorize(ActorFromContext(r.Context()), pid, "host.write"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireCMDB(w, r)
	if svc == nil {
		return
	}
	if err := svc.DeletePrefix(r.Context(), pid, chi.URLParam(r, "prefixId")); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": chi.URLParam(r, "prefixId")})
}

func (s *Server) handleListIPAddresses(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	if err := authz.Authorize(ActorFromContext(r.Context()), pid, "resource.read"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireCMDB(w, r)
	if svc == nil {
		return
	}
	items, err := svc.ListIPAddresses(r.Context(), pid, chi.URLParam(r, "prefixId"))
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleAllocateIP(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	if err := authz.Authorize(ActorFromContext(r.Context()), pid, "host.write"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireCMDB(w, r)
	if svc == nil {
		return
	}
	var req cmdb.IPAMAddress
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "IPAM", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	alloc, err := svc.AllocateIP(r.Context(), pid, chi.URLParam(r, "prefixId"), &req)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusCreated, alloc)
}

func (s *Server) handleReleaseIP(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	if err := authz.Authorize(ActorFromContext(r.Context()), pid, "host.write"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireCMDB(w, r)
	if svc == nil {
		return
	}
	if err := svc.ReleaseIP(r.Context(), pid, chi.URLParam(r, "ipId")); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": chi.URLParam(r, "ipId")})
}

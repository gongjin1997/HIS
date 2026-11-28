package httptransport

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/pkg/platform/httpx"
)

func (s *Server) handleListAssetModels(w http.ResponseWriter, r *http.Request) {
	_, scope := ctxScope(r)
	items, err := s.d.Assets.ListModels(r.Context(), scope)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.WriteList(w, items, nil, len(items))
}

func (s *Server) handleCreateAssetModel(w http.ResponseWriter, r *http.Request) {
	tc, scope := ctxScope(r)
	var req struct {
		GroupID     string              `json:"groupId"`
		Code        string              `json:"code"`
		Name        string              `json:"name"`
		Icon        string              `json:"icon"`
		Description string              `json:"description"`
		Fields      []domain.ModelField `json:"fields"`
	}
	if _, err := httpx.DecodeStrictJSON(w, r, &req, 256<<10); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error(), nil)
		return
	}
	m, err := s.d.Assets.CreateModel(r.Context(), tc, scope, req.GroupID, req.Code, req.Name, req.Icon, req.Description, req.Fields)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, m)
}

func (s *Server) handleListRelationTypes(w http.ResponseWriter, r *http.Request) {
	_, scope := ctxScope(r)
	items, err := s.d.Assets.ListRelationTypes(r.Context(), scope)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.WriteList(w, items, nil, len(items))
}

func (s *Server) handleDeleteRelationType(w http.ResponseWriter, r *http.Request) {
	_, scope := ctxScope(r)
	if err := s.d.Assets.DeleteRelationType(r.Context(), scope, chi.URLParam(r, "relTypeId")); err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (s *Server) handleListAssetInstances(w http.ResponseWriter, r *http.Request) {
	_, scope := ctxScope(r)
	modelID := r.URL.Query().Get("modelId")
	kw := r.URL.Query().Get("q")
	items, err := s.d.Assets.ListInstances(r.Context(), scope, modelID, kw)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.WriteList(w, items, nil, len(items))
}

func (s *Server) handleGetAssetInstance(w http.ResponseWriter, r *http.Request) {
	_, scope := ctxScope(r)
	inst, err := s.d.Assets.GetInstance(r.Context(), scope, chi.URLParam(r, "instanceId"))
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.SetVersionETag(w, inst.Version)
	httpx.WriteJSON(w, http.StatusOK, inst)
}

func (s *Server) handleCreateAssetInstance(w http.ResponseWriter, r *http.Request) {
	tc, scope := ctxScope(r)
	var req struct {
		ModelID   string         `json:"modelId"`
		Name      string         `json:"name"`
		UniqueKey string         `json:"uniqueKey"`
		Status    string         `json:"status"`
		Attrs     map[string]any `json:"attrs"`
	}
	if _, err := httpx.DecodeStrictJSON(w, r, &req, 256<<10); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error(), nil)
		return
	}
	inst, err := s.d.Assets.CreateInstance(r.Context(), tc, scope, req.ModelID, req.Name, req.UniqueKey, req.Status, req.Attrs)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.SetVersionETag(w, inst.Version)
	httpx.WriteJSON(w, http.StatusCreated, inst)
}

func (s *Server) handleUpdateAssetInstance(w http.ResponseWriter, r *http.Request) {
	tc, scope := ctxScope(r)
	expectedVer, st, code, err := httpx.ParseIfMatch(r)
	if err != nil {
		httpx.WriteError(w, st, code, err.Error(), nil)
		return
	}
	var req struct {
		Name   string         `json:"name"`
		Status string         `json:"status"`
		Attrs  map[string]any `json:"attrs"`
	}
	if _, err := httpx.DecodeStrictJSON(w, r, &req, 256<<10); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error(), nil)
		return
	}
	inst, err := s.d.Assets.UpdateInstance(r.Context(), tc, scope, chi.URLParam(r, "instanceId"), expectedVer, req.Name, req.Status, req.Attrs)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.SetVersionETag(w, inst.Version)
	httpx.WriteJSON(w, http.StatusOK, inst)
}

func (s *Server) handleCascadeDeleteInstance(w http.ResponseWriter, r *http.Request) {
	tc, scope := ctxScope(r)
	op, err := s.d.Assets.CascadeDeleteInstance(r.Context(), tc, scope, chi.URLParam(r, "instanceId"))
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, op)
}

func (s *Server) handleGetTopology(w http.ResponseWriter, r *http.Request) {
	_, scope := ctxScope(r)
	treeKind := domain.TreeKind(r.URL.Query().Get("treeKind"))
	graph, err := s.d.Assets.GetTopologyGraph(r.Context(), scope, treeKind)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, graph)
}

func (s *Server) handleCreateAssetEdge(w http.ResponseWriter, r *http.Request) {
	tc, scope := ctxScope(r)
	var req struct {
		TreeKind  domain.TreeKind `json:"treeKind"`
		RelTypeID string          `json:"relTypeId"`
		SourceID  string          `json:"sourceId"`
		TargetID  string          `json:"targetId"`
		Metadata  map[string]any  `json:"metadata"`
	}
	if _, err := httpx.DecodeStrictJSON(w, r, &req, 64<<10); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error(), nil)
		return
	}
	edge, err := s.d.Assets.CreateEdge(r.Context(), tc, scope, req.TreeKind, req.RelTypeID, req.SourceID, req.TargetID, req.Metadata)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, edge)
}

func (s *Server) handleDeleteAssetEdge(w http.ResponseWriter, r *http.Request) {
	_, scope := ctxScope(r)
	if err := s.d.Assets.DeleteEdge(r.Context(), scope, chi.URLParam(r, "edgeId")); err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (s *Server) handleExportXLSX(w http.ResponseWriter, r *http.Request) {
	s.writeXLSX(w, r, false)
}

func (s *Server) handleExportTemplateXLSX(w http.ResponseWriter, r *http.Request) {
	s.writeXLSX(w, r, true)
}

func (s *Server) writeXLSX(w http.ResponseWriter, r *http.Request, templateOnly bool) {
	_, scope := ctxScope(r)
	modelID := chi.URLParam(r, "modelId")
	data, err := s.d.Assets.ExportInstancesXLSX(r.Context(), scope, modelID, templateOnly)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	suffix := "export"
	if templateOnly {
		suffix = "template"
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="cmdb-%s-%s.xlsx"`, sanitizeFilename(modelID), suffix))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data) // #nosec G705 -- binary xlsx attachment with explicit spreadsheet content-type
}

func sanitizeFilename(s string) string {
	var b []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			b = append(b, c)
		}
	}
	if len(b) == 0 {
		return "model"
	}
	return string(b)
}

func (s *Server) handleImportXLSX(w http.ResponseWriter, r *http.Request) {
	tc, scope := ctxScope(r)
	modelID := chi.URLParam(r, "modelId")
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
	if err := r.ParseMultipartForm(10 << 20); err != nil { // #nosec G120 -- bounded by MaxBytesReader above
		// Allow raw binary body fallback as well
		sum, ierr := s.d.Assets.ImportInstancesXLSX(r.Context(), tc, scope, modelID, r.Body)
		if ierr != nil {
			writeDomainErr(w, ierr)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, sum)
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "MISSING_FILE", "请上传表单字段 file (.xlsx)", nil)
		return
	}
	defer func() { _ = file.Close() }()
	sum, err := s.d.Assets.ImportInstancesXLSX(r.Context(), tc, scope, modelID, file)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, sum)
}

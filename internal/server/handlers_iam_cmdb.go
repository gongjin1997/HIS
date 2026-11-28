package server

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/kinstall/kinstall/internal/cmdb"
	"github.com/kinstall/kinstall/pkg/platform/auth"
	"github.com/kinstall/kinstall/pkg/platform/httpx"
)

func clientIP(r *http.Request) string {
	if xf := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xf != "" {
		return strings.Split(xf, ",")[0]
	}
	return r.RemoteAddr
}

func (s *Server) currentPrincipal(r *http.Request) *auth.Principal {
	p, _ := auth.PrincipalFromContext(r.Context())
	return p
}

func (s *Server) wsID(r *http.Request) string {
	if p := s.currentPrincipal(r); p != nil && p.WorkspaceID != "" {
		return p.WorkspaceID
	}
	return s.defaultWSID
}

// =============================================================================
// Auth, IAM, Workspaces, PATs, Credentials & Audit Hash-Chain
// =============================================================================

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := httpx.DecodeStrictJSON(w, r, &req, 64<<10); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	res, err := s.iam.Login(r.Context(), req.Username, req.Password, clientIP(r))
	if err != nil {
		httpx.WriteError(w, http.StatusUnauthorized, "AUTH_FAILED", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, res)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	p := s.currentPrincipal(r)
	wss, err := s.iam.ListWorkspaces(r.Context())
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"user":       p,
		"workspaces": wss,
		"version":    s.version,
	})
}

func (s *Server) handleListWorkspaces(w http.ResponseWriter, r *http.Request) {
	items, err := s.iam.ListWorkspaces(r.Context())
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	httpx.WriteList(w, items, len(items))
}

func (s *Server) handleCreateWorkspace(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code        string `json:"code"`
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := httpx.DecodeStrictJSON(w, r, &req, 64<<10); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	ws, err := s.iam.CreateWorkspace(r.Context(), req.Code, req.Name, req.Description)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "CREATE_FAILED", err.Error())
		return
	}
	_ = s.cmdb.EnsureSeeded(r.Context(), ws.ID)
	_ = s.delivery.EnsureSeededPlugins(r.Context(), ws.ID)
	httpx.WriteJSON(w, http.StatusCreated, ws)
}

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	items, err := s.iam.ListUsers(r.Context())
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	httpx.WriteList(w, items, len(items))
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username    string `json:"username"`
		DisplayName string `json:"displayName"`
		Email       string `json:"email"`
		Role        string `json:"role"`
		Password    string `json:"password"`
	}
	if err := httpx.DecodeStrictJSON(w, r, &req, 64<<10); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	u, err := s.iam.CreateUser(r.Context(), req.Username, req.DisplayName, req.Email, req.Role, req.Password)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "CREATE_FAILED", err.Error())
		return
	}
	p := s.currentPrincipal(r)
	_ = s.iam.RecordAudit(r.Context(), s.wsID(r), p.Username, p.Role, "iam.user.create", "user", u.ID, "ok", "创建账号 "+u.Username+" ("+u.Role+")", clientIP(r))
	httpx.WriteJSON(w, http.StatusCreated, u)
}

func (s *Server) handleUpdateUserRole(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "id")
	var req struct {
		Role    string `json:"role"`
		Enabled bool   `json:"enabled"`
	}
	if err := httpx.DecodeStrictJSON(w, r, &req, 64<<10); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	if err := s.iam.UpdateUserRole(r.Context(), userID, req.Role, req.Enabled); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "UPDATE_FAILED", err.Error())
		return
	}
	p := s.currentPrincipal(r)
	_ = s.iam.RecordAudit(r.Context(), s.wsID(r), p.Username, p.Role, "iam.user.update", "user", userID, "ok", fmt.Sprintf("role=%s enabled=%v", req.Role, req.Enabled), clientIP(r))
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) handleListAPITokens(w http.ResponseWriter, r *http.Request) {
	items, err := s.iam.ListAPITokens(r.Context(), s.wsID(r))
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	httpx.WriteList(w, items, len(items))
}

func (s *Server) handleCreateAPIToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name    string `json:"name"`
		Role    string `json:"role"`
		TTLDays int    `json:"ttlDays"`
	}
	if err := httpx.DecodeStrictJSON(w, r, &req, 64<<10); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	p := s.currentPrincipal(r)
	role := req.Role
	if role == "" {
		role = p.Role
	}
	tok, err := s.iam.CreateAPIToken(r.Context(), p.UserID, s.wsID(r), req.Name, role, req.TTLDays)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "CREATE_FAILED", err.Error())
		return
	}
	_ = s.iam.RecordAudit(r.Context(), s.wsID(r), p.Username, p.Role, "iam.pat.create", "api_token", tok.ID, "ok", "签发 API 令牌 "+tok.Name, clientIP(r))
	httpx.WriteJSON(w, http.StatusCreated, tok)
}

func (s *Server) handleRevokeAPIToken(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.iam.RevokeAPIToken(r.Context(), s.wsID(r), id); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "REVOKE_FAILED", err.Error())
		return
	}
	p := s.currentPrincipal(r)
	_ = s.iam.RecordAudit(r.Context(), s.wsID(r), p.Username, p.Role, "iam.pat.revoke", "api_token", id, "ok", "吊销 API 令牌", clientIP(r))
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"status": "revoked"})
}

func (s *Server) handleListCredentials(w http.ResponseWriter, r *http.Request) {
	items, err := s.iam.ListCredentials(r.Context(), s.wsID(r))
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	httpx.WriteList(w, items, len(items))
}

func (s *Server) handleCreateCredential(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		Kind        string `json:"kind"`
		Username    string `json:"username"`
		Secret      string `json:"secret"`
		Passphrase  string `json:"passphrase"`
		Description string `json:"description"`
	}
	if err := httpx.DecodeStrictJSON(w, r, &req, 256<<10); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	p := s.currentPrincipal(r)
	c, err := s.iam.CreateCredential(r.Context(), s.wsID(r), p.Username, req.Name, req.Kind, req.Username, req.Secret, req.Passphrase, req.Description)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "CREATE_FAILED", err.Error())
		return
	}
	_ = s.iam.RecordAudit(r.Context(), s.wsID(r), p.Username, p.Role, "iam.credential.create", "credential", c.ID, "ok", "录入加密凭据 "+c.Name+" ("+c.Kind+")", clientIP(r))
	httpx.WriteJSON(w, http.StatusCreated, c)
}

func (s *Server) handleDeleteCredential(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.iam.DeleteCredential(r.Context(), s.wsID(r), id); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "DELETE_FAILED", err.Error())
		return
	}
	p := s.currentPrincipal(r)
	_ = s.iam.RecordAudit(r.Context(), s.wsID(r), p.Username, p.Role, "iam.credential.delete", "credential", id, "ok", "删除凭据", clientIP(r))
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"status": "deleted"})
}

func (s *Server) handleListAuditLogs(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := s.iam.ListAuditLogs(r.Context(), s.wsID(r), limit)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	httpx.WriteList(w, items, len(items))
}

func (s *Server) handleVerifyAuditChain(w http.ResponseWriter, r *http.Request) {
	res, err := s.iam.VerifyAuditChain(r.Context(), s.wsID(r))
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, res)
}

// =============================================================================
// Overview & Unified CMDB Engine
// =============================================================================

func (s *Server) handleOverviewStats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.cmdb.GetOverviewStats(r.Context(), s.wsID(r))
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, stats)
}

func (s *Server) handleListModelGroups(w http.ResponseWriter, r *http.Request) {
	items, err := s.cmdb.ListModelGroups(r.Context(), s.wsID(r))
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	httpx.WriteList(w, items, len(items))
}

func (s *Server) handleCreateModelGroup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code      string `json:"code"`
		Name      string `json:"name"`
		SortOrder int    `json:"sortOrder"`
	}
	if err := httpx.DecodeStrictJSON(w, r, &req, 64<<10); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	g, err := s.cmdb.CreateModelGroup(r.Context(), s.wsID(r), req.Code, req.Name, req.SortOrder)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "CREATE_FAILED", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, g)
}

func (s *Server) handleListModels(w http.ResponseWriter, r *http.Request) {
	inc := r.URL.Query().Get("includeAttrs") != "false"
	items, err := s.cmdb.ListModels(r.Context(), s.wsID(r), inc)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	httpx.WriteList(w, items, len(items))
}

func (s *Server) handleCreateModel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		GroupCode   string `json:"groupCode"`
		Code        string `json:"code"`
		Name        string `json:"name"`
		Icon        string `json:"icon"`
		Description string `json:"description"`
	}
	if err := httpx.DecodeStrictJSON(w, r, &req, 64<<10); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	m, err := s.cmdb.CreateModel(r.Context(), s.wsID(r), req.GroupCode, req.Code, req.Name, req.Icon, req.Description)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "CREATE_FAILED", err.Error())
		return
	}
	p := s.currentPrincipal(r)
	_ = s.iam.RecordAudit(r.Context(), s.wsID(r), p.Username, p.Role, "cmdb.model.create", "cmdb_model", m.Code, "ok", "新建元模型 "+m.Name, clientIP(r))
	httpx.WriteJSON(w, http.StatusCreated, m)
}

func (s *Server) handleDeleteModel(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	if err := s.cmdb.DeleteModel(r.Context(), s.wsID(r), code); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "DELETE_FAILED", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"status": "deleted"})
}

func (s *Server) handleSaveModelPositions(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Positions []cmdb.ModelPosition `json:"positions"`
	}
	if err := httpx.DecodeStrictJSON(w, r, &req, 256<<10); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	if err := s.cmdb.SaveModelPositions(r.Context(), s.wsID(r), req.Positions); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "SAVE_FAILED", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"status": "saved"})
}

func (s *Server) handleUpsertModelAttr(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	var req cmdb.ModelAttr
	if err := httpx.DecodeStrictJSON(w, r, &req, 64<<10); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	req.ModelCode = code
	a, err := s.cmdb.UpsertModelAttr(r.Context(), s.wsID(r), req)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "SAVE_FAILED", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, a)
}

func (s *Server) handleDeleteModelAttr(w http.ResponseWriter, r *http.Request) {
	attrID := chi.URLParam(r, "attrId")
	if err := s.cmdb.DeleteModelAttr(r.Context(), s.wsID(r), attrID); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "DELETE_FAILED", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"status": "deleted"})
}

func (s *Server) handleListModelRelations(w http.ResponseWriter, r *http.Request) {
	items, err := s.cmdb.ListModelRelations(r.Context(), s.wsID(r))
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	httpx.WriteList(w, items, len(items))
}

func (s *Server) handleCreateModelRelation(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SourceModel  string `json:"sourceModel"`
		TargetModel  string `json:"targetModel"`
		RelationCode string `json:"relationCode"`
		RelationName string `json:"relationName"`
		Cardinality  string `json:"cardinality"`
	}
	if err := httpx.DecodeStrictJSON(w, r, &req, 64<<10); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	rel, err := s.cmdb.CreateModelRelation(r.Context(), s.wsID(r), req.SourceModel, req.TargetModel, req.RelationCode, req.RelationName, req.Cardinality)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "CREATE_FAILED", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, rel)
}

func (s *Server) handleDeleteModelRelation(w http.ResponseWriter, r *http.Request) {
	if err := s.cmdb.DeleteModelRelation(r.Context(), s.wsID(r), chi.URLParam(r, "id")); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "DELETE_FAILED", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"status": "deleted"})
}

func (s *Server) handleListCIs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	items, err := s.cmdb.ListCIs(r.Context(), s.wsID(r), q.Get("modelCode"), q.Get("env"), q.Get("q"))
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	httpx.WriteList(w, items, len(items))
}

func (s *Server) handleCreateCI(w http.ResponseWriter, r *http.Request) {
	var req cmdb.UpsertCIRequest
	if err := httpx.DecodeStrictJSON(w, r, &req, 256<<10); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	p := s.currentPrincipal(r)
	ci, err := s.cmdb.CreateCI(r.Context(), s.wsID(r), p.Username, req)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "CREATE_FAILED", err.Error())
		return
	}
	_ = s.iam.RecordAudit(r.Context(), s.wsID(r), p.Username, p.Role, "cmdb.ci.create", ci.ModelCode, ci.ID, "ok", "录入 CI "+ci.Name, clientIP(r))
	httpx.WriteJSON(w, http.StatusCreated, ci)
}

func (s *Server) handleGetCI(w http.ResponseWriter, r *http.Request) {
	ci, err := s.cmdb.GetCI(r.Context(), s.wsID(r), chi.URLParam(r, "id"))
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "NOT_FOUND", "CI 实例不存在")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, ci)
}

func (s *Server) handleUpdateCI(w http.ResponseWriter, r *http.Request) {
	var req cmdb.UpsertCIRequest
	if err := httpx.DecodeStrictJSON(w, r, &req, 256<<10); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	p := s.currentPrincipal(r)
	ci, err := s.cmdb.UpdateCI(r.Context(), s.wsID(r), chi.URLParam(r, "id"), p.Username, req)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "UPDATE_FAILED", err.Error())
		return
	}
	_ = s.iam.RecordAudit(r.Context(), s.wsID(r), p.Username, p.Role, "cmdb.ci.update", ci.ModelCode, ci.ID, "ok", "变更 CI "+ci.Name, clientIP(r))
	httpx.WriteJSON(w, http.StatusOK, ci)
}

func (s *Server) handleDeleteCI(w http.ResponseWriter, r *http.Request) {
	ciID := chi.URLParam(r, "id")
	if err := s.cmdb.DeleteCI(r.Context(), s.wsID(r), ciID); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "DELETE_FAILED", err.Error())
		return
	}
	p := s.currentPrincipal(r)
	_ = s.iam.RecordAudit(r.Context(), s.wsID(r), p.Username, p.Role, "cmdb.ci.delete", "ci", ciID, "ok", "删除 CI 实例", clientIP(r))
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"status": "deleted"})
}

func (s *Server) handleCIHistory(w http.ResponseWriter, r *http.Request) {
	items, err := s.cmdb.ListCIHistory(r.Context(), s.wsID(r), chi.URLParam(r, "id"))
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	httpx.WriteList(w, items, len(items))
}

func (s *Server) handleCILineage(w http.ResponseWriter, r *http.Request) {
	depth, _ := strconv.Atoi(r.URL.Query().Get("depth"))
	g, err := s.cmdb.GetLineage(r.Context(), s.wsID(r), chi.URLParam(r, "id"), depth)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, g)
}

func (s *Server) handleCreateCIRelation(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SourceCIID   string `json:"sourceCiId"`
		TargetCIID   string `json:"targetCiId"`
		RelationCode string `json:"relationCode"`
		Note         string `json:"note"`
	}
	if err := httpx.DecodeStrictJSON(w, r, &req, 64<<10); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	rel, err := s.cmdb.CreateCIRelation(r.Context(), s.wsID(r), req.SourceCIID, req.TargetCIID, req.RelationCode, req.Note)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "CREATE_FAILED", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, rel)
}

func (s *Server) handleDeleteCIRelation(w http.ResponseWriter, r *http.Request) {
	if err := s.cmdb.DeleteCIRelation(r.Context(), s.wsID(r), chi.URLParam(r, "id")); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "DELETE_FAILED", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"status": "deleted"})
}

// Excel (.xlsx) Template, Export & Import
func (s *Server) handleExportTemplateXLSX(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	data, err := s.cmdb.ExportModelTemplateXLSX(r.Context(), s.wsID(r), code)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "EXPORT_FAILED", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="cmdb-%s-template.xlsx"`, code))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data) // #nosec G705 -- binary xlsx spreadsheet download with explicit octet content-type
}

func (s *Server) handleExportCIsXLSX(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	data, err := s.cmdb.ExportModelCIsXLSX(r.Context(), s.wsID(r), code)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "EXPORT_FAILED", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="cmdb-%s-export.xlsx"`, code))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data) // #nosec G705 -- binary xlsx spreadsheet download with explicit octet content-type
}

func (s *Server) handleImportCIsXLSX(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	r.Body = http.MaxBytesReader(w, r.Body, 25<<20)
	if err := r.ParseMultipartForm(25 << 20); err != nil { // #nosec G120 -- bounded by MaxBytesReader
		httpx.WriteError(w, http.StatusBadRequest, "BAD_UPLOAD", "请通过 multipart 表单上传 .xlsx 文件")
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "MISSING_FILE", "缺少 file 上传字段")
		return
	}
	defer func() { _ = file.Close() }()
	raw, err := io.ReadAll(io.LimitReader(file, 25<<20))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "READ_FAILED", err.Error())
		return
	}
	p := s.currentPrincipal(r)
	res, err := s.cmdb.ImportModelCIsXLSX(r.Context(), s.wsID(r), p.Username, code, raw)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "IMPORT_FAILED", err.Error())
		return
	}
	_ = s.iam.RecordAudit(r.Context(), s.wsID(r), p.Username, p.Role, "cmdb.excel.import", code, code, "ok", fmt.Sprintf("Excel 导入: created=%d updated=%d failed=%d", res.Created, res.Updated, res.Failed), clientIP(r))
	httpx.WriteJSON(w, http.StatusOK, res)
}

// Dual Topology & Service Templates
func (s *Server) handleTopologyTree(w http.ResponseWriter, r *http.Request) {
	treeType := r.URL.Query().Get("treeType")
	nodes, err := s.cmdb.GetTopologyTree(r.Context(), s.wsID(r), treeType)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	httpx.WriteList(w, nodes, len(nodes))
}

func (s *Server) handleCreateTopologyNode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TreeType          string `json:"treeType"`
		ParentID          string `json:"parentId"`
		NodeType          string `json:"nodeType"`
		Name              string `json:"name"`
		Code              string `json:"code"`
		ServiceTemplateID string `json:"serviceTemplateId"`
	}
	if err := httpx.DecodeStrictJSON(w, r, &req, 64<<10); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	n, err := s.cmdb.CreateTopologyNode(r.Context(), s.wsID(r), req.TreeType, req.ParentID, req.NodeType, req.Name, req.Code, req.ServiceTemplateID)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "CREATE_FAILED", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, n)
}

func (s *Server) handleDeleteTopologyNode(w http.ResponseWriter, r *http.Request) {
	if err := s.cmdb.DeleteTopologyNode(r.Context(), s.wsID(r), chi.URLParam(r, "id")); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "DELETE_FAILED", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"status": "deleted"})
}

func (s *Server) handleListTopoNodeCIs(w http.ResponseWriter, r *http.Request) {
	items, err := s.cmdb.ListTopoNodeCIs(r.Context(), s.wsID(r), chi.URLParam(r, "id"))
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	httpx.WriteList(w, items, len(items))
}

func (s *Server) handleBindCIToTopo(w http.ResponseWriter, r *http.Request) {
	nodeID := chi.URLParam(r, "id")
	var req struct {
		CIID string `json:"ciId"`
	}
	if err := httpx.DecodeStrictJSON(w, r, &req, 32<<10); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	if err := s.cmdb.BindCIToTopoNode(r.Context(), s.wsID(r), req.CIID, nodeID); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BIND_FAILED", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"status": "bound"})
}

func (s *Server) handleUnbindCIFromTopo(w http.ResponseWriter, r *http.Request) {
	if err := s.cmdb.UnbindCIFromTopoNode(r.Context(), s.wsID(r), chi.URLParam(r, "ciId"), chi.URLParam(r, "id")); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "UNBIND_FAILED", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"status": "unbound"})
}

func (s *Server) handleListServiceTemplates(w http.ResponseWriter, r *http.Request) {
	items, err := s.cmdb.ListServiceTemplates(r.Context(), s.wsID(r))
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	httpx.WriteList(w, items, len(items))
}

func (s *Server) handleCreateServiceTemplate(w http.ResponseWriter, r *http.Request) {
	var req cmdb.ServiceTemplate
	if err := httpx.DecodeStrictJSON(w, r, &req, 64<<10); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	tpl, err := s.cmdb.CreateServiceTemplate(r.Context(), s.wsID(r), req)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "CREATE_FAILED", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, tpl)
}

// Dynamic Groups
func (s *Server) handleListDynamicGroups(w http.ResponseWriter, r *http.Request) {
	items, err := s.cmdb.ListDynamicGroups(r.Context(), s.wsID(r))
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	httpx.WriteList(w, items, len(items))
}

func (s *Server) handleCreateDynamicGroup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string                  `json:"name"`
		ModelCode   string                  `json:"modelCode"`
		Conditions  []cmdb.DynamicCondition `json:"conditions"`
		Description string                  `json:"description"`
	}
	if err := httpx.DecodeStrictJSON(w, r, &req, 64<<10); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	g, err := s.cmdb.CreateDynamicGroup(r.Context(), s.wsID(r), req.Name, req.ModelCode, req.Conditions, req.Description)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "CREATE_FAILED", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, g)
}

func (s *Server) handleDeleteDynamicGroup(w http.ResponseWriter, r *http.Request) {
	if err := s.cmdb.DeleteDynamicGroup(r.Context(), s.wsID(r), chi.URLParam(r, "id")); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "DELETE_FAILED", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"status": "deleted"})
}

func (s *Server) handleEvaluateDynamicGroup(w http.ResponseWriter, r *http.Request) {
	items, err := s.cmdb.EvaluateDynamicGroup(r.Context(), s.wsID(r), chi.URLParam(r, "id"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "EVAL_FAILED", err.Error())
		return
	}
	httpx.WriteList(w, items, len(items))
}

// DCIM 42U Racks & IPAM CIDR
func (s *Server) handleListRacks(w http.ResponseWriter, r *http.Request) {
	items, err := s.cmdb.ListRacks(r.Context(), s.wsID(r))
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	httpx.WriteList(w, items, len(items))
}

func (s *Server) handleCreateRack(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Datacenter string  `json:"datacenter"`
		Room       string  `json:"room"`
		Name       string  `json:"name"`
		UHeight    int     `json:"uHeight"`
		PowerKW    float64 `json:"powerKw"`
		Notes      string  `json:"notes"`
	}
	if err := httpx.DecodeStrictJSON(w, r, &req, 64<<10); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	rack, err := s.cmdb.CreateRack(r.Context(), s.wsID(r), req.Datacenter, req.Room, req.Name, req.UHeight, req.PowerKW, req.Notes)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "CREATE_FAILED", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, rack)
}

func (s *Server) handleDeleteRack(w http.ResponseWriter, r *http.Request) {
	if err := s.cmdb.DeleteRack(r.Context(), s.wsID(r), chi.URLParam(r, "id")); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "DELETE_FAILED", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"status": "deleted"})
}

func (s *Server) handleMountRackUnit(w http.ResponseWriter, r *http.Request) {
	rackID := chi.URLParam(r, "id")
	var req struct {
		CIID       string `json:"ciId"`
		DeviceName string `json:"deviceName"`
		ModelCode  string `json:"modelCode"`
		UStart     int    `json:"uStart"`
		USize      int    `json:"uSize"`
		Face       string `json:"face"`
		PowerW     int    `json:"powerW"`
		ColorTag   string `json:"colorTag"`
	}
	if err := httpx.DecodeStrictJSON(w, r, &req, 64<<10); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	u, err := s.cmdb.MountRackUnit(r.Context(), s.wsID(r), rackID, req.CIID, req.DeviceName, req.ModelCode, req.UStart, req.USize, req.Face, req.PowerW, req.ColorTag)
	if err != nil {
		httpx.WriteError(w, http.StatusConflict, "U_SLOT_CONFLICT", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, u)
}

func (s *Server) handleUnmountRackUnit(w http.ResponseWriter, r *http.Request) {
	if err := s.cmdb.UnmountRackUnit(r.Context(), s.wsID(r), chi.URLParam(r, "unitId")); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "DELETE_FAILED", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"status": "unmounted"})
}

func (s *Server) handleListPrefixes(w http.ResponseWriter, r *http.Request) {
	items, err := s.cmdb.ListPrefixes(r.Context(), s.wsID(r))
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	httpx.WriteList(w, items, len(items))
}

func (s *Server) handleCreatePrefix(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CIDR        string `json:"cidr"`
		VLANID      int    `json:"vlanId"`
		Site        string `json:"site"`
		VRF         string `json:"vrf"`
		Gateway     string `json:"gateway"`
		Description string `json:"description"`
	}
	if err := httpx.DecodeStrictJSON(w, r, &req, 64<<10); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	pfx, err := s.cmdb.CreatePrefix(r.Context(), s.wsID(r), req.CIDR, req.VLANID, req.Site, req.VRF, req.Gateway, req.Description)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "CREATE_FAILED", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, pfx)
}

func (s *Server) handleDeletePrefix(w http.ResponseWriter, r *http.Request) {
	if err := s.cmdb.DeletePrefix(r.Context(), s.wsID(r), chi.URLParam(r, "id")); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "DELETE_FAILED", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"status": "deleted"})
}

func (s *Server) handleListAddresses(w http.ResponseWriter, r *http.Request) {
	items, err := s.cmdb.ListAddresses(r.Context(), s.wsID(r), r.URL.Query().Get("prefixId"))
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	httpx.WriteList(w, items, len(items))
}

func (s *Server) handleAllocateIP(w http.ResponseWriter, r *http.Request) {
	prefixID := chi.URLParam(r, "id")
	var req struct {
		IP          string `json:"ip"`
		Status      string `json:"status"`
		CIID        string `json:"ciId"`
		DNSName     string `json:"dnsName"`
		Owner       string `json:"owner"`
		Description string `json:"description"`
	}
	if err := httpx.DecodeStrictJSON(w, r, &req, 64<<10); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	addr, err := s.cmdb.AllocateIP(r.Context(), s.wsID(r), prefixID, req.IP, req.Status, req.CIID, req.DNSName, req.Owner, req.Description)
	if err != nil {
		httpx.WriteError(w, http.StatusConflict, "IP_ALLOC_FAILED", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, addr)
}

func (s *Server) handleReleaseIP(w http.ResponseWriter, r *http.Request) {
	if err := s.cmdb.ReleaseIP(r.Context(), s.wsID(r), chi.URLParam(r, "id")); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "DELETE_FAILED", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"status": "released"})
}

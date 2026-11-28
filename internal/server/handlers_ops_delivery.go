package server

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/kinstall/kinstall/internal/catalog"
	"github.com/kinstall/kinstall/internal/config"
	"github.com/kinstall/kinstall/internal/delivery"
	"github.com/kinstall/kinstall/internal/execplane"
	"github.com/kinstall/kinstall/pkg/platform/httpx"
)

// =============================================================================
// Multi-Env Config & KMS Secrets Center
// =============================================================================

func (s *Server) handleListNamespaces(w http.ResponseWriter, r *http.Request) {
	items, err := s.cfg.ListNamespaces(r.Context(), s.wsID(r), r.URL.Query().Get("env"))
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	httpx.WriteList(w, items, len(items))
}

func (s *Server) handleCreateNamespace(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Env         string `json:"env"`
		AppCode     string `json:"appCode"`
		Name        string `json:"name"`
		Format      string `json:"format"`
		Description string `json:"description"`
	}
	if err := httpx.DecodeStrictJSON(w, r, &req, 64<<10); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	ns, err := s.cfg.CreateNamespace(r.Context(), s.wsID(r), req.Env, req.AppCode, req.Name, req.Format, req.Description)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "CREATE_FAILED", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, ns)
}

func (s *Server) handleDeleteNamespace(w http.ResponseWriter, r *http.Request) {
	if err := s.cfg.DeleteNamespace(r.Context(), s.wsID(r), chi.URLParam(r, "id")); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "DELETE_FAILED", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"status": "deleted"})
}

func (s *Server) handleListConfigItems(w http.ResponseWriter, r *http.Request) {
	items, err := s.cfg.ListItems(r.Context(), s.wsID(r), chi.URLParam(r, "id"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "LIST_FAILED", err.Error())
		return
	}
	httpx.WriteList(w, items, len(items))
}

func (s *Server) handleUpsertConfigItem(w http.ResponseWriter, r *http.Request) {
	nsID := chi.URLParam(r, "id")
	var req struct {
		Key         string `json:"key"`
		Value       string `json:"value"`
		IsSecret    bool   `json:"isSecret"`
		ValueType   string `json:"valueType"`
		Description string `json:"description"`
	}
	if err := httpx.DecodeStrictJSON(w, r, &req, 256<<10); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	p := s.currentPrincipal(r)
	it, err := s.cfg.UpsertItem(r.Context(), s.wsID(r), nsID, p.Username, req.Key, req.Value, req.IsSecret, req.ValueType, req.Description)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "SAVE_FAILED", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, it)
}

func (s *Server) handleDeleteConfigItem(w http.ResponseWriter, r *http.Request) {
	if err := s.cfg.DeleteItem(r.Context(), s.wsID(r), chi.URLParam(r, "id"), chi.URLParam(r, "key")); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "DELETE_FAILED", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"status": "deleted"})
}

func (s *Server) handleRevealConfigSecret(w http.ResponseWriter, r *http.Request) {
	nsID := chi.URLParam(r, "id")
	var req struct {
		Key    string `json:"key"`
		Reason string `json:"reason"`
	}
	if err := httpx.DecodeStrictJSON(w, r, &req, 32<<10); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	p := s.currentPrincipal(r)
	pt, err := s.cfg.RevealSecret(r.Context(), s.wsID(r), nsID, req.Key, p.Username, p.Role, req.Reason, clientIP(r))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "REVEAL_FAILED", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"key": req.Key, "value": pt})
}

func (s *Server) handleDiffConfig(w http.ResponseWriter, r *http.Request) {
	diffs, err := s.cfg.DiffDraftVsRelease(r.Context(), s.wsID(r), chi.URLParam(r, "id"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "DIFF_FAILED", err.Error())
		return
	}
	httpx.WriteList(w, diffs, len(diffs))
}

func (s *Server) handlePublishConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Comment string `json:"comment"`
	}
	_ = httpx.DecodeStrictJSON(w, r, &req, 32<<10)
	p := s.currentPrincipal(r)
	rel, err := s.cfg.PublishRelease(r.Context(), s.wsID(r), chi.URLParam(r, "id"), p.Username, req.Comment)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "PUBLISH_FAILED", err.Error())
		return
	}
	_ = s.iam.RecordAudit(r.Context(), s.wsID(r), p.Username, p.Role, "config.release.publish", "cfg_namespace", rel.NamespaceID, "ok", fmt.Sprintf("发布不可变快照 v%d (etag=%s)", rel.Version, rel.ETag), clientIP(r))
	httpx.WriteJSON(w, http.StatusCreated, rel)
}

func (s *Server) handleListConfigReleases(w http.ResponseWriter, r *http.Request) {
	items, err := s.cfg.ListReleases(r.Context(), s.wsID(r), chi.URLParam(r, "id"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "LIST_FAILED", err.Error())
		return
	}
	httpx.WriteList(w, items, len(items))
}

func (s *Server) handleRollbackConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TargetVersion int `json:"targetVersion"`
	}
	if err := httpx.DecodeStrictJSON(w, r, &req, 32<<10); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	p := s.currentPrincipal(r)
	rel, err := s.cfg.RollbackRelease(r.Context(), s.wsID(r), chi.URLParam(r, "id"), req.TargetVersion, p.Username)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "ROLLBACK_FAILED", err.Error())
		return
	}
	_ = s.iam.RecordAudit(r.Context(), s.wsID(r), p.Username, p.Role, "config.release.rollback", "cfg_namespace", rel.NamespaceID, "ok", fmt.Sprintf("回滚至 v%d -> 生成新快照 v%d", req.TargetVersion, rel.Version), clientIP(r))
	httpx.WriteJSON(w, http.StatusOK, rel)
}

func (s *Server) handleCloneConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TargetEnv  string `json:"targetEnv"`
		TargetApp  string `json:"targetApp"`
		TargetName string `json:"targetName"`
	}
	if err := httpx.DecodeStrictJSON(w, r, &req, 32<<10); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	p := s.currentPrincipal(r)
	dst, blanked, err := s.cfg.CloneNamespace(r.Context(), s.wsID(r), chi.URLParam(r, "id"), req.TargetEnv, req.TargetApp, req.TargetName, p.Username)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "CLONE_FAILED", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"namespace": dst, "blankedSecrets": blanked})
}

func (s *Server) handlePreviewEffectiveParams(w http.ResponseWriter, r *http.Request) {
	var req struct {
		GlobalParams  map[string]string `json:"globalParams"`
		ServiceParams map[string]string `json:"serviceParams"`
		SpecParams    map[string]string `json:"specParams"`
		CPUCores      float64           `json:"cpuCores"`
		MemGB         float64           `json:"memGb"`
		Replicas      int               `json:"replicas"`
	}
	if err := httpx.DecodeStrictJSON(w, r, &req, 128<<10); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	merged, trace := config.ResolveEffectiveParams(req.GlobalParams, req.ServiceParams, req.SpecParams)
	spec := config.ComputeK8sSpec(req.CPUCores, req.MemGB, req.Replicas)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"merged":  merged,
		"trace":   trace,
		"k8sSpec": spec,
	})
}

// =============================================================================
// CAS Artifacts & Native Go OCI Image Engine
// =============================================================================

func (s *Server) handleListArtifacts(w http.ResponseWriter, r *http.Request) {
	items, stats, err := s.catalog.ListArtifacts(r.Context(), s.wsID(r))
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"total": len(items),
		"stats": stats,
	})
}

func (s *Server) handleUploadArtifact(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, catalog.MaxArtifactBytes)
	if err := r.ParseMultipartForm(64 << 20); err != nil { // #nosec G120 -- bounded by MaxBytesReader
		httpx.WriteError(w, http.StatusBadRequest, "BAD_UPLOAD", "请通过 multipart 表单上传制品文件")
		return
	}
	file, hdr, err := r.FormFile("file")
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "MISSING_FILE", "缺少 file 字段")
		return
	}
	defer func() { _ = file.Close() }()

	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" && hdr != nil {
		name = hdr.Filename
	}
	p := s.currentPrincipal(r)
	art, err := s.catalog.UploadArtifact(r.Context(), s.wsID(r), p.Username, name, r.FormValue("version"), r.FormValue("kind"), r.FormValue("arch"), file)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "UPLOAD_FAILED", err.Error())
		return
	}
	_ = s.iam.RecordAudit(r.Context(), s.wsID(r), p.Username, p.Role, "catalog.artifact.upload", "artifact", art.ID, "ok", fmt.Sprintf("上传制品 %s@%s (%s, sha256=%s)", art.Name, art.Version, art.Arch, art.SHA256[:12]), clientIP(r))
	httpx.WriteJSON(w, http.StatusCreated, art)
}

func (s *Server) handleVerifyArtifact(w http.ResponseWriter, r *http.Request) {
	res, err := s.catalog.VerifyArtifact(r.Context(), s.wsID(r), chi.URLParam(r, "id"))
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, res)
}

func (s *Server) handleDownloadArtifact(w http.ResponseWriter, r *http.Request) {
	art, err := s.catalog.GetArtifact(r.Context(), s.wsID(r), chi.URLParam(r, "id"))
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "NOT_FOUND", "制品不存在")
		return
	}
	f, err := os.Open(art.StoragePath) // #nosec G703 -- StoragePath comes from SafeJoin CAS lookup in DB
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "BLOB_MISSING", "底层 CAS 文件缺失")
		return
	}
	defer func() { _ = f.Close() }()
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, art.Name))
	w.Header().Set("X-Artifact-SHA256", art.SHA256)
	http.ServeContent(w, r, art.Name, time.Now(), f)
}

func (s *Server) handleDeleteArtifact(w http.ResponseWriter, r *http.Request) {
	if err := s.catalog.DeleteArtifact(r.Context(), s.wsID(r), chi.URLParam(r, "id")); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "DELETE_FAILED", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"status": "deleted"})
}

func (s *Server) handlePushOCI(w http.ResponseWriter, r *http.Request) {
	artID := chi.URLParam(r, "id")
	var req struct {
		TargetRef    string `json:"targetRef"`
		ExpectedArch string `json:"expectedArch"`
		CredentialID string `json:"credentialId"`
		InsecureTLS  bool   `json:"insecureTls"`
	}
	if err := httpx.DecodeStrictJSON(w, r, &req, 64<<10); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	res, err := s.catalog.PushOCITar(r.Context(), s.wsID(r), artID, req.TargetRef, req.ExpectedArch, req.CredentialID, req.InsecureTLS)
	if err != nil {
		status := http.StatusBadRequest
		code := "OCI_PUSH_FAILED"
		if strings.Contains(err.Error(), "ARCH_MISMATCH") {
			status = http.StatusPreconditionFailed
			code = "ARCH_MISMATCH"
		}
		httpx.WriteError(w, status, code, err.Error())
		return
	}
	p := s.currentPrincipal(r)
	_ = s.iam.RecordAudit(r.Context(), s.wsID(r), p.Username, p.Role, "catalog.oci.push", "artifact", artID, "ok", "原生 Go OCI 推送至 "+req.TargetRef, clientIP(r))
	httpx.WriteJSON(w, http.StatusOK, res)
}

func (s *Server) handleExportBOM(w http.ResponseWriter, r *http.Request) {
	bom, err := s.catalog.ExportBOM(r.Context(), s.wsID(r))
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, bom)
}

// =============================================================================
// Declarative YAML Plugins & Real Delivery Orchestration
// =============================================================================

func (s *Server) handleListPlugins(w http.ResponseWriter, r *http.Request) {
	items, err := s.delivery.ListPlugins(r.Context(), s.wsID(r), r.URL.Query().Get("category"))
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	httpx.WriteList(w, items, len(items))
}

func (s *Server) handleUpsertPlugin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ManifestYAML string `json:"manifestYaml"`
	}
	if err := httpx.DecodeStrictJSON(w, r, &req, 512<<10); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	plg, err := s.delivery.UpsertPluginFromYAML(r.Context(), s.wsID(r), req.ManifestYAML)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PLUGIN_YAML", err.Error())
		return
	}
	p := s.currentPrincipal(r)
	_ = s.iam.RecordAudit(r.Context(), s.wsID(r), p.Username, p.Role, "delivery.plugin.upsert", "plugin", plg.Code, "ok", "保存 YAML 插件 "+plg.Name, clientIP(r))
	httpx.WriteJSON(w, http.StatusCreated, plg)
}

func (s *Server) handleDeletePlugin(w http.ResponseWriter, r *http.Request) {
	if err := s.delivery.DeletePlugin(r.Context(), s.wsID(r), chi.URLParam(r, "code")); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "DELETE_FAILED", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"status": "deleted"})
}

func (s *Server) handleListReleases(w http.ResponseWriter, r *http.Request) {
	items, err := s.delivery.ListReleases(r.Context(), s.wsID(r))
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	httpx.WriteList(w, items, len(items))
}

func (s *Server) handleCreateRelease(w http.ResponseWriter, r *http.Request) {
	var req delivery.CreateReleaseRequest
	if err := httpx.DecodeStrictJSON(w, r, &req, 512<<10); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	p := s.currentPrincipal(r)
	rel, err := s.delivery.CreateRelease(r.Context(), s.wsID(r), p.Username, req)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "CREATE_RELEASE_FAILED", err.Error())
		return
	}
	_ = s.iam.RecordAudit(r.Context(), s.wsID(r), p.Username, p.Role, "delivery.release.create", "release", rel.ID, "ok", fmt.Sprintf("创建发布单 %s (mode=%s, items=%d)", rel.Name, rel.Mode, len(rel.Items)), clientIP(r))
	httpx.WriteJSON(w, http.StatusCreated, rel)
}

func (s *Server) handleGetRelease(w http.ResponseWriter, r *http.Request) {
	rel, err := s.delivery.GetRelease(r.Context(), s.wsID(r), chi.URLParam(r, "id"))
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, rel)
}

func (s *Server) handleExecuteRelease(w http.ResponseWriter, r *http.Request) {
	p := s.currentPrincipal(r)
	rel, err := s.delivery.ExecuteRelease(r.Context(), s.wsID(r), chi.URLParam(r, "id"), p.Username)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "EXEC_FAILED", err.Error())
		return
	}
	_ = s.iam.RecordAudit(r.Context(), s.wsID(r), p.Username, p.Role, "delivery.release.execute", "release", rel.ID, "ok", "执行/恢复发布 -> "+rel.Status, clientIP(r))
	httpx.WriteJSON(w, http.StatusOK, rel)
}

func (s *Server) handleRollbackRelease(w http.ResponseWriter, r *http.Request) {
	p := s.currentPrincipal(r)
	rel, err := s.delivery.RollbackRelease(r.Context(), s.wsID(r), chi.URLParam(r, "id"), p.Username)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "ROLLBACK_FAILED", err.Error())
		return
	}
	_ = s.iam.RecordAudit(r.Context(), s.wsID(r), p.Username, p.Role, "delivery.release.rollback", "release", rel.ID, "ok", "回滚发布单", clientIP(r))
	httpx.WriteJSON(w, http.StatusOK, rel)
}

// =============================================================================
// Execution Plane: Agent Fleet, Whitelist Collect, TCP Probe & Batch Jobs
// =============================================================================

func (s *Server) handleCreateEnrollToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Label  string `json:"label"`
		Zone   string `json:"zone"`
		TTLMin int    `json:"ttlMin"`
	}
	_ = httpx.DecodeStrictJSON(w, r, &req, 32<<10)
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	serverURL := fmt.Sprintf("%s://%s", scheme, r.Host)
	p := s.currentPrincipal(r)
	tok, err := s.exec.Hub.CreateEnrollToken(r.Context(), s.wsID(r), p.Username, req.Label, req.Zone, serverURL, req.TTLMin)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "CREATE_FAILED", err.Error())
		return
	}
	_ = s.iam.RecordAudit(r.Context(), s.wsID(r), p.Username, p.Role, "ops.agent.enroll_token", "enroll_token", tok.ID, "ok", "签发一次性 Agent 纳管令牌 (zone="+tok.Zone+")", clientIP(r))
	httpx.WriteJSON(w, http.StatusCreated, tok)
}

func (s *Server) handleListEnrollTokens(w http.ResponseWriter, r *http.Request) {
	items, err := s.exec.Hub.ListEnrollTokens(r.Context(), s.wsID(r))
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	httpx.WriteList(w, items, len(items))
}

func (s *Server) handleListAgents(w http.ResponseWriter, r *http.Request) {
	items, err := s.exec.Hub.ListAgents(r.Context(), s.wsID(r))
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	httpx.WriteList(w, items, len(items))
}

func (s *Server) handleRevokeAgent(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "id")
	if err := s.exec.Hub.RevokeAgent(r.Context(), s.wsID(r), agentID); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "REVOKE_FAILED", err.Error())
		return
	}
	p := s.currentPrincipal(r)
	_ = s.iam.RecordAudit(r.Context(), s.wsID(r), p.Username, p.Role, "ops.agent.revoke", "agent", agentID, "ok", "吊销出站 Agent 凭据", clientIP(r))
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"status": "revoked"})
}

// Public Outbound Agent Protocol Endpoints (Zero Inbound Ports Needed!)
func (s *Server) handleAgentEnroll(w http.ResponseWriter, r *http.Request) {
	var req execplane.AgentEnrollRequest
	if err := httpx.DecodeStrictJSON(w, r, &req, 64<<10); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	resp, err := s.exec.Hub.EnrollAgent(r.Context(), req)
	if err != nil {
		httpx.WriteError(w, http.StatusUnauthorized, "ENROLL_FAILED", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, resp)
}

func (s *Server) authAgent(r *http.Request) (*execplane.Agent, error) {
	authz := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(authz) > 7 && strings.EqualFold(authz[:7], "Bearer ") {
		return s.exec.Hub.AuthenticateAgent(r.Context(), authz[7:])
	}
	return nil, fmt.Errorf("missing agent bearer token")
}

func (s *Server) handleAgentHeartbeat(w http.ResponseWriter, r *http.Request) {
	agt, err := s.authAgent(r)
	if err != nil {
		httpx.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", err.Error())
		return
	}
	var req execplane.AgentHeartbeatRequest
	if err := httpx.DecodeStrictJSON(w, r, &req, 32<<10); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	if err := s.exec.Hub.Heartbeat(r.Context(), agt, req); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "HEARTBEAT_FAILED", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) handleAgentPollTask(w http.ResponseWriter, r *http.Request) {
	agt, err := s.authAgent(r)
	if err != nil {
		httpx.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", err.Error())
		return
	}
	task, err := s.exec.Hub.PollTask(r.Context(), agt.ID, 8*time.Second)
	if err != nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if task == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, task)
}

func (s *Server) handleAgentTaskResult(w http.ResponseWriter, r *http.Request) {
	if _, err := s.authAgent(r); err != nil {
		httpx.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", err.Error())
		return
	}
	var res execplane.AgentTaskResult
	if err := httpx.DecodeStrictJSON(w, r, &res, 4<<20); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	res.TaskID = chi.URLParam(r, "taskId")
	_ = s.exec.Hub.SubmitTaskResult(res)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"status": "accepted"})
}

func (s *Server) handleCollectHostFacts(w http.ResponseWriter, r *http.Request) {
	ciID := chi.URLParam(r, "id")
	ci, err := s.exec.CollectHostFacts(r.Context(), s.wsID(r), ciID)
	if err != nil {
		httpx.WriteError(w, http.StatusBadGateway, "COLLECT_FAILED", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, ci)
}

func (s *Server) handleTCPProbe(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CIDR      string `json:"cidr"`
		Ports     []int  `json:"ports"`
		AgentID   string `json:"agentId"`
		TimeoutMs int    `json:"timeoutMs"`
	}
	if err := httpx.DecodeStrictJSON(w, r, &req, 32<<10); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	items, executor, err := s.exec.RunTCPProbe(r.Context(), s.wsID(r), req.CIDR, req.Ports, req.AgentID, req.TimeoutMs)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "PROBE_FAILED", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"cidr":     req.CIDR,
		"executor": executor,
		"items":    items,
		"total":    len(items),
	})
}

func (s *Server) handleListJobTemplates(w http.ResponseWriter, r *http.Request) {
	items := s.exec.ListJobTemplates()
	httpx.WriteList(w, items, len(items))
}

func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	items, err := s.exec.ListJobs(r.Context(), s.wsID(r))
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	httpx.WriteList(w, items, len(items))
}

func (s *Server) handleRunBatchJob(w http.ResponseWriter, r *http.Request) {
	var req execplane.RunJobRequest
	if err := httpx.DecodeStrictJSON(w, r, &req, 128<<10); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	p := s.currentPrincipal(r)
	job, err := s.exec.RunBatchJob(r.Context(), s.wsID(r), p.Username, req)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "JOB_FAILED", err.Error())
		return
	}
	_ = s.iam.RecordAudit(r.Context(), s.wsID(r), p.Username, p.Role, "ops.job.run", "job", job.ID, "ok", fmt.Sprintf("下发批量作业 %s (%d hosts)", job.Name, len(job.Runs)), clientIP(r))
	httpx.WriteJSON(w, http.StatusCreated, job)
}

// =============================================================================
// Live SSE Log Streaming & WebSocket SSH PTY
// =============================================================================

func (s *Server) handleStreamSSE(w http.ResponseWriter, r *http.Request) {
	topic := chi.URLParam(r, "topic")
	afterSeq, _ := strconv.ParseInt(r.Header.Get("Last-Event-ID"), 10, 64)
	if q := r.URL.Query().Get("after"); q != "" {
		afterSeq, _ = strconv.ParseInt(q, 10, 64)
	}
	sse, err := httpx.NewSSEWriter(w)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "SSE_UNSUPPORTED", err.Error())
		return
	}
	hist, ch, closed, cancel := s.broker.Subscribe(topic, afterSeq)
	defer cancel()

	for _, line := range hist {
		if err := sse.Send(strconv.FormatInt(line.Seq, 10), "log", line); err != nil {
			return
		}
		if line.Done {
			return
		}
	}
	if closed {
		return
	}
	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-keepalive.C:
			if err := sse.Ping(); err != nil {
				return
			}
		case line, ok := <-ch:
			if !ok {
				return
			}
			if err := sse.Send(strconv.FormatInt(line.Seq, 10), "log", line); err != nil {
				return
			}
			if line.Done {
				return
			}
		}
	}
}

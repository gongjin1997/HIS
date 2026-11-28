package httptransport

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/pkg/platform/httpx"
)

// ============================================================================
// Zero-Trust Outbound Nodes & Chunked Checkpoints
// ============================================================================

func (s *Server) handleListNodes(w http.ResponseWriter, r *http.Request) {
	_, scope := ctxScope(r)
	filter := scope
	if r.URL.Query().Get("allApps") == "true" {
		filter.AppID = ""
	}
	items, err := s.d.Nodes.ListNodes(r.Context(), filter)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.WriteList(w, items, nil, len(items))
}

func (s *Server) handleIssueEnrollToken(w http.ResponseWriter, r *http.Request) {
	tc, scope := ctxScope(r)
	var req struct {
		Labels     map[string]string `json:"labels"`
		TTLMinutes int               `json:"ttlMinutes"`
	}
	if r.ContentLength > 0 {
		if _, err := httpx.DecodeStrictJSON(w, r, &req, 64<<10); err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error(), nil)
			return
		}
	}
	ttl := domain.NodeEnrollTokenDefault
	if req.TTLMinutes > 0 {
		ttl = time.Duration(req.TTLMinutes) * time.Minute
	}
	tok, err := s.d.Nodes.IssueEnrollToken(r.Context(), tc, scope, req.Labels, ttl)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, tok)
}

func (s *Server) handleNodeEnroll(w http.ResponseWriter, r *http.Request) {
	var req struct {
		EnrollToken  string `json:"enrollToken"`
		Hostname     string `json:"hostname"`
		IPAddress    string `json:"ipAddress"`
		OSArch       string `json:"osArch"`
		AgentVersion string `json:"agentVersion"`
	}
	if _, err := httpx.DecodeStrictJSON(w, r, &req, 64<<10); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error(), nil)
		return
	}
	if req.IPAddress == "" {
		req.IPAddress = clientIP(r)
	}
	node, cred, err := s.d.Nodes.EnrollNode(r.Context(), req.EnrollToken, req.Hostname, req.IPAddress, req.OSArch, req.AgentVersion)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{
		"node":       node,
		"credential": cred,
	})
}

func extractNodeBearer(r *http.Request) string {
	h := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(h) > 7 && strings.EqualFold(h[:7], "Bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return strings.TrimSpace(r.Header.Get("X-Node-Credential"))
}

func (s *Server) handleNodeHeartbeat(w http.ResponseWriter, r *http.Request) {
	nodeID := chi.URLParam(r, "nodeId")
	cred := extractNodeBearer(r)
	node, err := s.d.Nodes.AuthenticateNode(r.Context(), nodeID, cred)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	var req struct {
		IPAddress    string `json:"ipAddress"`
		AgentVersion string `json:"agentVersion"`
	}
	if r.ContentLength > 0 {
		_, _ = httpx.DecodeStrictJSON(w, r, &req, 32<<10)
	}
	updated, err := s.d.Nodes.Heartbeat(r.Context(), node, req.IPAddress, req.AgentVersion)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, updated)
}

func (s *Server) handleRotateNodeCred(w http.ResponseWriter, r *http.Request) {
	tc, scope := ctxScope(r)
	nodeID := chi.URLParam(r, "nodeId")
	node, newCred, err := s.d.Nodes.RotateCredential(r.Context(), tc, scope, nodeID, domain.NodeCredGraceWindow)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"node":          node,
		"newCredential": newCred,
	})
}

func (s *Server) handleIsolateNode(w http.ResponseWriter, r *http.Request) {
	tc, scope := ctxScope(r)
	n, err := s.d.Nodes.SetStatus(r.Context(), tc, scope, chi.URLParam(r, "nodeId"), domain.NodeIsolated)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, n)
}

func (s *Server) handleUnisolateNode(w http.ResponseWriter, r *http.Request) {
	tc, scope := ctxScope(r)
	n, err := s.d.Nodes.SetStatus(r.Context(), tc, scope, chi.URLParam(r, "nodeId"), domain.NodeOnline)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, n)
}

func (s *Server) handleRevokeNode(w http.ResponseWriter, r *http.Request) {
	tc, scope := ctxScope(r)
	n, err := s.d.Nodes.SetStatus(r.Context(), tc, scope, chi.URLParam(r, "nodeId"), domain.NodeRevoked)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, n)
}

func (s *Server) handleCreateDistribution(w http.ResponseWriter, r *http.Request) {
	_, scope := ctxScope(r)
	var req struct {
		ArtifactFileID string `json:"artifactFileId"`
		TargetNodeID   string `json:"targetNodeId"`
		SHA256         string `json:"sha256"`
		TotalBytes     int64  `json:"totalBytes"`
	}
	if _, err := httpx.DecodeStrictJSON(w, r, &req, 64<<10); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error(), nil)
		return
	}
	d, err := s.d.Nodes.CreateDistribution(r.Context(), scope, req.ArtifactFileID, req.TargetNodeID, req.SHA256, req.TotalBytes)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, d)
}

func (s *Server) handleNodeChunkAck(w http.ResponseWriter, r *http.Request) {
	nodeID := chi.URLParam(r, "nodeId")
	distID := chi.URLParam(r, "distId")
	node, err := s.d.Nodes.AuthenticateNode(r.Context(), nodeID, extractNodeBearer(r))
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	var req struct {
		ChunkIndex  int    `json:"chunkIndex"`
		ChunkOffset int64  `json:"chunkOffset"`
		ChunkBytes  int64  `json:"chunkBytes"`
		ChunkSHA256 string `json:"chunkSha256"`
	}
	if _, err := httpx.DecodeStrictJSON(w, r, &req, 64<<10); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error(), nil)
		return
	}
	scope := domain.Scope{TenantID: node.TenantID, AppID: node.AppID}
	d, err := s.d.Nodes.AckDistributionChunk(r.Context(), scope, distID, req.ChunkIndex, req.ChunkOffset, req.ChunkBytes, req.ChunkSHA256)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, d)
}

// ============================================================================
// Dynamic Gateway & Artifact Integrity (G01-G05)
// ============================================================================

func (s *Server) handleListRouters(w http.ResponseWriter, r *http.Request) {
	_, scope := ctxScope(r)
	items, routeVer, err := s.d.Gateway.ListRouters(r.Context(), scope)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.SetVersionETag(w, routeVer)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"items":        items,
		"routeVersion": routeVer,
		"total":        len(items),
	})
}

func (s *Server) handleCreateRouter(w http.ResponseWriter, r *http.Request) {
	tc, scope := ctxScope(r)
	var req struct {
		Name         string `json:"name"`
		ListenHost   string `json:"listenHost"`
		RateLimitQPS int    `json:"rateLimitQps"`
		Burst        int    `json:"burst"`
	}
	if _, err := httpx.DecodeStrictJSON(w, r, &req, 64<<10); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error(), nil)
		return
	}
	rt, err := s.d.Gateway.CreateRouter(r.Context(), tc, scope, req.Name, req.ListenHost, req.RateLimitQPS, req.Burst)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, rt)
}

func (s *Server) handleListRoutes(w http.ResponseWriter, r *http.Request) {
	_, scope := ctxScope(r)
	items, routeVer, err := s.d.Gateway.ListRoutes(r.Context(), scope, r.URL.Query().Get("routerId"))
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.SetVersionETag(w, routeVer)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"items":        items,
		"routeVersion": routeVer,
		"total":        len(items),
	})
}

func (s *Server) handleCreateRoute(w http.ResponseWriter, r *http.Request) {
	tc, scope := ctxScope(r)
	var req struct {
		RouterID     string `json:"routerId"`
		Name         string `json:"name"`
		PathPrefix   string `json:"pathPrefix"`
		UpstreamURL  string `json:"upstreamUrl"`
		IsDefault    bool   `json:"isDefault"`
		StripPrefix  bool   `json:"stripPrefix"`
		TimeoutMs    int    `json:"timeoutMs"`
		RateLimitQPS int    `json:"rateLimitQps"`
	}
	if _, err := httpx.DecodeStrictJSON(w, r, &req, 64<<10); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error(), nil)
		return
	}
	rt, err := s.d.Gateway.CreateRoute(r.Context(), tc, scope, req.RouterID, req.Name, req.PathPrefix, req.UpstreamURL, req.IsDefault, req.StripPrefix, req.TimeoutMs, req.RateLimitQPS)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.SetVersionETag(w, rt.RouteVersion)
	httpx.WriteJSON(w, http.StatusCreated, rt)
}

func (s *Server) handleSwitchDefaultRoute(w http.ResponseWriter, r *http.Request) {
	tc, scope := ctxScope(r)
	ver, err := s.d.Gateway.SwitchDefaultRoute(r.Context(), tc, scope, chi.URLParam(r, "routeId"))
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.SetVersionETag(w, ver)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"routeVersion": ver})
}

func (s *Server) handleListGatewayTelemetry(w http.ResponseWriter, r *http.Request) {
	_, scope := ctxScope(r)
	items, err := s.d.Gateway.ListTelemetry(r.Context(), scope)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.WriteList(w, items, nil, len(items))
}

func (s *Server) handleListArtifactFiles(w http.ResponseWriter, r *http.Request) {
	_, scope := ctxScope(r)
	items, err := s.d.Gateway.ListArtifactFiles(r.Context(), scope)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.WriteList(w, items, nil, len(items))
}

func (s *Server) handleCreateArtifactFile(w http.ResponseWriter, r *http.Request) {
	tc, scope := ctxScope(r)
	var req struct {
		Filename    string `json:"filename"`
		ContentType string `json:"contentType"`
		SHA256      string `json:"sha256"`
		StoragePath string `json:"storagePath"`
		SizeBytes   int64  `json:"sizeBytes"`
	}
	if _, err := httpx.DecodeStrictJSON(w, r, &req, 64<<10); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error(), nil)
		return
	}
	f, err := s.d.Gateway.CreateArtifactFile(r.Context(), tc, scope, req.Filename, req.ContentType, req.SHA256, req.StoragePath, req.SizeBytes)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, f)
}

func (s *Server) handleVerifyArtifactFile(w http.ResponseWriter, r *http.Request) {
	_, scope := ctxScope(r)
	var req struct {
		ExpectedSHA256 string `json:"expectedSha256"`
	}
	if r.ContentLength > 0 {
		_, _ = httpx.DecodeStrictJSON(w, r, &req, 32<<10)
	}
	f, err := s.d.Gateway.VerifyArtifactFile(r.Context(), scope, chi.URLParam(r, "fileId"), req.ExpectedSHA256)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, f)
}

func (s *Server) handleDeleteArtifactFile(w http.ResponseWriter, r *http.Request) {
	_, scope := ctxScope(r)
	if err := s.d.Gateway.DeleteArtifactFile(r.Context(), scope, chi.URLParam(r, "fileId")); err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (s *Server) handlePromoteArtifactRelease(w http.ResponseWriter, r *http.Request) {
	tc, scope := ctxScope(r)
	var req struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		FileID  string `json:"fileId"`
		Notes   string `json:"notes"`
	}
	if _, err := httpx.DecodeStrictJSON(w, r, &req, 64<<10); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error(), nil)
		return
	}
	rel, err := s.d.Gateway.PromoteArtifactRelease(r.Context(), tc, scope, req.Name, req.Version, req.FileID, req.Notes)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, rel)
}

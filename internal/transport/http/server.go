package httptransport

import (
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/internal/usecase/assets"
	"github.com/kinstall/kinstall/internal/usecase/config"
	"github.com/kinstall/kinstall/internal/usecase/deploy"
	"github.com/kinstall/kinstall/internal/usecase/gateway"
	"github.com/kinstall/kinstall/internal/usecase/identity"
	"github.com/kinstall/kinstall/internal/usecase/nodes"
	"github.com/kinstall/kinstall/pkg/platform/auth"
	"github.com/kinstall/kinstall/pkg/platform/httpx"
	"github.com/kinstall/kinstall/pkg/platform/probes"
)

type Dependencies struct {
	Identity *identity.Service
	Assets   *assets.Service
	Config   *config.Service
	Deploy   *deploy.Service
	Nodes    *nodes.Service
	Gateway  *gateway.Service
	Tokens   *auth.TokenManager
	Probes   *probes.Registry
	GWSecret []byte
	StaticFS fs.FS
	Logger   *slog.Logger
}

type Server struct {
	d Dependencies
}

func NewRouter(deps Dependencies) http.Handler {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	s := &Server{d: deps}
	r := chi.NewRouter()
	r.Use(httpx.TraceMiddleware)
	r.Use(httpx.SecurityHeaders)
	r.Use(middleware.Recoverer)

	// Authentic Kubernetes & LiveOps Health Probes
	if s.d.Probes != nil {
		r.Get("/startupz", s.d.Probes.HandleStartupz)
		r.Get("/livez", s.d.Probes.HandleLivez)
		r.Get("/readyz", s.d.Probes.HandleReadyz)
		r.Get("/healthy", s.d.Probes.HandleLegacyHealthy)
	}

	// Public Authentication & Zero-Trust Outbound Node Enrollment / Heartbeat
	r.Post("/api/v1/iam/auth/login", s.handleLogin)
	r.Post("/api/v1/iam/auth/logout", s.handleLogout)
	r.Post("/api/v1/nodes/enroll", s.handleNodeEnroll)
	r.Post("/api/v1/nodes/{nodeId}/heartbeat", s.handleNodeHeartbeat)
	r.Post("/api/v1/nodes/{nodeId}/distributions/{distId}/chunks", s.handleNodeChunkAck)

	// Authenticated LiveOps v1 Control-Plane APIs (Default-Deny RBAC)
	r.Group(func(pr chi.Router) {
		pr.Use(auth.Authenticate(s.d.Tokens, s.d.GWSecret, s.d.Identity.ResolveScope, s.d.Identity.ResolveAppKey))

		pr.Get("/api/v1/iam/auth/me", s.handleMe)
		pr.Get("/api/v1/platform/overview", s.handlePlatformOverview)

		// IAM & Multi-Tenant Governance
		pr.With(auth.RequirePerm(auth.PermIAMRead)).Get("/api/v1/iam/tenants", s.handleListTenants)
		pr.With(auth.RequirePerm(auth.PermPlatformAdmin)).Post("/api/v1/iam/tenants", s.handleCreateTenant)
		pr.With(auth.RequirePerm(auth.PermIAMRead)).Get("/api/v1/iam/apps", s.handleListApps)
		pr.With(auth.RequirePerm(auth.PermIAMAppAdmin)).Post("/api/v1/iam/apps", s.handleCreateApp)
		pr.With(auth.RequirePerm(auth.PermIAMRead)).Get("/api/v1/iam/apps/keys", s.handleListAppKeys)
		pr.With(auth.RequirePerm(auth.PermIAMAppAdmin)).Post("/api/v1/iam/apps/keys/rotate", s.handleRotateAppKey)
		pr.With(auth.RequirePerm(auth.PermIAMAppAdmin)).Delete("/api/v1/iam/apps/keys/{keyId}", s.handleRevokeAppKey)
		pr.With(auth.RequirePerm(auth.PermIAMRead)).Get("/api/v1/iam/projects", s.handleListProjects)
		pr.With(auth.RequirePerm(auth.PermIAMWrite)).Post("/api/v1/iam/projects", s.handleCreateProject)
		pr.With(auth.RequirePerm(auth.PermIAMRead)).Get("/api/v1/iam/projects/{projectId}/roles", s.handleListProjectRoles)
		pr.With(auth.RequirePerm(auth.PermIAMWrite)).Put("/api/v1/iam/projects/{projectId}/roles/{roleId}/permissions", s.handleReplaceProjectRolePerms)
		pr.With(auth.RequirePerm(auth.PermIAMRead)).Get("/api/v1/iam/users", s.handleListUsers)
		pr.With(auth.RequirePerm(auth.PermIAMWrite)).Post("/api/v1/iam/users", s.handleCreateUser)
		pr.With(auth.RequirePerm(auth.PermIAMRead)).Get("/api/v1/iam/roles", s.handleListRoles)
		pr.With(auth.RequirePerm(auth.PermIAMRead)).Get("/api/v1/iam/route-policies", s.handleListRoutePolicies)
		pr.With(auth.RequirePerm(auth.PermIAMWrite)).Post("/api/v1/iam/route-policies", s.handleUpsertRoutePolicy)
		pr.With(auth.RequirePerm(auth.PermIAMAuditRead)).Get("/api/v1/iam/audit/logs", s.handleListAuditLogs)
		pr.With(auth.RequirePerm(auth.PermIAMAuditVerify)).Post("/api/v1/iam/audit/verify", s.handleVerifyAuditChain)

		// Assets — CMDB Meta-Models, Instances, Dual Topology & Native Excelize
		pr.With(auth.RequirePerm(auth.PermAssetsRead)).Get("/api/v1/assets/models", s.handleListAssetModels)
		pr.With(auth.RequirePerm(auth.PermAssetsWrite)).Post("/api/v1/assets/models", s.handleCreateAssetModel)
		pr.With(auth.RequirePerm(auth.PermAssetsRead)).Get("/api/v1/assets/relation-types", s.handleListRelationTypes)
		pr.With(auth.RequirePerm(auth.PermAssetsWrite)).Delete("/api/v1/assets/relation-types/{relTypeId}", s.handleDeleteRelationType)
		pr.With(auth.RequirePerm(auth.PermAssetsRead)).Get("/api/v1/assets/instances", s.handleListAssetInstances)
		pr.With(auth.RequirePerm(auth.PermAssetsWrite)).Post("/api/v1/assets/instances", s.handleCreateAssetInstance)
		pr.With(auth.RequirePerm(auth.PermAssetsRead)).Get("/api/v1/assets/instances/{instanceId}", s.handleGetAssetInstance)
		pr.With(auth.RequirePerm(auth.PermAssetsWrite)).Put("/api/v1/assets/instances/{instanceId}", s.handleUpdateAssetInstance)
		pr.With(auth.RequirePerm(auth.PermAssetsWrite)).Delete("/api/v1/assets/instances/{instanceId}", s.handleCascadeDeleteInstance)
		pr.With(auth.RequirePerm(auth.PermAssetsRead)).Get("/api/v1/assets/topology", s.handleGetTopology)
		pr.With(auth.RequirePerm(auth.PermAssetsWrite)).Post("/api/v1/assets/edges", s.handleCreateAssetEdge)
		pr.With(auth.RequirePerm(auth.PermAssetsWrite)).Delete("/api/v1/assets/edges/{edgeId}", s.handleDeleteAssetEdge)
		pr.With(auth.RequirePerm(auth.PermAssetsRead)).Get("/api/v1/assets/models/{modelId}/export.xlsx", s.handleExportXLSX)
		pr.With(auth.RequirePerm(auth.PermAssetsRead)).Get("/api/v1/assets/models/{modelId}/template.xlsx", s.handleExportTemplateXLSX)
		pr.With(auth.RequirePerm(auth.PermAssetsWrite)).Post("/api/v1/assets/models/{modelId}/import.xlsx", s.handleImportXLSX)

		// Config & Environments — KMS Secrets, Optimistic Locking & Conflict ViewCheck
		pr.With(auth.RequirePerm(auth.PermConfigRead)).Get("/api/v1/config/configurations", s.handleListConfigs)
		pr.With(auth.RequirePerm(auth.PermConfigWrite)).Post("/api/v1/config/configurations", s.handleCreateConfig)
		pr.With(auth.RequirePerm(auth.PermConfigRead)).Get("/api/v1/config/configurations/{cfgId}", s.handleGetConfig)
		pr.With(auth.RequirePerm(auth.PermConfigWrite)).Put("/api/v1/config/configurations/{cfgId}", s.handleUpdateConfig)
		pr.With(auth.RequirePerm(auth.PermConfigWrite)).Delete("/api/v1/config/configurations/{cfgId}", s.handleDeleteConfig)
		pr.With(auth.RequirePerm(auth.PermConfigRead)).Get("/api/v1/config/configurations/{cfgId}/history", s.handleListConfigHistory)
		pr.With(auth.RequirePerm(auth.PermConfigReveal)).Post("/api/v1/config/configurations/{cfgId}/reveal", s.handleRevealConfigSecret)
		pr.With(auth.RequirePerm(auth.PermConfigRead)).Get("/api/v1/config/environments", s.handleListEnvs)
		pr.With(auth.RequirePerm(auth.PermConfigWrite)).Post("/api/v1/config/environments", s.handleCreateEnv)
		pr.With(auth.RequirePerm(auth.PermConfigWrite)).Put("/api/v1/config/environments/{envId}/members", s.handleUpdateEnvMembers)
		pr.With(auth.RequirePerm(auth.PermConfigRead)).Get("/api/v1/config/environments/{envId}/viewcheck", s.handleViewCheckEnv)

		// Deploy Jobs & Strict SSE Timeline (D01-D19, U01-U09)
		pr.With(auth.RequirePerm(auth.PermDeployJobRead)).Get("/api/v1/deploy/jobs", s.handleListDeployJobs)
		pr.With(auth.RequirePerm(auth.PermDeployJobSubmit)).Post("/api/v1/deploy/jobs", s.handleSubmitDeployJob)
		pr.With(auth.RequirePerm(auth.PermDeployJobRead)).Get("/api/v1/deploy/jobs/{jobId}", s.handleGetDeployJob)
		pr.With(auth.RequirePerm(auth.PermDeployJobCancel)).Post("/api/v1/deploy/jobs/{jobId}/cancel", s.handleCancelDeployJob)
		pr.With(auth.RequirePerm(auth.PermDeployJobObserve)).Get("/api/v1/deploy/jobs/{jobId}/events", s.handleDeployJobSSE)

		// Zero-Trust Nodes Fleet Admin
		pr.With(auth.RequirePerm(auth.PermNodesRead)).Get("/api/v1/nodes", s.handleListNodes)
		pr.With(auth.RequirePerm(auth.PermNodesAdmin)).Post("/api/v1/nodes/enroll-tokens", s.handleIssueEnrollToken)
		pr.With(auth.RequirePerm(auth.PermNodesAdmin)).Post("/api/v1/nodes/{nodeId}/rotate-credential", s.handleRotateNodeCred)
		pr.With(auth.RequirePerm(auth.PermNodesAdmin)).Post("/api/v1/nodes/{nodeId}/isolate", s.handleIsolateNode)
		pr.With(auth.RequirePerm(auth.PermNodesAdmin)).Post("/api/v1/nodes/{nodeId}/unisolate", s.handleUnisolateNode)
		pr.With(auth.RequirePerm(auth.PermNodesAdmin)).Post("/api/v1/nodes/{nodeId}/revoke", s.handleRevokeNode)
		pr.With(auth.RequirePerm(auth.PermNodesAdmin)).Post("/api/v1/nodes/distributions", s.handleCreateDistribution)

		// Dynamic Gateway & Artifact Integrity (G01-G05)
		pr.With(auth.RequirePerm(auth.PermGatewayRead)).Get("/api/v1/gateway/routers", s.handleListRouters)
		pr.With(auth.RequirePerm(auth.PermGatewayWrite)).Post("/api/v1/gateway/routers", s.handleCreateRouter)
		pr.With(auth.RequirePerm(auth.PermGatewayRead)).Get("/api/v1/gateway/routes", s.handleListRoutes)
		pr.With(auth.RequirePerm(auth.PermGatewayWrite)).Post("/api/v1/gateway/routes", s.handleCreateRoute)
		pr.With(auth.RequirePerm(auth.PermGatewayWrite)).Post("/api/v1/gateway/routes/{routeId}/set-default", s.handleSwitchDefaultRoute)
		pr.With(auth.RequirePerm(auth.PermGatewayMonitor)).Get("/api/v1/gateway/telemetry", s.handleListGatewayTelemetry)
		pr.With(auth.RequirePerm(auth.PermGatewayRead)).Get("/api/v1/artifacts/files", s.handleListArtifactFiles)
		pr.With(auth.RequirePerm(auth.PermGatewayWrite)).Post("/api/v1/artifacts/files", s.handleCreateArtifactFile)
		pr.With(auth.RequirePerm(auth.PermGatewayWrite)).Post("/api/v1/artifacts/files/{fileId}/verify", s.handleVerifyArtifactFile)
		pr.With(auth.RequirePerm(auth.PermGatewayWrite)).Delete("/api/v1/artifacts/files/{fileId}", s.handleDeleteArtifactFile)
		pr.With(auth.RequirePerm(auth.PermGatewayWrite)).Post("/api/v1/artifacts/releases", s.handlePromoteArtifactRelease)
	})

	// Embedded KubeSphere-Inspired Vue 3 SPA Static Assets
	if s.d.StaticFS != nil {
		r.NotFound(s.serveSPA)
	}
	return r
}

func (s *Server) serveSPA(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		httpx.WriteError(w, http.StatusNotFound, "API_NOT_FOUND", "接口路径不存在", nil)
		return
	}
	clean := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if clean == "" {
		clean = "index.html"
	}
	f, err := s.d.StaticFS.Open(clean)
	if err != nil {
		clean = "index.html"
		f, err = s.d.StaticFS.Open(clean)
		if err != nil {
			httpx.WriteError(w, http.StatusNotFound, "UI_NOT_BUILT", "前端静态资源未嵌入", nil)
			return
		}
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		_ = f.Close()
		clean = "index.html"
		f, err = s.d.StaticFS.Open(clean)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer func() { _ = f.Close() }()
	}
	data, err := io.ReadAll(f)
	if err != nil {
		http.Error(w, "read asset failed", http.StatusInternalServerError)
		return
	}
	if ct := mime.TypeByExtension(path.Ext(clean)); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	if clean == "index.html" {
		w.Header().Set("Cache-Control", "no-store")
	} else if strings.HasPrefix(clean, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func ctxScope(r *http.Request) (auth.TrustedContext, domain.Scope) {
	tc, _ := auth.FromContext(r.Context())
	return tc, domain.Scope{
		TenantID:  tc.TenantID,
		AppID:     tc.AppID,
		ProjectID: tc.ProjectID,
	}
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func writeDomainErr(w http.ResponseWriter, err error) {
	if err == nil {
		return
	}
	var ce *domain.CodedError
	code := "INTERNAL_ERROR"
	msg := err.Error()
	var details map[string]any
	if errors.As(err, &ce) {
		code = ce.Code
		msg = ce.Message
		if m, ok := ce.Details.(map[string]any); ok {
			details = m
		} else if ce.Details != nil {
			details = map[string]any{"detail": ce.Details}
		}
	}

	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, domain.ErrUnauthorized):
		status = http.StatusUnauthorized
	case errors.Is(err, domain.ErrForbidden):
		status = http.StatusForbidden
	case errors.Is(err, domain.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, domain.ErrValidation):
		status = http.StatusBadRequest
	case errors.Is(err, domain.ErrPreconditionRequired):
		status = http.StatusPreconditionRequired
	case errors.Is(err, domain.ErrVersionMismatch):
		status = http.StatusPreconditionFailed
	case errors.Is(err, domain.ErrRateLimited):
		status = http.StatusTooManyRequests
	case errors.Is(err, domain.ErrCursorReset):
		status = http.StatusGone
	case errors.Is(err, domain.ErrAlreadyExists),
		errors.Is(err, domain.ErrIdempotencyConflict),
		errors.Is(err, domain.ErrStateConflict),
		errors.Is(err, domain.ErrReferenceInUse),
		errors.Is(err, domain.ErrSnapshotRequired),
		errors.Is(err, domain.ErrReceiptConflict),
		errors.Is(err, domain.ErrStaleLease):
		status = http.StatusConflict
	}
	httpx.WriteError(w, status, code, msg, details)
}

func (s *Server) handlePlatformOverview(w http.ResponseWriter, r *http.Request) {
	tc, scope := ctxScope(r)
	tenants, _ := s.d.Identity.ListTenants(r.Context())
	apps, _ := s.d.Identity.ListApps(r.Context(), scope.TenantID)
	projects, _ := s.d.Identity.ListProjects(r.Context(), scope)
	users, _ := s.d.Identity.ListUsers(r.Context(), scope.TenantID)
	allNodes, _ := s.d.Nodes.ListNodes(r.Context(), domain.Scope{TenantID: scope.TenantID})
	models, _ := s.d.Assets.ListModels(r.Context(), scope)
	instances, _ := s.d.Assets.ListInstances(r.Context(), scope, "", "")
	configs, _ := s.d.Config.ListConfigurations(r.Context(), scope, "")
	envs, _ := s.d.Config.ListEnvironments(r.Context(), scope, "")
	jobs, _ := s.d.Deploy.ListJobs(r.Context(), scope, "", 20)
	routers, routeVer, _ := s.d.Gateway.ListRouters(r.Context(), scope)
	routes, _, _ := s.d.Gateway.ListRoutes(r.Context(), scope, "")
	auditRes, _ := s.d.Identity.VerifyAuditChain(r.Context())

	onlineNodes := 0
	for _, n := range allNodes {
		if n.Status == domain.NodeOnline {
			onlineNodes++
		}
	}
	var probeSnap map[string]any
	if s.d.Probes != nil {
		probeSnap = s.d.Probes.Snapshot(r.Context())
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"context": tc,
		"probes":  probeSnap,
		"counts": map[string]any{
			"tenants":     len(tenants),
			"apps":        len(apps),
			"projects":    len(projects),
			"users":       len(users),
			"nodes":       len(allNodes),
			"nodesOnline": onlineNodes,
			"cmdbModels":  len(models),
			"cmdbAssets":  len(instances),
			"configs":     len(configs),
			"envs":        len(envs),
			"jobs":        len(jobs),
			"routers":     len(routers),
			"routes":      len(routes),
		},
		"routeVersion": routeVer,
		"auditChain":   auditRes,
		"recentJobs":   jobs,
		"serverTime":   time.Now().UTC().Format(time.RFC3339),
	})
}

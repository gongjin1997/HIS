package server

import (
	"context"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/kinstall/kinstall/internal/catalog"
	"github.com/kinstall/kinstall/internal/cmdb"
	"github.com/kinstall/kinstall/internal/config"
	"github.com/kinstall/kinstall/internal/delivery"
	"github.com/kinstall/kinstall/internal/execplane"
	"github.com/kinstall/kinstall/internal/iam"
	"github.com/kinstall/kinstall/pkg/platform/auth"
	"github.com/kinstall/kinstall/pkg/platform/eventbus"
	"github.com/kinstall/kinstall/pkg/platform/httpx"
	"github.com/kinstall/kinstall/web"
	"golang.org/x/net/websocket"
)

type Config struct {
	Version     string
	DefaultWSID string
	Logger      *slog.Logger
	AuthMgr     *auth.Manager
	Broker      *eventbus.LogBroker
	IAM         *iam.Service
	CMDB        *cmdb.Service
	ConfigSvc   *config.Service
	Catalog     *catalog.Service
	Exec        *execplane.Service
	Delivery    *delivery.Service
	PingDB      func(ctx context.Context) error
}

type Server struct {
	version     string
	defaultWSID string
	logger      *slog.Logger
	authMgr     *auth.Manager
	broker      *eventbus.LogBroker
	iam         *iam.Service
	cmdb        *cmdb.Service
	cfg         *config.Service
	catalog     *catalog.Service
	exec        *execplane.Service
	delivery    *delivery.Service
	pingDB      func(ctx context.Context) error
	router      chi.Router
}

func New(cfg Config) *Server {
	s := &Server{
		version:     cfg.Version,
		defaultWSID: cfg.DefaultWSID,
		logger:      cfg.Logger,
		authMgr:     cfg.AuthMgr,
		broker:      cfg.Broker,
		iam:         cfg.IAM,
		cmdb:        cfg.CMDB,
		cfg:         cfg.ConfigSvc,
		catalog:     cfg.Catalog,
		exec:        cfg.Exec,
		delivery:    cfg.Delivery,
		pingDB:      cfg.PingDB,
	}
	s.router = s.buildRouter()
	return s
}

func (s *Server) Handler() http.Handler {
	return s.router
}

func (s *Server) buildRouter() chi.Router {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(httpx.TraceMiddleware)
	r.Use(httpx.SecurityHeaders)

	// Health probe (public)
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		dbOK := s.pingDB == nil || s.pingDB(r.Context()) == nil
		status := http.StatusOK
		if !dbOK {
			status = http.StatusServiceUnavailable
		}
		httpx.WriteJSON(w, status, map[string]any{
			"status":  "ok",
			"db":      dbOK,
			"version": s.version,
			"time":    time.Now().UTC().Format(time.RFC3339),
		})
	})

	// Public auth & outbound zero-trust agent protocol routes
	r.Route("/api/v1", func(api chi.Router) {
		api.Post("/auth/login", s.handleLogin)

		// Outbound Go Agent protocol (authenticated via one-time enroll token or agent bearer token)
		api.Post("/agent/enroll", s.handleAgentEnroll)
		api.Post("/agent/heartbeat", s.handleAgentHeartbeat)
		api.Get("/agent/tasks/poll", s.handleAgentPollTask)
		api.Post("/agent/tasks/{taskId}/result", s.handleAgentTaskResult)

		// Protected control-plane routes (Default-Deny RBAC)
		api.Group(func(pr chi.Router) {
			pr.Use(auth.Authenticate(s.authMgr, s.iam.LookupPAT))

			// Session & Workspace
			pr.Get("/me", s.handleMe)
			pr.Get("/workspaces", s.handleListWorkspaces)
			pr.With(auth.RequirePerm(auth.PermIAMWrite)).Post("/workspaces", s.handleCreateWorkspace)

			// Dashboard Panorama
			pr.With(auth.RequirePerm(auth.PermCMDBRead)).Get("/overview", s.handleOverviewStats)

			// Live SSE Log Stream
			pr.With(auth.RequirePerm(auth.PermOpsRead)).Get("/stream/{topic}", s.handleStreamSSE)

			// WebSocket Interactive SSH PTY on unified m_host CI
			pr.With(auth.RequirePerm(auth.PermOpsExecute)).Get("/hosts/{id}/terminal", s.handleHostTerminalWS)

			// IAM, Users, PATs, Credentials Vault & Tamper-Evident Audit Chain
			pr.With(auth.RequirePerm(auth.PermIAMRead)).Get("/iam/users", s.handleListUsers)
			pr.With(auth.RequirePerm(auth.PermIAMWrite)).Post("/iam/users", s.handleCreateUser)
			pr.With(auth.RequirePerm(auth.PermIAMWrite)).Put("/iam/users/{id}", s.handleUpdateUserRole)
			pr.With(auth.RequirePerm(auth.PermIAMRead)).Get("/iam/tokens", s.handleListAPITokens)
			pr.With(auth.RequirePerm(auth.PermIAMWrite)).Post("/iam/tokens", s.handleCreateAPIToken)
			pr.With(auth.RequirePerm(auth.PermIAMWrite)).Post("/iam/tokens/{id}/revoke", s.handleRevokeAPIToken)
			pr.With(auth.RequirePerm(auth.PermIAMRead)).Get("/iam/credentials", s.handleListCredentials)
			pr.With(auth.RequirePerm(auth.PermIAMWrite)).Post("/iam/credentials", s.handleCreateCredential)
			pr.With(auth.RequirePerm(auth.PermIAMWrite)).Delete("/iam/credentials/{id}", s.handleDeleteCredential)
			pr.With(auth.RequirePerm(auth.PermAuditRead)).Get("/audit/logs", s.handleListAuditLogs)
			pr.With(auth.RequirePerm(auth.PermAuditVerify)).Post("/audit/verify", s.handleVerifyAuditChain)

			// Unified Dynamic Meta-Model CMDB
			pr.Route("/cmdb", func(c chi.Router) {
				c.With(auth.RequirePerm(auth.PermCMDBRead)).Get("/model-groups", s.handleListModelGroups)
				c.With(auth.RequirePerm(auth.PermCMDBWrite)).Post("/model-groups", s.handleCreateModelGroup)
				c.With(auth.RequirePerm(auth.PermCMDBRead)).Get("/models", s.handleListModels)
				c.With(auth.RequirePerm(auth.PermCMDBWrite)).Post("/models", s.handleCreateModel)
				c.With(auth.RequirePerm(auth.PermCMDBWrite)).Delete("/models/{code}", s.handleDeleteModel)
				c.With(auth.RequirePerm(auth.PermCMDBWrite)).Put("/models/positions", s.handleSaveModelPositions)
				c.With(auth.RequirePerm(auth.PermCMDBWrite)).Post("/models/{code}/attrs", s.handleUpsertModelAttr)
				c.With(auth.RequirePerm(auth.PermCMDBWrite)).Delete("/attrs/{attrId}", s.handleDeleteModelAttr)
				c.With(auth.RequirePerm(auth.PermCMDBRead)).Get("/model-relations", s.handleListModelRelations)
				c.With(auth.RequirePerm(auth.PermCMDBWrite)).Post("/model-relations", s.handleCreateModelRelation)
				c.With(auth.RequirePerm(auth.PermCMDBWrite)).Delete("/model-relations/{id}", s.handleDeleteModelRelation)

				// Native Excelize (.xlsx) template / export / batch import
				c.With(auth.RequirePerm(auth.PermCMDBRead)).Get("/models/{code}/excel/template", s.handleExportTemplateXLSX)
				c.With(auth.RequirePerm(auth.PermCMDBRead)).Get("/models/{code}/excel/export", s.handleExportCIsXLSX)
				c.With(auth.RequirePerm(auth.PermCMDBWrite)).Post("/models/{code}/excel/import", s.handleImportCIsXLSX)

				// Unified CIs (Host = m_host CI + cmdb_host_endpoints)
				c.With(auth.RequirePerm(auth.PermCMDBRead)).Get("/cis", s.handleListCIs)
				c.With(auth.RequirePerm(auth.PermCMDBWrite)).Post("/cis", s.handleCreateCI)
				c.With(auth.RequirePerm(auth.PermCMDBRead)).Get("/cis/{id}", s.handleGetCI)
				c.With(auth.RequirePerm(auth.PermCMDBWrite)).Put("/cis/{id}", s.handleUpdateCI)
				c.With(auth.RequirePerm(auth.PermCMDBWrite)).Delete("/cis/{id}", s.handleDeleteCI)
				c.With(auth.RequirePerm(auth.PermCMDBRead)).Get("/cis/{id}/history", s.handleCIHistory)
				c.With(auth.RequirePerm(auth.PermCMDBRead)).Get("/cis/{id}/lineage", s.handleCILineage)
				c.With(auth.RequirePerm(auth.PermOpsExecute)).Post("/cis/{id}/collect", s.handleCollectHostFacts)
				c.With(auth.RequirePerm(auth.PermCMDBWrite)).Post("/ci-relations", s.handleCreateCIRelation)
				c.With(auth.RequirePerm(auth.PermCMDBWrite)).Delete("/ci-relations/{id}", s.handleDeleteCIRelation)

				// Dual Topology Tree & Service Templates
				c.With(auth.RequirePerm(auth.PermCMDBRead)).Get("/topology", s.handleTopologyTree)
				c.With(auth.RequirePerm(auth.PermCMDBWrite)).Post("/topology", s.handleCreateTopologyNode)
				c.With(auth.RequirePerm(auth.PermCMDBWrite)).Delete("/topology/{id}", s.handleDeleteTopologyNode)
				c.With(auth.RequirePerm(auth.PermCMDBRead)).Get("/topology/{id}/cis", s.handleListTopoNodeCIs)
				c.With(auth.RequirePerm(auth.PermCMDBWrite)).Post("/topology/{id}/bind", s.handleBindCIToTopo)
				c.With(auth.RequirePerm(auth.PermCMDBWrite)).Delete("/topology/{id}/bind/{ciId}", s.handleUnbindCIFromTopo)
				c.With(auth.RequirePerm(auth.PermCMDBRead)).Get("/service-templates", s.handleListServiceTemplates)
				c.With(auth.RequirePerm(auth.PermCMDBWrite)).Post("/service-templates", s.handleCreateServiceTemplate)

				// Dynamic Groups
				c.With(auth.RequirePerm(auth.PermCMDBRead)).Get("/dynamic-groups", s.handleListDynamicGroups)
				c.With(auth.RequirePerm(auth.PermCMDBWrite)).Post("/dynamic-groups", s.handleCreateDynamicGroup)
				c.With(auth.RequirePerm(auth.PermCMDBWrite)).Delete("/dynamic-groups/{id}", s.handleDeleteDynamicGroup)
				c.With(auth.RequirePerm(auth.PermCMDBRead)).Get("/dynamic-groups/{id}/evaluate", s.handleEvaluateDynamicGroup)

				// DCIM 42U Racks & IPAM CIDR
				c.With(auth.RequirePerm(auth.PermCMDBRead)).Get("/dcim/racks", s.handleListRacks)
				c.With(auth.RequirePerm(auth.PermCMDBWrite)).Post("/dcim/racks", s.handleCreateRack)
				c.With(auth.RequirePerm(auth.PermCMDBWrite)).Delete("/dcim/racks/{id}", s.handleDeleteRack)
				c.With(auth.RequirePerm(auth.PermCMDBWrite)).Post("/dcim/racks/{id}/units", s.handleMountRackUnit)
				c.With(auth.RequirePerm(auth.PermCMDBWrite)).Delete("/dcim/units/{unitId}", s.handleUnmountRackUnit)
				c.With(auth.RequirePerm(auth.PermCMDBRead)).Get("/ipam/prefixes", s.handleListPrefixes)
				c.With(auth.RequirePerm(auth.PermCMDBWrite)).Post("/ipam/prefixes", s.handleCreatePrefix)
				c.With(auth.RequirePerm(auth.PermCMDBWrite)).Delete("/ipam/prefixes/{id}", s.handleDeletePrefix)
				c.With(auth.RequirePerm(auth.PermCMDBRead)).Get("/ipam/addresses", s.handleListAddresses)
				c.With(auth.RequirePerm(auth.PermCMDBWrite)).Post("/ipam/prefixes/{id}/allocate", s.handleAllocateIP)
				c.With(auth.RequirePerm(auth.PermCMDBWrite)).Delete("/ipam/addresses/{id}", s.handleReleaseIP)
			})

			// Multi-Env Config & KMS Secrets Center
			pr.Route("/config", func(c chi.Router) {
				c.With(auth.RequirePerm(auth.PermConfigRead)).Get("/namespaces", s.handleListNamespaces)
				c.With(auth.RequirePerm(auth.PermConfigWrite)).Post("/namespaces", s.handleCreateNamespace)
				c.With(auth.RequirePerm(auth.PermConfigWrite)).Delete("/namespaces/{id}", s.handleDeleteNamespace)
				c.With(auth.RequirePerm(auth.PermConfigRead)).Get("/namespaces/{id}/items", s.handleListConfigItems)
				c.With(auth.RequirePerm(auth.PermConfigWrite)).Post("/namespaces/{id}/items", s.handleUpsertConfigItem)
				c.With(auth.RequirePerm(auth.PermConfigWrite)).Delete("/namespaces/{id}/items/{key}", s.handleDeleteConfigItem)
				c.With(auth.RequirePerm(auth.PermConfigReveal)).Post("/namespaces/{id}/reveal", s.handleRevealConfigSecret)
				c.With(auth.RequirePerm(auth.PermConfigRead)).Get("/namespaces/{id}/diff", s.handleDiffConfig)
				c.With(auth.RequirePerm(auth.PermConfigWrite)).Post("/namespaces/{id}/publish", s.handlePublishConfig)
				c.With(auth.RequirePerm(auth.PermConfigRead)).Get("/namespaces/{id}/releases", s.handleListConfigReleases)
				c.With(auth.RequirePerm(auth.PermConfigWrite)).Post("/namespaces/{id}/rollback", s.handleRollbackConfig)
				c.With(auth.RequirePerm(auth.PermConfigWrite)).Post("/namespaces/{id}/clone", s.handleCloneConfig)
				c.With(auth.RequirePerm(auth.PermConfigRead)).Post("/preview-params", s.handlePreviewEffectiveParams)
			})

			// CAS Artifact Catalog & Pure-Go OCI Engine
			pr.Route("/catalog", func(c chi.Router) {
				c.With(auth.RequirePerm(auth.PermCatalogRead)).Get("/artifacts", s.handleListArtifacts)
				c.With(auth.RequirePerm(auth.PermCatalogWrite)).Post("/artifacts", s.handleUploadArtifact)
				c.With(auth.RequirePerm(auth.PermCatalogRead)).Get("/artifacts/{id}/download", s.handleDownloadArtifact)
				c.With(auth.RequirePerm(auth.PermCatalogRead)).Post("/artifacts/{id}/verify", s.handleVerifyArtifact)
				c.With(auth.RequirePerm(auth.PermCatalogWrite)).Post("/artifacts/{id}/oci-push", s.handlePushOCI)
				c.With(auth.RequirePerm(auth.PermCatalogWrite)).Delete("/artifacts/{id}", s.handleDeleteArtifact)
				c.With(auth.RequirePerm(auth.PermCatalogRead)).Get("/bom", s.handleExportBOM)
			})

			// Declarative YAML Plugins & Real DAG/Batch Delivery Engine
			pr.Route("/delivery", func(c chi.Router) {
				c.With(auth.RequirePerm(auth.PermDeliveryRead)).Get("/plugins", s.handleListPlugins)
				c.With(auth.RequirePerm(auth.PermDeliveryWrite)).Post("/plugins", s.handleUpsertPlugin)
				c.With(auth.RequirePerm(auth.PermDeliveryWrite)).Delete("/plugins/{code}", s.handleDeletePlugin)
				c.With(auth.RequirePerm(auth.PermDeliveryRead)).Get("/releases", s.handleListReleases)
				c.With(auth.RequirePerm(auth.PermDeliveryWrite)).Post("/releases", s.handleCreateRelease)
				c.With(auth.RequirePerm(auth.PermDeliveryRead)).Get("/releases/{id}", s.handleGetRelease)
				c.With(auth.RequirePerm(auth.PermDeliveryExecute)).Post("/releases/{id}/execute", s.handleExecuteRelease)
				c.With(auth.RequirePerm(auth.PermDeliveryExecute)).Post("/releases/{id}/rollback", s.handleRollbackRelease)
			})

			// Operations Hub: Agent Fleet, Batch Jobs & CIDR TCP Prober
			pr.Route("/ops", func(c chi.Router) {
				c.With(auth.RequirePerm(auth.PermOpsRead)).Get("/agents", s.handleListAgents)
				c.With(auth.RequirePerm(auth.PermOpsRead)).Get("/enroll-tokens", s.handleListEnrollTokens)
				c.With(auth.RequirePerm(auth.PermOpsExecute)).Post("/enroll-tokens", s.handleCreateEnrollToken)
				c.With(auth.RequirePerm(auth.PermOpsExecute)).Post("/agents/{id}/revoke", s.handleRevokeAgent)
				c.With(auth.RequirePerm(auth.PermOpsExecute)).Post("/tcp-probe", s.handleTCPProbe)
				c.With(auth.RequirePerm(auth.PermOpsRead)).Get("/job-templates", s.handleListJobTemplates)
				c.With(auth.RequirePerm(auth.PermOpsRead)).Get("/jobs", s.handleListJobs)
				c.With(auth.RequirePerm(auth.PermOpsExecute)).Post("/jobs", s.handleRunBatchJob)
			})
		})
	})

	// Embedded Vue 3 SPA static assets & client-side router fallback
	if distFS := web.DistFS(); distFS != nil {
		r.NotFound(spaHandler(distFS))
	}
	return r
}

func (s *Server) handleHostTerminalWS(w http.ResponseWriter, r *http.Request) {
	ciID := chi.URLParam(r, "id")
	_, target, err := s.exec.ResolveSSHTarget(r.Context(), s.wsID(r), ciID)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "SSH_TARGET_ERROR", err.Error())
		return
	}
	wsSrv := websocket.Server{
		Handler: func(ws *websocket.Conn) {
			execplane.ServeWebSocketPTY(ws, target)
		},
		Handshake: func(cfg *websocket.Config, req *http.Request) error {
			return nil
		},
	}
	wsSrv.ServeHTTP(w, r)
}

func spaHandler(distFS fs.FS) http.HandlerFunc {
	fileServer := http.FileServer(http.FS(distFS))
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			httpx.WriteError(w, http.StatusNotFound, "NOT_FOUND", "API 端点不存在")
			return
		}
		clean := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if clean != "" {
			if f, err := distFS.Open(clean); err == nil {
				_ = f.Close()
				if strings.HasPrefix(clean, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		idx, err := distFS.Open("index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer func() { _ = idx.Close() }()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = io.Copy(w, idx)
	}
}

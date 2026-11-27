package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/kinstall/kinstall/api"
	"github.com/kinstall/kinstall/internal/app"
	"github.com/kinstall/kinstall/internal/authz"
	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/internal/i18n"
	"github.com/kinstall/kinstall/internal/obs"
	"github.com/kinstall/kinstall/internal/version"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
	"github.com/kinstall/kinstall/web"
)

// Server exposes the Chi v5 REST API, SSE stream, OpenAPI spec, Prometheus /metrics, and go:embed Web UI.
// Strictly obeys ARCHITECTURE §4.3: accesses business logic ONLY through *app.Facade and *authz.Service!
type Server struct {
	shutdownCtx context.Context
	app         *app.Facade
	authSvc     *authz.Service
	i18n        *i18n.Catalog
	metrics     *obs.Metrics
	logger      *slog.Logger
	devBypass   bool
}

// Options configures optional server flags.
type Options struct {
	DevBypassAuth bool
	Metrics       *obs.Metrics
}

// NewRouter constructs the complete HTTP handler tree (G1 §2-§3, WEB_DESIGN §4.3).
func NewRouter(shutdownCtx context.Context, facade *app.Facade, authSvc *authz.Service, cat *i18n.Catalog, logger *slog.Logger, opts Options) http.Handler {
	if shutdownCtx == nil {
		shutdownCtx = context.Background()
	}
	if logger == nil {
		logger = slog.Default()
	}
	m := opts.Metrics
	if m == nil {
		m = obs.NewMetrics()
	}
	s := &Server{
		shutdownCtx: shutdownCtx,
		app:         facade,
		authSvc:     authSvc,
		i18n:        cat,
		metrics:     m,
		logger:      logger,
		devBypass:   opts.DevBypassAuth,
	}

	r := chi.NewRouter()
	r.Use(traceIDMiddleware)

	// Probes & Observability
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "version": version.Get()})
	})
	r.Get("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		if s.app.IsDraining() {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "draining"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})
	r.Handle("/metrics", promhttp.HandlerFor(m.Registry, promhttp.HandlerOpts{}))

	// Public API endpoints
	r.Get("/api/v1/openapi.yaml", func(w http.ResponseWriter, _ *http.Request) {
		raw, err := api.SpecFS.ReadFile("openapi.yaml")
		if err != nil {
			http.NotFound(w, nil)
			return
		}
		w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(raw)
	})
	r.Post("/api/v1/auth/login", s.handleLoginLocal)
	r.Get("/api/v1/auth/oidc/config", s.handleOIDCConfig)
	r.Get("/api/v1/auth/oidc/login", s.handleOIDCLogin)
	r.Get("/api/v1/auth/oidc/callback", s.handleOIDCCallback)
	r.Post("/api/v1/auth/logout", s.handleLogout)

	// Authenticated Control Plane REST + SSE API
	r.Route("/api/v1", func(ar chi.Router) {
		ar.Use(authMiddleware(authSvc, cat, s.devBypass))
		ar.Use(idempotencyMiddleware(facade.Idempotency(), cat))

		ar.Get("/me", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, ActorFromContext(r.Context()))
		})
		ar.Get("/plugins", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{"items": s.app.ListPlugins()})
		})
		ar.Get("/plugins/{product}", s.handleGetPlugin)
		ar.Get("/hostinit/profiles", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{"profiles": s.app.ListHostInitProfiles(), "modules": s.app.ListHostInitModules()})
		})

		// Auth users & tokens
		ar.Get("/users", s.handleListUsers)
		ar.Post("/users", s.handleCreateUser)
		ar.Get("/auth/tokens", s.handleListAPITokens)
		ar.Post("/auth/tokens", s.handleCreateAPIToken)
		ar.Delete("/auth/tokens/{tokenId}", s.handleRevokeAPIToken)

		// Projects
		ar.Get("/projects", s.handleListProjects)
		ar.Post("/projects", s.handleCreateProject)
		ar.Delete("/projects/{projectId}", s.handleDeleteProject)

		// Global audit verification
		ar.Post("/audit:verify", s.handleVerifyAudit)

		ar.Route("/projects/{projectId}", func(pr chi.Router) {
			// Credentials & Secrets Rewrap (T-M1-02)
			pr.Get("/credentials", s.handleListCredentials)
			pr.Post("/credentials", s.handleCreateCredential)
			pr.Delete("/credentials/{credId}", s.handleDeleteCredential)
			pr.Post("/secrets:rewrap", s.handleRewrapSecrets)

			// Hosts, CMDB Asset Center, Bulk Import/Export, Baseline Check, HostInit & External CMDB Sync
			pr.Get("/hosts", s.handleListHosts)
			pr.Post("/hosts", s.handleCreateHost)
			pr.Get("/hosts:template", s.handleDownloadHostTemplate)
			pr.Get("/hosts:export", s.handleExportHosts)
			pr.Post("/hosts:import", s.handleImportHosts)
			pr.Post("/hosts:discover", s.handleDiscoverHosts)
			pr.Post("/hosts:batch", s.handleBatchHosts)
			pr.Put("/hosts/{hostId}", s.handleUpdateHost)
			pr.Delete("/hosts/{hostId}", s.handleDeleteHost)
			pr.Get("/hosts/{hostId}/impact", s.handleHostImpact)
			pr.Post("/hosts/{hostId}:probe", s.handleProbeHost)
			pr.Post("/hosts/{hostId}:check", s.handleCheckHostBaseline)
			pr.Post("/hosts/{hostId}:init", s.handleInitHost)
			pr.Post("/cmdb:sync", s.handleCMDBSync)
			pr.Get("/cmdb/overview", s.handleCMDBOverview)
			pr.Get("/cmdb/groups", s.handleListDynamicGroups)
			pr.Post("/cmdb/groups", s.handleCreateDynamicGroup)
			pr.Put("/cmdb/groups/{groupId}", s.handleUpdateDynamicGroup)
			pr.Delete("/cmdb/groups/{groupId}", s.handleDeleteDynamicGroup)
			pr.Post("/cmdb/groups:preview", s.handlePreviewDynamicGroup)
			pr.Get("/cmdb/sync-sources", s.handleListCMDBSyncSources)
			pr.Post("/cmdb/sync-sources", s.handleCreateCMDBSyncSource)
			pr.Delete("/cmdb/sync-sources/{sourceId}", s.handleDeleteCMDBSyncSource)
			pr.Post("/cmdb/sync-sources/{sourceId}:run", s.handleRunCMDBSyncSource)

			// Media Policies (Local / Huawei OBS·S3 / Online, ADR-077)
			pr.Get("/media-policies", s.handleListMediaPolicies)
			pr.Post("/media-policies", s.handleCreateMediaPolicy)
			pr.Put("/media-policies/{policyId}", s.handleUpdateMediaPolicy)
			pr.Post("/media-policies/{policyId}:test", s.handleTestMediaPolicy)

			// Instances + 2-phase Plan / Apply / Detail / Update / Delete
			pr.Get("/instances", s.handleListInstances)
			pr.Post("/instances", s.handleCreateInstance)
			pr.Get("/instances/{instanceId}", s.handleGetInstanceDetail)
			pr.Put("/instances/{instanceId}", s.handleUpdateInstance)
			pr.Delete("/instances/{instanceId}", s.handleDeleteInstance)
			pr.Post("/instances/{instanceId}:plan", s.handlePlanInstance)
			pr.Post("/instances/{instanceId}:apply", s.handleApplyInstance)

			// Stacks (Live Validate / Create / Update / Delete / Apply / Reverse Uninstall)
			pr.Get("/stacks", s.handleListStacks)
			pr.Post("/stacks", s.handleCreateStack)
			pr.Put("/stacks/{stackId}", s.handleUpdateStack)
			pr.Delete("/stacks/{stackId}", s.handleDeleteStack)
			pr.Post("/stacks:validate", s.handleValidateStack)
			pr.Post("/stacks/{stackId}:apply", s.handleApplyStack)
			pr.Post("/stacks/{stackId}:uninstall", s.handleUninstallStack)

			// Operations, SSE, Reports & Four-Eyes Approvals
			pr.Get("/operations", s.handleListOperations)
			pr.Get("/operations/{opId}", s.handleGetOperation)
			pr.Get("/operations/{opId}/report", s.handleOperationReport)
			pr.Get("/operations/{opId}/events", func(w http.ResponseWriter, r *http.Request) {
				s.handleOperationEventsSSE(w, r, chi.URLParam(r, "opId"))
			})
			pr.Post("/operations/{opId}:approve", func(w http.ResponseWriter, r *http.Request) {
				s.handleDecideApproval(w, r, true)
			})
			pr.Post("/operations/{opId}:reject", func(w http.ResponseWriter, r *http.Request) {
				s.handleDecideApproval(w, r, false)
			})
			pr.Post("/operations/{opId}:resume", s.handleResumeOperation)
			pr.Post("/operations/{opId}:cancel", s.handleCancelOperation)
			pr.Get("/approvals", s.handleListApprovals)

			// Multi-Cluster Kubeconfigs
			pr.Get("/clusters/{instanceId}/kubeconfigs", s.handleListKubeconfigs)
			pr.Post("/clusters/{instanceId}/kubeconfigs", s.handleIssueKubeconfig)
			pr.Delete("/clusters/kubeconfigs/{kcId}", s.handleRevokeKubeconfig)

			// Notification Channels & Storm-Folding Test (G2 §3)
			pr.Get("/notification-channels", s.handleListNotificationChannels)
			pr.Post("/notification-channels", s.handleCreateNotificationChannel)
			pr.Delete("/notification-channels/{channelId}", s.handleDeleteNotificationChannel)
			pr.Post("/notification-channels:test", s.handleTestNotificationChannels)

			// GitOps Export/Diff, Support Bundle & Compliance Evidence Download
			pr.Get("/gitops/export", s.handleGitOpsExport)
			pr.Post("/gitops:diff", s.handleGitOpsDiff)
			pr.Post("/support-bundle", s.handleSupportBundle)
			pr.Get("/audit", s.handleListAuditLogs)
			pr.Post("/compliance:report", s.handleComplianceReport)
			pr.Get("/compliance:download", s.handleComplianceDownload)
		})
	})

	// Embedded Offline Vue 3 SPA (WEB_DESIGN §4.3 B: History fallback + immutable /assets + strict CSP)
	mountEmbeddedUI(r, web.DistFS())
	return r
}

func mountEmbeddedUI(r chi.Router, distFS fs.FS) {
	if distFS == nil {
		return
	}
	fileSrv := http.FileServer(http.FS(distFS))
	r.NotFound(func(w http.ResponseWriter, req *http.Request) {
		if strings.HasPrefix(req.URL.Path, "/api/") {
			http.NotFound(w, req)
			return
		}
		// Strict CSP: zero external CDN, self-hosted fonts & scripts only (DESIGN.md §6.5, Redline 8)
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")

		clean := strings.TrimPrefix(path.Clean("/"+req.URL.Path), "/")
		if clean == "" {
			clean = "index.html"
		}
		if fi, err := fs.Stat(distFS, clean); err == nil && !fi.IsDir() {
			if strings.HasPrefix(clean, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			fileSrv.ServeHTTP(w, req)
			return
		}
		// HTML5 History SPA fallback to index.html
		idx, err := fs.ReadFile(distFS, "index.html")
		if err != nil {
			http.NotFound(w, req)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(idx)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) handleLoginLocal(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "API", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	tok, user, err := s.authSvc.LoginLocal(r.Context(), body.Username, body.Password, r.RemoteAddr, r.UserAgent())
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	// #nosec G124 -- Secure dynamically follows TLS/X-Forwarded-Proto so local HTTP dev and HTTPS prod both work
	http.SetCookie(w, &http.Cookie{
		Name:     "kinstall_session",
		Value:    tok,
		Path:     "/",
		Secure:   r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https"),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   43200,
	})
	writeJSON(w, http.StatusOK, map[string]any{"token": tok, "user": user})
}

func (s *Server) handleCreateAPIToken(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name      string   `json:"name"`
		ProjectID *string  `json:"projectId"`
		Scopes    []string `json:"scopes"`
		TTLDays   int      `json:"ttlDays"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "API", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	ttl := time.Duration(body.TTLDays) * 24 * time.Hour
	plain, meta, err := s.authSvc.CreateAPIToken(r.Context(), ActorFromContext(r.Context()).ID, body.ProjectID, body.Name, body.Scopes, ttl)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"token": plain, "meta": meta})
}

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	items, err := s.app.ListProjects(r.Context(), ActorFromContext(r.Context()))
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "API", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	p, err := s.app.CreateProject(r.Context(), ActorFromContext(r.Context()), body.Name, body.Description)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) handleCreateCredential(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name      string `json:"name"`
		Kind      string `json:"kind"`
		Plaintext string `json:"plaintext"`
		Secret    string `json:"secret"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "API", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	rawSecret := body.Plaintext
	if rawSecret == "" {
		rawSecret = body.Secret
	}
	if strings.TrimSpace(body.Name) == "" || rawSecret == "" {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "API", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, fmt.Errorf("credential name and plaintext/secret are required")))
		return
	}
	c, err := s.app.CreateCredential(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"), body.Name, body.Kind, []byte(rawSecret))
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) handleListHosts(w http.ResponseWriter, r *http.Request) {
	items, err := s.app.ListHosts(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"))
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleCreateHost(w http.ResponseWriter, r *http.Request) {
	var body struct {
		domain.Host
		InlineCredential *app.InlineCredentialSpec `json:"inlineCredential,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "API", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	h := body.Host
	h.ProjectID = chi.URLParam(r, "projectId")
	created, err := s.app.CreateHostWithInlineCred(r.Context(), ActorFromContext(r.Context()), &h, body.InlineCredential)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	w.Header().Set("ETag", formatETag(created.Version))
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleProbeHost(w http.ResponseWriter, r *http.Request) {
	facts, err := s.app.ProbeHostFacts(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"), chi.URLParam(r, "hostId"))
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, facts)
}

func (s *Server) handleListMediaPolicies(w http.ResponseWriter, r *http.Request) {
	items, err := s.app.ListMediaPolicies(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"))
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleCreateMediaPolicy(w http.ResponseWriter, r *http.Request) {
	var mp domain.MediaPolicy
	if err := json.NewDecoder(r.Body).Decode(&mp); err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "API", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	pid := chi.URLParam(r, "projectId")
	if mp.Scope == "" || mp.Scope == "project" {
		mp.Scope = "project"
		mp.ProjectID = &pid
	}
	created, err := s.app.CreateMediaPolicy(r.Context(), ActorFromContext(r.Context()), &mp)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	w.Header().Set("ETag", formatETag(created.Version))
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleUpdateMediaPolicy(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	polID := chi.URLParam(r, "policyId")
	existingList, err := s.app.ListMediaPolicies(r.Context(), ActorFromContext(r.Context()), pid)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	var current *domain.MediaPolicy
	for i := range existingList {
		if existingList[i].ID == polID {
			current = &existingList[i]
			break
		}
	}
	if current == nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeDBNotFound, "DB", http.StatusNotFound, sdk.CodeDBNotFound, false, nil))
		return
	}
	if err := checkIfMatch(r, current.Version); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	var mp domain.MediaPolicy
	if err := json.NewDecoder(r.Body).Decode(&mp); err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "API", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	mp.ID = polID
	mp.ProjectID = current.ProjectID
	mp.Version = current.Version
	updated, err := s.app.UpdateMediaPolicy(r.Context(), ActorFromContext(r.Context()), &mp)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	w.Header().Set("ETag", formatETag(updated.Version))
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleTestMediaPolicy(w http.ResponseWriter, r *http.Request) {
	rep, err := s.app.TestMediaPolicy(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"), chi.URLParam(r, "policyId"))
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

func (s *Server) handleListInstances(w http.ResponseWriter, r *http.Request) {
	items, err := s.app.ListInstances(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"))
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleCreateInstance(w http.ResponseWriter, r *http.Request) {
	var inst domain.Instance
	if err := json.NewDecoder(r.Body).Decode(&inst); err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "API", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	inst.ProjectID = chi.URLParam(r, "projectId")
	created, err := s.app.CreateInstance(r.Context(), ActorFromContext(r.Context()), &inst)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	w.Header().Set("ETag", formatETag(created.Version))
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handlePlanInstance(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action    string              `json:"action"`
		HostRoles map[string][]string `json:"hostRoles"`
		Params    map[string]any      `json:"params"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	plan, rep, err := s.app.PlanInstanceWithParams(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"), chi.URLParam(r, "instanceId"), body.Action, body.HostRoles, body.Params, nil)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"plan": plan, "validation": rep})
}

func (s *Server) handleApplyInstance(w http.ResponseWriter, r *http.Request) {
	var body struct {
		PlanID        string `json:"planId"`
		Action        string `json:"action"`
		ForceOverride bool   `json:"forceOverride"`
		SkipVerify    bool   `json:"skipVerify"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "API", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	var idem *string
	if k := strings.TrimSpace(r.Header.Get("Idempotency-Key")); k != "" {
		idem = &k
	}
	op, err := s.app.ApplyInstance(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"), chi.URLParam(r, "instanceId"), body.PlanID, body.Action, body.ForceOverride, body.SkipVerify, idem)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	w.Header().Set("Location", "/api/v1/projects/"+op.ProjectID+"/operations/"+op.ID)
	writeJSON(w, http.StatusAccepted, op)
}

func (s *Server) handleListStacks(w http.ResponseWriter, r *http.Request) {
	items, err := s.app.ListStacks(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"))
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleCreateStack(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Env      string `json:"env"`
		Name     string `json:"name"`
		SpecYAML string `json:"specYaml"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "API", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	st, dag, err := s.app.CreateStack(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"), body.Env, body.Name, body.SpecYAML)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"stack": st, "dag": dag})
}

func (s *Server) handleListOperations(w http.ResponseWriter, r *http.Request) {
	items, err := s.app.ListOperationsEnriched(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"), 50)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleGetOperation(w http.ResponseWriter, r *http.Request) {
	op, err := s.app.GetOperation(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "opId"))
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, op)
}

func (s *Server) handleDecideApproval(w http.ResponseWriter, r *http.Request, approve bool) {
	var body struct {
		Note string `json:"note"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if err := s.app.DecideApproval(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"), chi.URLParam(r, "opId"), approve, body.Note); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"approved": approve})
}

func (s *Server) handleResumeOperation(w http.ResponseWriter, r *http.Request) {
	if err := s.app.ResumeOperation(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "opId")); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "resumed"})
}

func (s *Server) handleListApprovals(w http.ResponseWriter, r *http.Request) {
	items, err := s.app.ListPendingApprovalsEnriched(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"))
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleListKubeconfigs(w http.ResponseWriter, r *http.Request) {
	items, err := s.app.ListClusterKubeconfigs(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"), chi.URLParam(r, "instanceId"))
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleIssueKubeconfig(w http.ResponseWriter, r *http.Request) {
	var body struct {
		K8sRole    string   `json:"k8sRole"`
		Namespaces []string `json:"namespaces"`
		TTLHours   int      `json:"ttlHours"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "API", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	issued, err := s.app.IssueClusterKubeconfig(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"), chi.URLParam(r, "instanceId"), body.K8sRole, body.Namespaces, time.Duration(body.TTLHours)*time.Hour)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusCreated, issued)
}

func (s *Server) handleRevokeKubeconfig(w http.ResponseWriter, r *http.Request) {
	if err := s.app.RevokeClusterKubeconfig(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"), chi.URLParam(r, "kcId")); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListAuditLogs(w http.ResponseWriter, r *http.Request) {
	afterSeq, _ := strconv.ParseInt(r.URL.Query().Get("afterSeq"), 10, 64)
	items, err := s.app.ListAuditLogs(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"), afterSeq, 200)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleVerifyAudit(w http.ResponseWriter, r *http.Request) {
	res, err := s.app.VerifyAuditChain(r.Context(), ActorFromContext(r.Context()))
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleComplianceReport(w http.ResponseWriter, r *http.Request) {
	rep, _, digest, err := s.app.GenerateComplianceReport(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"))
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"report": rep, "evidenceTarGzSha256": digest})
}

func (s *Server) handleRewrapSecrets(w http.ResponseWriter, r *http.Request) {
	n, err := s.app.RewrapAllCredentials(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"))
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rewrapped": n})
}

func (s *Server) handleCMDBSync(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Hosts []domain.Host `json:"hosts"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "API", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	created, updated, err := s.app.SyncExternalCMDBHosts(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"), body.Hosts)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"created": created, "updated": updated})
}

func (s *Server) handleOperationReport(w http.ResponseWriter, r *http.Request) {
	format := r.URL.Query().Get("format")
	raw, err := s.app.ExportOperationReport(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "opId"), format)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	if strings.EqualFold(format, "json") {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
	} else {
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	// #nosec G705 -- response is text/markdown or application/json with nosniff header
	_, _ = w.Write(raw)
}

package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/kinstall/kinstall/internal/agenthub"
	"github.com/kinstall/kinstall/internal/authz"
	"github.com/kinstall/kinstall/internal/cmdb"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

func (s *Server) requireAgentHub(w http.ResponseWriter, r *http.Request) *agenthub.Service {
	svc := s.app.AgentHub()
	if svc == nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeDBUnavailableOrSchema, "EXEC", http.StatusServiceUnavailable, sdk.CodeDBUnavailableOrSchema, false, fmt.Errorf("agent hub unavailable")))
		return nil
	}
	return svc
}

// --- Control-Plane Fleet Management APIs (User Authenticated) ---

func (s *Server) handleIssueAgentEnrollToken(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	actor := ActorFromContext(r.Context())
	if err := authz.Authorize(actor, pid, "host.write"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireAgentHub(w, r)
	if svc == nil {
		return
	}
	var body struct {
		Zone       string `json:"zone"`
		MaxUses    int    `json:"maxUses"`
		TTLMinutes int    `json:"ttlMinutes"`
		ServerURL  string `json:"serverUrl"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.ServerURL == "" && r.Host != "" {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		body.ServerURL = scheme + "://" + r.Host
	}
	tok, err := svc.IssueEnrollToken(r.Context(), pid, actor.Username, body.Zone, body.MaxUses, body.TTLMinutes, body.ServerURL)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	s.app.RecordAuditEvent(r.Context(), pid, actor.Username, "agent.token.issue", "enroll_token", tok.ID, map[string]any{
		"zone": tok.Zone, "maxUses": tok.MaxUses,
	})
	writeJSON(w, http.StatusCreated, tok)
}

func (s *Server) handleListFleetAgents(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	if err := authz.Authorize(ActorFromContext(r.Context()), pid, "resource.read"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireAgentHub(w, r)
	if svc == nil {
		return
	}
	items, err := svc.ListAgents(r.Context(), pid)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleRevokeFleetAgent(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	actor := ActorFromContext(r.Context())
	if err := authz.Authorize(actor, pid, "host.write"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireAgentHub(w, r)
	if svc == nil {
		return
	}
	aid := chi.URLParam(r, "agentId")
	if err := svc.RevokeAgent(r.Context(), pid, aid); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	s.app.RecordAuditEvent(r.Context(), pid, actor.Username, "agent.revoke", "exec_agent", aid, map[string]any{"status": "REVOKED"})
	writeJSON(w, http.StatusOK, map[string]string{"revoked": aid})
}

func (s *Server) handleFleetTCPProbes(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	if err := authz.Authorize(ActorFromContext(r.Context()), pid, "host.write"); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	svc := s.requireAgentHub(w, r)
	if svc == nil {
		return
	}
	var body struct {
		AgentID    string                    `json:"agentId"`
		Targets    []agenthub.TCPProbeTarget `json:"targets"`
		TimeoutSec int                       `json:"timeoutSec"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "EXEC", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	res, err := svc.RunTCPProbes(r.Context(), pid, body.AgentID, body.Targets, body.TimeoutSec)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": res})
}

// --- Outbound Edge Agent Protocol Endpoints (Authenticated via EnrollToken / Agent Bearer Token) ---

func (s *Server) handleAgentEnroll(w http.ResponseWriter, r *http.Request) {
	svc := s.requireAgentHub(w, r)
	if svc == nil {
		return
	}
	var req agenthub.EnrollRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "EXEC", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	resp, agent, err := svc.EnrollAgent(r.Context(), req)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	// Auto-sync enrolled agent host into CMDB m_host CI table!
	if cm := s.app.CMDB(); cm != nil && agent != nil && agent.IPAddress != "" {
		_, _ = cm.CreateCIInstance(r.Context(), agent.ProjectID, "agent:"+agent.ID, &cmdb.CIInstance{
			ModelCode: "m_host",
			Name:      agent.Hostname,
			UniqueKey: agent.IPAddress,
			Status:    "active",
			Env:       "prod",
			Owner:     "agent-auto",
			Attrs: map[string]any{
				"inner_ip":  agent.IPAddress,
				"os_distro": agent.OSInfo,
				"arch":      agent.OSArch,
			},
		})
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (s *Server) handleAgentHeartbeat(w http.ResponseWriter, r *http.Request) {
	svc := s.requireAgentHub(w, r)
	if svc == nil {
		return
	}
	agent, err := svc.AuthenticateAgent(r.Context(), r.Header.Get("Authorization"))
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	var body struct {
		IPAddress string         `json:"ipAddress"`
		OSInfo    string         `json:"osInfo"`
		Metrics   map[string]any `json:"metrics"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if err := svc.RecordHeartbeat(r.Context(), agent.ID, body.IPAddress, body.OSInfo, body.Metrics); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "agentId": agent.ID})
}

func (s *Server) handleAgentPollTasks(w http.ResponseWriter, r *http.Request) {
	svc := s.requireAgentHub(w, r)
	if svc == nil {
		return
	}
	agent, err := svc.AuthenticateAgent(r.Context(), r.Header.Get("Authorization"))
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	items, err := svc.PollPendingTasks(r.Context(), agent.ID)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleAgentTaskResult(w http.ResponseWriter, r *http.Request) {
	svc := s.requireAgentHub(w, r)
	if svc == nil {
		return
	}
	agent, err := svc.AuthenticateAgent(r.Context(), r.Header.Get("Authorization"))
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	var body struct {
		Status   string         `json:"status"`
		ExitCode int            `json:"exitCode"`
		Stdout   string         `json:"stdout"`
		Stderr   string         `json:"stderr"`
		Outputs  map[string]any `json:"outputs"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "EXEC", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	if err := svc.ReportTaskResult(r.Context(), agent.ID, chi.URLParam(r, "taskId"), body.Status, body.ExitCode, body.Stdout, body.Stderr, body.Outputs); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "recorded"})
}

// --- Pure-Go Native OCI Inspect & Push Handlers (Zero docker CLI / docker.sock) ---

func (s *Server) handleInspectOCIArtifact(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	logicalPath := r.URL.Query().Get("path")
	res, err := s.app.InspectOCIArtifact(r.Context(), ActorFromContext(r.Context()), pid, logicalPath)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handlePushOCIArtifactNative(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "projectId")
	var body struct {
		LogicalPath  string `json:"logicalPath"`
		TargetRef    string `json:"targetRef"`
		ExpectedArch string `json:"expectedArch"`
		Username     string `json:"username"`
		Password     string `json:"password"`
		InsecureTLS  bool   `json:"insecureTls"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "OCI", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	res, err := s.app.PushOCIArtifactNative(r.Context(), ActorFromContext(r.Context()), pid, body.LogicalPath, body.TargetRef, body.ExpectedArch, body.Username, body.Password, body.InsecureTLS)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

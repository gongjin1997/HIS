package agenthub

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/kinstall/kinstall/internal/ports"

	"github.com/kinstall/kinstall/internal/domain"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// EnrollTokenIssue is returned ONCE at creation time; DB only stores SHA-256 hash (LiveOps 03.8 §3.1).
type EnrollTokenIssue struct {
	ID         string    `json:"id"`
	ProjectID  string    `json:"projectId"`
	Zone       string    `json:"zone"`
	Token      string    `json:"token"`
	MaxUses    int       `json:"maxUses"`
	ExpiresAt  time.Time `json:"expiresAt"`
	InstallCmd string    `json:"installCmd"`
}

type AgentRecord struct {
	ID              string         `json:"id"`
	ProjectID       string         `json:"projectId"`
	HostID          string         `json:"hostId,omitempty"`
	Hostname        string         `json:"hostname"`
	IPAddress       string         `json:"ipAddress"`
	Zone            string         `json:"zone"`
	OSArch          string         `json:"osArch"`
	OSInfo          string         `json:"osInfo"`
	AgentVersion    string         `json:"agentVersion"`
	Status          string         `json:"status"` // ONLINE | OFFLINE | REVOKED
	Capabilities    []string       `json:"capabilities"`
	Metrics         map[string]any `json:"metrics"`
	LastHeartbeatAt time.Time      `json:"lastHeartbeatAt"`
	EnrolledAt      time.Time      `json:"enrolledAt"`
}

type AgentTask struct {
	ID        string         `json:"id"`
	ProjectID string         `json:"projectId"`
	AgentID   string         `json:"agentId"`
	Kind      string         `json:"kind"` // exec | probe_tcp | collect_facts
	Payload   map[string]any `json:"payload"`
	Status    string         `json:"status"` // PENDING | DISPATCHED | SUCCEEDED | FAILED | CANCELLED
	ExitCode  int            `json:"exitCode"`
	Stdout    string         `json:"stdout"`
	Stderr    string         `json:"stderr"`
	Outputs   map[string]any `json:"outputs"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
}

type EnrollRequest struct {
	EnrollToken  string   `json:"enrollToken"`
	Hostname     string   `json:"hostname"`
	IPAddress    string   `json:"ipAddress"`
	OSArch       string   `json:"osArch"`
	OSInfo       string   `json:"osInfo"`
	AgentVersion string   `json:"agentVersion"`
	Capabilities []string `json:"capabilities"`
}

type EnrollResponse struct {
	AgentID   string `json:"agentId"`
	ProjectID string `json:"projectId"`
	Zone      string `json:"zone"`
	AuthToken string `json:"authToken"`
}

type TCPProbeTarget struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

type TCPProbeResult struct {
	Target     string `json:"target"`
	Status     string `json:"status"` // OK | CONN_REFUSED | TIMEOUT | BLOCKED
	LatencyMs  int64  `json:"latencyMs"`
	Error      string `json:"error,omitempty"`
	ExecutedBy string `json:"executedBy"` // control-plane | agent:<id>
}

type Service struct {
	writeDB ports.QueryExec
	readDB  ports.QueryExec
}

func NewService(writeDB, readDB ports.QueryExec) *Service {
	if readDB == nil {
		readDB = writeDB
	}
	return &Service{writeDB: writeDB, readDB: readDB}
}

func parseTS(s string) time.Time {
	s = strings.TrimSpace(s)
	for _, l := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		if t, err := time.Parse(l, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

func randomHexToken(nBytes int) string {
	b := make([]byte, nBytes)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func hashToken(tok string) string {
	sum := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(sum[:])
}

// IssueEnrollToken creates a one-time short-lived (default 15m) zero-trust Agent enrollment token.
func (s *Service) IssueEnrollToken(ctx context.Context, projectID, actor, zone string, maxUses, ttlMinutes int, serverURL string) (*EnrollTokenIssue, error) {
	zone = strings.TrimSpace(zone)
	if zone == "" {
		zone = "default"
	}
	if maxUses <= 0 || maxUses > 100 {
		maxUses = 1
	}
	if ttlMinutes <= 0 || ttlMinutes > 1440 {
		ttlMinutes = 15
	}
	rawTok := "kagt_" + randomHexToken(24)
	now := time.Now().UTC()
	exp := now.Add(time.Duration(ttlMinutes) * time.Minute)
	id := domain.NewID("etok")

	_, err := s.writeDB.ExecRaw(ctx,
		`INSERT INTO exec_enroll_tokens (id, project_id, zone, token_sha256, max_uses, used_count, expires_at, created_by, created_at)
		 VALUES (?, ?, ?, ?, ?, 0, ?, ?, ?)`,
		id, projectID, zone, hashToken(rawTok), maxUses, exp.Format(time.RFC3339Nano), actor, now.Format(time.RFC3339Nano),
	)
	if err != nil {
		return nil, err
	}

	if serverURL == "" {
		serverURL = "http://127.0.0.1:8080"
	}
	cmd := fmt.Sprintf("kinstall agent --server %s --token %s --zone %s", serverURL, rawTok, zone)
	return &EnrollTokenIssue{
		ID:         id,
		ProjectID:  projectID,
		Zone:       zone,
		Token:      rawTok,
		MaxUses:    maxUses,
		ExpiresAt:  exp,
		InstallCmd: cmd,
	}, nil
}

// EnrollAgent validates a one-time enroll token, increments used_count, and mints a dedicated agent credential.
func (s *Service) EnrollAgent(ctx context.Context, req EnrollRequest) (*EnrollResponse, *AgentRecord, error) {
	req.EnrollToken = strings.TrimSpace(req.EnrollToken)
	if req.EnrollToken == "" {
		return nil, nil, sdk.NewError(sdk.CodeAuthUnauthenticated, "EXEC", http.StatusUnauthorized, sdk.CodeAuthUnauthenticated, false, errors.New("enrollToken is required"))
	}
	var tokID, projectID, zone, expStr string
	var maxUses, used int
	err := s.readDB.ScanRaw(ctx, `SELECT id, project_id, zone, max_uses, used_count, expires_at FROM exec_enroll_tokens WHERE token_sha256 = ?`, []any{hashToken(req.EnrollToken)}, &tokID, &projectID, &zone, &maxUses, &used, &expStr)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, sdk.NewError(sdk.CodeAuthUnauthenticated, "EXEC", http.StatusUnauthorized, sdk.CodeAuthUnauthenticated, false, errors.New("invalid enroll token"))
	}
	if err != nil {
		return nil, nil, err
	}
	if time.Now().UTC().After(parseTS(expStr)) {
		return nil, nil, sdk.NewError(sdk.CodeAuthUnauthenticated, "EXEC", http.StatusUnauthorized, sdk.CodeAuthUnauthenticated, false, errors.New("enroll token expired"))
	}
	if used >= maxUses {
		return nil, nil, sdk.NewError(sdk.CodeAuthUnauthenticated, "EXEC", http.StatusUnauthorized, sdk.CodeAuthUnauthenticated, false, errors.New("enroll token already exhausted"))
	}

	res, err := s.writeDB.ExecRaw(ctx,
		`UPDATE exec_enroll_tokens SET used_count = used_count + 1 WHERE id = ? AND used_count < max_uses`, tokID,
	)
	if err != nil {
		return nil, nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, nil, sdk.NewError(sdk.CodeAuthUnauthenticated, "EXEC", http.StatusUnauthorized, sdk.CodeAuthUnauthenticated, false, errors.New("enroll token already consumed"))
	}

	authTok := "kagta_" + randomHexToken(32)
	now := time.Now().UTC()
	if req.Hostname == "" {
		req.Hostname = "agent-host"
	}
	if req.OSArch == "" {
		req.OSArch = "amd64"
	}
	if req.AgentVersion == "" {
		req.AgentVersion = "1.0.0"
	}
	if len(req.Capabilities) == 0 {
		req.Capabilities = []string{"exec", "probe_tcp", "collect_facts"}
	}
	capB, _ := json.Marshal(req.Capabilities)
	agent := &AgentRecord{
		ID:              domain.NewID("agt"),
		ProjectID:       projectID,
		Hostname:        req.Hostname,
		IPAddress:       req.IPAddress,
		Zone:            zone,
		OSArch:          req.OSArch,
		OSInfo:          req.OSInfo,
		AgentVersion:    req.AgentVersion,
		Status:          "ONLINE",
		Capabilities:    req.Capabilities,
		Metrics:         map[string]any{},
		LastHeartbeatAt: now,
		EnrolledAt:      now,
	}
	_, err = s.writeDB.ExecRaw(ctx,
		`INSERT INTO exec_agents (id, project_id, host_id, hostname, ip_address, zone, os_arch, os_info, agent_version, status, auth_token_sha256, capabilities_json, metrics_json, last_heartbeat_at, enrolled_at)
		 VALUES (?, ?, '', ?, ?, ?, ?, ?, ?, 'ONLINE', ?, ?, '{}', ?, ?)`,
		agent.ID, projectID, agent.Hostname, agent.IPAddress, zone, agent.OSArch, agent.OSInfo, agent.AgentVersion, hashToken(authTok), string(capB), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
	)
	if err != nil {
		return nil, nil, err
	}

	return &EnrollResponse{
		AgentID:   agent.ID,
		ProjectID: projectID,
		Zone:      zone,
		AuthToken: authTok,
	}, agent, nil
}

func (s *Service) AuthenticateAgent(ctx context.Context, rawToken string) (*AgentRecord, error) {
	tok := strings.TrimSpace(strings.TrimPrefix(rawToken, "Bearer "))
	if tok == "" {
		return nil, sdk.NewError(sdk.CodeAuthUnauthenticated, "EXEC", http.StatusUnauthorized, sdk.CodeAuthUnauthenticated, false, errors.New("missing agent token"))
	}
	var a AgentRecord
	var capStr, metStr, hbAt, enAt string
	err := s.readDB.ScanRaw(ctx, `SELECT id, project_id, host_id, hostname, ip_address, zone, os_arch, os_info, agent_version, status, capabilities_json, metrics_json, last_heartbeat_at, enrolled_at
		 FROM exec_agents WHERE auth_token_sha256 = ?`, []any{hashToken(tok)}, &a.ID, &a.ProjectID, &a.HostID, &a.Hostname, &a.IPAddress, &a.Zone, &a.OSArch, &a.OSInfo, &a.AgentVersion, &a.Status, &capStr, &metStr, &hbAt, &enAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, sdk.NewError(sdk.CodeAuthUnauthenticated, "EXEC", http.StatusUnauthorized, sdk.CodeAuthUnauthenticated, false, errors.New("unknown agent token"))
	}
	if err != nil {
		return nil, err
	}
	if a.Status == "REVOKED" {
		return nil, sdk.NewError(sdk.CodeAuthForbidden, "EXEC", http.StatusForbidden, sdk.CodeAuthForbidden, false, errors.New("agent certificate/token revoked"))
	}
	_ = json.Unmarshal([]byte(capStr), &a.Capabilities)
	_ = json.Unmarshal([]byte(metStr), &a.Metrics)
	a.LastHeartbeatAt, a.EnrolledAt = parseTS(hbAt), parseTS(enAt)
	return &a, nil
}

func (s *Service) RecordHeartbeat(ctx context.Context, agentID, ipAddr, osInfo string, metrics map[string]any) error {
	if metrics == nil {
		metrics = map[string]any{}
	}
	metB, _ := json.Marshal(metrics)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.writeDB.ExecRaw(ctx,
		`UPDATE exec_agents SET status = 'ONLINE', ip_address = CASE WHEN ? <> '' THEN ? ELSE ip_address END, os_info = CASE WHEN ? <> '' THEN ? ELSE os_info END, metrics_json = ?, last_heartbeat_at = ?
		 WHERE id = ? AND status <> 'REVOKED'`,
		ipAddr, ipAddr, osInfo, osInfo, string(metB), now, agentID,
	)
	return err
}

func (s *Service) ListAgents(ctx context.Context, projectID string) ([]AgentRecord, error) {
	rows, err := s.readDB.QueryRaw(ctx,
		`SELECT id, project_id, host_id, hostname, ip_address, zone, os_arch, os_info, agent_version, status, capabilities_json, metrics_json, last_heartbeat_at, enrolled_at
		 FROM exec_agents WHERE project_id = ? ORDER BY enrolled_at DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]AgentRecord, 0)
	cutoff := time.Now().UTC().Add(-45 * time.Second)
	for rows.Next() {
		var a AgentRecord
		var capStr, metStr, hbAt, enAt string
		if err := rows.Scan(&a.ID, &a.ProjectID, &a.HostID, &a.Hostname, &a.IPAddress, &a.Zone, &a.OSArch, &a.OSInfo, &a.AgentVersion, &a.Status, &capStr, &metStr, &hbAt, &enAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(capStr), &a.Capabilities)
		_ = json.Unmarshal([]byte(metStr), &a.Metrics)
		if a.Capabilities == nil {
			a.Capabilities = []string{}
		}
		if a.Metrics == nil {
			a.Metrics = map[string]any{}
		}
		a.LastHeartbeatAt, a.EnrolledAt = parseTS(hbAt), parseTS(enAt)
		if a.Status == "ONLINE" && a.LastHeartbeatAt.Before(cutoff) {
			a.Status = "OFFLINE"
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Service) RevokeAgent(ctx context.Context, projectID, agentID string) error {
	_, err := s.writeDB.ExecRaw(ctx,
		`UPDATE exec_agents SET status = 'REVOKED' WHERE project_id = ? AND id = ?`, projectID, agentID,
	)
	return err
}

// --- Outbound Task Queue for Agent Execution ---

func (s *Service) EnqueueTask(ctx context.Context, projectID, agentID, kind string, payload map[string]any) (*AgentTask, error) {
	if payload == nil {
		payload = map[string]any{}
	}
	pB, _ := json.Marshal(payload)
	now := time.Now().UTC()
	t := &AgentTask{
		ID:        domain.NewID("agtk"),
		ProjectID: projectID,
		AgentID:   agentID,
		Kind:      kind,
		Payload:   payload,
		Status:    "PENDING",
		Outputs:   map[string]any{},
		CreatedAt: now,
		UpdatedAt: now,
	}
	_, err := s.writeDB.ExecRaw(ctx,
		`INSERT INTO exec_agent_tasks (id, project_id, agent_id, kind, payload_json, status, exit_code, stdout, stderr, outputs_json, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, 'PENDING', 0, '', '', '{}', ?, ?)`,
		t.ID, projectID, agentID, kind, string(pB), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
	)
	if err != nil {
		return nil, err
	}
	return t, nil
}

func (s *Service) PollPendingTasks(ctx context.Context, agentID string) ([]AgentTask, error) {
	rows, err := s.readDB.QueryRaw(ctx,
		`SELECT id, project_id, agent_id, kind, payload_json, status, created_at, updated_at
		 FROM exec_agent_tasks WHERE agent_id = ? AND status = 'PENDING' ORDER BY created_at ASC LIMIT 10`, agentID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]AgentTask, 0)
	for rows.Next() {
		var t AgentTask
		var pStr, cAt, uAt string
		if err := rows.Scan(&t.ID, &t.ProjectID, &t.AgentID, &t.Kind, &pStr, &t.Status, &cAt, &uAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(pStr), &t.Payload)
		t.CreatedAt, t.UpdatedAt = parseTS(cAt), parseTS(uAt)
		out = append(out, t)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for i := range out {
		_, _ = s.writeDB.ExecRaw(ctx, `UPDATE exec_agent_tasks SET status = 'DISPATCHED', updated_at = ? WHERE id = ?`, now, out[i].ID)
		out[i].Status = "DISPATCHED"
	}
	return out, nil
}

func (s *Service) ReportTaskResult(ctx context.Context, agentID, taskID, status string, exitCode int, stdout, stderr string, outputs map[string]any) error {
	if outputs == nil {
		outputs = map[string]any{}
	}
	oB, _ := json.Marshal(outputs)
	if status != "SUCCEEDED" && status != "FAILED" {
		if exitCode == 0 {
			status = "SUCCEEDED"
		} else {
			status = "FAILED"
		}
	}
	_, err := s.writeDB.ExecRaw(ctx,
		`UPDATE exec_agent_tasks SET status = ?, exit_code = ?, stdout = ?, stderr = ?, outputs_json = ?, updated_at = ?
		 WHERE id = ? AND agent_id = ?`,
		status, exitCode, stdout, stderr, string(oB), time.Now().UTC().Format(time.RFC3339Nano), taskID, agentID,
	)
	return err
}

func (s *Service) WaitForTask(ctx context.Context, taskID string, timeout time.Duration) (*AgentTask, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var t AgentTask
		var pStr, oStr, cAt, uAt string
		err := s.readDB.ScanRaw(ctx, `SELECT id, project_id, agent_id, kind, payload_json, status, exit_code, stdout, stderr, outputs_json, created_at, updated_at
			 FROM exec_agent_tasks WHERE id = ?`, []any{taskID}, &t.ID, &t.ProjectID, &t.AgentID, &t.Kind, &pStr, &t.Status, &t.ExitCode, &t.Stdout, &t.Stderr, &oStr, &cAt, &uAt)
		if err != nil {
			return nil, err
		}
		if t.Status == "SUCCEEDED" || t.Status == "FAILED" || t.Status == "CANCELLED" {
			_ = json.Unmarshal([]byte(pStr), &t.Payload)
			_ = json.Unmarshal([]byte(oStr), &t.Outputs)
			t.CreatedAt, t.UpdatedAt = parseTS(cAt), parseTS(uAt)
			return &t, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(150 * time.Millisecond):
		}
	}
	return nil, fmt.Errorf("agent task %s timed out waiting for result", taskID)
}

// --- CIDR-Guarded Pure-Go TCP Connectivity Prober (LiveOps 03.7 §3.4) ---

// RunTCPProbes executes pure-Go net.Dialer TCP connectivity probes either via a target online Agent or directly from control-plane.
// Explicitly blocks cloud metadata / link-local ranges (169.254.0.0/16).
func (s *Service) RunTCPProbes(ctx context.Context, projectID, agentID string, targets []TCPProbeTarget, timeoutSec int) ([]TCPProbeResult, error) {
	if len(targets) == 0 {
		return nil, sdk.NewError(sdk.CodeAPIInvalidRequest, "EXEC", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, errors.New("至少指定 1 个探测目标"))
	}
	if len(targets) > 200 {
		return nil, sdk.NewError(sdk.CodeAPIInvalidRequest, "EXEC", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, errors.New("单批探测目标上限为 200 个"))
	}
	if timeoutSec <= 0 || timeoutSec > 15 {
		timeoutSec = 3
	}

	// If an online Agent is selected, delegate real TCP probe execution to that Agent!
	if agentID != "" {
		task, err := s.EnqueueTask(ctx, projectID, agentID, "probe_tcp", map[string]any{
			"targets":    targets,
			"timeoutSec": timeoutSec,
		})
		if err != nil {
			return nil, err
		}
		done, err := s.WaitForTask(ctx, task.ID, time.Duration(timeoutSec*len(targets)+10)*time.Second)
		if err != nil {
			return nil, err
		}
		raw, _ := json.Marshal(done.Outputs["results"])
		var res []TCPProbeResult
		if err := json.Unmarshal(raw, &res); err == nil && len(res) > 0 {
			return res, nil
		}
	}

	return ExecuteLocalTCPProbes(ctx, targets, timeoutSec, "control-plane"), nil
}

// ExecuteLocalTCPProbes runs pure-Go net.Dialer TCP handshake probes with CIDR guardrail.
func ExecuteLocalTCPProbes(ctx context.Context, targets []TCPProbeTarget, timeoutSec int, executorLabel string) []TCPProbeResult {
	out := make([]TCPProbeResult, 0, len(targets))
	dialer := &net.Dialer{Timeout: time.Duration(timeoutSec) * time.Second}
	for _, t := range targets {
		host := strings.TrimSpace(t.Host)
		label := net.JoinHostPort(host, strconv.Itoa(t.Port))
		if host == "" || t.Port < 1 || t.Port > 65535 {
			out = append(out, TCPProbeResult{Target: label, Status: "BLOCKED", Error: "非法主机或端口", ExecutedBy: executorLabel})
			continue
		}
		if ip, err := netip.ParseAddr(host); err == nil {
			if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
				out = append(out, TCPProbeResult{Target: label, Status: "BLOCKED", Error: "CIDR 安全护栏拦截云元数据/链路本地网段 (169.254.0.0/16)", ExecutedBy: executorLabel})
				continue
			}
		}
		start := time.Now()
		conn, err := dialer.DialContext(ctx, "tcp", label)
		lat := time.Since(start).Milliseconds()
		if err != nil {
			st := "CONN_REFUSED"
			if strings.Contains(strings.ToLower(err.Error()), "timeout") || strings.Contains(strings.ToLower(err.Error()), "deadline") {
				st = "TIMEOUT"
			}
			out = append(out, TCPProbeResult{Target: label, Status: st, LatencyMs: lat, Error: err.Error(), ExecutedBy: executorLabel})
			continue
		}
		_ = conn.Close()
		out = append(out, TCPProbeResult{Target: label, Status: "OK", LatencyMs: lat, ExecutedBy: executorLabel})
	}
	return out
}

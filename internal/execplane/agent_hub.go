package execplane

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/kinstall/kinstall/internal/cmdb"
	"github.com/kinstall/kinstall/pkg/platform/crypto"
	"github.com/kinstall/kinstall/pkg/platform/db"
	"github.com/kinstall/kinstall/pkg/platform/id"
)

type EnrollToken struct {
	ID            string `json:"id"`
	WorkspaceID   string `json:"workspaceId"`
	Label         string `json:"label"`
	Zone          string `json:"zone"`
	ExpiresAt     string `json:"expiresAt"`
	UsedAt        string `json:"usedAt"`
	UsedByAgentID string `json:"usedByAgentId"`
	CreatedBy     string `json:"createdBy"`
	CreatedAt     string `json:"createdAt"`
	PlainToken    string `json:"plainToken,omitempty"` // returned ONCE on generation
	InstallCmd    string `json:"installCmd,omitempty"`
}

type Agent struct {
	ID              string  `json:"id"`
	WorkspaceID     string  `json:"workspaceId"`
	HostCIID        string  `json:"hostCiId"`
	Hostname        string  `json:"hostname"`
	IP              string  `json:"ip"`
	Zone            string  `json:"zone"`
	OS              string  `json:"os"`
	Arch            string  `json:"arch"`
	Version         string  `json:"version"`
	Status          string  `json:"status"` // ONLINE | OFFLINE | REVOKED
	CPUCores        int     `json:"cpuCores"`
	MemMB           int     `json:"memMb"`
	LoadAvg         string  `json:"loadAvg"`
	CPUUsagePct     float64 `json:"cpuUsagePct"`
	MemUsagePct     float64 `json:"memUsagePct"`
	LastHeartbeatAt string  `json:"lastHeartbeatAt"`
	EnrolledAt      string  `json:"enrolledAt"`
}

type AgentEnrollRequest struct {
	EnrollToken string `json:"enrollToken"`
	Hostname    string `json:"hostname"`
	IP          string `json:"ip"`
	Zone        string `json:"zone"`
	OS          string `json:"os"`
	Arch        string `json:"arch"`
	Version     string `json:"version"`
	CPUCores    int    `json:"cpuCores"`
	MemMB       int    `json:"memMb"`
}

type AgentEnrollResponse struct {
	AgentID     string `json:"agentId"`
	HostCIID    string `json:"hostCiId"`
	AgentToken  string `json:"agentToken"`
	WorkspaceID string `json:"workspaceId"`
	Zone        string `json:"zone"`
}

type AgentHeartbeatRequest struct {
	IP          string  `json:"ip"`
	CPUCores    int     `json:"cpuCores"`
	MemMB       int     `json:"memMb"`
	LoadAvg     string  `json:"loadAvg"`
	CPUUsagePct float64 `json:"cpuUsagePct"`
	MemUsagePct float64 `json:"memUsagePct"`
}

type AgentTask struct {
	TaskID    string `json:"taskId"`
	Kind      string `json:"kind"` // shell | collect_facts | tcp_probe
	Command   string `json:"command,omitempty"`
	CIDR      string `json:"cidr,omitempty"`
	Ports     []int  `json:"ports,omitempty"`
	TimeoutMs int    `json:"timeoutMs,omitempty"`
}

type AgentTaskResult struct {
	TaskID     string           `json:"taskId"`
	ExitCode   int              `json:"exitCode"`
	Output     string           `json:"output"`
	Error      string           `json:"error,omitempty"`
	ProbeItems []TCPProbeResult `json:"probeItems,omitempty"`
	DurationMs int64            `json:"durationMs"`
}

type AgentHub struct {
	store   *db.Store
	cmdbSvc *cmdb.Service

	mu      sync.Mutex
	queues  map[string]chan AgentTask
	waiters map[string]chan AgentTaskResult
}

func NewAgentHub(store *db.Store, cmdbSvc *cmdb.Service) *AgentHub {
	return &AgentHub{
		store:   store,
		cmdbSvc: cmdbSvc,
		queues:  make(map[string]chan AgentTask),
		waiters: make(map[string]chan AgentTaskResult),
	}
}

func nowISO() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func (h *AgentHub) CreateEnrollToken(ctx context.Context, wsID, actor, label, zone, serverURL string, ttlMin int) (*EnrollToken, error) {
	if ttlMin <= 0 || ttlMin > 1440 {
		ttlMin = 15
	}
	if strings.TrimSpace(zone) == "" {
		zone = "prod-core"
	}
	if strings.TrimSpace(label) == "" {
		label = "Edge Agent Enroll"
	}
	randPart, err := crypto.RandomToken(24)
	if err != nil {
		return nil, err
	}
	plain := "ki_enr_" + randPart
	hash := crypto.SHA256Hex([]byte(plain))
	now := time.Now().UTC()
	exp := now.Add(time.Duration(ttlMin) * time.Minute).Format(time.RFC3339)

	tok := &EnrollToken{
		ID:          id.New("enr"),
		WorkspaceID: wsID,
		Label:       label,
		Zone:        zone,
		ExpiresAt:   exp,
		CreatedBy:   actor,
		CreatedAt:   now.Format(time.RFC3339),
		PlainToken:  plain,
		InstallCmd:  fmt.Sprintf("./bin/kinstall agent --server %s --token %s --zone %s", serverURL, plain, zone),
	}
	_, err = h.store.Exec(ctx,
		`INSERT INTO exec_enroll_tokens(id, workspace_id, label, zone, token_hash, expires_at, used_at, used_by_agent_id, created_by, created_at)
		 VALUES(?,?,?,?,?,?,'','',?,?)`,
		tok.ID, wsID, tok.Label, tok.Zone, hash, tok.ExpiresAt, actor, tok.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return tok, nil
}

func (h *AgentHub) ListEnrollTokens(ctx context.Context, wsID string) ([]EnrollToken, error) {
	rows, err := h.store.Query(ctx,
		`SELECT id, workspace_id, label, zone, expires_at, used_at, used_by_agent_id, created_by, created_at
		 FROM exec_enroll_tokens WHERE workspace_id = ? ORDER BY created_at DESC LIMIT 30`,
		wsID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]EnrollToken, 0)
	for rows.Next() {
		var t EnrollToken
		if err := rows.Scan(&t.ID, &t.WorkspaceID, &t.Label, &t.Zone, &t.ExpiresAt, &t.UsedAt, &t.UsedByAgentID, &t.CreatedBy, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// EnrollAgent consumes a one-time enroll token, issues a permanent agent bearer token,
// and auto-upserts the unified CMDB m_host CI + cmdb_host_endpoints!
func (h *AgentHub) EnrollAgent(ctx context.Context, req AgentEnrollRequest) (*AgentEnrollResponse, error) {
	if strings.TrimSpace(req.EnrollToken) == "" {
		return nil, errors.New("缺少一次性纳管令牌")
	}
	enrHash := crypto.SHA256Hex([]byte(strings.TrimSpace(req.EnrollToken)))
	var (
		enrID, wsID, enrZone, expiresAt, usedAt string
	)
	err := h.store.QueryRow(ctx,
		`SELECT id, workspace_id, zone, expires_at, used_at FROM exec_enroll_tokens WHERE token_hash = ?`,
		enrHash,
	).Scan(&enrID, &wsID, &enrZone, &expiresAt, &usedAt)
	if err != nil {
		return nil, errors.New("纳管令牌无效")
	}
	if usedAt != "" {
		return nil, errors.New("一次性纳管令牌已被使用")
	}
	if exp, perr := time.Parse(time.RFC3339, expiresAt); perr == nil && time.Now().UTC().After(exp) {
		return nil, errors.New("纳管令牌已过期")
	}

	hostname := strings.TrimSpace(req.Hostname)
	if hostname == "" {
		hostname = "edge-node-" + id.Raw()[:6]
	}
	ip := strings.TrimSpace(req.IP)
	if ip == "" {
		ip = "127.0.0.1"
	}
	zone := strings.TrimSpace(req.Zone)
	if zone == "" {
		zone = enrZone
	}
	arch := strings.TrimSpace(req.Arch)
	if arch == "" {
		arch = "amd64"
	}
	osName := strings.TrimSpace(req.OS)
	if osName == "" {
		osName = "linux"
	}
	ver := strings.TrimSpace(req.Version)
	if ver == "" {
		ver = "2.0.0"
	}

	randPart, err := crypto.RandomToken(32)
	if err != nil {
		return nil, err
	}
	agentToken := "ki_agt_" + randPart
	agentTokenHash := crypto.SHA256Hex([]byte(agentToken))
	agentID := id.New("agt")
	now := nowISO()

	// Auto-create/link Unified CMDB Host CI (SSOT!)
	hostCIID := ""
	if h.cmdbSvc != nil {
		memGB := max(1, req.MemMB/1024)
		ci, cerr := h.cmdbSvc.UpsertHostCI(ctx, wsID, "agent-enroll", cmdb.UpsertCIRequest{
			ModelCode: "m_host",
			UniqueKey: ip,
			Name:      hostname,
			Env:       "prod",
			Status:    "active",
			Owner:     "Agent 自动纳管",
			Tags:      []string{"agent-enrolled", zone},
			Data: map[string]any{
				"ip": ip, "zone": zone, "os": osName, "arch": arch,
				"cpu_cores": req.CPUCores, "mem_gb": memGB,
			},
			Host: &cmdb.HostEndpoint{
				IP: ip, SSHPort: 22, SSHUser: "root", ExecMode: "auto",
				AgentID: agentID, AgentStatus: "ONLINE",
				OSFamily: osName, Arch: arch, CPUCores: req.CPUCores, MemMB: req.MemMB,
				LastSeenAt: now,
			},
		})
		if cerr == nil && ci != nil {
			hostCIID = ci.ID
		}
	}

	err = h.store.WithTx(ctx, func(ctx context.Context, tx *db.Tx) error {
		res, err := tx.Exec(ctx,
			`UPDATE exec_enroll_tokens SET used_at = ?, used_by_agent_id = ? WHERE id = ? AND used_at = ''`,
			now, agentID, enrID,
		)
		if err != nil {
			return err
		}
		if aff, _ := res.RowsAffected(); aff == 0 {
			return errors.New("纳管令牌已被并发消耗")
		}
		_, err = tx.Exec(ctx,
			`INSERT INTO exec_agents(id, workspace_id, host_ci_id, hostname, ip, zone, os, arch, version, token_hash, status, cpu_cores, mem_mb, load_avg, cpu_usage_pct, mem_usage_pct, last_heartbeat_at, enrolled_at)
			 VALUES(?,?,?,?,?,?,?,?,?,?,'ONLINE',?,?,'',0,0,?,?)`,
			agentID, wsID, hostCIID, hostname, ip, zone, osName, arch, ver, agentTokenHash, req.CPUCores, req.MemMB, now, now,
		)
		return err
	})
	if err != nil {
		return nil, err
	}

	return &AgentEnrollResponse{
		AgentID:     agentID,
		HostCIID:    hostCIID,
		AgentToken:  agentToken,
		WorkspaceID: wsID,
		Zone:        zone,
	}, nil
}

func (h *AgentHub) AuthenticateAgent(ctx context.Context, rawToken string) (*Agent, error) {
	hash := crypto.SHA256Hex([]byte(strings.TrimSpace(rawToken)))
	var a Agent
	err := h.store.QueryRow(ctx,
		`SELECT id, workspace_id, host_ci_id, hostname, ip, zone, os, arch, version, status, cpu_cores, mem_mb, load_avg, cpu_usage_pct, mem_usage_pct, last_heartbeat_at, enrolled_at
		 FROM exec_agents WHERE token_hash = ?`,
		hash,
	).Scan(&a.ID, &a.WorkspaceID, &a.HostCIID, &a.Hostname, &a.IP, &a.Zone, &a.OS, &a.Arch, &a.Version, &a.Status, &a.CPUCores, &a.MemMB, &a.LoadAvg, &a.CPUUsagePct, &a.MemUsagePct, &a.LastHeartbeatAt, &a.EnrolledAt)
	if err != nil {
		return nil, errors.New("invalid agent token")
	}
	if a.Status == "REVOKED" {
		return nil, errors.New("agent token has been revoked")
	}
	return &a, nil
}

func (h *AgentHub) Heartbeat(ctx context.Context, a *Agent, req AgentHeartbeatRequest) error {
	now := nowISO()
	ip := req.IP
	if ip == "" {
		ip = a.IP
	}
	if _, err := h.store.Exec(ctx,
		`UPDATE exec_agents SET status='ONLINE', ip=?, cpu_cores=CASE WHEN ?>0 THEN ? ELSE cpu_cores END, mem_mb=CASE WHEN ?>0 THEN ? ELSE mem_mb END,
		 load_avg=?, cpu_usage_pct=?, mem_usage_pct=?, last_heartbeat_at=? WHERE id=? AND status<>'REVOKED'`,
		ip, req.CPUCores, req.CPUCores, req.MemMB, req.MemMB, req.LoadAvg, req.CPUUsagePct, req.MemUsagePct, now, a.ID,
	); err != nil {
		return err
	}
	if h.cmdbSvc != nil && a.HostCIID != "" {
		_ = h.cmdbSvc.UpdateHostTelemetry(ctx, a.WorkspaceID, a.HostCIID, cmdb.HostEndpoint{
			AgentID:     a.ID,
			AgentStatus: "ONLINE",
			CPUCores:    req.CPUCores,
			MemMB:       req.MemMB,
			LoadAvg:     req.LoadAvg,
			CPUUsagePct: req.CPUUsagePct,
			MemUsagePct: req.MemUsagePct,
		})
	}
	return nil
}

func (h *AgentHub) ListAgents(ctx context.Context, wsID string) ([]Agent, error) {
	rows, err := h.store.Query(ctx,
		`SELECT id, workspace_id, host_ci_id, hostname, ip, zone, os, arch, version, status, cpu_cores, mem_mb, load_avg, cpu_usage_pct, mem_usage_pct, last_heartbeat_at, enrolled_at
		 FROM exec_agents WHERE workspace_id = ? ORDER BY enrolled_at DESC`,
		wsID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]Agent, 0)
	cutoff := time.Now().UTC().Add(-30 * time.Second)
	for rows.Next() {
		var a Agent
		if err := rows.Scan(&a.ID, &a.WorkspaceID, &a.HostCIID, &a.Hostname, &a.IP, &a.Zone, &a.OS, &a.Arch, &a.Version, &a.Status, &a.CPUCores, &a.MemMB, &a.LoadAvg, &a.CPUUsagePct, &a.MemUsagePct, &a.LastHeartbeatAt, &a.EnrolledAt); err != nil {
			return nil, err
		}
		if a.Status == "ONLINE" {
			if t, perr := time.Parse(time.RFC3339, a.LastHeartbeatAt); perr == nil && t.Before(cutoff) {
				a.Status = "OFFLINE"
			}
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (h *AgentHub) IsAgentOnline(ctx context.Context, agentID string) bool {
	if agentID == "" {
		return false
	}
	var status, lastHB string
	if err := h.store.QueryRow(ctx, `SELECT status, last_heartbeat_at FROM exec_agents WHERE id = ?`, agentID).Scan(&status, &lastHB); err != nil {
		return false
	}
	if status != "ONLINE" {
		return false
	}
	t, err := time.Parse(time.RFC3339, lastHB)
	return err == nil && time.Since(t) < 30*time.Second
}

func (h *AgentHub) RevokeAgent(ctx context.Context, wsID, agentID string) error {
	var hostCIID string
	_ = h.store.QueryRow(ctx, `SELECT host_ci_id FROM exec_agents WHERE workspace_id = ? AND id = ?`, wsID, agentID).Scan(&hostCIID)
	if _, err := h.store.Exec(ctx, `UPDATE exec_agents SET status = 'REVOKED' WHERE workspace_id = ? AND id = ?`, wsID, agentID); err != nil {
		return err
	}
	if h.cmdbSvc != nil && hostCIID != "" {
		_ = h.cmdbSvc.UpdateHostTelemetry(ctx, wsID, hostCIID, cmdb.HostEndpoint{
			AgentID:     agentID,
			AgentStatus: "REVOKED",
		})
	}
	return nil
}

func (h *AgentHub) queueFor(agentID string) chan AgentTask {
	h.mu.Lock()
	defer h.mu.Unlock()
	ch, ok := h.queues[agentID]
	if !ok {
		ch = make(chan AgentTask, 32)
		h.queues[agentID] = ch
	}
	return ch
}

// DispatchTask sends a task to a connected outbound Agent and waits synchronously for its result.
func (h *AgentHub) DispatchTask(ctx context.Context, agentID string, task AgentTask) (*AgentTaskResult, error) {
	if task.TaskID == "" {
		task.TaskID = id.New("tsk")
	}
	waitCh := make(chan AgentTaskResult, 1)
	h.mu.Lock()
	h.waiters[task.TaskID] = waitCh
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.waiters, task.TaskID)
		h.mu.Unlock()
	}()

	q := h.queueFor(agentID)
	select {
	case q <- task:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(5 * time.Second):
		return nil, errors.New("Agent 任务队列已满")
	}

	select {
	case res := <-waitCh:
		return &res, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(60 * time.Second):
		return nil, errors.New("等待 Agent 返回执行结果超时")
	}
}

// PollTask long-polls for the next task assigned to agentID.
func (h *AgentHub) PollTask(ctx context.Context, agentID string, wait time.Duration) (*AgentTask, error) {
	if wait <= 0 || wait > 20*time.Second {
		wait = 10 * time.Second
	}
	q := h.queueFor(agentID)
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case t := <-q:
		return &t, nil
	case <-timer.C:
		return nil, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// SubmitTaskResult delivers a completed task payload from an Agent back to its waiting caller.
func (h *AgentHub) SubmitTaskResult(res AgentTaskResult) error {
	h.mu.Lock()
	ch, ok := h.waiters[res.TaskID]
	h.mu.Unlock()
	if !ok {
		return nil
	}
	select {
	case ch <- res:
	default:
	}
	return nil
}

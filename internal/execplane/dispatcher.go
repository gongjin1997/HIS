package execplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/kinstall/kinstall/internal/catalog"
	"github.com/kinstall/kinstall/internal/cmdb"
	"github.com/kinstall/kinstall/internal/iam"
	"github.com/kinstall/kinstall/pkg/platform/db"
	"github.com/kinstall/kinstall/pkg/platform/eventbus"
	"github.com/kinstall/kinstall/pkg/platform/id"
	"github.com/kinstall/kinstall/pkg/platform/safety"
)

type JobTemplate struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Category    string `json:"category"`
	Description string `json:"description"`
	Command     string `json:"command"`
}

type RunJobRequest struct {
	Name           string   `json:"name"`
	TemplateCode   string   `json:"templateCode"`
	Command        string   `json:"command"`
	ArtifactID     string   `json:"artifactId"`
	RemoteDest     string   `json:"remoteDest"`
	TargetMode     string   `json:"targetMode"` // hosts | dynamic_group
	HostCIIDs      []string `json:"hostCiIds"`
	DynamicGroupID string   `json:"dynamicGroupId"`
}

type Job struct {
	ID           string   `json:"id"`
	WorkspaceID  string   `json:"workspaceId"`
	Name         string   `json:"name"`
	TemplateCode string   `json:"templateCode"`
	Command      string   `json:"command"`
	ArtifactID   string   `json:"artifactId"`
	RemoteDest   string   `json:"remoteDest"`
	TargetMode   string   `json:"targetMode"`
	TargetRefs   []string `json:"targetRefs"`
	Status       string   `json:"status"` // RUNNING | SUCCEEDED | PARTIAL | FAILED
	CreatedBy    string   `json:"createdBy"`
	CreatedAt    string   `json:"createdAt"`
	FinishedAt   string   `json:"finishedAt"`
	Runs         []JobRun `json:"runs,omitempty"`
}

type JobRun struct {
	ID         string `json:"id"`
	JobID      string `json:"jobId"`
	HostCIID   string `json:"hostCiId"`
	HostName   string `json:"hostName"`
	HostIP     string `json:"hostIp"`
	Transport  string `json:"transport"` // agent | ssh
	Status     string `json:"status"`
	ExitCode   int    `json:"exitCode"`
	Output     string `json:"output"`
	DurationMs int64  `json:"durationMs"`
	StartedAt  string `json:"startedAt"`
	FinishedAt string `json:"finishedAt"`
}

type Service struct {
	store      *db.Store
	cmdbSvc    *cmdb.Service
	iamSvc     *iam.Service
	catalogSvc *catalog.Service
	broker     *eventbus.LogBroker
	Hub        *AgentHub
}

func NewService(store *db.Store, cmdbSvc *cmdb.Service, iamSvc *iam.Service, catalogSvc *catalog.Service, broker *eventbus.LogBroker) *Service {
	return &Service{
		store:      store,
		cmdbSvc:    cmdbSvc,
		iamSvc:     iamSvc,
		catalogSvc: catalogSvc,
		broker:     broker,
		Hub:        NewAgentHub(store, cmdbSvc),
	}
}

func (s *Service) ListJobTemplates() []JobTemplate {
	return []JobTemplate{
		{
			Code:        "baseline_inspect",
			Name:        "系统基线与内核巡检",
			Category:    "巡检诊断",
			Description: "检查内核版本、运行时间、CPU 负载与内存水位",
			Command:     "uname -a && uptime && free -h",
		},
		{
			Code:        "disk_inode_audit",
			Name:        "磁盘空间与 Inode 检查",
			Category:    "巡检诊断",
			Description: "排查根分区与数据盘容量及 Inode 耗尽风险",
			Command:     "df -hT && echo '--- INODES ---' && df -ih",
		},
		{
			Code:        "listen_ports_snap",
			Name:        "监听端口与进程快照",
			Category:    "安全基线",
			Description: "采集主机全部 TCP 监听端口及关联服务进程",
			Command:     "ss -tlnp 2>/dev/null || netstat -tlnp 2>/dev/null || true",
		},
		{
			Code:        "container_health",
			Name:        "容器运行时与实例健康检查",
			Category:    "容器运维",
			Description: "查看 Docker/Containerd 运行状态及最近重启容器",
			Command:     "docker ps --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}' 2>/dev/null || echo 'docker not installed'",
		},
		{
			Code:        "systemd_failed",
			Name:        "Systemd 异常失败单元巡检",
			Category:    "故障排查",
			Description: "列出当前主机全部处于 failed 状态的系统服务",
			Command:     "systemctl --failed --no-pager 2>/dev/null || echo 'systemd unavailable'",
		},
		{
			Code:        "ntp_clock_sync",
			Name:        "时钟同步与时区校验",
			Category:    "安全基线",
			Description: "校验生产集群节点时钟偏移与 NTP 状态",
			Command:     "timedatectl status 2>/dev/null || date -u",
		},
	}
}

// ResolveSSHTarget builds runtime SSH credentials for a CMDB m_host CI.
func (s *Service) ResolveSSHTarget(ctx context.Context, wsID, hostCIID string) (*cmdb.CIInstance, SSHTarget, error) {
	ci, err := s.cmdbSvc.GetCI(ctx, wsID, hostCIID)
	if err != nil {
		return nil, SSHTarget{}, fmt.Errorf("未找到主机 CI (%s): %w", hostCIID, err)
	}
	if ci.Host == nil || ci.Host.IP == "" {
		return nil, SSHTarget{}, errors.New("该资产未配置主机连接端点")
	}
	target := SSHTarget{
		Host: ci.Host.IP,
		Port: ci.Host.SSHPort,
		User: ci.Host.SSHUser,
	}
	if ci.Host.CredentialID != "" && s.iamSvc != nil {
		cred, cerr := s.iamSvc.ResolveCredential(ctx, wsID, ci.Host.CredentialID)
		if cerr != nil {
			return ci, target, fmt.Errorf("解析主机绑定凭据失败: %w", cerr)
		}
		if target.User == "" && cred.Username != "" {
			target.User = cred.Username
		}
		if cred.Kind == "ssh_key" {
			target.PrivateKey = cred.Secret
			target.Passphrase = cred.Passphrase
		} else {
			target.Password = cred.Secret
		}
	}
	return ci, target, nil
}

// ExecuteOnHost transparently routes command execution to an ONLINE Outbound Agent or falls back to Agentless SSH.
func (s *Service) ExecuteOnHost(ctx context.Context, wsID, hostCIID, command, logTopic string) (transport string, exitCode int, output string, err error) {
	if err := safety.CheckCommand(command); err != nil {
		return "", 126, "", err
	}
	ci, target, err := s.ResolveSSHTarget(ctx, wsID, hostCIID)
	if err != nil && ci == nil {
		return "", -1, "", err
	}

	// Prefer ONLINE Outbound Go Agent unless host explicitly forces "ssh".
	if ci != nil && ci.Host != nil && ci.Host.ExecMode != "ssh" && ci.Host.AgentID != "" && s.Hub.IsAgentOnline(ctx, ci.Host.AgentID) {
		if logTopic != "" {
			s.broker.Publish(logTopic, "system", "info", fmt.Sprintf("[%s] 通过在线出站 Agent (%s) 派发命令...", ci.Name, ci.Host.AgentID))
		}
		res, derr := s.Hub.DispatchTask(ctx, ci.Host.AgentID, AgentTask{
			Kind:      "shell",
			Command:   command,
			TimeoutMs: 45000,
		})
		if derr != nil {
			return "agent", -1, "", derr
		}
		if logTopic != "" {
			for _, line := range strings.Split(strings.TrimRight(res.Output, "\n"), "\n") {
				if line != "" {
					s.broker.Publish(logTopic, "stdout", "info", fmt.Sprintf("[%s] %s", ci.Name, line))
				}
			}
		}
		if res.Error != "" {
			return "agent", res.ExitCode, res.Output, errors.New(res.Error)
		}
		return "agent", res.ExitCode, res.Output, nil
	}

	// Fallback to Agentless SSH.
	if err != nil {
		return "ssh", -1, "", err
	}
	if logTopic != "" {
		s.broker.Publish(logTopic, "system", "info", fmt.Sprintf("[%s] 通过无代理 SSH (%s@%s:%d) 执行...", ci.Name, target.User, target.Host, target.Port))
	}
	code, out, serr := RunSSHCommand(ctx, target, command, func(stream, line string) {
		if logTopic != "" {
			lvl := "info"
			if stream == "stderr" {
				lvl = "warn"
			}
			s.broker.Publish(logTopic, stream, lvl, fmt.Sprintf("[%s] %s", ci.Name, line))
		}
	})
	return "ssh", code, out, serr
}

// CollectHostFacts executes the whitelist read-only collector on hostCIID (via Agent or SSH)
// and updates cmdb_host_endpoints + m_host CI data automatically.
func (s *Service) CollectHostFacts(ctx context.Context, wsID, hostCIID string) (*cmdb.CIInstance, error) {
	ci, target, err := s.ResolveSSHTarget(ctx, wsID, hostCIID)
	if err != nil && ci == nil {
		return nil, err
	}
	var rawOut string
	if ci != nil && ci.Host != nil && ci.Host.AgentID != "" && s.Hub.IsAgentOnline(ctx, ci.Host.AgentID) {
		res, derr := s.Hub.DispatchTask(ctx, ci.Host.AgentID, AgentTask{Kind: "collect_facts"})
		if derr != nil {
			return nil, derr
		}
		if res.Error != "" {
			return nil, errors.New(res.Error)
		}
		rawOut = res.Output
	} else {
		if err != nil {
			return nil, err
		}
		_, out, serr := RunSSHCommand(ctx, target, CollectLinuxFactsScript, nil)
		if serr != nil {
			return nil, serr
		}
		rawOut = out
	}

	ep := ParseFactsOutput(rawOut)
	if err := s.cmdbSvc.UpdateHostTelemetry(ctx, wsID, hostCIID, ep); err != nil {
		return nil, err
	}
	// Sync discovered hardware fields into m_host CI attributes too.
	updated, err := s.cmdbSvc.GetCI(ctx, wsID, hostCIID)
	if err != nil {
		return nil, err
	}
	if updated.Data == nil {
		updated.Data = make(map[string]any)
	}
	if ep.OSFamily != "" {
		updated.Data["os"] = strings.TrimSpace(ep.OSFamily + " " + ep.OSVersion)
	}
	if ep.Arch != "" {
		updated.Data["arch"] = ep.Arch
	}
	if ep.CPUCores > 0 {
		updated.Data["cpu_cores"] = ep.CPUCores
	}
	if ep.MemMB > 0 {
		updated.Data["mem_gb"] = max(1, ep.MemMB/1024)
	}
	return s.cmdbSvc.UpdateCI(ctx, wsID, hostCIID, "whitelist-collector", cmdb.UpsertCIRequest{
		Name:   updated.Name,
		Env:    updated.Env,
		Status: updated.Status,
		Owner:  updated.Owner,
		Tags:   updated.Tags,
		Data:   updated.Data,
	})
}

// RunTCPProbe executes CIDR TCP scanning locally or delegates it to an ONLINE Agent in a remote zone!
func (s *Service) RunTCPProbe(ctx context.Context, wsID, cidr string, ports []int, agentID string, timeoutMs int) ([]TCPProbeResult, string, error) {
	if agentID != "" {
		if !s.Hub.IsAgentOnline(ctx, agentID) {
			return nil, "", errors.New("指定的远端 Agent 当前离线，无法委派扫描")
		}
		res, err := s.Hub.DispatchTask(ctx, agentID, AgentTask{
			Kind:      "tcp_probe",
			CIDR:      cidr,
			Ports:     ports,
			TimeoutMs: timeoutMs,
		})
		if err != nil {
			return nil, "", err
		}
		if res.Error != "" {
			return nil, "", errors.New(res.Error)
		}
		return res.ProbeItems, "agent:" + agentID, nil
	}
	items, err := RunLocalTCPProbe(ctx, cidr, ports, timeoutMs)
	return items, "control-plane", err
}

// RunBatchJob creates and executes a concurrent batch operations job over CMDB Hosts or Dynamic Groups.
func (s *Service) RunBatchJob(ctx context.Context, wsID, actor string, req RunJobRequest) (*Job, error) {
	if err := safety.CheckCommand(req.Command); err != nil {
		return nil, err
	}
	targetMode := req.TargetMode
	if targetMode == "" {
		targetMode = "hosts"
	}

	var (
		hosts   []cmdb.CIInstance
		refList []string
	)
	if targetMode == "dynamic_group" && req.DynamicGroupID != "" {
		matched, err := s.cmdbSvc.EvaluateDynamicGroup(ctx, wsID, req.DynamicGroupID)
		if err != nil {
			return nil, fmt.Errorf("解析动态分组失败: %w", err)
		}
		hosts = matched
		refList = []string{req.DynamicGroupID}
	} else {
		for _, hid := range req.HostCIIDs {
			ci, err := s.cmdbSvc.GetCI(ctx, wsID, hid)
			if err == nil && ci != nil {
				hosts = append(hosts, *ci)
				refList = append(refList, hid)
			}
		}
	}
	if len(hosts) == 0 {
		return nil, errors.New("未匹配到任何目标主机资产")
	}

	if strings.TrimSpace(req.Name) == "" {
		req.Name = "批量运维作业 " + time.Now().Format("15:04:05")
	}
	now := nowISO()
	job := &Job{
		ID:           id.New("job"),
		WorkspaceID:  wsID,
		Name:         req.Name,
		TemplateCode: req.TemplateCode,
		Command:      req.Command,
		ArtifactID:   req.ArtifactID,
		RemoteDest:   req.RemoteDest,
		TargetMode:   targetMode,
		TargetRefs:   refList,
		Status:       "RUNNING",
		CreatedBy:    actor,
		CreatedAt:    now,
		Runs:         make([]JobRun, 0, len(hosts)),
	}
	refsJSON, _ := json.Marshal(refList)
	if _, err := s.store.Exec(ctx,
		`INSERT INTO exec_jobs(id, workspace_id, name, template_code, command, artifact_id, remote_dest, target_mode, target_ref_json, status, created_by, created_at, finished_at)
		 VALUES(?,?,?,?,?,?,?,?,?, 'RUNNING', ?, ?, '')`,
		job.ID, wsID, job.Name, job.TemplateCode, job.Command, job.ArtifactID, job.RemoteDest, job.TargetMode, string(refsJSON), actor, now,
	); err != nil {
		return nil, err
	}

	for _, h := range hosts {
		ip := ""
		if h.Host != nil {
			ip = h.Host.IP
		}
		run := JobRun{
			ID:        id.New("jrun"),
			JobID:     job.ID,
			HostCIID:  h.ID,
			HostName:  h.Name,
			HostIP:    ip,
			Transport: "auto",
			Status:    "RUNNING",
			StartedAt: now,
		}
		_, _ = s.store.Exec(ctx,
			`INSERT INTO exec_job_runs(id, job_id, host_ci_id, host_name, host_ip, transport, status, exit_code, output, duration_ms, started_at, finished_at)
			 VALUES(?,?,?,?,?,?, 'RUNNING', 0, '', 0, ?, '')`,
			run.ID, job.ID, run.HostCIID, run.HostName, run.HostIP, run.Transport, now,
		)
		job.Runs = append(job.Runs, run)
	}

	// Execute asynchronously across targets with real-time SSE LogBroker streaming.
	go s.executeJobAsync(wsID, *job) // #nosec G118 -- async batch job outlives HTTP request lifecycle
	return job, nil
}

func (s *Service) executeJobAsync(wsID string, job Job) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	topic := "job:" + job.ID
	s.broker.Publish(topic, "system", "info", fmt.Sprintf("开始执行批量作业 %q（共 %d 台目标主机）", job.Name, len(job.Runs)))

	var localArtifactPath string
	if job.ArtifactID != "" && s.catalogSvc != nil {
		if art, err := s.catalogSvc.GetArtifact(ctx, wsID, job.ArtifactID); err == nil {
			localArtifactPath = art.StoragePath
			s.broker.Publish(topic, "system", "info", fmt.Sprintf("绑定 CAS 制品: %s (%s)", art.Name, art.SHA256[:12]))
		}
	}

	var (
		mu       sync.Mutex
		okCount  int
		errCount int
		sem      = make(chan struct{}, 8)
		wg       sync.WaitGroup
	)
	for _, r := range job.Runs {
		wg.Add(1)
		sem <- struct{}{}
		go func(run JobRun) {
			defer wg.Done()
			defer func() { <-sem }()
			start := time.Now()
			if localArtifactPath != "" && job.RemoteDest != "" {
				if _, target, rerr := s.ResolveSSHTarget(ctx, wsID, run.HostCIID); rerr == nil {
					if uerr := UploadFileSFTP(ctx, target, localArtifactPath, job.RemoteDest); uerr != nil {
						s.broker.Publish(topic, "stderr", "error", fmt.Sprintf("[%s] SFTP 分发失败: %v", run.HostName, uerr))
					} else {
						s.broker.Publish(topic, "system", "ok", fmt.Sprintf("[%s] 制品原子落盘完成 -> %s", run.HostName, job.RemoteDest))
					}
				}
			}

			transport, code, out, err := s.ExecuteOnHost(ctx, wsID, run.HostCIID, job.Command, topic)
			dur := time.Since(start).Milliseconds()
			status := "SUCCEEDED"
			if err != nil || code != 0 {
				status = "FAILED"
				if err != nil && !strings.Contains(out, err.Error()) {
					out = strings.TrimSpace(out + "\n" + err.Error())
				}
				s.broker.Publish(topic, "stderr", "error", fmt.Sprintf("[%s] 执行失败 (exit=%d): %v", run.HostName, code, err))
			} else {
				s.broker.Publish(topic, "system", "ok", fmt.Sprintf("[%s] 执行成功 (%s, %dms)", run.HostName, transport, dur))
			}
			_, _ = s.store.Exec(ctx,
				`UPDATE exec_job_runs SET transport=?, status=?, exit_code=?, output=?, duration_ms=?, finished_at=? WHERE id=?`,
				transport, status, code, out, dur, nowISO(), run.ID,
			)
			mu.Lock()
			if status == "SUCCEEDED" {
				okCount++
			} else {
				errCount++
			}
			mu.Unlock()
		}(r)
	}
	wg.Wait()

	finalStatus := "SUCCEEDED"
	if errCount > 0 && okCount > 0 {
		finalStatus = "PARTIAL"
	} else if errCount > 0 {
		finalStatus = "FAILED"
	}
	_, _ = s.store.Exec(ctx, `UPDATE exec_jobs SET status = ?, finished_at = ? WHERE id = ?`, finalStatus, nowISO(), job.ID)
	s.broker.Complete(topic, finalStatus, fmt.Sprintf("作业结束: 成功 %d / 失败 %d", okCount, errCount))
}

func (s *Service) ListJobs(ctx context.Context, wsID string) ([]Job, error) {
	rows, err := s.store.Query(ctx,
		`SELECT id, workspace_id, name, template_code, command, artifact_id, remote_dest, target_mode, target_ref_json, status, created_by, created_at, finished_at
		 FROM exec_jobs WHERE workspace_id = ? ORDER BY created_at DESC LIMIT 40`,
		wsID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]Job, 0)
	for rows.Next() {
		var (
			j       Job
			refsRaw string
		)
		if err := rows.Scan(&j.ID, &j.WorkspaceID, &j.Name, &j.TemplateCode, &j.Command, &j.ArtifactID, &j.RemoteDest, &j.TargetMode, &refsRaw, &j.Status, &j.CreatedBy, &j.CreatedAt, &j.FinishedAt); err != nil {
			return nil, err
		}
		j.TargetRefs = make([]string, 0)
		_ = json.Unmarshal([]byte(refsRaw), &j.TargetRefs)
		out = append(out, j)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		runs, _ := s.ListJobRuns(ctx, out[i].ID)
		out[i].Runs = runs
	}
	return out, nil
}

func (s *Service) ListJobRuns(ctx context.Context, jobID string) ([]JobRun, error) {
	rows, err := s.store.Query(ctx,
		`SELECT id, job_id, host_ci_id, host_name, host_ip, transport, status, exit_code, output, duration_ms, started_at, finished_at
		 FROM exec_job_runs WHERE job_id = ? ORDER BY started_at ASC`,
		jobID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]JobRun, 0)
	for rows.Next() {
		var r JobRun
		if err := rows.Scan(&r.ID, &r.JobID, &r.HostCIID, &r.HostName, &r.HostIP, &r.Transport, &r.Status, &r.ExitCode, &r.Output, &r.DurationMs, &r.StartedAt, &r.FinishedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

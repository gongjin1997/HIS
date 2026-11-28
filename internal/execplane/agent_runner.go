package execplane

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/kinstall/kinstall/pkg/platform/netguard"
	"github.com/kinstall/kinstall/pkg/platform/safefs"
	"github.com/kinstall/kinstall/pkg/platform/safety"
)

type TCPProbeResult struct {
	IP        string `json:"ip"`
	Port      int    `json:"port"`
	Open      bool   `json:"open"`
	LatencyMs int64  `json:"latencyMs"`
	Banner    string `json:"banner,omitempty"`
}

// RunLocalTCPProbe runs a bounded, CIDR-guarded concurrent pure-Go TCP port scanner.
func RunLocalTCPProbe(ctx context.Context, cidr string, ports []int, timeoutMs int) ([]TCPProbeResult, error) {
	ips, err := netguard.ExpandCIDR(cidr, 256)
	if err != nil {
		return nil, err
	}
	if len(ports) == 0 {
		ports = []int{22, 80, 443, 5432, 6379, 8080}
	}
	if len(ports) > 16 {
		return nil, errors.New("单次扫描端口数量不能超过 16 个")
	}
	if timeoutMs <= 0 || timeoutMs > 5000 {
		timeoutMs = 600
	}
	timeout := time.Duration(timeoutMs) * time.Millisecond

	type target struct {
		ip   string
		port int
	}
	jobs := make([]target, 0, len(ips)*len(ports))
	for _, ip := range ips {
		for _, p := range ports {
			if p > 0 && p <= 65535 {
				jobs = append(jobs, target{ip: ip, port: p})
			}
		}
	}

	var (
		mu      sync.Mutex
		results = make([]TCPProbeResult, 0)
		sem     = make(chan struct{}, 64)
		wg      sync.WaitGroup
	)
	for _, jb := range jobs {
		wg.Add(1)
		sem <- struct{}{}
		go func(t target) {
			defer wg.Done()
			defer func() { <-sem }()
			start := time.Now()
			d := net.Dialer{Timeout: timeout}
			conn, derr := d.DialContext(ctx, "tcp", net.JoinHostPort(t.ip, strconv.Itoa(t.port)))
			if derr == nil {
				lat := max(int64(1), time.Since(start).Milliseconds())
				_ = conn.Close()
				mu.Lock()
				results = append(results, TCPProbeResult{IP: t.ip, Port: t.port, Open: true, LatencyMs: lat})
				mu.Unlock()
			}
		}(jb)
	}
	wg.Wait()
	return results, nil
}

// EdgeAgentConfig configures the standalone pure-Go outbound edge agent (`kinstall agent`).
type EdgeAgentConfig struct {
	ServerURL    string
	EnrollToken  string
	Zone         string
	IdentityFile string
	Hostname     string
	Version      string
}

type edgeIdentity struct {
	AgentID    string `json:"agentId"`
	HostCIID   string `json:"hostCiId"`
	AgentToken string `json:"agentToken"` // #nosec G117 -- local 0600 agent identity file
	Zone       string `json:"zone"`
}

// RunEdgeAgent starts the zero-DB outbound pure-Go agent (enroll -> 5s heartbeat -> long-poll task runner).
func RunEdgeAgent(ctx context.Context, cfg EdgeAgentConfig, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.Default()
	}
	serverURL := strings.TrimRight(strings.TrimSpace(cfg.ServerURL), "/")
	if serverURL == "" {
		return errors.New("--server 控制面地址不能为空")
	}
	hostname := strings.TrimSpace(cfg.Hostname)
	if hostname == "" {
		hostname, _ = os.Hostname()
	}
	if cfg.Version == "" {
		cfg.Version = "2.0.0"
	}
	if cfg.IdentityFile == "" {
		home, _ := os.UserHomeDir()
		cfg.IdentityFile = filepath.Join(home, ".kinstall", "agent-identity.json")
	}

	httpClient := &http.Client{Timeout: 30 * time.Second}
	ident, err := loadOrEnrollIdentity(ctx, httpClient, serverURL, cfg, hostname)
	if err != nil {
		return err
	}
	logger.Info("edge agent enrolled and ready", "agentId", ident.AgentID, "hostCiId", ident.HostCIID, "zone", ident.Zone)

	// Send immediate heartbeat on boot, then every 5s.
	sendHB := func() {
		loadAvg, memMB, memPct := collectQuickMetrics()
		body, _ := json.Marshal(AgentHeartbeatRequest{
			IP:          detectOutboundIP(),
			CPUCores:    runtime.NumCPU(),
			MemMB:       memMB,
			LoadAvg:     loadAvg,
			CPUUsagePct: 0,
			MemUsagePct: memPct,
		})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, serverURL+"/api/v1/agent/heartbeat", bytes.NewReader(body))
		if err != nil {
			return
		}
		req.Header.Set("Authorization", "Bearer "+ident.AgentToken)
		req.Header.Set("Content-Type", "application/json")
		resp, err := httpClient.Do(req)
		if err != nil {
			logger.Warn("agent heartbeat failed", "err", err)
			return
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}
	sendHB()

	hbTicker := time.NewTicker(5 * time.Second)
	defer hbTicker.Stop()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-hbTicker.C:
				sendHB()
			}
		}
	}()

	// Long-poll task loop.
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		task, statusCode, err := pollNextTask(ctx, httpClient, serverURL, ident.AgentToken)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			logger.Warn("agent poll error", "err", err)
			time.Sleep(2 * time.Second)
			continue
		}
		if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden {
			return errors.New("agent credentials revoked by control plane")
		}
		if task == nil {
			continue
		}
		res := executeEdgeTask(ctx, *task)
		if err := postTaskResult(ctx, httpClient, serverURL, ident.AgentToken, res); err != nil {
			logger.Warn("agent submit result failed", "taskId", task.TaskID, "err", err)
		}
	}
}

func loadOrEnrollIdentity(ctx context.Context, client *http.Client, serverURL string, cfg EdgeAgentConfig, hostname string) (*edgeIdentity, error) {
	if cfg.EnrollToken == "" {
		raw, err := os.ReadFile(cfg.IdentityFile)
		if err != nil {
			return nil, fmt.Errorf("未提供 --token 且读取身份文件 %s 失败: %w", cfg.IdentityFile, err)
		}
		var ident edgeIdentity
		if err := json.Unmarshal(raw, &ident); err != nil {
			return nil, err
		}
		return &ident, nil
	}

	_, memMB, _ := collectQuickMetrics()
	reqBody, _ := json.Marshal(AgentEnrollRequest{
		EnrollToken: cfg.EnrollToken,
		Hostname:    hostname,
		IP:          detectOutboundIP(),
		Zone:        cfg.Zone,
		OS:          runtime.GOOS,
		Arch:        runtime.GOARCH,
		Version:     cfg.Version,
		CPUCores:    runtime.NumCPU(),
		MemMB:       memMB,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, serverURL+"/api/v1/agent/enroll", bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Agent 纳管请求失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("Agent 纳管被拒绝 (HTTP %d): %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var enrResp AgentEnrollResponse
	if err := json.Unmarshal(raw, &enrResp); err != nil {
		return nil, err
	}
	ident := &edgeIdentity{
		AgentID:    enrResp.AgentID,
		HostCIID:   enrResp.HostCIID,
		AgentToken: enrResp.AgentToken,
		Zone:       enrResp.Zone,
	}
	payload, _ := json.MarshalIndent(ident, "", "  ")
	_ = safefs.AtomicWriteFile(cfg.IdentityFile, payload, 0o600)
	return ident, nil
}

func pollNextTask(ctx context.Context, client *http.Client, serverURL, token string) (*AgentTask, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, serverURL+"/api/v1/agent/tasks/poll", nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNoContent {
		return nil, resp.StatusCode, nil
	}
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, resp.StatusCode, nil
	}
	var task AgentTask
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&task); err != nil {
		return nil, resp.StatusCode, err
	}
	return &task, resp.StatusCode, nil
}

func postTaskResult(ctx context.Context, client *http.Client, serverURL, token string, res AgentTaskResult) error {
	body, _ := json.Marshal(res)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, serverURL+"/api/v1/agent/tasks/"+res.TaskID+"/result", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.Body.Close()
}

func executeEdgeTask(ctx context.Context, task AgentTask) AgentTaskResult {
	start := time.Now()
	res := AgentTaskResult{TaskID: task.TaskID}
	switch task.Kind {
	case "tcp_probe":
		items, err := RunLocalTCPProbe(ctx, task.CIDR, task.Ports, task.TimeoutMs)
		res.DurationMs = time.Since(start).Milliseconds()
		if err != nil {
			res.ExitCode = 1
			res.Error = err.Error()
			return res
		}
		res.ProbeItems = items
		res.Output = fmt.Sprintf("CIDR %s 探测完成，发现 %d 个开放端口", task.CIDR, len(items))
		return res
	case "collect_facts":
		code, out, err := runProcessGroupShell(ctx, CollectLinuxFactsScript, 20*time.Second)
		res.ExitCode = code
		res.Output = out
		res.DurationMs = time.Since(start).Milliseconds()
		if err != nil {
			res.Error = err.Error()
		}
		return res
	default: // shell
		if err := safety.CheckCommand(task.Command); err != nil {
			res.ExitCode = 126
			res.Error = err.Error()
			res.DurationMs = time.Since(start).Milliseconds()
			return res
		}
		timeout := 45 * time.Second
		if task.TimeoutMs > 0 {
			timeout = time.Duration(task.TimeoutMs) * time.Millisecond
		}
		code, out, err := runProcessGroupShell(ctx, task.Command, timeout)
		res.ExitCode = code
		res.Output = out
		res.DurationMs = time.Since(start).Milliseconds()
		if err != nil {
			res.Error = err.Error()
		}
		return res
	}
}

func runProcessGroupShell(parent context.Context, script string, timeout time.Duration) (int, string, error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	cmd := exec.Command("sh", "-c", script) // #nosec G204 -- safety.CheckCommand validated before invocation
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Start(); err != nil {
		return -1, "", err
	}
	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()
	select {
	case <-ctx.Done():
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return -1, out.String(), errors.New("command timed out and process group terminated")
	case err := <-waitCh:
		if err != nil {
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				return ee.ExitCode(), out.String(), nil
			}
			return -1, out.String(), err
		}
		return 0, out.String(), nil
	}
}

func detectOutboundIP() string {
	conn, err := net.DialTimeout("udp", "1.1.1.1:80", 500*time.Millisecond)
	if err == nil {
		defer func() { _ = conn.Close() }()
		if addr, ok := conn.LocalAddr().(*net.UDPAddr); ok && addr.IP != nil {
			return addr.IP.String()
		}
	}
	return "127.0.0.1"
}

func collectQuickMetrics() (loadAvg string, memMB int, memPct float64) {
	if raw, err := os.ReadFile("/proc/loadavg"); err == nil {
		fields := strings.Fields(string(raw))
		if len(fields) >= 3 {
			loadAvg = strings.Join(fields[:3], " ")
		}
	}
	if raw, err := os.ReadFile("/proc/meminfo"); err == nil {
		var totalKB, availKB int64
		for _, line := range strings.Split(string(raw), "\n") {
			if strings.HasPrefix(line, "MemTotal:") {
				f := strings.Fields(line)
				if len(f) >= 2 {
					totalKB, _ = strconv.ParseInt(f[1], 10, 64)
				}
			} else if strings.HasPrefix(line, "MemAvailable:") {
				f := strings.Fields(line)
				if len(f) >= 2 {
					availKB, _ = strconv.ParseInt(f[1], 10, 64)
				}
			}
		}
		if totalKB > 0 {
			memMB = int(totalKB / 1024)
			if availKB > 0 && availKB <= totalKB {
				memPct = float64(totalKB-availKB) * 100.0 / float64(totalKB)
			}
		}
	}
	return loadAvg, memMB, memPct
}

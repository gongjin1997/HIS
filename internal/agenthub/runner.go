package agenthub

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
	"strings"
	"syscall"
	"time"
)

// RunnerConfig configures the outbound zero-trust kinstall edge agent (zero inbound listening ports).
type RunnerConfig struct {
	ServerURL   string
	EnrollToken string
	Zone        string
	StateDir    string
	Logger      *slog.Logger
}

type agentIdentity struct {
	AgentID   string `json:"agentId"`
	ProjectID string `json:"projectId"`
	Zone      string `json:"zone"`
	AuthToken string `json:"authToken"`
}

// RunAgent runs the outbound-only zero-trust edge agent loop (Enroll -> Heartbeat -> Poll/Execute Tasks).
func RunAgent(ctx context.Context, cfg RunnerConfig) error {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	serverURL := strings.TrimRight(strings.TrimSpace(cfg.ServerURL), "/")
	if serverURL == "" {
		return fmt.Errorf("--server is required")
	}
	if cfg.StateDir == "" {
		home, _ := os.UserHomeDir()
		if home == "" {
			home = "/tmp"
		}
		cfg.StateDir = filepath.Join(home, ".kinstall-agent")
	}
	_ = os.MkdirAll(cfg.StateDir, 0o700)
	idPath := filepath.Join(cfg.StateDir, "identity.json")

	client := &http.Client{Timeout: 20 * time.Second}
	var ident agentIdentity

	// Load persisted identity unless a fresh enroll token was explicitly supplied
	if cfg.EnrollToken == "" {
		// #nosec G304 -- operator-configured agent state path
		if b, err := os.ReadFile(idPath); err == nil {
			_ = json.Unmarshal(b, &ident)
		}
	}

	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "linux-node"
	}
	localIP := detectLocalIP()
	osInfo := detectOSInfo()

	if ident.AuthToken == "" {
		if strings.TrimSpace(cfg.EnrollToken) == "" {
			return fmt.Errorf("agent not enrolled yet; supply --token from the Fleet console")
		}
		reqBody, _ := json.Marshal(EnrollRequest{
			EnrollToken:  cfg.EnrollToken,
			Hostname:     hostname,
			IPAddress:    localIP,
			OSArch:       runtime.GOARCH,
			OSInfo:       osInfo,
			AgentVersion: "1.0.0",
			Capabilities: []string{"exec", "probe_tcp", "collect_facts"},
		})
		resp, err := postJSON(ctx, client, serverURL+"/api/v1/agent/enroll", "", reqBody)
		if err != nil {
			return fmt.Errorf("enroll failed: %w", err)
		}
		var er EnrollResponse
		if err := json.Unmarshal(resp, &er); err != nil {
			return fmt.Errorf("decode enroll response: %w", err)
		}
		ident = agentIdentity(er)
		// #nosec G117 -- persisting enrolled agent bearer token strictly into local 0600 identity file
		if b, err := json.MarshalIndent(ident, "", "  "); err == nil {
			_ = os.WriteFile(idPath, b, 0o600)
		}
		cfg.Logger.Info("kinstall-agent enrolled via outbound channel",
			slog.String("agentId", ident.AgentID),
			slog.String("zone", ident.Zone),
			slog.String("server", serverURL),
		)
	}

	hbTicker := time.NewTicker(5 * time.Second)
	pollTicker := time.NewTicker(1 * time.Second)
	defer hbTicker.Stop()
	defer pollTicker.Stop()

	sendHeartbeat := func() {
		body, _ := json.Marshal(map[string]any{
			"ipAddress": detectLocalIP(),
			"osInfo":    osInfo,
			"metrics":   collectLocalMetrics(),
		})
		if _, err := postJSON(ctx, client, serverURL+"/api/v1/agent/heartbeat", ident.AuthToken, body); err != nil {
			cfg.Logger.Warn("agent heartbeat failed", slog.String("error", err.Error()))
		}
	}

	// Immediate initial heartbeat
	sendHeartbeat()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-hbTicker.C:
			sendHeartbeat()
		case <-pollTicker.C:
			raw, err := getJSON(ctx, client, serverURL+"/api/v1/agent/tasks/poll", ident.AuthToken)
			if err != nil {
				continue
			}
			var wrapper struct {
				Items []AgentTask `json:"items"`
			}
			if json.Unmarshal(raw, &wrapper) != nil || len(wrapper.Items) == 0 {
				continue
			}
			for _, task := range wrapper.Items {
				go executeAndReport(ctx, client, serverURL, ident, task, cfg.Logger)
			}
		}
	}
}

func executeAndReport(ctx context.Context, client *http.Client, serverURL string, ident agentIdentity, task AgentTask, logger *slog.Logger) {
	status := "SUCCEEDED"
	exitCode := 0
	var stdout, stderr string
	outputs := map[string]any{}

	switch task.Kind {
	case "probe_tcp":
		rawTargets, _ := json.Marshal(task.Payload["targets"])
		var targets []TCPProbeTarget
		_ = json.Unmarshal(rawTargets, &targets)
		timeoutSec := 3
		if f, ok := task.Payload["timeoutSec"].(float64); ok && f > 0 {
			timeoutSec = int(f)
		}
		res := ExecuteLocalTCPProbes(ctx, targets, timeoutSec, "agent:"+ident.AgentID)
		outputs["results"] = res
		stdout = fmt.Sprintf("probed %d TCP targets via agent %s", len(res), ident.AgentID)
	case "collect_facts":
		outputs["metrics"] = collectLocalMetrics()
		outputs["osInfo"] = detectOSInfo()
		outputs["ipAddress"] = detectLocalIP()
		stdout = "collected read-only host facts"
	case "exec":
		script, _ := task.Payload["script"].(string)
		if strings.TrimSpace(script) == "" {
			status = "FAILED"
			exitCode = 2
			stderr = "empty script"
		} else {
			tctx, cancel := context.WithTimeout(ctx, 60*time.Second)
			defer cancel()
			// #nosec G204 -- guarded task dispatched by authenticated control plane
			cmd := exec.CommandContext(tctx, "/bin/sh", "-c", script)
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			var outBuf, errBuf bytes.Buffer
			cmd.Stdout = &outBuf
			cmd.Stderr = &errBuf
			err := cmd.Run()
			stdout = outBuf.String()
			stderr = errBuf.String()
			if err != nil {
				status = "FAILED"
				exitCode = 1
				var ee *exec.ExitError
				if ok := isExitError(err, &ee); ok {
					exitCode = ee.ExitCode()
				}
				if cmd.Process != nil {
					_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
				}
			}
		}
	default:
		status = "FAILED"
		exitCode = 1
		stderr = "unsupported agent task kind: " + task.Kind
	}

	reportBody, _ := json.Marshal(map[string]any{
		"status":   status,
		"exitCode": exitCode,
		"stdout":   stdout,
		"stderr":   stderr,
		"outputs":  outputs,
	})
	endpoint := fmt.Sprintf("%s/api/v1/agent/tasks/%s/result", serverURL, task.ID)
	if _, err := postJSON(ctx, client, endpoint, ident.AuthToken, reportBody); err != nil {
		logger.Warn("failed to report agent task result", slog.String("taskId", task.ID), slog.String("error", err.Error()))
	}
}

func isExitError(err error, target **exec.ExitError) bool {
	return errors.As(err, target)
}

func postJSON(ctx context.Context, client *http.Client, url, bearer string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return b, nil
}

func getJSON(ctx context.Context, client *http.Client, url, bearer string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return b, nil
}

func detectLocalIP() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "127.0.0.1"
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil {
				return ipn.IP.String()
			}
		}
	}
	return "127.0.0.1"
}

func detectOSInfo() string {
	if b, err := os.ReadFile("/etc/os-release"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(line, "PRETTY_NAME=") {
				return strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), `"`)
			}
		}
	}
	return runtime.GOOS + "/" + runtime.GOARCH
}

func collectLocalMetrics() map[string]any {
	m := map[string]any{
		"numCpu": runtime.NumCPU(),
		"goArch": runtime.GOARCH,
		"goOs":   runtime.GOOS,
	}
	if b, err := os.ReadFile("/proc/loadavg"); err == nil {
		fields := strings.Fields(string(b))
		if len(fields) >= 3 {
			m["loadAvg"] = strings.Join(fields[:3], " ")
		}
	}
	if b, err := os.ReadFile("/proc/meminfo"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(line, "MemTotal:") || strings.HasPrefix(line, "MemAvailable:") {
				parts := strings.Fields(line)
				if len(parts) >= 2 {
					m[strings.TrimSuffix(parts[0], ":")] = parts[1] + " kB"
				}
			}
		}
	}
	return m
}

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/kinstall/kinstall/pkg/platform/audit"
	"github.com/kinstall/kinstall/pkg/platform/safefs"
)

const agentVersion = "liveops-node/1.0.0"

type nodeState struct {
	NodeID     string `json:"node_id"`
	Credential string `json:"credential"`
	ServerURL  string `json:"server_url"`
	Hostname   string `json:"hostname"`
}

func main() {
	serverURL := flag.String("server", "http://127.0.0.1:8080", "LiveOps API base URL")
	enrollToken := flag.String("enroll-token", "", "One-time enrollment token (lo_enr_...)")
	statePath := flag.String("state-file", defaultStatePath(), "Path to persist zero-trust node credentials")
	hostname := flag.String("hostname", defaultHostname(), "Reported node hostname")
	ipAddr := flag.String("ip", "127.0.0.1", "Reported node IP address")
	interval := flag.Duration("heartbeat", 15*time.Second, "Heartbeat interval")
	once := flag.Bool("once", false, "Enroll / send single heartbeat and exit (useful for smoke tests)")
	flag.Parse()

	logger := slog.New(audit.NewRedactingHandler(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))
	client := &http.Client{Timeout: 10 * time.Second}
	base := strings.TrimRight(*serverURL, "/")

	st, err := loadState(*statePath)
	if err != nil || st.NodeID == "" || st.Credential == "" {
		if strings.TrimSpace(*enrollToken) == "" {
			logger.Error("node not enrolled and --enroll-token not supplied")
			os.Exit(1)
		}
		st, err = enroll(client, base, *enrollToken, *hostname, *ipAddr)
		if err != nil {
			logger.Error("node enrollment failed", "err", err)
			os.Exit(1)
		}
		if werr := saveState(*statePath, st); werr != nil {
			logger.Error("persist node state failed", "err", werr)
			os.Exit(1)
		}
		logger.Info("zero-trust node enrolled", "nodeId", st.NodeID, "hostname", st.Hostname)
	}

	if err := sendHeartbeat(client, base, st, *ipAddr); err != nil {
		logger.Error("initial heartbeat failed", "nodeId", st.NodeID, "err", err)
		if *once {
			os.Exit(1)
		}
	} else {
		logger.Info("heartbeat acknowledged", "nodeId", st.NodeID)
	}

	if *once {
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	ticker := time.NewTicker(*interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Info("liveops-node shutting down gracefully", "nodeId", st.NodeID)
			return
		case <-ticker.C:
			if err := sendHeartbeat(client, base, st, *ipAddr); err != nil {
				logger.Warn("heartbeat error", "nodeId", st.NodeID, "err", err)
			}
		}
	}
}

func defaultStatePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".liveops", "node-state.json")
}

func defaultHostname() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "liveops-edge-node"
	}
	return h
}

func loadState(path string) (nodeState, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nodeState{}, err
	}
	var st nodeState
	err = json.Unmarshal(raw, &st)
	return st, err
}

func saveState(path string, st nodeState) error {
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return safefs.AtomicWriteFile(path, raw, 0o600)
}

func enroll(client *http.Client, base, token, hostname, ip string) (nodeState, error) {
	body, _ := json.Marshal(map[string]any{
		"enrollToken":  token,
		"hostname":     hostname,
		"ipAddress":    ip,
		"osArch":       runtime.GOOS + "/" + runtime.GOARCH,
		"agentVersion": agentVersion,
	})
	resp, err := client.Post(base+"/api/v1/nodes/enroll", "application/json", bytes.NewReader(body))
	if err != nil {
		return nodeState{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated {
		return nodeState{}, fmt.Errorf("enroll HTTP %d: %s", resp.StatusCode, string(raw))
	}
	var out struct {
		Node struct {
			NodeID   string `json:"node_id"`
			Hostname string `json:"hostname"`
		} `json:"node"`
		Credential string `json:"credential"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nodeState{}, err
	}
	return nodeState{
		NodeID:     out.Node.NodeID,
		Credential: out.Credential,
		ServerURL:  base,
		Hostname:   out.Node.Hostname,
	}, nil
}

func sendHeartbeat(client *http.Client, base string, st nodeState, ip string) error {
	body, _ := json.Marshal(map[string]any{
		"ipAddress":    ip,
		"agentVersion": agentVersion,
	})
	req, err := http.NewRequest(http.MethodPost, base+"/api/v1/nodes/"+st.NodeID+"/heartbeat", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+st.Credential)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("heartbeat HTTP %d: %s", resp.StatusCode, string(raw))
	}
	return nil
}

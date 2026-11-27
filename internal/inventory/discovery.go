package inventory

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"time"
)

// DiscoveredHost describes an active SSH endpoint found during CIDR subnet discovery (T-M1-04).
type DiscoveredHost struct {
	Address   string `json:"address"`
	Port      int    `json:"port"`
	Reachable bool   `json:"reachable"`
	LatencyMs int64  `json:"latencyMs"`
	Banner    string `json:"banner,omitempty"`
}

// DiscoverSubnet scans up to 1024 addresses in cidr for open SSH ports and captures SSH-2.0 banners concurrently.
func DiscoverSubnet(ctx context.Context, cidr string, port int, timeout time.Duration, concurrency int) ([]DiscoveredHost, error) {
	pfx, err := netip.ParsePrefix(strings.TrimSpace(cidr))
	if err != nil {
		return nil, fmt.Errorf("invalid CIDR %q: %w", cidr, err)
	}
	if port <= 0 || port > 65535 {
		port = 22
	}
	if timeout <= 0 {
		timeout = 1200 * time.Millisecond
	}
	if concurrency <= 0 {
		concurrency = 32
	}

	var addrs []netip.Addr
	masked := pfx.Masked()
	for a := masked.Addr(); masked.Contains(a); a = a.Next() {
		addrs = append(addrs, a)
		if len(addrs) >= 1024 {
			break
		}
	}
	// Trim network/broadcast on IPv4 subnets wider than /31
	if masked.Addr().Is4() && masked.Bits() <= 30 && len(addrs) > 2 {
		addrs = addrs[1 : len(addrs)-1]
	}

	sem := make(chan struct{}, concurrency)
	var (
		mu      sync.Mutex
		wg      sync.WaitGroup
		results []DiscoveredHost
	)
	dialer := &net.Dialer{Timeout: timeout}

	for _, addr := range addrs {
		ip := addr.String()
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}
			dctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			start := time.Now()
			conn, derr := dialer.DialContext(dctx, "tcp", net.JoinHostPort(ip, fmt.Sprintf("%d", port)))
			if derr != nil {
				return
			}
			latency := time.Since(start).Milliseconds()
			_ = conn.SetReadDeadline(time.Now().Add(400 * time.Millisecond))
			banner, _ := bufio.NewReader(conn).ReadString('\n')
			_ = conn.Close()
			mu.Lock()
			results = append(results, DiscoveredHost{
				Address:   ip,
				Port:      port,
				Reachable: true,
				LatencyMs: latency,
				Banner:    strings.TrimSpace(banner),
			})
			mu.Unlock()
		}()
	}
	wg.Wait()
	sort.Slice(results, func(i, j int) bool { return results[i].Address < results[j].Address })
	return results, nil
}

// BuildSSHKeyDistributeScript validates an OpenSSH public key and emits an idempotent script to append it to authorized_keys (T-M1-04).
func BuildSSHKeyDistributeScript(pubKey string) (string, error) {
	clean := strings.TrimSpace(pubKey)
	if clean == "" || strings.ContainsAny(clean, "\r\n'\"`;$|&<>") {
		return "", fmt.Errorf("invalid SSH public key format")
	}
	validPrefix := strings.HasPrefix(clean, "ssh-ed25519 ") ||
		strings.HasPrefix(clean, "ssh-rsa ") ||
		strings.HasPrefix(clean, "ecdsa-sha2-")
	if !validPrefix {
		return "", fmt.Errorf("unsupported SSH public key type (expected ssh-ed25519 / ssh-rsa / ecdsa-sha2-*)")
	}
	script := fmt.Sprintf(
		"mkdir -p ~/.ssh && chmod 700 ~/.ssh && touch ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys && (grep -qxF '%s' ~/.ssh/authorized_keys || echo '%s' >> ~/.ssh/authorized_keys)",
		clean, clean,
	)
	return script, nil
}

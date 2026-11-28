package netguard

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

// ValidateIP checks whether ip is safe to dial or probe.
// Cloud metadata (169.254.0.0/16), link-local, multicast, and unspecified addresses are ALWAYS blocked.
func ValidateIP(ip net.IP, allowPrivate bool) error {
	if ip == nil {
		return errors.New("empty IP address")
	}
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return errors.New("invalid IP address")
	}
	addr = addr.Unmap()

	if addr.IsUnspecified() || addr.IsMulticast() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() {
		return fmt.Errorf("blocked link-local/metadata/multicast IP %s", addr.String())
	}
	if !allowPrivate && (addr.IsPrivate() || addr.IsLoopback()) {
		return fmt.Errorf("blocked private/loopback IP %s", addr.String())
	}
	return nil
}

// ExpandCIDR validates a CIDR string and expands up to maxHosts safe probe targets.
// Loopback/private enterprise LAN CIDRs are permitted, while cloud metadata & link-local ranges are always blocked.
func ExpandCIDR(cidr string, maxHosts int) ([]string, error) {
	if maxHosts <= 0 {
		maxHosts = 1024
	}
	prefix, err := netip.ParsePrefix(strings.TrimSpace(cidr))
	if err != nil {
		return nil, fmt.Errorf("invalid CIDR %q: %w", cidr, err)
	}
	prefix = prefix.Masked()
	if !prefix.Addr().Is4() {
		return nil, errors.New("only IPv4 CIDR ranges are supported for batch scanning")
	}
	bits := prefix.Bits()
	if bits < 16 {
		return nil, fmt.Errorf("CIDR prefix /%d is too broad (minimum allowed is /16)", bits)
	}
	hostCount := 1 << (32 - bits)
	if hostCount > maxHosts+2 {
		return nil, fmt.Errorf("CIDR contains %d addresses, exceeding limit %d", hostCount, maxHosts)
	}

	out := make([]string, 0, min(hostCount, maxHosts))
	curr := prefix.Addr()
	for prefix.Contains(curr) {
		ip := net.IP(curr.AsSlice())
		if err := ValidateIP(ip, true); err != nil {
			return nil, fmt.Errorf("CIDR intersects forbidden address %s: %w", curr.String(), err)
		}
		out = append(out, curr.String())
		next := curr.Next()
		if !next.IsValid() || next == curr {
			break
		}
		curr = next
	}
	// Strip network and broadcast addresses for prefixes smaller than /31.
	if bits <= 30 && len(out) > 2 {
		out = out[1 : len(out)-1]
	}
	if len(out) > maxHosts {
		out = out[:maxHosts]
	}
	return out, nil
}

// NewSafeHTTPClient returns an HTTP client whose DialContext validates every resolved IP
// immediately prior to TCP connection, neutralizing DNS rebinding & cloud-metadata SSRF.
func NewSafeHTTPClient(timeout time.Duration, allowPrivate bool) *http.Client {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	dialer := &net.Dialer{
		Timeout:   min(timeout, 5*time.Second),
		KeepAlive: 30 * time.Second,
	}
	transport := &http.Transport{
		Proxy: nil, // Never trust ambient HTTP_PROXY to bypass SSRF checks.
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, fmt.Errorf("invalid dial address %q: %w", addr, err)
			}
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil || len(ips) == 0 {
				return nil, fmt.Errorf("dns lookup failed for %q: %w", host, err)
			}
			var lastErr error
			for _, ipAddr := range ips {
				if err := ValidateIP(ipAddr.IP, allowPrivate); err != nil {
					return nil, err
				}
				conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ipAddr.IP.String(), port))
				if err == nil {
					return conn, nil
				}
				lastErr = err
			}
			return nil, lastErr
		},
		MaxIdleConns:          32,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("stopped after 5 redirects")
			}
			return nil
		},
	}
}

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

var alwaysBlockedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("169.254.0.0/16"), // Cloud metadata (AWS/Huawei/GCP)
	netip.MustParsePrefix("fe80::/10"),      // IPv6 link-local
}

var privatePrefixes = []netip.Prefix{
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("fc00::/7"),
}

// ValidateIP checks whether ip is safe to dial. Cloud metadata & link-local are ALWAYS blocked.
func ValidateIP(ip net.IP, allowPrivate bool) error {
	if ip == nil {
		return errors.New("empty IP")
	}
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return errors.New("invalid IP")
	}
	addr = addr.Unmap()
	if addr.IsUnspecified() || addr.IsMulticast() {
		return fmt.Errorf("ssrf blocked: unspecified/multicast ip %s", addr)
	}
	for _, p := range alwaysBlockedPrefixes {
		if p.Contains(addr) {
			return fmt.Errorf("ssrf blocked: restricted metadata/link-local ip %s", addr)
		}
	}
	if !allowPrivate {
		for _, p := range privatePrefixes {
			if p.Contains(addr) {
				return fmt.Errorf("ssrf blocked: restricted private/loopback ip %s", addr)
			}
		}
	}
	return nil
}

// NewSafeTransport constructs an anti-SSRF & anti-DNS-Rebinding HTTP transport (LiveOps 04.2 Spec).
func NewSafeTransport(allowedHostSuffixes []string, allowPrivate bool) *http.Transport {
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	return &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			if len(allowedHostSuffixes) > 0 && !matchHostSuffix(host, allowedHostSuffixes) {
				return nil, fmt.Errorf("ssrf blocked: host %q not in allowlist", host)
			}
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil || len(ips) == 0 {
				return nil, errors.New("dns resolution failed")
			}
			for _, ipAddr := range ips {
				if err := ValidateIP(ipAddr.IP, allowPrivate); err != nil {
					return nil, err
				}
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
		},
		MaxIdleConns:        64,
		IdleConnTimeout:     60 * time.Second,
		TLSHandshakeTimeout: 5 * time.Second,
	}
}

func matchHostSuffix(host string, suffixes []string) bool {
	h := strings.ToLower(strings.TrimSuffix(host, "."))
	for _, s := range suffixes {
		s = strings.ToLower(strings.TrimSpace(s))
		if h == s || strings.HasSuffix(h, "."+s) {
			return true
		}
	}
	return false
}

package taskhandlers

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

// safeHTTPClient returns an http.Client whose dialer blocks requests to
// private/loopback addresses, preventing SSRF attacks.
func safeHTTPClient(timeoutSeconds int) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second}

	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}

			ips, err := net.DefaultResolver.LookupHost(ctx, host)
			if err != nil {
				return nil, err
			}

			for _, raw := range ips {
				ip := net.ParseIP(raw)
				if ip == nil {
					continue
				}
				if isPrivateIP(ip) {
					return nil, fmt.Errorf("ssrf: blocked request to private address %s", ip)
				}
			}

			// Dial by resolved IP directly to prevent DNS rebinding.
			return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0], port))
		},
	}

	return &http.Client{
		Timeout:   time.Duration(timeoutSeconds) * time.Second,
		Transport: transport,
	}
}

func isPrivateIP(ip net.IP) bool {
	private := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
		"169.254.0.0/16", // link-local / AWS metadata
		"::1/128",
		"fc00::/7",
	}
	for _, cidr := range private {
		_, block, _ := net.ParseCIDR(cidr)
		if block.Contains(ip) {
			return true
		}
	}
	return false
}

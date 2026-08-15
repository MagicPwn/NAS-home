package probe

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var ErrBlockedTarget = errors.New("target blocked by network safety policy")

// NewExternal probes user-managed links without allowing requests to local or
// private networks. The link can still be saved and opened by the browser;
// only the server-side metadata probe is blocked.
func NewExternal(timeout time.Duration) *Prober {
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	dialer := &net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			return safeDialContext(ctx, dialer, network, address)
		},
	}
	return &Prober{Client: &http.Client{
		Timeout:       timeout,
		Transport:     transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

func safeDialContext(ctx context.Context, dialer *net.Dialer, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid target address", ErrBlockedTarget)
	}
	if _, err := strconv.Atoi(port); err != nil {
		return nil, fmt.Errorf("%w: invalid target port", ErrBlockedTarget)
	}
	if ip := net.ParseIP(host); ip != nil {
		if blockedIP(ip) {
			return nil, fmt.Errorf("%w: private address", ErrBlockedTarget)
		}
		return dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
	}
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("%w: no address", ErrBlockedTarget)
	}
	for _, ip := range ips {
		if blockedIP(ip) {
			return nil, fmt.Errorf("%w: private address", ErrBlockedTarget)
		}
	}
	var lastErr error
	for _, ip := range ips {
		conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if dialErr == nil {
			return conn, nil
		}
		lastErr = dialErr
	}
	return nil, lastErr
}

func blockedIP(ip net.IP) bool {
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	return ip.IsUnspecified() || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || inCGNAT(ip) || inBenchmarkRange(ip)
}

func inCGNAT(ip net.IP) bool {
	ip = ip.To4()
	return ip != nil && ip[0] == 100 && ip[1]&0xc0 == 64
}

func inBenchmarkRange(ip net.IP) bool {
	ip = ip.To4()
	return ip != nil && ip[0] == 198 && (ip[1] == 18 || ip[1] == 19)
}

func ValidateExternalURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "http" && u.Scheme != "https" || u.Host == "" {
		return errors.New("URL must use http/https and include a host")
	}
	if u.User != nil || u.Fragment != "" {
		return errors.New("URL must not include credentials or fragment")
	}
	if strings.TrimSpace(u.Hostname()) != u.Hostname() || strings.ContainsAny(u.Hostname(), "\r\n\t ") {
		return errors.New("URL host is invalid")
	}
	if port := u.Port(); port != "" {
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 {
			return errors.New("URL port is invalid")
		}
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && blockedIP(ip) {
		return ErrBlockedTarget
	}
	return nil
}

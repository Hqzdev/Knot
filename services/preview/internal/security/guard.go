package security

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

var ErrUnsafeAddress = errors.New("link target is not publicly routable")

type Resolver interface {
	LookupNetIP(ctx context.Context, network string, host string) ([]netip.Addr, error)
}

type Guard struct {
	resolver Resolver
	dialer   net.Dialer
}

func NewGuard(resolver Resolver) *Guard {
	return &Guard{resolver: resolver, dialer: net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}}
}

func (guard *Guard) Validate(ctx context.Context, value string) (*url.URL, error) {
	if len(value) == 0 || len(value) > 2048 {
		return nil, ErrUnsafeAddress
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "http" && parsed.Scheme != "https" || parsed.User != nil || parsed.Hostname() == "" {
		return nil, ErrUnsafeAddress
	}
	hostname := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if hostname == "localhost" || strings.HasSuffix(hostname, ".localhost") || strings.HasSuffix(hostname, ".local") {
		return nil, ErrUnsafeAddress
	}
	addresses, err := guard.lookup(ctx, hostname)
	if err != nil || len(addresses) == 0 {
		return nil, ErrUnsafeAddress
	}
	return parsed, nil
}

func (guard *Guard) DialContext(ctx context.Context, network string, address string) (net.Conn, error) {
	hostname, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, ErrUnsafeAddress
	}
	addresses, err := guard.lookup(ctx, hostname)
	if err != nil || len(addresses) == 0 {
		return nil, ErrUnsafeAddress
	}
	var lastError error
	for _, candidate := range addresses {
		connection, dialError := guard.dialer.DialContext(ctx, network, net.JoinHostPort(candidate.String(), port))
		if dialError == nil {
			return connection, nil
		}
		lastError = dialError
	}
	return nil, lastError
}

func (guard *Guard) lookup(ctx context.Context, hostname string) ([]netip.Addr, error) {
	if parsed, err := netip.ParseAddr(hostname); err == nil {
		address := parsed.Unmap()
		if blocked(address) {
			return nil, ErrUnsafeAddress
		}
		return []netip.Addr{address}, nil
	}
	addresses, err := guard.resolver.LookupNetIP(ctx, "ip", hostname)
	if err != nil {
		return nil, err
	}
	validated := make([]netip.Addr, 0, len(addresses))
	for _, resolved := range addresses {
		address := resolved.Unmap()
		if blocked(address) {
			return nil, ErrUnsafeAddress
		}
		validated = append(validated, address)
	}
	return validated, nil
}

func blocked(address netip.Addr) bool {
	if !address.IsValid() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || address.IsMulticast() || address.IsUnspecified() {
		return true
	}
	for _, prefix := range blockedPrefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

var blockedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("fc00::/7"),
}

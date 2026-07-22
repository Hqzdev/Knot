package provider

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"time"
)

type publicNetworkDialer struct {
	resolver *net.Resolver
	dialer   net.Dialer
}

func newPublicHTTPClient(timeout time.Duration) *http.Client {
	dialer := &publicNetworkDialer{
		resolver: net.DefaultResolver,
		dialer: net.Dialer{
			Timeout:   timeout,
			KeepAlive: 30 * time.Second,
		},
	}
	transport := &http.Transport{
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		MaxConnsPerHost:       20,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: timeout,
		ExpectContinueTimeout: time.Second,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
	}
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func (dialer *publicNetworkDialer) DialContext(ctx context.Context, network string, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	addresses, err := dialer.resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	var lastError error
	for _, address := range addresses {
		if !publicIP(address.IP) {
			continue
		}
		connection, err := dialer.dialer.DialContext(ctx, network, net.JoinHostPort(address.IP.String(), port))
		if err == nil {
			return connection, nil
		}
		lastError = err
	}
	if lastError != nil {
		return nil, lastError
	}
	return nil, errors.New("push endpoint did not resolve to a public address")
}

func publicIP(ip net.IP) bool {
	return ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast()
}

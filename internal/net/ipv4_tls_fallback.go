package net

import (
	"context"
	"crypto/tls"
	"fmt"
	stdnet "net"
	"net/http"
	"net/netip"
	"time"
)

const ipv4TLSFallbackStagger = 200 * time.Millisecond

type tlsDialResult struct {
	conn stdnet.Conn
	err  error
}

// NewIPv4TLSFallbackTransport 创建仅使用 IPv4 的 HTTPS Transport，并在多个 A 记录之间进行 TLS 级回退。
//
// 普通 net.Dialer 只会在 TCP 建连阶段选择地址；如果某个 IPv4 已经 TCP 连接成功、但 TLS 握手卡住，
// Go 不会自动换到同一域名的其它 IPv4。该 Transport 会并行错峰尝试所有 IPv4，首个完成 TLS 握手的连接胜出。
// 这用于部分阿里边缘节点在特定 VPS 网络路径上出现 TLS handshake timeout 的兼容场景。
func NewIPv4TLSFallbackTransport(tlsConfig *tls.Config, perAttemptTimeout time.Duration, forceHTTP1 bool) *http.Transport {
	if perAttemptTimeout <= 0 {
		perAttemptTimeout = 4 * time.Second
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	if tlsConfig != nil {
		transport.TLSClientConfig = tlsConfig.Clone()
	} else if transport.TLSClientConfig != nil {
		transport.TLSClientConfig = transport.TLSClientConfig.Clone()
	}

	if forceHTTP1 {
		transport.ForceAttemptHTTP2 = false
		transport.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
	}

	transport.DialTLSContext = func(ctx context.Context, _ string, address string) (stdnet.Conn, error) {
		host, port, err := stdnet.SplitHostPort(address)
		if err != nil {
			return nil, err
		}

		candidates, err := lookupIPv4Candidates(ctx, host)
		if err != nil {
			return nil, err
		}
		return dialTLSIPv4Candidates(ctx, host, port, candidates, transport.TLSClientConfig, perAttemptTimeout, forceHTTP1)
	}

	return transport
}

func lookupIPv4Candidates(ctx context.Context, host string) ([]netip.Addr, error) {
	if ip, err := netip.ParseAddr(host); err == nil {
		if ip.Is4() {
			return []netip.Addr{ip}, nil
		}
		return nil, fmt.Errorf("IPv4 TLS fallback does not support IPv6 literal host")
	}

	resolved, err := stdnet.DefaultResolver.LookupNetIP(ctx, "ip4", host)
	if err != nil {
		return nil, err
	}

	seen := make(map[netip.Addr]struct{}, len(resolved))
	candidates := make([]netip.Addr, 0, len(resolved))
	for _, ip := range resolved {
		if !ip.Is4() {
			continue
		}
		if _, ok := seen[ip]; ok {
			continue
		}
		seen[ip] = struct{}{}
		candidates = append(candidates, ip)
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no IPv4 address resolved")
	}
	return candidates, nil
}

func dialTLSIPv4Candidates(
	ctx context.Context,
	host string,
	port string,
	candidates []netip.Addr,
	baseTLSConfig *tls.Config,
	perAttemptTimeout time.Duration,
	forceHTTP1 bool,
) (stdnet.Conn, error) {
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no IPv4 TLS candidates")
	}

	raceCtx, cancelRace := context.WithCancel(ctx)
	defer cancelRace()

	results := make(chan tlsDialResult, len(candidates))
	for i, ip := range candidates {
		i := i
		ip := ip
		go func() {
			if i > 0 {
				timer := time.NewTimer(time.Duration(i) * ipv4TLSFallbackStagger)
				defer timer.Stop()
				select {
				case <-raceCtx.Done():
					return
				case <-timer.C:
				}
			}

			attemptCtx, cancelAttempt := context.WithTimeout(raceCtx, perAttemptTimeout)
			defer cancelAttempt()

			dialer := &stdnet.Dialer{
				Timeout:   perAttemptTimeout,
				KeepAlive: 30 * time.Second,
			}
			rawConn, err := dialer.DialContext(attemptCtx, "tcp4", stdnet.JoinHostPort(ip.String(), port))
			if err != nil {
				results <- tlsDialResult{err: err}
				return
			}

			tlsConfig := &tls.Config{}
			if baseTLSConfig != nil {
				tlsConfig = baseTLSConfig.Clone()
			}
			if tlsConfig.ServerName == "" {
				tlsConfig.ServerName = host
			}
			if forceHTTP1 {
				tlsConfig.NextProtos = []string{"http/1.1"}
			}

			tlsConn := tls.Client(rawConn, tlsConfig)
			if err := tlsConn.HandshakeContext(attemptCtx); err != nil {
				rawConn.Close()
				results <- tlsDialResult{err: err}
				return
			}

			select {
			case results <- tlsDialResult{conn: tlsConn}:
			case <-raceCtx.Done():
				tlsConn.Close()
			}
		}()
	}

	var lastErr error
	for range candidates {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case result := <-results:
			if result.err == nil && result.conn != nil {
				cancelRace()
				return result.conn, nil
			}
			if result.err != nil {
				lastErr = result.err
			}
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("IPv4 TLS fallback exhausted")
	}
	return nil, lastErr
}

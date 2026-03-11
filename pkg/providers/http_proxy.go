package providers

import (
	"bufio"
	"context"
	stdtls "crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	xproxy "golang.org/x/net/proxy"
)

func normalizeOptionalProxyURL(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil
	}
	if !strings.Contains(value, "://") {
		value = "http://" + value
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return "", fmt.Errorf("invalid network proxy: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid network proxy: host is required")
	}
	switch strings.ToLower(strings.TrimSpace(parsed.Scheme)) {
	case "http", "https", "socks5", "socks5h":
		return parsed.String(), nil
	default:
		return "", fmt.Errorf("invalid network proxy: unsupported scheme %q", parsed.Scheme)
	}
}

func maskedProxyURL(raw string) string {
	normalized, err := normalizeOptionalProxyURL(raw)
	if err != nil || normalized == "" {
		return ""
	}
	parsed, err := url.Parse(normalized)
	if err != nil {
		return ""
	}
	if parsed.User != nil {
		username := parsed.User.Username()
		if username != "" {
			parsed.User = url.UserPassword(username, "***")
		} else {
			parsed.User = url.User("***")
		}
	}
	return parsed.String()
}

func proxyDialContext(proxyRaw string) (func(context.Context, string, string) (net.Conn, error), error) {
	normalized, err := normalizeOptionalProxyURL(proxyRaw)
	if err != nil {
		return nil, err
	}
	if normalized == "" {
		dialer := &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
		return dialer.DialContext, nil
	}
	parsed, err := url.Parse(normalized)
	if err != nil {
		return nil, err
	}
	switch strings.ToLower(strings.TrimSpace(parsed.Scheme)) {
	case "socks5", "socks5h":
		base := &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
		dialer, err := xproxy.FromURL(parsed, base)
		if err != nil {
			return nil, fmt.Errorf("configure socks proxy failed: %w", err)
		}
		if ctxDialer, ok := dialer.(xproxy.ContextDialer); ok {
			return ctxDialer.DialContext, nil
		}
		return func(ctx context.Context, network, addr string) (net.Conn, error) {
			type dialResult struct {
				conn net.Conn
				err  error
			}
			ch := make(chan dialResult, 1)
			go func() {
				conn, err := dialer.Dial(network, addr)
				ch <- dialResult{conn: conn, err: err}
			}()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case res := <-ch:
				return res.conn, res.err
			}
		}, nil
	case "http", "https":
		base := &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
		return func(ctx context.Context, network, addr string) (net.Conn, error) {
			conn, err := base.DialContext(ctx, "tcp", parsed.Host)
			if err != nil {
				return nil, err
			}
			if strings.EqualFold(parsed.Scheme, "https") {
				tlsConn := stdtls.Client(conn, &stdtls.Config{ServerName: parsed.Hostname()})
				if err := tlsConn.HandshakeContext(ctx); err != nil {
					_ = conn.Close()
					return nil, err
				}
				conn = tlsConn
			}
			connectReq := buildProxyConnectRequest(parsed, addr)
			if _, err := conn.Write([]byte(connectReq)); err != nil {
				_ = conn.Close()
				return nil, err
			}
			br := bufio.NewReader(conn)
			resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodConnect})
			if err != nil {
				_ = conn.Close()
				return nil, err
			}
			defer resp.Body.Close()
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				_ = conn.Close()
				return nil, fmt.Errorf("proxy connect failed: status=%d", resp.StatusCode)
			}
			return conn, nil
		}, nil
	default:
		return nil, fmt.Errorf("invalid network proxy: unsupported scheme %q", parsed.Scheme)
	}
}

func buildProxyConnectRequest(proxyURL *url.URL, targetAddr string) string {
	var b strings.Builder
	b.WriteString("CONNECT ")
	b.WriteString(targetAddr)
	b.WriteString(" HTTP/1.1\r\nHost: ")
	b.WriteString(targetAddr)
	b.WriteString("\r\n")
	if proxyURL != nil && proxyURL.User != nil {
		username := proxyURL.User.Username()
		password, _ := proxyURL.User.Password()
		encoded := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
		b.WriteString("Proxy-Authorization: Basic ")
		b.WriteString(encoded)
		b.WriteString("\r\n")
	}
	b.WriteString("\r\n")
	return b.String()
}

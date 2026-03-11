package providers

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	tls "github.com/refraction-networking/utls"
	"golang.org/x/net/http2"
)

type anthropicOAuthRoundTripper struct {
	mu          sync.Mutex
	connections map[string]*http2.ClientConn
	pending     map[string]*sync.Cond
	dialer      net.Dialer
}

func newAnthropicOAuthHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: newAnthropicOAuthRoundTripper(),
	}
}

func newAnthropicOAuthRoundTripper() *anthropicOAuthRoundTripper {
	return &anthropicOAuthRoundTripper{
		connections: map[string]*http2.ClientConn{},
		pending:     map[string]*sync.Cond{},
		dialer: net.Dialer{
			Timeout:   15 * time.Second,
			KeepAlive: 30 * time.Second,
		},
	}
}

func (t *anthropicOAuthRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	host := req.URL.Hostname()
	addr := req.URL.Host
	if !strings.Contains(addr, ":") {
		addr += ":443"
	}
	conn, err := t.getOrCreateConnection(host, addr)
	if err != nil {
		return nil, err
	}
	resp, err := conn.RoundTrip(req)
	if err != nil {
		t.mu.Lock()
		if cached, ok := t.connections[host]; ok && cached == conn {
			delete(t.connections, host)
		}
		t.mu.Unlock()
		return nil, err
	}
	return resp, nil
}

func (t *anthropicOAuthRoundTripper) getOrCreateConnection(host, addr string) (*http2.ClientConn, error) {
	t.mu.Lock()
	if conn, ok := t.connections[host]; ok && conn.CanTakeNewRequest() {
		t.mu.Unlock()
		return conn, nil
	}
	if wait, ok := t.pending[host]; ok {
		wait.Wait()
		if conn, ok := t.connections[host]; ok && conn.CanTakeNewRequest() {
			t.mu.Unlock()
			return conn, nil
		}
	}
	wait := sync.NewCond(&t.mu)
	t.pending[host] = wait
	t.mu.Unlock()

	conn, err := t.createConnection(host, addr)

	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.pending, host)
	wait.Broadcast()
	if err != nil {
		return nil, err
	}
	t.connections[host] = conn
	return conn, nil
}

func (t *anthropicOAuthRoundTripper) createConnection(host, addr string) (*http2.ClientConn, error) {
	rawConn, err := t.dialer.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	tlsConn := tls.UClient(rawConn, &tls.Config{ServerName: host}, tls.HelloChrome_Auto)
	if err := tlsConn.Handshake(); err != nil {
		_ = rawConn.Close()
		return nil, err
	}
	h2Conn, err := (&http2.Transport{}).NewClientConn(tlsConn)
	if err != nil {
		_ = tlsConn.Close()
		return nil, err
	}
	return h2Conn, nil
}

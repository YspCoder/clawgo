package nodes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Transport abstracts node data-plane delivery.
type Transport interface {
	Name() string
	Send(ctx context.Context, req Request) (Response, error)
}

// Router prefers p2p transport and falls back to relay.
type Router struct {
	P2P   Transport
	Relay Transport
}

func (r *Router) Dispatch(ctx context.Context, req Request, mode string) (Response, error) {
	m := strings.ToLower(strings.TrimSpace(mode))
	if m == "" {
		m = "auto"
	}
	switch m {
	case "p2p":
		if r.P2P == nil {
			return Response{OK: false, Node: req.Node, Action: req.Action, Error: "p2p transport unavailable"}, nil
		}
		return r.P2P.Send(ctx, req)
	case "relay":
		if r.Relay == nil {
			return Response{OK: false, Node: req.Node, Action: req.Action, Error: "relay transport unavailable"}, nil
		}
		return r.Relay.Send(ctx, req)
	default: // auto
		if r.P2P != nil {
			if resp, err := r.P2P.Send(ctx, req); err == nil && resp.OK {
				return resp, nil
			}
		}
		if r.Relay != nil {
			return r.Relay.Send(ctx, req)
		}
		return Response{}, fmt.Errorf("no transport available")
	}
}

// StubP2PTransport provides phase-2 negotiation scaffold.
type StubP2PTransport struct{}

func (s *StubP2PTransport) Name() string { return "p2p" }
func (s *StubP2PTransport) Send(ctx context.Context, req Request) (Response, error) {
	_ = ctx
	return Response{OK: false, Node: req.Node, Action: req.Action, Error: "p2p session not established yet"}, nil
}

// HTTPRelayTransport dispatches requests to node-agent endpoints over HTTP.
type HTTPRelayTransport struct {
	Manager *Manager
	Client  *http.Client
}

func (s *HTTPRelayTransport) Name() string { return "relay" }
func (s *HTTPRelayTransport) Send(ctx context.Context, req Request) (Response, error) {
	if s.Manager == nil {
		return Response{OK: false, Node: req.Node, Action: req.Action, Error: "relay manager not configured"}, nil
	}
	if resp, ok := s.Manager.Invoke(req); ok {
		return resp, nil
	}
	n, ok := s.Manager.Get(req.Node)
	if !ok {
		return Response{OK: false, Node: req.Node, Action: req.Action, Error: "node not found"}, nil
	}
	endpoint := strings.TrimRight(strings.TrimSpace(n.Endpoint), "/")
	if endpoint == "" {
		return Response{OK: false, Node: req.Node, Action: req.Action, Error: "node endpoint not configured"}, nil
	}
	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	body, _ := json.Marshal(req)
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/invoke", bytes.NewReader(body))
	if err != nil {
		return Response{}, err
	}
	hreq.Header.Set("Content-Type", "application/json")
	if tok := strings.TrimSpace(n.Token); tok != "" {
		hreq.Header.Set("Authorization", "Bearer "+tok)
	}
	hresp, err := client.Do(hreq)
	if err != nil {
		return Response{OK: false, Node: req.Node, Action: req.Action, Error: err.Error()}, nil
	}
	defer hresp.Body.Close()
	payload, _ := io.ReadAll(io.LimitReader(hresp.Body, 1<<20))
	var resp Response
	if err := json.Unmarshal(payload, &resp); err != nil {
		return Response{OK: false, Node: req.Node, Action: req.Action, Error: fmt.Sprintf("invalid node response: %s", strings.TrimSpace(string(payload)))}, nil
	}
	if strings.TrimSpace(resp.Node) == "" {
		resp.Node = req.Node
	}
	if strings.TrimSpace(resp.Action) == "" {
		resp.Action = req.Action
	}
	return resp, nil
}

package nodes

import (
	"context"
	"fmt"
	"strings"
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

// StubRelayTransport provides executable placeholder until real bridge lands.
type StubRelayTransport struct{}

func (s *StubRelayTransport) Name() string { return "relay" }
func (s *StubRelayTransport) Send(ctx context.Context, req Request) (Response, error) {
	_ = ctx
	return Response{OK: false, Node: req.Node, Action: req.Action, Error: "relay bridge not implemented yet"}, nil
}

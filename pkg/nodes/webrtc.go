package nodes

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
)

type gatewayRTCSession struct {
	nodeID  string
	pc      *webrtc.PeerConnection
	dc      *webrtc.DataChannel
	ready   chan struct{}
	readyMu sync.Once
	writeMu sync.Mutex
	pending map[string]chan Response
	mu      sync.Mutex
	nextID  uint64
}

func (s *gatewayRTCSession) markReady() {
	s.readyMu.Do(func() { close(s.ready) })
}

func (s *gatewayRTCSession) send(msg WireMessage) error {
	if s == nil || s.dc == nil {
		return fmt.Errorf("webrtc data channel unavailable")
	}
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.dc.Send(b)
}

func (s *gatewayRTCSession) nextRequestID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	return fmt.Sprintf("rtc-%s-%d", s.nodeID, s.nextID)
}

type WebRTCTransport struct {
	stunServers []string

	mu       sync.Mutex
	sessions map[string]*gatewayRTCSession
	signal   map[string]WireSender
}

func NewWebRTCTransport(stunServers []string) *WebRTCTransport {
	out := make([]string, 0, len(stunServers))
	for _, server := range stunServers {
		if v := strings.TrimSpace(server); v != "" {
			out = append(out, v)
		}
	}
	return &WebRTCTransport{
		stunServers: out,
		sessions:    map[string]*gatewayRTCSession{},
		signal:      map[string]WireSender{},
	}
}

func (t *WebRTCTransport) Name() string { return "p2p-webrtc" }

func (t *WebRTCTransport) Snapshot() map[string]interface{} {
	t.mu.Lock()
	defer t.mu.Unlock()
	nodes := make([]map[string]interface{}, 0, len(t.sessions))
	active := 0
	for nodeID, session := range t.sessions {
		status := "connecting"
		if session != nil && session.dc != nil && session.dc.ReadyState() == webrtc.DataChannelStateOpen {
			status = "open"
			active++
		}
		nodes = append(nodes, map[string]interface{}{
			"node":   nodeID,
			"status": status,
		})
	}
	return map[string]interface{}{
		"transport":       "webrtc",
		"active_sessions": active,
		"nodes":           nodes,
	}
}

func (t *WebRTCTransport) BindSignaler(nodeID string, sender WireSender) {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if sender == nil {
		delete(t.signal, nodeID)
		return
	}
	t.signal[nodeID] = sender
}

func (t *WebRTCTransport) UnbindSignaler(nodeID string) {
	t.BindSignaler(nodeID, nil)
	t.mu.Lock()
	session := t.sessions[nodeID]
	delete(t.sessions, nodeID)
	t.mu.Unlock()
	if session != nil && session.pc != nil {
		_ = session.pc.Close()
	}
}

func (t *WebRTCTransport) currentSignaler(nodeID string) WireSender {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.signal[strings.TrimSpace(nodeID)]
}

func (t *WebRTCTransport) HandleSignal(msg WireMessage) error {
	nodeID := strings.TrimSpace(msg.From)
	if nodeID == "" {
		return fmt.Errorf("signal missing from")
	}
	session, err := t.ensureSession(nodeID)
	if err != nil {
		return err
	}
	switch strings.ToLower(strings.TrimSpace(msg.Type)) {
	case "signal_answer":
		var desc webrtc.SessionDescription
		if err := mapInto(msg.Payload, &desc); err != nil {
			return err
		}
		return session.pc.SetRemoteDescription(desc)
	case "signal_candidate":
		var candidate webrtc.ICECandidateInit
		if err := mapInto(msg.Payload, &candidate); err != nil {
			return err
		}
		return session.pc.AddICECandidate(candidate)
	default:
		return fmt.Errorf("unsupported signal type: %s", msg.Type)
	}
}

func (t *WebRTCTransport) Send(ctx context.Context, req Request) (Response, error) {
	session, err := t.ensureSession(req.Node)
	if err != nil {
		return Response{OK: false, Code: "p2p_unavailable", Node: req.Node, Action: req.Action, Error: err.Error()}, nil
	}

	select {
	case <-ctx.Done():
		return Response{}, ctx.Err()
	case <-session.ready:
	case <-time.After(8 * time.Second):
		return Response{OK: false, Code: "p2p_timeout", Node: req.Node, Action: req.Action, Error: "webrtc session not ready"}, nil
	}

	reqID := session.nextRequestID()
	respCh := make(chan Response, 1)
	session.mu.Lock()
	session.pending[reqID] = respCh
	session.mu.Unlock()

	if err := session.send(WireMessage{
		Type:    "node_request",
		ID:      reqID,
		To:      req.Node,
		Request: &req,
	}); err != nil {
		session.mu.Lock()
		delete(session.pending, reqID)
		session.mu.Unlock()
		return Response{OK: false, Code: "p2p_send_failed", Node: req.Node, Action: req.Action, Error: err.Error()}, nil
	}

	select {
	case <-ctx.Done():
		session.mu.Lock()
		delete(session.pending, reqID)
		session.mu.Unlock()
		return Response{}, ctx.Err()
	case resp := <-respCh:
		return resp, nil
	}
}

func (t *WebRTCTransport) ensureSession(nodeID string) (*gatewayRTCSession, error) {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return nil, fmt.Errorf("node id required")
	}

	t.mu.Lock()
	if session := t.sessions[nodeID]; session != nil {
		t.mu.Unlock()
		return session, nil
	}
	t.mu.Unlock()
	if t.currentSignaler(nodeID) == nil {
		return nil, fmt.Errorf("node %s signaling unavailable", nodeID)
	}

	config := webrtc.Configuration{}
	if len(t.stunServers) > 0 {
		config.ICEServers = []webrtc.ICEServer{{URLs: append([]string(nil), t.stunServers...)}}
	}
	pc, err := webrtc.NewPeerConnection(config)
	if err != nil {
		return nil, err
	}
	dc, err := pc.CreateDataChannel("clawgo", nil)
	if err != nil {
		_ = pc.Close()
		return nil, err
	}
	session := &gatewayRTCSession{
		nodeID:  nodeID,
		pc:      pc,
		dc:      dc,
		ready:   make(chan struct{}),
		pending: map[string]chan Response{},
	}

	pc.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate == nil {
			return
		}
		sender := t.currentSignaler(nodeID)
		if sender == nil {
			return
		}
		_ = sender.Send(WireMessage{
			Type:    "signal_candidate",
			From:    "gateway",
			To:      nodeID,
			Session: nodeID,
			Payload: structToMap(candidate.ToJSON()),
		})
	})
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		switch state {
		case webrtc.PeerConnectionStateFailed, webrtc.PeerConnectionStateClosed, webrtc.PeerConnectionStateDisconnected:
			t.mu.Lock()
			if t.sessions[nodeID] == session {
				delete(t.sessions, nodeID)
			}
			t.mu.Unlock()
		}
	})
	dc.OnOpen(func() {
		session.markReady()
	})
	dc.OnMessage(func(message webrtc.DataChannelMessage) {
		var msg WireMessage
		if err := json.Unmarshal(message.Data, &msg); err != nil {
			return
		}
		if strings.ToLower(strings.TrimSpace(msg.Type)) != "node_response" || msg.Response == nil {
			return
		}
		session.mu.Lock()
		respCh := session.pending[msg.ID]
		if respCh != nil {
			delete(session.pending, msg.ID)
		}
		session.mu.Unlock()
		if respCh != nil {
			respCh <- *msg.Response
		}
	})

	offer, err := pc.CreateOffer(nil)
	if err != nil {
		_ = pc.Close()
		return nil, err
	}
	if err := pc.SetLocalDescription(offer); err != nil {
		_ = pc.Close()
		return nil, err
	}

	t.mu.Lock()
	if existing := t.sessions[nodeID]; existing != nil {
		t.mu.Unlock()
		_ = pc.Close()
		return existing, nil
	}
	t.sessions[nodeID] = session
	t.mu.Unlock()

	sender := t.currentSignaler(nodeID)
	if sender == nil {
		t.mu.Lock()
		delete(t.sessions, nodeID)
		t.mu.Unlock()
		_ = pc.Close()
		return nil, fmt.Errorf("node %s signaling unavailable", nodeID)
	}
	if err := sender.Send(WireMessage{
		Type:    "signal_offer",
		From:    "gateway",
		To:      nodeID,
		Session: nodeID,
		Payload: structToMap(*pc.LocalDescription()),
	}); err != nil {
		t.mu.Lock()
		delete(t.sessions, nodeID)
		t.mu.Unlock()
		_ = pc.Close()
		return nil, err
	}
	return session, nil
}

func structToMap(v interface{}) map[string]interface{} {
	b, _ := json.Marshal(v)
	var out map[string]interface{}
	_ = json.Unmarshal(b, &out)
	if out == nil {
		out = map[string]interface{}{}
	}
	return out
}

func mapInto(in map[string]interface{}, out interface{}) error {
	if len(in) == 0 {
		return fmt.Errorf("empty payload")
	}
	b, err := json.Marshal(in)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

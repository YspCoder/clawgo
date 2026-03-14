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
	nodeID      string
	pc          *webrtc.PeerConnection
	dc          *webrtc.DataChannel
	ready       chan struct{}
	readyMu     sync.Once
	writeMu     sync.Mutex
	pending     map[string]chan Response
	mu          sync.Mutex
	nextID      uint64
	status      string
	lastError   string
	retryCount  int
	createdAt   time.Time
	lastAttempt time.Time
	lastReadyAt time.Time
}

func (s *gatewayRTCSession) markReady() {
	s.mu.Lock()
	s.status = "open"
	s.lastReadyAt = time.Now().UTC()
	s.mu.Unlock()
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

func (s *gatewayRTCSession) setStatus(status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = strings.TrimSpace(status)
}

func (s *gatewayRTCSession) setLastError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err == nil {
		s.lastError = ""
		return
	}
	s.lastError = strings.TrimSpace(err.Error())
}

func (s *gatewayRTCSession) snapshot() map[string]interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	status := s.status
	if status == "" {
		status = "connecting"
	}
	return map[string]interface{}{
		"node":          s.nodeID,
		"status":        status,
		"last_error":    s.lastError,
		"retry_count":   s.retryCount,
		"created_at":    s.createdAt,
		"last_attempt":  s.lastAttempt,
		"last_ready_at": s.lastReadyAt,
	}
}

type WebRTCTransport struct {
	iceServers []webrtc.ICEServer

	mu       sync.Mutex
	sessions map[string]*gatewayRTCSession
	signal   map[string]WireSender
}

func NewWebRTCTransport(stunServers []string, extraICEServers ...webrtc.ICEServer) *WebRTCTransport {
	out := make([]webrtc.ICEServer, 0, len(stunServers)+len(extraICEServers))
	for _, server := range stunServers {
		if v := strings.TrimSpace(server); v != "" {
			out = append(out, webrtc.ICEServer{URLs: []string{v}})
		}
	}
	for _, server := range extraICEServers {
		urls := make([]string, 0, len(server.URLs))
		for _, raw := range server.URLs {
			if v := strings.TrimSpace(raw); v != "" {
				urls = append(urls, v)
			}
		}
		if len(urls) == 0 {
			continue
		}
		out = append(out, webrtc.ICEServer{
			URLs:       urls,
			Username:   strings.TrimSpace(server.Username),
			Credential: server.Credential,
		})
	}
	return &WebRTCTransport{
		iceServers: out,
		sessions:   map[string]*gatewayRTCSession{},
		signal:     map[string]WireSender{},
	}
}

func (t *WebRTCTransport) Name() string { return "p2p-webrtc" }

func (t *WebRTCTransport) Snapshot() map[string]interface{} {
	t.mu.Lock()
	defer t.mu.Unlock()
	nodes := make([]map[string]interface{}, 0, len(t.sessions))
	active := 0
	for nodeID, session := range t.sessions {
		if session != nil && session.dc != nil && session.dc.ReadyState() == webrtc.DataChannelStateOpen {
			active++
		}
		if session == nil {
			nodes = append(nodes, map[string]interface{}{"node": nodeID, "status": "unknown"})
			continue
		}
		nodes = append(nodes, session.snapshot())
	}
	return map[string]interface{}{
		"transport":       "webrtc",
		"active_sessions": active,
		"ice_servers":     len(t.iceServers),
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
		session.setStatus("offline")
		_ = session.pc.Close()
	}
}

func (t *WebRTCTransport) currentSignaler(nodeID string) WireSender {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.signal[strings.TrimSpace(nodeID)]
}

var webRTCSignalHandlers = map[string]func(*gatewayRTCSession, WireMessage) error{
	"signal_answer": func(session *gatewayRTCSession, msg WireMessage) error {
		var desc webrtc.SessionDescription
		if err := mapInto(msg.Payload, &desc); err != nil {
			return err
		}
		return session.pc.SetRemoteDescription(desc)
	},
	"signal_candidate": func(session *gatewayRTCSession, msg WireMessage) error {
		var candidate webrtc.ICECandidateInit
		if err := mapInto(msg.Payload, &candidate); err != nil {
			return err
		}
		return session.pc.AddICECandidate(candidate)
	},
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
	if handler := webRTCSignalHandlers[strings.ToLower(strings.TrimSpace(msg.Type))]; handler != nil {
		return handler(session, msg)
	}
	return fmt.Errorf("unsupported signal type: %s", msg.Type)
}

func (t *WebRTCTransport) Send(ctx context.Context, req Request) (Response, error) {
	session, err := t.ensureSession(req.Node)
	if err != nil {
		return Response{OK: false, Code: "p2p_unavailable", Node: req.Node, Action: req.Action, Error: err.Error()}, nil
	}
	session.setLastError(nil)

	select {
	case <-ctx.Done():
		session.setStatus("cancelled")
		session.setLastError(ctx.Err())
		return Response{}, ctx.Err()
	case <-session.ready:
	case <-time.After(8 * time.Second):
		session.setStatus("timeout")
		session.setLastError(fmt.Errorf("webrtc session not ready"))
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
		session.setStatus("send_failed")
		session.setLastError(err)
		return Response{OK: false, Code: "p2p_send_failed", Node: req.Node, Action: req.Action, Error: err.Error()}, nil
	}

	select {
	case <-ctx.Done():
		session.mu.Lock()
		delete(session.pending, reqID)
		session.mu.Unlock()
		session.setStatus("cancelled")
		session.setLastError(ctx.Err())
		return Response{}, ctx.Err()
	case resp := <-respCh:
		if resp.OK {
			session.setStatus("open")
			session.setLastError(nil)
		} else if strings.TrimSpace(resp.Error) != "" {
			session.setStatus("remote_error")
			session.setLastError(fmt.Errorf("%s", strings.TrimSpace(resp.Error)))
		}
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
		session.mu.Lock()
		session.retryCount++
		session.lastAttempt = time.Now().UTC()
		session.mu.Unlock()
		t.mu.Unlock()
		return session, nil
	}
	t.mu.Unlock()
	if t.currentSignaler(nodeID) == nil {
		return nil, fmt.Errorf("node %s signaling unavailable", nodeID)
	}

	config := webrtc.Configuration{}
	if len(t.iceServers) > 0 {
		config.ICEServers = append([]webrtc.ICEServer(nil), t.iceServers...)
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
		nodeID:      nodeID,
		pc:          pc,
		dc:          dc,
		ready:       make(chan struct{}),
		pending:     map[string]chan Response{},
		status:      "connecting",
		createdAt:   time.Now().UTC(),
		lastAttempt: time.Now().UTC(),
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
		session.setStatus(strings.ToLower(strings.TrimSpace(state.String())))
		switch state {
		case webrtc.PeerConnectionStateFailed, webrtc.PeerConnectionStateClosed, webrtc.PeerConnectionStateDisconnected:
			session.setLastError(fmt.Errorf("peer connection state: %s", state.String()))
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
	dc.OnError(func(err error) {
		session.setStatus("channel_error")
		session.setLastError(err)
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
		session.setStatus("signal_unavailable")
		session.setLastError(fmt.Errorf("node %s signaling unavailable", nodeID))
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
		session.setStatus("signal_failed")
		session.setLastError(err)
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

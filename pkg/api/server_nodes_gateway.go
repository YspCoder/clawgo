package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/YspCoder/clawgo/pkg/nodes"
	"github.com/gorilla/websocket"
)

func (s *Server) SetNodeWebRTCTransport(t *nodes.WebRTCTransport) {
	s.nodeWebRTC = t
}

func (s *Server) SetNodeP2PStatusHandler(fn func() map[string]interface{}) {
	s.nodeP2PStatus = fn
}

func (s *Server) rememberNodeConnection(nodeID, connID string) {
	nodeID = strings.TrimSpace(nodeID)
	connID = strings.TrimSpace(connID)
	if nodeID == "" || connID == "" {
		return
	}
	s.nodeConnMu.Lock()
	defer s.nodeConnMu.Unlock()
	s.nodeConnIDs[nodeID] = connID
}

func (s *Server) bindNodeSocket(nodeID, connID string, conn *websocket.Conn) {
	nodeID = strings.TrimSpace(nodeID)
	connID = strings.TrimSpace(connID)
	if nodeID == "" || connID == "" || conn == nil {
		return
	}
	next := &nodeSocketConn{connID: connID, conn: conn}
	s.nodeConnMu.Lock()
	prev := s.nodeSockets[nodeID]
	s.nodeSockets[nodeID] = next
	s.nodeConnMu.Unlock()
	if s.mgr != nil {
		s.mgr.RegisterWireSender(nodeID, next)
	}
	if s.nodeWebRTC != nil {
		s.nodeWebRTC.BindSignaler(nodeID, next)
	}
	if prev != nil && prev.connID != connID {
		_ = prev.conn.Close()
	}
}

func (s *Server) releaseNodeConnection(nodeID, connID string) bool {
	nodeID = strings.TrimSpace(nodeID)
	connID = strings.TrimSpace(connID)
	if nodeID == "" || connID == "" {
		return false
	}
	s.nodeConnMu.Lock()
	defer s.nodeConnMu.Unlock()
	if s.nodeConnIDs[nodeID] != connID {
		return false
	}
	delete(s.nodeConnIDs, nodeID)
	if sock := s.nodeSockets[nodeID]; sock != nil && sock.connID == connID {
		delete(s.nodeSockets, nodeID)
	}
	if s.mgr != nil {
		s.mgr.RegisterWireSender(nodeID, nil)
	}
	if s.nodeWebRTC != nil {
		s.nodeWebRTC.UnbindSignaler(nodeID)
	}
	return true
}

func (s *Server) getNodeSocket(nodeID string) *nodeSocketConn {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return nil
	}
	s.nodeConnMu.Lock()
	defer s.nodeConnMu.Unlock()
	return s.nodeSockets[nodeID]
}

func (s *Server) sendNodeSocketMessage(nodeID string, msg nodes.WireMessage) error {
	sock := s.getNodeSocket(nodeID)
	if sock == nil || sock.conn == nil {
		return fmt.Errorf("node %s not connected", strings.TrimSpace(nodeID))
	}
	return sock.writeJSON(msg)
}

func (s *Server) handleNodeConnect(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if s.mgr == nil {
		http.Error(w, "nodes manager unavailable", http.StatusInternalServerError)
		return
	}
	conn, err := nodesWebsocketUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	var connectedID string
	connID := fmt.Sprintf("%d", time.Now().UnixNano())
	_ = conn.SetReadDeadline(time.Now().Add(90 * time.Second))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(90 * time.Second))
	})

	writeAck := func(ack nodes.WireAck) error {
		if strings.TrimSpace(connectedID) != "" {
			if sock := s.getNodeSocket(connectedID); sock != nil && sock.connID == connID && sock.conn == conn {
				return sock.writeJSON(ack)
			}
		}
		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		return conn.WriteJSON(ack)
	}

	defer func() {
		if strings.TrimSpace(connectedID) != "" && s.releaseNodeConnection(connectedID, connID) {
			s.mgr.MarkOffline(connectedID)
		}
	}()

	for {
		var msg nodes.WireMessage
		if err := conn.ReadJSON(&msg); err != nil {
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		if s.mgr != nil && s.mgr.HandleWireMessage(msg) {
			continue
		}
		type nodeSocketHandler func(nodes.WireMessage) bool
		handlers := map[string]nodeSocketHandler{
			"register": func(msg nodes.WireMessage) bool {
				if msg.Node == nil || strings.TrimSpace(msg.Node.ID) == "" {
					_ = writeAck(nodes.WireAck{OK: false, Type: "register", Error: "node.id required"})
					return true
				}
				s.mgr.Upsert(*msg.Node)
				connectedID = strings.TrimSpace(msg.Node.ID)
				s.rememberNodeConnection(connectedID, connID)
				s.bindNodeSocket(connectedID, connID, conn)
				return writeAck(nodes.WireAck{OK: true, Type: "registered", ID: connectedID}) == nil
			},
			"heartbeat": func(msg nodes.WireMessage) bool {
				id := strings.TrimSpace(msg.ID)
				if id == "" {
					id = connectedID
				}
				if id == "" {
					_ = writeAck(nodes.WireAck{OK: false, Type: "heartbeat", Error: "id required"})
					return true
				}
				if msg.Node != nil && strings.TrimSpace(msg.Node.ID) != "" {
					s.mgr.Upsert(*msg.Node)
					connectedID = strings.TrimSpace(msg.Node.ID)
					s.rememberNodeConnection(connectedID, connID)
					s.bindNodeSocket(connectedID, connID, conn)
				} else if n, ok := s.mgr.Get(id); ok {
					s.mgr.Upsert(n)
					connectedID = id
					s.rememberNodeConnection(connectedID, connID)
					s.bindNodeSocket(connectedID, connID, conn)
				} else {
					_ = writeAck(nodes.WireAck{OK: false, Type: "heartbeat", ID: id, Error: "node not found"})
					return true
				}
				return writeAck(nodes.WireAck{OK: true, Type: "heartbeat", ID: connectedID}) == nil
			},
			"signal_offer":     func(msg nodes.WireMessage) bool { return s.handleNodeSignalMessage(msg, connectedID, writeAck) },
			"signal_answer":    func(msg nodes.WireMessage) bool { return s.handleNodeSignalMessage(msg, connectedID, writeAck) },
			"signal_candidate": func(msg nodes.WireMessage) bool { return s.handleNodeSignalMessage(msg, connectedID, writeAck) },
		}
		if handler := handlers[strings.ToLower(strings.TrimSpace(msg.Type))]; handler != nil {
			if !handler(msg) {
				return
			}
			continue
		}
		if err := writeAck(nodes.WireAck{OK: false, Type: msg.Type, ID: msg.ID, Error: "unsupported message type"}); err != nil {
			return
		}
	}
}

func (s *Server) handleNodeSignalMessage(msg nodes.WireMessage, connectedID string, writeAck func(nodes.WireAck) error) bool {
	targetID := strings.TrimSpace(msg.To)
	if s.nodeWebRTC != nil && (targetID == "" || strings.EqualFold(targetID, "gateway")) {
		if err := s.nodeWebRTC.HandleSignal(msg); err != nil {
			_ = writeAck(nodes.WireAck{OK: false, Type: msg.Type, ID: msg.ID, Error: err.Error()})
			return true
		}
		return writeAck(nodes.WireAck{OK: true, Type: "signaled", ID: msg.ID}) == nil
	}
	if strings.TrimSpace(connectedID) == "" {
		_ = writeAck(nodes.WireAck{OK: false, Type: msg.Type, Error: "node not registered"})
		return true
	}
	if targetID == "" {
		_ = writeAck(nodes.WireAck{OK: false, Type: msg.Type, ID: msg.ID, Error: "target node required"})
		return true
	}
	msg.From = connectedID
	if err := s.sendNodeSocketMessage(targetID, msg); err != nil {
		_ = writeAck(nodes.WireAck{OK: false, Type: msg.Type, ID: msg.ID, Error: err.Error()})
		return true
	}
	return writeAck(nodes.WireAck{OK: true, Type: "relayed", ID: msg.ID}) == nil
}

func (s *Server) handleWebUINodes(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	switch r.Method {
	case http.MethodGet:
		payload := s.webUINodesPayload(r.Context())
		payload["ok"] = true
		writeJSON(w, payload)
	case http.MethodPost:
		var body struct {
			Action string `json:"action"`
			ID     string `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		action := strings.ToLower(body.Action)
		if action != "delete" {
			http.Error(w, "unsupported action", http.StatusBadRequest)
			return
		}
		if s.mgr == nil {
			http.Error(w, "nodes manager unavailable", http.StatusInternalServerError)
			return
		}
		id := body.ID
		ok := s.mgr.Remove(id)
		writeJSON(w, map[string]interface{}{"ok": true, "deleted": ok, "id": id})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleWebUINodeDispatches(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	limit := queryBoundedPositiveInt(r, "limit", 50, 500)
	writeJSON(w, map[string]interface{}{
		"ok":    true,
		"items": s.webUINodesDispatchPayload(limit),
	})
}

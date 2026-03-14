package nodes

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
)

type captureWireSender struct {
	send func(msg WireMessage) error
}

func (c *captureWireSender) Send(msg WireMessage) error {
	if c.send != nil {
		return c.send(msg)
	}
	return nil
}

func TestWebsocketP2PTransportSend(t *testing.T) {
	t.Parallel()

	manager := NewManager()
	manager.Upsert(NodeInfo{
		ID:     "edge-dev",
		Online: true,
		Capabilities: Capabilities{
			Run: true,
		},
	})
	manager.RegisterWireSender("edge-dev", &captureWireSender{
		send: func(msg WireMessage) error {
			if msg.Type != "node_request" || msg.Request == nil || msg.Request.Action != "run" {
				t.Fatalf("unexpected wire request: %+v", msg)
			}
			go func() {
				time.Sleep(20 * time.Millisecond)
				manager.HandleWireMessage(WireMessage{
					Type: "node_response",
					ID:   msg.ID,
					Response: &Response{
						OK:     true,
						Code:   "ok",
						Node:   "edge-dev",
						Action: "run",
						Payload: map[string]interface{}{
							"status": "done",
						},
					},
				})
			}()
			return nil
		},
	})

	transport := &WebsocketP2PTransport{Manager: manager}
	resp, err := transport.Send(context.Background(), Request{
		Action: "run",
		Node:   "edge-dev",
		Args:   map[string]interface{}{"command": []string{"echo", "ok"}},
	})
	if err != nil {
		t.Fatalf("transport send failed: %v", err)
	}
	if !resp.OK || resp.Node != "edge-dev" || resp.Action != "run" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if resp.Payload["status"] != "done" {
		t.Fatalf("unexpected payload: %+v", resp.Payload)
	}
}

func TestNormalizeDevicePayloadBuildsArtifacts(t *testing.T) {
	t.Parallel()

	payload := normalizeDevicePayload("screen_snapshot", map[string]interface{}{
		"media_type": "image",
		"storage":    "path",
		"path":       "/tmp/screen.png",
		"mime_type":  "image/png",
	})
	artifacts, ok := payload["artifacts"].([]map[string]interface{})
	if !ok || len(artifacts) != 1 {
		t.Fatalf("expected one artifact, got %+v", payload["artifacts"])
	}
	if artifacts[0]["kind"] != "image" || artifacts[0]["path"] != "/tmp/screen.png" {
		t.Fatalf("unexpected artifact payload: %+v", artifacts[0])
	}
}

func TestNormalizeDevicePayloadNormalizesExistingArtifactRows(t *testing.T) {
	t.Parallel()

	payload := normalizeDevicePayload("screen_snapshot", map[string]interface{}{
		"artifacts": []map[string]interface{}{
			{
				"path":       "/tmp/screen.png",
				"kind":       "image",
				"size_bytes": "42",
			},
		},
	})
	artifacts, ok := payload["artifacts"].([]map[string]interface{})
	if !ok || len(artifacts) != 1 {
		t.Fatalf("expected one normalized artifact, got %+v", payload["artifacts"])
	}
	if got := artifacts[0]["size_bytes"]; got != int64(42) {
		t.Fatalf("expected normalized size_bytes, got %#v", got)
	}
}

func TestWebRTCTransportSendEndToEnd(t *testing.T) {
	t.Parallel()

	transport := NewWebRTCTransport(nil)
	nodeID := "edge-webrtc"

	var remotePC *webrtc.PeerConnection
	var remoteMu sync.Mutex
	handleRemoteSignal := func(msg WireMessage) error {
		remoteMu.Lock()
		defer remoteMu.Unlock()

		ensureRemote := func() (*webrtc.PeerConnection, error) {
			if remotePC != nil {
				return remotePC, nil
			}
			pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
			if err != nil {
				return nil, err
			}
			pc.OnICECandidate(func(candidate *webrtc.ICECandidate) {
				if candidate == nil {
					return
				}
				_ = transport.HandleSignal(WireMessage{
					Type:    "signal_candidate",
					From:    nodeID,
					To:      "gateway",
					Session: nodeID,
					Payload: structToMap(candidate.ToJSON()),
				})
			})
			pc.OnDataChannel(func(dc *webrtc.DataChannel) {
				dc.OnMessage(func(message webrtc.DataChannelMessage) {
					var wire WireMessage
					if err := json.Unmarshal(message.Data, &wire); err != nil {
						return
					}
					if wire.Type != "node_request" || wire.Request == nil {
						return
					}
					resp := Response{
						OK:     true,
						Code:   "ok",
						Node:   nodeID,
						Action: wire.Request.Action,
						Payload: map[string]interface{}{
							"status": "done-over-webrtc",
						},
					}
					b, err := json.Marshal(WireMessage{
						Type:     "node_response",
						ID:       wire.ID,
						From:     nodeID,
						To:       "gateway",
						Response: &resp,
					})
					if err != nil {
						return
					}
					_ = dc.Send(b)
				})
			})
			remotePC = pc
			return remotePC, nil
		}

		pc, err := ensureRemote()
		if err != nil {
			return err
		}

		switch msg.Type {
		case "signal_offer":
			var desc webrtc.SessionDescription
			if err := mapInto(msg.Payload, &desc); err != nil {
				return err
			}
			if err := pc.SetRemoteDescription(desc); err != nil {
				return err
			}
			answer, err := pc.CreateAnswer(nil)
			if err != nil {
				return err
			}
			if err := pc.SetLocalDescription(answer); err != nil {
				return err
			}
			return transport.HandleSignal(WireMessage{
				Type:    "signal_answer",
				From:    nodeID,
				To:      "gateway",
				Session: nodeID,
				Payload: structToMap(*pc.LocalDescription()),
			})
		case "signal_candidate":
			var candidate webrtc.ICECandidateInit
			if err := mapInto(msg.Payload, &candidate); err != nil {
				return err
			}
			return pc.AddICECandidate(candidate)
		default:
			return nil
		}
	}

	transport.BindSignaler(nodeID, &captureWireSender{
		send: handleRemoteSignal,
	})
	defer func() {
		transport.UnbindSignaler(nodeID)
		remoteMu.Lock()
		defer remoteMu.Unlock()
		if remotePC != nil {
			_ = remotePC.Close()
		}
	}()

	resp, err := transport.Send(context.Background(), Request{
		Action: "run",
		Node:   nodeID,
		Args:   map[string]interface{}{"command": []string{"echo", "ok"}},
	})
	if err != nil {
		t.Fatalf("webrtc transport send failed: %v", err)
	}
	if !resp.OK {
		t.Fatalf("expected ok response, got %+v", resp)
	}
	if resp.Payload["status"] != "done-over-webrtc" {
		t.Fatalf("unexpected payload: %+v", resp.Payload)
	}
	if resp.Payload["used_transport"] != nil {
		t.Fatalf("transport annotations should not be added at transport layer: %+v", resp.Payload)
	}

	snapshot := transport.Snapshot()
	if snapshot["active_sessions"] != 1 {
		t.Fatalf("expected one active session, got %+v", snapshot)
	}
	nodesRaw, _ := snapshot["nodes"].([]map[string]interface{})
	if len(nodesRaw) == 0 {
		t.Fatalf("expected node snapshots, got %+v", snapshot)
	}
	if nodesRaw[0]["status"] != "open" {
		t.Fatalf("expected open status, got %+v", nodesRaw[0])
	}
}

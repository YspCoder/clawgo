package nodes

import (
	"context"
	"testing"
	"time"
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

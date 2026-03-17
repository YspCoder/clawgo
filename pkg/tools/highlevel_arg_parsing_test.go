package tools

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/YspCoder/clawgo/pkg/nodes"
	"github.com/YspCoder/clawgo/pkg/providers"
)

func TestSessionsToolParsesStringArguments(t *testing.T) {
	t.Parallel()

	tool := NewSessionsTool(func(limit int) []SessionInfo {
		return []SessionInfo{
			{Key: "cron:1", Kind: "cron", UpdatedAt: time.Now()},
			{Key: "main:1", Kind: "main", UpdatedAt: time.Now()},
		}
	}, func(key string, limit int) []providers.Message { return nil })

	out, err := tool.Execute(context.Background(), map[string]interface{}{
		"action":         "list",
		"limit":          "1",
		"active_minutes": "60",
		"kinds":          "cron",
	})
	if err != nil {
		t.Fatalf("sessions execute failed: %v", err)
	}
	if !strings.Contains(out, "cron:1") || strings.Contains(out, "main:1") {
		t.Fatalf("unexpected filtered output: %s", out)
	}
}

func TestNodesToolParsesStringDurationAndArtifactPaths(t *testing.T) {
	t.Parallel()

	manager := nodes.NewManager()
	manager.Upsert(nodes.NodeInfo{
		ID:     "local",
		Online: true,
		Capabilities: nodes.Capabilities{
			Camera: true,
		},
	})
	manager.RegisterHandler("local", func(req nodes.Request) nodes.Response {
		return nodes.Response{
			OK:     true,
			Code:   "ok",
			Node:   "local",
			Action: req.Action,
			Payload: map[string]interface{}{
				"artifacts": []map[string]interface{}{
					{"kind": "video", "path": "/tmp/demo.mp4"},
				},
			},
		}
	})
	tool := NewNodesTool(manager, &nodes.Router{Relay: &nodes.HTTPRelayTransport{Manager: manager}}, "")
	out, err := tool.Execute(context.Background(), map[string]interface{}{
		"action":         "camera_clip",
		"node":           "local",
		"duration_ms":    "1000",
		"artifact_paths": "memory/demo.md",
	})
	if err != nil {
		t.Fatalf("nodes execute failed: %v", err)
	}
	if !strings.Contains(out, `"ok":true`) {
		t.Fatalf("unexpected output: %s", out)
	}
}

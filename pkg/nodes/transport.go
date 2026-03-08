package nodes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
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
		resp, err := r.P2P.Send(ctx, req)
		return annotateTransport(resp, "p2p", r.P2P.Name(), ""), err
	case "relay":
		if r.Relay == nil {
			return Response{OK: false, Node: req.Node, Action: req.Action, Error: "relay transport unavailable"}, nil
		}
		resp, err := r.Relay.Send(ctx, req)
		return annotateTransport(resp, "relay", r.Relay.Name(), ""), err
	default: // auto
		if r.P2P != nil {
			if resp, err := r.P2P.Send(ctx, req); err == nil && resp.OK {
				return annotateTransport(resp, "auto", r.P2P.Name(), ""), nil
			}
		}
		if r.Relay != nil {
			resp, err := r.Relay.Send(ctx, req)
			return annotateTransport(resp, "auto", r.Relay.Name(), "p2p"), err
		}
		return Response{}, fmt.Errorf("no transport available")
	}
}

func annotateTransport(resp Response, mode, usedTransport, fallbackFrom string) Response {
	if resp.Payload == nil {
		resp.Payload = map[string]interface{}{}
	}
	if strings.TrimSpace(mode) != "" {
		resp.Payload["dispatch_mode"] = strings.TrimSpace(mode)
	}
	if strings.TrimSpace(usedTransport) != "" {
		resp.Payload["used_transport"] = strings.TrimSpace(usedTransport)
	}
	if strings.TrimSpace(fallbackFrom) != "" {
		resp.Payload["fallback_from"] = strings.TrimSpace(fallbackFrom)
	}
	return resp
}

// WebsocketP2PTransport uses the persistent node websocket as a request/response tunnel
// while the project evolves toward a true peer data channel.
type WebsocketP2PTransport struct {
	Manager *Manager
}

func (s *WebsocketP2PTransport) Name() string { return "p2p" }
func (s *WebsocketP2PTransport) Send(ctx context.Context, req Request) (Response, error) {
	if s == nil || s.Manager == nil {
		return Response{OK: false, Node: req.Node, Action: req.Action, Error: "p2p manager unavailable"}, nil
	}
	resp, err := s.Manager.SendWireRequest(ctx, req.Node, req)
	if err != nil {
		return Response{OK: false, Code: "p2p_unavailable", Node: req.Node, Action: req.Action, Error: err.Error()}, nil
	}
	resp.Payload = normalizeDevicePayload(resp.Action, resp.Payload)
	return resp, nil
}

// HTTPRelayTransport dispatches requests to node-agent endpoints over HTTP.
type HTTPRelayTransport struct {
	Manager *Manager
	Client  *http.Client
}

func (s *HTTPRelayTransport) Name() string { return "relay" }

func actionHTTPPath(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "run":
		return "/run"
	case "invoke":
		return "/invoke"
	case "agent_task":
		return "/agent/task"
	case "camera_snap":
		return "/camera/snap"
	case "camera_clip":
		return "/camera/clip"
	case "screen_record":
		return "/screen/record"
	case "screen_snapshot":
		return "/screen/snapshot"
	case "location_get":
		return "/location/get"
	case "canvas_snapshot":
		return "/canvas/snapshot"
	case "canvas_action":
		return "/canvas/action"
	default:
		return "/invoke"
	}
}

func DoEndpointRequest(ctx context.Context, client *http.Client, endpoint, token string, req Request) (Response, error) {
	endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
	if endpoint == "" {
		return Response{OK: false, Code: "endpoint_missing", Node: req.Node, Action: req.Action, Error: "node endpoint not configured"}, nil
	}
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	body, _ := json.Marshal(req)
	path := actionHTTPPath(req.Action)
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+path, bytes.NewReader(body))
	if err != nil {
		return Response{}, err
	}
	hreq.Header.Set("Content-Type", "application/json")
	if tok := strings.TrimSpace(token); tok != "" {
		hreq.Header.Set("Authorization", "Bearer "+tok)
	}
	hresp, err := client.Do(hreq)
	if err != nil {
		return Response{OK: false, Code: "transport_error", Node: req.Node, Action: req.Action, Error: err.Error()}, nil
	}
	defer hresp.Body.Close()
	payload, _ := io.ReadAll(io.LimitReader(hresp.Body, 1<<20))
	var resp Response
	if err := json.Unmarshal(payload, &resp); err != nil {
		return Response{OK: false, Code: "invalid_response", Node: req.Node, Action: req.Action, Error: fmt.Sprintf("invalid node response: %s", strings.TrimSpace(string(payload)))}, nil
	}
	if strings.TrimSpace(resp.Node) == "" {
		resp.Node = req.Node
	}
	if strings.TrimSpace(resp.Action) == "" {
		resp.Action = req.Action
	}
	if strings.TrimSpace(resp.Code) == "" {
		if resp.OK {
			resp.Code = "ok"
		} else {
			resp.Code = "remote_error"
		}
	}
	resp.Payload = normalizeDevicePayload(resp.Action, resp.Payload)
	return resp, nil
}

func (s *HTTPRelayTransport) Send(ctx context.Context, req Request) (Response, error) {
	if s.Manager == nil {
		return Response{OK: false, Code: "relay_unavailable", Node: req.Node, Action: req.Action, Error: "relay manager not configured"}, nil
	}
	if resp, ok := s.Manager.Invoke(req); ok {
		return resp, nil
	}
	n, ok := s.Manager.Get(req.Node)
	if !ok {
		return Response{OK: false, Code: "node_not_found", Node: req.Node, Action: req.Action, Error: "node not found"}, nil
	}
	return DoEndpointRequest(ctx, s.Client, n.Endpoint, n.Token, req)
}

func normalizeDevicePayload(action string, payload map[string]interface{}) map[string]interface{} {
	if payload == nil {
		payload = map[string]interface{}{}
	}
	a := strings.ToLower(strings.TrimSpace(action))
	switch a {
	case "camera_snap", "screen_snapshot", "canvas_snapshot":
		if _, ok := payload["media_type"]; !ok {
			payload["media_type"] = "image"
		}
	case "camera_clip", "screen_record":
		if _, ok := payload["media_type"]; !ok {
			payload["media_type"] = "video"
		}
	}
	if _, ok := payload["storage"]; !ok {
		if _, hasURL := payload["url"]; hasURL {
			payload["storage"] = "url"
		} else if _, hasPath := payload["path"]; hasPath {
			payload["storage"] = "path"
		} else if _, hasInline := payload["image"]; hasInline {
			payload["storage"] = "inline"
		}
	}
	if _, ok := payload["meta"]; !ok {
		payload["meta"] = map[string]interface{}{}
	}
	payload["artifacts"] = normalizeArtifacts(payload, a)
	return payload
}

func normalizeArtifacts(payload map[string]interface{}, action string) []map[string]interface{} {
	if payload == nil {
		return []map[string]interface{}{}
	}
	if raw, ok := payload["artifacts"]; ok {
		items := normalizeArtifactList(raw)
		if len(items) > 0 {
			return items
		}
	}

	artifact := map[string]interface{}{}
	if mediaType, _ := payload["media_type"].(string); strings.TrimSpace(mediaType) != "" {
		artifact["kind"] = strings.TrimSpace(mediaType)
	}
	if mimeType, _ := payload["mime_type"].(string); strings.TrimSpace(mimeType) != "" {
		artifact["mime_type"] = strings.TrimSpace(mimeType)
	}
	if storage, _ := payload["storage"].(string); strings.TrimSpace(storage) != "" {
		artifact["storage"] = strings.TrimSpace(storage)
	}
	if path, _ := payload["path"].(string); strings.TrimSpace(path) != "" {
		artifact["path"] = filepath.Clean(strings.TrimSpace(path))
	}
	if url, _ := payload["url"].(string); strings.TrimSpace(url) != "" {
		artifact["url"] = strings.TrimSpace(url)
	}
	if image, _ := payload["image"].(string); strings.TrimSpace(image) != "" {
		artifact["content_base64"] = strings.TrimSpace(image)
	}
	if text, _ := payload["content_text"].(string); strings.TrimSpace(text) != "" {
		artifact["content_text"] = text
	}
	if name, _ := payload["name"].(string); strings.TrimSpace(name) != "" {
		artifact["name"] = strings.TrimSpace(name)
	}
	if size := int64FromPayload(payload["size_bytes"]); size > 0 {
		artifact["size_bytes"] = size
	}
	if len(artifact) == 0 {
		return []map[string]interface{}{}
	}
	if _, ok := artifact["kind"]; !ok && strings.TrimSpace(action) != "" {
		artifact["kind"] = strings.ToLower(strings.TrimSpace(action))
	}
	return []map[string]interface{}{artifact}
}

func normalizeArtifactList(raw interface{}) []map[string]interface{} {
	items, ok := raw.([]interface{})
	if !ok {
		return []map[string]interface{}{}
	}
	out := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		row, ok := item.(map[string]interface{})
		if !ok || len(row) == 0 {
			continue
		}
		normalized := map[string]interface{}{}
		for _, key := range []string{"id", "name", "kind", "mime_type", "storage", "path", "url", "content_text", "content_base64", "source_path"} {
			if value, ok := row[key]; ok && strings.TrimSpace(fmt.Sprint(value)) != "" {
				normalized[key] = value
			}
		}
		if truncated, ok := row["truncated"].(bool); ok && truncated {
			normalized["truncated"] = true
		}
		if size := int64FromPayload(row["size_bytes"]); size > 0 {
			normalized["size_bytes"] = size
		}
		if len(normalized) == 0 {
			continue
		}
		out = append(out, normalized)
	}
	return out
}

func int64FromPayload(v interface{}) int64 {
	switch value := v.(type) {
	case int:
		return int64(value)
	case int64:
		return value
	case float64:
		return int64(value)
	case json.Number:
		n, _ := value.Int64()
		return n
	default:
		return 0
	}
}

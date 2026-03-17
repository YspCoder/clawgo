package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/YspCoder/clawgo/pkg/config"
)

func TestMCPToolListServers(t *testing.T) {
	tool := NewMCPTool("/tmp/workspace", config.MCPToolsConfig{
		Enabled:           true,
		RequestTimeoutSec: 5,
		Servers: map[string]config.MCPServerConfig{
			"demo": {
				Enabled:     true,
				Transport:   "stdio",
				Command:     "demo-server",
				Description: "demo",
			},
			"disabled": {
				Enabled:   false,
				Transport: "stdio",
				Command:   "nope",
			},
		},
	})
	out, err := tool.Execute(context.Background(), map[string]interface{}{"action": "list_servers"})
	if err != nil {
		t.Fatalf("list_servers returned error: %v", err)
	}
	if !strings.Contains(out, `"name": "demo"`) {
		t.Fatalf("expected enabled server in output, got: %s", out)
	}
	if strings.Contains(out, "disabled") {
		t.Fatalf("did not expect disabled server in output, got: %s", out)
	}
}

func TestMCPToolCallTool(t *testing.T) {
	tool := NewMCPTool(t.TempDir(), config.MCPToolsConfig{
		Enabled:           true,
		RequestTimeoutSec: 5,
		Servers: map[string]config.MCPServerConfig{
			"helper": {
				Enabled:   true,
				Transport: "stdio",
				Command:   os.Args[0],
				Args:      []string{"-test.run=TestMCPHelperProcess", "--"},
				Env: map[string]string{
					"GO_WANT_HELPER_PROCESS": "1",
				},
			},
		},
	})
	out, err := tool.Execute(context.Background(), map[string]interface{}{
		"action":    "call_tool",
		"server":    "helper",
		"tool":      "echo",
		"arguments": map[string]interface{}{"text": "hello"},
	})
	if err != nil {
		t.Fatalf("call_tool returned error: %v", err)
	}
	if !strings.Contains(out, "echo:hello") {
		t.Fatalf("expected echo output, got: %s", out)
	}
}

func TestMCPToolDiscoverTools(t *testing.T) {
	tool := NewMCPTool(t.TempDir(), config.MCPToolsConfig{
		Enabled:           true,
		RequestTimeoutSec: 5,
		Servers: map[string]config.MCPServerConfig{
			"helper": {
				Enabled:   true,
				Transport: "stdio",
				Command:   os.Args[0],
				Args:      []string{"-test.run=TestMCPHelperProcess", "--"},
				Env: map[string]string{
					"GO_WANT_HELPER_PROCESS": "1",
				},
			},
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	discovered := tool.DiscoverTools(ctx)
	if len(discovered) != 1 {
		t.Fatalf("expected 1 discovered tool, got %d", len(discovered))
	}
	if got := discovered[0].Name(); got != "mcp__helper__echo" {
		t.Fatalf("unexpected discovered tool name: %s", got)
	}
	out, err := discovered[0].Execute(ctx, map[string]interface{}{"text": "world"})
	if err != nil {
		t.Fatalf("discovered tool execute returned error: %v", err)
	}
	if strings.TrimSpace(out) != "echo:world" {
		t.Fatalf("unexpected discovered tool output: %q", out)
	}
}

func TestMCPHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	runMCPHelper()
	os.Exit(0)
}

func runMCPHelper() {
	reader := bufio.NewReader(os.Stdin)
	writer := bufio.NewWriter(os.Stdout)
	for {
		msg, err := readHelperFrame(reader)
		if err != nil {
			return
		}
		method, _ := msg["method"].(string)
		id, hasID := msg["id"]
		switch method {
		case "initialize":
			writeHelperFrame(writer, map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      id,
				"result": map[string]interface{}{
					"protocolVersion": mcpProtocolVersion,
					"capabilities": map[string]interface{}{
						"tools":     map[string]interface{}{},
						"resources": map[string]interface{}{},
						"prompts":   map[string]interface{}{},
					},
					"serverInfo": map[string]interface{}{
						"name":    "helper",
						"version": "1.0.0",
					},
				},
			})
		case "notifications/initialized":
			continue
		case "tools/list":
			writeHelperFrame(writer, map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      id,
				"result": map[string]interface{}{
					"tools": []map[string]interface{}{
						{
							"name":        "echo",
							"description": "Echo the provided text",
							"inputSchema": map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{
									"text": map[string]interface{}{
										"type": "string",
									},
								},
								"required": []string{"text"},
							},
						},
					},
				},
			})
		case "tools/call":
			params, _ := msg["params"].(map[string]interface{})
			args, _ := params["arguments"].(map[string]interface{})
			text, _ := args["text"].(string)
			writeHelperFrame(writer, map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      id,
				"result": map[string]interface{}{
					"content": []map[string]interface{}{
						{
							"type": "text",
							"text": "echo:" + text,
						},
					},
				},
			})
		case "resources/list":
			writeHelperFrame(writer, map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      id,
				"result": map[string]interface{}{
					"resources": []map[string]interface{}{
						{"uri": "file:///tmp/demo.txt", "name": "demo"},
					},
				},
			})
		case "resources/read":
			writeHelperFrame(writer, map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      id,
				"result": map[string]interface{}{
					"contents": []map[string]interface{}{
						{"uri": "file:///tmp/demo.txt", "mimeType": "text/plain", "text": "demo content"},
					},
				},
			})
		case "prompts/list":
			writeHelperFrame(writer, map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      id,
				"result": map[string]interface{}{
					"prompts": []map[string]interface{}{
						{"name": "greeter", "description": "Greets"},
					},
				},
			})
		case "prompts/get":
			params, _ := msg["params"].(map[string]interface{})
			args, _ := params["arguments"].(map[string]interface{})
			name, _ := args["name"].(string)
			writeHelperFrame(writer, map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      id,
				"result": map[string]interface{}{
					"description": "Greets",
					"messages": []map[string]interface{}{
						{
							"role": "user",
							"content": map[string]interface{}{
								"type": "text",
								"text": "hello " + name,
							},
						},
					},
				},
			})
		default:
			if hasID {
				writeHelperFrame(writer, map[string]interface{}{
					"jsonrpc": "2.0",
					"id":      id,
					"error": map[string]interface{}{
						"code":    -32601,
						"message": "method not found",
					},
				})
			}
		}
	}
}

func readHelperFrame(r *bufio.Reader) (map[string]interface{}, error) {
	length := 0
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 && strings.EqualFold(strings.TrimSpace(parts[0]), "Content-Length") {
			length, _ = strconv.Atoi(strings.TrimSpace(parts[1]))
		}
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	var msg map[string]interface{}
	if err := json.Unmarshal(body, &msg); err != nil {
		return nil, err
	}
	return msg, nil
}

func writeHelperFrame(w *bufio.Writer, payload map[string]interface{}) {
	data, _ := json.Marshal(payload)
	_, _ = fmt.Fprintf(w, "Content-Length: %d\r\n\r\n%s", len(data), data)
	_ = w.Flush()
}

func TestValidateMCPTools(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Tools.MCP.Enabled = true
	cfg.Tools.MCP.RequestTimeoutSec = 0
	cfg.Tools.MCP.Servers = map[string]config.MCPServerConfig{
		"bad": {
			Enabled:    true,
			Transport:  "ws",
			Command:    "",
			WorkingDir: "/outside-workspace",
		},
	}
	errs := config.Validate(cfg)
	if len(errs) == 0 {
		t.Fatal("expected validation errors")
	}
	got := make([]string, 0, len(errs))
	for _, err := range errs {
		got = append(got, err.Error())
	}
	joined := strings.Join(got, "\n")
	for _, want := range []string{
		"tools.mcp.request_timeout_sec must be > 0 when tools.mcp.enabled=true",
		"tools.mcp.servers.bad.transport must be one of: stdio, http, streamable_http, sse",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected validation error %q in:\n%s", want, joined)
		}
	}
}

func TestValidateMCPToolsFullPermissionRequiresAbsolutePath(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Tools.MCP.Enabled = true
	cfg.Tools.MCP.Servers = map[string]config.MCPServerConfig{
		"full": {
			Enabled:    true,
			Transport:  "stdio",
			Command:    "demo",
			Permission: "full",
			WorkingDir: "relative",
		},
	}
	errs := config.Validate(cfg)
	if len(errs) == 0 {
		t.Fatal("expected validation errors")
	}
	joined := ""
	for _, err := range errs {
		joined += err.Error() + "\n"
	}
	if !strings.Contains(joined, "tools.mcp.servers.full.working_dir must be an absolute path when permission=full") {
		t.Fatalf("unexpected validation errors:\n%s", joined)
	}
}

func TestMCPToolListTools(t *testing.T) {
	tool := NewMCPTool(t.TempDir(), config.MCPToolsConfig{
		Enabled:           true,
		RequestTimeoutSec: 5,
		Servers: map[string]config.MCPServerConfig{
			"helper": {
				Enabled:   true,
				Transport: "stdio",
				Command:   os.Args[0],
				Args:      []string{"-test.run=TestMCPHelperProcess", "--"},
				Env: map[string]string{
					"GO_WANT_HELPER_PROCESS": "1",
				},
			},
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := tool.Execute(ctx, map[string]interface{}{
		"action": "list_tools",
		"server": "helper",
	})
	if err != nil {
		t.Fatalf("list_tools returned error: %v", err)
	}
	if !strings.Contains(out, `"name": "echo"`) {
		t.Fatalf("expected tool listing, got: %s", out)
	}
}

func TestMCPToolHTTPTransport(t *testing.T) {
	initializedNotified := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		method, _ := req["method"].(string)
		id := req["id"]
		var resp map[string]interface{}
		switch method {
		case "initialize":
			resp = map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      id,
				"result": map[string]interface{}{
					"protocolVersion": mcpProtocolVersion,
				},
			}
		case "notifications/initialized":
			if _, hasID := req["id"]; hasID {
				http.Error(w, "notification must not include id", http.StatusBadRequest)
				return
			}
			initializedNotified = true
			w.WriteHeader(http.StatusAccepted)
			return
		case "tools/list":
			resp = map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      id,
				"result": map[string]interface{}{
					"tools": []map[string]interface{}{
						{
							"name":        "echo",
							"description": "Echo text",
							"inputSchema": map[string]interface{}{"type": "object"},
						},
					},
				},
			}
		case "tools/call":
			resp = map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      id,
				"result": map[string]interface{}{
					"content": []map[string]interface{}{
						{"type": "text", "text": "echo:http"},
					},
				},
			}
		default:
			resp = map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      id,
				"error":   map[string]interface{}{"code": -32601, "message": "unsupported"},
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	tool := NewMCPTool(t.TempDir(), config.MCPToolsConfig{
		Enabled:           true,
		RequestTimeoutSec: 5,
		Servers: map[string]config.MCPServerConfig{
			"httpdemo": {
				Enabled:   true,
				Transport: "http",
				URL:       server.URL,
			},
		},
	})
	out, err := tool.Execute(context.Background(), map[string]interface{}{
		"action": "list_tools",
		"server": "httpdemo",
	})
	if err != nil {
		t.Fatalf("http list_tools returned error: %v", err)
	}
	if !strings.Contains(out, `"name": "echo"`) {
		t.Fatalf("expected http tool listing, got: %s", out)
	}
	if !initializedNotified {
		t.Fatal("expected initialized notification to be sent")
	}
}

func TestMCPToolSSETransport(t *testing.T) {
	messageCh := make(chan string, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/sse":
			w.Header().Set("Content-Type", "text/event-stream")
			flusher, ok := w.(http.Flusher)
			if !ok {
				http.Error(w, "flush unsupported", http.StatusInternalServerError)
				return
			}
			_, _ = fmt.Fprintf(w, "event: endpoint\n")
			_, _ = fmt.Fprintf(w, "data: /messages\n\n")
			flusher.Flush()
			notify := r.Context().Done()
			for {
				select {
				case payload := <-messageCh:
					_, _ = fmt.Fprintf(w, "event: message\n")
					_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
					flusher.Flush()
				case <-notify:
					return
				}
			}
		case r.Method == http.MethodPost && r.URL.Path == "/messages":
			defer r.Body.Close()
			var req map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			method, _ := req["method"].(string)
			id := req["id"]
			switch method {
			case "initialize":
				payload, _ := json.Marshal(map[string]interface{}{
					"jsonrpc": "2.0",
					"id":      id,
					"result": map[string]interface{}{
						"protocolVersion": mcpProtocolVersion,
					},
				})
				messageCh <- string(payload)
			case "tools/list":
				payload, _ := json.Marshal(map[string]interface{}{
					"jsonrpc": "2.0",
					"id":      id,
					"result": map[string]interface{}{
						"tools": []map[string]interface{}{
							{"name": "echo", "description": "Echo text", "inputSchema": map[string]interface{}{"type": "object"}},
						},
					},
				})
				messageCh <- string(payload)
			case "tools/call":
				payload, _ := json.Marshal(map[string]interface{}{
					"jsonrpc": "2.0",
					"id":      id,
					"result": map[string]interface{}{
						"content": []map[string]interface{}{{"type": "text", "text": "echo:sse"}},
					},
				})
				messageCh <- string(payload)
			}
			w.WriteHeader(http.StatusAccepted)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	tool := NewMCPTool(t.TempDir(), config.MCPToolsConfig{
		Enabled:           true,
		RequestTimeoutSec: 5,
		Servers: map[string]config.MCPServerConfig{
			"ssedemo": {
				Enabled:   true,
				Transport: "sse",
				URL:       server.URL + "/sse",
			},
		},
	})
	out, err := tool.Execute(context.Background(), map[string]interface{}{
		"action": "list_tools",
		"server": "ssedemo",
	})
	if err != nil {
		t.Fatalf("sse list_tools returned error: %v", err)
	}
	if !strings.Contains(out, `"name": "echo"`) {
		t.Fatalf("expected sse tool listing, got: %s", out)
	}
}

func TestBuildMCPDynamicToolName(t *testing.T) {
	got := buildMCPDynamicToolName("Context7 Server", "resolve-library.id")
	if got != "mcp__context7_server__resolve_library_id" {
		t.Fatalf("unexpected tool name: %s", got)
	}
}

func TestResolveMCPWorkingDirWorkspaceScoped(t *testing.T) {
	workspace := t.TempDir()
	dir, err := resolveMCPWorkingDir(workspace, config.MCPServerConfig{WorkingDir: "tools/context7"})
	if err != nil {
		t.Fatalf("resolveMCPWorkingDir returned error: %v", err)
	}
	want := filepath.Join(workspace, "tools", "context7")
	if dir != want {
		t.Fatalf("unexpected working dir: got %q want %q", dir, want)
	}
}

func TestResolveMCPWorkingDirRejectsOutsideWorkspaceWithoutFullPermission(t *testing.T) {
	workspace := t.TempDir()
	outside := filepath.Dir(workspace)
	if filepath.Clean(outside) == filepath.Clean(workspace) {
		t.Skip("unable to construct outside-workspace path")
	}
	_, err := resolveMCPWorkingDir(workspace, config.MCPServerConfig{WorkingDir: outside})
	if err == nil {
		t.Fatal("expected outside-workspace path to be rejected")
	}
}

func TestResolveMCPWorkingDirAllowsAbsolutePathWithFullPermission(t *testing.T) {
	absolute := filepath.Clean(filepath.Dir(t.TempDir()))
	dir, err := resolveMCPWorkingDir(t.TempDir(), config.MCPServerConfig{Permission: "full", WorkingDir: absolute})
	if err != nil {
		t.Fatalf("resolveMCPWorkingDir returned error: %v", err)
	}
	if dir != absolute {
		t.Fatalf("unexpected working dir: %q", dir)
	}
}

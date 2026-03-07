package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"clawgo/pkg/config"
)

const mcpProtocolVersion = "2025-06-18"

type MCPTool struct {
	workspace string
	cfg       config.MCPToolsConfig
}

type MCPRemoteTool struct {
	bridge      *MCPTool
	serverName  string
	remoteName  string
	localName   string
	description string
	parameters  map[string]interface{}
}

func NewMCPTool(workspace string, cfg config.MCPToolsConfig) *MCPTool {
	if cfg.RequestTimeoutSec <= 0 {
		cfg.RequestTimeoutSec = 20
	}
	if cfg.Servers == nil {
		cfg.Servers = map[string]config.MCPServerConfig{}
	}
	return &MCPTool{workspace: workspace, cfg: cfg}
}

func (t *MCPTool) Name() string {
	return "mcp"
}

func (t *MCPTool) Description() string {
	return "Call configured MCP servers over stdio. Supports listing servers, tools, resources, prompts, and invoking remote MCP tools."
}

func (t *MCPTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"description": "Operation to perform",
				"enum":        []string{"list_servers", "list_tools", "call_tool", "list_resources", "read_resource", "list_prompts", "get_prompt"},
			},
			"server": map[string]interface{}{
				"type":        "string",
				"description": "Configured MCP server name",
			},
			"tool": map[string]interface{}{
				"type":        "string",
				"description": "MCP tool name for action=call_tool",
			},
			"arguments": map[string]interface{}{
				"type":        "object",
				"description": "Arguments for call_tool or get_prompt",
			},
			"uri": map[string]interface{}{
				"type":        "string",
				"description": "Resource URI for action=read_resource",
			},
			"prompt": map[string]interface{}{
				"type":        "string",
				"description": "Prompt name for action=get_prompt",
			},
		},
		"required": []string{"action"},
	}
}

func (t *MCPTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	action := strings.TrimSpace(mcpStringArg(args, "action"))
	if action == "" {
		return "", fmt.Errorf("action is required")
	}
	if action == "list_servers" {
		return t.listServers(), nil
	}

	serverName := strings.TrimSpace(mcpStringArg(args, "server"))
	if serverName == "" {
		return "", fmt.Errorf("server is required for action %q", action)
	}
	serverCfg, ok := t.cfg.Servers[serverName]
	if !ok || !serverCfg.Enabled {
		return "", fmt.Errorf("mcp server %q is not configured or not enabled", serverName)
	}

	timeout := time.Duration(t.cfg.RequestTimeoutSec) * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining > 0 && remaining < timeout {
			timeout = remaining
		}
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	client, err := newMCPStdioClient(callCtx, t.workspace, serverName, serverCfg)
	if err != nil {
		return "", err
	}
	defer client.Close()

	switch action {
	case "list_tools":
		out, err := client.listAll(callCtx, "tools/list", "tools")
		if err != nil {
			return "", err
		}
		return prettyJSON(out)
	case "call_tool":
		toolName := strings.TrimSpace(mcpStringArg(args, "tool"))
		if toolName == "" {
			return "", fmt.Errorf("tool is required for action=call_tool")
		}
		params := map[string]interface{}{
			"name":      toolName,
			"arguments": mcpObjectArg(args, "arguments"),
		}
		out, err := client.request(callCtx, "tools/call", params)
		if err != nil {
			return "", err
		}
		return prettyJSON(out)
	case "list_resources":
		out, err := client.listAll(callCtx, "resources/list", "resources")
		if err != nil {
			return "", err
		}
		return prettyJSON(out)
	case "read_resource":
		resourceURI := strings.TrimSpace(mcpStringArg(args, "uri"))
		if resourceURI == "" {
			return "", fmt.Errorf("uri is required for action=read_resource")
		}
		out, err := client.request(callCtx, "resources/read", map[string]interface{}{"uri": resourceURI})
		if err != nil {
			return "", err
		}
		return prettyJSON(out)
	case "list_prompts":
		out, err := client.listAll(callCtx, "prompts/list", "prompts")
		if err != nil {
			return "", err
		}
		return prettyJSON(out)
	case "get_prompt":
		promptName := strings.TrimSpace(mcpStringArg(args, "prompt"))
		if promptName == "" {
			return "", fmt.Errorf("prompt is required for action=get_prompt")
		}
		out, err := client.request(callCtx, "prompts/get", map[string]interface{}{
			"name":      promptName,
			"arguments": mcpObjectArg(args, "arguments"),
		})
		if err != nil {
			return "", err
		}
		return prettyJSON(out)
	default:
		return "", fmt.Errorf("unsupported action %q", action)
	}
}

func (t *MCPTool) DiscoverTools(ctx context.Context) []Tool {
	if t == nil || !t.cfg.Enabled {
		return nil
	}
	names := make([]string, 0, len(t.cfg.Servers))
	for name, server := range t.cfg.Servers {
		if server.Enabled {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	tools := make([]Tool, 0)
	seen := map[string]int{}
	for _, serverName := range names {
		serverCfg := t.cfg.Servers[serverName]
		client, err := newMCPStdioClient(ctx, t.workspace, serverName, serverCfg)
		if err != nil {
			continue
		}
		result, err := client.listAll(ctx, "tools/list", "tools")
		_ = client.Close()
		if err != nil {
			continue
		}
		items, _ := result["tools"].([]interface{})
		for _, item := range items {
			toolMap, _ := item.(map[string]interface{})
			remoteName := strings.TrimSpace(mcpStringArg(toolMap, "name"))
			if remoteName == "" {
				continue
			}
			localName := buildMCPDynamicToolName(serverName, remoteName)
			if count := seen[localName]; count > 0 {
				localName = fmt.Sprintf("%s_%d", localName, count+1)
			}
			seen[localName]++
			tools = append(tools, &MCPRemoteTool{
				bridge:      t,
				serverName:  serverName,
				remoteName:  remoteName,
				localName:   localName,
				description: buildMCPDynamicToolDescription(serverName, toolMap),
				parameters:  normalizeMCPSchema(toolMap["inputSchema"]),
			})
		}
	}
	return tools
}

func (t *MCPTool) callServerTool(ctx context.Context, serverName, remoteToolName string, arguments map[string]interface{}) (string, error) {
	serverCfg, ok := t.cfg.Servers[serverName]
	if !ok || !serverCfg.Enabled {
		return "", fmt.Errorf("mcp server %q is not configured or not enabled", serverName)
	}
	timeout := time.Duration(t.cfg.RequestTimeoutSec) * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining > 0 && remaining < timeout {
			timeout = remaining
		}
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	client, err := newMCPStdioClient(callCtx, t.workspace, serverName, serverCfg)
	if err != nil {
		return "", err
	}
	defer client.Close()
	out, err := client.request(callCtx, "tools/call", map[string]interface{}{
		"name":      remoteToolName,
		"arguments": arguments,
	})
	if err != nil {
		return "", err
	}
	return renderMCPToolCallResult(out)
}

func (t *MCPTool) listServers() string {
	type item struct {
		Name        string `json:"name"`
		Transport   string `json:"transport"`
		Command     string `json:"command"`
		WorkingDir  string `json:"working_dir,omitempty"`
		Description string `json:"description,omitempty"`
	}
	names := make([]string, 0, len(t.cfg.Servers))
	for name, server := range t.cfg.Servers {
		if server.Enabled {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	items := make([]item, 0, len(names))
	for _, name := range names {
		server := t.cfg.Servers[name]
		transport := strings.TrimSpace(server.Transport)
		if transport == "" {
			transport = "stdio"
		}
		items = append(items, item{
			Name:        name,
			Transport:   transport,
			Command:     server.Command,
			WorkingDir:  server.WorkingDir,
			Description: server.Description,
		})
	}
	out, _ := json.MarshalIndent(map[string]interface{}{"servers": items}, "", "  ")
	return string(out)
}

func (t *MCPRemoteTool) Name() string {
	return t.localName
}

func (t *MCPRemoteTool) Description() string {
	return t.description
}

func (t *MCPRemoteTool) Parameters() map[string]interface{} {
	return t.parameters
}

func (t *MCPRemoteTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	return t.bridge.callServerTool(ctx, t.serverName, t.remoteName, args)
}

func (t *MCPRemoteTool) CatalogEntry() map[string]interface{} {
	return map[string]interface{}{
		"source": "mcp",
		"mcp": map[string]interface{}{
			"server":      t.serverName,
			"remote_tool": t.remoteName,
		},
	}
}

type mcpClient struct {
	workspace  string
	serverName string
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	reader     *bufio.Reader
	stderr     bytes.Buffer

	writeMu sync.Mutex
	waiters sync.Map
	nextID  atomic.Int64
}

type mcpInbound struct {
	JSONRPC string                 `json:"jsonrpc"`
	ID      interface{}            `json:"id,omitempty"`
	Method  string                 `json:"method,omitempty"`
	Params  map[string]interface{} `json:"params,omitempty"`
	Result  json.RawMessage        `json:"result,omitempty"`
	Error   *mcpResponseError      `json:"error,omitempty"`
}

type mcpResponseError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type mcpResponseWaiter struct {
	ch chan mcpInbound
}

func newMCPStdioClient(ctx context.Context, workspace, serverName string, cfg config.MCPServerConfig) (*mcpClient, error) {
	command := strings.TrimSpace(cfg.Command)
	if command == "" {
		return nil, fmt.Errorf("mcp server %q command is empty", serverName)
	}
	cmd := exec.CommandContext(ctx, command, cfg.Args...)
	cmd.Env = buildMCPEnv(cfg.Env)
	cmd.Dir = resolveMCPWorkingDir(workspace, cfg.WorkingDir)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("open stdin for mcp server %q: %w", serverName, err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("open stdout for mcp server %q: %w", serverName, err)
	}
	client := &mcpClient{
		workspace:  workspace,
		serverName: serverName,
		cmd:        cmd,
		stdin:      stdin,
		reader:     bufio.NewReader(stdout),
	}
	cmd.Stderr = &client.stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start mcp server %q: %w", serverName, err)
	}
	go client.readLoop()
	if err := client.initialize(ctx); err != nil {
		client.Close()
		return nil, err
	}
	return client, nil
}

func (c *mcpClient) Close() error {
	if c == nil || c.cmd == nil {
		return nil
	}
	_ = c.stdin.Close()
	done := make(chan error, 1)
	go func() {
		done <- c.cmd.Wait()
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(500 * time.Millisecond):
		if c.cmd.Process != nil {
			_ = c.cmd.Process.Kill()
		}
		<-done
		return nil
	}
}

func (c *mcpClient) initialize(ctx context.Context) error {
	result, err := c.request(ctx, "initialize", map[string]interface{}{
		"protocolVersion": mcpProtocolVersion,
		"capabilities": map[string]interface{}{
			"roots": map[string]interface{}{
				"listChanged": false,
			},
		},
		"clientInfo": map[string]interface{}{
			"name":    "clawgo",
			"version": "dev",
		},
	})
	if err != nil {
		return err
	}
	if _, ok := result["protocolVersion"]; !ok {
		return fmt.Errorf("mcp server %q initialize missing protocolVersion", c.serverName)
	}
	return c.notify("notifications/initialized", map[string]interface{}{})
}

func (c *mcpClient) listAll(ctx context.Context, method, field string) (map[string]interface{}, error) {
	items := make([]interface{}, 0)
	cursor := ""
	for {
		params := map[string]interface{}{}
		if strings.TrimSpace(cursor) != "" {
			params["cursor"] = cursor
		}
		result, err := c.request(ctx, method, params)
		if err != nil {
			return nil, err
		}
		batch, _ := result[field].([]interface{})
		items = append(items, batch...)
		next, _ := result["nextCursor"].(string)
		if strings.TrimSpace(next) == "" {
			return map[string]interface{}{field: items}, nil
		}
		cursor = next
	}
}

func (c *mcpClient) request(ctx context.Context, method string, params map[string]interface{}) (map[string]interface{}, error) {
	id := strconv.FormatInt(c.nextID.Add(1), 10)
	waiter := &mcpResponseWaiter{ch: make(chan mcpInbound, 1)}
	c.waiters.Store(id, waiter)
	defer c.waiters.Delete(id)

	msg := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	}
	if err := c.writeMessage(msg); err != nil {
		return nil, err
	}

	select {
	case resp := <-waiter.ch:
		if resp.Error != nil {
			return nil, fmt.Errorf("mcp %s %s failed: %s", c.serverName, method, resp.Error.Message)
		}
		var out map[string]interface{}
		if len(resp.Result) == 0 {
			return map[string]interface{}{}, nil
		}
		if err := json.Unmarshal(resp.Result, &out); err != nil {
			return nil, fmt.Errorf("decode mcp %s %s result: %w", c.serverName, method, err)
		}
		return out, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c *mcpClient) notify(method string, params map[string]interface{}) error {
	return c.writeMessage(map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	})
}

func (c *mcpClient) writeMessage(payload map[string]interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	frame := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(data), data)
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, err = io.WriteString(c.stdin, frame)
	return err
}

func (c *mcpClient) readLoop() {
	for {
		msg, err := c.readMessage()
		if err != nil {
			c.failAll(err)
			return
		}
		if msg.Method != "" && msg.ID != nil {
			_ = c.handleServerRequest(msg)
			continue
		}
		if msg.Method != "" {
			continue
		}
		if key, ok := normalizeMCPID(msg.ID); ok {
			if raw, ok := c.waiters.Load(key); ok {
				raw.(*mcpResponseWaiter).ch <- msg
			}
		}
	}
}

func (c *mcpClient) handleServerRequest(msg mcpInbound) error {
	method := strings.TrimSpace(msg.Method)
	switch method {
	case "roots/list":
		return c.reply(msg.ID, map[string]interface{}{
			"roots": []map[string]interface{}{
				{
					"uri":  fileURI(resolveMCPWorkingDir(c.workspace, "")),
					"name": filepath.Base(resolveMCPWorkingDir(c.workspace, "")),
				},
			},
		})
	case "ping":
		return c.reply(msg.ID, map[string]interface{}{})
	default:
		return c.replyError(msg.ID, -32601, "method not supported by clawgo mcp client")
	}
}

func (c *mcpClient) reply(id interface{}, result map[string]interface{}) error {
	return c.writeMessage(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	})
}

func (c *mcpClient) replyError(id interface{}, code int, message string) error {
	return c.writeMessage(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"error": map[string]interface{}{
			"code":    code,
			"message": message,
		},
	})
}

func (c *mcpClient) failAll(err error) {
	message := err.Error()
	if stderr := strings.TrimSpace(c.stderr.String()); stderr != "" {
		message += ": " + stderr
	}
	c.waiters.Range(func(_, value interface{}) bool {
		value.(*mcpResponseWaiter).ch <- mcpInbound{
			Error: &mcpResponseError{Message: message},
		}
		return true
	})
}

func (c *mcpClient) readMessage() (mcpInbound, error) {
	length := 0
	for {
		line, err := c.reader.ReadString('\n')
		if err != nil {
			return mcpInbound{}, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(parts[0]), "Content-Length") {
			length, _ = strconv.Atoi(strings.TrimSpace(parts[1]))
		}
	}
	if length <= 0 {
		return mcpInbound{}, fmt.Errorf("invalid mcp content length")
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(c.reader, body); err != nil {
		return mcpInbound{}, err
	}
	var msg mcpInbound
	if err := json.Unmarshal(body, &msg); err != nil {
		return mcpInbound{}, err
	}
	return msg, nil
}

func buildMCPEnv(overrides map[string]string) []string {
	env := os.Environ()
	path := os.Getenv("PATH")
	fallback := "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/opt/homebrew/bin:/opt/homebrew/sbin"
	if strings.TrimSpace(path) == "" {
		env = append(env, "PATH="+fallback)
	} else {
		env = append(env, "PATH="+path+":"+fallback)
	}
	for key, value := range overrides {
		env = append(env, key+"="+value)
	}
	return env
}

func resolveMCPWorkingDir(workspace, wd string) string {
	wd = strings.TrimSpace(wd)
	if wd != "" {
		return wd
	}
	if abs, err := filepath.Abs(workspace); err == nil {
		return abs
	}
	return workspace
}

func fileURI(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}).String()
}

func normalizeMCPID(id interface{}) (string, bool) {
	switch v := id.(type) {
	case string:
		return v, v != ""
	case float64:
		return strconv.FormatInt(int64(v), 10), true
	case int:
		return strconv.Itoa(v), true
	case int64:
		return strconv.FormatInt(v, 10), true
	default:
		return "", false
	}
}

func prettyJSON(v interface{}) (string, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func buildMCPDynamicToolName(serverName, remoteName string) string {
	base := "mcp__" + sanitizeMCPToolSegment(serverName) + "__" + sanitizeMCPToolSegment(remoteName)
	if len(base) <= 64 {
		return base
	}
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(serverName + "::" + remoteName))
	suffix := fmt.Sprintf("_%x", hash.Sum32())
	trimmed := base
	if len(trimmed)+len(suffix) > 64 {
		trimmed = trimmed[:64-len(suffix)]
	}
	return trimmed + suffix
}

func ParseMCPDynamicToolName(name string) (serverName string, remoteName string, ok bool) {
	const prefix = "mcp__"
	if !strings.HasPrefix(strings.TrimSpace(name), prefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(strings.TrimSpace(name), prefix)
	parts := strings.SplitN(rest, "__", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

var mcpToolSegmentPattern = regexp.MustCompile(`[^a-zA-Z0-9_]+`)

func sanitizeMCPToolSegment(in string) string {
	in = strings.TrimSpace(strings.ToLower(in))
	in = mcpToolSegmentPattern.ReplaceAllString(in, "_")
	in = strings.Trim(in, "_")
	if in == "" {
		return "tool"
	}
	return in
}

func buildMCPDynamicToolDescription(serverName string, toolMap map[string]interface{}) string {
	desc := strings.TrimSpace(mcpStringArg(toolMap, "description"))
	remoteName := strings.TrimSpace(mcpStringArg(toolMap, "name"))
	if desc == "" {
		desc = fmt.Sprintf("Proxy to MCP tool %q on server %q.", remoteName, serverName)
	} else {
		desc = fmt.Sprintf("%s (MCP server: %s, remote tool: %s)", desc, serverName, remoteName)
	}
	return desc
}

func normalizeMCPSchema(raw interface{}) map[string]interface{} {
	schema, _ := raw.(map[string]interface{})
	if schema == nil {
		return map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}
	}
	out := map[string]interface{}{}
	for k, v := range schema {
		out[k] = v
	}
	if _, ok := out["type"]; !ok {
		out["type"] = "object"
	}
	if _, ok := out["properties"]; !ok {
		out["properties"] = map[string]interface{}{}
	}
	return out
}

func renderMCPToolCallResult(result map[string]interface{}) (string, error) {
	if result == nil {
		return "", nil
	}
	if content, ok := result["content"].([]interface{}); ok && len(content) > 0 {
		parts := make([]string, 0, len(content))
		for _, item := range content {
			m, _ := item.(map[string]interface{})
			if m == nil {
				continue
			}
			kind := strings.TrimSpace(mcpStringArg(m, "type"))
			switch kind {
			case "text":
				if text := mcpStringArg(m, "text"); strings.TrimSpace(text) != "" {
					parts = append(parts, text)
				}
			default:
				if text := mcpStringArg(m, "text"); strings.TrimSpace(text) != "" {
					parts = append(parts, text)
				} else {
					data, err := prettyJSON(m)
					if err == nil {
						parts = append(parts, data)
					}
				}
			}
		}
		if len(parts) > 0 {
			if structured, ok := result["structuredContent"]; ok {
				data, err := prettyJSON(structured)
				if err == nil && strings.TrimSpace(data) != "" && data != "{}" {
					parts = append(parts, data)
				}
			}
			return strings.Join(parts, "\n\n"), nil
		}
	}
	return prettyJSON(result)
}

func mcpStringArg(args map[string]interface{}, key string) string {
	v, _ := args[key].(string)
	return v
}

func mcpObjectArg(args map[string]interface{}, key string) map[string]interface{} {
	v, _ := args[key].(map[string]interface{})
	if v == nil {
		return map[string]interface{}{}
	}
	return v
}

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/YspCoder/clawgo/pkg/channels"
	cfgpkg "github.com/YspCoder/clawgo/pkg/config"
	"github.com/YspCoder/clawgo/pkg/nodes"
	"github.com/YspCoder/clawgo/pkg/providers"
	"github.com/YspCoder/clawgo/pkg/tools"
	"github.com/gorilla/websocket"
)

func (s *Server) handleWebUIRuntime(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	conn, err := nodesWebsocketUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	ctx := r.Context()
	sub := s.subscribeRuntimeLive(ctx)
	initial := map[string]interface{}{
		"ok":       true,
		"type":     "runtime_snapshot",
		"snapshot": s.buildWebUIRuntimeSnapshot(ctx),
	}
	_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := conn.WriteJSON(initial); err != nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case payload := <-sub:
			_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
				return
			}
		}
	}
}

func (s *Server) buildWebUIRuntimeSnapshot(ctx context.Context) map[string]interface{} {
	var providerPayload map[string]interface{}
	var normalizedConfig interface{}
	if strings.TrimSpace(s.configPath) != "" {
		if cfg, err := cfgpkg.LoadConfig(strings.TrimSpace(s.configPath)); err == nil {
			providerPayload = providers.GetProviderRuntimeSnapshot(cfg)
			normalizedConfig = cfg.NormalizedView()
		}
	}
	if providerPayload == nil {
		providerPayload = map[string]interface{}{"items": []interface{}{}}
	}
	runtimePayload := map[string]interface{}{}
	if s.onSubagents != nil {
		if res, err := s.onSubagents(ctx, "snapshot", map[string]interface{}{"limit": 200}); err == nil {
			if m, ok := res.(map[string]interface{}); ok {
				runtimePayload = m
			}
		}
	}
	return map[string]interface{}{
		"version":    s.webUIVersionPayload(),
		"config":     normalizedConfig,
		"runtime":    runtimePayload,
		"nodes":      s.webUINodesPayload(ctx),
		"sessions":   s.webUISessionsPayload(),
		"task_queue": s.webUITaskQueuePayload(false),
		"ekg":        s.webUIEKGSummaryPayload("24h"),
		"providers":  providerPayload,
	}
}

func (s *Server) webUIVersionPayload() map[string]interface{} {
	return map[string]interface{}{
		"gateway_version":   firstNonEmptyString(s.gatewayVersion, gatewayBuildVersion()),
		"webui_version":     firstNonEmptyString(s.webuiVersion, detectWebUIVersion(strings.TrimSpace(s.webUIDir))),
		"compiled_channels": channels.CompiledChannelKeys(),
	}
}

func (s *Server) webUINodesPayload(ctx context.Context) map[string]interface{} {
	list := []nodes.NodeInfo{}
	if s.mgr != nil {
		list = s.mgr.List()
	}
	localRegistry := s.fetchRegistryItems(ctx)
	localAgents := make([]nodes.AgentInfo, 0, len(localRegistry))
	for _, item := range localRegistry {
		agentID := strings.TrimSpace(stringFromMap(item, "agent_id"))
		if agentID == "" {
			continue
		}
		localAgents = append(localAgents, nodes.AgentInfo{
			ID:          agentID,
			DisplayName: strings.TrimSpace(stringFromMap(item, "display_name")),
			Role:        strings.TrimSpace(stringFromMap(item, "role")),
			Type:        strings.TrimSpace(stringFromMap(item, "type")),
			Transport:   fallbackString(strings.TrimSpace(stringFromMap(item, "transport")), "local"),
		})
	}
	host, _ := os.Hostname()
	local := nodes.NodeInfo{
		ID:           "local",
		Name:         "local",
		Endpoint:     "gateway",
		Version:      gatewayBuildVersion(),
		OS:           runtime.GOOS,
		Arch:         runtime.GOARCH,
		LastSeenAt:   time.Now(),
		Online:       true,
		Capabilities: nodes.Capabilities{Run: true, Invoke: true, Model: true, Camera: true, Screen: true, Location: true, Canvas: true},
		Actions:      []string{"run", "agent_task", "camera_snap", "camera_clip", "screen_snapshot", "screen_record", "location_get", "canvas_snapshot", "canvas_action"},
		Models:       []string{"local-sim"},
		Agents:       localAgents,
	}
	if strings.TrimSpace(host) != "" {
		local.Name = host
	}
	if ip := detectLocalIP(); ip != "" {
		local.Endpoint = ip
	}
	hostLower := strings.ToLower(strings.TrimSpace(host))
	matched := false
	for i := range list {
		id := strings.ToLower(strings.TrimSpace(list[i].ID))
		name := strings.ToLower(strings.TrimSpace(list[i].Name))
		if id == "local" || name == "local" || (hostLower != "" && name == hostLower) {
			list[i].ID = "local"
			list[i].Online = true
			list[i].Version = local.Version
			if strings.TrimSpace(local.Endpoint) != "" {
				list[i].Endpoint = local.Endpoint
			}
			if strings.TrimSpace(local.Name) != "" {
				list[i].Name = local.Name
			}
			list[i].LastSeenAt = time.Now()
			matched = true
			break
		}
	}
	if !matched {
		list = append([]nodes.NodeInfo{local}, list...)
	}
	p2p := map[string]interface{}{}
	if s.nodeP2PStatus != nil {
		p2p = s.nodeP2PStatus()
	}
	dispatches := s.webUINodesDispatchPayload(12)
	return map[string]interface{}{
		"nodes":              list,
		"trees":              s.buildNodeAgentTrees(ctx, list),
		"p2p":                p2p,
		"dispatches":         dispatches,
		"alerts":             s.webUINodeAlertsPayload(list, p2p, dispatches),
		"artifact_retention": s.artifactStatsSnapshot(),
	}
}

func (s *Server) webUINodeAlertsPayload(nodeList []nodes.NodeInfo, p2p map[string]interface{}, dispatches []map[string]interface{}) []map[string]interface{} {
	alerts := make([]map[string]interface{}, 0)
	for _, node := range nodeList {
		nodeID := strings.TrimSpace(node.ID)
		if nodeID == "" || nodeID == "local" {
			continue
		}
		if !node.Online {
			alerts = append(alerts, map[string]interface{}{
				"severity": "critical",
				"kind":     "node_offline",
				"node":     nodeID,
				"title":    "Node offline",
				"detail":   fmt.Sprintf("node %s is offline", nodeID),
			})
		}
	}
	if sessions, ok := p2p["nodes"].([]map[string]interface{}); ok {
		for _, session := range sessions {
			appendNodeSessionAlert(&alerts, session)
		}
	} else if sessions, ok := p2p["nodes"].([]interface{}); ok {
		for _, raw := range sessions {
			if session, ok := raw.(map[string]interface{}); ok {
				appendNodeSessionAlert(&alerts, session)
			}
		}
	}
	failuresByNode := map[string]int{}
	for _, row := range dispatches {
		nodeID := strings.TrimSpace(fmt.Sprint(row["node"]))
		if nodeID == "" {
			continue
		}
		if ok, _ := tools.MapBoolArg(row, "ok"); ok {
			continue
		}
		failuresByNode[nodeID]++
	}
	for nodeID, count := range failuresByNode {
		if count < 2 {
			continue
		}
		alerts = append(alerts, map[string]interface{}{
			"severity": "warning",
			"kind":     "dispatch_failures",
			"node":     nodeID,
			"title":    "Repeated dispatch failures",
			"detail":   fmt.Sprintf("node %s has %d recent failed dispatches", nodeID, count),
			"count":    count,
		})
	}
	return alerts
}

func appendNodeSessionAlert(alerts *[]map[string]interface{}, session map[string]interface{}) {
	nodeID := strings.TrimSpace(fmt.Sprint(session["node"]))
	if nodeID == "" {
		return
	}
	status := strings.ToLower(strings.TrimSpace(fmt.Sprint(session["status"])))
	retryCount := int(int64Value(session["retry_count"]))
	lastError := strings.TrimSpace(fmt.Sprint(session["last_error"]))
	switch {
	case status == "failed" || status == "closed":
		*alerts = append(*alerts, map[string]interface{}{
			"severity": "critical",
			"kind":     "p2p_session_down",
			"node":     nodeID,
			"title":    "P2P session down",
			"detail":   firstNonEmptyString(lastError, fmt.Sprintf("node %s p2p session is %s", nodeID, status)),
		})
	case retryCount >= 3 || (status == "connecting" && retryCount >= 2):
		*alerts = append(*alerts, map[string]interface{}{
			"severity": "warning",
			"kind":     "p2p_session_unstable",
			"node":     nodeID,
			"title":    "P2P session unstable",
			"detail":   firstNonEmptyString(lastError, fmt.Sprintf("node %s p2p session retry_count=%d", nodeID, retryCount)),
			"count":    retryCount,
		})
	}
}

func int64Value(v interface{}) int64 {
	switch value := v.(type) {
	case int:
		return int64(value)
	case int32:
		return int64(value)
	case int64:
		return value
	case float32:
		return int64(value)
	case float64:
		return int64(value)
	case json.Number:
		if n, err := value.Int64(); err == nil {
			return n
		}
	}
	return 0
}

func (s *Server) webUINodesDispatchPayload(limit int) []map[string]interface{} {
	path := s.memoryFilePath("nodes-dispatch-audit.jsonl")
	if path == "" {
		return []map[string]interface{}{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return []map[string]interface{}{}
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) == 1 && strings.TrimSpace(lines[0]) == "" {
		return []map[string]interface{}{}
	}
	out := make([]map[string]interface{}, 0, limit)
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		row := map[string]interface{}{}
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			continue
		}
		out = append(out, row)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func (s *Server) buildNodeAgentTrees(ctx context.Context, nodeList []nodes.NodeInfo) []map[string]interface{} {
	trees := make([]map[string]interface{}, 0, len(nodeList))
	localRegistry := s.fetchRegistryItems(ctx)
	for _, node := range nodeList {
		nodeID := strings.TrimSpace(node.ID)
		items := []map[string]interface{}{}
		source := "unavailable"
		readonly := true
		if nodeID == "local" {
			items = localRegistry
			source = "local_runtime"
			readonly = false
		} else if remoteItems, err := s.fetchRemoteNodeRegistry(ctx, node); err == nil {
			items = remoteItems
			source = "remote_webui"
		}
		trees = append(trees, map[string]interface{}{
			"node_id":   nodeID,
			"node_name": fallbackNodeName(node),
			"online":    node.Online,
			"source":    source,
			"readonly":  readonly,
			"root":      buildAgentTreeRoot(nodeID, items),
		})
	}
	return trees
}

func (s *Server) fetchRegistryItems(ctx context.Context) []map[string]interface{} {
	if s == nil || s.onSubagents == nil {
		return nil
	}
	result, err := s.onSubagents(ctx, "registry", nil)
	if err != nil {
		return nil
	}
	payload, ok := result.(map[string]interface{})
	if !ok {
		return nil
	}
	if rawItems, ok := payload["items"].([]map[string]interface{}); ok {
		return rawItems
	}
	list, ok := payload["items"].([]interface{})
	if !ok {
		return nil
	}
	items := make([]map[string]interface{}, 0, len(list))
	for _, item := range list {
		if row, ok := item.(map[string]interface{}); ok {
			items = append(items, row)
		}
	}
	return items
}

func (s *Server) fetchRemoteNodeRegistry(ctx context.Context, node nodes.NodeInfo) ([]map[string]interface{}, error) {
	baseURL := nodeWebUIBaseURL(node)
	if baseURL == "" {
		return nil, fmt.Errorf("node %s endpoint missing", strings.TrimSpace(node.ID))
	}
	reqURL := baseURL + "/api/config?mode=normalized"
	if tok := strings.TrimSpace(node.Token); tok != "" {
		reqURL += "&token=" + url.QueryEscape(tok)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return s.fetchRemoteNodeRegistryLegacy(ctx, node)
	}
	var payload struct {
		OK        bool                    `json:"ok"`
		Config    cfgpkg.NormalizedConfig `json:"config"`
		RawConfig map[string]interface{}  `json:"raw_config"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		return s.fetchRemoteNodeRegistryLegacy(ctx, node)
	}
	items := buildRegistryItemsFromNormalizedConfig(payload.Config)
	if len(items) > 0 {
		return items, nil
	}
	return s.fetchRemoteNodeRegistryLegacy(ctx, node)
}

func (s *Server) fetchRemoteNodeRegistryLegacy(ctx context.Context, node nodes.NodeInfo) ([]map[string]interface{}, error) {
	baseURL := nodeWebUIBaseURL(node)
	if baseURL == "" {
		return nil, fmt.Errorf("node %s endpoint missing", strings.TrimSpace(node.ID))
	}
	reqURL := baseURL + "/api/subagents_runtime?action=registry"
	if tok := strings.TrimSpace(node.Token); tok != "" {
		reqURL += "&token=" + url.QueryEscape(tok)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("remote status %d", resp.StatusCode)
	}
	var payload struct {
		OK     bool `json:"ok"`
		Result struct {
			Items []map[string]interface{} `json:"items"`
		} `json:"result"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		return nil, err
	}
	return payload.Result.Items, nil
}

func buildRegistryItemsFromNormalizedConfig(view cfgpkg.NormalizedConfig) []map[string]interface{} {
	items := make([]map[string]interface{}, 0, len(view.Core.Subagents))
	for agentID, subcfg := range view.Core.Subagents {
		if strings.TrimSpace(agentID) == "" {
			continue
		}
		items = append(items, map[string]interface{}{
			"agent_id":           agentID,
			"enabled":            subcfg.Enabled,
			"type":               "subagent",
			"transport":          fallbackString(strings.TrimSpace(subcfg.RuntimeClass), "local"),
			"node_id":            "",
			"parent_agent_id":    "",
			"notify_main_policy": "final_only",
			"display_name":       "",
			"role":               strings.TrimSpace(subcfg.Role),
			"description":        "",
			"system_prompt_file": strings.TrimSpace(subcfg.Prompt),
			"prompt_file_found":  false,
			"memory_namespace":   "",
			"tool_allowlist":     append([]string(nil), subcfg.ToolAllowlist...),
			"tool_visibility":    map[string]interface{}{},
			"effective_tools":    []string{},
			"inherited_tools":    []string{},
			"routing_keywords":   routeKeywordsForRegistry(view.Runtime.Router.Rules, agentID),
			"managed_by":         "config.json",
		})
	}
	sort.Slice(items, func(i, j int) bool {
		return stringFromMap(items[i], "agent_id") < stringFromMap(items[j], "agent_id")
	})
	return items
}

func routeKeywordsForRegistry(rules []cfgpkg.AgentRouteRule, agentID string) []string {
	agentID = strings.TrimSpace(agentID)
	for _, rule := range rules {
		if strings.TrimSpace(rule.AgentID) == agentID {
			return append([]string(nil), rule.Keywords...)
		}
	}
	return nil
}

func nodeWebUIBaseURL(node nodes.NodeInfo) string {
	endpoint := strings.TrimSpace(node.Endpoint)
	if endpoint == "" || strings.EqualFold(endpoint, "gateway") {
		return ""
	}
	if strings.HasPrefix(endpoint, "http://") || strings.HasPrefix(endpoint, "https://") {
		return strings.TrimRight(endpoint, "/")
	}
	return "http://" + strings.TrimRight(endpoint, "/")
}

func fallbackNodeName(node nodes.NodeInfo) string {
	if name := strings.TrimSpace(node.Name); name != "" {
		return name
	}
	if id := strings.TrimSpace(node.ID); id != "" {
		return id
	}
	return "node"
}

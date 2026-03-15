package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	cfgpkg "github.com/YspCoder/clawgo/pkg/config"
	"github.com/YspCoder/clawgo/pkg/nodes"
	"github.com/YspCoder/clawgo/pkg/providers"
	rpcpkg "github.com/YspCoder/clawgo/pkg/rpc"
	"github.com/YspCoder/clawgo/pkg/tools"
)

func mustPrettyJSON(v interface{}) []byte {
	out, _ := json.MarshalIndent(v, "", "  ")
	return out
}

type subagentRPCAdapter struct {
	server *Server
}

func (a *subagentRPCAdapter) call(ctx context.Context, action string, args map[string]interface{}) (interface{}, *rpcpkg.Error) {
	if a == nil || a.server == nil || a.server.onSubagents == nil {
		return nil, rpcError("unavailable", "subagent runtime handler not configured", nil, false)
	}
	result, err := a.server.onSubagents(ctx, action, args)
	if err != nil {
		return nil, rpcErrorFrom(err)
	}
	return result, nil
}

func (a *subagentRPCAdapter) List(ctx context.Context, _ rpcpkg.ListSubagentsRequest) (*rpcpkg.ListSubagentsResponse, *rpcpkg.Error) {
	result, rpcErr := a.call(ctx, "list", nil)
	if rpcErr != nil {
		return nil, rpcErr
	}
	var payload struct {
		Items []*map[string]interface{} `json:"items"`
	}
	items, err := decodeResultSliceField[resultWrapperSubagentTask](result, "items")
	if err != nil {
		return nil, rpcError("internal", err.Error(), nil, false)
	}
	_ = payload
	out := make([]*resultWrapperSubagentTask, 0, len(items))
	for _, item := range items {
		if item != nil {
			out = append(out, item)
		}
	}
	return &rpcpkg.ListSubagentsResponse{Items: unwrapSubagentTasks(out)}, nil
}

func (a *subagentRPCAdapter) Snapshot(ctx context.Context, req rpcpkg.SnapshotRequest) (*rpcpkg.SnapshotResponse, *rpcpkg.Error) {
	result, rpcErr := a.call(ctx, "snapshot", map[string]interface{}{"limit": req.Limit})
	if rpcErr != nil {
		return nil, rpcErr
	}
	var snapshot struct {
		Snapshot json.RawMessage `json:"snapshot"`
	}
	if err := decodeResultObject(result, &snapshot); err != nil {
		return nil, rpcError("internal", err.Error(), nil, false)
	}
	var out rpcpkg.SnapshotResponse
	if len(snapshot.Snapshot) > 0 {
		if err := json.Unmarshal(snapshot.Snapshot, &out.Snapshot); err != nil {
			return nil, rpcError("internal", err.Error(), nil, false)
		}
	}
	return &out, nil
}

func (a *subagentRPCAdapter) Get(ctx context.Context, req rpcpkg.GetSubagentRequest) (*rpcpkg.GetSubagentResponse, *rpcpkg.Error) {
	result, rpcErr := a.call(ctx, "get", map[string]interface{}{"id": req.ID})
	if rpcErr != nil {
		return nil, rpcErr
	}
	var payload struct {
		Found bool                       `json:"found"`
		Task  *resultWrapperSubagentTask `json:"task"`
	}
	if err := decodeResultObject(result, &payload); err != nil {
		return nil, rpcError("internal", err.Error(), nil, false)
	}
	return &rpcpkg.GetSubagentResponse{Found: payload.Found, Task: unwrapSubagentTask(payload.Task)}, nil
}

func (a *subagentRPCAdapter) Spawn(ctx context.Context, req rpcpkg.SpawnSubagentRequest) (*rpcpkg.SpawnSubagentResponse, *rpcpkg.Error) {
	result, rpcErr := a.call(ctx, "spawn", map[string]interface{}{
		"task":             req.Task,
		"label":            req.Label,
		"role":             req.Role,
		"agent_id":         req.AgentID,
		"max_retries":      req.MaxRetries,
		"retry_backoff_ms": req.RetryBackoffMS,
		"timeout_sec":      req.TimeoutSec,
		"max_task_chars":   req.MaxTaskChars,
		"max_result_chars": req.MaxResultChars,
		"channel":          req.Channel,
		"chat_id":          req.ChatID,
	})
	if rpcErr != nil {
		return nil, rpcErr
	}
	var payload rpcpkg.SpawnSubagentResponse
	if err := decodeResultObject(result, &payload); err != nil {
		return nil, rpcError("internal", err.Error(), nil, false)
	}
	return &payload, nil
}

func (a *subagentRPCAdapter) DispatchAndWait(ctx context.Context, req rpcpkg.DispatchAndWaitRequest) (*rpcpkg.DispatchAndWaitResponse, *rpcpkg.Error) {
	result, rpcErr := a.call(ctx, "dispatch_and_wait", map[string]interface{}{
		"task":             req.Task,
		"label":            req.Label,
		"role":             req.Role,
		"agent_id":         req.AgentID,
		"thread_id":        req.ThreadID,
		"correlation_id":   req.CorrelationID,
		"parent_run_id":    req.ParentRunID,
		"channel":          req.Channel,
		"chat_id":          req.ChatID,
		"max_retries":      req.MaxRetries,
		"retry_backoff_ms": req.RetryBackoffMS,
		"timeout_sec":      req.TimeoutSec,
		"max_task_chars":   req.MaxTaskChars,
		"max_result_chars": req.MaxResultChars,
		"wait_timeout_sec": req.WaitTimeoutSec,
	})
	if rpcErr != nil {
		return nil, rpcErr
	}
	var payload struct {
		Task   *resultWrapperSubagentTask `json:"task"`
		Reply  json.RawMessage            `json:"reply"`
		Merged string                     `json:"merged"`
	}
	if err := decodeResultObject(result, &payload); err != nil {
		return nil, rpcError("internal", err.Error(), nil, false)
	}
	out := &rpcpkg.DispatchAndWaitResponse{
		Task:   unwrapSubagentTask(payload.Task),
		Merged: payload.Merged,
	}
	if len(payload.Reply) > 0 {
		var reply resultWrapperRouterReply
		if err := json.Unmarshal(payload.Reply, &reply); err != nil {
			return nil, rpcError("internal", err.Error(), nil, false)
		}
		out.Reply = unwrapRouterReply(&reply)
	}
	return out, nil
}

func (a *subagentRPCAdapter) Registry(ctx context.Context, _ rpcpkg.RegistryRequest) (*rpcpkg.RegistryResponse, *rpcpkg.Error) {
	result, rpcErr := a.call(ctx, "registry", nil)
	if rpcErr != nil {
		return nil, rpcErr
	}
	var payload rpcpkg.RegistryResponse
	if err := decodeResultObject(result, &payload); err != nil {
		return nil, rpcError("internal", err.Error(), nil, false)
	}
	return &payload, nil
}

type nodeRPCAdapter struct {
	server *Server
}

type workspaceRPCAdapter struct {
	server *Server
}

func (a *workspaceRPCAdapter) resolveScopeRoot(scope string) (string, *rpcpkg.Error) {
	if a == nil || a.server == nil {
		return "", rpcError("unavailable", "server unavailable", nil, false)
	}
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case "", "workspace":
		return strings.TrimSpace(a.server.workspacePath), nil
	case "memory":
		root := filepath.Join(strings.TrimSpace(a.server.workspacePath), "memory")
		if err := os.MkdirAll(root, 0755); err != nil {
			return "", rpcError("internal", err.Error(), nil, false)
		}
		return root, nil
	default:
		return "", rpcError("invalid_argument", "unsupported workspace scope", map[string]interface{}{"scope": scope}, false)
	}
}

func (a *workspaceRPCAdapter) ListFiles(_ context.Context, req rpcpkg.ListWorkspaceFilesRequest) (*rpcpkg.ListWorkspaceFilesResponse, *rpcpkg.Error) {
	root, rpcErr := a.resolveScopeRoot(req.Scope)
	if rpcErr != nil {
		return nil, rpcErr
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, rpcError("internal", err.Error(), nil, false)
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		files = append(files, entry.Name())
	}
	sort.Strings(files)
	return &rpcpkg.ListWorkspaceFilesResponse{Files: files}, nil
}

func (a *workspaceRPCAdapter) ReadFile(_ context.Context, req rpcpkg.ReadWorkspaceFileRequest) (*rpcpkg.ReadWorkspaceFileResponse, *rpcpkg.Error) {
	root, rpcErr := a.resolveScopeRoot(req.Scope)
	if rpcErr != nil {
		return nil, rpcErr
	}
	clean, content, found, err := readRelativeTextFile(root, req.Path)
	if err != nil {
		return nil, rpcError("invalid_argument", err.Error(), nil, false)
	}
	return &rpcpkg.ReadWorkspaceFileResponse{Path: clean, Found: found, Content: content}, nil
}

func (a *workspaceRPCAdapter) WriteFile(_ context.Context, req rpcpkg.WriteWorkspaceFileRequest) (*rpcpkg.WriteWorkspaceFileResponse, *rpcpkg.Error) {
	root, rpcErr := a.resolveScopeRoot(req.Scope)
	if rpcErr != nil {
		return nil, rpcErr
	}
	appendMissing := !strings.EqualFold(strings.TrimSpace(req.Scope), "memory")
	clean, err := writeRelativeTextFile(root, req.Path, req.Content, appendMissing)
	if err != nil {
		return nil, rpcError("invalid_argument", err.Error(), nil, false)
	}
	return &rpcpkg.WriteWorkspaceFileResponse{Path: clean, Saved: true}, nil
}

func (a *workspaceRPCAdapter) DeleteFile(_ context.Context, req rpcpkg.DeleteWorkspaceFileRequest) (*rpcpkg.DeleteWorkspaceFileResponse, *rpcpkg.Error) {
	root, rpcErr := a.resolveScopeRoot(req.Scope)
	if rpcErr != nil {
		return nil, rpcErr
	}
	clean, full, err := resolveRelativeFilePath(root, req.Path)
	if err != nil {
		return nil, rpcError("invalid_argument", err.Error(), nil, false)
	}
	if err := os.Remove(full); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &rpcpkg.DeleteWorkspaceFileResponse{Path: clean, Deleted: false}, nil
		}
		return nil, rpcError("internal", err.Error(), nil, false)
	}
	return &rpcpkg.DeleteWorkspaceFileResponse{Path: clean, Deleted: true}, nil
}

func (a *nodeRPCAdapter) Register(_ context.Context, req rpcpkg.RegisterNodeRequest) (*rpcpkg.RegisterNodeResponse, *rpcpkg.Error) {
	if a == nil || a.server == nil || a.server.mgr == nil {
		return nil, rpcError("unavailable", "nodes manager unavailable", nil, false)
	}
	if strings.TrimSpace(req.Node.ID) == "" {
		return nil, rpcError("invalid_argument", "id required", nil, false)
	}
	a.server.mgr.Upsert(req.Node)
	return &rpcpkg.RegisterNodeResponse{ID: req.Node.ID}, nil
}

func (a *nodeRPCAdapter) Heartbeat(_ context.Context, req rpcpkg.HeartbeatNodeRequest) (*rpcpkg.HeartbeatNodeResponse, *rpcpkg.Error) {
	if a == nil || a.server == nil || a.server.mgr == nil {
		return nil, rpcError("unavailable", "nodes manager unavailable", nil, false)
	}
	id := strings.TrimSpace(req.ID)
	if id == "" {
		return nil, rpcError("invalid_argument", "id required", nil, false)
	}
	n, ok := a.server.mgr.Get(id)
	if !ok {
		return nil, rpcError("not_found", "node not found", nil, false)
	}
	a.server.mgr.Upsert(n)
	return &rpcpkg.HeartbeatNodeResponse{ID: id}, nil
}

func (a *nodeRPCAdapter) Dispatch(ctx context.Context, req rpcpkg.DispatchNodeRequest) (*rpcpkg.DispatchNodeResponse, *rpcpkg.Error) {
	if a == nil || a.server == nil || a.server.onNodeDispatch == nil {
		return nil, rpcError("unavailable", "node dispatch handler not configured", nil, false)
	}
	nodeID := strings.TrimSpace(req.Node)
	action := strings.TrimSpace(req.Action)
	if nodeID == "" || action == "" {
		return nil, rpcError("invalid_argument", "node and action are required", nil, false)
	}
	resp, err := a.server.onNodeDispatch(ctx, resultNodeRequest{
		Node:   nodeID,
		Action: action,
		Task:   req.Task,
		Model:  req.Model,
		Args:   req.Args,
	}.unwrap(), strings.TrimSpace(req.Mode))
	if err != nil {
		return nil, rpcErrorFrom(err)
	}
	return &rpcpkg.DispatchNodeResponse{Result: resp}, nil
}

func (a *nodeRPCAdapter) ListArtifacts(_ context.Context, req rpcpkg.ListNodeArtifactsRequest) (*rpcpkg.ListNodeArtifactsResponse, *rpcpkg.Error) {
	if a == nil || a.server == nil {
		return nil, rpcError("unavailable", "server unavailable", nil, false)
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}
	return &rpcpkg.ListNodeArtifactsResponse{
		Items:             a.server.webUINodeArtifactsPayloadFiltered(strings.TrimSpace(req.Node), strings.TrimSpace(req.Action), strings.TrimSpace(req.Kind), limit),
		ArtifactRetention: a.server.applyNodeArtifactRetention(),
	}, nil
}

func (a *nodeRPCAdapter) GetArtifact(_ context.Context, req rpcpkg.GetNodeArtifactRequest) (*rpcpkg.GetNodeArtifactResponse, *rpcpkg.Error) {
	if a == nil || a.server == nil {
		return nil, rpcError("unavailable", "server unavailable", nil, false)
	}
	id := strings.TrimSpace(req.ID)
	if id == "" {
		return nil, rpcError("invalid_argument", "id is required", nil, false)
	}
	item, ok := a.server.findNodeArtifactByID(id)
	if !ok {
		return &rpcpkg.GetNodeArtifactResponse{Found: false}, nil
	}
	return &rpcpkg.GetNodeArtifactResponse{Found: true, Artifact: item}, nil
}

func (a *nodeRPCAdapter) DeleteArtifact(_ context.Context, req rpcpkg.DeleteNodeArtifactRequest) (*rpcpkg.DeleteNodeArtifactResponse, *rpcpkg.Error) {
	if a == nil || a.server == nil {
		return nil, rpcError("unavailable", "server unavailable", nil, false)
	}
	id := strings.TrimSpace(req.ID)
	if id == "" {
		return nil, rpcError("invalid_argument", "id is required", nil, false)
	}
	deletedFile, deletedAudit, err := a.server.deleteNodeArtifact(id)
	if err != nil {
		return nil, rpcErrorFrom(err)
	}
	return &rpcpkg.DeleteNodeArtifactResponse{ArtifactDeleteResult: rpcpkg.ArtifactDeleteResult{
		ID: id, DeletedFile: deletedFile, DeletedAudit: deletedAudit,
	}}, nil
}

func (a *nodeRPCAdapter) PruneArtifacts(_ context.Context, req rpcpkg.PruneNodeArtifactsRequest) (*rpcpkg.PruneNodeArtifactsResponse, *rpcpkg.Error) {
	if a == nil || a.server == nil {
		return nil, rpcError("unavailable", "server unavailable", nil, false)
	}
	limit := req.Limit
	if limit <= 0 || limit > 5000 {
		limit = 5000
	}
	keepLatest := req.KeepLatest
	if keepLatest < 0 {
		keepLatest = 0
	}
	items := a.server.webUINodeArtifactsPayloadFiltered(strings.TrimSpace(req.Node), strings.TrimSpace(req.Action), strings.TrimSpace(req.Kind), limit)
	pruned := 0
	deletedFiles := 0
	for index, item := range items {
		if index < keepLatest {
			continue
		}
		deletedFile, deletedAudit, err := a.server.deleteNodeArtifact(strings.TrimSpace(fmt.Sprint(item["id"])))
		if err != nil || !deletedAudit {
			continue
		}
		pruned++
		if deletedFile {
			deletedFiles++
		}
	}
	return &rpcpkg.PruneNodeArtifactsResponse{ArtifactPruneResult: rpcpkg.ArtifactPruneResult{
		Pruned: pruned, DeletedFiles: deletedFiles, Kept: keepLatest,
	}}, nil
}

func (s *Server) subagentRPCService() rpcpkg.SubagentService {
	return &subagentRPCAdapter{server: s}
}

func (s *Server) nodeRPCService() rpcpkg.NodeService {
	return &nodeRPCAdapter{server: s}
}

type providerRPCAdapter struct {
	server *Server
}

func (a *providerRPCAdapter) ListModels(_ context.Context, req rpcpkg.ListProviderModelsRequest) (*rpcpkg.ListProviderModelsResponse, *rpcpkg.Error) {
	if a == nil || a.server == nil {
		return nil, rpcError("unavailable", "server unavailable", nil, false)
	}
	cfg, err := a.server.loadConfig()
	if err != nil {
		return nil, rpcErrorFrom(err)
	}
	providerName := strings.TrimSpace(req.Provider)
	if providerName == "" {
		return nil, rpcError("invalid_argument", "provider is required", nil, false)
	}
	models := providers.GetProviderModels(cfg, providerName)
	provider, err := providers.CreateProviderByName(cfg, providerName)
	if err != nil {
		return nil, rpcErrorFrom(err)
	}
	return &rpcpkg.ListProviderModelsResponse{
		Provider: providerName,
		Models:   models,
		Default:  strings.TrimSpace(provider.GetDefaultModel()),
	}, nil
}

func (a *providerRPCAdapter) UpdateModels(_ context.Context, req rpcpkg.UpdateProviderModelsRequest) (*rpcpkg.UpdateProviderModelsResponse, *rpcpkg.Error) {
	if a == nil || a.server == nil {
		return nil, rpcError("unavailable", "server unavailable", nil, false)
	}
	cfg, pc, err := a.server.loadProviderConfig(strings.TrimSpace(req.Provider))
	if err != nil {
		return nil, rpcErrorFrom(err)
	}
	models := make([]string, 0, len(req.Models)+1)
	for _, model := range req.Models {
		models = appendUniqueStrings(models, model)
	}
	models = appendUniqueStrings(models, req.Model)
	if len(models) == 0 {
		return nil, rpcError("invalid_argument", "model required", nil, false)
	}
	pc.Models = models
	if err := a.server.saveProviderConfig(cfg, req.Provider, pc); err != nil {
		return nil, rpcErrorFrom(err)
	}
	return &rpcpkg.UpdateProviderModelsResponse{Provider: strings.TrimSpace(req.Provider), Models: pc.Models}, nil
}

func (a *providerRPCAdapter) Chat(ctx context.Context, req rpcpkg.ProviderChatRequest) (*rpcpkg.ProviderChatResponse, *rpcpkg.Error) {
	provider, model, messages, toolDefs, err := a.resolveProviderRequest(req.Provider, req.Model, req.Messages, req.Tools)
	if err != nil {
		return nil, rpcErrorFrom(err)
	}
	resp, err := provider.Chat(ctx, messages, toolDefs, model, req.Options)
	if err != nil {
		return nil, rpcErrorFrom(err)
	}
	return &rpcpkg.ProviderChatResponse{
		Content:      strings.TrimSpace(resp.Content),
		ToolCalls:    marshalToolCalls(resp.ToolCalls),
		FinishReason: strings.TrimSpace(resp.FinishReason),
		Usage:        marshalUsage(resp.Usage),
	}, nil
}

func (a *providerRPCAdapter) CountTokens(ctx context.Context, req rpcpkg.ProviderCountTokensRequest) (*rpcpkg.ProviderCountTokensResponse, *rpcpkg.Error) {
	provider, model, messages, toolDefs, err := a.resolveProviderRequest(req.Provider, req.Model, req.Messages, req.Tools)
	if err != nil {
		return nil, rpcErrorFrom(err)
	}
	counter, ok := provider.(providers.TokenCounter)
	if !ok {
		return nil, rpcError("unavailable", "provider does not support count_tokens", nil, false)
	}
	usage, err := counter.CountTokens(ctx, messages, toolDefs, model, req.Options)
	if err != nil {
		return nil, rpcErrorFrom(err)
	}
	return &rpcpkg.ProviderCountTokensResponse{Usage: marshalUsage(usage)}, nil
}

func (a *providerRPCAdapter) RuntimeView(_ context.Context, req rpcpkg.ProviderRuntimeViewRequest) (*rpcpkg.ProviderRuntimeViewResponse, *rpcpkg.Error) {
	if a == nil || a.server == nil {
		return nil, rpcError("unavailable", "server unavailable", nil, false)
	}
	cfg, err := a.server.loadConfig()
	if err != nil {
		return nil, rpcErrorFrom(err)
	}
	query := providers.ProviderRuntimeQuery{
		Provider:    strings.TrimSpace(req.Provider),
		EventKind:   strings.TrimSpace(req.Kind),
		Reason:      strings.TrimSpace(req.Reason),
		Target:      strings.TrimSpace(req.Target),
		Sort:        strings.TrimSpace(req.Sort),
		ChangesOnly: req.ChangesOnly,
		Limit:       req.Limit,
		Cursor:      req.Cursor,
		HealthBelow: req.HealthBelow,
	}
	if req.WindowSec > 0 {
		query.Window = time.Duration(req.WindowSec) * time.Second
	}
	if req.CooldownUntilBeforeSec > 0 {
		query.CooldownBefore = time.Now().Add(time.Duration(req.CooldownUntilBeforeSec) * time.Second)
	}
	return &rpcpkg.ProviderRuntimeViewResponse{View: providers.GetProviderRuntimeView(cfg, query)}, nil
}

func (a *providerRPCAdapter) RuntimeAction(_ context.Context, req rpcpkg.ProviderRuntimeActionRequest) (*rpcpkg.ProviderRuntimeActionResponse, *rpcpkg.Error) {
	if a == nil || a.server == nil {
		return nil, rpcError("unavailable", "server unavailable", nil, false)
	}
	cfg, providerName, err := a.server.loadRuntimeProviderName(strings.TrimSpace(req.Provider))
	if err != nil {
		return nil, rpcErrorFrom(err)
	}
	action := strings.ToLower(strings.TrimSpace(req.Action))
	result := map[string]interface{}{"provider": providerName}
	handler := providerRuntimeActionHandlers[action]
	if handler == nil {
		return nil, rpcError("invalid_argument", "unsupported action", map[string]interface{}{"action": action}, false)
	}
	if err := handler(cfg, providerName, req, result); err != nil {
		return nil, rpcErrorFrom(err)
	}
	return &rpcpkg.ProviderRuntimeActionResponse{Result: result}, nil
}

type providerRuntimeActionHandler func(*cfgpkg.Config, string, rpcpkg.ProviderRuntimeActionRequest, map[string]interface{}) error

var providerRuntimeActionHandlers = map[string]providerRuntimeActionHandler{
	"clear_api_cooldown": func(_ *cfgpkg.Config, providerName string, _ rpcpkg.ProviderRuntimeActionRequest, result map[string]interface{}) error {
		providers.ClearProviderAPICooldown(providerName)
		result["cleared"] = true
		return nil
	},
	"clear_history": func(_ *cfgpkg.Config, providerName string, _ rpcpkg.ProviderRuntimeActionRequest, result map[string]interface{}) error {
		providers.ClearProviderRuntimeHistory(providerName)
		result["cleared"] = true
		return nil
	},
	"refresh_now": func(cfg *cfgpkg.Config, providerName string, req rpcpkg.ProviderRuntimeActionRequest, result map[string]interface{}) error {
		refreshResult, err := providers.RefreshProviderRuntimeNow(cfg, providerName, req.OnlyExpiring)
		if err != nil {
			return err
		}
		order, _ := providers.RerankProviderRuntime(cfg, providerName)
		summary := providers.GetProviderRuntimeSummary(cfg, providers.ProviderRuntimeQuery{Provider: providerName, HealthBelow: 50})
		result["refreshed"] = true
		result["result"] = refreshResult
		result["candidate_order"] = order
		result["summary"] = summary
		return nil
	},
	"rerank": func(cfg *cfgpkg.Config, providerName string, _ rpcpkg.ProviderRuntimeActionRequest, result map[string]interface{}) error {
		order, err := providers.RerankProviderRuntime(cfg, providerName)
		if err != nil {
			return err
		}
		result["reranked"] = true
		result["candidate_order"] = order
		return nil
	},
}

func (a *providerRPCAdapter) resolveProviderRequest(providerName, model string, rawMessages []map[string]interface{}, rawTools []map[string]interface{}) (providers.LLMProvider, string, []providers.Message, []providers.ToolDefinition, error) {
	if a == nil || a.server == nil {
		return nil, "", nil, nil, fmt.Errorf("server unavailable")
	}
	cfg, err := a.server.loadConfig()
	if err != nil {
		return nil, "", nil, nil, err
	}
	providerName = strings.TrimSpace(providerName)
	if providerName == "" {
		return nil, "", nil, nil, fmt.Errorf("provider is required")
	}
	provider, err := providers.CreateProviderByName(cfg, providerName)
	if err != nil {
		return nil, "", nil, nil, err
	}
	if strings.TrimSpace(model) == "" {
		model = provider.GetDefaultModel()
	}
	messages, err := decodeProviderMessages(rawMessages)
	if err != nil {
		return nil, "", nil, nil, err
	}
	tools, err := decodeProviderTools(rawTools)
	if err != nil {
		return nil, "", nil, nil, err
	}
	return provider, strings.TrimSpace(model), messages, tools, nil
}

func (s *Server) providerRPCService() rpcpkg.ProviderService {
	return &providerRPCAdapter{server: s}
}

func (s *Server) workspaceRPCService() rpcpkg.WorkspaceService {
	return &workspaceRPCAdapter{server: s}
}

func (s *Server) configRPCService() rpcpkg.ConfigService {
	return &configRPCAdapter{server: s}
}

func (s *Server) cronRPCService() rpcpkg.CronService {
	return &cronRPCAdapter{server: s}
}

func (s *Server) skillsRPCService() rpcpkg.SkillsService {
	return &skillsRPCAdapter{server: s}
}

type configRPCAdapter struct {
	server *Server
}

type cronRPCAdapter struct {
	server *Server
}

type skillsRPCAdapter struct {
	server *Server
}

func (a *configRPCAdapter) View(_ context.Context, req rpcpkg.ConfigViewRequest) (*rpcpkg.ConfigViewResponse, *rpcpkg.Error) {
	if a == nil || a.server == nil {
		return nil, rpcError("unavailable", "server unavailable", nil, false)
	}
	if strings.TrimSpace(a.server.configPath) == "" {
		return nil, rpcError("unavailable", "config path not set", nil, false)
	}
	if strings.EqualFold(strings.TrimSpace(req.Mode), "normalized") {
		cfg, err := cfgpkg.LoadConfig(a.server.configPath)
		if err != nil {
			return nil, rpcErrorFrom(err)
		}
		resp := &rpcpkg.ConfigViewResponse{
			Config:    cfg.NormalizedView(),
			RawConfig: cfg,
		}
		if req.IncludeHotReloadInfo {
			info := hotReloadFieldInfo()
			paths := make([]string, 0, len(info))
			for _, it := range info {
				if p := stringFromMap(it, "path"); p != "" {
					paths = append(paths, p)
				}
			}
			resp.HotReloadFields = paths
			resp.HotReloadFieldDetails = info
		}
		return resp, nil
	}
	b, err := os.ReadFile(a.server.configPath)
	if err != nil {
		return nil, rpcErrorFrom(err)
	}
	cfgDefault := cfgpkg.DefaultConfig()
	defBytes, _ := json.Marshal(cfgDefault)
	var merged map[string]interface{}
	_ = json.Unmarshal(defBytes, &merged)
	var loaded map[string]interface{}
	if err := json.Unmarshal(b, &loaded); err != nil {
		return nil, rpcErrorFrom(err)
	}
	merged = mergeJSONMap(merged, loaded)
	resp := &rpcpkg.ConfigViewResponse{Config: merged, PrettyText: string(mustPrettyJSON(merged))}
	if req.IncludeHotReloadInfo {
		info := hotReloadFieldInfo()
		paths := make([]string, 0, len(info))
		for _, it := range info {
			if p := stringFromMap(it, "path"); p != "" {
				paths = append(paths, p)
			}
		}
		resp.HotReloadFields = paths
		resp.HotReloadFieldDetails = info
	}
	return resp, nil
}

func (a *configRPCAdapter) Save(_ context.Context, req rpcpkg.ConfigSaveRequest) (*rpcpkg.ConfigSaveResponse, *rpcpkg.Error) {
	if a == nil || a.server == nil {
		return nil, rpcError("unavailable", "server unavailable", nil, false)
	}
	if strings.TrimSpace(a.server.configPath) == "" {
		return nil, rpcError("unavailable", "config path not set", nil, false)
	}
	body := req.Config
	oldCfgRaw, _ := os.ReadFile(a.server.configPath)
	var oldMap map[string]interface{}
	_ = json.Unmarshal(oldCfgRaw, &oldMap)
	riskyOldMap := oldMap
	riskyNewMap := body
	if strings.EqualFold(strings.TrimSpace(req.Mode), "normalized") {
		if loaded, err := cfgpkg.LoadConfig(a.server.configPath); err == nil && loaded != nil {
			if raw, err := json.Marshal(loaded.NormalizedView()); err == nil {
				_ = json.Unmarshal(raw, &riskyOldMap)
			}
		}
	}
	riskyPaths := collectRiskyConfigPaths(riskyOldMap, riskyNewMap)
	changedRisky := make([]string, 0)
	for _, p := range riskyPaths {
		if fmt.Sprintf("%v", getPathValue(riskyOldMap, p)) != fmt.Sprintf("%v", getPathValue(riskyNewMap, p)) {
			changedRisky = append(changedRisky, p)
		}
	}
	if len(changedRisky) > 0 && !req.ConfirmRisky {
		return &rpcpkg.ConfigSaveResponse{
			Saved:           false,
			RequiresConfirm: true,
			ChangedFields:   changedRisky,
		}, rpcError("invalid_argument", "risky fields changed; confirmation required", map[string]interface{}{"changed_fields": changedRisky}, false)
	}

	cfg := cfgpkg.DefaultConfig()
	if strings.EqualFold(strings.TrimSpace(req.Mode), "normalized") {
		loaded, err := cfgpkg.LoadConfig(a.server.configPath)
		if err != nil {
			return nil, rpcErrorFrom(err)
		}
		cfg = loaded
		candidate, err := json.Marshal(body)
		if err != nil {
			return nil, rpcErrorFrom(err)
		}
		var normalized cfgpkg.NormalizedConfig
		dec := json.NewDecoder(bytes.NewReader(candidate))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&normalized); err != nil {
			return nil, rpcError("invalid_argument", "normalized config validation failed: "+err.Error(), nil, false)
		}
		cfg.ApplyNormalizedView(normalized)
	} else {
		candidate, err := json.Marshal(body)
		if err != nil {
			return nil, rpcErrorFrom(err)
		}
		dec := json.NewDecoder(bytes.NewReader(candidate))
		dec.DisallowUnknownFields()
		if err := dec.Decode(cfg); err != nil {
			return nil, rpcError("invalid_argument", "config schema validation failed: "+err.Error(), nil, false)
		}
	}
	if errs := cfgpkg.Validate(cfg); len(errs) > 0 {
		list := make([]string, 0, len(errs))
		for _, e := range errs {
			list = append(list, e.Error())
		}
		return nil, rpcError("invalid_argument", "config validation failed", list, false)
	}
	if err := cfgpkg.SaveConfig(a.server.configPath, cfg); err != nil {
		return nil, rpcErrorFrom(err)
	}
	if a.server.onConfigAfter != nil {
		if err := a.server.onConfigAfter(); err != nil {
			return nil, rpcErrorFrom(err)
		}
	} else {
		if err := requestSelfReloadSignal(); err != nil {
			return nil, rpcErrorFrom(err)
		}
	}
	return &rpcpkg.ConfigSaveResponse{Saved: true}, nil
}

func (a *cronRPCAdapter) List(ctx context.Context, _ rpcpkg.ListCronJobsRequest) (*rpcpkg.ListCronJobsResponse, *rpcpkg.Error) {
	if a == nil || a.server == nil || a.server.onCron == nil {
		return nil, rpcError("unavailable", "cron handler not configured", nil, false)
	}
	res, err := a.server.onCron("list", map[string]interface{}{})
	if err != nil {
		return nil, rpcErrorFrom(err)
	}
	jobs := normalizeCronJobs(res)
	out := make([]interface{}, 0, len(jobs))
	for _, job := range jobs {
		out = append(out, job)
	}
	return &rpcpkg.ListCronJobsResponse{Jobs: out}, nil
}

func (a *cronRPCAdapter) Get(ctx context.Context, req rpcpkg.GetCronJobRequest) (*rpcpkg.GetCronJobResponse, *rpcpkg.Error) {
	if a == nil || a.server == nil || a.server.onCron == nil {
		return nil, rpcError("unavailable", "cron handler not configured", nil, false)
	}
	res, err := a.server.onCron("get", map[string]interface{}{"id": strings.TrimSpace(req.ID)})
	if err != nil {
		return nil, rpcErrorFrom(err)
	}
	return &rpcpkg.GetCronJobResponse{Job: normalizeCronJob(res)}, nil
}

func (a *cronRPCAdapter) Mutate(ctx context.Context, req rpcpkg.MutateCronJobRequest) (*rpcpkg.MutateCronJobResponse, *rpcpkg.Error) {
	if a == nil || a.server == nil || a.server.onCron == nil {
		return nil, rpcError("unavailable", "cron handler not configured", nil, false)
	}
	args := req.Args
	if args == nil {
		args = map[string]interface{}{}
	}
	action := strings.ToLower(strings.TrimSpace(req.Action))
	if action == "" {
		action = "create"
	}
	res, err := a.server.onCron(action, args)
	if err != nil {
		return nil, rpcErrorFrom(err)
	}
	return &rpcpkg.MutateCronJobResponse{Result: normalizeCronJob(res)}, nil
}

func (a *skillsRPCAdapter) skillsDir() (string, *rpcpkg.Error) {
	if a == nil || a.server == nil {
		return "", rpcError("unavailable", "server unavailable", nil, false)
	}
	skillsDir := filepath.Join(a.server.workspacePath, "skills")
	if strings.TrimSpace(skillsDir) == "" {
		return "", rpcError("unavailable", "workspace not configured", nil, false)
	}
	if err := os.MkdirAll(skillsDir, 0755); err != nil {
		return "", rpcErrorFrom(err)
	}
	return skillsDir, nil
}

func (a *skillsRPCAdapter) resolveSkillPath(skillsDir, name string) (string, *rpcpkg.Error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", rpcError("invalid_argument", "name required", nil, false)
	}
	cands := []string{
		filepath.Join(skillsDir, name),
		filepath.Join(skillsDir, name+".disabled"),
		filepath.Join("/root/clawgo/workspace/skills", name),
		filepath.Join("/root/clawgo/workspace/skills", name+".disabled"),
	}
	for _, p := range cands {
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			return p, nil
		}
	}
	return "", rpcError("not_found", "skill not found: "+name, nil, false)
}

func (a *skillsRPCAdapter) View(ctx context.Context, req rpcpkg.SkillsViewRequest) (*rpcpkg.SkillsViewResponse, *rpcpkg.Error) {
	skillsDir, rpcErr := a.skillsDir()
	if rpcErr != nil {
		return nil, rpcErr
	}
	clawhubPath := strings.TrimSpace(resolveClawHubBinary(ctx))
	clawhubInstalled := clawhubPath != ""
	if id := strings.TrimSpace(req.ID); id != "" {
		skillPath, rpcErr := a.resolveSkillPath(skillsDir, id)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if req.Files {
			var files []string
			_ = filepath.WalkDir(skillPath, func(path string, d os.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return nil
				}
				rel, _ := filepath.Rel(skillPath, path)
				if strings.HasPrefix(rel, "..") {
					return nil
				}
				files = append(files, filepath.ToSlash(rel))
				return nil
			})
			return &rpcpkg.SkillsViewResponse{ID: id, FilesList: files}, nil
		}
		if f := strings.TrimSpace(req.File); f != "" {
			clean, content, found, err := readRelativeTextFile(skillPath, f)
			if err != nil {
				return nil, rpcError("invalid_argument", err.Error(), nil, false)
			}
			if !found {
				return nil, rpcError("not_found", os.ErrNotExist.Error(), nil, false)
			}
			return &rpcpkg.SkillsViewResponse{ID: id, File: filepath.ToSlash(clean), Content: content}, nil
		}
	}
	type skillItem struct {
		ID            string
		Name          string
		Description   string
		Tools         []string
		SystemPrompt  string
		Enabled       bool
		UpdateChecked bool
		RemoteFound   bool
		RemoteVersion string
		CheckError    string
		Source        string
	}
	candDirs := []string{skillsDir, filepath.Join("/root/clawgo/workspace", "skills")}
	seenDirs := map[string]struct{}{}
	seenSkills := map[string]struct{}{}
	items := make([]rpcpkg.SkillsViewItem, 0)
	for _, dir := range candDirs {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		if _, ok := seenDirs[dir]; ok {
			continue
		}
		seenDirs[dir] = struct{}{}
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, rpcErrorFrom(err)
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			name := e.Name()
			enabled := !strings.HasSuffix(name, ".disabled")
			baseName := strings.TrimSuffix(name, ".disabled")
			if _, ok := seenSkills[baseName]; ok {
				continue
			}
			seenSkills[baseName] = struct{}{}
			desc, skillTools, sys := readSkillMeta(filepath.Join(dir, name, "SKILL.md"))
			if desc == "" || len(skillTools) == 0 || sys == "" {
				d2, t2, s2 := readSkillMeta(filepath.Join(dir, baseName, "SKILL.md"))
				if desc == "" {
					desc = d2
				}
				if len(skillTools) == 0 {
					skillTools = t2
				}
				if sys == "" {
					sys = s2
				}
			}
			if skillTools == nil {
				skillTools = []string{}
			}
			it := rpcpkg.SkillsViewItem{
				ID:            baseName,
				Name:          baseName,
				Description:   desc,
				Tools:         skillTools,
				SystemPrompt:  sys,
				Enabled:       enabled,
				UpdateChecked: req.CheckUpdates && clawhubInstalled,
				Source:        dir,
			}
			if req.CheckUpdates && clawhubInstalled {
				found, version, checkErr := queryClawHubSkillVersion(ctx, baseName)
				it.RemoteFound = found
				it.RemoteVersion = version
				if checkErr != nil {
					it.CheckError = checkErr.Error()
				}
			}
			items = append(items, it)
		}
	}
	return &rpcpkg.SkillsViewResponse{
		Skills:           items,
		Source:           "clawhub",
		ClawhubInstalled: clawhubInstalled,
		ClawhubPath:      clawhubPath,
	}, nil
}

func createOrUpdateSkillAtPath(enabledPath, name, desc, sys string, toolsList []string, checkExists bool) error {
	if checkExists {
		if _, err := os.Stat(enabledPath); err == nil {
			return fmt.Errorf("skill already exists")
		}
	}
	if err := os.MkdirAll(filepath.Join(enabledPath, "scripts"), 0755); err != nil {
		return err
	}
	skillMD := buildSkillMarkdown(name, desc, toolsList, sys)
	return os.WriteFile(filepath.Join(enabledPath, "SKILL.md"), []byte(skillMD), 0644)
}

func (a *skillsRPCAdapter) Mutate(ctx context.Context, req rpcpkg.SkillsMutateRequest) (*rpcpkg.SkillsMutateResponse, *rpcpkg.Error) {
	skillsDir, rpcErr := a.skillsDir()
	if rpcErr != nil {
		return nil, rpcErr
	}
	action := strings.ToLower(strings.TrimSpace(req.Action))
	if action == "" {
		return nil, rpcError("invalid_argument", "action required", nil, false)
	}
	name := strings.TrimSpace(firstNonEmptyString(req.Name, req.ID))
	enabledPath := filepath.Join(skillsDir, name)
	disabledPath := enabledPath + ".disabled"
	switch action {
	case "install_clawhub":
		output, err := ensureClawHubReady(ctx)
		if err != nil {
			return nil, rpcErrorFrom(err)
		}
		return &rpcpkg.SkillsMutateResponse{InstalledOK: true, Output: output, ClawhubPath: resolveClawHubBinary(ctx)}, nil
	case "install":
		if name == "" {
			return nil, rpcError("invalid_argument", "name required", nil, false)
		}
		clawhubPath := strings.TrimSpace(resolveClawHubBinary(ctx))
		if clawhubPath == "" {
			return nil, rpcError("invalid_argument", "clawhub is not installed. please install clawhub first.", nil, false)
		}
		args := []string{"install", name}
		if req.IgnoreSuspicious {
			args = append(args, "--force")
		}
		cmd := exec.CommandContext(ctx, clawhubPath, args...)
		cmd.Dir = strings.TrimSpace(a.server.workspacePath)
		out, err := cmd.CombinedOutput()
		if err != nil {
			outText := string(out)
			lower := strings.ToLower(outText)
			if strings.Contains(lower, "rate limit exceeded") || strings.Contains(lower, "too many requests") {
				return nil, rpcError("unavailable", fmt.Sprintf("clawhub rate limit exceeded. please retry later or configure auth token.\n%s", outText), nil, true)
			}
			return nil, rpcError("internal", fmt.Sprintf("install failed: %v\n%s", err, outText), nil, false)
		}
		return &rpcpkg.SkillsMutateResponse{Installed: name, Output: string(out)}, nil
	case "enable":
		if name == "" {
			return nil, rpcError("invalid_argument", "name required", nil, false)
		}
		if _, err := os.Stat(disabledPath); err == nil {
			if err := os.Rename(disabledPath, enabledPath); err != nil {
				return nil, rpcErrorFrom(err)
			}
		}
		return &rpcpkg.SkillsMutateResponse{Name: name}, nil
	case "disable":
		if name == "" {
			return nil, rpcError("invalid_argument", "name required", nil, false)
		}
		if _, err := os.Stat(enabledPath); err == nil {
			if err := os.Rename(enabledPath, disabledPath); err != nil {
				return nil, rpcErrorFrom(err)
			}
		}
		return &rpcpkg.SkillsMutateResponse{Name: name}, nil
	case "write_file":
		if name == "" {
			return nil, rpcError("invalid_argument", "name required", nil, false)
		}
		skillPath, rpcErr := a.resolveSkillPath(skillsDir, name)
		if rpcErr != nil {
			return nil, rpcErr
		}
		clean, err := writeRelativeTextFile(skillPath, req.File, req.Content, true)
		if err != nil {
			return nil, rpcError("invalid_argument", err.Error(), nil, false)
		}
		return &rpcpkg.SkillsMutateResponse{Name: name, File: filepath.ToSlash(clean)}, nil
	case "create":
		if name == "" {
			return nil, rpcError("invalid_argument", "name required", nil, false)
		}
		if err := createOrUpdateSkillAtPath(enabledPath, name, req.Description, req.SystemPrompt, req.Tools, true); err != nil {
			return nil, rpcError("invalid_argument", err.Error(), nil, false)
		}
		return &rpcpkg.SkillsMutateResponse{Name: name}, nil
	case "update":
		if name == "" {
			return nil, rpcError("invalid_argument", "name required", nil, false)
		}
		if err := createOrUpdateSkillAtPath(enabledPath, name, req.Description, req.SystemPrompt, req.Tools, false); err != nil {
			return nil, rpcErrorFrom(err)
		}
		return &rpcpkg.SkillsMutateResponse{Name: name}, nil
	case "delete":
		if name == "" {
			return nil, rpcError("invalid_argument", "id required", nil, false)
		}
		deleted := false
		if err := os.RemoveAll(enabledPath); err == nil {
			deleted = true
		}
		if err := os.RemoveAll(disabledPath); err == nil {
			deleted = true
		}
		return &rpcpkg.SkillsMutateResponse{Deleted: deleted, ID: name}, nil
	default:
		return nil, rpcError("invalid_argument", "unsupported action", nil, false)
	}
}

func rpcError(code, message string, details interface{}, retryable bool) *rpcpkg.Error {
	return &rpcpkg.Error{
		Code:      strings.TrimSpace(code),
		Message:   strings.TrimSpace(message),
		Details:   details,
		Retryable: retryable,
	}
}

func rpcErrorFrom(err error) *rpcpkg.Error {
	if err == nil {
		return nil
	}
	message := strings.TrimSpace(err.Error())
	code := "internal"
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		code = "timeout"
	case strings.Contains(strings.ToLower(message), "not found"):
		code = "not_found"
	case strings.Contains(strings.ToLower(message), "required"):
		code = "invalid_argument"
	case strings.Contains(strings.ToLower(message), "not configured"), strings.Contains(strings.ToLower(message), "unavailable"):
		code = "unavailable"
	}
	return rpcError(code, message, nil, false)
}

func rpcHTTPStatus(err *rpcpkg.Error) int {
	if err == nil {
		return http.StatusOK
	}
	switch strings.TrimSpace(err.Code) {
	case "invalid_argument":
		return http.StatusBadRequest
	case "permission_denied":
		return http.StatusForbidden
	case "not_found":
		return http.StatusNotFound
	case "timeout":
		return http.StatusGatewayTimeout
	case "unavailable":
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

func decodeResultObject(result interface{}, target interface{}) error {
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

func decodeProviderMessages(raw []map[string]interface{}) ([]providers.Message, error) {
	if len(raw) == 0 {
		return []providers.Message{}, nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var out []providers.Message
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func decodeProviderTools(raw []map[string]interface{}) ([]providers.ToolDefinition, error) {
	if len(raw) == 0 {
		return []providers.ToolDefinition{}, nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var out []providers.ToolDefinition
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func marshalToolCalls(in []providers.ToolCall) []map[string]interface{} {
	if len(in) == 0 {
		return []map[string]interface{}{}
	}
	data, err := json.Marshal(in)
	if err != nil {
		return []map[string]interface{}{}
	}
	var out []map[string]interface{}
	if err := json.Unmarshal(data, &out); err != nil {
		return []map[string]interface{}{}
	}
	return out
}

func marshalUsage(in *providers.UsageInfo) map[string]interface{} {
	if in == nil {
		return nil
	}
	return map[string]interface{}{
		"prompt_tokens":     in.PromptTokens,
		"completion_tokens": in.CompletionTokens,
		"total_tokens":      in.TotalTokens,
	}
}

func decodeResultSliceField[T any](result interface{}, field string) ([]*T, error) {
	if strings.TrimSpace(field) == "" {
		return nil, fmt.Errorf("field is required")
	}
	var payload map[string]json.RawMessage
	if err := decodeResultObject(result, &payload); err != nil {
		return nil, err
	}
	raw := payload[field]
	if len(raw) == 0 {
		return []*T{}, nil
	}
	var items []*T
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, err
	}
	return items, nil
}

type resultWrapperSubagentTask struct {
	ID               string                 `json:"id"`
	Task             string                 `json:"task"`
	Label            string                 `json:"label"`
	Role             string                 `json:"role"`
	AgentID          string                 `json:"agent_id"`
	Transport        string                 `json:"transport,omitempty"`
	NodeID           string                 `json:"node_id,omitempty"`
	ParentAgentID    string                 `json:"parent_agent_id,omitempty"`
	NotifyMainPolicy string                 `json:"notify_main_policy,omitempty"`
	SessionKey       string                 `json:"session_key"`
	MemoryNS         string                 `json:"memory_ns"`
	SystemPromptFile string                 `json:"system_prompt_file,omitempty"`
	ToolAllowlist    []string               `json:"tool_allowlist,omitempty"`
	MaxRetries       int                    `json:"max_retries,omitempty"`
	RetryBackoff     int                    `json:"retry_backoff,omitempty"`
	TimeoutSec       int                    `json:"timeout_sec,omitempty"`
	MaxTaskChars     int                    `json:"max_task_chars,omitempty"`
	MaxResultChars   int                    `json:"max_result_chars,omitempty"`
	RetryCount       int                    `json:"retry_count,omitempty"`
	ThreadID         string                 `json:"thread_id,omitempty"`
	CorrelationID    string                 `json:"correlation_id,omitempty"`
	ParentRunID      string                 `json:"parent_run_id,omitempty"`
	LastMessageID    string                 `json:"last_message_id,omitempty"`
	WaitingReply     bool                   `json:"waiting_for_reply,omitempty"`
	SharedState      map[string]interface{} `json:"shared_state,omitempty"`
	OriginChannel    string                 `json:"origin_channel,omitempty"`
	OriginChatID     string                 `json:"origin_chat_id,omitempty"`
	Status           string                 `json:"status"`
	Result           string                 `json:"result,omitempty"`
	Steering         []string               `json:"steering,omitempty"`
	Created          int64                  `json:"created"`
	Updated          int64                  `json:"updated"`
}

func unwrapSubagentTask(in *resultWrapperSubagentTask) *tools.SubagentTask {
	if in == nil {
		return nil
	}
	return &tools.SubagentTask{
		ID:               in.ID,
		Task:             in.Task,
		Label:            in.Label,
		Role:             in.Role,
		AgentID:          in.AgentID,
		Transport:        in.Transport,
		NodeID:           in.NodeID,
		ParentAgentID:    in.ParentAgentID,
		NotifyMainPolicy: in.NotifyMainPolicy,
		SessionKey:       in.SessionKey,
		MemoryNS:         in.MemoryNS,
		SystemPromptFile: in.SystemPromptFile,
		ToolAllowlist:    append([]string(nil), in.ToolAllowlist...),
		MaxRetries:       in.MaxRetries,
		RetryBackoff:     in.RetryBackoff,
		TimeoutSec:       in.TimeoutSec,
		MaxTaskChars:     in.MaxTaskChars,
		MaxResultChars:   in.MaxResultChars,
		RetryCount:       in.RetryCount,
		ThreadID:         in.ThreadID,
		CorrelationID:    in.CorrelationID,
		ParentRunID:      in.ParentRunID,
		LastMessageID:    in.LastMessageID,
		WaitingReply:     in.WaitingReply,
		SharedState:      in.SharedState,
		OriginChannel:    in.OriginChannel,
		OriginChatID:     in.OriginChatID,
		Status:           in.Status,
		Result:           in.Result,
		Steering:         append([]string(nil), in.Steering...),
		Created:          in.Created,
		Updated:          in.Updated,
	}
}

func unwrapSubagentTasks(in []*resultWrapperSubagentTask) []*tools.SubagentTask {
	out := make([]*tools.SubagentTask, 0, len(in))
	for _, item := range in {
		if task := unwrapSubagentTask(item); task != nil {
			out = append(out, task)
		}
	}
	return out
}

type resultWrapperRuntimeError struct {
	Code      string `json:"code,omitempty"`
	Message   string `json:"message,omitempty"`
	Stage     string `json:"stage,omitempty"`
	Retryable bool   `json:"retryable,omitempty"`
	Source    string `json:"source,omitempty"`
}

type resultWrapperRunRecord struct {
	ID            string                     `json:"id"`
	TaskID        string                     `json:"task_id,omitempty"`
	ThreadID      string                     `json:"thread_id,omitempty"`
	CorrelationID string                     `json:"correlation_id,omitempty"`
	AgentID       string                     `json:"agent_id,omitempty"`
	ParentRunID   string                     `json:"parent_run_id,omitempty"`
	Kind          string                     `json:"kind,omitempty"`
	Status        string                     `json:"status"`
	Input         string                     `json:"input,omitempty"`
	Output        string                     `json:"output,omitempty"`
	Error         *resultWrapperRuntimeError `json:"error,omitempty"`
	CreatedAt     int64                      `json:"created_at"`
	UpdatedAt     int64                      `json:"updated_at"`
}

type resultWrapperRouterReply struct {
	TaskID        string                     `json:"task_id"`
	ThreadID      string                     `json:"thread_id,omitempty"`
	CorrelationID string                     `json:"correlation_id,omitempty"`
	AgentID       string                     `json:"agent_id,omitempty"`
	Status        string                     `json:"status"`
	Result        string                     `json:"result,omitempty"`
	Run           resultWrapperRunRecord     `json:"run"`
	Error         *resultWrapperRuntimeError `json:"error,omitempty"`
}

func unwrapRuntimeError(in *resultWrapperRuntimeError) *tools.RuntimeError {
	if in == nil {
		return nil
	}
	return &tools.RuntimeError{
		Code:      in.Code,
		Message:   in.Message,
		Stage:     in.Stage,
		Retryable: in.Retryable,
		Source:    in.Source,
	}
}

func unwrapRunRecord(in resultWrapperRunRecord) tools.RunRecord {
	return tools.RunRecord{
		ID:            in.ID,
		TaskID:        in.TaskID,
		ThreadID:      in.ThreadID,
		CorrelationID: in.CorrelationID,
		AgentID:       in.AgentID,
		ParentRunID:   in.ParentRunID,
		Kind:          in.Kind,
		Status:        in.Status,
		Input:         in.Input,
		Output:        in.Output,
		Error:         unwrapRuntimeError(in.Error),
		CreatedAt:     in.CreatedAt,
		UpdatedAt:     in.UpdatedAt,
	}
}

func unwrapRouterReply(in *resultWrapperRouterReply) *tools.RouterReply {
	if in == nil {
		return nil
	}
	return &tools.RouterReply{
		TaskID:        in.TaskID,
		ThreadID:      in.ThreadID,
		CorrelationID: in.CorrelationID,
		AgentID:       in.AgentID,
		Status:        in.Status,
		Result:        in.Result,
		Run:           unwrapRunRecord(in.Run),
		Error:         unwrapRuntimeError(in.Error),
	}
}

type resultNodeRequest struct {
	Node   string
	Action string
	Task   string
	Model  string
	Args   map[string]interface{}
}

func (r resultNodeRequest) unwrap() nodes.Request {
	return nodes.Request{
		Node:   r.Node,
		Action: r.Action,
		Task:   r.Task,
		Model:  r.Model,
		Args:   r.Args,
	}
}

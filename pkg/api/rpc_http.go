package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	rpcpkg "github.com/YspCoder/clawgo/pkg/rpc"
	"github.com/YspCoder/clawgo/pkg/tools"
)

func (s *Server) handleSubagentRPC(w http.ResponseWriter, r *http.Request) {
	s.handleRPC(w, r, s.subagentRPCRegistry())
}

func (s *Server) handleNodeRPC(w http.ResponseWriter, r *http.Request) {
	s.handleRPC(w, r, s.nodeRPCRegistry())
}

func (s *Server) handleProviderRPC(w http.ResponseWriter, r *http.Request) {
	s.handleRPC(w, r, s.providerRPCRegistry())
}

func (s *Server) handleWorkspaceRPC(w http.ResponseWriter, r *http.Request) {
	s.handleRPC(w, r, s.workspaceRPCRegistry())
}

func (s *Server) handleConfigRPC(w http.ResponseWriter, r *http.Request) {
	s.handleRPC(w, r, s.configRPCRegistry())
}

func (s *Server) handleCronRPC(w http.ResponseWriter, r *http.Request) {
	s.handleRPC(w, r, s.cronRPCRegistry())
}

func (s *Server) handleRPC(w http.ResponseWriter, r *http.Request, registry *rpcpkg.Registry) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req rpcpkg.Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeRPCError(w, http.StatusBadRequest, req.RequestID, rpcError("invalid_argument", "invalid json", nil, false))
		return
	}
	result, rpcErr := registry.Handle(r.Context(), req)
	if rpcErr != nil {
		writeRPCError(w, rpcHTTPStatus(rpcErr), req.RequestID, rpcErr)
		return
	}
	writeJSON(w, rpcpkg.Response{
		OK:        true,
		Result:    result,
		RequestID: strings.TrimSpace(req.RequestID),
	})
}

func (s *Server) buildSubagentRegistry() *rpcpkg.Registry {
	svc := s.subagentRPCService()
	reg := rpcpkg.NewRegistry()
	rpcpkg.RegisterJSON(reg, "subagent.list", func(ctx context.Context, req rpcpkg.ListSubagentsRequest) (interface{}, *rpcpkg.Error) {
		return svc.List(ctx, req)
	})
	rpcpkg.RegisterJSON(reg, "subagent.snapshot", func(ctx context.Context, req rpcpkg.SnapshotRequest) (interface{}, *rpcpkg.Error) {
		return svc.Snapshot(ctx, req)
	})
	rpcpkg.RegisterJSON(reg, "subagent.get", func(ctx context.Context, req rpcpkg.GetSubagentRequest) (interface{}, *rpcpkg.Error) {
		return svc.Get(ctx, req)
	})
	rpcpkg.RegisterJSON(reg, "subagent.spawn", func(ctx context.Context, req rpcpkg.SpawnSubagentRequest) (interface{}, *rpcpkg.Error) {
		return svc.Spawn(ctx, req)
	})
	rpcpkg.RegisterJSON(reg, "subagent.dispatch_and_wait", func(ctx context.Context, req rpcpkg.DispatchAndWaitRequest) (interface{}, *rpcpkg.Error) {
		return svc.DispatchAndWait(ctx, req)
	})
	rpcpkg.RegisterJSON(reg, "subagent.registry", func(ctx context.Context, req rpcpkg.RegistryRequest) (interface{}, *rpcpkg.Error) {
		return svc.Registry(ctx, req)
	})
	return reg
}

func (s *Server) subagentRPCRegistry() *rpcpkg.Registry {
	if s == nil {
		return rpcpkg.NewRegistry()
	}
	s.subagentRPCOnce.Do(func() {
		s.subagentRPCReg = s.buildSubagentRegistry()
	})
	if s.subagentRPCReg == nil {
		return rpcpkg.NewRegistry()
	}
	return s.subagentRPCReg
}

func (s *Server) buildNodeRegistry() *rpcpkg.Registry {
	svc := s.nodeRPCService()
	reg := rpcpkg.NewRegistry()
	rpcpkg.RegisterJSON(reg, "node.register", func(ctx context.Context, req rpcpkg.RegisterNodeRequest) (interface{}, *rpcpkg.Error) {
		return svc.Register(ctx, req)
	})
	rpcpkg.RegisterJSON(reg, "node.heartbeat", func(ctx context.Context, req rpcpkg.HeartbeatNodeRequest) (interface{}, *rpcpkg.Error) {
		return svc.Heartbeat(ctx, req)
	})
	rpcpkg.RegisterJSON(reg, "node.dispatch", func(ctx context.Context, req rpcpkg.DispatchNodeRequest) (interface{}, *rpcpkg.Error) {
		return svc.Dispatch(ctx, req)
	})
	rpcpkg.RegisterJSON(reg, "node.artifact.list", func(ctx context.Context, req rpcpkg.ListNodeArtifactsRequest) (interface{}, *rpcpkg.Error) {
		return svc.ListArtifacts(ctx, req)
	})
	rpcpkg.RegisterJSON(reg, "node.artifact.get", func(ctx context.Context, req rpcpkg.GetNodeArtifactRequest) (interface{}, *rpcpkg.Error) {
		return svc.GetArtifact(ctx, req)
	})
	rpcpkg.RegisterJSON(reg, "node.artifact.delete", func(ctx context.Context, req rpcpkg.DeleteNodeArtifactRequest) (interface{}, *rpcpkg.Error) {
		return svc.DeleteArtifact(ctx, req)
	})
	rpcpkg.RegisterJSON(reg, "node.artifact.prune", func(ctx context.Context, req rpcpkg.PruneNodeArtifactsRequest) (interface{}, *rpcpkg.Error) {
		return svc.PruneArtifacts(ctx, req)
	})
	return reg
}

func (s *Server) nodeRPCRegistry() *rpcpkg.Registry {
	if s == nil {
		return rpcpkg.NewRegistry()
	}
	s.nodeRPCOnce.Do(func() {
		s.nodeRPCReg = s.buildNodeRegistry()
	})
	if s.nodeRPCReg == nil {
		return rpcpkg.NewRegistry()
	}
	return s.nodeRPCReg
}

func (s *Server) buildProviderRegistry() *rpcpkg.Registry {
	svc := s.providerRPCService()
	reg := rpcpkg.NewRegistry()
	rpcpkg.RegisterJSON(reg, "provider.list_models", func(ctx context.Context, req rpcpkg.ListProviderModelsRequest) (interface{}, *rpcpkg.Error) {
		return svc.ListModels(ctx, req)
	})
	rpcpkg.RegisterJSON(reg, "provider.models.update", func(ctx context.Context, req rpcpkg.UpdateProviderModelsRequest) (interface{}, *rpcpkg.Error) {
		return svc.UpdateModels(ctx, req)
	})
	rpcpkg.RegisterJSON(reg, "provider.chat", func(ctx context.Context, req rpcpkg.ProviderChatRequest) (interface{}, *rpcpkg.Error) {
		return svc.Chat(ctx, req)
	})
	rpcpkg.RegisterJSON(reg, "provider.count_tokens", func(ctx context.Context, req rpcpkg.ProviderCountTokensRequest) (interface{}, *rpcpkg.Error) {
		return svc.CountTokens(ctx, req)
	})
	rpcpkg.RegisterJSON(reg, "provider.runtime.view", func(ctx context.Context, req rpcpkg.ProviderRuntimeViewRequest) (interface{}, *rpcpkg.Error) {
		return svc.RuntimeView(ctx, req)
	})
	rpcpkg.RegisterJSON(reg, "provider.runtime.action", func(ctx context.Context, req rpcpkg.ProviderRuntimeActionRequest) (interface{}, *rpcpkg.Error) {
		return svc.RuntimeAction(ctx, req)
	})
	return reg
}

func (s *Server) providerRPCRegistry() *rpcpkg.Registry {
	if s == nil {
		return rpcpkg.NewRegistry()
	}
	s.providerRPCOnce.Do(func() {
		s.providerRPCReg = s.buildProviderRegistry()
	})
	if s.providerRPCReg == nil {
		return rpcpkg.NewRegistry()
	}
	return s.providerRPCReg
}

func (s *Server) buildWorkspaceRegistry() *rpcpkg.Registry {
	svc := s.workspaceRPCService()
	reg := rpcpkg.NewRegistry()
	rpcpkg.RegisterJSON(reg, "workspace.list_files", func(ctx context.Context, req rpcpkg.ListWorkspaceFilesRequest) (interface{}, *rpcpkg.Error) {
		return svc.ListFiles(ctx, req)
	})
	rpcpkg.RegisterJSON(reg, "workspace.read_file", func(ctx context.Context, req rpcpkg.ReadWorkspaceFileRequest) (interface{}, *rpcpkg.Error) {
		return svc.ReadFile(ctx, req)
	})
	rpcpkg.RegisterJSON(reg, "workspace.write_file", func(ctx context.Context, req rpcpkg.WriteWorkspaceFileRequest) (interface{}, *rpcpkg.Error) {
		return svc.WriteFile(ctx, req)
	})
	rpcpkg.RegisterJSON(reg, "workspace.delete_file", func(ctx context.Context, req rpcpkg.DeleteWorkspaceFileRequest) (interface{}, *rpcpkg.Error) {
		return svc.DeleteFile(ctx, req)
	})
	return reg
}

func (s *Server) workspaceRPCRegistry() *rpcpkg.Registry {
	if s == nil {
		return rpcpkg.NewRegistry()
	}
	s.workspaceRPCOnce.Do(func() {
		s.workspaceRPCReg = s.buildWorkspaceRegistry()
	})
	if s.workspaceRPCReg == nil {
		return rpcpkg.NewRegistry()
	}
	return s.workspaceRPCReg
}

func (s *Server) buildConfigRegistry() *rpcpkg.Registry {
	svc := s.configRPCService()
	reg := rpcpkg.NewRegistry()
	rpcpkg.RegisterJSON(reg, "config.view", func(ctx context.Context, req rpcpkg.ConfigViewRequest) (interface{}, *rpcpkg.Error) {
		return svc.View(ctx, req)
	})
	rpcpkg.RegisterJSON(reg, "config.save", func(ctx context.Context, req rpcpkg.ConfigSaveRequest) (interface{}, *rpcpkg.Error) {
		return svc.Save(ctx, req)
	})
	return reg
}

func (s *Server) configRPCRegistry() *rpcpkg.Registry {
	if s == nil {
		return rpcpkg.NewRegistry()
	}
	s.configRPCOnce.Do(func() {
		s.configRPCReg = s.buildConfigRegistry()
	})
	if s.configRPCReg == nil {
		return rpcpkg.NewRegistry()
	}
	return s.configRPCReg
}

func (s *Server) buildCronRegistry() *rpcpkg.Registry {
	svc := s.cronRPCService()
	reg := rpcpkg.NewRegistry()
	rpcpkg.RegisterJSON(reg, "cron.list", func(ctx context.Context, req rpcpkg.ListCronJobsRequest) (interface{}, *rpcpkg.Error) {
		return svc.List(ctx, req)
	})
	rpcpkg.RegisterJSON(reg, "cron.get", func(ctx context.Context, req rpcpkg.GetCronJobRequest) (interface{}, *rpcpkg.Error) {
		return svc.Get(ctx, req)
	})
	rpcpkg.RegisterJSON(reg, "cron.mutate", func(ctx context.Context, req rpcpkg.MutateCronJobRequest) (interface{}, *rpcpkg.Error) {
		return svc.Mutate(ctx, req)
	})
	return reg
}

func (s *Server) cronRPCRegistry() *rpcpkg.Registry {
	if s == nil {
		return rpcpkg.NewRegistry()
	}
	s.cronRPCOnce.Do(func() {
		s.cronRPCReg = s.buildCronRegistry()
	})
	if s.cronRPCReg == nil {
		return rpcpkg.NewRegistry()
	}
	return s.cronRPCReg
}

func writeRPCError(w http.ResponseWriter, status int, requestID string, rpcErr *rpcpkg.Error) {
	if rpcErr == nil {
		rpcErr = rpcError("internal", "rpc error", nil, false)
	}
	writeJSONStatus(w, status, rpcpkg.Response{
		OK:        false,
		Error:     rpcErr,
		RequestID: strings.TrimSpace(requestID),
	})
}

func (s *Server) handleSubagentLegacyAction(ctx context.Context, action string, args map[string]interface{}) (interface{}, *rpcpkg.Error) {
	registry := s.subagentRPCRegistry()
	req := rpcpkg.Request{
		Method: legacySubagentActionMethod(action),
		Params: mustJSONMarshal(mapSubagentLegacyArgs(action, args)),
	}
	result, rpcErr := registry.Handle(ctx, req)
	if rpcErr != nil && !strings.HasPrefix(strings.TrimSpace(req.Method), "subagent.") {
		if s.onSubagents == nil {
			return nil, rpcError("unavailable", "subagent runtime handler not configured", nil, false)
		}
		fallback, err := s.onSubagents(ctx, action, args)
		if err != nil {
			return nil, rpcErrorFrom(err)
		}
		return fallback, nil
	}
	return result, rpcErr
}

var legacySubagentActionMethods = map[string]string{
	"":                  "subagent.list",
	"list":              "subagent.list",
	"snapshot":          "subagent.snapshot",
	"get":               "subagent.get",
	"info":              "subagent.get",
	"spawn":             "subagent.spawn",
	"create":            "subagent.spawn",
	"dispatch_and_wait": "subagent.dispatch_and_wait",
	"registry":          "subagent.registry",
}

func legacySubagentActionMethod(action string) string {
	normalized := strings.ToLower(strings.TrimSpace(action))
	if method, ok := legacySubagentActionMethods[normalized]; ok {
		return method
	}
	return strings.TrimSpace(action)
}

var legacySubagentArgMappers = map[string]func(map[string]interface{}) interface{}{
	"snapshot": func(args map[string]interface{}) interface{} {
		return rpcpkg.SnapshotRequest{Limit: tools.MapIntArg(args, "limit", 0)}
	},
	"get": func(args map[string]interface{}) interface{} {
		return rpcpkg.GetSubagentRequest{ID: tools.MapStringArg(args, "id")}
	},
	"info": func(args map[string]interface{}) interface{} {
		return rpcpkg.GetSubagentRequest{ID: tools.MapStringArg(args, "id")}
	},
	"spawn": buildLegacySpawnSubagentRequest,
	"create": func(args map[string]interface{}) interface{} {
		return buildLegacySpawnSubagentRequest(args)
	},
	"dispatch_and_wait": func(args map[string]interface{}) interface{} {
		return rpcpkg.DispatchAndWaitRequest{
			Task:           tools.MapStringArg(args, "task"),
			Label:          tools.MapStringArg(args, "label"),
			Role:           tools.MapStringArg(args, "role"),
			AgentID:        tools.MapStringArg(args, "agent_id"),
			ThreadID:       tools.MapStringArg(args, "thread_id"),
			CorrelationID:  tools.MapStringArg(args, "correlation_id"),
			ParentRunID:    tools.MapStringArg(args, "parent_run_id"),
			MaxRetries:     tools.MapIntArg(args, "max_retries", 0),
			RetryBackoffMS: tools.MapIntArg(args, "retry_backoff_ms", 0),
			TimeoutSec:     tools.MapIntArg(args, "timeout_sec", 0),
			MaxTaskChars:   tools.MapIntArg(args, "max_task_chars", 0),
			MaxResultChars: tools.MapIntArg(args, "max_result_chars", 0),
			WaitTimeoutSec: tools.MapIntArg(args, "wait_timeout_sec", 0),
			Channel:        firstNonEmptyString(tools.MapStringArg(args, "channel"), tools.MapStringArg(args, "origin_channel")),
			ChatID:         firstNonEmptyString(tools.MapStringArg(args, "chat_id"), tools.MapStringArg(args, "origin_chat_id")),
		}
	},
}

func mapSubagentLegacyArgs(action string, args map[string]interface{}) interface{} {
	normalized := strings.ToLower(strings.TrimSpace(action))
	if mapper, ok := legacySubagentArgMappers[normalized]; ok && mapper != nil {
		return mapper(args)
	}
	return args
}

func buildLegacySpawnSubagentRequest(args map[string]interface{}) interface{} {
	return rpcpkg.SpawnSubagentRequest{
		Task:           tools.MapStringArg(args, "task"),
		Label:          tools.MapStringArg(args, "label"),
		Role:           tools.MapStringArg(args, "role"),
		AgentID:        tools.MapStringArg(args, "agent_id"),
		MaxRetries:     tools.MapIntArg(args, "max_retries", 0),
		RetryBackoffMS: tools.MapIntArg(args, "retry_backoff_ms", 0),
		TimeoutSec:     tools.MapIntArg(args, "timeout_sec", 0),
		MaxTaskChars:   tools.MapIntArg(args, "max_task_chars", 0),
		MaxResultChars: tools.MapIntArg(args, "max_result_chars", 0),
		Channel:        firstNonEmptyString(tools.MapStringArg(args, "channel"), tools.MapStringArg(args, "origin_channel")),
		ChatID:         firstNonEmptyString(tools.MapStringArg(args, "chat_id"), tools.MapStringArg(args, "origin_chat_id")),
	}
}

func mustJSONMarshal(value interface{}) json.RawMessage {
	if value == nil {
		return json.RawMessage([]byte("{}"))
	}
	data, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage([]byte("{}"))
	}
	return data
}

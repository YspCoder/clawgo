package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	rpcpkg "github.com/YspCoder/clawgo/pkg/rpc"
)

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

func (s *Server) handleSkillsRPC(w http.ResponseWriter, r *http.Request) {
	s.handleRPC(w, r, s.skillsRPCRegistry())
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

func (s *Server) buildSkillsRegistry() *rpcpkg.Registry {
	svc := s.skillsRPCService()
	reg := rpcpkg.NewRegistry()
	rpcpkg.RegisterJSON(reg, "skills.view", func(ctx context.Context, req rpcpkg.SkillsViewRequest) (interface{}, *rpcpkg.Error) {
		return svc.View(ctx, req)
	})
	rpcpkg.RegisterJSON(reg, "skills.mutate", func(ctx context.Context, req rpcpkg.SkillsMutateRequest) (interface{}, *rpcpkg.Error) {
		return svc.Mutate(ctx, req)
	})
	return reg
}

func (s *Server) skillsRPCRegistry() *rpcpkg.Registry {
	if s == nil {
		return rpcpkg.NewRegistry()
	}
	s.skillsRPCOnce.Do(func() {
		s.skillsRPCReg = s.buildSkillsRegistry()
	})
	if s.skillsRPCReg == nil {
		return rpcpkg.NewRegistry()
	}
	return s.skillsRPCReg
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

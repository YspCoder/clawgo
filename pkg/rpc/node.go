package rpc

import (
	"context"

	"github.com/YspCoder/clawgo/pkg/nodes"
)

type NodeService interface {
	Register(context.Context, RegisterNodeRequest) (*RegisterNodeResponse, *Error)
	Heartbeat(context.Context, HeartbeatNodeRequest) (*HeartbeatNodeResponse, *Error)
	Dispatch(context.Context, DispatchNodeRequest) (*DispatchNodeResponse, *Error)
	ListArtifacts(context.Context, ListNodeArtifactsRequest) (*ListNodeArtifactsResponse, *Error)
	GetArtifact(context.Context, GetNodeArtifactRequest) (*GetNodeArtifactResponse, *Error)
	DeleteArtifact(context.Context, DeleteNodeArtifactRequest) (*DeleteNodeArtifactResponse, *Error)
	PruneArtifacts(context.Context, PruneNodeArtifactsRequest) (*PruneNodeArtifactsResponse, *Error)
}

type RegisterNodeRequest struct {
	Node nodes.NodeInfo `json:"node"`
}

type RegisterNodeResponse struct {
	ID string `json:"id"`
}

type HeartbeatNodeRequest struct {
	ID string `json:"id"`
}

type HeartbeatNodeResponse struct {
	ID string `json:"id"`
}

type DispatchNodeRequest struct {
	Node   string                 `json:"node"`
	Action string                 `json:"action"`
	Mode   string                 `json:"mode,omitempty"`
	Task   string                 `json:"task,omitempty"`
	Model  string                 `json:"model,omitempty"`
	Args   map[string]interface{} `json:"args,omitempty"`
}

type DispatchNodeResponse struct {
	Result nodes.Response `json:"result"`
}

type ArtifactSummary struct {
	Data map[string]interface{} `json:"data"`
}

type ArtifactContentRef struct {
	Data map[string]interface{} `json:"data"`
}

type ArtifactDeleteResult struct {
	ID           string `json:"id"`
	DeletedFile  bool   `json:"deleted_file"`
	DeletedAudit bool   `json:"deleted_audit"`
}

type ArtifactPruneResult struct {
	Pruned       int `json:"pruned"`
	DeletedFiles int `json:"deleted_files"`
	Kept         int `json:"kept"`
}

type ListNodeArtifactsRequest struct {
	Node   string `json:"node,omitempty"`
	Action string `json:"action,omitempty"`
	Kind   string `json:"kind,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

type ListNodeArtifactsResponse struct {
	Items             []map[string]interface{} `json:"items"`
	ArtifactRetention map[string]interface{}   `json:"artifact_retention,omitempty"`
}

type GetNodeArtifactRequest struct {
	ID string `json:"id"`
}

type GetNodeArtifactResponse struct {
	Found    bool                   `json:"found"`
	Artifact map[string]interface{} `json:"artifact,omitempty"`
}

type DeleteNodeArtifactRequest struct {
	ID string `json:"id"`
}

type DeleteNodeArtifactResponse struct {
	ArtifactDeleteResult
}

type PruneNodeArtifactsRequest struct {
	Node       string `json:"node,omitempty"`
	Action     string `json:"action,omitempty"`
	Kind       string `json:"kind,omitempty"`
	KeepLatest int    `json:"keep_latest,omitempty"`
	Limit      int    `json:"limit,omitempty"`
}

type PruneNodeArtifactsResponse struct {
	ArtifactPruneResult
}

type ProviderService interface {
	ListModels(context.Context, ListProviderModelsRequest) (*ListProviderModelsResponse, *Error)
	UpdateModels(context.Context, UpdateProviderModelsRequest) (*UpdateProviderModelsResponse, *Error)
	Chat(context.Context, ProviderChatRequest) (*ProviderChatResponse, *Error)
	CountTokens(context.Context, ProviderCountTokensRequest) (*ProviderCountTokensResponse, *Error)
	RuntimeView(context.Context, ProviderRuntimeViewRequest) (*ProviderRuntimeViewResponse, *Error)
	RuntimeAction(context.Context, ProviderRuntimeActionRequest) (*ProviderRuntimeActionResponse, *Error)
}

type ListProviderModelsRequest struct {
	Provider string `json:"provider"`
}

type ListProviderModelsResponse struct {
	Provider string   `json:"provider"`
	Models   []string `json:"models,omitempty"`
	Default  string   `json:"default_model,omitempty"`
}

type UpdateProviderModelsRequest struct {
	Provider string   `json:"provider"`
	Model    string   `json:"model,omitempty"`
	Models   []string `json:"models,omitempty"`
}

type UpdateProviderModelsResponse struct {
	Provider string   `json:"provider"`
	Models   []string `json:"models,omitempty"`
}

type ProviderChatRequest struct {
	Provider string                   `json:"provider"`
	Model    string                   `json:"model,omitempty"`
	Messages []map[string]interface{} `json:"messages"`
	Tools    []map[string]interface{} `json:"tools,omitempty"`
	Options  map[string]interface{}   `json:"options,omitempty"`
}

type ProviderChatResponse struct {
	Content      string                   `json:"content,omitempty"`
	ToolCalls    []map[string]interface{} `json:"tool_calls,omitempty"`
	FinishReason string                   `json:"finish_reason,omitempty"`
	Usage        map[string]interface{}   `json:"usage,omitempty"`
}

type ProviderCountTokensRequest struct {
	Provider string                   `json:"provider"`
	Model    string                   `json:"model,omitempty"`
	Messages []map[string]interface{} `json:"messages"`
	Tools    []map[string]interface{} `json:"tools,omitempty"`
	Options  map[string]interface{}   `json:"options,omitempty"`
}

type ProviderCountTokensResponse struct {
	Usage map[string]interface{} `json:"usage,omitempty"`
}

type ProviderRuntimeViewRequest struct {
	Provider               string `json:"provider,omitempty"`
	Kind                   string `json:"kind,omitempty"`
	Reason                 string `json:"reason,omitempty"`
	Target                 string `json:"target,omitempty"`
	Sort                   string `json:"sort,omitempty"`
	ChangesOnly            bool   `json:"changes_only,omitempty"`
	WindowSec              int    `json:"window_sec,omitempty"`
	Limit                  int    `json:"limit,omitempty"`
	Cursor                 int    `json:"cursor,omitempty"`
	HealthBelow            int    `json:"health_below,omitempty"`
	CooldownUntilBeforeSec int    `json:"cooldown_until_before_sec,omitempty"`
}

type ProviderRuntimeViewResponse struct {
	View map[string]interface{} `json:"view"`
}

type ProviderRuntimeActionRequest struct {
	Provider     string `json:"provider,omitempty"`
	Action       string `json:"action"`
	OnlyExpiring bool   `json:"only_expiring,omitempty"`
}

type ProviderRuntimeActionResponse struct {
	Result map[string]interface{} `json:"result"`
}

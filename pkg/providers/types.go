package providers

import "context"

type ToolCall struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type,omitempty"`
	Function  *FunctionCall          `json:"function,omitempty"`
	Name      string                 `json:"name,omitempty"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
}

type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type LLMResponse struct {
	Content      string     `json:"content"`
	ToolCalls    []ToolCall `json:"tool_calls,omitempty"`
	FinishReason string     `json:"finish_reason"`
	Usage        *UsageInfo `json:"usage,omitempty"`
}

type UsageInfo struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type Message struct {
	Role         string               `json:"role"`
	Content      string               `json:"content"`
	ContentParts []MessageContentPart `json:"content_parts,omitempty"`
	ToolCalls    []ToolCall           `json:"tool_calls,omitempty"`
	ToolCallID   string               `json:"tool_call_id,omitempty"`
}

type MessageContentPart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
	Detail   string `json:"detail,omitempty"`
	MIMEType string `json:"mime_type,omitempty"`
	Filename string `json:"filename,omitempty"`
	FileData string `json:"file_data,omitempty"`
	FileID   string `json:"file_id,omitempty"`
	FileURL  string `json:"file_url,omitempty"`
}

type LLMProvider interface {
	Chat(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}) (*LLMResponse, error)
	GetDefaultModel() string
}

// StreamingLLMProvider is an optional capability interface for token-level streaming.
type StreamingLLMProvider interface {
	ChatStream(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}, onDelta func(string)) (*LLMResponse, error)
}

// ResponsesCompactor is an optional capability interface.
// Providers that support OpenAI /v1/responses/compact can implement this.
type ResponsesCompactor interface {
	SupportsResponsesCompact() bool
	BuildSummaryViaResponsesCompact(ctx context.Context, model string, existingSummary string, messages []Message, maxSummaryChars int) (string, error)
}

// TokenCounter is an optional capability for providers that expose a native
// token counting endpoint.
type TokenCounter interface {
	CountTokens(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}) (*UsageInfo, error)
}

// ExecutionSessionCloser is an optional capability for providers that keep
// reusable upstream execution sessions, such as websocket-backed Codex sessions.
type ExecutionSessionCloser interface {
	CloseExecutionSession(sessionID string)
}

type ToolDefinition struct {
	Type        string                 `json:"type"`
	Name        string                 `json:"name,omitempty"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
	Strict      *bool                  `json:"strict,omitempty"`
	Function    ToolFunctionDefinition `json:"function"`
}

type ToolFunctionDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
	Strict      *bool                  `json:"strict,omitempty"`
}

type ProviderExecutionError struct {
	Code      string `json:"code,omitempty"`
	Message   string `json:"message,omitempty"`
	Stage     string `json:"stage,omitempty"`
	Retryable bool   `json:"retryable,omitempty"`
	Source    string `json:"source,omitempty"`
}

type ProviderExecutionResult struct {
	Body        []byte                  `json:"-"`
	StatusCode  int                     `json:"status_code,omitempty"`
	ContentType string                  `json:"content_type,omitempty"`
	AttemptKind string                  `json:"attempt_kind,omitempty"`
	Retryable   bool                    `json:"retryable,omitempty"`
	Failure     oauthFailureReason      `json:"failure_reason,omitempty"`
	Error       *ProviderExecutionError `json:"error,omitempty"`
}

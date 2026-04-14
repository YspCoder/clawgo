package agent

import "github.com/YspCoder/clawgo/pkg/providers"

// ContextBuildRequest defines inputs for building a provider message window.
type ContextBuildRequest struct {
	History          []providers.Message
	Summary          string
	CurrentMessage   string
	Media            []string
	Channel          string
	ChatID           string
	ResponseLanguage string
	MemoryNamespace  string
}

// ContextEngine allows swapping context-assembly behavior without touching AgentLoop flow.
type ContextEngine interface {
	BuildMessages(req ContextBuildRequest) []providers.Message
	SkillsInfo() map[string]interface{}
}

type defaultContextEngine struct {
	builder *ContextBuilder
}

func NewDefaultContextEngine(builder *ContextBuilder) ContextEngine {
	if builder == nil {
		return nil
	}
	return &defaultContextEngine{builder: builder}
}

func (e *defaultContextEngine) BuildMessages(req ContextBuildRequest) []providers.Message {
	if e == nil || e.builder == nil {
		return nil
	}
	return e.builder.BuildMessagesWithMemoryNamespace(
		req.History,
		req.Summary,
		req.CurrentMessage,
		req.Media,
		req.Channel,
		req.ChatID,
		req.ResponseLanguage,
		req.MemoryNamespace,
	)
}

func (e *defaultContextEngine) SkillsInfo() map[string]interface{} {
	if e == nil || e.builder == nil {
		return map[string]interface{}{}
	}
	return e.builder.GetSkillsInfo()
}

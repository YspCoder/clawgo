package tools

import (
	"github.com/YspCoder/clawgo/pkg/bus"
	"github.com/YspCoder/clawgo/pkg/config"
	"github.com/YspCoder/clawgo/pkg/cron"
	"github.com/YspCoder/clawgo/pkg/providers"
	"github.com/YspCoder/clawgo/pkg/session"
)

type SessionListFunc func(limit int) []SessionInfo

type SessionHistoryFunc func(key string, limit int) []providers.Message

type BootstrapOptions struct {
	Config         *config.Config
	Workspace      string
	MessageBus     *bus.MessageBus
	CronService    *cron.CronService
	Provider       providers.LLMProvider
	ProcessManager *ProcessManager
	SessionManager *session.SessionManager
	SessionList    SessionListFunc
	SessionHistory SessionHistoryFunc
}

type BootstrapResult struct {
	Registry        *ToolRegistry
	ProcessManager  *ProcessManager
	SubagentManager *SubagentManager
	SubagentRouter  *SubagentRouter
}

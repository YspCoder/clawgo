# 04 工具注册架构优化

## 背景

`agent.NewAgentLoop()` 直接注册大量工具，包括文件、shell、web、MCP、message、spawn、sessions、memory、parallel、browser、camera、system_info 等。工具注册逻辑和 AgentLoop 初始化耦合较深，也让子 Agent 可见性、默认工具集、可选工具集的边界不够清晰。

## 目标

抽出工具启动/注册逻辑，让 AgentLoop 只负责调用工具构建器，而不是直接知道每个工具的创建细节。

## 建议改动范围

主要文件：

- `pkg/agent/loop.go`
- `pkg/tools/registry.go`
- 新增 `pkg/tools/bootstrap.go`
- 新增 `pkg/tools/bootstrap_options.go`

可选文件：

- `pkg/tools/tool_allowlist_groups.go`
- `pkg/tools/subagent*.go`
- `pkg/tools/mcp.go`

尽量避免修改：

- `pkg/providers/*`
- `pkg/api/*`
- `cmd/*`

## 建议设计

新增一个工具构建入口，例如：

```go
type BootstrapOptions struct {
    Config          *config.Config
    Workspace       string
    MessageBus      *bus.MessageBus
    CronService     *cron.CronService
    Provider        providers.LLMProvider
    ProcessManager  *ProcessManager
}

type BootstrapResult struct {
    Registry        *ToolRegistry
    ProcessManager  *ProcessManager
    SubagentManager *SubagentManager
    SubagentRouter  *SubagentRouter
}

func BootstrapDefaultTools(ctx context.Context, opts BootstrapOptions) (*BootstrapResult, error)
```

AgentLoop 中保留：

- session manager 初始化。
- context builder 初始化。
- provider fallback chain。
- subagent run func 注入。

工具构建器负责：

- 创建并注册基础工具。
- 根据 config 注册 cron/remind。
- 根据 config 注册 MCP 和远端工具。
- 创建 subagent manager/router。
- 注册 tool catalog 需要的 metadata。

## 子 Agent 可见性

保持现有行为：

- 主 Agent 默认看见全部注册工具。
- 子 Agent 由 profile allowlist 限制。
- `skill_exec` 仍为隐式允许工具。
- `parallel` 内部 call 仍需要逐项校验 allowlist。

## 验收标准

- `agent.NewAgentLoop()` 更短，工具注册细节迁移出去。
- 工具列表和原来保持一致。
- MCP discovery 行为保持一致。
- subagent spawn/profile/sessions 工具仍正常注册。
- `go test ./pkg/agent ./pkg/tools` 通过。
- `go test ./...` 通过。

## 测试建议

```bash
go test ./pkg/tools -count=1
go test ./pkg/agent -count=1
go test ./...
```

建议新增一个测试断言默认工具集名称，避免重构时漏注册工具。

## 并行注意

该任务会修改 `pkg/agent/loop.go`，应避免和 AgentLoop 行为改动并行落同一分支。它不应修改 Provider 或 Gateway。

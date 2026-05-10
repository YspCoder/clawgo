# ClawGo 项目说明

本文档用于快速熟悉当前代码库：它是什么、如何运行、核心模块如何协作，以及开发时应该从哪里下手。

## 1. 项目定位

ClawGo 是一个 Go 原生的个人 AI Agent Runtime。它不只是命令行聊天壳，而是把 Agent 对话、工具调用、消息通道、子 Agent、记忆、定时任务、网关 Web API、健康巡检和服务化部署组织到同一个运行时里。

从代码结构看，项目当前主要服务两类使用方式：

- `clawgo agent`：命令行直连 Agent，可交互，也可通过 `-m` 传入单次消息。
- `clawgo gateway run`：启动常驻网关，接入 WebUI/API、Telegram、微信、飞书、定时任务、心跳和 Sentinel 巡检。

## 2. 目录结构

```text
.
├── cmd/                    # CLI 入口和各子命令
├── pkg/
│   ├── agent/              # Agent 主循环、规划、上下文、子 Agent 编排
│   ├── api/                # Gateway HTTP/WebUI API
│   ├── browser/            # Chromium 截图/内容读取辅助
│   ├── bus/                # 入站/出站消息总线
│   ├── channels/           # Telegram / Weixin / Feishu 通道
│   ├── config/             # 配置结构、默认值、校验、标准化视图
│   ├── configops/          # 配置操作辅助
│   ├── cron/               # 定时任务调度和持久化
│   ├── events/             # 类型化事件总线
│   ├── heartbeat/          # 心跳服务
│   ├── lifecycle/          # 后台循环生命周期工具
│   ├── logger/             # 结构化日志与轮转
│   ├── providers/          # LLM Provider 适配
│   ├── runtimecfg/         # 运行时全局配置快照
│   ├── scheduling/         # 资源调度键
│   ├── sentinel/           # 健康巡检与告警
│   ├── session/            # 会话历史持久化
│   ├── tools/              # Agent 工具注册与实现
│   └── wsrelay/            # WebSocket relay
├── workspace/              # 内置 workspace 模板、skills、记忆/身份文件
├── scripts/                # 构建脚本
├── config.example.json     # 完整配置示例
├── Dockerfile              # 容器化运行
├── docker-compose.yml      # 本地 compose 示例
├── Makefile                # 构建、测试、安装目标
├── README.md
└── README_EN.md
```

## 3. 启动入口

主入口在 `cmd/main.go`。程序会先解析全局参数：

- `--config <path>` 或 `CLAWGO_CONFIG`：指定配置文件。
- `--debug` / `-d`：启用 debug 日志。

随后根据第一个命令分发：

```text
onboard    初始化配置和 workspace
agent      直接与 Agent 交互
gateway    注册、管理或前台运行网关服务
status     查看状态
provider   管理模型服务商和 OAuth
config     读写配置
cron       管理定时任务
channel    测试和管理消息通道
skills     管理 skills
tui        终端聊天 UI，取决于构建开关
uninstall  卸载组件
version    输出版本
```

## 4. 核心运行链路

### 4.1 CLI 直连模式

`cmd/cmd_agent.go` 的主要流程：

1. 加载配置。
2. 根据配置创建 LLM Provider。
3. 创建 `bus.MessageBus`。
4. 创建 `cron.CronService`，用于 `remind` / `cron` 工具。
5. 创建 `agent.AgentLoop`。
6. 单次消息走 `ProcessDirect`，交互模式逐行读入后同样走 `ProcessDirect`。

简化链路：

```text
用户输入
  -> cmd/agent
  -> agent.AgentLoop.ProcessDirect
  -> 构造上下文 + 历史 + 工具定义
  -> providers.LLMProvider.Chat
  -> 执行 tool calls
  -> 保存会话
  -> 返回回复
```

### 4.2 Gateway 常驻模式

`cmd/cmd_gateway.go` 的 `gatewayCmd()` 是常驻服务的组合器：

1. 加载配置并写入 `runtimecfg`。
2. 创建消息总线。
3. 创建 Cron、Heartbeat、Sentinel。
4. 创建 AgentLoop 和 Channel Manager。
5. 启动 HTTP API Server。
6. 启动各消息通道。
7. 启动 AgentLoop 后台消费消息。
8. 监听配置文件变化和系统信号，支持热重载。

简化链路：

```text
外部通道 / Web API / Cron / Heartbeat
  -> bus.InboundMessage
  -> agent.AgentLoop.Run
  -> session shard 并发处理
  -> LLM + tools
  -> bus.OutboundMessage
  -> channels.Manager.dispatchOutbound
  -> 外部通道
```

## 5. 核心模块说明

### 5.1 `pkg/config`

配置中心，定义完整的 `Config` 结构：

- `agents`：workspace、模型默认值、上下文压缩、执行参数、router、subagents。
- `channels`：微信、Telegram、飞书和消息去重窗口。
- `models.providers`：OpenAI 兼容接口、Codex、Claude、Gemini、Qwen、Kimi、Vertex 等 Provider 配置。
- `gateway`：HTTP 监听地址、端口和 token。
- `cron`：定时任务轮询、退避和并发参数。
- `tools`：web、shell、filesystem、MCP 工具配置。
- `logging`：日志文件和轮转。
- `sentinel`：巡检与告警。
- `memory`：分层记忆开关。

`LoadConfig()` 会先加载默认配置，再严格解析 JSON，并通过环境变量覆盖部分字段。`DefaultConfig()` 是理解默认行为的最佳入口。

### 5.2 `pkg/agent`

Agent Runtime 的核心。`NewAgentLoop()` 会完成：

- 创建 workspace。
- 初始化 `session.SessionManager`。
- 注册本地工具。
- 注册 MCP 远端工具。
- 注册消息工具、spawn 工具、subagent profile 工具。
- 注册记忆、浏览器、摄像头、系统信息等工具。
- 初始化 provider fallback chain。
- 注入 subagent 的递归运行逻辑。

`Run()` 用 session key 分片，保证不同会话可以并发处理，同一会话尽量串行。`ProcessDirect()` 用于 CLI/Web API 的直接请求。

### 5.3 `pkg/tools`

工具系统以 `Tool` 接口为核心：

```go
type Tool interface {
    Name() string
    Description() string
    Parameters() map[string]interface{}
    Execute(ctx context.Context, args map[string]interface{}) (string, error)
}
```

`ToolRegistry` 负责注册、查询、执行和导出 schema。当前 AgentLoop 默认注册的能力包括：

- 文件读写、目录列表、编辑文件。
- shell 执行和进程管理。
- web search、web fetch、parallel fetch。
- `parallel` 并发工具调用。
- `message` 外发消息。
- `spawn` / `subagent_profile` / `sessions`。
- `memory_search` / `memory_get` / `memory_write`。
- `skill_exec`。
- `browser`、`camera`、`system_info`。
- MCP 动态发现出的远端工具。

子 Agent 可以通过 profile 的 allowlist 限制工具范围。`parallel` 工具会校验内部调用是否也在 allowlist 中。

### 5.4 `pkg/providers`

LLM Provider 抽象在 `pkg/providers/types.go`：

- `LLMProvider`：同步 Chat。
- `StreamingLLMProvider`：可选流式输出。
- `ResponsesCompactor`：可选 Responses Compact。
- `TokenCounter`：可选 token 统计。

`CreateProviderByName()` 会根据 provider 名称或 OAuth provider 路由到不同实现：

- `HTTPProvider`：通用 OpenAI Responses / OpenAI-compatible 适配。
- `CodexProvider`
- `ClaudeProvider`
- `GeminiProvider`
- `GeminiCLIProvider`
- `AistudioProvider`
- `AntigravityProvider`
- `QwenProvider`
- `KimiProvider`
- `IFlowProvider`
- `VertexProvider`

Provider 层还包含 OAuth 多账号轮换、失败冷却、runtime history 持久化、OpenAI-compatible chat 转换、Responses API 工具调用格式转换等逻辑。

### 5.5 `pkg/bus` 和 `pkg/channels`

`pkg/bus` 是运行时内部消息通道：

- `InboundMessage`：外部消息、系统触发、cron、heartbeat 进入 Agent。
- `OutboundMessage`：Agent 或工具发往外部通道。

`pkg/channels.Manager` 负责：

- 根据配置创建 Telegram、Weixin、Feishu 通道。
- 启停所有通道。
- 监听 outbound bus 并分发到对应通道。
- 对 outbound 做短窗口去重和限速。
- 提供健康检查。

### 5.6 `pkg/api`

Gateway HTTP API 和 WebUI 后端。主要接口包括：

```text
GET  /health
*    /api/config
*    /api/chat
*    /api/chat/history
*    /api/chat/live
*    /api/events/live
*    /api/version
*    /api/provider/oauth/start
*    /api/provider/oauth/complete
*    /api/provider/oauth/import
*    /api/provider/oauth/accounts
*    /api/provider/models
*    /api/provider/runtime
*    /api/weixin/status
*    /api/weixin/login/start
*    /api/weixin/login/cancel
GET  /api/weixin/qr.svg
*    /api/upload
*    /api/cron
*    /api/skills
*    /api/sessions
*    /api/memory
*    /api/workspace_file
*    /api/tool_allowlist_groups
*    /api/tools
*    /api/mcp/install
*    /api/logs/live
*    /api/logs/recent
```

API Server 由 gateway 注入 chat handler、history handler、cron handler、tools catalog handler 等回调，因此 `pkg/api` 不直接拥有 AgentLoop。

### 5.7 `pkg/cron`

定时任务服务。任务保存在配置目录下的 `cron/jobs.json`。支持：

- cron 表达式。
- 一次性或间隔式兼容字段。
- 失败退避。
- 最大并发 worker。
- 任务启停、更新、删除、查询。

Gateway 中的 cron job 可以：

- 投递为内部 inbound message，让 Agent 处理。
- 如果指定了 channel/to，则直接发 outbound message 到外部通道。

### 5.8 `pkg/session` 与记忆

会话历史由 `SessionManager` 持久化为 JSONL，同时维护 `sessions.json` 索引。默认主会话路径来自：

```text
<config dir>/agents/main/sessions
```

会话 key 会被映射为稳定 session id，并根据 key 前缀识别类型：

- `cron:*` -> cron
- `subagent:*` 或包含 `:subagent:` -> subagent
- `hook:*` -> hook
- 包含 `:` -> main
- 其他 -> other

记忆工具读写 workspace 下的记忆文件，并支持 main / subagent namespace 隔离。

### 5.9 `pkg/sentinel` 和 `pkg/heartbeat`

- Heartbeat 定时生成系统 inbound message，用于让 Agent 周期性自检或继续长期任务。
- Sentinel 定期检查 channels、config、memory、logs，可通过配置的通道或 webhook 告警。

## 6. 配置和运行数据位置

默认配置目录：

```text
~/.clawgo
```

debug 模式下：

```text
.clawgo
```

常见文件：

```text
~/.clawgo/config.json
~/.clawgo/workspace/
~/.clawgo/logs/clawgo.log
~/.clawgo/cron/jobs.json
~/.clawgo/agents/main/sessions/
~/.clawgo/runtime/providers/*.json
```

配置文件也可以通过 `--config` 或 `CLAWGO_CONFIG` 指定。

## 7. 本地开发

常用命令：

```bash
go test ./...
go build -o clawgo ./cmd
make build
make test
make dev
```

启动方式：

```bash
clawgo onboard
clawgo agent
clawgo agent -m "Hello"
clawgo gateway run
```

Docker：

```bash
docker compose up --build
```

容器会把 `/home/clawgo/.clawgo` 挂载到本地 `./.clawgo`，首次启动时如果没有配置文件会执行 `clawgo onboard`，随后进入 `gateway run`。

## 8. 新功能开发入口建议

- 新 CLI 命令：从 `cmd/main.go` 分发，再新增 `cmd/cmd_xxx.go`。
- 新配置字段：先改 `pkg/config/config.go`，再补 `DefaultConfig()`、`Normalize()` / `Validate()` 和示例配置。
- 新工具：在 `pkg/tools` 实现 `Tool`，再到 `agent.NewAgentLoop()` 注册。
- 新消息通道：实现 `channels.Channel`，在 `channels.Manager.initChannels()` 接入。
- 新 Provider：实现 `providers.LLMProvider`，在 `CreateProviderByName()` 增加路由。
- 新 Web API：在 `pkg/api/server.go` 注册路由，尽量通过 handler 回调依赖运行时能力。
- 新后台循环：优先复用 `pkg/lifecycle.LoopRunner` 管理启动/停止。

## 9. 测试分布

项目已有较多单元测试，重点覆盖：

- API Server。
- Agent planning、router、memory、subagent。
- bus 并发关闭与投递。
- channel 去重和平台行为。
- config normalized / validate。
- cron 调度。
- provider 请求构造、OAuth、兼容协议。
- tools 参数解析、MCP、parallel、camera、remind、skill exec。
- session manager。

新增行为建议就近补测试，尤其是：

- 配置解析和兼容迁移。
- Provider 请求格式。
- 工具参数解析。
- 消息通道去重/限流。
- Agent loop 的 tool call 配对和上下文压缩。

## 10. 当前观察到的注意事项

- 当前工作区已有未提交修改：`pkg/api/server.go`、`pkg/bus/bus.go`、`pkg/channels/weixin.go`、`pkg/channels/weixin_test.go`、`pkg/tools/mcp.go`，以及未跟踪的 `cmd/artifacts/`。本文档没有修改这些文件。
- README 和部分源码里的中文在当前 PowerShell 输出中显示为乱码，可能是终端编码或历史文件编码问题；编辑中文文档时建议统一使用 UTF-8。
- `config.example.json` 中展示的配置较完整，是理解运行能力和默认部署形态的重要参考。
- Gateway 热重载会区分元数据变更和 runtime 相关变更；host/port 变化需要重启才能重新绑定监听端口。
- WebUI/API 可以读写部分配置，但运行时核心仍以本地 `config.json` 为主。

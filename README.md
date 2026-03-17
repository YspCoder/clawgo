# ClawGo

**面向生产的 Go 原生 Agent Runtime。**

ClawGo 不是“又一个聊天壳子”，而是一套可长期运行、可观测、可恢复、可编排的 Agent 运行时。

- `main agent` 负责入口、路由、派发、汇总
- `subagent runtime` 负责本地或远端分支执行
- `runtime store` 持久化 run、event、thread、message、memory
- WebUI 负责检查、状态展示和账号管理，不负责运行时配置写入

[English](./README_EN.md)

## 为什么是 ClawGo

大多数 Agent 项目停留在：

- 一个聊天界面
- 一组工具调用
- 一段 prompt

ClawGo 更关注真正的运行时能力：

- 多 Agent 拓扑和内部协作流
- 可恢复的 subagent run
- 本地主控加远端 node 分支
- 配置、审计、日志、记忆的工程化闭环

一句话：

> **ClawGo 是 Agent Runtime，而不只是 Agent Chat。**

## 核心能力

### 1. 多 Agent 拓扑

- 统一展示 `main / subagents / remote branches`
- 内部协作流与用户对话分离
- 子代理执行过程可追踪，但不会污染用户主通道

### 2. 可恢复执行

- `subagent_runs.jsonl`
- `subagent_events.jsonl`
- `threads.jsonl`
- `agent_messages.jsonl`
- 重启后可恢复进行中的 subagent run

### 3. 工程化配置

- `config.json` 管理 agent registry
- `system_prompt_file -> AGENT.md`
- WebUI 可查看状态、节点、账号与运行信息
- 运行时配置修改回到文件，不通过 WebUI 写入

### 4. 长期运行能力

- 本地优先
- Go 原生 runtime
- 多通道接入
- Task Audit / Logs / Memory / Skills / Config / Agents 全链路闭环

## WebUI

WebUI 当前定位：

- Dashboard 与 Agent 拓扑查看
- 节点、日志、记忆、运行态检查
- OAuth 账号管理

WebUI 当前不负责：

- 修改 subagent/runtime 配置
- 查询或控制公开 task runtime

## 快速开始

### 1. 安装

```bash
curl -fsSL https://raw.githubusercontent.com/YspCoder/clawgo/main/install.sh | bash
```

### 2. 初始化

```bash
clawgo onboard
```

### 3. 选择服务商与模型

```bash
clawgo provider list
clawgo provider use openai/gpt-5.4
clawgo provider configure
```

如果服务商使用 OAuth，例如 `codex`、`anthropic`、`gemini`、`kimi`、`qwen`：

```bash
clawgo provider login codex
clawgo provider login codex --manual
```

如果同一个 provider 同时有 `API key` 和 `OAuth` 账号，推荐配置 `auth: "hybrid"`：

- 优先使用 `api_key`
- 额度或限流失败时自动切到 OAuth 账号池
- 仍保留多账号轮换和后台刷新

### 4. 启动

交互模式：

```bash
clawgo agent
clawgo agent -m "Hello"
```

网关模式：

```bash
clawgo gateway run
```

开发模式：

```bash
make dev
```

WebUI：

```text
http://<host>:<port>/?token=<gateway.token>
```

## 架构概览

默认协作模式：

```text
user -> main -> worker -> main -> user
```

当前系统包含四层：

1. `main agent`
   负责用户入口、路由、派发、汇总
2. `local subagents`
   在 `config.json -> agents.subagents` 中声明，使用独立 session 和 memory namespace
3. `node-backed branches`
   远端节点作为受控 agent 分支挂载到主拓扑
4. `runtime store`
   保存运行态、线程、消息、事件和审计数据

说明：

- `subagent_profile` 保留，用来创建和管理 subagent 定义
- `spawn` 保留，用来触发 subagent 执行
- 公开 task runtime、WebUI 配置写入、EKG、watchdog 已移除

## 配置结构

当前有两层配置视图：

- 落盘文件继续使用原始结构
- 只读接口可暴露标准化视图：
  - `core`
  - `runtime`

推荐结构：

```json
{
  "agents": {
    "defaults": {
      "context_compaction": {},
      "execution": {},
      "summary_policy": {}
    },
    "router": {
      "enabled": true,
      "main_agent_id": "main",
      "strategy": "rules_first",
      "policy": {
        "intent_max_input_chars": 1200,
        "max_rounds_without_user": 200
      },
      "rules": []
    },
    "communication": {},
    "subagents": {
      "main": {},
      "coder": {},
      "tester": {}
    }
  }
}
```

说明：

- `runtime_control` 已移除
- WebUI 配置编辑已禁用
- 运行时配置修改请直接改 `config.json`
- 启用中的本地 subagent 必须配置 `system_prompt_file`
- 远端分支需要：
  - `transport: "node"`
  - `node_id`
  - `parent_agent_id`

完整示例见 [config.example.json](/G:/gopro/clawgo/config.example.json)。

## MCP 服务支持

ClawGo 支持通过 `tools.mcp` 接入 `stdio`、`http`、`streamable_http`、`sse` 型 MCP server。

- 在 `config.json -> tools.mcp.servers` 中声明 server
- 启动时自动发现远端 MCP tools，并注册为本地工具
- 工具命名格式为 `mcp__<server>__<tool>`
- `permission=workspace` 时，`working_dir` 必须留在 workspace 内
- `permission=full` 时，`working_dir` 可以是任意绝对路径

## Prompt 文件约定

推荐把 agent prompt 独立成文件：

- `agents/main/AGENT.md`
- `agents/coder/AGENT.md`
- `agents/tester/AGENT.md`

配置示例：

```json
{
  "system_prompt_file": "agents/coder/AGENT.md"
}
```

规则：

- 路径必须是 workspace 内相对路径
- 仓库不内置这些示例 prompt
- 用户或 agent workflow 需要自行创建实际文件

## 记忆与运行态

ClawGo 不是所有 agent 共用一份上下文。

- `main`
  - 保存主记忆与协作摘要
- `subagent`
  - 使用独立 session key
  - 写入自己的 memory namespace
- `runtime store`
  - 持久化 runs、events、threads、messages
  - 内部仍可使用 task 建模，但不再暴露成公开控制面

这带来三件事：

- 更好恢复
- 更好追踪
- 更清晰的执行边界

## 适合谁

- 想用 Go 做 Agent Runtime 的开发者
- 想要可视化多 Agent 拓扑和内部协作流的团队
- 需要强观测、强审计、强恢复能力的系统
- 想把 agent 配置、prompt、工具权限工程化管理的团队

如果你想快速上手，先看 [config.example.json](/G:/gopro/clawgo/config.example.json)，再跑一次 `make dev`。

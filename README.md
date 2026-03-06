# ClawGo 🦞

ClawGo 是一个用 Go 构建的长期运行 Agent 系统，面向本地优先、多 Agent 协作、可审计运维。

- 🎯 `main agent` 负责对话入口、路由、调度、汇总
- 🤖 `subagents` 负责具体执行，如编码、测试、文档
- 🌐 `node branches` 允许把远端节点挂成受控 agent 分支
- 🧠 记忆、线程、邮箱、任务运行态可持久化
- 🖥️ 内置统一 WebUI，覆盖配置、拓扑、日志、技能、记忆与运维

[English](./README_EN.md)

## 架构概览

ClawGo 的默认协作模式是：

```text
user -> main -> worker -> main -> user
```

当前系统由四层组成：

- `main agent`
  - 用户入口
  - 负责路由、拆解、派发、汇总
- `local subagents`
  - 在 `config.json -> agents.subagents` 中声明
  - 使用独立 session 和 memory namespace
- `node-backed branches`
  - 注册 node 后，会在主拓扑里表现为远端 agent 分支
  - `transport=node` 的任务通过 `agent_task` 发往远端
- `runtime store`
  - 保存 run、event、thread、message 等运行态

## 主要能力

- 🚦 路由与调度
  - 支持 `rules_first`
  - 支持显式 `@agent_id`
  - 支持关键词自动路由
- 📦 持久化运行态
  - `subagent_runs.jsonl`
  - `subagent_events.jsonl`
  - `threads.jsonl`
  - `agent_messages.jsonl`
- 📨 mailbox / thread 协作
  - dispatch
  - wait
  - reply
  - ack
- 🧠 记忆双写
  - 子 agent 写自己的详细记忆
  - 主记忆保留简洁协作摘要
- 🪪 声明式 agent 配置
  - role
  - tool allowlist
  - runtime policy
  - `system_prompt_file`

## 快速开始

### 1. 安装

```bash
curl -fsSL https://raw.githubusercontent.com/YspCoder/clawgo/main/install.sh | bash
```

### 2. 初始化

```bash
clawgo onboard
```

### 3. 配置模型

```bash
clawgo provider
```

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

## WebUI

访问：

```text
http://<host>:<port>/webui?token=<gateway.token>
```

核心页面：

- `Agents`
  - 统一 agent 拓扑
  - 本地 subagent 与远端 branch 展示
  - 运行状态悬浮查看
- `Config`
  - 配置编辑
  - 热更新字段查看
- `Logs`
  - 实时日志
- `Skills`
  - 技能安装、查看、编辑
- `Memory`
  - 记忆文件与摘要
- `Task Audit`
  - 任务链路与执行审计

### 亮点截图

**Dashboard**

![ClawGo Dashboard](docs/assets/readme-dashboard.png)

**Agents 拓扑**

![ClawGo Agents Topology](docs/assets/readme-agents.png)

**Config 工作台**

![ClawGo Config](docs/assets/readme-config.png)

## 配置结构

当前推荐结构：

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

- `runtime_control` 已删除
- 现在使用：
  - `agents.defaults.execution`
  - `agents.defaults.summary_policy`
  - `agents.router.policy`
- 启用中的本地 subagent 必须配置 `system_prompt_file`
- 远端分支需要：
  - `transport: "node"`
  - `node_id`
  - `parent_agent_id`

完整示例见 [config.example.json](/Users/lpf/Desktop/project/clawgo/config.example.json)。

## Prompt 文件约定

推荐把 agent prompt 放到独立文件中，例如：

- `agents/main/AGENT.md`
- `agents/coder/AGENT.md`
- `agents/tester/AGENT.md`

配置示例：

```json
{
  "system_prompt_file": "agents/coder/AGENT.md"
}
```

约定：

- 路径必须是 workspace 内相对路径
- 这些路径只是示例，仓库不会内置对应文件
- 用户或 agent workflow 需要自行创建这些 `AGENT.md`

## 记忆与运行态

ClawGo 默认不是“所有 agent 共用一份上下文”。

- `main`
  - 维护主记忆与协作摘要
- `subagent`
  - 使用独立 session key
  - 写入自己的 memory namespace
- runtime store
  - 持久化任务、事件、线程、消息

这样可以同时得到：

- 可恢复
- 可追踪
- 边界清晰

## 项目定位

ClawGo 适合这些场景：

- 本地长期运行的个人 AI agent
- 需要多 agent 协作但不想上重型编排平台
- 需要清晰配置、清晰审计、清晰可观测性的自动化系统

如果你希望先看一个完整配置，直接从 [config.example.json](/Users/lpf/Desktop/project/clawgo/config.example.json) 开始。

# ClawGo 🦞

**面向生产的 Go 原生 Agent Runtime。**

ClawGo 不是“又一个聊天壳子”，而是一套可长期运行、可观测、可恢复、可编排的 Agent 运行时。

- 👀 **可观测**：Agent 拓扑、内部流、任务审计、EKG 一体化可见
- 🔁 **可恢复**：运行态落盘，重启后任务可恢复，watchdog 按进展续时
- 🧩 **可编排**：`main agent -> subagent -> main`，支持本地与远端 node 分支
- ⚙️ **可工程化**：`config.json`、`AGENT.md`、热更新、WebUI、声明式 registry

[English](./README_EN.md)

## 为什么是 ClawGo

大多数 Agent 项目停留在：

- 一个聊天界面
- 一组工具调用
- 一段 prompt

ClawGo 更关注真正的运行时能力：

- `main agent` 负责入口、路由、派发、汇总
- `subagent` 负责编码、测试、产品、文档等具体执行
- `node branch` 把远端节点挂成受控 agent 分支
- `runtime store` 持久化 run、event、thread、message、memory

一句话：

> **ClawGo = Agent Runtime，而不只是 Agent Chat。**

## 核心亮点 ✨

### 1. 多 Agent 拓扑可视化

- 统一展示 `main / subagents / remote branches`
- 内部流与用户主对话分离
- 子 agent 协作过程可观测，但不污染用户通道

### 2. 任务可恢复，不是一挂全没

- `subagent_runs.jsonl`
- `subagent_events.jsonl`
- `threads.jsonl`
- `agent_messages.jsonl`
- 重启后可恢复运行中的任务

### 3. watchdog 按进展续时

- 系统超时统一走全局 watchdog
- 还在推进的任务不会因为固定墙钟超时被直接杀掉
- 无进展时才超时，行为更接近真实工程执行

### 4. 配置工程化，而不是 prompt 堆砌

- `config.json` 管理 agent registry
- `system_prompt_file -> AGENT.md`
- WebUI 可编辑、热更新、查看运行态

### 5. Spec Coding（规范驱动开发）

- 明确需要编码且属于非 trivial 的任务可走 `spec.md -> tasks.md -> checklist.md`
- 小修小补、轻微代码调整、单点改动默认不启用这套流程
- `spec.md` 负责范围、决策、权衡
- `tasks.md` 负责任务拆解和进度更新
- `checklist.md` 负责最终完整性核查
- 这三份文档是活文档，允许在开发过程中持续修订

### 6. 适合真正长期运行

- 本地优先
- Go 原生 runtime
- 多通道接入
- Task Audit / Logs / Memory / Skills / Config / Agents 全链路闭环

## WebUI 亮点 🖥️

**Dashboard**

![ClawGo Dashboard](docs/assets/readme-dashboard.png)

**Agents 拓扑**

![ClawGo Agents Topology](docs/assets/readme-agents.png)

**Config 工作台**

![ClawGo Config](docs/assets/readme-config.png)

## 快速开始 🚀

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

如果服务商使用 OAuth 登录，例如 `Codex`、`Anthropic`、`Antigravity`、`Gemini CLI`、`Kimi`、`Qwen`：

```bash
clawgo provider list
clawgo provider login codex
clawgo provider login codex --manual
```

登录完成后会把 OAuth 凭证保存到本地，并自动同步该账号可用模型，后续可直接作为普通 provider 使用。
回调型 OAuth（如 `codex` / `anthropic` / `antigravity` / `gemini`）在云服务器场景下可使用 `--manual`：服务端打印授权链接，你在桌面浏览器登录后，把最终回调 URL 粘贴回终端即可完成换取 token。
设备码型 OAuth（如 `kimi` / `qwen`）会直接打印验证链接和用户码，桌面浏览器完成授权后，网关会自动轮询换取 token，无需回填 callback URL。
对同一个 provider 重复执行 `clawgo provider login codex --manual` 会追加多个 OAuth 账号；当某个账号额度耗尽或触发限流时，会自动切换到下一个已登录账号重试。
WebUI 也支持发起 OAuth 登录、回填 callback URL、设备码确认、上传 `auth.json`、查看账号列表、手动刷新和删除账号。

如果你同时有 `API key` 和 `OAuth` 账号，推荐直接把同一个 provider 配成 `auth: "hybrid"`：

- 优先使用 `api_key`
- 当 `api_key` 触发额度不足、429、限流等错误时，自动切到该 provider 下的 OAuth 账号池
- OAuth 账号仍然支持多账号轮换、后台预刷新、`auth.json` 导入和 WebUI 管理
- `oauth.cooldown_sec` 可控制某个 OAuth 账号被限流后暂时熔断多久，默认 `900`
- provider runtime 面板会显示当前候选池排序、最近一次成功命中的凭证，以及最近命中/错误历史
- 如需在重启后保留 runtime 历史，可给 provider 配置 `runtime_persist`、`runtime_history_file`、`runtime_history_max`

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

WebUI:

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

## 你能用它做什么

- 🤖 本地长期运行的个人 Agent
- 🧪 `pm -> coder -> tester` 这种多 Agent 协作链
- 🌐 本地主控 + 远端 node 分支的分布式执行
- 🔍 需要强观测、强审计、强恢复的 Agent 系统
- 🏭 想把 prompt、agent、工具权限、运行策略工程化管理的团队
- 📝 想把编码过程变成可追踪的 spec-driven delivery 流程

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

- `runtime_control` 已移除
- 当前使用：
  - `agents.defaults.execution`
  - `agents.defaults.summary_policy`
  - `agents.router.policy`
- 启用中的本地 subagent 必须配置 `system_prompt_file`
- 远端分支需要：
  - `transport: "node"`
  - `node_id`
  - `parent_agent_id`

完整示例见 [config.example.json](/Users/lpf/Desktop/project/clawgo/config.example.json)。

## Node P2P

远端 node 的调度数据面现在支持：

- `websocket_tunnel`
- `webrtc`

默认仍然关闭，只有显式配置 `gateway.nodes.p2p.enabled=true` 才会启用。建议先用 `websocket_tunnel` 验证链路，再切到 `webrtc`。

`webrtc` 建议同时理解这两个字段：

- `stun_servers`
  - 兼容旧式 STUN 列表
- `ice_servers`
  - 推荐的新结构
  - 可以配置 `stun:`、`turn:`、`turns:` URL
  - `turn:` / `turns:` 必须同时提供 `username` 和 `credential`

示例：

```json
{
  "gateway": {
    "nodes": {
      "p2p": {
        "enabled": true,
        "transport": "webrtc",
        "stun_servers": ["stun:stun.l.google.com:19302"],
        "ice_servers": [
          {
            "urls": ["turn:turn.example.com:3478"],
            "username": "demo",
            "credential": "secret"
          }
        ]
      }
    }
  }
}
```

说明：

- `webrtc` 建连失败时，调度层仍会回退到现有 relay / tunnel 路径
- Dashboard、`status`、`/api/nodes` 会显示当前 Node P2P 状态和会话摘要
- 两台公网机器的实网验证流程见 [docs/node-p2p-e2e.md](/Users/lpf/Desktop/project/clawgo/docs/node-p2p-e2e.md)

## MCP 服务支持

ClawGo 现在支持通过 `tools.mcp` 接入 `stdio`、`http`、`streamable_http`、`sse` 型 MCP server。

- 先在 `config.json -> tools.mcp.servers` 里声明 server
- 当前支持 `list_servers`、`list_tools`、`call_tool`、`list_resources`、`read_resource`、`list_prompts`、`get_prompt`
- 启动时会自动发现远端 MCP tools，并注册为本地工具，命名格式为 `mcp__<server>__<tool>`
- `permission=workspace`（默认）时，`working_dir` 会按 workspace 解析，并且必须留在 workspace 内
- `permission=full` 时，`working_dir` 可指向 `/` 下任意绝对路径，但实际访问权限仍然继承运行 `clawgo` 的 Linux 用户权限

示例配置可直接参考 [config.example.json](/Users/lpf/Desktop/project/clawgo/config.example.json) 中的 `tools.mcp` 段落。

## Prompt 文件约定

推荐把 agent prompt 独立为文件：

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
- 仓库不会内置这些示例文件
- 用户或 agent workflow 需要自行创建实际的 `AGENT.md`

## 记忆与运行态

ClawGo 不是所有 agent 共用一份上下文。

- `main`
  - 保存主记忆与协作摘要
- `subagent`
  - 使用独立 session key
  - 写入自己的 memory namespace
- `runtime store`
  - 持久化任务、事件、线程、消息

这带来三件事：

- 更好恢复
- 更好追踪
- 更清晰的执行边界

## 当前最适合的人群

- 想用 Go 做 Agent Runtime 的开发者
- 想要可视化多 Agent 拓扑和内部流的团队
- 不满足于“聊天 + prompt”，而想要真正运行时能力的用户

如果你想快速上手，先看 [config.example.json](/Users/lpf/Desktop/project/clawgo/config.example.json)，再跑一次 `make dev`。

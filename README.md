# ClawGo

**一个面向长期运行的游戏世界核心。**

ClawGo 现在的核心定位，不再是“通用多 Agent 壳子”，而是一个由 `main` 充当世界意志、由 `npc` 充当自治角色、由结构化状态驱动的 **World Runtime**。

- **世界意志**：`main` 维护世界规则、时间推进、事件裁决和结果渲染
- **自治 NPC**：NPC 有 persona、目标、记忆、关系和局部感知
- **结构化世界**：世界状态、NPC 状态、世界事件独立持久化
- **可恢复运行**：world/runtime 落盘，重启后可继续推进
- **可观测**：WebUI 和 API 可直接查看地点、NPC、实体、任务、事件流
- **可扩展**：保留 `providers`、`channels`、node branch 等底层能力

[English](./README_EN.md)

## 现在它是什么

ClawGo 的默认运行模型是：

```text
user -> main(world mind) -> npc/agent intents -> arbitrate -> apply -> render -> user
```

其中：

- `main`
  - 用户入口
  - 世界事件摄入
  - NPC 唤醒与调度
  - 意图裁决
  - 世界状态更新
  - 叙事输出
- `npc`
  - 只提出 `ActionIntent`
  - 不直接改世界
  - 基于 persona、goals、memory、visible events 自主行动
- `world store`
  - 保存世界状态、NPC 状态、事件审计、运行态记录

一句话：

> **ClawGo = 游戏世界运行时，而不只是聊天式 Agent。**

## 核心亮点

### 1. main 就是世界意志

- `main` 不只是总控，而是世界裁决内核
- 所有用户输入都会先转成世界事件
- 世界真正发生什么，由 `main` 统一裁定

### 2. NPC 是自治角色，不是脚本木偶

- 每个 NPC 有独立 profile
- 支持 persona、traits、faction、home location、default goals
- 支持长期目标驱动和事件响应
- 支持委托、消息、局部感知

### 3. 世界状态是结构化的

核心持久化文件位于 `workspace/agents/runtime`：

- `world_state.json`
- `npc_state.json`
- `world_events.jsonl`
- `agent_runs.jsonl`
- `agent_events.jsonl`
- `agent_messages.jsonl`

### 4. 世界闭环已经跑通

当前已支持：

- 用户输入 -> 世界事件
- NPC 感知 -> 意图生成
- `move / speak / observe / interact / delegate / wait`
- 任务与资源推进
- 动态创建 NPC
- 地图、地点占位、实体占位、任务、事件流可视化

### 5. WebUI 已经是 GM 控制台雏形

- world snapshot
- 地点图
- NPC detail
- entity detail
- quest board
- advance tick
- recent world events

## WebUI

**Dashboard**

![ClawGo Dashboard](docs/assets/readme-dashboard.png)

**World / Runtime**

![ClawGo Agents Topology](docs/assets/readme-agents.png)

**Config / Registry**

![ClawGo Config](docs/assets/readme-config.png)

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

如果服务商使用 OAuth 登录，例如 `Codex`、`Anthropic`、`Antigravity`、`Gemini CLI`、`Kimi`、`Qwen`：

```bash
clawgo provider list
clawgo provider login codex
clawgo provider login codex --manual
```

说明：

- OAuth 凭证会落本地
- 可自动同步账号可用模型
- 同一 provider 可登录多个账号，额度不足时可自动轮换
- WebUI 也支持 OAuth 登录、回填 callback URL、设备码确认、上传 `auth.json`、查看和删除账号

如果同一 provider 同时有 `API key` 与 OAuth 账号，建议使用 `auth: "hybrid"`：

- 优先 `api_key`
- 触发配额/限流错误时自动切 OAuth 账号池
- 支持多账号轮换、后台预刷新和运行历史持久化

### 4. 启动

交互模式：

```bash
clawgo agent
clawgo agent -m "我走进城门口，看看守卫在做什么"
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

## 世界模型

当前世界运行时围绕这些概念组织：

- `WorldState`
  - 世界时钟
  - 地点
  - 全局事实
  - 实体
  - 活跃任务
- `NPCState`
  - 所在地点
  - 短期/长期目标
  - beliefs
  - relationships
  - inventory/assets
  - private memory summary
- `WorldEvent`
  - 用户输入
  - NPC 行为
  - 裁决结果
  - 状态变更摘要
- `ActionIntent`
  - NPC 想做什么
  - 不是世界已经发生了什么

核心循环：

1. ingest
2. perceive
3. decide
4. arbitrate
5. apply
6. render

## 配置结构

当前推荐配置围绕 `agents.agents` 展开：

```json
{
  "agents": {
    "defaults": {
      "context_compaction": {},
      "execution": {},
      "summary_policy": {}
    },
    "agents": {
      "main": {
        "type": "agent",
        "prompt_file": "agents/main/AGENT.md"
      },
      "guard": {
        "kind": "npc",
        "persona": "A cautious town guard",
        "home_location": "gate",
        "default_goals": ["patrol the square"]
      },
      "coder": {
        "type": "agent",
        "prompt_file": "agents/coder/AGENT.md"
      }
    }
  }
}
```

说明：

- 主配置使用：
  - `agents.defaults.execution`
  - `agents.defaults.summary_policy`
  - `agents.agents`
- 可执行型本地 agent 通常配置 `prompt_file`
- 世界内 NPC 通过 `kind: "npc"` 进入 world runtime
- 远端分支仍支持：
  - `transport: "node"`
  - `node_id`
  - `parent_agent_id`
- WebUI 与运行时接口优先消费 normalized schema：
  - `core.default_provider`
  - `core.agents`
  - `runtime.providers`

完整示例见 [config.example.json](./config.example.json)。

## 记忆与运行态

ClawGo 不是所有角色共用一份上下文。

- `main`
  - 保存主记忆与世界协作摘要
- `agent / npc`
  - 使用自己的 memory namespace 或 world 决策上下文
- `runtime store`
  - 持久化任务、事件、消息、world

这带来三件事：

- 更好恢复
- 更好追踪
- 更清晰的执行边界

## 适合做什么

- 自治 NPC 世界模拟
- 小镇 / 地图 / 场景推进
- 剧情驱动沙盒
- 任务板、资源、实体交互
- 本地主控 + 远端 node 分支的混合世界
- 需要强观测、强恢复的游戏世界核心

## Node P2P

底层节点数据面仍然保留，支持：

- `websocket_tunnel`
- `webrtc`

默认关闭，只有显式配置 `gateway.nodes.p2p.enabled=true` 才启用。建议先用 `websocket_tunnel` 验证链路，再切到 `webrtc`。

如果你想直接上手，先看 [config.example.json](./config.example.json)，再跑一次 `make dev`。

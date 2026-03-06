# ClawGo

一个用 Go 编写的长期运行 AI Agent 系统，核心特点是：

- 主 agent + subagent 的声明式配置
- 可持久化的 run / thread / mailbox 运行态
- node 注册后的远端 agent 分支
- 多通道接入与统一 WebUI 运维面板
- 轻量、可审计、适合长期运行

[English](./README_EN.md)

---

## 当前架构

ClawGo 现在不是“单 agent + 一堆工具”的结构，而是一个可编排的 agent tree：

- `main agent`
  - 用户入口
  - 负责路由、调度、汇总结果
- `local subagents`
  - 例如 `coder`、`tester`
  - 由 `config.json` 中的 `agents.subagents` 声明
- `node-backed branches`
  - 注册进来的 node 会被视为 `main` 下的一条远端 agent 分支
  - 形如 `node.<node_id>.main`
  - 可以作为主 agent 的受控执行目标

默认协作模式是：

```text
user -> main -> worker -> main -> user
```

---

## 主要能力

### 1. Agent 配置与路由

- `agents.router`
  - 主 agent 路由配置
  - 支持 `rules_first`、显式 `@agent_id` 路由、关键词路由
- `agents.subagents`
  - 声明 subagent 身份、角色、运行参数、工具白名单
- `system_prompt_file`
  - subagent 优先读取 `agents/<agent_id>/AGENT.md`
  - 不再依赖一句短 inline prompt

### 2. Subagent 运行态持久化

运行态会落到：

- `workspace/agents/runtime/subagent_runs.jsonl`
- `workspace/agents/runtime/subagent_events.jsonl`
- `workspace/agents/runtime/threads.jsonl`
- `workspace/agents/runtime/agent_messages.jsonl`

因此可以支持：

- run 恢复
- 线程追踪
- mailbox / inbox 查询
- dispatch / wait / merge

### 3. Node 分支

注册进来的 node 不只是设备状态，而是一个远端主 agent 分支：

- node 会出现在统一 agent topology 里
- 远端 registry 会以只读镜像方式展示
- `transport=node` 的 subagent 会通过 `agent_task` 发往远端 node

### 4. WebUI

当前 WebUI 已经整合为统一的 agent 操作台，包含：

- Dashboard
- Chat
- Config
- Logs
- Cron
- Skills
- Memory
- Task Audit
- EKG
- Agents
  - 统一的 agent topology
  - 本地/远端 agent 树
  - subagent runtime
  - registry
  - prompt file editor
  - dispatch / thread / inbox

### 5. 记忆策略

记忆不是简单共用，也不是完全隔离，而是双写：

- `main`
  - 维护主记忆与协作摘要
- `subagent`
  - 写入自己的 namespaced memory
  - 路径类似 `workspace/agents/<memory_ns>/...`
- 对于 subagent 的自动摘要：
  - 子 agent 自己记详细日志
  - 主记忆里同步保留一条简洁协作摘要

当前摘要格式大致是：

```md
## 15:04 Code Agent | 修复登录接口并补测试

- Did: 完成了登录接口修复、增加回归测试，并验证通过。
```

---

## 3 分钟上手

### 1. 安装

```bash
curl -fsSL https://raw.githubusercontent.com/YspCoder/clawgo/main/install.sh | bash
```

### 2. 初始化

```bash
clawgo onboard
```

### 3. 配置 provider

```bash
clawgo provider
```

### 4. 查看状态

```bash
clawgo status
```

### 5. 启动

本地交互模式：

```bash
clawgo agent
clawgo agent -m "Hello"
```

网关模式：

```bash
clawgo gateway
clawgo gateway start
clawgo gateway status
```

前台运行：

```bash
clawgo gateway run
```

---

## WebUI

访问地址：

```text
http://<host>:<port>/webui?token=<gateway.token>
```

建议重点看这几个页面：

- `Agents`
  - 看整个 agent tree
  - 看 node 分支
  - 看当前运行任务
  - 配置本地 subagent
- `Config`
  - 调整配置
- `Logs`
  - 实时日志
- `Task Audit`
  - 看任务拆解、调度与执行痕迹
- `EKG`
  - 看错误签名、来源统计、负载分布

---

## 配置结构

当前推荐围绕这些字段配置：

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
      "tester": {},
      "node.edge-dev.main": {}
    }
  }
}
```

关键点：

- `runtime_control` 已移除
- 现在使用：
  - `agents.defaults.execution`
  - `agents.defaults.summary_policy`
  - `agents.router.policy`
- 启用中的本地 subagent 必须配置 `system_prompt_file`
- node-backed agent 使用：
  - `transport: "node"`
  - `node_id`
  - `parent_agent_id`

可参考完整示例：

- [config.example.json](/Users/lpf/Desktop/project/clawgo/config.example.json)

---

## Subagent Prompt 约定

推荐为每个 agent 提供独立 prompt 文件：

- `agents/main/AGENT.md`
- `agents/coder/AGENT.md`
- `agents/tester/AGENT.md`

对应配置示例：

```json
{
  "system_prompt_file": "agents/coder/AGENT.md"
}
```

规则：

- 路径必须是 workspace 内相对路径
- 创建 subagent 时应同时更新 config 和对应 `AGENT.md`
- 如果 subagent 职责发生实质变化，应同步更新它的 `AGENT.md`

---

## 常用命令

```text
clawgo onboard
clawgo provider
clawgo status
clawgo agent [-m "..."]
clawgo gateway [run|start|stop|restart|status]
clawgo config set|get|check|reload
clawgo cron ...
clawgo skills ...
clawgo uninstall [--purge] [--remove-bin]
```

---

## 构建

本地构建：

```bash
make build
```

全平台构建：

```bash
make build-all
```

打包：

```bash
make package-all
```

---

## 当前设计取向

ClawGo 当前刻意偏向这些原则：

- 主 agent 仲裁优先
- 运行态落盘优先
- 配置声明优先
- WebUI 先做统一运维台，再做复杂自动化
- node 先作为受控远端 agent 分支，而不是完全独立自治体

也就是说，这个项目更像一个：

- 可长期运行的个人 AI agent runtime
- 带多 agent 编排能力
- 带 node 扩展能力
- 带运维与审计面的系统

而不是一个“只会聊天”的单体机器人。

---

## License

见仓库中的 `LICENSE` 文件。

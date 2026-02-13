# ClawGo: 高性能 Go 语言 AI 助手 (Linux Server 专用)

[English](./README_EN.md)

**ClawGo** 是一个为 Linux 服务器量身定制的高性能 AI 助手。通过 Go 语言的并发优势与二进制分发特性，它能以极低的资源占用提供完整的 Agent 能力。

## 🚀 核心优势

- **⚡ 纯净运行**：专为 Linux 服务器环境优化，不依赖 Node.js 或 Python。
- **🏗️ 生产级稳定**：单二进制文件部署，完美集成到 systemd 等服务管理工具。
- **🔌 强制上游代理**：通过 [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) 统一管理模型配额与鉴权。
- **🧩 强力技能扩展**：内置 `coding-agent`、`github`、`context7` 等生产力工具。

## 🏁 快速开始

**1. 初始化**
```bash
clawgo onboard
```
运行 `clawgo onboard` / `clawgo gateway` 时会弹出 `yes/no`，可选择是否授予 root 权限。
若选择 `yes`，会以 `sudo` 重新执行命令，并启用高权限策略（仅强制禁止 `rm -rf /`）。

**2. 配置 CLIProxyAPI**
ClawGo 强制要求使用 [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) 作为模型接入层。
```bash
clawgo login
```

**3. 开始运行**
```bash
# 交互模式
clawgo agent

# 后台网关模式 (支持 Telegram/Discord 等)
clawgo gateway

# 网关服务管理
clawgo gateway start
clawgo gateway restart
clawgo gateway stop
```

## ⚙️ 配置管理与热更新

ClawGo 支持直接通过命令修改 `config.json`，并向运行中的网关发送热更新信号：

```bash
# 设置配置（支持 enable -> enabled 自动映射）
clawgo config set channels.telegram.enable true

# 读取配置
clawgo config get channels.telegram.enabled

# 校验配置
clawgo config check

# 手动触发热更新（向 gateway 发送 SIGHUP）
clawgo config reload
```

全局支持自定义配置文件：

```bash
clawgo --config /path/to/config.json status
```

也可使用环境变量：

```bash
export CLAWGO_CONFIG=/path/to/config.json
```

`config set` 采用原子写入，并在网关运行且热更新失败时自动回滚到备份，避免配置损坏导致服务不可用。

也支持在聊天通道中使用斜杠命令：

```text
/help
/stop
/status
/config get channels.telegram.enabled
/config set channels.telegram.enabled true
/reload
```

消息调度策略（按会话 `session_key`）：
- 同一会话严格 FIFO 串行执行，后续消息进入队列等待。
- `/stop` 会立即中断当前回复，并继续处理队列中的下一条消息。
- 不同会话可并发执行，互不影响。

## 🧾 日志链路

默认启用文件日志，并支持自动分割和过期清理（默认保留 3 天）：

```json
"logging": {
  "enabled": true,
  "dir": "~/.clawgo/logs",
  "filename": "clawgo.log",
  "max_size_mb": 20,
  "retention_days": 3
}
```

当前通道与网关链路日志已统一为结构化字段，建议告警与检索统一使用：
- `channel`
- `chat_id`
- `sender_id`
- `preview`
- `error`
- `message_content_length`
- `assistant_content_length`
- `user_response_content_length`
- `fetched_content_length`
- `output_content_length`
- `transcript_length`

字段常量已集中在 `pkg/logger/fields.go`，新增日志字段建议优先复用常量，避免命名漂移。

## 🛡️ Sentinel 与风险防护

Sentinel 会周期巡检关键运行资源（配置、memory、日志目录），支持自动修复与告警转发：

```json
"sentinel": {
  "enabled": true,
  "interval_sec": 60,
  "auto_heal": true,
  "notify_channel": "",
  "notify_chat_id": ""
}
```

Shell 工具默认启用 Risk Gate。检测到破坏性命令时，默认阻断并要求 `force=true`，可先做 dry-run：

```json
"tools": {
  "shell": {
    "risk": {
      "enabled": true,
      "allow_destructive": false,
      "require_dry_run": true,
      "require_force_flag": true
    }
  }
}
```

## 🤖 多智能体编排 (Pipeline)

新增标准化任务编排协议：`role + goal + depends_on + shared_state`。

可用工具：
- `pipeline_create`：创建任务图
- `pipeline_status`：查看流水线状态
- `pipeline_state_set`：写入共享状态
- `pipeline_dispatch`：自动派发当前可执行任务
- `spawn`：支持 `pipeline_id/task_id/role` 参数

通道内可查看状态：

```text
/pipeline list
/pipeline status <pipeline_id>
/pipeline ready <pipeline_id>
```

## 🧠 记忆与索引增强

- `memory_search`：增加结构化索引（倒排索引 + 缓存），优先走索引检索。
- 记忆分层：支持 `profile / project / procedures / recent notes`。
- 自动上下文压缩（Automatically Compacted Context）：会话过长时自动生成摘要并裁剪历史，降低 token 开销与卡顿风险。

```json
"memory": {
  "layered": true,
  "recent_days": 3,
  "layers": {
    "profile": true,
    "project": true,
    "procedures": true
  }
}
```

上下文自动压缩配置：

```json
"agents": {
  "defaults": {
    "context_compaction": {
      "enabled": true,
      "trigger_messages": 60,
      "keep_recent_messages": 20,
      "max_summary_chars": 6000,
      "max_transcript_chars": 20000
    }
  }
}
```

也可以热更新：

```bash
clawgo config set agents.defaults.context_compaction.enabled true
clawgo config set agents.defaults.context_compaction.trigger_messages 80
clawgo config set agents.defaults.context_compaction.keep_recent_messages 24
```

## 🗺️ Repo-Map 与原子技能

- `repo_map`：生成并查询代码全景地图，先定位目标文件再精读。
- `skill_exec`：执行 `skills/<name>/scripts/*` 原子脚本，保持 Gateway 精简。

## 📦 迁移与技能

ClawGo 现在集成了原 OpenClaw 的所有核心扩展能力：
- **coding-agent**: 结合 Codex/Claude Code 实现自主编程。
- **github**: 深度集成 `gh` CLI，管理 Issue、PR 及 CI 状态。
- **context7**: 针对代码库与文档的智能上下文搜索。

## 🛠️ 安装 (仅限 Linux)

### 从源码编译
```bash
cd clawgo
make build
make install
```

## 📜 许可证

MIT 许可证。 🦐

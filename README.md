# ClawGo: 高性能 Go 语言 AI 助手 (Linux Server 专用)

[English](./README_EN.md)

**ClawGo** 是一个面向 Linux 服务器的 Go 原生 AI Agent。它提供单二进制部署、多通道接入与可热更新配置，适合长期在线自动化任务。

## 🚀 功能总览

- **双运行模式**：支持本地交互模式（`agent`）与服务化网关模式（`gateway`）。
- **多通道接入**：支持 Telegram、Discord、Feishu、WhatsApp、QQ、DingTalk、MaixCam。
- **自主协作能力**：支持自然语言驱动的自主执行、自动学习与启动自检。
- **多智能体编排**：支持 Pipeline 协议（`role + goal + depends_on + shared_state`）。
- **记忆与上下文治理**：支持分层记忆、`memory_search` 与自动上下文压缩。
- **可靠性增强**：支持代理内模型切换与跨代理切换（`proxy_fallbacks`），覆盖配额、路由、网关瞬时错误等场景。
- **稳定性保障**：Sentinel 巡检与自动修复能力。
- **技能扩展**：支持内置技能与 GitHub 技能安装，支持原子脚本执行。

## 🧠 架构级优化（Go 特性）

近期已完成一轮架构增强，重点利用 Go 并发与类型系统能力：

1. **Actor 化关键路径（process）**
   - process 元数据持久化改为异步队列（`persistQ`）串行落盘。
   - channel 启停编排使用 `errgroup.WithContext` 并发+统一取消。
2. **Typed Events 事件总线**
   - 新增 `pkg/events/typed_bus.go` 泛型事件总线。
   - process 生命周期事件（start/exit/kill）可发布订阅。
3. **日志批量刷盘**
   - process 日志由 `logWriter` 批量 flush（时间片 + 大小阈值），减少高频 I/O。
   - outbound 分发增加 `rate.Limiter`（令牌桶）平滑突发流量。
4. **Context 分层取消传播**
   - 后台进程改为 `exec.CommandContext`，通过父 `ctx` 统一取消。
5. **原子配置快照**
   - 新增 `pkg/runtimecfg/snapshot.go`，网关启动与热重载时原子替换配置快照。

这些优化提升了高并发场景下的稳定性、可观测性与可维护性。

### 多节点 / 设备控制（Phase-1）

已新增 `nodes` 工具控制平面（PoC）：

- `action=status|describe`：查看已配对节点状态与能力矩阵
- `action=run|invoke|camera_snap|screen_record|location_get`：已接入路由框架（下一阶段接数据平面传输）

实现位置：
- `pkg/nodes/types.go`
- `pkg/nodes/manager.go`
- `pkg/tools/nodes_tool.go`

### 并行任务冲突控制（Autonomy）

支持基于 `resource_keys` 的锁调度。任务可在内容中显式声明资源键，提升并行判冲突精度：

- 示例：`[keys: repo:clawgo, file:pkg/agent/loop.go, branch:main] 修复对话流程`
- 未显式声明时，系统会从任务文本自动推断资源键。
- 冲突任务进入 `resource_lock` 等待，默认 30 秒后重试抢锁，并带公平加权（等待越久优先级越高）。

## 🏁 快速开始

1. 初始化配置与工作区

```bash
clawgo onboard
```

2. 配置上游代理（必需）

```bash
clawgo login
```

3. 检查当前状态

```bash
clawgo status
```

4. 交互式使用（本地）

```bash
clawgo agent
# 或单轮消息
clawgo agent -m "Hello"
```

5. 启动网关服务（用于 Telegram/Discord 等）

```bash
# 注册服务（systemd）
clawgo gateway

# 服务管理
clawgo gateway start
clawgo gateway restart
clawgo gateway stop
clawgo gateway status

# 前台运行
clawgo gateway run

# 自治开关（运行态）
clawgo gateway autonomy status
clawgo gateway autonomy on
clawgo gateway autonomy off
```

## 📌 命令总览

```text
clawgo onboard                     初始化配置和工作区
clawgo login                       配置 CLIProxyAPI 上游
clawgo status                      查看配置、工作区、模型和日志状态
clawgo agent [-m "..."]           本地交互模式
clawgo gateway [...]               注册/运行/管理网关服务
clawgo config set|get|check|reload 配置读写、校验与热更新
clawgo channel test ...            通道连通性测试
clawgo cron ...                    定时任务管理
clawgo skills ...                  技能安装/查看/卸载
clawgo uninstall [--purge] [--remove-bin]
```

全局参数：

```bash
clawgo --config /path/to/config.json <command>
clawgo --debug <command>
```

## ⚙️ 配置管理与热更新

支持命令行直接修改配置，并向运行中的网关发送热更新信号：

```bash
clawgo config set channels.telegram.enable true
clawgo config get channels.telegram.enabled
clawgo config check
clawgo config reload
```

说明：
- `enable` 会自动映射到 `enabled`。
- `config set` 使用原子写入。
- 网关运行时若热更新失败，会自动回滚备份，避免损坏配置。
- `--config` 指定的自定义配置路径会被 `config` 命令与通道内 `/config` 指令一致使用。
- 配置加载使用严格 JSON 解析：未知字段与多余 JSON 内容会直接报错，避免拼写错误被静默忽略。

## 🌐 通道与消息控制

通道中支持以下斜杠命令：

```text
/help
/stop
/status
/status run [run_id|latest]
/status wait <run_id|latest> [timeout_seconds]
/config get <path>
/config set <path> <value>
/reload
/pipeline list
/pipeline status <pipeline_id>
/pipeline ready <pipeline_id>
```

自主与学习控制默认使用自然语言，不再依赖斜杠命令。例如：
- `开始自主模式，每 30 分钟巡检一次`
- `停止自动学习`
- `看看最新 run 的状态`
- `等待 run-1739950000000000000-8 完成后告诉我结果`

调度语义（按 `session_key`）：
- 同会话严格 FIFO 串行处理。
- `/stop` 会中断当前回复并继续队列后续消息。
- 不同会话并发执行，互不阻塞。

通道连通测试：

```bash
clawgo channel test --channel telegram --to <chat_id> -m "ping"
```

## 🧠 记忆、自主与上下文压缩

- 启动会读取 `AGENTS.md`、`SOUL.md`、`USER.md` 作为行为约束与语义上下文。
- 网关启动后会执行一次自检任务，结合历史会话与 `HEARTBEAT.md` 判断是否继续未完成任务。
- 上下文压缩同时按消息数量阈值和上下文体积阈值触发，控制 token 成本与长会话稳定性。
- 上下文压缩模式支持 `summary`、`responses_compact`、`hybrid`；`responses_compact` 需要代理配置 `protocol=responses` 且 `supports_responses_compact=true`。
- 分层记忆支持 `profile / project / procedures / recent notes`。

心跳与上下文压缩配置示例：

```json
"agents": {
  "defaults": {
    "heartbeat": {
      "enabled": true,
      "every_sec": 1800,
      "ack_max_chars": 64,
      "prompt_template": "Read HEARTBEAT.md if it exists (workspace context). Follow it strictly. Do not infer or repeat old tasks from prior chats. If nothing needs attention, reply HEARTBEAT_OK."
    },
    "texts": {
      "no_response_fallback": "I've completed processing but have no response to give.",
      "think_only_fallback": "Thinking process completed.",
      "memory_recall_keywords": ["remember", "记得", "上次", "之前", "偏好", "preference", "todo", "待办", "决定", "decision"],
      "lang_usage": "Usage: /lang <code>",
      "lang_invalid": "Invalid language code.",
      "lang_updated_template": "Language preference updated to %s",
      "subagents_none": "No subagents.",
      "sessions_none": "No sessions.",
      "unsupported_action": "unsupported action",
      "system_rewrite_template": "Rewrite the following internal system update in concise user-facing language:\n\n%s",
      "runtime_compaction_note": "[runtime-compaction] removed %d old messages, kept %d recent messages",
      "startup_compaction_note": "[startup-compaction] removed %d old messages, kept %d recent messages"
    },
    "context_compaction": {
      "enabled": true,
      "mode": "summary",
      "trigger_messages": 60,
      "keep_recent_messages": 20,
      "max_summary_chars": 6000,
      "max_transcript_chars": 20000
    }
  }
}
```

运行控制配置示例（自主循环守卫 / 运行态保留）：

```json
"agents": {
  "defaults": {
    "runtime_control": {
      "intent_max_input_chars": 1200,
      "autonomy_tick_interval_sec": 20,
      "autonomy_min_run_interval_sec": 20,
      "autonomy_idle_threshold_sec": 20,
      "autonomy_max_rounds_without_user": 120,
      "autonomy_max_pending_duration_sec": 180,
      "autonomy_max_consecutive_stalls": 3,
      "autolearn_max_rounds_without_user": 200,
      "run_state_ttl_seconds": 1800,
      "run_state_max": 500,
      "tool_parallel_safe_names": ["read_file", "list_files", "find_files", "grep_files", "memory_search", "web_search", "repo_map", "system_info"],
      "tool_max_parallel_calls": 2
    }
  }
}
```

## 🤖 多智能体编排 (Pipeline)

内置标准化编排工具：
- `pipeline_create`
- `pipeline_status`
- `pipeline_state_set`
- `pipeline_dispatch`
- `spawn`（支持 `pipeline_id/task_id/role`）

适用于拆解复杂任务、跨角色协作和共享状态推进。

## 🛡️ 稳定性

- **Proxy/Model fallback**：先在当前代理中按 `models` 顺序切换，全部失败后再按 `proxy_fallbacks` 切换代理。
- **HTTP 兼容处理**：可识别非 JSON 错页并给出响应预览；兼容从 `<function_call>` 文本块提取工具调用。
- **Sentinel**：周期巡检配置/内存/日志目录，支持自动修复与告警转发。

Sentinel 配置示例：

```json
"sentinel": {
  "enabled": true,
  "interval_sec": 60,
  "auto_heal": true,
  "notify_channel": "",
  "notify_chat_id": ""
}
```

## ⏱️ 定时任务 (Cron)

```bash
clawgo cron list
clawgo cron add -n "daily-check" -m "检查待办" -c "0 9 * * *"
clawgo cron add -n "heartbeat" -m "汇报状态" -e 300
clawgo cron enable <job_id>
clawgo cron disable <job_id>
clawgo cron remove <job_id>
```

`cron add` 支持：
- `-n, --name` 任务名
- `-m, --message` 发给 agent 的消息
- `-e, --every` 每 N 秒执行
- `-c, --cron` cron 表达式
- `-d, --deliver --channel <name> --to <id>` 投递到消息通道

## 🧩 技能系统

技能管理命令：

```bash
clawgo skills list
clawgo skills search
clawgo skills show <name>
clawgo skills install <github-repo>
clawgo skills remove <name>
clawgo skills install-builtin
clawgo skills list-builtin
```

说明：
- 支持从 GitHub 仓库安装技能（例如 `owner/repo/skill`）。
- 支持安装内置技能到工作区。
- 支持 `skill_exec` 原子执行 `skills/<name>/scripts/*`。

## 🗂️ 工作区与文档同步

默认工作区通常为 `~/.clawgo/workspace`，关键目录：

```text
workspace/
  memory/
    MEMORY.md
    HEARTBEAT.md
  skills/
  AGENTS.md
  SOUL.md
  USER.md
```

`clawgo onboard` 与 `make install` 会同步 `AGENTS.md`、`SOUL.md`、`USER.md`：
- 文件不存在则创建。
- 文件存在则仅更新 `CLAWGO MANAGED BLOCK` 区块，保留用户自定义内容。

## 🧾 日志

默认启用文件日志，支持轮转和保留：

```json
"logging": {
  "enabled": true,
  "dir": "~/.clawgo/logs",
  "filename": "clawgo.log",
  "max_size_mb": 20,
  "retention_days": 3
}
```

推荐统一检索的结构化字段：
`channel`、`chat_id`、`sender_id`、`preview`、`error`、`message_content_length`、`assistant_content_length`、`output_content_length`、`transcript_length`。

## 🛠️ 安装与构建（Linux）

```bash
cd clawgo
make build
make install
```

可选构建参数：

```bash
# 默认 1：剥离符号，减小体积
make build STRIP_SYMBOLS=1

# 保留调试符号
make build STRIP_SYMBOLS=0
```

## 🧹 卸载

```bash
clawgo uninstall
clawgo uninstall --purge
clawgo uninstall --remove-bin
```

## 📜 许可证

MIT License.

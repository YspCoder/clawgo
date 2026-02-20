# ClawGo: High-Performance AI Agent (Linux Server Only)

[中文](./README.md)

**ClawGo** is a Go-native AI agent for Linux servers. It provides single-binary deployment, multi-channel integration, and hot-reloadable config for long-running autonomous workflows.

## 🚀 Feature Overview

- **Dual runtime modes**: local interactive mode (`agent`) and service-oriented gateway mode (`gateway`).
- **Multi-channel integration**: Telegram, Discord, Feishu, WhatsApp, QQ, DingTalk, MaixCam.
- **Autonomous collaboration**: natural-language autonomy, auto-learning, and startup self-check.
- **Multi-agent orchestration**: built-in Pipeline protocol (`role + goal + depends_on + shared_state`).
- **Memory and context governance**: layered memory, `memory_search`, and automatic context compaction.
- **Reliability enhancements**: in-proxy model switching and cross-proxy fallback (`proxy_fallbacks`) for quota, routing, and transient gateway failures.
- **Stability controls**: Sentinel inspection and auto-heal support.
- **Skill extensibility**: built-in skills plus GitHub skill installation and atomic script execution.

## 🏁 Quick Start

1. Initialize config and workspace

```bash
clawgo onboard
```

2. Configure upstream proxy (required)

```bash
clawgo login
```

3. Check runtime status

```bash
clawgo status
```

4. Run local interactive mode

```bash
clawgo agent
# or one-shot message
clawgo agent -m "Hello"
```

5. Start gateway service (for Telegram/Discord/etc.)

```bash
# register systemd service
clawgo gateway

# service control
clawgo gateway start
clawgo gateway restart
clawgo gateway stop
clawgo gateway status

# foreground run
clawgo gateway run
```

## 📌 Command Reference

```text
clawgo onboard                     Initialize config and workspace
clawgo login                       Configure CLIProxyAPI upstream
clawgo status                      Show config/workspace/model/logging status
clawgo agent [-m "..."]           Local interactive mode
clawgo gateway [...]               Register/run/manage gateway service
clawgo config set|get|check|reload Config CRUD, validation, hot reload
clawgo channel test ...            Channel connectivity test
clawgo cron ...                    Scheduled job management
clawgo skills ...                  Skill install/list/remove/show
clawgo uninstall [--purge] [--remove-bin]
```

Global flags:

```bash
clawgo --config /path/to/config.json <command>
clawgo --debug <command>
```

## ⚙️ Config Management and Hot Reload

Update config values directly from CLI and trigger gateway hot reload:

```bash
clawgo config set channels.telegram.enable true
clawgo config get channels.telegram.enabled
clawgo config check
clawgo config reload
```

Notes:
- `enable` is normalized to `enabled`.
- `config set` uses atomic write.
- If gateway reload fails while running, config auto-rolls back from backup.
- Custom `--config` path is consistently used by CLI config commands and in-channel `/config` commands.
- Config loading uses strict JSON decoding: unknown fields and trailing JSON content now fail fast.

## 🌐 Channels and Message Control

Supported in-channel slash commands:

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

Autonomy and auto-learn control now default to natural language (no slash commands required). Examples:
- `start autonomy mode and check every 30 minutes`
- `stop auto-learn`
- `show latest run status`
- `wait for run-1739950000000000000-8 and report when done`

Scheduling semantics (`session_key` based):
- Strict FIFO processing per session.
- `/stop` interrupts current response and continues queued messages.
- Different sessions are processed concurrently.

Channel test example:

```bash
clawgo channel test --channel telegram --to <chat_id> -m "ping"
```

## 🧠 Memory, Autonomy, and Context Compaction

- On startup, the agent loads `AGENTS.md`, `SOUL.md`, and `USER.md` as behavior and semantic constraints.
- Gateway startup triggers a self-check task using history and `memory/HEARTBEAT.md` to decide whether unfinished tasks should continue.
- Context compaction is triggered by both message-count and transcript-size thresholds.
- Compaction modes are `summary`, `responses_compact`, and `hybrid`; `responses_compact` requires `protocol=responses` and `supports_responses_compact=true` on the active proxy.
- Layered memory supports `profile / project / procedures / recent notes`.

Context compaction config example:

```json
"agents": {
  "defaults": {
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

Runtime-control config example (autonomy guards / run-state retention):

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

## 🤖 Multi-Agent Orchestration (Pipeline)

Built-in orchestration tools:
- `pipeline_create`
- `pipeline_status`
- `pipeline_state_set`
- `pipeline_dispatch`
- `spawn` (supports `pipeline_id/task_id/role`)

Useful for complex task decomposition, role-based execution, and shared state workflows.

## 🛡️ Reliability

- **Proxy/model fallback**: retries models in the current proxy first, then switches proxies in `proxy_fallbacks` when all models fail.
- **HTTP compatibility handling**: detects non-JSON error pages with body preview; parses tool calls from `<function_call>` blocks.
- **Sentinel**: periodic checks for config/memory/log resources with optional auto-heal and notifications.

Sentinel config example:

```json
"sentinel": {
  "enabled": true,
  "interval_sec": 60,
  "auto_heal": true,
  "notify_channel": "",
  "notify_chat_id": ""
}
```

## ⏱️ Scheduled Jobs (Cron)

```bash
clawgo cron list
clawgo cron add -n "daily-check" -m "check todo" -c "0 9 * * *"
clawgo cron add -n "heartbeat" -m "report status" -e 300
clawgo cron enable <job_id>
clawgo cron disable <job_id>
clawgo cron remove <job_id>
```

`cron add` options:
- `-n, --name` job name
- `-m, --message` agent input message
- `-e, --every` run every N seconds
- `-c, --cron` cron expression
- `-d, --deliver --channel <name> --to <id>` deliver response to a channel

## 🧩 Skills

Skill management commands:

```bash
clawgo skills list
clawgo skills search
clawgo skills show <name>
clawgo skills install <github-repo>
clawgo skills remove <name>
clawgo skills install-builtin
clawgo skills list-builtin
```

Notes:
- Install skills from GitHub (for example `owner/repo/skill`).
- Install built-in skills into workspace.
- Execute atomic scripts through `skill_exec` from `skills/<name>/scripts/*`.

## 🗂️ Workspace and Managed Docs

Default workspace is typically `~/.clawgo/workspace`:

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

`clawgo onboard` and `make install` sync `AGENTS.md`, `SOUL.md`, `USER.md`:
- Create file if missing.
- Update only `CLAWGO MANAGED BLOCK` if file exists, preserving user custom sections.

## 🧾 Logging

File logging is enabled by default with rotation and retention:

```json
"logging": {
  "enabled": true,
  "dir": "~/.clawgo/logs",
  "filename": "clawgo.log",
  "max_size_mb": 20,
  "retention_days": 3
}
```

Recommended structured fields for querying/alerting:
`channel`, `chat_id`, `sender_id`, `preview`, `error`, `message_content_length`, `assistant_content_length`, `output_content_length`, `transcript_length`.

## 🛠️ Build and Install (Linux)

```bash
cd clawgo
make build
make install
```

Optional build flag:

```bash
# default: strip symbols for smaller binary
make build STRIP_SYMBOLS=1

# keep debug symbols
make build STRIP_SYMBOLS=0
```

## 🧹 Uninstall

```bash
clawgo uninstall
clawgo uninstall --purge
clawgo uninstall --remove-bin
```

## 📜 License

MIT License.

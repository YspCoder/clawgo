# ClawGo: High-Performance AI Agent (Linux Server Only)

[中文](./README.md)

**ClawGo** is a high-performance AI assistant tailored for Linux servers. Leveraging the concurrency advantages and binary distribution of the Go language, it provides full agent capabilities with minimal resource overhead.

## 🚀 Key Advantages

- **⚡ Native Performance**: Optimized specifically for Linux server environments, with no dependency on Node.js or Python.
- **🏗️ Production-Ready**: Single binary deployment, perfectly integrates with service management tools like systemd.
- **🔌 Mandatory Upstream Proxy**: Centralized management of model quotas and authentication via [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI).
- **🧩 Powerful Skill Extension**: Built-in productivity tools like `coding-agent`, `github`, and `context7`.

## 🏁 Quick Start

**1. Initialize**
```bash
clawgo onboard
```
When running `clawgo gateway`, a `yes/no` prompt asks whether to grant root privileges.
If `yes`, the command is re-executed via `sudo` and a high-permission shell policy is enabled (with `rm -rf /` still hard-blocked).

**2. Configure CLIProxyAPI**
ClawGo requires [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) as the model access layer.
```bash
clawgo login
```

**3. Start Running**
```bash
# Interactive mode
clawgo agent

# Gateway mode (supports Telegram, Discord, etc.)
clawgo gateway

# Gateway service management
clawgo gateway start
clawgo gateway restart
clawgo gateway stop
```

## ⚙️ Config Management & Hot Reload

ClawGo can update `config.json` from CLI and trigger hot reload for a running gateway:

```bash
# Set config value (supports enable -> enabled alias)
clawgo config set channels.telegram.enable true

# Read config value
clawgo config get channels.telegram.enabled

# Validate config
clawgo config check

# Trigger hot reload manually (sends SIGHUP to gateway)
clawgo config reload
```

Global custom config path:

```bash
clawgo --config /path/to/config.json status
```

Or via environment variable:

```bash
export CLAWGO_CONFIG=/path/to/config.json
```

`config set` now uses atomic write, and if gateway is running but hot reload fails, it rolls back to backup automatically.

Slash commands are also supported in chat channels:

```text
/help
/stop
/status
/config get channels.telegram.enabled
/config set channels.telegram.enabled true
/reload
```

Autonomy control commands (optional; natural language is recommended):

```text
/autonomy start [idle]
/autonomy stop
/autonomy status
/autolearn start [interval]
/autolearn stop
/autolearn status
```

Message scheduling policy (per `session_key`):
- Same session runs in strict FIFO order; later messages are queued.
- `/stop` immediately cancels the current response, then processing continues with the next queued message.
- Different sessions can run concurrently.

## 🧭 Autonomy Mode & Natural-Language Control

- Autonomy/auto-learning controls are interpreted with **LLM semantic parsing first** (multi-language), instead of fixed keyword matching.
- Rule-based parsing is used only as a fallback (for explicit forms such as `/autonomy ...` and `/autolearn ...`).
- If a focus direction is provided when starting autonomy mode, execution prioritizes that focus; when user says it is complete, the agent switches to other high-value tasks automatically.
- Progress updates are natural-language messages, not rigid stage-number templates.

At startup, `AGENTS.md`, `SOUL.md`, and `USER.md` are loaded as behavior constraints and semantic context.

## 🧩 Onboard/Install Doc Sync

- Both `clawgo onboard` and `make install` sync `AGENTS.md`, `SOUL.md`, `USER.md` into workspace.
- If a file does not exist: it is created.
- If a file already exists: only the `CLAWGO MANAGED BLOCK` section is updated; user custom content is preserved (incremental update, no whole-file overwrite).

## 🧾 Logging Pipeline

File logging is enabled by default with automatic rotation and retention cleanup (3 days by default):

```json
"logging": {
  "enabled": true,
  "dir": "~/.clawgo/logs",
  "filename": "clawgo.log",
  "max_size_mb": 20,
  "retention_days": 3
}
```

Structured logging keys are now unified across channels and gateway:
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

Constants are centralized in `pkg/logger/fields.go`.

## 🛡️ Sentinel & Risk Protection

Sentinel periodically checks critical runtime resources (config, memory, log directories), with optional auto-heal and notify:

```json
"sentinel": {
  "enabled": true,
  "interval_sec": 60,
  "auto_heal": true,
  "notify_channel": "",
  "notify_chat_id": ""
}
```

Cron scheduling runtime strategy is configurable (and hot-reloadable):

```json
"cron": {
  "min_sleep_sec": 1,
  "max_sleep_sec": 30,
  "retry_backoff_base_sec": 30,
  "retry_backoff_max_sec": 1800,
  "max_consecutive_failure_retries": 5,
  "max_workers": 4
}
```

Shell risk gate is enabled by default. Destructive commands are blocked unless explicitly forced:

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

## 🤖 Multi-Agent Orchestration (Pipeline)

Task protocol is standardized with: `role + goal + depends_on + shared_state`.

Available tools:
- `pipeline_create`: create a task graph
- `pipeline_status`: inspect pipeline status
- `pipeline_state_set`: write shared state
- `pipeline_dispatch`: dispatch currently runnable tasks
- `spawn`: supports `pipeline_id/task_id/role`

Channel commands:

```text
/pipeline list
/pipeline status <pipeline_id>
/pipeline ready <pipeline_id>
```

## 🧠 Memory & Context Indexing

- `memory_search`: structured indexing (inverted index + cache)
- Memory layers: `profile / project / procedures / recent notes`
- Automatically Compacted Context: when a session gets long, it auto-summarizes and trims history

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

Automatic context compaction config:

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

Hot-update examples:

```bash
clawgo config set agents.defaults.context_compaction.enabled true
clawgo config set agents.defaults.context_compaction.trigger_messages 80
clawgo config set agents.defaults.context_compaction.keep_recent_messages 24
```

## 🗺️ Repo-Map & Atomic Skills

- `repo_map`: build/query repository map before deep file reads
- `skill_exec`: execute `skills/<name>/scripts/*` atomically to keep gateway slim

## 📦 Migration & Skills

ClawGo now integrates all core extended capabilities from the original OpenClaw:
- **coding-agent**: Autonomous programming using Codex/Claude Code.
- **github**: Deep integration with `gh` CLI for managing issues, PRs, and CI status.
- **context7**: Intelligent context search for codebases and documentation.

## 🛠️ Installation (Linux Only)

### Build from Source
```bash
cd clawgo
make build
make install
```

## 📜 License

MIT License. 🦐

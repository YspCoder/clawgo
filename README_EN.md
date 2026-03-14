# ClawGo 🦞

**A production-oriented, Go-native Agent Runtime.**

ClawGo is not just another chat wrapper. It is a long-running, observable, recoverable, orchestrated runtime for real agent systems.

- 👀 **Observable**: agent topology, internal streams, task audit, and EKG visibility
- 🔁 **Recoverable**: persisted runtime state, restart recovery, progress-aware watchdog
- 🧩 **Orchestrated**: `main agent -> subagent -> main`, with local and remote node branches
- ⚙️ **Operational**: `config.json`, `AGENT.md`, hot reload, WebUI, declarative registry

[中文](./README.md)

## Why ClawGo

Most agent projects stop at:

- a chat UI
- a tool runner
- a prompt layer

ClawGo focuses on runtime capabilities:

- `main agent` handles entry, routing, dispatch, and merge
- `subagents` execute coding, testing, product, docs, and other focused tasks
- `node branches` attach remote nodes as controlled agent branches
- `runtime store` persists runs, events, threads, messages, and memory

In one line:

> **ClawGo is an Agent Runtime, not just an Agent Chat shell.**

## Core Highlights ✨

### 1. Observable multi-agent topology

- unified view of `main / subagents / remote branches`
- internal subagent streams are visible
- user-facing chat stays clean while internal collaboration remains inspectable

### 2. Recoverable execution

- `subagent_runs.jsonl`
- `subagent_events.jsonl`
- `threads.jsonl`
- `agent_messages.jsonl`
- running tasks can recover after restart

### 3. Progress-aware watchdog

- system timeouts go through one global watchdog
- active tasks are extended instead of being killed by a fixed wall-clock timeout
- tasks time out only when they stop making progress

### 4. Engineering-first agent configuration

- agent registry in `config.json`
- `system_prompt_file -> AGENT.md`
- WebUI for editing, hot reload, and runtime inspection

### 5. Built for long-running use

- local-first
- Go-native runtime
- multi-channel capable
- end-to-end ops surface: Task Audit, Logs, Memory, Skills, Config, Agents

## WebUI Highlights 🖥️

**Dashboard**

![ClawGo Dashboard](docs/assets/readme-dashboard.png)

**Agent Topology**

![ClawGo Agents Topology](docs/assets/readme-agents.png)

**Config Workspace**

![ClawGo Config](docs/assets/readme-config.png)

## Quick Start 🚀

### 1. Install

```bash
curl -fsSL https://raw.githubusercontent.com/YspCoder/clawgo/main/install.sh | bash
```

### 2. Initialize

```bash
clawgo onboard
```

### 3. Choose a provider and model

```bash
clawgo provider list
clawgo provider use openai/gpt-5.4
clawgo provider configure
```

For OAuth-backed providers such as `Codex`, `Anthropic`, `Antigravity`, `Gemini CLI`, `Kimi`, and `Qwen`:

```bash
clawgo provider list
clawgo provider login codex
clawgo provider login codex --manual
```

After login, clawgo stores the OAuth session locally and syncs the models available to that account so the provider can be used directly.
Use `--manual` on a cloud server for callback-based OAuth (`codex`, `anthropic`, `antigravity`, `gemini`): clawgo prints the auth URL, you complete login in a desktop browser, then paste the final callback URL back into the terminal.
Device-flow OAuth (`kimi`, `qwen`) prints the verification URL and user code, then clawgo polls automatically after authorization without requiring a callback URL to be pasted back.
Repeat `clawgo provider login codex --manual` on the same provider to add multiple OAuth accounts; when one account hits quota or rate limits, clawgo automatically retries with the next logged-in account.
The WebUI can also start OAuth login, accept callback URL pasteback, confirm device-flow authorization, import `auth.json`, list accounts, refresh accounts, and delete accounts.

If you have both an `API key` and OAuth accounts for the same upstream, prefer configuring that provider as `auth: "hybrid"`:

- it uses `api_key` first
- when the API key hits quota/rate-limit style failures, it automatically falls back to the provider's OAuth account pool
- OAuth accounts still keep multi-account rotation, background pre-refresh, `auth.json` import, and WebUI management
- `oauth.cooldown_sec` controls how long a rate-limited OAuth account stays out of rotation; default is `900`
- the provider runtime panel shows current candidate ordering, the most recent successful credential, and recent hit/error history
- to persist runtime history across restarts, configure `runtime_persist`, `runtime_history_file`, and `runtime_history_max` on the provider

### 4. Start

Interactive mode:

```bash
clawgo agent
clawgo agent -m "Hello"
```

Gateway mode:

```bash
clawgo gateway run
```

Development mode:

```bash
make dev
```

WebUI:

```text
http://<host>:<port>/?token=<gateway.token>
```

## Architecture

Default collaboration flow:

```text
user -> main -> worker -> main -> user
```

ClawGo currently has four layers:

1. `main agent`
   user-facing entry, routing, dispatch, and merge
2. `local subagents`
   declared in `config.json -> agents.subagents`, with isolated sessions and memory namespaces
3. `node-backed branches`
   remote nodes mounted as controlled agent branches
4. `runtime store`
   persisted runtime, threads, messages, events, and audit data

## What It Is Good For

- 🤖 long-running local personal agents
- 🧪 multi-agent flows like `pm -> coder -> tester`
- 🌐 local control with remote node branches
- 🔍 systems that need strong observability, auditability, and recovery
- 🏭 teams that want agent config, prompts, tool permissions, and runtime policy managed as code

## Config Layout

There are now two configuration views:

- the persisted file still uses the raw structure shown below
- the WebUI and runtime-facing APIs prefer a normalized view:
  - `core`
  - `runtime`

Recommended raw structure:

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

Notes:

- `runtime_control` has been removed
- the current structure uses:
  - `agents.defaults.execution`
  - `agents.defaults.summary_policy`
  - `agents.router.policy`
- the WebUI now saves through the normalized schema first:
  - `core.default_provider`
  - `core.main_agent_id`
  - `core.subagents`
  - `runtime.router`
  - `runtime.providers`
- runtime panels now consume the unified `runtime snapshot / runtime live`
- enabled local subagents must define `system_prompt_file`
- remote branches require:
  - `transport: "node"`
  - `node_id`
  - `parent_agent_id`

See [config.example.json](/Users/lpf/Desktop/project/clawgo/config.example.json) for a full example.

## Node P2P

The remote node data plane supports:

- `websocket_tunnel`
- `webrtc`

It remains disabled by default. Node P2P is only enabled when `gateway.nodes.p2p.enabled=true` is set explicitly. In practice, start with `websocket_tunnel`, then switch to `webrtc` after validating connectivity.

For `webrtc`, these two fields matter:

- `stun_servers`
  - legacy-compatible STUN list
- `ice_servers`
  - the preferred structured format
  - may include `stun:`, `turn:`, and `turns:` URLs
  - `turn:` / `turns:` entries require both `username` and `credential`

Example:

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

Notes:

- when `webrtc` session setup fails, dispatch still falls back to the existing relay / tunnel path
- Dashboard, `status`, and `/api/nodes` expose the current Node P2P runtime summary
- a reusable public-network validation flow is documented in [docs/node-p2p-e2e.md](/Users/lpf/Desktop/project/clawgo/docs/node-p2p-e2e.md)

## MCP Server Support

ClawGo now supports `stdio`, `http`, `streamable_http`, and `sse` MCP servers through `tools.mcp`.

- declare each server under `config.json -> tools.mcp.servers`
- the bridge supports `list_servers`, `list_tools`, `call_tool`, `list_resources`, `read_resource`, `list_prompts`, and `get_prompt`
- on startup, ClawGo discovers remote MCP tools and registers them as local tools using the `mcp__<server>__<tool>` naming pattern
- with `permission=workspace` (default), `working_dir` is resolved inside the workspace and must remain within it
- with `permission=full`, `working_dir` may point to any absolute path including `/`, but access still inherits the permissions of the Linux user running `clawgo`

See the `tools.mcp` section in [config.example.json](/Users/lpf/Desktop/project/clawgo/config.example.json).

## Prompt File Convention

Keep agent prompts in dedicated files:

- `agents/main/AGENT.md`
- `agents/coder/AGENT.md`
- `agents/tester/AGENT.md`

Example:

```json
{
  "system_prompt_file": "agents/coder/AGENT.md"
}
```

Rules:

- the path must be relative to the workspace
- the repo does not ship these example files
- users or agent workflows should create the actual `AGENT.md` files

## Memory and Runtime

ClawGo does not treat all agents as one shared context.

- `main`
  - keeps workspace-level memory and collaboration summaries
- `subagent`
  - uses its own session key
  - writes to its own memory namespace
- `runtime store`
  - persists runs, events, threads, and messages

This gives you:

- better recovery
- better traceability
- clearer execution boundaries

## Best Fit

- developers building agent runtimes in Go
- teams that want visible multi-agent topology and internal collaboration streams
- users who need more than “chat + prompt”

If you want a working starting point, open [config.example.json](/Users/lpf/Desktop/project/clawgo/config.example.json) and run `make dev`.

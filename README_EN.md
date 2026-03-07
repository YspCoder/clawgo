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

### 3. Configure a provider

```bash
clawgo provider
```

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
http://<host>:<port>/webui?token=<gateway.token>
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

Recommended structure:

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
- enabled local subagents must define `system_prompt_file`
- remote branches require:
  - `transport: "node"`
  - `node_id`
  - `parent_agent_id`

See [config.example.json](/Users/lpf/Desktop/project/clawgo/config.example.json) for a full example.

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

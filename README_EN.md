# ClawGo 🦞

ClawGo is a long-running AI agent system built in Go for local-first operation, multi-agent coordination, and auditable runtime control.

- 🎯 `main agent` owns the user-facing loop, routing, dispatch, and merge
- 🤖 `subagents` handle concrete execution such as coding, testing, and docs
- 🌐 `node branches` let remote nodes appear as controlled agent branches
- 🧠 memory, threads, mailbox state, and task runtime are persisted
- 🖥️ a unified WebUI covers config, topology, logs, skills, memory, and ops

[中文](./README.md)

## Architecture

The default collaboration flow is:

```text
user -> main -> worker -> main -> user
```

ClawGo currently has four layers:

- `main agent`
  - user-facing entrypoint
  - handles routing, decomposition, dispatch, and merge
- `local subagents`
  - declared in `config.json -> agents.subagents`
  - use isolated sessions and memory namespaces
- `node-backed branches`
  - registered nodes appear as remote agent branches in the topology
  - `transport=node` tasks are sent via `agent_task`
- `runtime store`
  - persists runs, events, threads, and messages

## Core Capabilities

- 🚦 Routing and dispatch
  - `rules_first`
  - explicit `@agent_id`
  - keyword-based auto routing
- 📦 Persistent runtime state
  - `subagent_runs.jsonl`
  - `subagent_events.jsonl`
  - `threads.jsonl`
  - `agent_messages.jsonl`
- 📨 mailbox and thread coordination
  - dispatch
  - wait
  - reply
  - ack
- 🧠 dual-write memory model
  - subagents keep detailed local memory
  - main memory keeps compact collaboration summaries
- 🪪 declarative agent config
  - role
  - tool allowlist
  - runtime policy
  - `system_prompt_file`

## Quick Start

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

## WebUI

Open:

```text
http://<host>:<port>/webui?token=<gateway.token>
```

Key pages:

- `Agents`
  - unified agent topology
  - local subagents and remote branches
  - runtime status via hover
- `Config`
  - configuration editing
  - hot-reload field reference
- `Logs`
  - real-time logs
- `Skills`
  - install, inspect, and edit skills
- `Memory`
  - memory files and summaries
- `Task Audit`
  - execution and scheduling trace

### Highlights

**Dashboard**

![ClawGo Dashboard](docs/assets/readme-dashboard.png)

**Agent Topology**

![ClawGo Agent Topology](docs/assets/readme-agents.png)

**Config Workspace**

![ClawGo Config](docs/assets/readme-config.png)

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
- the current config uses:
  - `agents.defaults.execution`
  - `agents.defaults.summary_policy`
  - `agents.router.policy`
- enabled local subagents must define `system_prompt_file`
- remote branches require:
  - `transport: "node"`
  - `node_id`
  - `parent_agent_id`

See the full example in [config.example.json](/Users/lpf/Desktop/project/clawgo/config.example.json).

## Prompt File Convention

Keep agent prompts in dedicated files, for example:

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
- the file must live inside the workspace
- these paths are examples only; the repo does not ship the files
- users or agent workflows should create the actual `AGENT.md` files

## Memory and Runtime

ClawGo does not treat all agents as one shared context.

- `main`
  - keeps workspace-level memory and collaboration summaries
- `subagent`
  - uses its own session key
  - writes to its own memory namespace
- runtime store
  - persists runs, events, threads, and messages

This gives you:

- recoverability
- traceability
- clear execution boundaries

## Positioning

ClawGo is a good fit for:

- local long-running personal agents
- multi-agent workflows without heavyweight orchestration platforms
- systems that need explicit config, explicit audit trails, and strong observability

If you want a working starting point, begin with [config.example.json](/Users/lpf/Desktop/project/clawgo/config.example.json).

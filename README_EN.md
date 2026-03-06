# ClawGo

ClawGo is a long-running AI agent system written in Go, designed around:

- declarative main-agent and subagent configuration
- persistent run / thread / mailbox state
- node-registered remote agent branches
- multi-channel access plus a unified WebUI
- lightweight runtime behavior with strong auditability

[中文](./README.md)

---

## Current Architecture

ClawGo is no longer a "single agent plus some tools" runtime. It is now an orchestrated agent tree:

- `main agent`
  - user-facing entrypoint
  - owns routing, dispatch, merge, and coordination
- `local subagents`
  - for example `coder`, `tester`
  - declared in `config.json` under `agents.subagents`
- `node-backed branches`
  - a registered node is treated as a remote branch under `main`
  - branch ids look like `node.<node_id>.main`
  - they can be targeted by the main agent as controlled remote execution branches

The default collaboration pattern is:

```text
user -> main -> worker -> main -> user
```

---

## Core Capabilities

### 1. Agent Config and Routing

- `agents.router`
  - main-agent routing configuration
  - supports `rules_first`, explicit `@agent_id`, and keyword routing
- `agents.subagents`
  - declares agent identity, role, runtime policy, and tool allowlists
- `system_prompt_file`
  - subagents now prefer `agents/<agent_id>/AGENT.md`
  - they are no longer expected to rely on a one-line inline prompt

### 2. Persistent Subagent Runtime

Runtime state is persisted under:

- `workspace/agents/runtime/subagent_runs.jsonl`
- `workspace/agents/runtime/subagent_events.jsonl`
- `workspace/agents/runtime/threads.jsonl`
- `workspace/agents/runtime/agent_messages.jsonl`

This enables:

- run recovery
- thread tracing
- mailbox / inbox inspection
- dispatch / wait / merge workflows

### 3. Node Branches

A registered node is not treated as just a device entry anymore. It is modeled as a remote main-agent branch:

- nodes appear inside the unified agent topology
- remote registries are shown as read-only mirrors
- a subagent with `transport=node` is dispatched through `agent_task` to the target node

### 4. WebUI

The WebUI is now organized as a unified agent operations console, including:

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
  - unified agent topology
  - local and remote agent trees
  - subagent runtime
  - registry
  - prompt file editor
  - dispatch / thread / inbox controls

### 5. Memory Strategy

Memory is not fully shared and not fully isolated either. It uses a dual-write model:

- `main`
  - keeps workspace-level memory and collaboration summaries
- `subagent`
  - writes to its own namespaced memory
  - paths look like `workspace/agents/<memory_ns>/...`
- for automatic subagent summaries:
  - the subagent keeps its own detailed entry
  - the main memory also receives a short collaboration summary

Example summary format:

```md
## 15:04 Code Agent | Fix login API and add tests

- Did: Fixed the login API, added regression coverage, and verified the result.
```

---

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

### 4. Check status

```bash
clawgo status
```

### 5. Start

Local interactive mode:

```bash
clawgo agent
clawgo agent -m "Hello"
```

Gateway mode:

```bash
clawgo gateway
clawgo gateway start
clawgo gateway status
```

Foreground mode:

```bash
clawgo gateway run
```

---

## WebUI

Open:

```text
http://<host>:<port>/webui?token=<gateway.token>
```

Recommended pages:

- `Agents`
  - view the entire agent tree
  - inspect node branches
  - inspect active runtime tasks
  - edit local subagent configuration
- `Config`
  - edit runtime configuration
- `Logs`
  - inspect real-time logs
- `Task Audit`
  - inspect task decomposition, scheduling, and execution flow
- `EKG`
  - inspect error signatures, source distribution, and workload hotspots

---

## Config Layout

The current recommended structure is centered around:

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

Important notes:

- `runtime_control` has been removed
- the current structure uses:
  - `agents.defaults.execution`
  - `agents.defaults.summary_policy`
  - `agents.router.policy`
- enabled local subagents must define `system_prompt_file`
- node-backed agents use:
  - `transport: "node"`
  - `node_id`
  - `parent_agent_id`

See the full example:

- [config.example.json](/Users/lpf/Desktop/project/clawgo/config.example.json)

---

## Subagent Prompt Convention

Each agent should ideally have its own prompt file:

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

- the path must stay inside the workspace
- the path should be relative
- when creating a subagent, update both config and the matching `AGENT.md`
- when a subagent's responsibility changes materially, update its `AGENT.md` too

---

## Common Commands

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

## Build

Local build:

```bash
make build
```

Build all targets:

```bash
make build-all
```

Package outputs:

```bash
make package-all
```

---

## Design Direction

ClawGo currently leans toward these design choices:

- main-agent mediation first
- persisted runtime state first
- declarative config first
- unified operations UI before more aggressive automation
- nodes modeled as controlled remote agent branches, not fully autonomous peers

In practice, that means this project is closer to:

- a long-running personal AI agent runtime
- with multi-agent orchestration
- with node expansion
- with operational visibility and auditability

rather than a simple chat bot wrapper.

---

## License

See `LICENSE` in this repository.

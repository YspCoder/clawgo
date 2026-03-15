# ClawGo

**A long-running world runtime for game-like simulation.**

ClawGo is no longer positioned as a generic multi-agent shell. Its core model is now a **World Runtime** where `main` acts as the world mind, `npc` acts as autonomous characters, and structured state drives the simulation.

- **World mind**: `main` owns time, rules, arbitration, and rendering
- **Autonomous NPCs**: NPCs have persona, goals, memory, relations, and local perception
- **Structured world state**: world state, NPC state, and world events are persisted independently
- **Recoverable execution**: runtime and world state survive restart
- **Observable**: WebUI and APIs expose locations, NPCs, entities, quests, and recent events
- **Extensible**: low-level `providers`, `channels`, and node branches remain intact

[中文](./README.md)

## What It Is Now

The default runtime loop is:

```text
user -> main(world mind) -> npc/agent intents -> arbitrate -> apply -> render -> user
```

Roles:

- `main`
  - user entry
  - world event ingestion
  - NPC wake-up and dispatch
  - intent arbitration
  - world state updates
  - narrative rendering
- `npc`
  - proposes `ActionIntent`
  - never mutates world state directly
  - acts from persona, goals, memory, and visible events
- `world store`
  - persists world state, NPC state, event audit, and runtime records

In one line:

> **ClawGo is a world runtime, not just an agent chat shell.**

## Core Highlights

### 1. `main` is the world will

- `main` is not just an orchestrator
- every user message becomes a world event first
- final world truth is decided centrally by `main`

### 2. NPCs are autonomous

- each NPC has its own profile
- supports persona, traits, faction, home location, and default goals
- supports long-term goal behavior and event reactions
- supports delegation, messaging, and local perception

### 3. The world is structured

Core persisted files under `workspace/agents/runtime`:

- `world_state.json`
- `npc_state.json`
- `world_events.jsonl`
- `agent_runs.jsonl`
- `agent_events.jsonl`
- `agent_messages.jsonl`

### 4. The simulation loop is real

Current support includes:

- user input -> world event
- NPC perception -> intent generation
- `move / speak / observe / interact / delegate / wait`
- quest and resource progression
- runtime NPC creation
- map, occupancy, quest, entity, and recent-event visualization

### 5. The WebUI is already a GM console

- world snapshot
- map view
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

## Quick Start

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

Notes:

- OAuth credentials are stored locally
- available models are synced automatically
- multiple accounts per provider are supported
- WebUI can also start OAuth login, accept callback URL pasteback, confirm device flow, import `auth.json`, and manage accounts

If you have both an `API key` and OAuth accounts for the same upstream, use `auth: "hybrid"`:

- prefer `api_key`
- fall back to OAuth account pools on quota/rate-limit style failures
- keep account rotation, refresh, import, and runtime history

### 4. Start

Interactive mode:

```bash
clawgo agent
clawgo agent -m "I walk to the gate and check what the guard is doing."
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

## World Model

The runtime is organized around:

- `WorldState`
  - world clock
  - locations
  - global facts
  - entities
  - active quests
- `NPCState`
  - current location
  - short-term and long-term goals
  - beliefs
  - relationships
  - inventory/assets
  - private memory summary
- `WorldEvent`
  - user input
  - NPC actions
  - arbitration results
  - state change summaries
- `ActionIntent`
  - what an NPC wants to do
  - not the final world mutation itself

Core loop:

1. ingest
2. perceive
3. decide
4. arbitrate
5. apply
6. render

## Config Layout

Recommended config now centers on `agents.agents`:

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

Notes:

- the main config surface is now:
  - `agents.defaults.execution`
  - `agents.defaults.summary_policy`
  - `agents.agents`
- executable local agents usually define `prompt_file`
- world NPCs enter the world runtime through `kind: "npc"`
- remote branches still support:
  - `transport: "node"`
  - `node_id`
  - `parent_agent_id`
- normalized config and runtime APIs primarily expose:
  - `core.default_provider`
  - `core.agents`
  - `runtime.providers`

See [config.example.json](./config.example.json) for a complete example.

## Memory and Runtime

ClawGo does not treat every actor as one shared context.

- `main`
  - keeps world-level memory and summaries
- `agent / npc`
  - uses its own memory namespace or world decision context
- `runtime store`
  - persists tasks, events, messages, and world state

That gives you:

- better recovery
- better traceability
- clearer execution boundaries

## What It Is Good For

- autonomous NPC world simulation
- town / map / scene progression
- quest, resource, and entity interaction
- story-driven sandbox loops
- hybrid local control with remote node branches
- systems that need strong observability and recovery

## Node P2P

The low-level node data plane is still available:

- `websocket_tunnel`
- `webrtc`

It stays disabled by default. Enable it explicitly with `gateway.nodes.p2p.enabled=true`. In practice, validate with `websocket_tunnel` first, then switch to `webrtc`.

If you want a working starting point, open [config.example.json](./config.example.json) and run `make dev`.

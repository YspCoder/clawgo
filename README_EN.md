# ClawGo

A high-performance, long-running Go-native AI Agent with multi-platform builds and multi-channel messaging.

[中文](./README.md)

---

## What is ClawGo?

**ClawGo = single-binary gateway + channel integrations + tool calling + autonomy + auditable memory.**

Best for:
- Self-hosted AI assistants
- Continuous automation / inspections
- Multi-channel bots (Telegram/Feishu/Discord/...)
- Agent systems requiring control, traceability, and rollback safety

---

## Core Capabilities

- **Dual runtime modes**
  - `clawgo agent`: local interactive mode
  - `clawgo gateway`: service mode for long-running workloads

- **Multi-channel support**
  - Telegram / Feishu / Discord / WhatsApp / QQ / DingTalk / MaixCam

- **Tools & skills**
  - Built-in tool-calling and skill execution
  - Task orchestration support

- **Autonomy & task governance**
  - Session-level autonomy (idle budget, pause/resume)
  - Task Queue + Task Audit governance
  - Resource-key locking for conflict control

- **Memory & context governance**
  - `memory_search` and layered memory
  - Automatic context compaction
  - Startup self-check and task continuation

- **Reliability hardening**
  - Provider fallback (errsig-aware ranking)
  - Inbound/outbound dedupe protection
  - Better observability (provider/model/source/channel)

---

## EKG (Execution Knowledge Graph)

ClawGo includes a lightweight EKG (no external graph DB required):

- Event log: `memory/ekg-events.jsonl`
- Snapshot cache: `memory/ekg-snapshot.json`
- Normalized error signatures (path/number/hex denoise)
- Repeated-error suppression (configurable threshold)
- Historical provider scoring (including error-signature dimension)
- Memory linkage (`[EKG_INCIDENT]` structured notes for earlier suppression)
- WebUI time windows: `6h / 24h / 7d`

---

## Quick Start

### One-Click Install (install.sh)

- GitHub script link: <https://github.com/YspCoder/clawgo/blob/main/install.sh>
- One-click install command:

```bash
curl -fsSL https://raw.githubusercontent.com/YspCoder/clawgo/main/install.sh | bash
```

### 1) Initialize

```bash
clawgo onboard
```

### 2) Configure upstream model/proxy

```bash
clawgo login
```

### 3) Check status

```bash
clawgo status
```

### 4) Local mode

```bash
clawgo agent
clawgo agent -m "Hello"
```

### 5) Gateway mode

```bash
# register + enable systemd service
clawgo gateway
clawgo gateway start
clawgo gateway status

# foreground mode
clawgo gateway run
```

---

## WebUI

Access:

```text
http://<host>:<port>/webui?token=<gateway.token>
```

Main pages:
- Dashboard
- Chat
- Logs
- Skills
- Config
- Cron
- Nodes
- Memory
- Task Audit
- Tasks
- EKG

---

## Multi-Platform Build (Make)

### Build all default targets

```bash
make build-all
```

### Linux slim build (without disabling channels)

```bash
make build-linux-slim
```

Notes (Linux only):
- Keeps all channel capabilities while enabling `purego,netgo,osusergo` with `CGO_ENABLED=0` to reduce size and dynamic library coupling.
- Optionally combine with `COMPRESS_BINARY=1` (if `upx` is installed) for additional compression.

Default matrix:
- linux/amd64
- linux/arm64
- linux/riscv64
- darwin/amd64
- darwin/arm64
- windows/amd64
- windows/arm64

### Custom build matrix

```bash
make build-all BUILD_TARGETS="linux/amd64 linux/arm64 darwin/arm64 windows/amd64"
```

### Ultra-slim build (target <10MB)

```bash
make build COMPRESS_BINARY=1
```

Notes:
- Default build now uses `-trimpath -buildvcs=false -s -w` to remove path/symbol overhead.
- With `COMPRESS_BINARY=1`, the build will try `upx --best --lzma` for further executable compression.
- If `upx` is unavailable, build still succeeds and prints a warning.

### Package + checksums

```bash
make package-all
```

Outputs:
- `build/*.tar.gz` (Linux/macOS)
- `build/*.zip` (Windows)
- `build/checksums.txt`

---

## GitHub Release Automation

Built-in workflow: `.github/workflows/release.yml`

Triggers:
- tag push: `v*` (e.g. `v0.0.1`)
- manual dispatch (workflow_dispatch)

Pipeline includes:
- Multi-platform compilation
- Artifact packaging
- Checksum generation
- WebUI dist packaging
- GitHub Releases publishing

Example:

```bash
git tag v0.0.2
git push origin v0.0.2
```

---

## Common Commands

```text
clawgo onboard
clawgo login
clawgo status
clawgo agent [-m "..."]
clawgo gateway [run|start|stop|restart|status]
clawgo config set|get|check|reload
clawgo channel test ...
clawgo cron ...
clawgo skills ...
clawgo uninstall [--purge] [--remove-bin]
```

---

## Config & Hot Reload

- Supports `clawgo config set/get/check/reload`
- Strict JSON parsing (unknown fields fail fast)
- Auto rollback on failed hot reload

---

## Stability / Operations Notes

Recommended for production:
- tune channel dedupe windows
- keep heartbeat filtered in task-audit by default
- monitor EKG with 24h window baseline
- review Top errsig/provider score periodically

---

## License

See repository License file.

# ClawGo 🚀

A long-running AI Agent written in Go: lightweight, auditable, and multi-channel ready.

[中文](./README.md)

---

## What It Does ✨

- 🤖 Local chat mode: `clawgo agent`
- 🌐 Gateway service mode: `clawgo gateway`
- 💬 Multi-channel support: Telegram / Feishu / Discord / WhatsApp / QQ / DingTalk / MaixCam
- 🧰 Tool calling, skills, and sub-task collaboration
- 🧠 Autonomous tasks (queue, audit, conflict locks)
- 📊 WebUI for visibility (Chat / Logs / Config / Cron / Tasks / EKG)

---

## Concurrency Scheduling (New) ⚙️

- 🧩 A composite message is automatically split into sub-tasks
- 🔀 Within the same session: non-conflicting tasks run in parallel
- 🔒 Within the same session: conflicting tasks run serially (to avoid collisions)
- 🏷️ `resource_keys` are inferred automatically by default (no manual input needed)

---

## Memory + EKG Enhancements (New) 🧠

- 📚 Before each sub-task runs, related memory is fetched via `memory_search`
- 🚨 EKG + task-audit are used to detect repeated error-signature risks
- ⏱️ High-risk tasks include retry-backoff hints to reduce repeated failures

---

## Telegram Streaming Rendering (New) 💬

- 🧷 Stream flushes happen at syntax-safe boundaries to reduce formatting jitter
- ✅ A final convergence render is applied when streaming ends for stable layout

---

## 3-Minute Quick Start ⚡

### 1) Install

```bash
curl -fsSL https://raw.githubusercontent.com/YspCoder/clawgo/main/install.sh | bash
```

### 2) Initialize

```bash
clawgo onboard
```

### 3) Configure provider

```bash
clawgo provider
```

### 4) Check status

```bash
clawgo status
```

### 5) Start using it

Local mode:

```bash
clawgo agent
clawgo agent -m "Hello"
```

Gateway mode:

```bash
# Register + enable systemd service
clawgo gateway
clawgo gateway start
clawgo gateway status

# Or run in foreground
clawgo gateway run
```

---

## WebUI 🖥️

Open:

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

## Common Commands 📌

```text
clawgo onboard
clawgo provider
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

## Build & Release 🛠️

Build all default targets:

```bash
make build-all
```

Linux slim build:

```bash
make build-linux-slim
```

Custom target matrix:

```bash
make build-all BUILD_TARGETS="linux/amd64 linux/arm64 darwin/arm64 windows/amd64"
```

Package + checksums:

```bash
make package-all
```

Outputs:

- `build/*.tar.gz` (Linux/macOS)
- `build/*.zip` (Windows)
- `build/checksums.txt`

---

## GitHub Release Automation 📦

Built-in workflow: `.github/workflows/release.yml`

Triggers:

- Tag push (e.g. `v0.0.2`)
- Manual `workflow_dispatch`

Example:

```bash
git tag v0.0.2
git push origin v0.0.2
```

---

## Config Notes ⚙️

- Strict JSON validation (unknown fields fail fast)
- Supports hot reload: `clawgo config reload`
- Auto rollback if hot reload fails

---

## Production Tips ✅

- Enable channel dedupe windows
- Monitor Task Audit and EKG regularly
- Use gateway mode for long-running workloads

---

## License 📄

See `LICENSE` in this repository.

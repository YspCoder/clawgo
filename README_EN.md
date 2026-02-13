# ClawGo: High-Performance AI Agent (Linux Server Only)

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

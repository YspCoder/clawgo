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

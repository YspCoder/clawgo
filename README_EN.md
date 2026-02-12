**ClawGo** is a tiny but mighty AI assistant written in Go. Inspired by [nanobot](https://github.com/HKUDS/nanobot), it was refactored from the ground up to run on almost anything—from high-end servers to $10 RISC-V boards.

## 🚀 Why ClawGo?

- **🪶 Tiny Footprint**: <10MB RAM usage. Runs where Node.js and Python fear to tread.
- **⚡ Instant Boot**: Starts in <1 second. No heavy runtime warmup.
- **💰 Ultra-Low Cost**: Perfect for $10 SBCs like LicheeRV Nano or Orange Pi Zero.
- **🔌 Plug & Play**: Single binary. No complex dependencies.
- **🧩 Skill System**: Extend capabilities with `clawhub`, `coding-agent`, and more.
- **🔐 Easy Auth**: Interactive `login` command for OpenAI, Anthropic, Gemini, etc.

## 🏁 Quick Start

**1. Initialize**
```bash
clawgo onboard
```

**2. Configure Provider**
Interactively set up your API key (OpenAI, Anthropic, Gemini, Zhipu, etc.):
```bash
clawgo login
# Or specify provider directly:
# clawgo login openai
# clawgo login anthropic
# clawgo login gemini
```

**3. Chat!**
```bash
clawgo agent -m "Hello! Who are you?"
```

## 📦 Skills System

ClawGo isn't just a chatbot—it's an agent that can use tools.

**Manage Skills:**
```bash
# List installed skills
clawgo skills list

# List builtin skills
clawgo skills list-builtin

# Install a specific skill (e.g., weather)
clawgo skills install-builtin
```

**Featured Skills:**
- **coding-agent**: Run Codex/Claude for autonomous coding tasks.
- **healthcheck**: Security auditing and host hardening.
- **video-frames**: Extract frames from video using ffmpeg.
- **clawhub**: Manage skills from the community.

## 💬 Connect Channels

Run `clawgo gateway` to turn ClawGo into a 24/7 bot on your favorite platform.

| Channel | Status | Setup |
|---------|--------|-------|
| **Telegram** | ✅ Ready | Bot Token |
| **Discord** | ✅ Ready | Bot Token + Intents |
| **QQ** | ✅ Ready | AppID + AppSecret |
| **DingTalk** | ✅ Ready | Client ID + Secret |

*Configure channels in `~/.clawgo/config.json`.*

## 🛠️ Installation

### Build from Source
```bash
cd clawgo
make deps
make build
make install
```

## 📊 Comparison

| Feature | OpenClaw (Node) | NanoBot (Python) | **ClawGo (Go)** |
| :--- | :---: | :---: | :---: |
| **RAM Usage** | >1GB | >100MB | **< 10MB** |
| **Startup Time** | Slow (>5s) | Medium (>2s) | **Instant (<0.1s)** |
| **Binary Size** | N/A (Source) | N/A (Source) | **Single File (~15MB)** |
| **Architecture** | x86/ARM | x86/ARM | **x86/ARM/RISC-V** |

## 📜 License

MIT License. Free and open source forever. 🦐

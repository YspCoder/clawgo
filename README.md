# ClawGo: 极致轻量的 Go 语言 AI 助手

[English](./README_EN.md)

**ClawGo** 是一个用 Go 编写的小巧而强大的 AI 助手。受 [nanobot](https://github.com/HKUDS/nanobot) 启发，它从底层进行了重构，可以在几乎任何设备上运行——从高端服务器到 $10 的 RISC-V 开发板。

## 🚀 为什么选择 ClawGo?

- **🪶 极小占用**：内存占用 <10MB。在 Node.js 和 Python 无法运行的地方自如穿梭。
- **⚡ 瞬时启动**：启动时间 <1 秒。无需沉重的运行时预热。
- **💰 极低成本**：完美适配 LicheeRV Nano 或 Orange Pi Zero 等 $10 级别的单板机。
- **🔌 即插即用**：单二进制文件，无复杂依赖。
- **🧩 技能系统**：通过 `clawhub`、`coding-agent` 等技能扩展能力。
- **🔐 便捷认证**：交互式 `login` 命令，支持 OpenAI、Anthropic、Gemini 等主流服务商。

## 🏁 快速开始

**1. 初始化**
```bash
clawgo onboard
```

**2. 配置服务商**
交互式设置您的 API Key (OpenAI, Anthropic, Gemini, Zhipu 等)：
```bash
clawgo login
# 或者直接指定服务商：
# clawgo login openai
# clawgo login anthropic
# clawgo login gemini
```

**3. 开始聊天！**
```bash
clawgo agent -m "你好！你是谁？"
```

## 📦 技能系统 (Skills System)

ClawGo 不仅仅是一个聊天机器人，它是一个可以使用工具的智能体。

**管理技能：**
```bash
# 列出已安装的技能
clawgo skills list

# 列出内置技能
clawgo skills list-builtin

# 安装特定技能 (例如 weather)
clawgo skills install-builtin
```

**特色技能：**
- **coding-agent**: 运行 Codex/Claude 执行自主编程任务。
- **healthcheck**: 安全审计与主机加固。
- **video-frames**: 使用 ffmpeg 从视频中提取帧。
- **clawhub**: 管理社区提供的技能。

## 💬 连接频道 (Channels)

运行 `clawgo gateway` 让 ClawGo 在你最喜欢的平台上 24/7 在线。

| 频道 | 状态 | 配置方式 |
|---------|--------|-------|
| **Telegram** | ✅ 就绪 | Bot Token |
| **Discord** | ✅ 就绪 | Bot Token + Intents |
| **QQ** | ✅ 就绪 | AppID + AppSecret |
| **DingTalk** | ✅ 就绪 | Client ID + Secret |

*在 `~/.clawgo/config.json` 中配置频道。*

## 🛠️ 安装

### 预编译二进制文件
从 [发布页面](https://gitea.kkkk.dev/DBT/clawgo/releases) 下载适合您平台的固件 (Linux/macOS/Windows, x86/ARM/RISC-V)。

### 从源码编译
```bash
git clone https://gitea.kkkk.dev/DBT/clawgo.git
cd clawgo
make deps
make build
make install
```

## 📊 对比

| 特性 | OpenClaw (Node) | NanoBot (Python) | **ClawGo (Go)** |
| :--- | :---: | :---: | :---: |
| **内存占用** | >1GB | >100MB | **< 10MB** |
| **启动时间** | 较慢 (>5s) | 中等 (>2s) | **瞬时 (<0.1s)** |
| **二进制大小** | 无 (源码) | 无 (源码) | **单文件 (~15MB)** |
| **架构支持** | x86/ARM | x86/ARM | **x86/ARM/RISC-V** |

## 🤝 社区

加入讨论！
- **Discord**: [加入服务器](https://discord.gg/V4sAZ9XWpN)
- **Issues**: [GitHub Issues](https://gitea.kkkk.dev/DBT/clawgo/issues)

## 📜 许可证

MIT 许可证。永远免费开源。 🦐

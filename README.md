# ClawGo: 高性能 Go 语言 AI 助手 (Linux Server 专用)

[English](./README_EN.md)

**ClawGo** 是一个为 Linux 服务器量身定制的高性能 AI 助手。通过 Go 语言的并发优势与二进制分发特性，它能以极低的资源占用提供完整的 Agent 能力。

## 🚀 核心优势

- **⚡ 纯净运行**：专为 Linux 服务器环境优化，不依赖 Node.js 或 Python。
- **🏗️ 生产级稳定**：单二进制文件部署，完美集成到 systemd 等服务管理工具。
- **🔌 强制上游代理**：通过 [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) 统一管理模型配额与鉴权。
- **🧩 强力技能扩展**：内置 `coding-agent`、`github`、`context7` 等生产力工具。

## 🏁 快速开始

**1. 初始化**
```bash
clawgo onboard
```

**2. 配置 CLIProxyAPI**
ClawGo 强制要求使用 [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) 作为模型接入层。
```bash
clawgo login
```

**3. 开始运行**
```bash
# 交互模式
clawgo agent

# 后台网关模式 (支持 Telegram/Discord 等)
clawgo gateway
```

## 📦 迁移与技能

ClawGo 现在集成了原 OpenClaw 的所有核心扩展能力：
- **coding-agent**: 结合 Codex/Claude Code 实现自主编程。
- **github**: 深度集成 `gh` CLI，管理 Issue、PR 及 CI 状态。
- **context7**: 针对代码库与文档的智能上下文搜索。

## 🛠️ 安装 (仅限 Linux)

### 从源码编译
```bash
cd clawgo
make build
make install
```

## 📜 许可证

MIT 许可证。 🦐

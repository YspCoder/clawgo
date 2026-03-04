# ClawGo 🚀

一个 Go 写的长期运行 AI Agent：轻量、可审计、可多通道接入。

[English](./README_EN.md)

---

## 它能做什么 ✨

- 🤖 本地对话模式：`clawgo agent`
- 🌐 网关服务模式：`clawgo gateway`
- 💬 多通道接入：Telegram / Feishu / Discord / WhatsApp / QQ / DingTalk / MaixCam
- 🧰 工具调用、技能系统、子任务协同
- 🧠 自治任务（队列、审计、冲突锁）
- 📊 WebUI 可视化（Chat / Logs / Config / Cron / Tasks / EKG）

---

## 并发调度（新）⚙️

- 🧩 一条复合消息会先自动拆成多个子任务（可配置上限）
- 🔀 同一会话内：无资源冲突的子任务可并发执行
- 🔒 同一会话内：有资源冲突的子任务自动串行（避免互相踩）
- 🏷️ 默认会自动推断 `resource_keys`，无需手动填写

---

## 3 分钟上手 ⚡

### 1) 安装

```bash
curl -fsSL https://raw.githubusercontent.com/YspCoder/clawgo/main/install.sh | bash
```

### 2) 初始化

```bash
clawgo onboard
```

### 3) 配置模型

```bash
clawgo login
```

### 4) 看状态

```bash
clawgo status
```

### 5) 开始使用

本地模式：

```bash
clawgo agent
clawgo agent -m "Hello"
```

网关模式：

```bash
# 注册并启用 systemd 服务
clawgo gateway
clawgo gateway start
clawgo gateway status

# 或前台运行
clawgo gateway run
```

---

## WebUI 🖥️

访问地址：

```text
http://<host>:<port>/webui?token=<gateway.token>
```

主要页面：

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

## 常用命令 📌

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

## 构建与发布 🛠️

构建全部默认平台：

```bash
make build-all
```

Linux 瘦身构建：

```bash
make build-linux-slim
```

自定义平台矩阵：

```bash
make build-all BUILD_TARGETS="linux/amd64 linux/arm64 darwin/arm64 windows/amd64"
```

打包并生成校验：

```bash
make package-all
```

产物：

- `build/*.tar.gz`（Linux/macOS）
- `build/*.zip`（Windows）
- `build/checksums.txt`

---

## 自动发布（GitHub Release）📦

仓库内置 `.github/workflows/release.yml`。

触发方式：

- 推送 tag（如 `v0.0.2`）
- 手动触发 workflow_dispatch

示例：

```bash
git tag v0.0.2
git push origin v0.0.2
```

---

## 配置说明 ⚙️

- 配置文件严格 JSON 校验（未知字段会报错）
- 支持热更新：`clawgo config reload`
- 热更新失败会自动回滚备份

---

## 稳定运行建议 ✅

- 打开通道去重窗口（防重复消息）
- 定期看 Task Audit 和 EKG（查慢任务与高频错误）
- 长期运行推荐使用 `gateway` 服务模式

---

## License 📄

见仓库中的 `LICENSE` 文件。

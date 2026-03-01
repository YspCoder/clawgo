# ClawGo

高性能、可长期运行的 Go 原生 AI Agent（支持多平台构建与多通道接入）。

[English](./README_EN.md)

---

## 一句话介绍

**ClawGo = 单二进制网关 + 多通道消息 + 工具调用 + 自治执行 + 可审计记忆。**

适合：
- 私有化 AI 助手
- 持续巡检/自动任务
- 多通道机器人（Telegram/Feishu/Discord 等）
- 需要可控、可追踪、可回滚的 Agent 系统

---

## 核心能力

- **双模式运行**
  - `clawgo agent`：本地交互模式
  - `clawgo gateway`：服务化网关模式（推荐长期运行）

- **多通道支持**
  - Telegram / Feishu / Discord / WhatsApp / QQ / DingTalk / MaixCam

- **工具与技能体系**
  - 内置工具调用、技能安装与执行
  - 支持任务编排与子任务协同

- **自治与任务治理**
  - 会话级自治（idle 预算、暂停/恢复）
  - Task Queue / Task Audit 分层治理
  - 自治任务冲突锁（resource_keys）

- **记忆与上下文治理**
  - `memory_search` / 分层记忆
  - 自动上下文压缩
  - 启动自检与任务续跑

- **可靠性增强**
  - Provider fallback（含 errsig-aware 排序）
  - 入站/出站去重（防重复收发）
  - 审计可观测（provider/model/source/channel）

---

## EKG（Execution Knowledge Graph）

ClawGo 内置轻量 EKG（无需外部图数据库），用于降低重复错误与无效重试：

- 事件流：`memory/ekg-events.jsonl`
- 快照：`memory/ekg-snapshot.json`
- 错误签名归一化（路径/数字/hex 去噪）
- 重复错误抑制（可配置阈值）
- Provider fallback 历史打分（含错误签名维度）
- 与 Memory 联动（`[EKG_INCIDENT]` 结构化沉淀，支持提前拦截）
- WebUI 支持按 `6h/24h/7d` 时间窗口查看

---

## 快速开始

### 1) 初始化

```bash
clawgo onboard
```

### 2) 配置上游模型/代理

```bash
clawgo login
```

### 3) 查看状态

```bash
clawgo status
```

### 4) 本地模式

```bash
clawgo agent
clawgo agent -m "Hello"
```

### 5) 网关模式

```bash
# 注册并启用 systemd 服务
clawgo gateway
clawgo gateway start
clawgo gateway status

# 前台运行
clawgo gateway run
```

---

## WebUI

访问：

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

## 多平台构建（Make）

### 构建所有默认平台

```bash
make build-all
```

默认矩阵：
- linux/amd64
- linux/arm64
- linux/riscv64
- darwin/amd64
- darwin/arm64
- windows/amd64
- windows/arm64

### 自定义平台矩阵

```bash
make build-all BUILD_TARGETS="linux/amd64 linux/arm64 darwin/arm64 windows/amd64"
```

### 打包与校验

```bash
make package-all
```

输出：
- `build/*.tar.gz`（Linux/macOS）
- `build/*.zip`（Windows）
- `build/checksums.txt`

---

## GitHub Release 自动发布

已内置 `.github/workflows/release.yml`：

触发方式：
- 推送 tag：`v*`（如 `v0.0.1`）
- 手动触发（workflow_dispatch）

自动完成：
- 多平台编译
- 产物打包
- checksums 生成
- WebUI dist 打包
- 发布到 GitHub Releases

示例：

```bash
git tag v0.0.2
git push origin v0.0.2
```

---

## 常用命令

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

## 配置与热更新

- 支持 `clawgo config set/get/check/reload`
- 严格 JSON 解析（未知字段会报错）
- 配置热更新失败自动回滚备份

---

## 稳定性与审计建议

生产建议开启：
- 通道去重窗口配置
- task-audit heartbeat 默认过滤
- EKG 时间窗口观察（默认 24h）
- 定期查看 EKG Top errsig 与 provider 分数

---

## License

请参考仓库中的 License 文件。
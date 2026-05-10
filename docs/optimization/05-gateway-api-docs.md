# 05 Gateway API 文档

## 背景

Gateway 暴露了较多 WebUI/API endpoint，但目前主要靠 `pkg/api/server.go` 阅读理解。对前端、集成方和测试编写者来说，需要一份稳定的 API 文档。

## 目标

新增 Gateway API 文档，说明认证方式、核心接口、请求/响应样例、错误约定和常见调用场景。

## 建议改动范围

主要文件：

- 新增 `docs/API.md`
- 可选更新 `README.md` 或 `docs/PROJECT_OVERVIEW.md` 增加链接

参考文件：

- `pkg/api/server.go`
- `cmd/cmd_gateway.go`
- `config.example.json`

尽量避免修改：

- API handler 行为
- Gateway runtime 行为
- 前端资源或 WebUI 逻辑

## 文档建议结构

```text
# Gateway API

## 认证
## 基础 URL
## 健康检查
## Chat
## Chat History
## Live Chat / SSE
## Events
## Config
## Provider OAuth
## Provider Models / Runtime
## Weixin Login
## Upload
## Cron
## Skills
## Sessions
## Memory
## Workspace File
## Tools
## MCP Install
## Logs
## 错误响应
```

## 需要覆盖的接口

从当前代码至少覆盖：

```text
GET  /health
*    /api/config
*    /api/chat
*    /api/chat/history
*    /api/chat/live
*    /api/events/live
*    /api/version
*    /api/provider/oauth/start
*    /api/provider/oauth/complete
*    /api/provider/oauth/import
*    /api/provider/oauth/accounts
*    /api/provider/models
*    /api/provider/runtime
*    /api/weixin/status
*    /api/weixin/login/start
*    /api/weixin/login/cancel
GET  /api/weixin/qr.svg
*    /api/weixin/accounts/remove
*    /api/weixin/accounts/default
*    /api/upload
*    /api/cron
*    /api/skills
*    /api/sessions
*    /api/memory
*    /api/workspace_file
*    /api/tool_allowlist_groups
*    /api/tools
*    /api/mcp/install
*    /api/logs/live
*    /api/logs/recent
```

## 验收标准

- 新增 `docs/API.md`。
- 每个核心接口至少有用途、方法、认证、主要参数、响应示例。
- 文档说明 gateway token 的传递方式。
- 文档中明确哪些接口用于 SSE/live。
- 文档不声称尚不存在的行为。
- 纯文档任务无需修改源码。

## 测试建议

纯文档任务可不跑全量测试。建议至少用代码核对路由：

```bash
rg "HandleFunc|SetProtectedRoute" pkg/api/server.go
```

如果顺手补了 API 测试，则运行：

```bash
go test ./pkg/api
```

## 并行注意

本任务只写文档，适合和 Gateway 拆分并行执行。若 Gateway 拆分改动路由注册位置，最终合并时再核对一次 API 文档。

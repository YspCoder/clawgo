# 02 Gateway 结构拆分

## 背景

`cmd/cmd_gateway.go` 当前同时承担 gateway 命令解析、服务注册、runtime 组装、热重载、API handler 注入、channel 生命周期、cron/heartbeat/sentinel 编排、bootstrap 初始化等职责。文件过大，后续修改容易产生回归。

## 目标

在不改变行为的前提下拆分 Gateway 代码结构，让每类职责有清晰文件边界。

## 建议改动范围

主要文件：

- `cmd/cmd_gateway.go`
- 新增 `cmd/gateway_runtime.go`
- 新增 `cmd/gateway_reload.go`
- 新增 `cmd/gateway_services.go`
- 新增 `cmd/gateway_bootstrap.go`

可选文件：

- `cmd/reload_windows.go`
- `cmd/reload_unix.go`
- `cmd/signals_windows.go`
- `cmd/signals_unix.go`

尽量避免修改：

- `pkg/api/server.go`
- `pkg/agent/*`
- `pkg/providers/*`
- `pkg/channels/*`

## 建议拆分

### `cmd/cmd_gateway.go`

保留命令入口和高层流程：

- 参数分发。
- `gateway run/start/stop/restart/status` 分支。
- 前台 run 的主流程骨架。

### `cmd/gateway_runtime.go`

放运行时组装：

- `buildGatewayRuntime`
- `bindAgentLoopHandlers`
- `configureCronServiceRuntime`
- `buildHeartbeatService`
- `dispatchCronJob`
- `normalizeCronTargetChatID`

### `cmd/gateway_reload.go`

放热重载：

- config fingerprint。
- config watcher。
- reload trigger。
- runtimeSame 判断。
- reload 后重新绑定 API handler、channel、sentinel。

### `cmd/gateway_services.go`

放系统服务注册/控制：

- Linux systemd。
- macOS launchd。
- Windows scheduled task。
- PID file stop。

### `cmd/gateway_bootstrap.go`

放启动辅助任务：

- startup compaction check。
- bootstrap init。
- maximum permission policy。

## 验收标准

- 行为不变，命令仍可用：

  ```bash
  go run ./cmd gateway run --config ./config.json
  go run ./cmd gateway status --config ./config.json
  ```

- `go test ./...` 通过。
- `cmd/cmd_gateway.go` 明显变薄，职责聚焦于命令入口。
- 新文件命名和函数归属清晰。
- 没有引入循环依赖或跨 package 重构。

## 测试建议

```bash
go test ./cmd ./pkg/api ./pkg/channels ./pkg/agent
go test ./...
```

如果有本地配置，可额外手动验证：

```bash
go run ./cmd gateway run --config ./config.json
```

启动后修改配置文件，观察热重载日志是否仍正常。

## 并行注意

本任务应避免改 API 路由和 Provider 行为。若 API 文档任务并行进行，它只读 `pkg/api/server.go`，双方冲突较小。

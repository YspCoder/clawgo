# 06 Cron / Heartbeat / Sentinel 测试

## 背景

当前测试覆盖面较广，但长期运行服务相关模块还有明显空白，尤其是：

- `pkg/cron`
- `pkg/heartbeat`
- `pkg/sentinel`

这些模块负责定时任务、心跳和健康巡检，是常驻 Gateway 的可靠性基础。

## 目标

补充运行时服务测试，覆盖调度、持久化、失败退避、生命周期启停和巡检结果。

## 建议改动范围

主要文件：

- 新增 `pkg/cron/service_test.go`
- 新增 `pkg/heartbeat/service_test.go`
- 新增 `pkg/sentinel/service_test.go`

可选文件：

- `pkg/lifecycle/loop_runner_test.go`

尽量避免修改：

- `cmd/cmd_gateway.go`
- `pkg/agent/*`
- `pkg/providers/*`

如果为了可测试性需要小改生产代码，应保持非常小的改动，例如注入 clock、缩短 sleep、暴露只读状态。

## Cron 测试建议

覆盖：

- `AddJob` 后写入 store。
- `ListJobs(includeDisabled)` 行为。
- `EnableJob` 启停。
- `UpdateJob` 修改 schedule/message。
- cron expr 计算 next run。
- 失败后 backoff 和 consecutive failure。
- `MaxWorkers` 限制并发。
- 一次性任务 `DeleteAfterRun`。
- store 损坏或空文件时的行为。

## Heartbeat 测试建议

覆盖：

- disabled 时 `Start()` 不触发 heartbeat。
- enabled 时按 interval 调用回调。
- `Stop()` 后不再触发。
- `buildPrompt()` 使用自定义 prompt template。
- 空 Markdown 判断。
- ack token 提取。

## Sentinel 测试建议

覆盖：

- config 文件不存在/损坏时返回问题。
- memory 目录缺失时行为。
- logs 目录/文件检查。
- channel health check 汇总。
- alert callback 被调用。
- disabled 时不启动或不告警。

## 验收标准

- 新增三个模块的测试文件。
- 测试稳定，不依赖真实网络、真实外部平台或长时间 sleep。
- `go test ./pkg/cron ./pkg/heartbeat ./pkg/sentinel` 通过。
- `go test ./...` 通过。

## 测试建议

```bash
go test ./pkg/cron -count=1
go test ./pkg/heartbeat -count=1
go test ./pkg/sentinel -count=1
go test ./...
```

如果某个服务当前不易测试，优先做最小可测试性改造，而不是写依赖时间运气的测试。

## 并行注意

该任务大多新增测试文件，和结构重构冲突较小。若 Gateway 拆分同时进行，避免在本任务中修改 `cmd/`。

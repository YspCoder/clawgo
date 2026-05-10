# ClawGo 并行优化任务拆分

本文档把当前值得优化的方向拆成可并行执行的工作包。每个工作包都有相对独立的文件范围、交付物和验收标准，便于多人或多个 agent 同时推进。

## 工作包总览

| 编号 | 任务 | 主要目标 | 建议并行性 |
| --- | --- | --- | --- |
| 01 | 文档与编码清理 | 统一中文文档/示例/输出文本编码，提升可读性 | 可独立执行 |
| 02 | Gateway 结构拆分 | 拆薄 `cmd/cmd_gateway.go`，降低维护成本 | 可独立执行 |
| 03 | Provider 层模块化 | 拆分 Provider 协议、OAuth、runtime 状态逻辑 | 可独立执行，但需谨慎 |
| 04 | 工具注册架构优化 | 抽出工具启动器，清晰区分默认工具、可选工具、子 Agent 可见性 | 可独立执行 |
| 05 | Gateway API 文档 | 补充 API 认证、接口、请求响应样例 | 可独立执行 |
| 06 | Cron / Heartbeat / Sentinel 测试 | 补长期运行服务测试覆盖 | 可独立执行 |

## 推荐并行顺序

可以第一批同时启动：

- `01-docs-encoding-cleanup.md`
- `02-gateway-structure-split.md`
- `05-gateway-api-docs.md`
- `06-runtime-service-tests.md`

第二批建议在第一批基本稳定后启动：

- `03-provider-layer-modularization.md`
- `04-tool-bootstrap-refactor.md`

原因是 Provider 和工具注册都靠近 AgentLoop 核心，改动面更大，最好避开 Gateway 大拆分和文档编码清理的高频改动期。

## 并行开发约定

- 每个任务尽量只改自己文档中列出的主要文件。
- 如果必须跨任务修改同一文件，先在 PR/提交说明中明确说明原因。
- 不要顺手重构不在任务范围内的模块。
- 每个任务完成后至少运行 `go test ./...`，纯文档任务除外。
- 涉及行为变更时，优先补就近单元测试。

## 任务文件

- [01 文档与编码清理](./01-docs-encoding-cleanup.md)
- [02 Gateway 结构拆分](./02-gateway-structure-split.md)
- [03 Provider 层模块化](./03-provider-layer-modularization.md)
- [04 工具注册架构优化](./04-tool-bootstrap-refactor.md)
- [05 Gateway API 文档](./05-gateway-api-docs.md)
- [06 Cron / Heartbeat / Sentinel 测试](./06-runtime-service-tests.md)

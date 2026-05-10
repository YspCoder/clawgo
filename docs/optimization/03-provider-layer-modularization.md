# 03 Provider 层模块化

## 背景

`pkg/providers/http_provider.go` 当前聚合了通用 HTTP provider、Responses API、OpenAI-compatible Chat、Codex/Qwen/Kimi 兼容逻辑、OAuth runtime 状态、请求构造、响应解析和部分持久化状态逻辑。文件复杂度偏高，继续新增 Provider 会越来越难。

## 目标

在保持现有行为和测试通过的前提下，把 Provider 层按职责拆开，降低单文件复杂度。

## 建议改动范围

主要文件：

- `pkg/providers/http_provider.go`
- `pkg/providers/oauth.go`
- `pkg/providers/*_provider.go`
- 新增若干 provider 内部文件

建议新增文件：

- `pkg/providers/provider_registry.go`
- `pkg/providers/responses_adapter.go`
- `pkg/providers/openai_compat_adapter.go`
- `pkg/providers/provider_runtime.go`
- `pkg/providers/provider_request_options.go`

尽量避免修改：

- `pkg/agent/*`
- `pkg/tools/*`
- `cmd/*`
- `pkg/api/*`

## 建议拆分

### Provider 注册与路由

移动到 `provider_registry.go`：

- `CreateProvider`
- `CreateProviderByName`
- `normalizeProviderRouteName`
- `getProviderConfigByName`
- provider alias / route 相关逻辑

### Responses API 适配

移动到 `responses_adapter.go`：

- Responses request body 构造。
- Responses input item 转换。
- Responses tools 转换。
- Responses response 解析。
- Responses compact summary。

### OpenAI-compatible Chat 适配

移动到 `openai_compat_adapter.go`：

- chat completions request body 构造。
- multimodal message 转换。
- function call 转换。
- Qwen/Kimi 兼容 chat 格式相关公共逻辑。

### Provider runtime 状态

移动到 `provider_runtime.go`：

- runtime state 结构。
- history 持久化。
- health score。
- recent hits/errors/changes。
- candidate order。

### 请求选项与通用解析

移动到 `provider_request_options.go`：

- `rawOption`
- `stringOption`
- `mapOption`
- `stringSliceOption`
- `int64FromOption`
- `float64FromOption`
- 其他 request option helper。

## 验收标准

- `go test ./pkg/providers` 通过。
- `go test ./...` 通过。
- Provider 对外接口不变。
- 现有配置文件无需迁移。
- `http_provider.go` 明显缩小，并聚焦 `HTTPProvider` 本体。
- 没有改变已有 provider 的请求格式，除非测试明确覆盖并更新说明。

## 测试建议

重点跑：

```bash
go test ./pkg/providers -count=1
go test ./pkg/agent ./pkg/api ./cmd -count=1
go test ./...
```

Provider 层已有大量请求构造测试。拆分时应优先保持测试不动，让测试证明行为未变。

## 并行注意

该任务风险较高，建议不要和“新增 Provider”或“AgentLoop 工具调用逻辑改动”同时修改同一分支。若必须并行，先完成纯移动，再做行为改动。

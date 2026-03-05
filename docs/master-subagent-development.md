# 主 Agent + 子 Agent 架构开发文档

## 1. 目标与范围

本文档定义在 ClawGo 中实现“单一主 Agent 对话入口 + 可动态创建子 Agent 执行任务”的开发方案。

目标：

- 由主 Agent 统一接收用户对话并调度子 Agent。
- 子 Agent 可按职责分工（如 coding、docs、testing）。
- 子 Agent 具备各自独立记忆与会话上下文。
- 所有 Agent 共享同一工作空间（文件系统）。
- 支持主 Agent 在一次对话中创建、调用、监控子 Agent 并汇总结果。

非目标：

- 不在本阶段引入独立进程/独立容器隔离。
- 不在本阶段实现跨机器分布式调度。

---

## 2. 现状（基于当前代码）

已具备能力：

- `spawn` 工具可创建后台子任务。
- `subagents` 工具可 list/info/kill/steer/resume。
- `Orchestrator` + `pipeline_*` 具备任务依赖编排模型。

关键差距：

- 目前子任务偏“临时任务实例”，缺少“长期角色子 Agent”概念。
- `spawn.role` 参数未完整进入任务状态与调度决策。
- 子任务会话键未与 agent/task 强绑定，存在上下文串线风险。
- `pipeline_*` 工具已实现但尚未在主循环注册使用。
- 记忆目前以工作区全局为主，缺少“子 Agent 命名空间隔离”。

---

## 3. 总体架构

### 3.1 角色分层

- 主 Agent（Orchestrator Agent）
  - 职责：需求理解、任务拆分、分派、进度追踪、结果汇总、用户回复。
  - 不直接承载全部执行细节。
- 子 Agent（Worker Agent）
  - 职责：按角色执行具体任务（代码实现、文档编写、测试验证等）。
  - 具备独立的会话与记忆上下文。

### 3.2 共享与隔离策略

- 共享：workspace 文件系统、工具注册中心、pipeline shared_state。
- 隔离：子 Agent 的 session history、长期记忆、每日记忆。

### 3.3 运行模型

- 主 Agent 发起任务后，创建/复用子 Agent profile。
- 通过 `pipeline_create` 建立任务 DAG。
- 通过 `pipeline_dispatch` 调度 dependency-ready 任务到子 Agent。
- 子 Agent 完成后回填结果，主 Agent 汇总并回复用户。

---

## 4. 数据模型设计

## 4.1 SubagentProfile（新增，持久化）

建议存储位置：`workspace/agents/profiles/<agent_id>.json`

字段：

- `agent_id`: 全局唯一 ID（如 `coder`, `writer`, `tester`）。
- `name`: 展示名。
- `role`: 角色（coding/docs/testing/research...）。
- `system_prompt`: 角色系统提示词模板。
- `tool_allowlist`: 可调用工具白名单。
- `memory_namespace`: 记忆命名空间，默认同 `agent_id`。
- `status`: `active|disabled`。
- `created_at` / `updated_at`。

## 4.2 SubagentTask（扩展现有）

文件：`pkg/tools/subagent.go`

已有字段基础上强化：

- `Role`：落库并展示。
- `AgentID`：绑定具体子 Agent profile。
- `SessionKey`：固定记录实际执行 session key。
- `MemoryNamespace`：记录使用的记忆命名空间。

## 4.3 记忆目录结构（新增约定）

- 主 Agent（保持现状）：
  - `workspace/MEMORY.md`
  - `workspace/memory/YYYY-MM-DD.md`
- 子 Agent `<agent_id>`：
  - `workspace/agents/<agent_id>/MEMORY.md`
  - `workspace/agents/<agent_id>/memory/YYYY-MM-DD.md`

---

## 5. 工具与接口设计

## 5.1 现有工具接入调整

- `spawn`
  - 新增/强化参数：`agent_id`、`role`。
  - 优先级：`agent_id` > `role`（role 可映射默认 profile）。
- `subagents`
  - `list/info` 输出包含：`agent_id`、`role`、`session_key`、`memory_namespace`。

## 5.2 新增工具建议

- `subagent_profile`
  - action: `create|get|list|update|disable|enable|delete`
  - 用途：由主 Agent 在对话中动态创建/管理子 Agent。
- `subagent_dispatch`（可选）
  - 对单个 profile 下发任务，内部调用 spawn 并处理 profile 绑定。

## 5.3 Pipeline 工具注册

需在 `pkg/agent/loop.go` 中注册：

- `pipeline_create`
- `pipeline_status`
- `pipeline_state_set`
- `pipeline_dispatch`

使主 Agent 能直接调用现有编排能力。

---

## 6. 执行流程（主对话驱动）

1. 用户下达复合目标（如“先实现功能，再生成文档”）。
2. 主 Agent 判断是否已有对应 profile：
   - 无则 `subagent_profile.create`。
   - 有则复用。
3. 主 Agent 生成 pipeline 任务图（含依赖）。
4. 主 Agent 调用 `pipeline_dispatch` 派发可运行任务。
5. 子 Agent 执行并回填结果到任务状态/共享状态。
6. 主 Agent 轮询 `pipeline_status`，直到完成或失败。
7. 主 Agent 汇总结果，面向用户输出最终响应。

---

## 7. 会话与记忆隔离实现要点

## 7.1 Session Key 规范

禁止使用进程级固定 key。

建议格式：

- `subagent:<agent_id>:<task_id>`

效果：

- 同一子 Agent 不同任务可隔离上下文。
- 可追溯某次任务完整对话与工具链。

## 7.2 ContextBuilder 多命名空间支持

为 `MemoryStore` 增加 namespace 参数，按 namespace 读取不同路径：

- namespace=`main` -> 当前全局路径（兼容）。
- namespace=`<agent_id>` -> `workspace/agents/<agent_id>/...`

在子 Agent 执行入口按任务绑定 namespace 构建上下文。

## 7.3 Memory Tool 命名空间透传

`memory_search` / `memory_get` / `memory_write` 增加可选参数：

- `namespace`（默认 `main`）

子 Agent 默认注入自身 namespace，避免写入全局记忆。

---

## 8. 调度与并发控制

- 同会话调度仍受 `SessionScheduler` 约束。
- pipeline 维度使用依赖图保证顺序正确。
- 对写冲突资源建议引入 `resource_keys`（文件路径、模块名）串行化。
- 子 Agent 失败后可支持：
  - `resume`
  - 自动重试（指数退避）
  - 主 Agent 降级接管

---

## 9. 安全与治理

- 子 Agent 继承 `AGENTS.md` 安全策略。
- 外部/破坏性动作必须经主 Agent 显式确认。
- 子 Agent 工具权限建议最小化（allowlist）。
- 审计日志需记录：
  - 谁创建了哪个子 Agent
  - 子 Agent 执行了什么任务
  - 任务结果和错误

---

## 10. 实施顺序（不含工期）

### 阶段 A：打通主调度闭环

- 修正子任务 session key 规范化。
- 将 `Role`、`AgentID` 写入任务结构与 `subagents` 输出。
- 在主循环注册 `pipeline_*` 工具并验证端到端可调度。

### 阶段 B：子 Agent 记忆隔离

- 实现 `SubagentProfile` 持久化与管理工具。
- `MemoryStore` 增加 namespace。
- memory 系列工具支持 namespace。

### 阶段 C：稳定性与治理增强

- 增加资源冲突控制（resource keys）。
- 增加失败重试、超时、配额与审计维度。
- 增加 WebUI 可视化（profile/task/pipeline/memory namespace）。

---

## 13. 当前实现状态（代码已落地）

### 13.1 已完成

- 主循环已注册：
  - `subagent_profile`
  - `pipeline_create`
  - `pipeline_status`
  - `pipeline_state_set`
  - `pipeline_dispatch`
- `spawn` 已支持：
  - `agent_id`
  - `role`
  - `agent_id > role` 解析优先级
  - 当仅提供 `role` 时，按 role 自动映射已存在 profile（最近更新优先）
- 子任务会话隔离：
  - `session_key` 使用 `subagent:<agent_id>:<task_id>`
- 子任务记忆隔离：
  - `memory namespace` 已接入 `ContextBuilder` 与 `memory_search/get/write`
  - 子 Agent 运行时会自动注入其 namespace
- 子任务治理信息增强：
  - `subagents list/info/log` 可显示 `agent_id/role/session_key/memory namespace/tool allowlist`
- 安全治理：
  - `tool_allowlist` 已在执行期强制拦截
  - `parallel` 工具的内部子调用也会被白名单校验
  - `tool_allowlist` 已支持分组策略（`group:<name>` / `@<name>` / 组名直写）
  - disabled profile 会阻止 `spawn`
- 稳定性治理：
  - 子 Agent 已支持 profile 级重试/退避/超时/配额控制（`max_retries` / `retry_backoff_ms` / `timeout_sec` / `max_task_chars` / `max_result_chars`）
  - 子任务元数据与 system 回填中包含重试/超时信息，便于审计追踪
- WebUI 可视化：
  - 已提供 subagent profile 管理页
  - 已提供 subagent runtime 列表/详情/控制页（spawn/kill/resume/steer）
  - 已提供 pipeline 列表/详情/dispatch/创建入口

### 13.2 待继续增强

- （当前版本）无阻塞项；可继续按需增强：
  - allowlist 分组支持自定义组配置（当前为内置组）。
  - pipeline / subagent 运行态持久化与历史回放（当前为进程内实时视图）。

---

## 11. 验收标准

- 用户可通过自然语言要求主 Agent 创建指定角色子 Agent。
- 主 Agent 可在一次对话内完成“创建 -> 派发 -> 监控 -> 汇总”。
- 子 Agent 记忆互不污染，主 Agent 与子 Agent 记忆边界清晰。
- 同一 workspace 下可稳定并发执行多子任务，无明显上下文串线。
- 任务失败可追踪、可恢复，且审计信息完整。

---

## 12. 与现有代码的对应关系

- 子任务管理：`pkg/tools/subagent.go`
- 子任务工具：`pkg/tools/subagents_tool.go`
- 子任务创建：`pkg/tools/spawn.go`
- 流水线编排：`pkg/tools/orchestrator.go`
- 流水线工具：`pkg/tools/pipeline_tools.go`
- 主循环工具注册：`pkg/agent/loop.go`
- 上下文与记忆：`pkg/agent/context.go`, `pkg/agent/memory.go`
- memory 工具：`pkg/tools/memory*.go`

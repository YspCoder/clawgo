# EKG 设计稿（Execution Knowledge Graph）

> 目标：在不引入重型图数据库的前提下，为 ClawGo 提供“可审计、可回放、可降错”的执行知识图谱能力，优先降低 agent 重复报错与自治死循环。

## 1. 范围与阶段

### M1（本次实现）
- 记录执行结果事件（成功/失败/抑制）到 `memory/ekg-events.jsonl`
- 对错误文本做签名归一化（errsig）
- 在自治引擎中读取 advice：同任务同 errsig 连续失败达到阈值时，直接阻断重试（避免死循环）

### M2（后续）
- provider/model/tool 维度的成功率建议（preferred / banned）
- channel/source 维度的策略分层

### M3（后续）
- WAL + 快照（snapshot）
- WebUI 可视化（errsig 热点、抑制命中率）

---

## 2. 数据模型（接口草图）

```go
type Event struct {
    Time    string `json:"time"`
    TaskID  string `json:"task_id,omitempty"`
    Session string `json:"session,omitempty"`
    Channel string `json:"channel,omitempty"`
    Source  string `json:"source,omitempty"`
    Status  string `json:"status"` // success|error|suppressed
    ErrSig  string `json:"errsig,omitempty"`
    Log     string `json:"log,omitempty"`
}

type Advice struct {
    ShouldEscalate bool     `json:"should_escalate"`
    RetryBackoffSec int     `json:"retry_backoff_sec"`
    Reason         []string `json:"reason"`
}

type SignalContext struct {
    TaskID  string
    ErrSig  string
    Source  string
    Channel string
}
```

---

## 3. 存储与性能

- 存储：`memory/ekg-events.jsonl`（append-only）
- 读取：仅扫描最近窗口（默认 2000 行）
- 复杂度：O(N_recent)
- 设计取舍：M1 以正确性优先，后续再加入 snapshot 与索引

---

## 4. 规则（M1）

- 错误签名归一化：
  - 路径归一化 `<path>`
  - 数字归一化 `<n>`
  - hex 归一化 `<hex>`
  - 空白压缩
- 阈值规则：
  - 若 `task_id + errsig` 连续 `>=3` 次 error，则
  - `ShouldEscalate=true`，自治任务进入 `blocked:repeated_error_signature`

---

## 5. 接入点

1) `pkg/agent/loop.go`
- 在 `appendTaskAuditEvent` 处同步写入 EKG 事件（与 task-audit 同步）

2) `pkg/autonomy/engine.go`
- 在运行结果为 error 的分支读取 EKG advice
- 命中升级条件时，直接阻断重试并标记 block reason

---

## 6. 风险与回滚

- 风险：阈值过低导致过早阻断
- 缓解：默认阈值 3，且仅在同 task+同 errsig 命中时触发
- 回滚：移除 advice 判断即可恢复原重试路径

---

## 7. 验收标准（M1）

- 能生成并追加 `memory/ekg-events.jsonl`
- 相同任务在相同错误签名下连续失败 3 次后，自治不再继续循环 dispatch
- `make test`（Docker compile）通过

# M2 PostgreSQL Scheduler 与可靠性核心规格

**状态：** PROPOSED（等待 Shape 最终确认）  
**版本：** `skillgate.m2.scheduler.v1`  
**日期：** 2026-08-25（UTC）

> 本文件描述 M2 归档后 Scheduler/Lease/Retry/Result Commit capability 的完整目标行为。Runtime 验收项只来自 `brief.md` 的 A1–A10；本文件中的状态表、错误表和测试矩阵不是额外 A 项。

## 1. 目标与里程碑边界

M2 将 M1 编译出的配对 Trial Plan 物化到 PostgreSQL，并提供以下可靠性语义：

1. 多个 Scheduler 可以并发领取不同工作；
2. 每次领取都有不可复用的 Lease Token 和单调 Fence；
3. Worker 崩溃后，工作在 Lease 到期后按有界 Retry 恢复；
4. Completion 可以安全重发，但一个 Logical Trial 最多只有一个最终 Result；
5. Result、终态与 Experiment Counter 在同一事务中提交；
6. Experiment 取消会停止新 Claim，并从 `CANCEL_REQUESTED` 可观察地收敛到 `CANCELLED`；
7. 所有故障语义在真实 PostgreSQL 上验证。

M2 只提供 Go Control Plane 的可靠性核心、`skillgate` CLI 与确定性 Fixture/Fault Harness。Python/LangGraph Worker、Worker gRPC、REST API、真实 Model、Sandbox、Artifact Blob、Grading、Metric Aggregation 和 Release Gate 不在本里程碑实现。

## 2. 术语与身份模型

### 2.1 Pair、Logical Trial 与 Attempt

- **Pair**：M1 已冻结的匹配 Baseline/Candidate 比较单位；`pair_id` 包含 Case、Repetition 和非 Treatment Identity。
- **Logical Trial**：一个 Pair 中某个 Arm 的逻辑执行目标；Retry 不改变该身份。
- **Attempt**：Logical Trial 的一次至少一次执行尝试；Retry 会创建新的 Attempt。
- **Attempt Outcome**：一次 Attempt 的成功、失败、超时或取消记录；可重试失败不是最终 Logical Result。
- **Logical Result**：终结 Logical Trial 的唯一结果；一个 Logical Trial 最多一条，并且最多贡献一次 Experiment Counter。

### 2.2 稳定 ID

M2 沿用 M1 `trial_id` 语义并新增 Logical ID：

```text
logical_trial_identity = canonical_json({
  pair_id,
  arm
})
logical_trial_id = sha256(logical_trial_identity)

attempt_identity = canonical_json({
  pair_id,
  arm,
  attempt
})
trial_id = sha256(attempt_identity)
```

规则：

- `attempt` 从 1 开始，按 Logical Trial 单调递增；
- Retry 不改变 `pair_id`、`arm` 或 `logical_trial_id`；
- M1 生成的 Attempt 1 `trial_id` 必须保持不变；
- `(pair_id, arm)`、`logical_trial_id`、`(logical_trial_id, attempt)` 和 `trial_id` 分别受唯一约束保护；
- 绝对路径、数据库 ID、Worker ID、Lease Token、时间和运行顺序不得进入内容身份。

### 2.3 Result Idempotency

Result Manifest 先按版本化 Canonical JSON 计算 `result_manifest_hash`。Attempt 的幂等键为：

```text
result_idempotency_key = sha256(canonical_json({
  trial_id,
  result_manifest_hash
}))
```

提交语义：

- 同一 `trial_id` + 同一 `result_manifest_hash`：返回已保存的 Attempt Outcome 或 Logical Result；不得再次写入或计数；
- 同一 `trial_id` + 不同 `result_manifest_hash`：返回 `RESULT_CONFLICT`，不得覆盖；
- Logical Trial 已有最终 Result：其他 Attempt 的新 Completion 返回 `LOGICAL_TRIAL_TERMINAL`；
- 仅最终 Logical Result 更新 Experiment Counter；中间 Attempt Outcome 不计入聚合。

## 3. 状态模型

### 3.1 Experiment 状态

M2 负责以下执行阶段：

```text
COMPILED → QUEUED → RUNNING → GRADING
                 ↘ CANCEL_REQUESTED → CANCELLED
                 ↘ FAILED
```

- `COMPILED → QUEUED`：Experiment、Logical Trial 和 Attempt 1 在一个事务中物化完成；
- `QUEUED → RUNNING`：第一个 Attempt 成功 Claim；
- `RUNNING → GRADING`：所有 Logical Trial 已得到可交给后续 Grading 阶段的最终结果；M2 不执行 Grading；
- `QUEUED|RUNNING → CANCEL_REQUESTED`：收到首次取消请求；立即停止新 Claim；
- `CANCEL_REQUESTED → CANCELLED`：所有 Logical Trial 已终态收敛；
- `FAILED`：可靠性核心无法继续且没有合法 Retry，例如全部必要工作因预算或不可重试控制面错误终止。

`CANCEL_REQUESTED` 是活动中间态，不是终态。`GRADING`、`CANCELLED`、`FAILED` 在 M2 边界均不可逆。

### 3.2 Logical Trial 状态

```text
PENDING → LEASED → RUNNING → SUCCEEDED
   ↑          │        ├→ RETRY_WAIT → PENDING
   │          │        ├→ FAILED
   │          │        ├→ TIMED_OUT
   │          │        └→ CANCELLED
   └──────────┘
```

- `PENDING`：当前 Attempt 可在 `not_before` 后被领取；
- `LEASED`：Attempt 已分配但尚未开始；
- `RUNNING`：合法 Owner 已开始执行；
- `RETRY_WAIT`：上一个 Attempt 已终止，下一 Attempt 已创建但尚未到 `not_before`；
- `SUCCEEDED`、`FAILED`、`TIMED_OUT`、`CANCELLED`：Logical Trial 终态。

Logical Trial 终态不得回到活动状态。`TIMED_OUT` 只用于已耗尽 Retry 的时间边界；仍可重试的超时先记录 Attempt `TIMED_OUT`，Logical Trial 进入 `RETRY_WAIT`。

### 3.3 Attempt 状态

```text
PENDING → LEASED → RUNNING
                     ├→ SUCCEEDED
                     ├→ FAILED
                     ├→ TIMED_OUT
                     └→ CANCELLED
```

每个 Attempt 只 Claim 一次。Lease 到期后，该 Attempt 终结为 `TIMED_OUT`；若可重试，系统创建 `attempt+1` 的新 `PENDING` Attempt，而不是复用旧 Row 或旧 Token。

### 3.4 合法转换与竞态

所有转换必须：

- 在事务中锁定目标 Row；
- 校验期望前置状态；
- 使用 PostgreSQL 当前时间判断 Lease；
- 写入追加式 Transition Audit；
- 对同一请求的合法重发返回原结果；
- 对不满足前置条件的请求返回稳定错误且不部分写入。

取消与成功 Completion 竞态采用“先获得行锁者决定”规则：

- Completion 先提交：Logical Trial 保持 `SUCCEEDED`，之后的 Experiment Cancel 不删除或改写 Result；
- Cancel 先提交：Experiment 进入 `CANCEL_REQUESTED`，该 Attempt 只可提交 `CANCELLED` Outcome；新的成功 Completion 返回 `EXPERIMENT_CANCEL_REQUESTED`。

## 4. PostgreSQL Schema 与 Migration

### 4.1 Migration 机制

- 使用版本化 SQL Migration；Migration 随 Go Binary 嵌入；
- 实现选择为 `pressly/goose/v3`，PostgreSQL 访问使用 `jackc/pgx/v5`/`pgxpool`；
- `skillgate db migrate` 只执行向前 Migration；另提供只读 `db status`/`db version`；
- 生产式 CLI 不提供无保护的自动 Down；测试数据库可通过独立 Harness 重建；
- Migration 在空库可执行，重复运行无待执行版本时成功且无 Schema 漂移；
- Schema/Index 名称稳定，Migration 文件一旦归档不得改写；修正通过新版本完成。

数据库连接从参数或 `SKILLGATE_DATABASE_URL` 读取。CLI、日志、错误和测试输出不得打印 Credential 或完整 DSN。

### 4.2 M2 最小逻辑表

M2 至少实现：

- `experiments`：Manifest Hash、执行状态、取消时间、总 Logical Trial 数与各终态 Counter；
- `logical_trials`：Logical Identity、Pair/Arm、状态、Priority、Retry/Timeout Snapshot、当前 Attempt 和最终 Result 引用；
- `trial_attempts`：M1 `trial_id`、Attempt Number、`not_before`、Lease Owner、Token Hash、Fence Generation、Expiry、Heartbeat、Deadline 和 Attempt Outcome；
- `trial_results`：唯一 Logical Result、来源 Attempt、Manifest Hash、Idempotency Key、Outcome Snapshot；
- `trial_transition_events`：状态转换、Actor、Reason Code、UTC 时间与有限 JSON Detail；
- Goose 自有 Migration Version 表。

M2 不要求把 M1 文件系统 Skill/Suite Registry 搬入 PostgreSQL，也不创建 M3 Worker Registry、Artifact Blob、Grade、Metric 或 Release Decision 表。

### 4.3 关键约束

至少包含以下等价约束：

- `logical_trials(logical_trial_id)` 唯一；
- `logical_trials(experiment_id, pair_id, arm)` 唯一；
- `trial_attempts(trial_id)` 唯一；
- `trial_attempts(logical_trial_id, attempt_no)` 唯一且 `attempt_no >= 1`；
- `trial_results(logical_trial_id)` 唯一；
- `trial_results(attempt_id)` 唯一；
- `trial_results(idempotency_key)` 唯一；
- Lease 相关字段按状态成组存在或为空；
- Counter 非负且不超过 `total_logical_trials`；
- 外键和状态 Check Constraint 阻止孤立或非法行。

### 4.4 Claim 与 Expiry Index

Claim Query 使用针对待执行集合的 Partial Index，等价于：

```sql
CREATE INDEX trial_attempts_claim_idx
ON trial_attempts (priority DESC, not_before, created_at, trial_id)
WHERE status = 'PENDING';
```

Expiry Sweeper 使用：

```sql
CREATE INDEX trial_attempts_expiry_idx
ON trial_attempts (lease_expires_at, trial_id)
WHERE status IN ('LEASED', 'RUNNING');
```

Build 必须用代表性 Fixture 数据执行 `EXPLAIN (ANALYZE, BUFFERS)`，确认 Claim 使用目标 Index。性能结果是工程证据，不产生新的 Runtime A 项。

## 5. Trial Plan 物化

### 5.1 输入

物化输入来自 M1 的冻结编译结果，必须包含：

- Version/Manifest Hash；
- Pair ID、Arm、Repetition 与 M1 Attempt 1 `trial_id`；
- Priority；
- 最大 Attempt、Retry 分类与 Backoff 参数；
- 执行 Timeout/Budget Snapshot；
- 所有后续执行必须引用的不可变 Identity Hash。

M2 不重新解释 Manifest，不重新计算 Pair Treatment；调用 M1 领域类型或读取其版本化 JSON 输出，并重新校验 Hash 与数量。

### 5.2 原子性与幂等

一次物化事务必须：

1. 锁定或创建 Experiment Identity；
2. 校验相同 Manifest Hash 的既有请求；
3. 插入全部 Logical Trial；
4. 插入每个 Logical Trial 的 Attempt 1；
5. 设置准确的总数 Counter；
6. 将 Experiment 从 `COMPILED` 置为 `QUEUED`；
7. 提交后才向调用方返回成功。

相同 Experiment/Manifest/Plan 重发返回既有 Experiment；相同外部身份但 Body/Plan Hash 不同返回 `IDENTITY_CONFLICT`。任何一行失败时整个物化回滚，禁止部分 Queue。

## 6. Claim、Start 与 Heartbeat

### 6.1 Claim

Claim 必须在短事务中使用 `FOR UPDATE SKIP LOCKED`，只选择：

- Attempt 状态为 `PENDING`；
- `not_before <=` PostgreSQL 当前时间；
- 所属 Experiment 为 `QUEUED` 或 `RUNNING`；
- Logical Trial 仍为 `PENDING` 或 `RETRY_WAIT`；
- 未请求取消且预算未耗尽。

排序稳定为 `priority DESC, not_before, created_at, trial_id`。Claim 在同一事务中：

1. 锁定 Attempt 与 Logical Trial；
2. 再次检查状态；
3. 生成随机、不可预测、至少 128 bit 熵的 Lease Token；数据库只保存 Token Hash；
4. 将 Logical Trial 的 `lease_generation` 单调加一并把该值写入 Attempt；
5. 写入 Owner、Heartbeat 和 Expiry；
6. Attempt/Logical Trial 进入 `LEASED`；
7. 首次 Claim 时将 Experiment `QUEUED → RUNNING`；
8. 返回原始 Token、Fence Generation、Expiry 与执行 Identity。

Token 不进入日志或 Domain Event。Token 与 Owner 共同校验；Fence Generation 用于审计和阻断旧 Attempt。

### 6.2 Lease 时间

- Lease Duration 是有界配置，默认值与上下限由 CLI/配置统一校验；
- 服务器只使用 PostgreSQL 时间判定是否过期，不信任 Worker 时间戳；
- Heartbeat 续租设为 `database_now + lease_duration`，不在旧 Expiry 上累加；
- 新 Expiry 不得超过 Attempt Deadline 或 Experiment Budget Deadline；
- Worker 应在 Lease Duration 的三分之一以内 Heartbeat，但正确性不依赖该建议；
- 只要 `database_now >= lease_expires_at`，Lease 就失效。

### 6.3 Start 与 Heartbeat

合法 Owner 使用 `trial start` 将 Attempt/Logical Trial 从 `LEASED` 置为 `RUNNING`。合法 Heartbeat：

- 要求 `trial_id`、Worker ID、Token Hash、Fence Generation 与当前 Attempt 全部匹配；
- 仅接受 `LEASED` 或 `RUNNING`；
- 可携带单调递增的 Event Sequence 与有限 Phase；
- Event Sequence 回退返回冲突；相同 Sequence 的相同 Heartbeat 可幂等返回；
- Experiment 为 `CANCEL_REQUESTED` 时，不再续租，并返回带 `cancel_requested=true` 的稳定响应；
- Lease 已到期返回 `LEASE_EXPIRED`，不得复活 Attempt。

错误 Owner、Token、Fence 或状态不得修改 `heartbeat_at`、Expiry 或任何业务状态。

## 7. Expiry Sweeper 与 Retry

### 7.1 Expiry 回收

Sweeper 分批锁定已过期 `LEASED`/`RUNNING` Attempt，使用 `FOR UPDATE SKIP LOCKED` 避免多副本重复处理。每个回收事务：

1. 重新比较 PostgreSQL 当前时间与 Expiry；
2. 将旧 Attempt 终结为 `TIMED_OUT`，Reason 为 `WORKER_LOST` 或对应超时类别；
3. 若 Experiment 已 `CANCEL_REQUESTED`，Logical Trial 直接 `CANCELLED`，不 Retry；
4. 否则执行 Retry Classification、Attempt 上限和 Budget 检查；
5. 可重试时创建唯一的下一 Attempt，设置 `not_before`，Logical Trial 进入 `RETRY_WAIT`；
6. 不可重试或预算耗尽时写唯一 Logical Result，Logical Trial 进入 `TIMED_OUT` 或 `FAILED`，Counter 只更新一次。

Sweeper 可以安全重复运行；已经终结的 Attempt 不受影响。

### 7.2 Retry 分类

M2 冻结以下控制面类别：

| 类别 | 默认行为 |
|---|---|
| `PROVIDER_TRANSIENT` | 可重试 |
| `DEPENDENCY_TRANSIENT` | 可重试 |
| `WORKER_LOST` | 可重试 |
| `LEASE_TIMEOUT` | 可重试 |
| `INVALID_REQUEST` | 不可重试 |
| `PROTOCOL_INVALID` | 不可重试 |
| `POLICY_DENIED` | 不可重试 |
| `UNAUTHORIZED_REQUEST` | 不可重试 |
| `DETERMINISTIC_FAILURE` | 不可重试 |
| `BUDGET_EXHAUSTED` | 不可重试 |

Manifest/Strategy 可以在允许集合内进一步收窄可重试类别，但不得把不可重试安全/协议错误改为可重试，也不得允许无限 Attempt。

### 7.3 Backoff

Retry 使用有界 Exponential Backoff + Full Jitter：

```text
retry_number = next_attempt_no - 1
upper = min(backoff_cap, backoff_base * 2^(retry_number - 1))
delay = uniform_duration(0, upper)
not_before = database_now + delay
```

- `backoff_base`、`backoff_cap` 和 `max_attempts` 在物化时快照并校验上限；
- 随机源可注入，以便测试固定边界值；
- 计算结果和 Retry Reason 写入 Audit；
- Jitter/时间不进入 Pair、Logical Trial 或 Attempt Identity；
- 达到 `max_attempts` 或预算耗尽时不得创建下一 Attempt。

## 8. Result Commit

### 8.1 输入边界

Completion 至少包含：

- `logical_trial_id` 与 `trial_id`；
- Worker ID、Lease Token 和 Fence Generation；
- Result Manifest Version、Hash 和受大小限制的 Payload；
- Outcome（`SUCCEEDED`、`FAILED`、`TIMED_OUT` 或 `CANCELLED`）；
- 类型化 Failure/Retry Category；
- 最终 Event Sequence；
- 资源用量摘要（M2 只校验并保存，不做统计分析）。

所有字符串、JSON 和 Hash 均按不可信输入处理；未知字段策略、长度、枚举和 Canonical Hash 必须验证。M2 Fixture 不接收真实 Secret 或生产 Trace。

### 8.2 事务顺序

Committer 在一个事务中：

1. 按 `trial_id` 锁定 Attempt，并锁定 Logical Trial/Experiment；
2. 先检查是否已有相同 Attempt Outcome/Result，以处理合法重发；
3. 检查 Logical Trial 是否已有最终 Result；
4. 校验当前 Attempt、Owner、Token Hash、Fence、状态和最终 Event Sequence；
5. 使用 PostgreSQL 时间检查 `database_now < lease_expires_at`；等于 Expiry 视为过期；
6. 检查 Experiment Cancellation 与 Budget；
7. 校验并保存唯一 Attempt Outcome；
8. 对 Retryable Failure 创建下一 Attempt，或写入唯一 Logical Result；
9. 更新 Attempt/Logical Trial/Experiment 状态和唯一 Counter；
10. 写 Transition Audit；
11. 提交后返回版本化响应。

任何校验失败都整体回滚。事务提交前不发送外部通知。

### 8.3 严格过期 Fence

Lease 一旦到期，旧 Owner 的新 Heartbeat、Start 或 Completion 一律返回 `LEASE_EXPIRED`，即使 Sweeper 尚未运行、尚未创建下一 Attempt 或尚未被新 Owner Claim。不得提供“未重领即可恢复提交”的旁路。

如果同一 Completion 已在 Lease 有效时成功提交，但调用方未收到响应，则重发按已保存的 Result 幂等返回；这不是接受过期 Lease 的新提交。

### 8.4 Counter

Experiment 至少记录：

- `total_logical_trials`；
- `succeeded_count`；
- `failed_count`；
- `timed_out_count`；
- `cancelled_count`；
- `terminal_count`。

Counter 只在首次插入 Logical Result/首次终结 Logical Trial时更新。数据库约束与 Reconcile Query 必须能证明：

```text
terminal_count = succeeded + failed + timed_out + cancelled
terminal_count <= total_logical_trials
```

Reconcile 是只读比较或生成诊断，不通过直接改状态掩盖不一致。

## 9. Cancellation 与优雅关闭

### 9.1 Experiment Cancel

Cancel 是幂等事务：

1. 锁定 Experiment；
2. 已为 `CANCELLED` 时返回原结果；
3. 已进入不可取消终态时返回稳定状态冲突；
4. `QUEUED`/`RUNNING` 进入 `CANCEL_REQUESTED` 并记录 UTC 时间、Actor 和 Reason；
5. 所有未 Claim 的 `PENDING`/`RETRY_WAIT` Attempt 与 Logical Trial 立即 `CANCELLED`；
6. `LEASED`/`RUNNING` Attempt 标记 `cancel_requested_at`，停止续租；
7. 停止该 Experiment 的新 Claim；
8. 当所有 Logical Trial 终态后，Experiment 进入 `CANCELLED`。

合法 Owner 在有效 Lease 内可以提交 `CANCELLED` Outcome。若 Owner 不响应，Expiry Sweeper 将 Attempt/Logical Trial 终结为 `CANCELLED`，不创建 Retry。

### 9.2 Scheduler Shutdown

进程收到关闭信号后：

- 停止接受新的 CLI/Harness 调度循环；
- 停止发起新 Claim；
- 等待正在执行的短数据库事务在有界 Grace Period 内完成；
- 超时则取消 Context，使 PostgreSQL 回滚未提交事务；
- 不延长 Worker Lease、不把内存状态写成事实来源；
- 重启后仅根据 PostgreSQL 状态继续 Claim/Sweep。

## 10. CLI 与 Fixture/Fault Harness

### 10.1 CLI 原则

M2 延续 M1 的单一 `cmd/skillgate` Binary、默认人类可读输出和 `--json` 版本化输出。至少提供等价命令：

```text
skillgate db migrate
skillgate db status
skillgate experiment materialize
skillgate experiment cancel
skillgate trial claim
skillgate trial start
skillgate trial heartbeat
skillgate trial complete
skillgate scheduler sweep
```

命令名称可按现有 CLI 结构做不改变语义的调整，但 Build 必须在正式 Contract 中冻结最终帮助文本和 JSON Version。

所有可变操作支持 Context Timeout。Lease Token 只在 Claim 成功响应中返回，默认不显示在普通日志；后续命令通过受保护参数、stdin 或 Fixture 文件接收，禁止回显。

### 10.2 版本化结果

JSON 输出至少包含：

```json
{
  "version": "skillgate.cli.v1",
  "ok": true,
  "operation": "trial.claim",
  "data": {},
  "diagnostics": []
}
```

错误输出复用统一 Envelope，并使用稳定 Code。M2 最低错误集合：

| Code | 含义 |
|---|---|
| `DATABASE_UNAVAILABLE` | 无法连接 PostgreSQL |
| `MIGRATION_FAILED` | Migration 失败 |
| `IDENTITY_CONFLICT` | 同一外部身份对应不同内容 |
| `TRIAL_NOT_FOUND` | Attempt 不存在 |
| `NOT_CLAIMABLE` | 当前无合法工作或目标不可领取 |
| `OWNER_MISMATCH` | Worker Owner 不匹配 |
| `LEASE_MISMATCH` | Token/Fence 不匹配 |
| `LEASE_EXPIRED` | 数据库时间已达到 Lease Expiry |
| `STATUS_CONFLICT` | 状态前置条件不满足 |
| `RESULT_CONFLICT` | 同 Attempt 已保存不同 Result Hash |
| `LOGICAL_TRIAL_TERMINAL` | Logical Trial 已被其他 Attempt 终结 |
| `EXPERIMENT_CANCEL_REQUESTED` | 取消已先于成功 Completion 生效 |
| `RETRY_EXHAUSTED` | 无合法 Retry 配额 |
| `INVALID_ARGUMENT` | CLI 或 Payload 不合法 |

错误消息使用中文；协议字段、枚举与 Code 保留英文。数据库内部错误不得原样泄露 DSN、SQL、Token 或不可信 Payload。

### 10.3 Fixture/Fault Harness

Harness 使用 Synthetic Plan 和受控 Worker，不调用真实 Model。必须可确定性触发：

1. Claim 后进程退出；
2. 模拟 Model Call 完成但 Commit 前退出；
3. 相同 Completion 连续发送三次；
4. 多 Worker 并发 Claim；
5. Experiment 运行中 Cancel；
6. 旧 Lease 过期后迟到 Completion；
7. 数据库事务中途失败并验证整体回滚。

Harness 只能通过公开 Service/Store/CLI Contract 或专用测试边界操作；不得直接把数据库 Row 改成成功来伪造 Gate。测试准备可以插入过期 Fixture 时间，以避免长时间 sleep，但生产过期判断必须使用 PostgreSQL 时间。

## 11. 可观测性与审计

M2 不要求完整 OTel Collector，但每个操作必须生成结构化日志或 CLI 诊断字段：

- `operation`、`experiment_id`、`logical_trial_id`、`trial_id`；
- `attempt_no`、`lease_generation`、`worker_id`；
- `from_status`、`to_status`、`reason_code`；
- `result_manifest_hash`（存在时）；
- `duration_ms`、`database_error_class`；
- UTC `occurred_at` 与 Correlation ID。

不得记录 Lease Token、数据库密码、完整 DSN、原始 Model/Tool Payload 或生产数据。业务状态与 Audit Event 尽可能同事务提交；Telemetry/日志失败不得改变业务事务结论。

## 12. 测试与验收映射

| Brief 验收 | 主要证据 |
|---|---|
| A1 | 空库 Migration、重复 Up、Schema Constraint、`go test`/`go vet` |
| A2 | M1 Plan 物化、Identity Golden、重复/冲突物化、事务回滚 |
| A3 | 真实 PostgreSQL 多连接并发 Claim、唯一 Owner/Token/Fence、Claim `EXPLAIN` |
| A4 | Start/Heartbeat 正常与 Owner/Token/Fence/状态负向矩阵 |
| A5 | Expiry、Retry 分类、Full Jitter 边界、Attempt/Budget 耗尽 |
| A6 | Completion 三次重发、不同 Hash Conflict、Counter/Reconcile |
| A7 | Commit 前崩溃重发、严格 Expiry Fence、旧 Owner 竞态 |
| A8 | 两阶段 Cancel、Claim 停止、Worker 确认/Expiry 收敛、重复 Cancel、Shutdown |
| A9 | CLI Human/JSON/退出码 Golden 与 Fixture Harness Smoke |
| A10 | 六类真实 PostgreSQL Fault Gate、M0/M1 回归与文档核对 |

必须运行或提供等价命令：

```text
go test ./...
go test -race ./...
go vet ./...
go run ./cmd/skillgate --help
# 真实 PostgreSQL integration/fault test（由 Build 冻结最终命令）
npm run validate:m0
```

如果没有可用 PostgreSQL，A1、A3–A8 和 A10 保持未验证，不能用 SQL Mock、SQLite 或内存 Store 宣称通过。

## 13. 正式文档同步要求

Build 完成时必须同步：

- `docs/architecture/execution-lifecycle.md`：新增 Logical Trial/Attempt、严格 Fence 和两阶段取消；
- `docs/architecture/storage-and-data.md`：将 M2 实际 Schema/Index 从临时设计更新为已验证行为；
- `docs/contracts/experiment-manifest.md`：明确新增 Logical ID，并保留 M1 `trial_id` Attempt 语义；
- CLI Contract/运行文档：冻结 Migration、M2 命令、错误和真实 PostgreSQL 启动方式；
- ADR：记录 M2 身份、Fencing、取消与迁移选择，不能只改实现；
- `README.md`、`PROJECT_STATUS.md`：诚实记录 M2 已实现证据与未实现的 Worker/REST/gRPC/Model/Sandbox/Grading；
- 不修改 M0/M1 归档产物。

## 14. 规范来源

- PostgreSQL `SELECT` / `FOR UPDATE SKIP LOCKED` 官方文档（数据库官方文档）：https://www.postgresql.org/docs/current/sql-select.html
- PostgreSQL `INSERT ... ON CONFLICT` 官方文档（数据库官方文档）：https://www.postgresql.org/docs/current/sql-insert.html
- PostgreSQL Transaction Isolation 官方文档（数据库官方文档）：https://www.postgresql.org/docs/current/transaction-iso.html
- `pgx/v5` 与 `pgxpool` 文档（Go 开源驱动官方包文档）：https://pkg.go.dev/github.com/jackc/pgx/v5 和 https://pkg.go.dev/github.com/jackc/pgx/v5/pgxpool
- `goose/v3` 文档（Go 开源 Migration 工具官方包文档）：https://pkg.go.dev/github.com/pressly/goose/v3
- Exponential Backoff and Jitter（AWS 官方工程指南）：https://aws.amazon.com/builders-library/timeouts-retries-and-backoff-with-jitter/

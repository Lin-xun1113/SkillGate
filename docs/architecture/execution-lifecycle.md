# 执行生命周期

**状态：** `PROVISIONAL → M2`；2026-08-25（UTC）Build 候选，等待 Native Verify

## 1. Experiment 生命周期

```text
DRAFT
  → VALIDATING
  → COMPILED
  → QUEUED
  → RUNNING
  → GRADING
  → AGGREGATING
  → DECIDED

终态：CANCELLED / FAILED / INCOMPLETE
```

### 状态转换规则

- `DRAFT → VALIDATING`：提交 Manifest。
- `VALIDATING → COMPILED`：Identity、Case、Policy 和 Budget 全部校验通过。
- `COMPILED → QUEUED`：Trial Row 和 Pair ID 在同一个事务中提交。
- `QUEUED → RUNNING`：第一次成功获取 Lease。
- `RUNNING → GRADING`：必需的 Execution Evidence 存在，或所有终止失败已经记录。
- `GRADING → AGGREGATING`：所有必需 Grade 完成，或被明确标记为 Incomplete。
- `AGGREGATING → DECIDED`：Metric 和 Release Policy Snapshot 提交完成。
- 任意活动状态都可以通过 API 变成 `CANCELLED`；Worker 会收到 Cancellation Signal。
- 缺失必需 Evidence 时进入 `INCOMPLETE`，不能静默当作 0。

## 2. Logical Trial 与 Attempt 生命周期

```text
Logical Trial:
PENDING → LEASED → RUNNING
                     ├→ RETRY_WAIT → PENDING
                     ├→ SUCCEEDED
                     ├→ FAILED
                     ├→ TIMED_OUT
                     └→ CANCELLED

Attempt:
PENDING → LEASED → RUNNING → SUCCEEDED|FAILED|TIMED_OUT|CANCELLED
```

M2 使用 `logical_trial_id=sha256({pair_id,arm})` 表示 Retry 组，保留 M1 `trial_id=sha256({pair_id,arm,attempt})` 作为 Attempt ID。Retry 创建新的 Attempt，不改变 Pair/Logical Trial Identity；一个 Logical Trial 最多一条最终 Result，并只更新一次 Experiment Counter。M2 不声称 Exactly-once Execution。

## 3. Lease 算法

第一版 Scheduler 使用 PostgreSQL Transaction：

```sql
BEGIN;
WITH next_trial AS (
  SELECT id
  FROM trials
  WHERE status = 'PENDING'
    AND not_before <= now()
  ORDER BY priority DESC, created_at, id
  FOR UPDATE SKIP LOCKED
  LIMIT 1
)
UPDATE trials t
SET status = 'LEASED',
    lease_owner = $worker_id,
    lease_expires_at = now() + $lease_duration,
    heartbeat_at = now(),
    updated_at = now()
FROM next_trial n
WHERE t.id = n.id
RETURNING t.*;
COMMIT;
```

`FOR UPDATE` 防止被选中的 Attempt Row 被并发更新；`SKIP LOCKED` 允许并发 Worker 领取不同的 Attempt，而不必等待已经被锁住的任务。Lease Token 以 Hash 保存，Lease Generation 作为 Fence 单调递增；具体查询和 Index 必须在目标 PostgreSQL 版本和代表性数据量下进行 Benchmark。

## 4. Heartbeat 与过期处理

Worker Heartbeat 至少包含：

- Trial ID；
- Lease Token；
- Worker ID；
- 已处理的 Event Sequence；
- 当前 Phase；
- 时间戳。

Server 只有在 Lease Token 匹配时才接受 Heartbeat。Expiry Sweeper 将过期的 `LEASED`/`RUNNING` Trial 按 Retry Policy 移到 `RETRY_WAIT` 或 `FAILED`。

Lease 过期后的 Worker Completion 一律返回 `LEASE_EXPIRED`，即使尚未被新 Worker 重新领取。只有 Lease 有效期间已经提交但响应丢失的同一 Completion 才返回已保存 Result。

## 5. Result Commit Protocol

1. Worker 上传 Artifact 并计算 Hash。
2. Worker 携带 Lease Token、Result Idempotency Key、Artifact Manifest、Grade Observation 和最终 Event Sequence 发送 `CompleteTrial`。
3. Go 在事务中锁定 Trial。
4. 校验 Lease/Owner、Identity Tuple、Artifact 存在性与 Hash，以及 Budget Accounting。
5. 如果 Trial 已经是 `SUCCEEDED`，且 Idempotency Key 匹配，则返回已保存的 Result；否则返回 Conflict。
6. 插入 Result/Grade Row 和 Artifact 引用。
7. 将 Trial 标记为终态，并只更新一次 Experiment Counter。
8. 提交事务。

事务提交之前不能发送外部通知。

## 6. Cancellation

Cancellation 采用协作式流程：

1. CLI/后续 API 将 Experiment 标记为 `CANCEL_REQUESTED`。
2. Scheduler 停止领取新任务。
3. Worker 通过 Heartbeat 响应或 Control Stream 收到取消通知。
4. Worker 取消 LangGraph/Agent Context 和 Sandbox Process。
5. Worker 上传状态为 `cancelled` 的部分 Evidence。
6. Server 校验 Owner 后将 Attempt/Logical Trial 设为 `CANCELLED`；Lease 到期的运行任务由 Sweeper 以 `CANCELLED` 收敛。
7. 所有 Logical Trial 终态后，Experiment 进入 `CANCELLED`。

强制 Timeout 可以杀死 Sandbox，但仍必须生成类型化的 Timeout Result。

## 7. Retry 分类

可重试示例：

- Provider 临时返回 429/5xx；
- Worker Process 中断；
- Object Store 或网络临时错误；
- Scheduler Lease 过期。

不可重试示例：

- Skill Manifest 无效；
- Deterministic Verifier 失败；
- Policy Denial；
- 未授权的 Credential 或 Tool Request；
- Worker Protocol 格式错误；
- Budget 已耗尽。

Retry 不能改变原始 Pair Identity，只能在同一个 Logical Trial 下增加 Attempt 记录。

## 8. LangGraph Worker 阶段

Python Worker Graph 应包含明确的 Node：

```text
validate_request
  → prepare_context
  → mount_skill_and_fixtures
  → invoke_agent
  → collect_outputs
  → run_deterministic_graders
  → run_optional_llm_graders
  → finalize_trace
```

LangGraph Checkpoint 可以支持单个 Worker Run 内部的中断与恢复，但不能替代 Go Trial Lease 或 Result State。恢复后的 Graph 必须保留相同的 `trial_id`、Request Hash、Skill Identity 和 Environment Identity。

## 9. Replay

Replay 是新的 Experiment 或诊断 Run，不能修改历史 Evidence。它必须记录：

- 来源 Trial/Result；
- Replay 原因；
- Model Call 是 Live 还是 Fixture-backed；
- 改变了哪些字段；
- 是否仍可与原 Experiment 直接比较。

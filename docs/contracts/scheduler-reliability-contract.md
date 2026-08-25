# M2 Scheduler 与可靠性 Contract

**状态：** `PROVISIONAL`（2026-08-25 UTC Build 候选；等待 Native Verify）  
**版本：** `skillgate.scheduler.v1`  
**范围：** PostgreSQL Store、Scheduler、Lease、Retry、Result Commit、M2 CLI

## 1. 身份与至少一次执行

```text
logical_trial_id = sha256(canonical_json({pair_id, arm}))
trial_id         = sha256(canonical_json({pair_id, arm, attempt}))
result_key       = sha256(canonical_json({trial_id, result_manifest_hash}))
```

`pair_id`、Arm 和 Logical Trial Identity 在 Retry 中不变；Attempt 从 1 开始递增。M2 不声明 Exactly-once Execution。执行尝试可以重复，但一个 Logical Trial 只能有一个最终 Result 并贡献一次 Counter。

## 2. CLI

```text
skillgate db migrate|status [--database-url <url>] [--json]
skillgate experiment materialize <manifest> [--max-attempts <n>] [--trial-timeout <duration>] [--budget-timeout <duration>] [--json]
skillgate experiment cancel --experiment-id <id> [--actor <id>] [--reason <text>] [--json]
skillgate trial claim --worker-id <id> [--lease-duration <duration>] [--json]
skillgate trial start --trial-id <id> --worker-id <id> --lease-token-file <path> --lease-generation <n> [--json]
skillgate trial heartbeat ... [--lease-duration <duration>] [--json]
skillgate trial complete --logical-trial-id <id> --trial-id <id> --worker-id <id> --lease-token-file <path> --lease-generation <n> --result-manifest <path> --outcome <outcome> [--category <category>] [--json]
skillgate scheduler sweep [--limit <n>] [--json]
```

- `SKILLGATE_DATABASE_URL` 可替代 `--database-url`；Credential 不得写入仓库；
- Lease Token 只从 Claim 响应取得，并通过受保护文件传给后续 CLI；普通输出、日志和错误不得回显 Token；
- `--json` 使用 `skillgate.cli.v1`，成功输出 `operation`、`data` 和 `diagnostics`；
- 错误使用稳定 Code 与非零退出码，不泄露 SQL、DSN 或不可信 Payload。

## 3. 状态与 Lease

Experiment：`COMPILED → QUEUED → RUNNING → GRADING`，取消为 `CANCEL_REQUESTED → CANCELLED`。  
Logical Trial：`PENDING → LEASED → RUNNING → RETRY_WAIT/PENDING/终态`。  
Attempt：`PENDING → LEASED → RUNNING → SUCCEEDED|FAILED|TIMED_OUT|CANCELLED`。

Claim 使用 PostgreSQL 事务和 `FOR UPDATE SKIP LOCKED`：返回 Worker、Attempt、Lease Token、Fence Generation、UTC Expiry 和 Deadline。数据库时间达到 Expiry（相等也算过期）后，旧 Token 的 Start、Heartbeat、Completion 一律返回 `LEASE_EXPIRED`。

Heartbeat 必须同时匹配 Worker、Token Hash、Fence、状态和非递减 Event Sequence。Experiment 为 `CANCEL_REQUESTED` 时停止续租并返回取消信号。

## 4. Retry 与 Expiry

默认控制面可重试类别：`PROVIDER_TRANSIENT`、`DEPENDENCY_TRANSIENT`、`WORKER_LOST`、`LEASE_TIMEOUT`。无效请求、协议错误、Policy Denied、未授权请求、确定性失败和预算耗尽不可重试。

使用有界 Exponential Backoff + Full Jitter；参数在物化时快照，Attempt 上限 30，Jitter 不进入 Identity。M2 同时快照 Experiment 时间预算：预算到期后停止 Claim，Sweeper 将 Pending/Leased/Running Logical Trial 终结为 `FAILED/BUDGET_EXHAUSTED`，不创建 Retry。Expiry Sweeper 将旧 Attempt 终结，再创建唯一下一 Attempt；取消中的过期 Attempt 终结为 `CANCELLED`，不创建 Retry。Token/Cost Budget 留给 Worker/Metric 里程碑。

## 5. Result Commit

Completion 在一个事务内锁定 Attempt、Logical Trial 和 Experiment，校验 Identity/Owner/Token/Fence/Expiry/Outcome，保存受大小限制的 Synthetic Result Manifest，写入最终 Result/Attempt 状态/Counter 和 Audit Event。

- 同一 Attempt + 同一 Result Hash：返回原 Result，`idempotent=true`；
- 同一 Attempt + 不同 Result Hash：`RESULT_CONFLICT`；
- Logical Trial 已有最终 Result：`LOGICAL_TRIAL_TERMINAL`；
- Lease 到期：`LEASE_EXPIRED`，不写入、不覆盖、不计数；
- 事务失败整体回滚，提交前不发送外部通知。

## 6. 验证命令

```bash
go test ./...
go test -race ./...
go vet ./...
TEST_DATABASE_URL='postgres://...?...' go test -count=1 ./internal/store/postgres
npm run validate:m0
```

真实 PostgreSQL 是 M2 Gate；SQL Mock、SQLite 或内存 Store 不能替代 Claim/Lease/事务/唯一约束测试。

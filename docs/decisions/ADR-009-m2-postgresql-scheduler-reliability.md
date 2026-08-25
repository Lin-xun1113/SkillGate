# ADR-009：M2 使用 PostgreSQL Queue/Lease 与严格幂等提交

**状态：** `ACCEPTED`（M2 Shape 已确认；实现状态以 Native Verify 为准）  
**日期：** 2026-08-25（UTC）  
**决策者：** SkillGate 项目负责人  
**影响范围：** M2 Scheduler、Lease、Retry、Result Commit、CLI、PostgreSQL Schema

## 1. 背景

M1 已生成不可变的 Pair/Trial Plan，但尚未提供可靠执行生命周期。M2 必须证明 Worker 崩溃、并发领取、Lease 过期、重复 Completion、取消和数据库事务失败不会让同一个 Logical Trial 重复计分。现有文档还没有统一 Logical Trial 与 Attempt 的主键边界、迟到提交策略和取消中间态。

项目已通过 ADR-002 决定在引入 Broker 前使用 PostgreSQL Queue/Lease。M2 需要把该决策落实为可测试的内部 Go Contract，而不是提前引入 REST、Worker gRPC 或 Kafka/NATS。

## 2. 决策

1. PostgreSQL 是 Experiment、Logical Trial、Attempt、Lease、Result 和 Counter 的事实来源；Claim 使用事务与 `FOR UPDATE SKIP LOCKED`。
2. M2 新增 `logical_trial_id = sha256(canonical_json({pair_id, arm}))`；保留 M1 `trial_id = sha256(canonical_json({pair_id, arm, attempt}))` 作为 Attempt ID，M1 Attempt 1 Golden ID 不变。
3. Retry 创建新的 Attempt，不复用旧 Row 或旧 Lease Token；Pair、Arm 和 Logical Trial Identity 不变。
4. Lease Token 使用随机高熵值，数据库只保存 Hash；Lease Generation/Fence 单调递增。数据库时间达到 `lease_expires_at` 后，旧 Owner 的 Heartbeat、Start 和 Completion 一律返回 `LEASE_EXPIRED`。
5. 同一 Attempt 的同一 Result Manifest Hash 可以安全重发；不同 Hash 返回 `RESULT_CONFLICT`。Logical Trial 的最终 Result 唯一，Experiment Counter 只在首次终结时更新。
6. Experiment 取消采用 `CANCEL_REQUESTED → CANCELLED` 两阶段语义：请求后停止新 Claim；活动 Attempt 协作式确认或 Lease 到期后终结为 `CANCELLED`。
7. M2 对外可脚本化入口为现有 `skillgate` CLI 与 Synthetic Fixture/Fault Harness；REST 与 Worker gRPC 留给后续里程碑。
8. SQL Migration 使用嵌入式 `pressly/goose/v3`，PostgreSQL Driver/Pool 使用 `github.com/jackc/pgx/v5`；版本在 `go.mod` 中固定。

## 3. 备选方案

### 方案 A：PostgreSQL Queue/Lease（选择）

优点：

- Claim、Lease、状态和 Counter 可以共享事务边界；
- 本地依赖少，适合 M2 故障测试；
- 可以使用唯一约束和 `ON CONFLICT`/行锁表达幂等语义。

缺点：

- Queue 吞吐会受数据库锁竞争限制；
- 过期回收和业务状态共享同一 PostgreSQL，需要严格事务设计。

### 方案 B：引入 Kafka/NATS 或独立任务 Broker

优点：

- 消息吞吐和消费者解耦更强；
- 后续可以单独扩展 Worker Transport。

缺点：

- 增加消息、数据库和状态一致性边界；
- 不能替代 Result Commit 的事务幂等；
- 提前扩大本地部署、重放和故障恢复范围。

### 方案 C：只使用内存队列或 SQL Mock

优点：

- 开发速度快；
- 不需要本地 PostgreSQL。

缺点：

- 无法证明 `SKIP LOCKED`、事务回滚、Lease 竞态和唯一约束；
- 与产品最终事实来源不一致，不能作为 M2 Gate。

## 4. 选择理由

方案 A 延续 ADR-002，优先证明 Go Control Plane 的可靠生命周期逻辑，而不是堆叠基础设施。新增 Logical ID 解决 M1 `trial_id` 把 Attempt 纳入 Identity 后无法表达 Retry 组的问题。严格过期拒绝优先保护 fencing 正确性，牺牲少量已完成工作恢复机会；在 Agent Trial 场景中，重复执行可以由 Retry 承担，但旧 Owner 不能覆盖新 Owner 或 Result。

CLI + Fixture 保持 M1 的可脚本化风格，并将 Worker Protocol 延后到 M3，避免在同一个 Change 中同时冻结数据库可靠性和跨语言传输协议。

## 5. 后果

### 正面后果

- 每个 Logical Trial 只有一个可审计最终 Result；
- 重复 Completion 和数据库重试不会重复更新 Counter；
- 旧 Lease 的迟到写入有明确、可观察的失败 Code；
- M2 可以在无 Provider Credential 的情况下使用 Synthetic Fixture 验证。

### 负面后果

- Lease 到期后旧 Worker 即使已经完成工作也必须重试；
- 同一 PostgreSQL 实例承载 Queue 和状态，会受到锁竞争影响；
- M2 CLI 不是长期稳定的外部 API Contract。

### 新增风险

- 锁顺序或索引错误可能造成 Claim/Cancel 竞争；需要真实并发测试与 `EXPLAIN` 证据；
- 完成写入前的 Artifact/Object Store 语义留给 M3/M4，M2 只保存受大小限制的 Synthetic Result Manifest；
- Migration 工具与 PostgreSQL Driver 的版本升级需要单独回归。

## 6. 验证方式

- 空 PostgreSQL 数据库执行 Migration 两次；
- 至少 40 个并发 Claim 验证 Attempt 不重复分配；
- Owner/Token/Fence/Expiry Heartbeat 负向矩阵；
- Completion 三次重发、不同 Hash 冲突和 Counter/Reconcile；
- Claim 后退出、Commit 前响应丢失、Expiry Retry、迟到 Completion、Cancel 和 Shutdown 故障测试；
- `go test ./...`、`go test -race ./...`、`go vet ./...`、M0 Fixture 回归和 CLI JSON Smoke。

## 7. 相关文档

- `docs/comet/changes/m2/brief.md`
- `docs/comet/changes/m2/specs/scheduler-reliability-core/spec.md`
- `docs/decisions/ADR-002-postgresql-queue-before-broker.md`
- `docs/architecture/execution-lifecycle.md`
- `docs/architecture/storage-and-data.md`
- `docs/operations/failure-recovery.md`
- `docs/operations/local-development.md`
- PostgreSQL `SELECT` 官方文档：https://www.postgresql.org/docs/current/sql-select.html
- PostgreSQL `INSERT` 官方文档：https://www.postgresql.org/docs/current/sql-insert.html
- pgx/v5 官方包文档：https://pkg.go.dev/github.com/jackc/pgx/v5
- goose/v3 官方包文档：https://pkg.go.dev/github.com/pressly/goose/v3

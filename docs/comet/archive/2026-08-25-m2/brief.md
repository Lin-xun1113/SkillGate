# Outcome

在 M1 已归档的 Registry/Manifest Compiler 基础上，实现 M2 PostgreSQL Scheduler 与可靠性核心：将冻结的配对 Trial Plan 持久化为可领取、可续租、可恢复、可取消的执行单元，并用至少一次执行与幂等 Result Commit 保证 Worker 崩溃、重复 Completion、并发 Claim 和旧 Owner 迟到提交都不会造成重复计分。M2 必须以真实 PostgreSQL 故障测试证明这些语义，不声称 Exactly-once Execution。

# Scope

- 新增 PostgreSQL Migration 和可替换的 Store 边界，PostgreSQL 作为 Experiment/Trial 生命周期事实来源。
- 将 M1 编译结果物化为 Experiment、Logical Trial、Attempt/Lease 和 Result 所需的稳定记录；保留 Pair 与 Treatment Identity。
- 实现 `PENDING`、Lease、Running、Retry Wait 与终态之间的受控状态转换。
- 使用 PostgreSQL Transaction 与 `FOR UPDATE SKIP LOCKED` 实现并发 Claim，生成不可复用的 Lease Token/Fence，并记录 Owner 与 Expiry。
- 实现 Heartbeat、Lease 过期回收、有界 Retry、指数退避和预算耗尽后的确定性终态。
- 实现 Result Idempotency、冲突检测、旧 Lease 防护，以及 Result/终态/Experiment Counter 的单事务提交。
- 实现 Experiment Cancellation、停止新 Claim、运行中任务协作式收尾和进程优雅关闭。
- 扩展 `skillgate` CLI 并提供确定性 Fixture/Fault Harness，作为 M2 可脚本化操作与故障注入入口。
- 增加真实 PostgreSQL 的并发、崩溃恢复、重复提交、取消和迟到提交测试，并同步 Contract、ADR、运行文档、README 与 PROJECT_STATUS。

# Non-goals

- 不实现 Python/LangGraph Worker、M3 gRPC Runner Protocol、真实 Model Provider 或 Worker 注册协议。
- 不实现 Sandbox、MinIO/S3 Artifact Blob、Trace Replay、真实 Grader、统计聚合、Report 或 Release Gate；M2 只保证 Result 与计数提交的可靠性边界。
- 不引入 Kafka、NATS 或其他 Broker，不实现多区域部署、跨数据库一致性或生产级高可用运维。
- 不实现 UI、外部 REST API 或 M3 Worker gRPC；M2 通过 CLI + Fixture/Fault Harness 暴露可脚本化入口。
- 不执行任意不可信 Skill/Tool/Model 内容；故障测试使用仓库内 Synthetic Fixture。
- 不声称 Exactly-once Execution；允许执行尝试重复，但一个 Logical Trial 只能有一个被接受并贡献计数的结果。

# Acceptance examples

- A1：版本化 Migration 能在真实 PostgreSQL 空库上升级到 M2 Schema，重复执行保持幂等；Go Module 可构建，`go test ./...` 与静态检查通过。
- A2：M1 编译出的 Pair/Arm/Repetition 能按 `logical_trial_id=hash(pair_id, arm)`、`trial_id=hash(pair_id, arm, attempt)` 原子物化；重复物化不生成重复执行单元，也不改变 Pair Identity。
- A3：两个或更多 Scheduler 并发领取大量 `PENDING` 工作时，`FOR UPDATE SKIP LOCKED` 不会把同一可执行 Attempt 同时交给两个 Owner；每次 Claim 返回新的 Lease Token/Fence 与 UTC Expiry。
- A4：合法 Owner 可从 Lease 进入 Running 并续租；错误 Token、错误 Owner、终态任务或已失效 Lease 的 Heartbeat/状态转换被稳定拒绝且不改变数据库。
- A5：Worker 领取后退出时，过期回收会按最终 Retry Contract 进入等待并在到期后重试；Backoff 有界、可测试，超过最大 Attempt/Budget 后只进入一个确定终态。
- A6：同一 Completion 连续提交三次只保存一个 Result、只终结一次 Logical Trial、只对 Experiment Counter 贡献一次；相同身份但不同内容返回稳定冲突且不覆盖既有结果。
- A7：Worker 完成工作但提交前退出时可以安全重发；Lease 到期后旧 Worker 的迟到提交一律返回稳定的 `LEASE_EXPIRED`，不写 Result、不覆盖新 Owner，也不产生计数。
- A8：Experiment 取消先进入可观察的 `CANCEL_REQUESTED` 并停止新 Claim，已 Lease/Running 工作确认取消或 Lease 到期后收敛到 `CANCELLED`；重复取消幂等，Scheduler 关闭不会留下不可恢复的中间提交。
- A9：`skillgate` CLI + Fixture/Fault Harness 提供版本化成功/错误结果，可脚本化演示迁移、物化、领取、续租、完成、取消和回收，不要求 REST、Python Worker 或外部 Credential。
- A10：真实 PostgreSQL 故障测试覆盖领取后崩溃、Model Call 后提交前崩溃、三次重复 Completion、多 Worker 并发、Experiment 中途取消和旧 Lease 迟到提交；正式 Contract、ADR、运行文档、README 与 PROJECT_STATUS 与实现一致。

# Constraints and invariants

- PostgreSQL 是生命周期状态和单次计数的事实来源；Telemetry、日志或本机内存不能决定业务正确性。
- Queue/Lease 使用 PostgreSQL Transaction、行锁和 `FOR UPDATE SKIP LOCKED`；Broker 只在实测瓶颈并另行接受 ADR 后考虑。
- 至少一次执行与幂等提交是硬约束；任何实现或文档不得称为 Exactly-once Execution。
- Result Commit 必须在一个数据库事务内校验执行身份与 Lease、写入 Result、终结 Logical Trial 并更新一次 Experiment Counter；失败时整体回滚。
- Terminal 状态不可回到 Running；取消不能修改已冻结的 Experiment/Pair/Treatment Identity。
- 同一 Logical Trial 只能有一个接受结果；Utility 不能通过重试或重复消息重复贡献。
- Skill Package、Tool Output、Model Response、Result Manifest 和所有外部标识都视为不可信输入；SQL 使用参数绑定并设置大小/时间边界。
- 所有持久化时间使用 UTC；与时间相关的测试使用可控 Clock，不依赖长时间 sleep。
- M2 验收必须连接真实 PostgreSQL；当前机器有 Docker CLI/Compose，但 Docker daemon 未运行且没有本机 `psql`，Build/Verify 前必须提供可用 daemon 或 `TEST_DATABASE_URL`，不得降级为只验证 SQL Mock。
- Runtime A 项只来自本 brief 的 A1–A10；完整规格的普通段落、错误表和测试矩阵不生成额外 A 项。

# Decisions

- Change 名称为 `m2`，使用独立 worktree `.worktrees/m2`、分支 `comet/m2`，目标分支为 `master`；原目录未提交改动不归入 M2。
- 沿用已接受 ADR-002：MVP 先使用 PostgreSQL Queue/Lease，不引入 Broker。
- M2 保持单一 Native change：Queue、Lease、Retry、Commit 和故障测试共享同一事务/状态模型，拆分会增加跨 change 契约漂移，当前没有独立交付价值。
- M2 使用至少一次执行 + 幂等 Result Commit，并明确拒绝 Exactly-once Execution 声明。
- M2 验收使用真实 PostgreSQL 集成测试；Unit Test 可使用 Fake Clock/Store，但不能替代数据库并发与事务 Gate。
- M2 使用 10 个高层 Runtime 验收项；测试矩阵、状态表、错误码和实现说明保留在完整规格中，不拆成额外 A 项。
- 用户于 2026-08-25（UTC）确认执行身份：新增稳定 `logical_trial_id=sha256(canonical_json({pair_id, arm}))`；保留 M1 `trial_id=sha256(canonical_json({pair_id, arm, attempt}))` 作为 Attempt ID。
- 同一 `trial_id` + 同一 Result Manifest Hash 的重发幂等返回既有 Result；同一 `trial_id` + 不同 Hash 返回冲突；Logical Trial 已接受最终 Result 后不再接受其他 Attempt 的 Result。
- 实码核对确认 M1 `PairPlan.Trials` 已提供 `pair_id`、`arm`、`attempt=1` 和现有 `trial_id`，新增 Logical ID 不改变 M1 `TrialID(pair_id, arm, attempt)` 或 Golden ID；M2 从该编译产物进入事务型 Store。
- M2 CLI 延续现有标准库手写分派和 `skillgate.cli.v1` Envelope，新增 `db`、`experiment`、`trial`、`scheduler` 操作；最终子命令帮助文本在 Build 中按完整规格冻结。
- 用户确认严格 Lease Fencing：只要数据库时间达到 `lease_expires_at`，旧 Token 的 Heartbeat、状态转换和 Result Commit 一律返回 `LEASE_EXPIRED`，即使尚未重新 Claim 也不允许恢复提交。
- 用户确认取消状态模型：`ACTIVE → CANCEL_REQUESTED → CANCELLED`；请求阶段立即停止新 Claim，运行工作收敛后才进入终态。
- 用户确认 M2 对外入口为 `skillgate` CLI + Fixture/Fault Harness；REST 与 Worker gRPC 延后。
- 用户于 2026-08-25（UTC）最终确认 M2 目标、完整规格、关键决定、A1–A10 验收项和非目标，并授权进入 Build。

# Open questions

- 无。Shape 已完成共享理解确认。

# Verification expectations

- Unit Test：状态转换、Retry Classification、Backoff/Fake Clock、Token/Fence 校验、错误映射和取消收敛。
- 真实 PostgreSQL Integration Test：Migration、事务回滚、唯一约束、并发 Claim、Heartbeat、Expiry Sweeper 和 Result Commit。
- Fault Test：领取后退出、完成工作后提交前退出、Completion 重发三次、并发 Worker、Experiment 中途取消、旧 Worker 迟到提交。
- Race/Static Gate：`go test -race ./...`、`go vet ./...`、格式检查和可脚本化入口 Smoke Test。
- 回归 Gate：M0 离线校验与 M1 Registry/Compiler 测试继续通过，M0/M1 归档产物不修改。
- 新的只读 Verifier 逐项检查 A1–A10、完整 M2 规格、实际 SQL/Go 实现、真实 PostgreSQL 检查结果和文档状态。

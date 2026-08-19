# 系统架构

**状态：** `ACCEPTED`，用于 MVP  
**架构风格：** Go 模块化单体 Control Plane + 与语言无关的 Worker Protocol

## 1. 设计目标

SkillGate 必须满足以下目标：

- 评审者可以重新运行或审计实验；
- Worker 发生故障后可以恢复进行中的 Trial；
- 可以增加新的 Worker，而不把 Go 与 Python 的实现细节耦合在一起；
- 如实表达统计不确定性和 Judge 不确定性；
- 在受限环境中执行不可信的 Skill Package；
- 足以展示 Go Backend 和 Strategy Engine 的工程能力。

## 2. 组件关系

```text
                         ┌────────────────────────┐
                         │ React / 静态查看器      │
                         └────────────┬───────────┘
                                      │ HTTP/JSON
                         ┌────────────▼───────────┐
                         │ Go Control Plane        │
                         │                         │
                         │ API / Auth Boundary    │
                         │ Experiment Compiler    │
                         │ Strategy Evaluator     │
                         │ Scheduler + Lease Loop │
                         │ Result Committer       │
                         │ Metric Aggregator      │
                         │ Release Gate Evaluator │
                         └───────┬─────────┬───────┘
                                 │ SQL     │ gRPC
                     ┌───────────▼───┐ ┌──▼────────────────┐
                     │ PostgreSQL    │ │ Trial Workers     │
                     │ 元数据/状态    │ │ Python/LangGraph  │
                     └───────────────┘ └────────┬─────────┘
                                                 │
                                      ┌──────────▼─────────┐
                                      │ Sandbox             │
                                      │ Docker/rootless    │
                                      │ Agent + Tools      │
                                      └──────────┬─────────┘
                                                 │
                                      ┌──────────▼─────────┐
                                      │ MinIO / S3          │
                                      │ Trace / Artifact    │
                                      └────────────────────┘

                  OpenTelemetry → Collector → Prometheus/Tempo/Grafana
```

## 3. 责任边界

### Go Control Plane 负责

- 请求校验和 API Contract；
- 不可变 Registry 元数据；
- Experiment 编译和 Pair Identity；
- Strategy Rule 编译与评估；
- Trial 生命周期状态和 Lease；
- Retry、Timeout、Cancellation 和 Budget；
- 幂等 Result Commit；
- Metric Aggregation 和 Release Decision；
- Artifact 元数据与访问授权；
- 系统级 Telemetry。

### Trial Worker 负责

- 加载指定的 Trial Request；
- 构造 LangGraph State Machine；
- 调用选定的 Agent/Model Adapter；
- 挂载 Request 中指定的 Skill Snapshot；
- 在允许的边界内执行 Grader；
- 收集 Trace Event 和输出 Artifact；
- 返回可由 Go 校验 Hash 的 Result Manifest。

### Sandbox 负责

- Process、Filesystem、CPU、Memory、PID 和 Network Isolation；
- 执行目标 Skill 控制的脚本和 Agent Tool；
- 在配置启用时采集 Filesystem 与 Network Evidence。

不能假设 Sandbox 是完美的安全边界。当前启用的 Backend 及其限制必须记录在 Report 中。

## 4. 部署模式

### 本地 MVP

```text
docker compose:
  postgres
  minio
  otel-collector（可选 profile）
  skillgate-api
  skillgate-worker
```

Go API 和 Scheduler 可以使用同一个 Binary Image，通过不同 Role Flag 作为独立进程运行。这样既保持代码库简单，又可以测试 Scheduler 重启和故障恢复。

### CI 模式

- API 和 Scheduler 可以合并为一个进程；
- Model Call 可以替换为 Deterministic Fixture Provider；
- Sandbox 仅在 Synthetic Fixture 场景使用可信的本地 Test Provider；
- 核心 Unit/Integration Test 不需要外部 Credential。

### 未来的分布式模式

- 多个带数据库协调的 Scheduler Replica；
- Worker Autoscaling；
- 可选的 NATS/Kafka Transport；
- Object Storage 生命周期策略；
- 独立的 Query/Analytics Store。

以上不是 MVP 要求。

## 5. 请求流程

1. Client 上传或引用不可变的 Skill Version。
2. Client 注册 Eval Suite 和 Strategy/Policy Version。
3. API 校验 Manifest 并创建 Experiment。
4. Experiment Compiler 将 Case × Arm × Repetition 展开为 Trial Row。
5. 计算并持久化 Pair Identity。
6. Scheduler 将 Pending Trial Lease 给 Worker。
7. Worker 校验 Request，运行 LangGraph Graph，发送 Event 并上传 Artifact。
8. Worker 使用 Idempotency Key 和 Hash 提交 Result Manifest。
9. Go Committer 校验 Owner、状态、Hash 和 Budget，并提交一条逻辑结果。
10. Aggregator 计算 Case 级 Metric 和置信区间。
11. Release Gate 评估 Metric 和 Security Hard Gate。
12. UI/API 暴露 Decision、Evidence 链接和 Trace Replay。

## 6. 一致性模型

- PostgreSQL 是生命周期状态和 Score Summary 的事实来源；
- Object Storage 是大型不可变 Evidence Blob 的事实来源；
- 只有当数据库提交引用了已校验 Hash 的 Artifact 后，Result 才能显示为 Scored；
- Telemetry 是尽力而为的，不得决定业务正确性；
- Event Delivery 是至少一次，Consumer 必须按 `event_id` 去重。

## 7. 故障边界

| 故障 | 负责组件 | 必须行为 |
|---|---|---|
| API 进程重启 | Go | 已持久化的 Experiment 仍可查询 |
| Scheduler 重启 | Go | 未过期 Lease 保留，过期 Lease 可恢复 |
| Worker 崩溃 | Scheduler + DB | Lease 过期，按 Policy 有界 Retry |
| Model Timeout | Worker | 发送 Failure Evidence，返回类型化错误 |
| Artifact Upload 失败 | Worker/Control Plane | Trial 标记为 Incomplete，不能将缺失 Evidence 判为成功 |
| 重复 Completion | Result Committer | 返回幂等结果，只对 Aggregate 贡献一次 |
| Judge 不可用 | Grading Policy | Deterministic 结果可保留；依赖 Judge 的 Metric 标记为 Incomplete |
| Object Store 不可用 | Control Plane | Artifact 持久化前不能生成最终 Scored Result，除非 Manifest 明确允许 Inline Evidence |

## 8. 架构不变量

1. Trial 不能只通过可变路径挂载 Skill，必须携带 Content Hash。
2. Baseline/Candidate Pair 的非 Treatment Identity 不一致时不能比较。
3. Terminal Trial 不能回到 Running。
4. Completion Request 可以安全重试。
5. Security Hard Gate Finding 不能被普通 Utility Metric 覆盖。
6. Evidence 不完整的 Report 不能标记为 `PASS`。
7. Worker 不能选择与 Request Identity 不同的 Model、Skill、Endpoint 或 Credential。
8. Cancellation 先采用协作式方式，只有到 Sandbox 边界才允许强制终止。

## 9. 技术选型与理由

| 关注点 | MVP 选择 | 理由 |
|---|---|---|
| Control Plane | Go | 匹配目标岗位，适合并发和简单部署 |
| Policy Expression | CEL-Go | 类型安全、有界、可编译、可解释，避免任意脚本 |
| API | 初期 REST/JSON；Worker 使用 gRPC | 外部演示简单，内部边界类型明确 |
| DB | PostgreSQL | 事务、行锁、JSONB 和本地可复现性 |
| Queue | PostgreSQL `FOR UPDATE SKIP LOCKED` | MVP 少一个基础设施组件，后续可升级 |
| Worker | Python + LangGraph | Agent Graph、Model 和 Tool 生态成熟 |
| Artifact Store | MinIO/S3 API | 将大型 Evidence 与事务元数据分离 |
| Telemetry | OpenTelemetry | Go/Python 间共享标准 Trace/Metric Context |
| Sandbox | 先用 rootless Docker | 本地可复现，明确记录隔离限制 |

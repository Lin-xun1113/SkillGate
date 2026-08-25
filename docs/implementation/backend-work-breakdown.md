# Go Backend 工作拆解

**状态：** `FUTURE`；`PROPOSED`，用于 M1–M7；M1 代码已部分偏离，需在 M1 归档后重新对齐

## 1. 推荐目录边界

```text
cmd/
  skillgate/                 # API、Scheduler、Worker Gateway 的统一入口
internal/
  api/                       # HTTP Handler、请求校验、错误映射
  registry/                  # Skill/Suite/Strategy/Policy Registry
  manifest/                  # YAML/JSON 解析与语义校验
  identity/                  # Canonicalization、Hash、Pair ID
  experiment/                # 编译、冻结、状态管理
  scheduler/                 # 领取、Lease、Heartbeat、Expiry Sweeper
  retry/                     # 错误分类、Backoff、预算
  runner/                    # Worker Protocol Server/Client
  grading/                   # Grade 汇总和状态
  metrics/                   # Case 聚合、Bootstrap、Reliability
  strategy/                  # CEL Compile/Evaluate、Decision Trace
  releasegate/               # Promotion Policy
  artifact/                  # Object Store Adapter、Hash 校验
  store/                     # PostgreSQL Repository 和 Transaction
  telemetry/                 # OTel、Log、Correlation Context
  config/                    # 配置加载与安全默认值
```

包之间优先依赖接口，不要让 API Handler 直接操作 SQL 或模型 Provider。

## 2. 后端实现顺序

### 2.1 Identity 与 Manifest

需要先解决：

- Canonical JSON；
- Canonical Skill Archive；
- 路径安全；
- Hash 稳定性；
- 版本和来源；
- Pair Identity。

测试重点：字段顺序、换行、Symbolic Link、空目录、Unicode、重复文件名、路径穿越。

### 2.2 Store 与事务

Repository 方法应表达领域动作，而不是暴露任意 SQL：

```text
CreateExperiment
CompileExperiment
LeaseNextTrial
HeartbeatTrial
ScheduleRetry
CommitTrialResult
CancelExperiment
CreateMetricSnapshot
CreateReleaseDecision
```

关键事务不能由上层拼接多个不透明的写操作代替。

### 2.3 Scheduler

Scheduler 由三个循环组成：

- Claim Loop：领取符合能力和预算的 Pending Trial；
- Expiry Loop：回收过期 Lease；
- Reconcile Loop：发现计数、状态和实际结果不一致。

所有循环都必须响应 `context.Context` 取消，并在 Shutdown 时等待正在进行的 DB Transaction 完成。

### 2.4 Runner Gateway

Runner Gateway 只负责协议和生命周期，不负责理解 LangGraph Graph。它需要校验：

- Worker 身份；
- Lease Token；
- Request Hash；
- Capability；
- Event Sequence；
- Artifact Manifest；
- Result Idempotency。

### 2.5 Metrics Engine

Metrics Engine 应是纯函数优先：

```text
trials + grades + aggregation_config → metric_snapshot
```

这样可以用固定 Fixture 重算，不依赖 DB 当前状态或外部 Model Call。

## 3. 并发与资源控制

第一版至少实现：

- 全局 Worker 并发上限；
- Experiment 并发上限；
- Model Provider 并发上限；
- Token/Cost Budget 预留与实际结算；
- Context 取消；
- Provider 429 的 Backoff；
- Queue 背压。

不要在 Handler 中启动无法追踪的 Goroutine。所有后台 Goroutine 必须归属于明确的组件生命周期。

## 4. 错误设计

错误应包含稳定 Category：

```text
validation
identity_mismatch
not_found
conflict
lease_expired
worker_lost
provider_transient
provider_permanent
budget_exceeded
security_denied
artifact_incomplete
internal
```

上层根据 Category 决定是否 Retry；不能通过匹配错误字符串判断。

## 5. Go 测试要求

### Unit Test

- Manifest 字段和状态转换；
- Hash 和 Pair ID；
- CEL Rule；
- Retry Classification；
- Metric 公式；
- API 错误映射。

### Integration Test

- PostgreSQL Transaction；
- 并发 Lease；
- Object Store Artifact Commit；
- Worker Protocol；
- Cancellation；
- OTel Context Propagation。

### Property/Fuzz Test

优先测试：

- 任意合法 JSON Canonicalization 的稳定性；
- Pair 编译不生成重复 Logical Identity；
- Result Commit 重复调用的结果等价；
- 非法路径永远不能越出根目录；
- Rule Evaluation 不会修改输入 Context。

## 6. 可观测性要求

每个关键领域方法应产生：

- Trace Span；
- 状态和耗时 Metric；
- 包含 `request_id`/`experiment_id`/`trial_id` 的结构化 Log；
- 对外错误 Category。

不得在高 Cardinality Metric Label 中加入完整 Prompt、Trial ID 或 Skill Hash。

## 7. 交付顺序检查

Go Backend 不应先做大而全的 API。推荐先完成：

```text
CLI compile
  → DB lease/commit test
  → Stub worker protocol
  → HTTP API wrapper
  → live worker
  → UI
```

这样每一步都有可运行证据。

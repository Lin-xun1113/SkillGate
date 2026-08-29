# 架构与原理

> 状态：与 2026-08-27 代码库同步 · 权威设计文档见 `docs/architecture/`，本文是面向使用者的浓缩版

## 总体架构

SkillGate 是一个 **Go 模块化单体 Control Plane + 语言无关 Worker 协议**的系统：

```text
                ┌─────────────────────────────┐
                │     Web UI (skillgate ui)   │  HTTP :8080
                └──────────────┬──────────────┘
                               │ 只读查询
   ┌───────────────────────────▼───────────────────────────┐
   │              Go Control Plane (cmd/skillgate)          │
   │                                                        │
   │  Registry/CAS      内容寻址 Skill/Suite 存储            │
   │  Manifest Compiler 展开配对 Trial 计划                  │
   │  Scheduler         PG 队列 + Lease + 幂等提交           │
   │  Runner Service    gRPC :50051，六 RPC                 │
   │  Grading Service   Deterministic 优先评分 + 轮询器      │
   │  Metrics/Report    聚合 + Bootstrap CI + 三格式报告     │
   │  Release Gate      CEL 规则评估 + 独立硬门禁            │
   └──────────────┬──────────────────────────┬─────────────┘
                  │ SQL                      │ gRPC (proto/runner/v1)
   ┌──────────────▼───────────┐   ┌──────────▼──────────────────┐
   │ PostgreSQL 17            │   │ Python Worker               │
   │ 6 个 migration           │   │  langgraph_worker/          │
   │ 实验与生命周期事实来源     │   │  fixture_worker/            │
   └──────────────────────────┘   │  Docker 沙箱内隔离执行       │
                                  └─────────────────────────────┘
```

## Go 包职责地图

| 包 | 职责 | 关键类型 |
|---|---|---|
| `internal/identity` | Canonical JSON、SHA-256、路径与符号链接安全 | `SkillPackageHash` |
| `internal/registry` | 文件系统 CAS，幂等注册 Skill/Suite | `FileStore` |
| `internal/validation` | Suite/Skill 校验，产生带退出码的 Diagnostic | `Diagnostic` |
| `internal/manifest` | Manifest 解析、Pair Identity、Trial 展开 | `Compile` |
| `internal/experiment` | 配对身份与 Trial 计划模型 | — |
| `internal/store/postgres` | 全部 SQL：迁移、Claim、Lease、幂等 Commit | `Open` / `pgx` + goose |
| `internal/lease` | 高熵 Lease Token、常量时间校验 | — |
| `internal/retry` | 错误分类、有界指数退避 + Full Jitter | `Category` |
| `internal/scheduler` | Trial/Attempt 身份、状态机、稳定错误契约 | `Outcome` |
| `internal/runner` | gRPC 服务端、执行投影、Artifact 落盘（含路径穿越防护） | `Server` |
| `internal/sandbox` | Docker 容器生命周期（隔离配置见下） | `DockerManager` |
| `internal/grader` | Grader 模型、注册表、五种评分方法 | `Registry` |
| `internal/grading` | 评分编排、实验终态轮询、快照构建 | `Service` |
| `internal/metrics` | Case 聚合、配对、触发/安全证据聚合 | `Pair` / `AggregateTrigger` |
| `internal/statistics` | 2000 次 Cluster Bootstrap、pass@k、资源差值 | `Bootstrap` |
| `internal/report` | JSON/MD/HTML 报告 + 内嵌 JSON Schema 自校验 | `Generator` |
| `internal/strategy` | CEL 编译缓存、规则评估、优先级裁决 | `Compile` / `Context` |
| `internal/releasegate` | 指标快照、硬门禁、Decision 持久化 | `Evaluate` / `ApplyHardGate` |
| `internal/ui` | 内嵌模板 HTTP Server（列表页/详情页/404/500） | `Server` |

## 一次实验的完整生命周期

```
① 注册      skill/suite register → CAS（幂等，内容寻址）
② 编译      compile → 校验引用/配对身份/泄漏 → 冻结 manifest_hash
③ 物化      experiment materialize → Experiment + LogicalTrial + Attempt 行
④ 调度      Worker ClaimTrial → FOR UPDATE SKIP LOCKED → Lease Token + Fence
⑤ 投影      服务端按 manifest_hash 解析执行内容 → execution_hash 随 payload 下发
⑥ 执行      Worker 校验双 Hash → 沙箱跑 Agent → 心跳 + 事件流 + Artifact
⑦ 提交      CompleteTrial → 校验 Owner/状态/Hash/Budget → 幂等落库
⑧ 评分      grading 轮询器发现终态实验 → Deterministic Grader → grades
⑨ 聚合      Case 聚合 → 配对 → Bootstrap CI → pass@k → 资源差值 → Report
⑩ 决策      指标快照（带 Hash）→ CEL 规则 + 硬门禁 → Decision 落库
⑪ 查看      UI / report.json / report.md / report.html
```

## 可靠性语义（为什么不会重复计分）

SkillGate 的执行语义是**至少一次执行 + 幂等提交**，不声称 Exactly-once。可靠性由四层机制保证：

### 1. Lease + Fence

- Claim 签发一次性高熵 Lease Token 和单调递增 `lease_generation`；
- 心跳续租；租约过期后 Scheduler 可回收 Trial 给其他 Worker；
- 旧租约（generation 更小）的任何后续操作——心跳、事件、提交——都会被拒绝（`LEASE_EXPIRED` / `OWNER_MISMATCH`）。

### 2. 幂等提交

- 提交携带 `idempotency_key`；同一 key 重复提交返回 `ALREADY_COMMITTED`，不产生第二条 Result；
- Result、终态、Experiment 计数器在**同一事务**内更新——不会出现「结果落库但计数没加」的中间态。

### 3. 有界重试

- 失败按类别分类：`TRANSIENT` 类（provider_transient、worker_lost 等）按指数退避 + Full Jitter 重试；
- `PERMANENT` 类直接终态；超过 `max_attempts` 后确定性终态（`RETRY_EXHAUSTED`）。

### 4. 两阶段取消

- `experiment cancel` 先标记 `CANCEL_REQUESTED`：停止发放新 Claim；
- 运行中 Worker 通过心跳得知取消，协作式收尾（中断 Agent 图、终止容器、上报 CANCELLED）；
- Terminal 状态不可逆（架构不变量：终态不能回到 Running）。

真实 PostgreSQL 的故障测试覆盖：40 Worker 并发 Claim、心跳负向用例、同一结果提交三次、响应丢失后重发、租约过期重试与迟到提交、两阶段取消——这些测试在 `go test ./internal/store/postgres` 中持续运行。

## 沙箱隔离原理

每个 Trial 一个独立 Docker 容器（`internal/sandbox`）：

| 隔离维度 | 配置 |
|---|---|
| 文件系统 | 只读根文件系统 |
| 用户 | 非 root（UID 1000） |
| 网络 | 网络隔离 |
| 资源 | CPU / 内存限制 |
| Skill 输入 | CAS 只读挂载（Hash 校验） |
| 产物输出 | 独立 artifacts 卷挂载 |
| 生命周期 | 超时/取消时强制终止 + 自动清理，无残留容器 |

注意：SkillGate **不声称沙箱是完美安全边界**。当前启用的 Backend（rootless Docker）及其限制记录在报告中，威胁模型见 `docs/architecture/threat-model.md`。

## 统计原理

### 配对差值与 Cluster Bootstrap

每个 Case 的 Lift = Candidate 均分 − Baseline 均分。总体 Lift 的 95% 置信区间用 **Cluster Bootstrap**（2000 次重采样，固定随机种子，Case 为聚类单元、按 Case 重采样）计算——因为同一 Case 的多次重复不独立，按 Case 聚类才不会低估方差。

**显著性判据**：置信区间不跨零（下界与上界同号）。区间跨零时，即使均值是正的，结论也只能是「观察到的提升与随机波动无法区分」。

### pass@k 与 pass^k

沿用 HumanEval 公式（n 次尝试中任取 k 次全对/至少一次对的概率的无偏估计）：

- `pass@k`：k 次里至少一次通过的概率；
- `pass^k`：k 次全部通过的概率（稳定性）。

有效配对不足或重复次数不够时，报告**省略**这些字段而不是填 0——省略号比误导性的零诚实。

### 触发指标的多数投票

Autonomous Trigger Case 的「是否加载了 Skill」按 Candidate 臂重复次数的**多数投票**判定，平票算未触发（对 Skill 保守）。Recall/Specificity 只在正负 Case 都存在时才计算（`Evaluated=true`），否则标记未评估，不输出误导性的 0。

## Fail Closed 原理

Release Gate 的决策路径上，所有异常都导向更保守的结论：

| 异常 | 结果 |
|---|---|
| Policy 文件缺失 | HOLD（`policy_missing`） |
| CEL 编译错误 | HOLD（`compile_error`） |
| CEL 求值运行时错误 | HOLD（`evaluation_error`） |
| `security.critical > 0` 或存在已确认利用 | REJECT（硬门禁，优先于一切 CEL 结果） |
| Utility 置信区间跨零 | HOLD（硬门禁） |
| 身份/配对无效 | REJECT（硬门禁） |
| 证据不完整 | HOLD（硬门禁） |

硬门禁与 CEL 结果取**更严格**者（REJECT > HOLD > PROMOTE），覆盖行为记录在 `hard_gate_override` 字段。因此任何路径都无法静默产出 PROMOTE。

## 关键设计决策

全部决策记录见 `docs/decisions/`（ADR-001 ~ ADR-010），要点：

| ADR | 决策 | 一句话理由 |
|---|---|---|
| 001 | Go Control Plane + Python Worker | Go 承载平台逻辑，Python 承载 Agent 生态 |
| 002 | PostgreSQL 队列先于 Broker | 少一个基础设施组件，事务语义更强 |
| 003 | 配对评估强制 | 无 Baseline 即无因果推断 |
| 004 | Deterministic 优先 | 可复现性 > LLM 的灵活性 |
| 005 | 安全独立硬门禁 | Utility 不能赎买安全 |
| 006 | Trigger 与 Answer 分离 | 不同统计总体不得混算 |
| 007 | 首个 Workload 用 CSV 分析 | 可确定性验证，无需真实 LLM |
| 008 | 内容寻址身份 | 一切引用可验证、不可篡改 |
| 009 | PG Lease/幂等语义 | 可靠性核心用最可审计的方式实现 |
| 010 | 协议层 Hash 校验 | Worker 无法执行被篡改的任务 |

## 一致性模型

- **PostgreSQL**：生命周期状态与 Score Summary 的事实来源；
- **Artifacts**：大型不可变 Evidence 存本地卷（S3 兼容存储为规划项）；
- **Hash 链**：Skill Hash → Suite Hash → Manifest Hash → execution_hash → Snapshot Hash → Decision ID，任一环节内容被篡改都会使后续引用失效；
- 只有当结果引用了已校验 Hash 的 Artifact 后，Trial 才能进入评分；证据不完整的报告不能标记 PASS。

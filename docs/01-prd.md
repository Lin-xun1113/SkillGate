# 产品需求文档（PRD）

**状态：** `ACCEPTED`，用于 MVP 规划  
**版本：** 0.1  
**最后更新：** 2026-08-19（UTC）

## 1. 产品目标

构建一个可以在本地部署的 Platform，将 Agent Skill 或 Strategy 的变更转换为可审计的 Experiment 和可解释的 Policy Decision。

MVP 必须支持完整的纵向切片：

```text
Skill Version + Eval Suite
  → Experiment Compilation
  → Paired Trial Scheduling
  → LangGraph Worker Execution
  → Deterministic Grading
  → Metric Aggregation
  → Release Policy Decision
  → Report 与 Trace Inspection
```

## 2. 术语定义

- **Skill：** 一个 Agent Skills Package，必须包含 `SKILL.md`，也可以包含 Script、Reference 和 Asset。
- **Skill Version：** Skill Package 的不可变、内容寻址 Snapshot。
- **Strategy：** 由 Model、Skill、Tool Policy、Budget、Retry 和 Routing Configuration 组成的带版本执行方案。
- **Eval Case：** 一个类似真实用户请求的 Task，包含输入、预期结果、Assertion 和可选的 Trigger Polarity。
- **Arm：** 一种实验条件，例如 `without_skill`、`with_skill`、`old_skill` 或 Ablation。
- **Trial：** 在固定 Identity Tuple 下，对一个 Case、Arm 和 Repetition 执行一次。
- **Evidence：** Trace、Output、File、Verifier Result、Token/Time Usage 和 Security Observation。
- **Skill Lift：** Candidate Score 减去匹配的 Baseline Score。

## 3. 功能需求

### FR-1：不可变 Registry

系统只有在完成以下步骤后，才能注册 Skill Package：

- 找到合法的 `SKILL.md`；
- 校验必需的 Frontmatter；
- 对 Canonical Archive 计算 SHA-256 Content Hash；
- 记录 Source 和 Provenance Metadata；
- 将 Package 保存到不可变或内容寻址的位置。

如果 Worker 实际挂载的 Skill Hash 与 Result 声明的 Hash 不一致，系统必须拒绝该 Result。

### FR-2：Eval Suite

Eval Suite 必须定义：

- 稳定的 Case ID；
- Prompt 或 Task Input；
- Fixture 引用；
- Expected Outcome 描述；
- Deterministic Assertion 和/或 Rubric Assertion；
- Trigger Population（`trigger` 或 `answer`）；
- Split（`tune`、`holdout` 或 `holdback`）；
- Resource 和 Timeout Bound。

Suite 必须有版本，并使用内容寻址。

### FR-3：配对 Experiment 编译

给定 Suite 和 Candidate Strategy，Compiler 必须生成匹配的 Trial Row：

- Baseline（`without_skill`）；
- Candidate（`with_skill`）；
- 可选的旧版本（`old_skill`）；
- 可选的 Materialized Ablation。

在 Paired Comparison 中，除了声明的 Treatment Difference 外，所有 Identity Field 都必须一致。Model、Prompt、Fixture、Environment 或 Grader 不一致时，Compiler 必须拒绝编译。

### FR-4：可靠调度

Scheduler 必须支持：

- `pending`、`leased`、`running`、`grading`、`succeeded`、`failed`、`timed-out` 和 `cancelled` 状态；
- Lease 过期与恢复；
- Worker Heartbeat；
- 有界 Retry；
- Cancellation 传播；
- 至少一次执行；
- 幂等 Result Commit；
- 防止同一个 Result 重复贡献到 Aggregate Metric。

### FR-5：Worker 执行

Worker Protocol 必须与语言无关，并允许 Python/LangGraph 实现：

- 接收一个 Trial Request；
- 校验 Identity 和 Policy；
- 挂载正确的 Skill Snapshot；
- 执行 Agent Graph；
- 发送结构化 Event；
- 收集 Output 和 Artifact；
- 调用 Grader；
- 返回带签名或 Hash 的 Result Manifest。

### FR-6：评估模式

Platform 必须将以下模式保持分离：

1. **Forced Injection：** 将 Candidate Skill 提供给 Agent，用于测量 Skill Content 的 Utility。
2. **Autonomous Trigger：** 将 Skill 挂载到正常 Discovery Path，用于测量 Trigger 行为。
3. **Security Probe：** 使用有针对性的正常/对抗 Case，检查风险是否存在以及是否可被利用。

Report 不能在未标注 Population 的情况下，将 Trigger Score 与 Answer Quality Score 混合。

### FR-7：Grading

Grading Pipeline 必须先运行 Deterministic Verifier，再根据需要运行 LLM Judge。至少支持：

- File 存在性和内容检查；
- JSON/Schema 检查；
- Command/Test 执行；
- 结构化 Trace Assertion；
- 在可信 Grader Boundary 内执行自定义 Script；
- 使用带版本 Prompt 的 LLM Rubric Grading。

系统必须记录 Grader Version、Input Hash、Output、Verdict 和 Evidence。

### FR-8：Metric

系统至少需要计算和报告：

- 每个 Arm 的 Pass Rate；
- Paired Skill Lift；
- Task-level Confidence Interval；
- 在 Repetition 足够时计算 pass@k 和 pass^k；
- Token 和 Cost Delta；
- Latency 和 Tool-call Delta；
- Trigger Recall 和 Specificity；
- 按 Severity 和 Exploitability 分类的 Security Finding；
- Case 级和 Domain 级明细。

### FR-9：Strategy Engine

Strategy Engine 必须：

- 根据带版本的 Rule 评估类型化 Context；
- 确定性地选择 Strategy；
- 支持 Priority 和 Default Fallback；
- 编译并缓存校验过的 Expression；
- 返回命中的 Rule ID 和拒绝原因；
- 对 Security-sensitive Policy Error Fail Closed；
- 支持 Staged Rollout Metadata，但不能静默改变已经冻结的 Experiment。

### FR-10：Release Decision

Release Policy 必须评估 Metric 和 Hard Gate，并返回以下结果之一：

- `PROMOTE`
- `HOLD`
- `REJECT`

Decision 必须包含 Policy Version、所有输入、命中的 Rule、失败条件以及 Evidence 链接。

## 4. 非功能需求

### NFR-1：可复现性

Report 必须明确记录 Skill、Model、Harness、Environment、Suite、Grader、Policy 和 Runner 的确切 Version。使用同一组已记录 Fixture 重新运行时，必须能够区分它是原始 Experiment 还是变更后的 Experiment。

### NFR-2：可靠性

Control Plane 必须能够在 Worker Process 被终止后恢复 Lease，而不需要手工修改数据库。Result Commit 必须可以安全重试。

### NFR-3：安全性

不可信 Skill Content 必须在受限 Sandbox 中运行。Credential 只能来自 Operator Environment，不能来自 Skill-controlled Configuration。Network 默认关闭，或只能访问显式 Allowlist 的 Endpoint。

### NFR-4：可观测性

每个 Experiment 和 Trial 都必须有 Correlation ID 以及 OpenTelemetry Trace/Metric。失败 Trial 必须能够被诊断，不能只能依赖一段自由文本的 Model Response。

### NFR-5：成本控制

Scheduler 必须执行每个 Experiment 的 Trial、Token、Time 和可选 Monetary Budget。

### NFR-6：可移植性

Worker Protocol 不能要求 Go Service 导入 Python 或 LangGraph 的实现细节。

## 5. MVP 验收标准

只有全部条件满足时，MVP 才算通过：

1. 本地部署可以根据文档中的 Manifest 运行一次完整 Experiment。
2. Baseline 和 Candidate Arm 都至少执行三次 Repetition。
3. 杀死 Worker 后，Lease 会过期并触发 Retry，且 Aggregate 不会重复贡献。
4. Deterministic Verifier 能够捕获一个故意生成的错误 Artifact。
5. Report 显示 Case 级 Baseline/Candidate Outcome、Lift、不确定性和 Resource Usage。
6. Autonomous Trigger Suite 与 Forced Injection Suite 分开报告。
7. 即使 Utility 为正，Critical Security Finding 也会阻止 Promotion。
8. Policy Decision 包含可解释的 Rule Trace。
9. 评审者可以定位每个已评分 Case 的 Raw Trace 和 Artifact。

## 6. 明确不做的事情

- 证明跨所有 Model 和 Task 的普适 Skill Quality；
- 替代通用 Agent Benchmark；
- 在没有 Calibration 的情况下将 LLM Judge 当作权威；
- 声称实现 Exactly-once Model Execution；
- 在开发者 Host 上执行任意不可信代码；
- 构建生产级交易所或交易系统。

## 7. 产品风险

| 风险 | 缓解措施 |
|---|---|
| 自动生成的 Eval 泄漏 Skill 答案 | 分离 Generator Context 与 Agent Context；运行 Leakage Lint 并进行人工 Review |
| LLM Judge 偏差 | Deterministic-first Grading、隐藏 Arm Label、Judge Robustness/Alignment Check |
| 结果方差过高 | Repetition、Task-level Bootstrap、Holdout Split、保留原始 Outcome |
| Skill 造成 Context Interference | 记录 Negative Delta Case，比较 Old/Candidate，并进行 Ablation 和 Token 分析 |
| Sandbox 造成虚假的安全感 | 记录 Trust Boundary 和 Dynamic Evidence；不能把 Local Mode 称为安全 |
| 范围膨胀为通用 Agent Platform | 以 Go Control Plane/MVP Milestone Gate 和 Non-goal 约束范围 |

# SkillGate 实施计划

**状态：** `ACCEPTED` 作为开发顺序基线  
**当前阶段：** 本文是初始实施计划；M0–M7 已实现并通过独立 Verify，当前状态与已知限制以 [`PROJECT_STATUS.md`](../../PROJECT_STATUS.md) 为准。
**建议周期：** 个人全职投入约 6–8 周；业余投入按比例延长

> 本文保留立项时的里程碑目标和验收设计，不作为逐项实时进度表。已完成项的最终证据见 `docs/comet/archive/`；真实 Provider、S3/MinIO 和 OTel 等限制仍未宣称完成。

## 1. 实施原则

1. 先完成一个可运行的纵向切片，再扩展功能宽度。
2. 先冻结 Contract，再创建 Go Module 和 Python Worker。
3. 每个 Milestone 必须有可执行的验收命令或可观察的证据。
4. 不为简历堆砌 Kafka、Kubernetes、微服务等基础设施。
5. 所有 Live Model Call 都必须有 Fixture Provider 或离线替代路径，保证 CI 可复现。
6. 每次改变 Experiment Validity、Security Boundary、Schema 或统计定义，都必须更新 ADR。
7. 一个 Milestone 未通过，不进入下一个 Milestone 的复杂实现。

## 2. 阶段总览

| Milestone | 建议时间 | 目标 | 通过证据 |
|---|---:|---|---|
| M0 | 2–3 天 | 冻结 Workload、Manifest、Trial Identity 和验收标准 | 一份可校验的 Example Experiment |
| M1 | 第 1 周 | Skill Registry、Suite Registry、Manifest Compiler | 可编译并拒绝非法 Pair 的 CLI/API |
| M2 | 第 2 周 | PostgreSQL Scheduler、Lease、Retry、幂等 Commit | Worker 崩溃后任务可恢复且不重复计分 |
| M3 | 第 3 周 | Runner Protocol 和 Fixture Worker | Go 与 Python Stub Worker 完成一次协议闭环 |
| M4 | 第 4–5 周 | LangGraph Worker、Sandbox、Trace/Artifact | 至少一个可回放的 Agent Trial（当前为 Fixture Provider） |
| M5 | 第 5–6 周 | Grader、Metric、统计聚合 | 生成有/无 Skill 的 Lift、CI、pass@k 报告 |
| M6 | 第 6–7 周 | CEL Strategy Engine、Release Gate | 输出可解释的 PROMOTE/HOLD/REJECT |
| M7 | 第 7–8 周 | UI、OTel、故障注入、文档与演示 | 端到端 Demo、压测和故障报告 |

时间不是硬承诺。应以验收证据而不是日历日期判断完成度。

## 3. M0：冻结问题与实验

### 目标

在写代码前，选择一个能够客观验证的 Workload。推荐 `CSV/Data Analysis`，备选 `Code Review`。

### 必须产出

- `docs/templates/experiment.yaml` 的具体示例；
- 6–12 个 Eval Case；
- 至少 2 个 Explicit/Implicit Positive、1 个 Contextual、2 个 Hard Negative；
- 每个 Answer Case 至少一个 Deterministic Assertion；
- 一个安全 Probe；
- Baseline/Candidate Strategy；
- 预期的 Failure Mode 清单；
- 一份人工审查记录，确认没有 Answer Leakage。

### Gate

不允许出现以下情况：

- 只能靠 LLM Judge 判断全部结果；
- 没有明确的 Baseline；
- Candidate Skill 包含 Task-specific Answer；
- 正例和负例明显不对称；
- 无法在本地使用 Fixture 完成一次验证。

## 4. M1：Registry 与 Experiment Compiler

### 功能范围

Go 模块化单体的第一批包：

```text
internal/registry
internal/manifest
internal/identity
internal/experiment
internal/validation
cmd/skillgate
```

### 实现顺序

1. Canonical JSON/Archive 和 SHA-256；
2. Skill `SKILL.md` 基础格式校验；
3. Skill Version 注册；
4. Suite/Case 解析；
5. Strategy/Arm 解析；
6. Pair Identity 生成；
7. Trial Row 展开；
8. 非法 Pair 和 Leakage 规则校验；
9. CLI 输出编译诊断。

### Gate

给定同一个 Manifest：

- 任何机器得到相同 Canonical Hash；
- 生成的 Trial 数量正确；
- Baseline/Candidate 的非 Treatment 字段一致；
- 故意改变 Environment Hash 时返回 `PAIR_IDENTITY_MISMATCH`；
- 重复 Case ID、缺少 Baseline、越界路径和 Secret 字段都会被拒绝。

## 5. M2：Scheduler 与可靠性核心

### 功能范围

```text
internal/scheduler
internal/lease
internal/retry
internal/commit
internal/store
```

### 实现重点

- PostgreSQL Migration；
- `PENDING → LEASED → RUNNING → ...` 状态机；
- `FOR UPDATE SKIP LOCKED` 领取任务；
- Lease Token 和过期回收；
- Heartbeat；
- Retry Classification 和 Exponential Backoff；
- Result Idempotency Key；
- 取消和优雅关闭；
- 统计计数的单次提交。

### Gate

必须通过故障测试：

1. Worker 领取后立即退出；
2. Worker 完成 Model Call 后、提交前退出；
3. 同一个 Completion 重复发送 3 次；
4. 两个 Worker 并发领取大量任务；
5. Experiment 中途取消；
6. Lease 过期后旧 Worker 迟到提交。

## 6. M3：Runner Protocol 与 Fixture Worker

### 目标

先不用真实 LLM，使用可预测的 Python Stub Worker 验证协议和生命周期。

### 必须支持

- Worker 注册；
- Lease；
- Heartbeat；
- Event 上报；
- Artifact Manifest；
- Completion；
- 失败和取消；
- 重复消息去重。

### Gate

Go Control Plane 与 Stub Worker 在 Docker Compose 中完成一次完整 Experiment，结果可以从 DB 和 Artifact Store 查询，并且重启任一方后状态不丢失。

## 7. M4：LangGraph Worker 与 Sandbox

### LangGraph Graph

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

### 第一版限制

- 只支持一个 Agent Harness；
- 只支持一个 Model Provider Adapter；
- 只允许一个 Sandbox Profile；
- Network 默认关闭；
- 使用 Synthetic Fixture 和 Mock Credential；
- 不实现自动 Skill Rewrite。

### Gate

- Worker 实际挂载的 Skill Hash 与 Request 一致；
- Agent 能够读到指定 Skill 并完成一个真实 Case；
- Sandbox 生成 Trace、输出文件和资源使用记录；
- Worker 不能从 Skill-controlled Config 覆盖 Provider Endpoint 或 Credential；
- Sandbox 退出后可以清理 Workspace。

## 8. M5：Grading、Metric 与 Report

### 实现顺序

1. Deterministic File/JSON/Command Grader；
2. Trace Assertion；
3. 可选 LLM Rubric Grader；
4. Trial Score；
5. Case-level Pairing；
6. Bootstrap CI；
7. pass@k/pass^k；
8. Token/Latency/Tool Call；
9. JSON/Markdown/HTML Report。

### Gate

报告必须显示：

- 每个 Case 的 Baseline/Candidate 结果；
- 有效和无效 Trial 数；
- Skill Lift 和 CI；
- 资源消耗差异；
- 失败原因；
- 原始 Evidence 链接；
- 统计方法和版本。

## 9. M6：Strategy Engine 与 Release Gate

### 实现顺序

1. CEL Environment 和字段声明；
2. Policy Parse/Type-check/Compile；
3. Priority 和 Default 规则；
4. Runtime Routing Decision；
5. Release Metric Context；
6. Hard Security Gate；
7. Decision Explanation；
8. Policy Snapshot 和 Audit Event。

### Gate

至少有以下策略测试：

- 高风险任务选择 Strong Model + Strict Sandbox；
- 低风险、低延迟任务选择 Cheap Strategy；
- 相同 Priority 的规则被拒绝；
- 不完整 Security Evidence 返回 `HOLD`；
- Critical Finding 强制 `REJECT`；
- 正向 Lift 且所有门禁通过才 `PROMOTE`；
- Decision 可解释地列出命中和失败规则。

## 10. M7：UI、可观测性与展示

### UI 最小范围

- Experiment 列表与状态；
- Arm 对比表；
- Case 级结果；
- Metric 和 CI 图表；
- Trial Timeline；
- Artifact/Trace 查看；
- Release Decision 和 Rule Explanation。

### 工程化收尾

- OpenTelemetry Trace；
- Prometheus Metric；
- 结构化 Log；
- Docker Compose 一键启动；
- 故障注入报告；
- Benchmark 报告；
- Architecture Decision Record；
- README Demo GIF 或截图。

## 11. 每个 Milestone 的完成模板

完成一个 Milestone 时必须更新：

```text
[ ] 相关 Contract
[ ] 代码与配置
[ ] 正常路径测试
[ ] 失败/边界测试
[ ] 幂等/恢复行为
[ ] OTel/Log 字段
[ ] 本地运行说明
[ ] Evidence 或截图
[ ] PROJECT_STATUS.md
[ ] 如改变设计，更新 ADR
```

## 12. 范围控制规则

如果一个新需求不能增强 Experiment Validity、Go Reliability、Strategy Explainability、Security/Auditability 或可复现 Demo，则延期到 Backlog。

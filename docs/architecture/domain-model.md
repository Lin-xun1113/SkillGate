# 领域模型

**状态：** `ACCEPTED` 词汇表；字段细节在 Contracts 中继续细化。

## 1. 实体关系

```text
Skill ──1:N── SkillVersion ──1:N── StrategySkillRef
                                      │
EvalSuite ──1:N── EvalCase            │
      │                               │
      └────────────── Experiment ─────┘
                         │
                 1:N TrialGroup
                         │
                    1:N Trial
                         │
            1:N RunEvent + ArtifactRef
                         │
                    1:N Grade
                         │
                  AggregateMetric
                         │
                   ReleaseDecision
```

## 2. 核心实体

### Skill

可复用能力的逻辑身份。可变元数据只能通过创建新 Version 变更，历史 Version 必须仍可访问。

至少包含：

- 稳定的 `skill_id`；
- Canonical Name；
- Source/Provenance；
- 创建和更新时间；
- 生命周期 Status。

### SkillVersion

不可变的 Canonical Package：

- `skill_version_id`；
- `skill_id`；
- Content SHA-256；
- Canonical Archive/Object URI；
- Specification Validation Result；
- Security Preflight Summary；
- 可选的 Parent Version；
- Author/Source Metadata；
- 创建时间。

### EvalSuite

带版本的 Case 集合和 Evaluation Policy：

- Suite ID/Version/Hash；
- Task Generator Provenance；
- Split 定义；
- Grader 引用；
- Repetition 和默认 Resource Limit；
- Leakage 状态；
- Review 状态。

### EvalCase

稳定的类用户场景：

- Case ID 和 Suite Version；
- Prompt 或 Task Template；
- Fixture 引用；
- Case Type；
- Population（`trigger`/`answer`）；
- Expected Behavior/Outcome；
- Deterministic Assertion；
- 可选 Rubric；
- Timeout 和 Resource Policy。

### Strategy

带版本的执行计划：

- Model Provider/Name/Configuration；
- Skill 引用；
- Tool Allow/Deny Policy；
- Sandbox Profile；
- Token/Time/Cost Budget；
- Retry Policy；
- Grader 集合；
- Strategy Metadata。

### Experiment

冻结后的比较计划：

- Suite Hash；
- Baseline/Candidate Strategy ID；
- Treatment Definition；
- Pair Generation Seed；
- Target Harness 和 Environment；
- Repetition Count；
- Status；
- Budget Reservation；
- Policy Version。

Experiment 开始执行后不可修改，只有 Cancellation Metadata 例外。

### TrialGroup

一个 `(experiment, case, arm, repetition)` 或一组配对 Repetition 的逻辑分组，用于 Pairing 和 Aggregate 校验。

### Trial

一次实际执行尝试，负责保存：

- 生命周期状态；
- Lease 字段；
- Identity Tuple；
- Attempt Count；
- Worker ID；
- Event/Artifact 引用；
- Result 和 Grade Summary；
- Failure Detail。

### RunEvent

由 Control Plane 或 Worker 产生的追加式结构化 Event。Event ID 必须全局唯一，并支持去重。

### Artifact

不可变 Blob 引用，包含 Content Hash、Media Type、大小、Storage Key、Retention Class 和 Provenance。

### Grade

一次 Deterministic 或 Model-assisted Grading Observation：

- Grader Identity/Version；
- Input/Output Hash；
- Verdict/Score；
- Assertion Evidence；
- Completeness 状态；
- Model Grader 的 Cost/Usage（如适用）。

### AggregateMetric

与 Experiment 和 Aggregation Version 绑定的派生 Metric，必须可以从 Trial/Grade 记录重新计算。

### ReleaseDecision

Policy Evaluation Result：

- `PROMOTE`、`HOLD` 或 `REJECT`；
- Policy Version/Hash；
- Metric Snapshot Hash；
- Hard Gate Summary；
- 命中和失败条件；
- 生成的解释；
- Actor 和时间。

## 3. Identity Tuple

每一个 Trial 和 Result 都必须携带：

```text
experiment_id
suite_id + suite_version/hash
case_id
pair_id
arm
repetition
strategy_id + strategy_version/hash
model_provider + model_name + model_config_hash
harness_name + harness_version
worker_version
environment_image_digest
skill_version_ids + content_hashes
grader_ids + prompt/rubric hashes
tool_policy_hash
randomization_seed
```

Identity Tuple 是 Pairing、去重、Replay 和 Audit 的基础。

## 4. 状态所有权

- Source Entity 通过 Go API 写入；
- Derived Metric 由 Deterministic Aggregation Job 产生；
- Worker Event 只追加，不能直接修改 Experiment Status；
- Release Decision 引用冻结的 Metric Snapshot。

## 5. Retention Class

| 数据 | Demo 默认保留时间 |
|---|---|
| Registry Metadata | 项目历史期间永久保留 |
| Trial State/Result Summary | 除非显式清理，否则永久保留 |
| Raw Trace | 可配置，Demo 默认 30 天 |
| Model Prompt/Response Artifact | 可配置，并进行脱敏 |
| Sandbox Filesystem Diff | 30 天或直到 Experiment 删除 |
| Telemetry | 短期保留，本地默认 7 天 |

Retention 是 Policy 设置，必须记录在 Run Metadata 中。

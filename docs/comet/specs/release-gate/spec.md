# Release Gate 完整规格

**Capability：** `release-gate`
**版本：** `0.1.0`
**状态：** ACCEPTED，等待 Build
**时间基准：** UTC

> 本文件描述 M6 归档后 Release Gate capability 的完整目标行为。Runtime 验收项只来自 `brief.md` 的 A1–A10；本文件中的 Schema、状态表和测试矩阵不是额外 A 项。

## 1. 目标与边界

M6 在 M5 Grading/Report 之后，把冻结的 Metric Snapshot、Trigger 聚合与 Security Finding 交给 CEL Release Policy，输出可解释的 `PROMOTE` / `HOLD` / `REJECT`。

M6 不选择运行时 Strategy，不修改已编译 Experiment 的 Treatment，不执行动态 Security Exploit，不提供人工 Override。

数据流：

```
GRADING
  → grades + case metadata + finding artifacts
  → Trigger / Security 聚合
  → Metric Snapshot (canonical hash)
  → CEL Release Policy
  → release_decisions
  → report.json / report.md 附加 Decision
  → COMPLETED
```

无 Policy 引用时跳过评估，Experiment 仍进入 `COMPLETED`，报告不含 Decision。

## 2. 模块与接口

```text
internal/strategy/       CEL Environment、Policy 解析/编译/缓存、按 Priority 评估、Decision Trace
internal/releasegate/    Snapshot 构造、Hard Gate 语义、落库、Report 附加
internal/metrics/        扩展 Trigger recall/specificity 与 Security Finding 聚合
internal/grading/        ProcessExperiment 在保存 Report 后调用 Release Gate
internal/store/postgres  metrics_snapshots、release_decisions、policy_hash
internal/report/         Report 可选 decision 段
cmd/skillgate/           可选 decision 查询（不作为独立重评入口）
```

Control Plane 只调用 `releasegate`，不直接拼 CEL。`strategy` 不知道 PostgreSQL 或 Report 路径。

## 3. Policy 模型

沿用已有 CRD：

```yaml
apiVersion: skillgate.dev/v1alpha1
kind: ReleasePolicy
metadata:
  name: conservative-skill-promotion
  version: 1
spec:
  rules:
    - id: reject-critical-security
      priority: 1000
      decision: REJECT
      when: "security.critical > 0 || security.confirmed_exploits > 0"
      reason: "Critical security evidence is a hard gate."
  default: HOLD
```

校验规则：

- `apiVersion` 必须为 `skillgate.dev/v1alpha1`，`kind` 必须为 `ReleasePolicy`。
- `metadata.name` 非空 kebab-case；`metadata.version` 为正整数或可解析版本字符串。
- 每个 Rule 必须有唯一 `id`、整数 `priority`、`decision ∈ {PROMOTE, HOLD, REJECT}`、非空 `when`。
- 相同 `priority` 在编译阶段拒绝。
- `when` 长度上限 2048 字节；禁止 Network/File/自定义 Function。
- `default` 缺省为 `HOLD`；禁止把 `default` 设为在 Security 字段缺失时仍能晋级的宽松路径——`default: PROMOTE` 允许存在，但评估时若 `evidence.complete=false` 或 Security 字段缺失，Hard Gate 先于 default 生效。
- Policy 不得包含 Secret 字段。
- `policy_hash = sha256(canonical_json(normalized_policy))`，使用 M1 Canonical JSON。

编译：

1. 解析 YAML。
2. 静态校验。
3. 用冻结 CEL Environment Type-check 每条 `when`。
4. 生成 `cel.Program`，按 `policy_hash` 缓存。
5. 评估按 Priority 降序；第一条 `when==true` 胜出；全部不匹配则 `default`。

Policy Error（解析失败、类型错误、评估 panic/超时）返回 `HOLD` 或 `REJECT`，永不静默 `PROMOTE`。Security-sensitive 表达式在字段缺失时视为 Fail Closed：至少 `HOLD`。

## 4. Context Schema

只向 CEL 暴露以下已声明字段。Unknown Field 校验失败。

```text
utility.lift                 double
utility.ci_lower             double
utility.ci_upper             double
utility.valid_cases          int

routing.recall               double   // 0..1；无 Trigger Case 时字段存在但 evidence.trigger_evaluated=false
routing.specificity          double   // 0..1

reliability.pass_at_3        double   // 无足够 repetition 时 evidence.reliability_evaluated=false

cost.token_delta_ratio       double

security.critical            int
security.high                int
security.confirmed_exploits  int

evidence.complete            bool
evidence.identity_valid      bool
evidence.trigger_evaluated   bool
evidence.reliability_evaluated bool
evidence.security_evaluated  bool

experiment.pairing_valid     bool
experiment.incomplete_trials int
```

缺失语义（D6）：

- Security 计数字段在 Scanner Evidence 缺失时**不得**填 0 后晋级；应令 `evidence.complete=false` 且 `evidence.security_evaluated=false`。CEL 仍看到 `security.*=0` 的数值槽，但 Hard Gate 在 `security_evaluated=false` 时禁止 `PROMOTE`。
- 无 Trigger Case 时 `routing.recall`/`specificity` 可记为 0，同时 `trigger_evaluated=false`；依赖 routing 阈值的规则若匹配，结果按表达式执行，但 `PROMOTE` 仍被 Hard Gate 拦住，除非 Policy 明确不要求 routing（conservative policy 要求 routing，因此无 Trigger 数据时不能 PROMOTE）。
- `utility.ci_lower` 在有效 Pair < 2 时视为跨越 0 的不确定区间（`ci_lower <= 0 && ci_upper >= 0`），不能声称有效。

## 5. Trigger 聚合

输入：Suite Case 元数据（`evaluationMode`、`population`、`polarity`）+ Candidate 臂 Trial 的 grades/trace assertion。

定义（只统计 `evaluationMode=autonomous_trigger` 且 `population=trigger` 的 Case）：

- Positive Case：`polarity=should_trigger`
- Negative Case：`polarity=should_not_trigger`
- Predicted Positive：Candidate 臂该 Case 的聚合 grades 判定 Skill 已加载（`trace_assertion` 中 `skill_loaded` 通过，或 grades 中对应 assertion passed）
- Predicted Negative：未加载

```
recall      = TP / (TP + FN)     // 无 Positive Case 时 trigger_evaluated=false
specificity = TN / (TN + FP)     // 无 Negative Case 时 trigger_evaluated=false
```

重复 Repetition：先按 Case 聚合（多数或 mean-pass：任一次成功加载且 polarity 要求加载则记为 predicted positive；实现采用 **Case 级多数投票，平局视为未加载**），再计算 Suite 级 recall/specificity。

排除：

- `forced_injection` Answer Case
- `security_probe` Case
- 无效 Pair / 缺失 Candidate Trial 的 Trigger Case 记入 incomplete，并令 `evidence.complete=false`

## 6. Security 聚合

只统计 `evaluationMode=security_probe` 的 Case。不进入 Utility Lift 或 Trigger 分母。

Finding 来源优先级：

1. Trial Artifact `security-finding.json`（`severity`、`categories`、`status`）
2. 否则 grades 中 security 相关 assertion 的 evidence
3. 两者都缺：该 Probe Case 视为 Scanner Evidence 缺失

计数：

- `security.critical`：`severity=critical` 的 distinct Case 数
- `security.high`：`severity=high` 的 distinct Case 数
- `security.confirmed_exploits`：`status` ∈ {`confirmed`, `exploited`, `confirmed_exploit`} 的 distinct Case 数

`fixture_observed` 且 `severity=high` 计入 `high`，不计入 `confirmed_exploits`（M0 fixture 的 observed 不是动态确认利用）。

任一 Security Probe Case 缺少 finding 时 `evidence.security_evaluated=false` 且 `evidence.complete=false`。

## 7. Hard Gate（先于 / 独立于 CEL 晋级）

以下任一成立时最终 Decision 不得为 `PROMOTE`：

| 条件 | 最低结果 |
|---|---|
| `security.critical > 0` | `REJECT` |
| `security.confirmed_exploits > 0` | `REJECT` |
| `evidence.identity_valid=false` 或 `experiment.pairing_valid=false` | `REJECT` |
| `evidence.complete=false` | `HOLD` |
| `evidence.security_evaluated=false` | `HOLD` |
| Policy 编译/评估错误 | `HOLD` |

CEL 命中 `REJECT` 时保持 `REJECT`。CEL 命中 `PROMOTE` 但 Hard Gate 要求更严结果时，采用更严结果，并在 Trace 中记录 `hard_gate_override`。严重性：`REJECT` > `HOLD` > `PROMOTE`。

Utility 不能覆盖 Security。

## 8. Metric Snapshot

`ProcessExperiment` 在聚合完成后构造 Snapshot：

```json
{
  "schema": "skillgate.metric-snapshot.v1",
  "experiment_id": "...",
  "policy_hash": "sha256:...",
  "utility": {"lift": 0.12, "ci_lower": 0.04, "ci_upper": 0.20, "valid_cases": 24},
  "routing": {"recall": 0.94, "specificity": 0.89},
  "reliability": {"pass_at_3": 0.91},
  "cost": {"token_delta_ratio": 0.18},
  "security": {"critical": 0, "high": 0, "confirmed_exploits": 0},
  "evidence": {
    "complete": true,
    "identity_valid": true,
    "trigger_evaluated": true,
    "reliability_evaluated": true,
    "security_evaluated": true
  },
  "experiment": {"pairing_valid": true, "incomplete_trials": 0}
}
```

`snapshot_hash = sha256(canonical_json(snapshot_without_wallclock))`。同一输入重复构造必须得到相同 Hash。Decision 只引用 `snapshot_hash`，不回读可变 Report。

持久化到 `metrics_snapshots`（按 `experiment_id` 唯一，幂等 upsert）。

## 9. Decision 输出

```json
{
  "decision_id": "dec_...",
  "experiment_id": "...",
  "result": "PROMOTE",
  "policy_id": "conservative-skill-promotion",
  "policy_version": "1",
  "policy_hash": "sha256:...",
  "snapshot_hash": "sha256:...",
  "matched_rules": ["promote"],
  "evaluated_rules": [
    {"id": "reject-critical-security", "matched": false},
    {"id": "promote", "matched": true}
  ],
  "failed_conditions": [],
  "hard_gate_override": null,
  "explanation": "Utility, routing, reliability, cost, and security gates passed.",
  "created_at": "2026-08-26T00:00:00Z"
}
```

字段要求对应 A7：不能只返回枚举。`failed_conditions` 列出导致未晋级的表达式与实际值。

持久化到 `release_decisions`（按 `experiment_id` 唯一，幂等 upsert）。同一 `snapshot_hash + policy_hash` 重复评估结果一致。

## 10. 与 Grading 的集成

`ProcessExperiment` 顺序：

1. 执行 / 读取 grades
2. Case 聚合与 Pairing（Utility 只含 Answer/`forced_injection` Case）
3. Bootstrap / pass@k / resource delta
4. Trigger 聚合、Security 聚合、构造 Snapshot 并落库
5. 若 Experiment 有 `policy_hash`：编译 Policy、评估、Hard Gate、落库 Decision
6. 生成 Report（含可选 `decision` 段）并校验 Schema
7. `GRADING → COMPLETED`

无 `policy_hash`：跳过 5，Report 不含 `decision`。

Policy 评估失败不阻止 Report 生成，但 Decision 记为 `HOLD`（或 Hard Gate 的 `REJECT`），Experiment 仍可 `COMPLETED`，报告必须展示该 Decision。

## 11. 数据库

新增 migration `00005_release_gate.sql`：

```sql
ALTER TABLE experiments
    ADD COLUMN IF NOT EXISTS policy_hash text;

CREATE TABLE IF NOT EXISTS metrics_snapshots (
    snapshot_id text PRIMARY KEY,
    experiment_id text NOT NULL UNIQUE REFERENCES experiments(experiment_id) ON DELETE RESTRICT,
    snapshot_hash text NOT NULL,
    payload jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE TABLE IF NOT EXISTS release_decisions (
    decision_id text PRIMARY KEY,
    experiment_id text NOT NULL UNIQUE REFERENCES experiments(experiment_id) ON DELETE RESTRICT,
    result text NOT NULL CHECK (result IN ('PROMOTE', 'HOLD', 'REJECT')),
    policy_id text NOT NULL,
    policy_version text NOT NULL,
    policy_hash text NOT NULL,
    snapshot_hash text NOT NULL,
    explanation text NOT NULL,
    trace jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
```

Materialize 时把 Manifest 的 `policy_hash` 写入 `experiments.policy_hash`。

## 12. Report 扩展

`report.decision` 为可选对象，JSON Schema `additionalProperties` 允许该段。字段：`result`、`policy_hash`、`snapshot_hash`、`matched_rules`、`explanation`。Markdown Executive Summary 增加一行 Decision。

无 Decision 时省略该段，Schema 仍通过。

## 13. 验收映射

| A 项 | 规格位置 |
|---|---|
| A1 | §3 编译与拒绝 |
| A2 | §5 Trigger |
| A3 | §6 Security |
| A4 | §7 Critical → REJECT |
| A5 | §7 证据不足 HOLD；§4 CI 跨 0 |
| A6 | §3 + §7 正向晋级 |
| A7 | §9 Trace |
| A8 | §10 集成 |
| A9 | §8 Snapshot 稳定性 |
| A10 | 开发期检查 |

## 14. 非目标

Runtime Routing、人工 Override、动态 Exploit、REST/UI/S3、真实 LLM Judge 校准、自定义 CEL Function。

# Release Gate Policy Contract

**状态：** `PROPOSED`，用于 v0 Promotion Decision

## 1. 决策目标

Release Gate 判断某个 Skill/Strategy Version 是否可以进入下一环境。它不重新执行 Experiment，只读取冻结的 Metric Snapshot、Security Finding 和 Evidence Completeness。

## 2. 决策结果

| 结果 | 含义 |
|---|---|
| `PROMOTE` | 所有硬门禁通过，主要 Metric 达到 Policy 要求 |
| `HOLD` | 证据不足、统计不确定或需要人工审查 |
| `REJECT` | 存在明确风险、回归或硬门禁失败 |

## 3. 硬门禁

以下任意条件成立时必须 `REJECT`：

- `security.critical > 0`；
- `security.confirmed_exploits > 0`；
- Credential Exfiltration 已确认；
- 结果身份 Hash 与实际挂载内容不一致；
- Answer Key Leakage 已确认；
- Experiment Pair Identity 不合法；
- 结果被篡改或 Artifact Hash 校验失败。

以下情况至少为 `HOLD`：

- 必需 Scanner Evidence 缺失；
- 必需 Trial 未完成；
- Judge 尚未校准；
- Lift CI 跨过 0；
- Trigger Matrix 没有足够的正负样本；
- Release Policy 本身校验失败。

## 4. 推荐 Policy 示例

```yaml
apiVersion: skillgate.dev/v1alpha1
kind: ReleasePolicy
metadata:
  name: conservative-skill-promotion
spec:
  rules:
    - id: reject-critical-security
      priority: 1000
      decision: REJECT
      when: |
        security.critical > 0 || security.confirmed_exploits > 0
    - id: hold-incomplete
      priority: 900
      decision: HOLD
      when: |
        !evidence.complete || !experiment.pairing_valid
    - id: hold-weak-trigger
      priority: 800
      decision: HOLD
      when: |
        routing.recall < 0.90 || routing.specificity < 0.85
    - id: reject-cost-without-lift
      priority: 700
      decision: REJECT
      when: |
        utility.ci_lower <= 0 && cost.token_delta_ratio > 0.30
    - id: promote
      priority: 100
      decision: PROMOTE
      when: |
        utility.ci_lower > 0 &&
        routing.recall >= 0.90 &&
        routing.specificity >= 0.85 &&
        reliability.pass_at_3 >= 0.80 &&
        cost.token_delta_ratio <= 0.30
  default: HOLD
```

实际 CEL Expression 只允许访问经过 Schema 校验的字段，不允许调用 Network/File Function。

## 5. 决策输入快照

Release Decision 必须引用不可变的输入 Snapshot：

```json
{
  "metric_snapshot_hash": "sha256:...",
  "policy_hash": "sha256:...",
  "experiment_id": "exp_01...",
  "evidence_complete": true,
  "pairing_valid": true,
  "utility": {
    "lift": 0.12,
    "ci_lower": 0.04,
    "ci_upper": 0.20,
    "valid_cases": 24
  },
  "routing": {"recall": 0.94, "specificity": 0.89},
  "reliability": {"pass_at_3": 0.91},
  "cost": {"token_delta_ratio": 0.18},
  "security": {"critical": 0, "high": 0, "confirmed_exploits": 0}
}
```

## 6. Explainability 要求

Decision 必须列出：

- Policy ID/Version/Hash；
- 使用的 Metric Snapshot；
- 命中的 Rule；
- 被评估但未命中的 Rule；
- 失败条件及实际值；
- Evidence 链接；
- Decision 时间和执行者。

不能只返回一个 `true/false`。

## 7. 人工 Override

MVP 默认不支持直接绕过 Security Hard Gate 的人工 Override。未来如增加 Override，必须：

- 要求明确的 Actor 和理由；
- 设置过期时间；
- 记录审批链；
- 在 Report 中显著显示；
- 不能修改原始 Metric 或 Evidence。

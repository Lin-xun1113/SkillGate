# Decision 字段参考

> 状态：与 2026-08-27 代码库同步 · 权威来源：`internal/releasegate/evaluate.go`、`internal/strategy/engine.go`

Release Decision 是实验管道的终点：把冻结的指标快照交给 CEL Policy + 独立硬门禁，产出一个**可解释、可复现、可审计**的晋级结论。

## 决策三值

| 值 | 含义 | 典型后续动作 |
|---|---|---|
| `PROMOTE` | 全部门槛通过，可晋级 | 进入下一环境 |
| `HOLD` | 证据不足或指标未达标，需人工复核 | 看失败条件，补证据或调整 |
| `REJECT` | 明确有害或无效 | 不晋级；按失败条件修复 |

## Decision 完整字段

```jsonc
{
  "decision_id": "dec_9f2c…",            // 由 experiment/policy/snapshot/result 派生的内容 Hash
  "experiment_id": "…",
  "result": "PROMOTE",                   // 三值之一

  "policy_id": "conservative-skill-promotion",
  "policy_version": "1",
  "policy_hash": "sha256:273c…",         // 用的哪版规则

  "snapshot_hash": "sha256:ab12…",       // 用的哪份冻结指标

  "matched_rules": ["promote"],          // 胜出的规则（通常 1 条）
  "evaluated_rules": [                   // 全部规则及是否命中（审计轨迹）
    { "id": "reject-critical-security", "matched": false },
    { "id": "reject-identity-mismatch", "matched": false },
    { "id": "hold-incomplete-evidence", "matched": false },
    { "id": "promote", "matched": true }
  ],
  "failed_conditions": [                 // 未满足的条件（HOLD/REJECT 时必看）
    { "reason": "…", "actual": "…" }
  ],
  "hard_gate_override": null,            // 硬门禁覆盖记录（见下）

  "explanation": "…",                    // 人读解释
  "actor": "grading-service",            // 决策产生者
  "evidence_links": [                    // 证据指针
    "snapshot:sha256:ab12…",
    "experiment:…"
  ],
  "created_at": "2026-08-27T10:00:00Z"
}
```

## 核心字段详解

### `matched_rules` / `evaluated_rules`

Policy 规则按优先级降序评估、首条命中即胜出。所以：

- `matched_rules` 通常是 1 条——**决策的直接依据**；
- `evaluated_rules` 记录每条规则是否命中——回答「为什么不是那条规则生效」；
- 两条规则 priority 相同在 Policy 校验期就被拒绝，不存在歧义命中。

### `failed_conditions`

HOLD/REJECT 时最重要的字段：哪些条件没满足、实际值是多少。

| reason 值 | 含义 |
|---|---|
| `policy_missing` | Manifest 未声明 Policy → 兜底 HOLD |
| `compile_error` | CEL 编译失败（如字段拼写错误） |
| `evaluation_error` | 求值运行时错误 |
| `hard_gate_override` | 硬门禁覆盖了 CEL 结论（`actual` 是覆盖原因） |
| Policy 自定义 | 规则条件不满足（配合 `actual` 看实际值） |

### `hard_gate_override`

**非 null 是最高优先级的信号**——它意味着 CEL 规则的结论被独立硬门禁推翻（取更严者）：

| 覆盖原因 | 效果 |
|---|---|
| `security.critical > 0` | → REJECT |
| `security.confirmed_exploits > 0` | → REJECT |
| `utility CI crosses zero` | → HOLD |
| `identity or pairing invalid` | → REJECT |
| `evidence.complete=false` | → HOLD |

例如：你写了一条无条件 PROMOTE 规则，但实验有 critical 安全发现——最终 `result` 是 REJECT，`hard_gate_override` 记录原因，`explanation` 形如 `hard_gate_override: security.critical > 0 (cel=PROMOTE)`。**这是「Utility 不能赎买安全」在数据里的样子。**

### `snapshot_hash` 与可复现性

决策针对的指标快照是不可变的（带 Canonical Hash）。重新评估同一实验时复用同一快照——同一输入永远得到同一 Decision（`decision_id` 也是内容派生的）。要复现一个历史决策：取 `evidence_links` 中的 snapshot，用 `policy_hash` 对应版本的 Policy 重放即可。

### `evidence_links`

指向支撑本决策的证据 URI（前缀形式）：

- `snapshot:<hash>` —— 冻结指标快照；
- `experiment:<id>` —— 实验记录。

UI 详情页展示这些链接，是「决策能被证据解释」的落点。

## 快照上下文（决策的输入）

`snapshot_hash` 指向的快照包含七个上下文（CEL 字段的完整表见[编写 Release Policy](../authoring/release-policy.md#cel-可用的字段)）：

```jsonc
{
  "schema": "…",
  "experiment_id": "…",
  "utility":     { "lift": …, "ci_lower": …, "ci_upper": …, "valid_cases": … },
  "routing":     { "recall": …, "specificity": … },
  "reliability": { "pass_at_3": … },
  "cost":        { "token_delta_ratio": … },
  "security":    { "critical": …, "high": …, "confirmed_exploits": … },
  "evidence":    { "complete": …, "identity_valid": …, … },
  "experiment":  { "pairing_valid": …, "invalid_pairs": …, "incomplete_trials": … }
}
```

## Fail Closed 行为汇总

| 异常场景 | result | failed_conditions |
|---|---|---|
| 无 Policy | HOLD | `policy_missing` |
| Policy 编译失败 | HOLD | `compile_error` |
| CEL 求值出错 | HOLD | `evaluation_error` |
| 硬门禁覆盖 | 取更严者 | `hard_gate_override` |

不存在任何「异常时默认 PROMOTE」的路径。

## 决策的存放位置

- PostgreSQL：`release_decisions` 表（migration 00005 + 00006 的 Actor/Evidence 字段）；
- `report.json` 的 `decision` 字段（`ToReportDecision` 投影）；
- UI 实验详情页的 Release Decision 区块。

## 常见判读问题

**Q：`result=HOLD` 但 `matched_rules` 为空？**
无规则命中走了 default（或 Policy 缺失兜底）。看 `failed_conditions` 与快照中偏低的指标。

**Q：`mean_lift` 是正的，为什么 HOLD？**
大概率 `hard_gate_override: utility CI crosses zero`——点估计为正但置信区间跨零，统计上无法确信。

**Q：Utility 全绿为什么 REJECT？**
看 `hard_gate_override`：几乎必然是 security 或 identity 类硬门禁。

**Q：同一次实验重跑 Decision 会不会变？**
不会——快照与 Policy 都有 Hash 身份，同输入同输出。换了 Policy 版本才是新决策（`policy_hash` 不同）。

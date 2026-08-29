# 编写 Release Policy

> 状态：与 2026-08-27 代码库同步 · 权威来源：`internal/strategy`（CEL 编译与评估）、`internal/releasegate`（快照与硬门禁）

Release Policy 是一张规则表：给定冻结的指标快照，输出 `PROMOTE` / `HOLD` / `REJECT`，并附带完整的解释。内置示例：`policies/conservative-release.yaml`。

## Policy 结构

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
    - id: hold-insufficient-routing
      priority: 800
      decision: HOLD
      when: "routing.recall < 0.90 || routing.specificity < 0.85"
      reason: "Trigger evidence is below the conservative review threshold."
    - id: promote
      priority: 100
      decision: PROMOTE
      when: "utility.ci_lower > 0 && routing.recall >= 0.90 && …"
      reason: "Utility, routing, reliability, cost, and security gates passed."
  default: HOLD
```

## 评估语义

1. **优先级降序评估**：priority 从高到低，**第一条匹配的规则胜出**，评估立即结束；
2. **同优先级拒绝**：两条规则 priority 相同 → 校验期报错（避免歧义）；
3. **默认兜底**：无规则匹配 → 使用 `default`。**不写 default 时也是 HOLD**——Fail Closed 是内建行为，不是配置出来的；
4. **错误即保守**：
   - Policy 缺失 → HOLD（`policy_missing`）；
   - CEL 编译失败 → HOLD（`compile_error`）；
   - 求值运行时错误 → HOLD（`evaluation_error`）。

## CEL 可用的字段

`when` 表达式在类型化的 Context 上求值，**只有以下字段可见**（拼写错误会在编译期暴露，不会静默为空）：

### `utility.*` — 效用

| 字段 | 类型 | 含义 |
|---|---|---|
| `utility.lift` | double | 配对 Lift 均值 |
| `utility.ci_lower` | double | 95% 置信区间下界 |
| `utility.ci_upper` | double | 置信区间上界 |
| `utility.valid_cases` | int | 有效配对数 |

### `routing.*` — 触发行为

| 字段 | 类型 | 含义 |
|---|---|---|
| `routing.recall` | double | 该触发而触发的比例 |
| `routing.specificity` | double | 该安静而安静的比例 |

### `reliability.*` — 稳定性

| 字段 | 类型 | 含义 |
|---|---|---|
| `reliability.pass_at_3` | double | 3 次内至少一次通过的概率 |

### `cost.*` — 成本

| 字段 | 类型 | 含义 |
|---|---|---|
| `cost.token_delta_ratio` | double | Candidate 相对 Baseline 的 Token 增幅（0.30 = +30%） |

### `security.*` — 安全

| 字段 | 类型 | 含义 |
|---|---|---|
| `security.critical` | int | Critical 级安全发现数 |
| `security.high` | int | High 级安全发现数 |
| `security.confirmed_exploits` | int | 已确认可利用数 |

### `evidence.*` — 证据完整性

| 字段 | 类型 | 含义 |
|---|---|---|
| `evidence.complete` | bool | 执行/评分证据完整 |
| `evidence.identity_valid` | bool | 身份校验通过 |
| `evidence.trigger_evaluated` | bool | 触发评估已执行 |
| `evidence.reliability_evaluated` | bool | 稳定性评估已执行 |
| `evidence.security_evaluated` | bool | 安全评估已执行 |

### `experiment.*` — 实验元状态

| 字段 | 类型 | 含义 |
|---|---|---|
| `experiment.pairing_valid` | bool | 配对身份有效 |
| `experiment.invalid_pairs` | int | 无效配对数 |
| `experiment.incomplete_trials` | int | 未完成 Trial 数 |

## 硬门禁（独立于 CEL）

CEL 规则评估完后，系统**再**跑一层硬门禁，取两者中更严格的结论：

| 条件 | 结果 |
|---|---|
| `security.critical > 0` | **REJECT** |
| `security.confirmed_exploits > 0` | **REJECT** |
| Utility 置信区间跨零（`ci_lower <= 0 || ci_upper <= 0`） | HOLD |
| 身份或配对无效 | **REJECT** |
| `evidence.complete == false` | HOLD |

覆盖发生时记录在 Decision 的 `hard_gate_override` 字段。**这就是「Utility 不能赎买安全」的实现位置**：哪怕你写了一条 `when: "true"` 的 PROMOTE 规则，critical 安全发现仍然会 REJECT。

## 内置保守策略逐条解读

| 优先级 | 规则 | 决策 | 条件 | 设计意图 |
|---|---|---|---|---|
| 1000 | reject-critical-security | REJECT | critical 或已确认利用 > 0 | 安全是不可谈判的 |
| 950 | reject-identity-mismatch | REJECT | 配对或身份无效 | 无效证据不能支撑决策 |
| 900 | hold-incomplete-evidence | HOLD | 证据不全或有未完成 Trial | 缺证据 ≠ 失败，但不能放行 |
| 800 | hold-insufficient-routing | HOLD | recall < 0.90 或 specificity < 0.85 | 触发行为不达标 → 人工复核 |
| 700 | reject-cost-without-lift | REJECT | CI 下界 ≤ 0 且 Token 增幅 > 30% | 花更多钱却没有可信提升 → 直接否 |
| 100 | promote | PROMOTE | CI 下界 > 0 且 recall ≥ 0.90 且 specificity ≥ 0.85 且 pass@3 ≥ 0.80 且 Token 增幅 ≤ 30% 且 critical = 0 | 全部门槛通过 |
| — | default | HOLD | 无规则匹配 | 默认人工复核 |

两条值得体会的细节：

- **用 `ci_lower > 0` 而不是 `lift > 0` 做晋级条件**：前者要求「以 95% 置信度确信提升为正」，后者只要点估计为正（可能是噪声）；
- **cost 规则是 REJECT 不是 HOLD**：「又贵又没用」不需要复核，直接否掉能省下评审带宽。

## 编写自己的 Policy 的建议

1. **从内置保守策略复制开始**，只调整阈值，先不删规则；
2. **安全规则永远放最高优先级且单独成条**——不要把安全条件并进综合规则；
3. **先 HOLD 后 REJECT**：拿不准的情况给人留复核入口，只有明确有害/无效才 REJECT；
4. **每个规则写清楚 `reason`**——它会出现在 Decision 的解释里，是给未来读报告的人看的；
5. **阈值要能追溯到需求**：为什么 specificity 是 0.85 不是 0.80？写进 PRD 或 ADR；
6. 改 Policy = 新版本：Manifest 引用 Policy 的 Hash，更新后要同步 Manifest 的 `policyHash`。

## 编写质量检查单

- [ ] 安全 REJECT 规则在最高优先级
- [ ] PROMOTE 规则的条件包含 utility/routing/reliability/cost/security 全部维度
- [ ] 有 `default`（或明确接受隐式 HOLD）
- [ ] 每条规则的 `reason` 可被人读懂
- [ ] 无重复 priority
- [ ] `when` 里的字段名与上文字段表逐一核对过（编译期会抓拼写错误）
- [ ] Manifest 的 `policyHash` 已更新

# Strategy Engine 设计

**状态：** 方向已 `ACCEPTED`；CEL Schema 在第一次实现前仍为临时方案

## 1. 目的

Strategy Engine 根据类型化的 Request Context 选择可执行的 Agent Strategy。它不是任意脚本执行器，也不能在 Experiment 编译后修改已经冻结的 Treatment。

Strategy 可以选择：

- Model/Provider；
- Skill Bundle；
- Tool Allow/Deny Policy；
- Sandbox Profile；
- Token/Time/Cost Budget；
- Retry Behavior；
- Grader 集合；
- Fallback Behavior。

## 2. 输入 Context

Context 必须有版本、大小边界和明确 Schema：

```json
{
  "task": {
    "domain": "data-analysis",
    "risk": "medium",
    "case_type": "implicit",
    "input_size_bytes": 524288
  },
  "request": {
    "latency_budget_ms": 15000,
    "cost_budget_usd": 0.05,
    "tenant_tier": "demo"
  },
  "runtime": {
    "available_models": ["cheap", "strong"],
    "network": "disabled",
    "worker_capacity": 8
  }
}
```

只向 CEL 暴露已声明的字段。Unknown Field 必须校验失败，或以明确的 Unknown Value 参与计算；对 Security Policy，不能静默将 Unknown 转成宽松的默认值。

## 3. Rule Model

```yaml
policy:
  id: default-promotion-policy
  version: 3
  rules:
    - id: high-risk-strict
      priority: 100
      when: task.risk == 'high'
      select:
        strategy: secure-strong
    - id: low-latency-cheap
      priority: 50
      when: request.latency_budget_ms < 5000 && task.risk == 'low'
      select:
        strategy: fast-cheap
  default:
    strategy: balanced-default
```

`select` 指向不可变的 Strategy 定义。Rule 不得包含 Credential、任意 Go Code、Shell Command 或 Model Prompt。

## 4. 评估算法

1. 解析 Policy YAML/JSON。
2. 校验 Rule ID、Priority、Expression Size、Strategy 引用和允许字段。
3. 在 Policy 发布时编译 CEL Expression。
4. 保存编译后的 Policy Metadata 和 Source Hash。
5. 进行 Decision 时按 Priority 从高到低评估 Rule。
6. 使用确定性的冲突处理：第一条匹配 Rule 胜出；相同 Priority 在校验阶段直接拒绝。
7. 根据 Context 的 Budget 和 Tool Policy 校验被选中的 Strategy。
8. 返回 Selected Strategy 和 Decision Trace。

CEL-Go 的推荐流程是先 Parse/Check 再 Evaluate；生成的 `cel.Program` 是无状态、线程安全且可缓存的。SkillGate 应在发布阶段编译，并按 Policy Hash 缓存。

## 5. Decision Output

```json
{
  "policy_id": "default-promotion-policy",
  "policy_version": 3,
  "decision_id": "dec_01...",
  "selected_strategy": "secure-strong",
  "matched_rules": ["high-risk-strict"],
  "evaluated_rules": [
    {"id": "high-risk-strict", "matched": true},
    {"id": "low-latency-cheap", "matched": false}
  ],
  "explanation": "High-risk task selected strict strategy.",
  "context_hash": "sha256:...",
  "policy_hash": "sha256:..."
}
```

## 6. Release Policy 与 Runtime Routing 的区别

这是两个不同的 Policy Domain：

- **Runtime Routing：** 在执行前为 Task 选择 Strategy；
- **Release Gating：** 在评估完成后决定 Skill/Strategy Version 是否可以晋级。

二者可以复用 CEL Evaluator，但必须使用不同的允许变量集合和 Audit Trail。Runtime Rule 不能晋级 Package；Release Rule 不能意外改变已经编译的 Experiment。

## 7. Release Gate 输入

示例 Context：

```json
{
  "utility": {"lift": 0.12, "ci_lower": 0.04, "valid_cases": 24},
  "routing": {"recall": 0.94, "specificity": 0.89},
  "reliability": {"pass_at_3": 0.91},
  "cost": {"token_delta_ratio": 0.18, "time_delta_ratio": 0.03},
  "security": {"critical": 0, "high": 0, "confirmed_exploits": 0},
  "evidence": {"complete": true, "judge_calibrated": true}
}
```

示例条件：

```text
security.critical == 0
&& security.high == 0
&& security.confirmed_exploits == 0
&& evidence.complete
&& utility.ci_lower > 0
&& routing.recall >= 0.90
&& routing.specificity >= 0.85
&& cost.token_delta_ratio <= 0.30
```

## 8. 安全规则

- Skill Content 不能注册动态 Function；
- Release Policy 的 CEL 不允许 Network/File Function；
- 限制 Expression 长度和评估成本；
- 所有 Expression 必须在启用前 Type-check；
- 拒绝存在歧义的 Priority；
- 记录 Policy Source/Hash 和所有 Input Value；
- Security Field 缺失或 Evidence 不完整时 Fail Closed；
- Policy Error 根据 Domain 返回 `HOLD` 或 `REJECT`，不能静默晋级。

## 9. 性能计划

在优化之前先测量：

- Compile Latency；
- Hot Evaluation Latency 的 p50/p95/p99；
- 并发 Evaluation Throughput；
- Cache Hit Ratio；
- 每个 Compiled Policy 的 Memory；
- Decision Trace Serialization 成本。

Hot Path 应使用不可变的 Compiled Program 和有界的 Context 转换。

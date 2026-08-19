# 成功指标与 Release Criteria

**状态：** `PROPOSED`，作为 MVP 基线；阈值是校准用默认值，不是普适规律。

## 1. 为什么需要多个指标

一个 Skill 可能提高正确率，却同时增加成本、过度触发，或者引入安全漏洞。因此 SkillGate 输出的是一组 Scorecard，而不是一个混合的总分。

## 2. Scorecard

### Utility

对于第 `i` 个 Task，令 `y_i^1` 为 Candidate Score，`y_i^0` 为匹配的 Baseline Score：

```text
lift_i = y_i^1 - y_i^0
mean_lift = mean(lift_i)
```

对于二元 Deterministic Outcome，`y` 为 0/1；对于 Checklist Grader，`y` 可以是通过条目占比。

### Reliability

当共有 `n` 次 Attempt，其中 `c` 次成功时：

```text
pass@k = 1 - C(n-c, k) / C(n, k)
pass^k = (c / n)^k
```

`pass@k` 表示至少成功一次的概率；`pass^k` 是更严格的连续成功信号。

### Trigger Quality

对于 Autonomous Trigger Case：

```text
TPR / recall    = true positives / all positive cases
specificity     = true negatives / all negative cases
FPR             = 1 - specificity
precision       = true positives / all predicted positives
```

必须分别报告正例和负例总体，不能将误触发隐藏在单一 Accuracy 数字中。

### Efficiency

```text
token_delta_ratio = (tokens_candidate - tokens_baseline) / tokens_baseline
time_delta_ratio  = (time_candidate - time_baseline) / time_baseline
tool_delta        = calls_candidate - calls_baseline
```

### Security

Security 是硬门禁维度，不是可以抵消 Utility 的扣分项。需要报告：

- 按严重级别统计 Finding 数量；
- Static Existence Verdict；
- Dynamic Exploitability Verdict；
- confirmed/suspected/refused/not-triggered；
- Evidence 链接。

## 3. 统计规则

- 在 Task/Repetition 身份层面配对 Baseline 和 Candidate。
- 先在一个 Task 内聚合 Repetition，再计算 Suite 均值。
- 使用以 Task 为 Cluster 的 Bootstrap 计算置信区间，避免把同一个 Task 的多次 Attempt 当成独立 Task。
- 保留所有 Case 级数值，不能只发布四舍五入后的总分。
- 使用 Tune 数据迭代，使用 Holdout/Holdback 数据做 Release Claim。
- 缺失或无效的 Run 记录为 Incomplete；除非 Manifest 明确规定 Timeout 算失败，否则不能自动当作 0。
- 记录随机种子和 Bootstrap 次数。

## 4. 推荐的默认 Release Band

以下是等待真实数据校准的工程默认值：

| Signal | 默认解释 |
|---|---|
| Lift 的 CI 下界 > 0 | 方向上具有统计可信度的正向提升 |
| Lift 的 CI 跨过 0 | `HOLD`，需要收集更多证据 |
| Lift 的 CI 完全低于 0 | `REJECT`，除非存在已批准的权衡 |
| Trigger Recall < 0.90 | `HOLD`，需要优化 Description/Routing |
| Trigger Specificity < 0.85 | `HOLD`，存在过度触发 |
| 没有正向 Lift 且 Token Overhead > 30% | `REJECT` |
| 存在任意 Critical Security Finding | 立即 `REJECT` |
| 存在 Confirmed Dynamic Exploit | 立即 `REJECT` |
| Scanner Evidence 不完整 | `HOLD`，不能判定为绿色 |

阈值应由 Policy 配置，不能硬编码在 Metric Library 中。

## 5. Judge Quality Gate

在使用 LLM Judge 支撑 Release Claim 前：

1. 运行顺序和展示方式扰动检查。
2. 测试空输出、错误输出和 Prompt Injection 负控制。
3. 标注一批人工校准样本。
4. 报告 Agreement、Cohen's Kappa、Precision、Recall 和 Confusion Matrix。
5. 当 Judge 对结论有实质影响时，比较至少两种 Judge 配置下的 Headline Lift。

如果更换 Judge 会改变 Lift 的正负号，则实验不能用于 Release。

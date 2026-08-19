# Evaluation Theory 与测量规范

**状态：** `ACCEPTED`，作为 MVP 的测量方法  
**读者：** Compiler、Metric、Grader 和 Report 实现者

## 1. Potential Outcome 视角

对于第 `i` 个 Case，定义：

- `Y_i(1)`：Candidate Skill/Strategy 可用时的结果；
- `Y_i(0)`：匹配 Baseline 下的结果；
- `d_i = Y_i(1) - Y_i(0)`。

Skill Effect 的平均估计值为：

```text
ATE_hat = mean_i(d_i)
```

这个视角可以避免一个常见错误：Candidate Pass Rate 很高，可能只是因为 Base Model 本身很强。SkillGate 的主要 Utility Claim 必须相对于匹配的 Baseline 表达。

## 2. 统计单位

默认统计单位是 **Case**，不是单个 LLM Judge Vote，也不是单个 Tool Event。

当有 `K` 次 Repetition 时：

```text
candidate_case_score_i = mean_k(candidate_i,k)
baseline_case_score_i  = mean_k(baseline_i,k)
d_i = candidate_case_score_i - baseline_case_score_i
```

置信区间应重采样 Case（Cluster Bootstrap）。Repetition 用于反映 Reliability 和 Variance，但不能增加独立 Task 的表观数量。

## 3. Pairing 规则

只有以下字段全部一致时，Baseline/Candidate Pair 才有效：

- Case ID 和 Suite Version；
- Prompt/Fixture Hash；
- Model 和 Model Configuration；
- Agent Harness 和 Version；
- Environment Image/Configuration；
- Grader 和 Rubric Version；
- Repetition Index 和 Randomization Policy；
- Resource Limit。

预期的 Treatment Difference 必须明确，例如 Skill Hash 或 Strategy ID。如果其他字段不一致，应将 Pair 标记为 Invalid，并说明原因。

## 4. Confidence Interval

MVP 默认流程：

1. 每个 Arm 在 Repetition 聚合后，为每个 Case 计算一个 Score；
2. 计算配对 Difference；
3. 以 Case 为单位进行带放回 Bootstrap，报告默认使用 2,000 次 Resample，本地 Smoke Test 可以更低；
4. 使用 Percentile 或 BCa Interval，并在 Report 中说明方法；
5. 报告点估计、上下界、有效 Case 数量和缺失 Case 数量。

对于二元配对数据，可以使用不一致 Pair 的数量补充 McNemar Test，但它不能替代 Effect Size Interval。

## 5. pass@k 与 pass^k

共有 `n` 次 Attempt，其中 `c` 次成功时：

```text
pass@k = 1 - C(n-c, k) / C(n, k)
pass^k  = (c / n)^k
```

用 `pass@k` 表示多次尝试下至少成功一次的概率；用 `pass^k` 表示严格的重复成功能力。不能把 pass@k 当成确定性的 Reliability。

## 6. Trigger Metric

Autonomous Trigger Case 有明确的正负预期。计算 Confusion Matrix：

```text
TP：正向 Query 加载了目标 Skill
FN：正向 Query 没有加载目标 Skill
TN：负向 Query 保持安静
FP：负向 Query 加载了 Skill
```

报告 Recall、Specificity、Precision、FPR 以及每个 Model/Harness Cell。单一总平均可能掩盖某个被支持 Model 的失败。

加载证据必须来自 Agent Event Stream 或对挂载 Skill Path 的实际读取。最终答案提到 Skill 名称不属于加载证据。

## 7. Judge 的测量质量

LLM Judge 是一种 Measurement Instrument。在它支撑 Release Claim 前：

- 随机化 Candidate 展示顺序；
- 测试 Rubric 顺序扰动；
- 使用空输出、错误输出和 Prompt Injection Control；
- 用人工 Label 做 Calibration；
- 除原始 Agreement 外报告 Cohen's Kappa；
- 比较不同 Judge 下的 Headline Lift；
- 固定 Judge Model 和 Prompt Version。

对同一个 Output 的多次 Judge Call 是重复测量，不是独立 Case。应先聚合，再做 Case-level Statistics。

## 8. Leakage 与 Contamination

以下是不同的失败类别：

- Task Generator 看到 Candidate Skill Instructions；
- Expected Answer 进入 Agent Prompt；
- Grader 看到 Arm Label；
- 使用 Holdout Case 重写 Skill；
- Model 或 Benchmark 记住了原始 Task；
- 复用了之前 Run 的 Output Artifact。

每个 Experiment 都应记录 Leakage Check；当 Answer Key 进入 Agent Context 时 Fail Closed。

## 9. Cost 与 Efficiency

同时报告原始值和归一化值：

```text
token_delta_ratio = (candidate_tokens - baseline_tokens) / baseline_tokens
time_delta_ratio  = (candidate_time - baseline_time) / baseline_time
lift_per_1k_tokens = lift / max(abs(token_delta), 1) * 1000
```

不能因为 Skill 更早失败、所以消耗更少 Token，就把它判为高效。Efficiency 计算应只使用成功或明确可比较的 Run，并在 Report 中展示资格规则。

## 10. Subgroup Analysis

至少按以下维度拆分：

- Explicit/Implicit/Contextual/Negative Case Type；
- Domain；
- Task Difficulty；
- Model；
- Harness；
- Skill Length/Bundle Size；
- 是否包含 Script；
- Security Sensitivity。

避免事后挑选有利 Subgroup。探索性 Subgroup 必须明确标记，并保留完整 Suite 结果。

## 11. 解释规则

- 点估计为正但 CI 跨过 0：`HOLD`，不能说“有效”；
- 点估计为负但 CI 很宽：先补证据，不能直接宣称有害；
- Lift 为正但 Token Overhead 很高：报告为 Trade-off，不是无条件成功；
- Trigger Recall 高但 Specificity 低：存在过度触发，应优化 Description/Routing；
- Forced Lift 高但 Autonomous Lift 低：内容可能有效，但 Discovery 较弱；
- Utility 为正但存在 Confirmed Security Exploit：`REJECT`。

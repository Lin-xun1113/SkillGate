# 报告输出项参考

> 状态：与 2026-08-27 代码库同步 · 权威来源：`internal/report/generator.go` 与内嵌 Schema `internal/report/schema/report.schema.json`

每个实验完成后产出三份同构报告，存储于 `artifacts/<experiment_id>/`：

| 文件 | 用途 |
|---|---|
| `report.json` | 机器可读的完整报告，写入前经过内嵌 JSON Schema 自校验（`skillgate.report.v1`） |
| `report.md` | 人读版：执行摘要、Case 表格、资源用量、失败分析、统计方法说明 |
| `report.html` | 自包含 HTML 版（UI 详情页同样数据） |

本文逐项说明 JSON 字段（MD/HTML 是相同数据的投影）。

## 顶层结构

```jsonc
{
  "schema": "skillgate.report.v1",
  "experiment_id": "…",
  "metadata": { … },            // 实验元信息
  "summary": { … },             // 聚合统计（核心）
  "case_results": [ … ],        // 每个 Case 的配对明细
  "pass_at_k": { … },           // 可选：稳定性指标
  "resource_usage": { … },      // 可选：资源差值
  "invalid_pairs": [ … ],       // 无效配对及原因
  "statistical_method": { … },  // 统计方法声明
  "decision": { … }             // 可选：Release 决策（见 Decision 参考）
}
```

## `metadata` — 实验元信息

| 字段 | 类型 | 说明 |
|---|---|---|
| `experiment_name` | string | Manifest 中的实验名 |
| `created_at` | timestamp | 报告生成时间（UTC） |
| `total_trials` | int | 实验的 Trial 总数（如 48） |
| `valid_pairs` | int | 有效配对数——参与统计的 Case 数 |
| `invalid_pairs` | int | 无效配对数 |

> 第一眼看 `valid_pairs / (valid_pairs + invalid_pairs)`：比例太低说明执行层有问题，先查 `invalid_pairs[].reason` 再看任何统计数字。

## `summary` — 聚合统计

```jsonc
{
  "mean_lift": 0.25,                    // 配对差值均值
  "ci_lower": 0.10,                     // 95% CI 下界
  "ci_upper": 0.40,                     // 95% CI 上界
  "n_cases": 6,                         // 进入统计的 Case 数
  "statistically_significant": true,    // CI 是否不跨零
  "method": "cluster_bootstrap",        // 统计方法
  "num_resamples": 2000                 // Bootstrap 重采样次数
}
```

| 字段 | 解读要点 |
|---|---|
| `mean_lift` | 点估计。**单独看没有决策意义**，必须配合 CI |
| `ci_lower` / `ci_upper` | 两者同号才显著。`ci_lower > 0` 是晋级的常规门槛 |
| `statistically_significant` | `ci_lower` 与 `ci_upper` 同号时的布尔投影 |
| `method` / `num_resamples` | 方法透明度声明——评审者据此判断统计口径 |

无效配对存在时，summary 基于有效子集计算；`n_cases < case_results` 数量即提示有配对被剔除。

## `case_results[]` — Case 级配对明细

```jsonc
{
  "case_id": "csv-explicit-001",
  "baseline_score": 0.0,         // Baseline 臂均分（0–1）
  "candidate_score": 1.0,        // Candidate 臂均分
  "difference": 1.0,             // candidate − baseline
  "baseline_repetitions": 3,     // 参与聚合的重复次数
  "candidate_repetitions": 3
}
```

解读：

- **只包含有效配对**；无效的进 `invalid_pairs`；
- `difference` 的分布比均值更有信息量：全部为正 → Skill 一致有益；正负两极 → Skill 对某类任务有害，逐个排查；
- 重复次数低于配置值说明有 Trial 未成功完成，聚合用的是实际完成的部分。

## `pass_at_k` — 稳定性指标（可选）

```jsonc
{ "1": 0.83, "3": 0.94 }     // 键是 k 值字符串
```

- `pass@k`：k 次中至少一次通过的概率（HumanEval 无偏估计）；
- 数据不足（有效配对 < 2 或重复不足）时**整个字段省略**，不会出现误导性的 0；
- 高 `pass@1` + 高 `pass@3` = 既准又稳；低 `pass@1` + 高 `pass@3` = 不稳定（要靠重试兜底）。

## `resource_usage` — 资源差值（可选）

```jsonc
{
  "token_delta": {
    "baseline_mean": 850,      // Baseline 臂均值
    "candidate_mean": 1200,    // Candidate 臂均值
    "delta": 350,              // 绝对差值
    "delta_ratio": 0.41        // 相对增幅 = delta / baseline_mean
  },
  "latency_delta": { … }       // 同结构
}
```

- 无任何用量记录时字段省略（Fixture Provider 场景常见）；
- `delta_ratio > 0.30` 且提升不显著是保守策略的 REJECT 条件（见[Release Policy](../authoring/release-policy.md)）。

## `invalid_pairs[]` — 无效配对

```jsonc
[
  { "case_id": "csv-quality-001", "reason": "candidate arm missing valid result" }
]
```

常见原因与排查方向：

| reason 模式 | 排查方向 |
|---|---|
| missing valid result | Trial 失败/超时/取消 → Worker 日志、`trial_results` 状态 |
| pairing identity mismatch | 两臂身份字段不一致 → 检查 Manifest 两臂 Strategy |
| identity hash mismatch | Hash 链断裂 → Skill/Suite/Grader 内容是否被改动 |

## `statistical_method` — 统计方法声明

```jsonc
{
  "bootstrap_method": "cluster_bootstrap",   // Case 为聚类单元
  "ci_method": "percentile",                 // 分位法 CI
  "significance_level": 0.05                 // α = 0.05
}
```

固定输出，作用是让报告自解释：任何评审者都能据此复现统计口径。

## `decision` — Release 决策（可选）

存在条件：Manifest 声明了 `policy` 且实验完成评分。完整字段说明见[Decision 字段参考](decision-output.md)。快速判读：

```jsonc
{
  "result": "PROMOTE",             // PROMOTE / HOLD / REJECT
  "matched_rules": ["promote"],    // 胜出规则
  "hard_gate_override": null,      // 非 null 时 CEL 结果被硬门禁覆盖
  "explanation": "…"               // 人读解释
}
```

## Markdown / HTML 版的章节对照

`report.md` 的章节与 JSON 的对应关系：

| MD 章节 | 对应 JSON 字段 |
|---|---|
| Header（Experiment ID / Generated / Status） | `experiment_id` / `metadata.created_at` |
| Executive Summary（Lift、CI、显著性、有效配对） | `summary` + `metadata` |
| Case Results 表 | `case_results[]` |
| Resource Usage | `resource_usage` |
| Failure Analysis / Invalid Pairs | `invalid_pairs[]` |
| Statistical Methods | `statistical_method` |
| Release Decision | `decision` |

## 读取报告的建议顺序

1. `invalid_pairs` → 配对可信吗？
2. `summary.statistically_significant` → 提升是真的吗？
3. `case_results[].difference` → 提升在哪、拖累在哪？
4. `resource_usage` → 代价多大？
5. `decision`（`hard_gate_override` 优先看）→ 系统怎么判的？为什么？

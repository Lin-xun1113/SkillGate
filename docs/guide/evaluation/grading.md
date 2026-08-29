# 评分体系

> 状态：与 2026-08-27 代码库同步 · 权威来源：`internal/grader`、`internal/grading`

本文说明 SkillGate 如何把 Trial 的产出变成分数：评分器类型、执行顺序、结果结构。

## 设计原则

1. **Deterministic Verifier 永远优先。** 能用文件、Schema、命令、Trace 客观验证的，不用 LLM；
2. **LLM Judge 是兜底不是主力。** 只用于无法确定性检查的属性，且必须带版本化 Prompt；
3. **评分必须可审计。** 每个 Grade 记录 Grader 身份、输入证据、结论和执行时间；
4. **Grader 是版本化组件。** Grader 定义的内容 Hash 进入评分身份链——换 Grader = 换测量仪器 = 新实验。

## 评分器类型

### Deterministic Grader（确定性评分器）

五种方法（`internal/grader` 的 `GraderMethod`）：

| 方法 | 验证内容 | 典型用途 |
|---|---|---|
| `file_content` | 文件存在性与内容（含 `file_exists` 语义） | 要求产出文件的任务 |
| `json_schema` | 输出 JSON 符合声明 Schema | 结构化输出合规性 |
| `json_field` | 指定字段值与标准答案一致 | 数值/文本正确性 |
| `command_exit_code` | 在可信边界内执行命令，检查退出码 | 运行测试/脚本类验证 |
| `trace_assertion` | 执行 Trace 满足行为断言 | 安全边界、Skill 触发、输入约束 |

内置示例走 `suite_assertions` 模式：`grader.yaml` 声明 `mode: deterministic_only`、`llmJudge: false`，评分器按 `assertionSemantics: suite-assertions-v1` 执行 Suite 中每个 Case 的全部断言（详见[编写 Eval Suite](../authoring/eval-suite.md#断言类型参考)）。

**Trickle 语义**：一个 Trial 的分数 = 通过断言的加权结果。所有断言通过 → `passed=true`、`score=1.0`；任一确定性断言失败 → 该 Trial 不通过。断言级结果保留在 evidence 中，报告能告诉你**哪一条**断言挂了。

### LLM Rubric Grader（可选）

```yaml
grading:
  llm:
    enabled: true    # 内置示例为 false
```

用于「有帮助程度」「表述清晰度」这类无法确定性检查的属性。使用前提：

- 版本化 Judge Prompt（Prompt Hash 进入 Grader 身份）；
- 明确的评分量表（Rubric）；
- 报告中如实标注哪些分数来自 Judge。

Judge 不可用时：Deterministic 结果保留，依赖 Judge 的指标标记 Incomplete——不臆造分数。

## 评分执行流程

评分发生在 Control Plane（不是 Worker）：

```
实验最后一个 Trial 终态
    → commit 阶段把实验置为 GRADING
    → serve 的评分轮询器（--grading-poll-interval，默认 5s）发现 GRADING 实验
    → ProcessExperiment:
        ① 从 artifacts 目录读取各 Trial 的产出与 Grades
        ② 执行 Grader（Registry 已在 serve 启动时从 --graders-dir 加载）
        ③ Case 聚合 → 配对 → Bootstrap CI → pass@k → 资源差值
        ④ 生成 report.json / report.md / report.html（JSON 先过内嵌 Schema 自校验）
        ⑤ 构建 Metric Snapshot（带 Hash）→ Release Gate 评估
        ⑥ Decision 落库，实验置为 COMPLETED
```

> 一个常见的部署误区：把 `serve` 只当 gRPC 服务、以为评分在别处。**评分与 Release Decision 都在 `serve` 进程里**（M6 审计修复后的单一二进制设计）。

## Grader Registry

`serve` 启动时从 `--graders-dir`（默认 `./graders`）加载 Grader 定义目录：

```text
graders/
├── csv-analysis-grader.yaml     # DeterministicGraderSet
└── …
```

- 加载失败只告警不崩溃（日志 `Warning: failed to load graders`），因为不是所有实验都需要本地 Grader 定义（Suite 断言模式除外）；
- Grader 内容 Hash 是评分身份的一部分：Manifest 的 `grading.hash` 校验通过才执行。

## Grade 数据结构

单个 Trial 的评分结果（`internal/grader`）：

```jsonc
{
  "graders": [
    {
      "grader_id": "suite-assertions-v1",
      "grader_type": "deterministic",
      "passed": true,
      "score": 1.0,
      "message": "all 9 assertions passed",
      "evidence": { … },              // 断言级明细：哪条过/挂、期望值、实际值
      "executed_at": "2026-08-27T10:00:00Z"
    }
  ],
  "aggregated_score": 1.0             // 本 Trial 的聚合分
}
```

这些 Grades 存入 PostgreSQL 的 `trial_results.grades`，是后续所有统计的原子数据。

## 评分的统计消费

Trial 分数之后的旅程（详见[指标说明](metrics.md)与[报告输出项](../reference/report-output.md)）：

```
Trial Grades
  → TrialResult{aggregated_score, passed, usage, hashes}
  → 按 (case_id, arm) 聚合：CaseScore{mean_score, std_dev, trial_scores[]}
  → 按 case_id 配对：Pair{baseline_score, candidate_score, difference, valid}
  → 有效 Pair 集合 → mean_lift + 95% Cluster Bootstrap CI
  → Candidate 臂重复的 pass 布尔序列 → pass@k / pass^k
  → Token/延迟均值差 → resource_usage
```

无效 Pair（某臂缺分、身份不符）不进聚合，记录在 `invalid_pairs` 并给出原因。

## 评分相关的常见问题

| 现象 | 原因与处理 |
|---|---|
| 报告里全部 0 分 | grades 为空（如评分器没加载）。M6 修复后空 grades 会触发短路保护而不是产出 0 分报告；检查 `--graders-dir` 与 Suite 的 schemaRef/expectedRef 路径 |
| 断言 `json_field` 挂了但输出「看起来对」 | 多为数值格式差异（如 `"¥3,000"` vs `3000`）。Skill 的 Output discipline 应要求纯数值；Schema 定义类型约束 |
| `trace_assertion` 意外失败 | Trace 里出现了断言禁止的行为（多读了文件、加载了不该加载的 Skill）。用 Trace 明细定位具体事件 |
| LLM Judge 分数缺失 | Judge 未配置或不可用；相应指标标 Incomplete。Deterministic 分数不受影响 |

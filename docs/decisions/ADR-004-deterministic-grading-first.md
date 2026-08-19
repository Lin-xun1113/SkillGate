# ADR-004：Deterministic Grading 优先于 LLM Judging

**状态：** `ACCEPTED`  
**日期：** 2026-08-19（UTC）  
**影响范围：** Worker、Grader、Metric、Judge Calibration

## 背景

代码、文件、Schema、数值和测试结果通常可以客观检查。直接使用 LLM Judge 会引入顺序偏差、长度偏差、Prompt Injection 和不可重复性。

## 决策

Grading 按以下顺序执行：

```text
Deterministic Verifier
→ Trace/File Assertion
→ Calibration 后的 LLM Rubric
→ 人工 Review
```

LLM Judge 只能补充无法确定性表达的属性，不能覆盖 Deterministic Failure。Judge 必须隐藏 Arm Label，并经过 Negative Control、人工 Calibration 和 Judge Sensitivity 检查。

## 后果

正面：

- 结果更可解释、可调试；
- CI 不必默认依赖外部 Model；
- 降低 Judge 操纵和成本风险。

负面：

- 主观质量需要额外设计 Rubric；
- Deterministic Verifier 编写成本较高；
- 某些开放式任务只能得到部分评估。

## 验证方式

- Fixture Grader 不调用 Model；
- 错误 Artifact 必须被 Deterministic Test 捕获；
- Judge 负控制和 Calibration 报告可查询；
- Judge 不可用时结果标记 Incomplete。

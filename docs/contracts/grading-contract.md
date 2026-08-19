# Grading Contract

**状态：** `ACCEPTED` 作为 M0/M1 v0 Grader 基线；Runtime 实现仍需 Contract Test

## 1. 目标

Grading 的职责是把 Trial Evidence 转换为可追溯的 Verdict/Score。Grader 不能依赖未声明的上下文，也不能因为 Agent 在输出中声称“已经完成”就判定成功。

## 2. Grading 优先级

```text
Deterministic Verifier
        ↓
结构化 Trace/File Assertion
        ↓
经过校准的 LLM Rubric Judge
        ↓
人工 Review（用于 Calibration 或争议处理）
```

对于可执行代码、文件格式、数值和 Schema，必须优先使用 Deterministic 方法。

## 3. Grader 定义

```yaml
graders:
  - id: output-json-schema
    type: deterministic
    version: 1
    input: output/summary.json
    verifier:
      kind: json_schema
      schema_ref: schemas/summary.json
    weight: 1.0

  - id: workflow-quality
    type: llm_rubric
    version: 2
    prompt_ref: rubrics/workflow-quality.md
    model_ref: evaluator-model-v1
    weight: 0.3
```

## 4. Deterministic Grader 类型

MVP 至少支持：

| 类型 | 检查内容 |
|---|---|
| `file_exists` | 文件是否存在 |
| `file_hash` | 文件内容是否匹配预期 Hash |
| `json_schema` | JSON 是否符合 Schema |
| `json_field` | 指定字段值和类型 |
| `regex` | 文本匹配正则 |
| `command` | 在可信 Grader Boundary 执行命令 |
| `test_suite` | 运行 Go/Python/Node 测试并解析结果 |
| `trace_assertion` | 是否调用 Skill、Tool、Command，顺序是否正确 |
| `resource_limit` | Token、时间、Tool Call 是否超过限制 |

命令型 Grader 必须：

- 使用固定的 Grader Image 或受信目录；
- 接收明确的 Workspace；
- 有 Timeout 和输出大小限制；
- 不能读取 Answer Key 之外的未声明目录；
- 记录命令、退出码、标准输出摘要和版本。

## 5. LLM Rubric Judge

LLM Judge 的输入至少包含：

```json
{
  "rubric_id": "workflow-quality-v2",
  "rubric_hash": "sha256:...",
  "expected_outcome": "...",
  "assertions": ["..."],
  "candidate_evidence": "artifact://...",
  "arm_label_visible_to_judge": false
}
```

规则：

- 不把 `with_skill`/`without_skill` 标签暴露给 Judge；
- 对 A/B 比较随机化展示顺序；
- Prompt 中明确要求只根据 Evidence 判定；
- 使用 JSON Schema 约束输出；
- 记录 Model、Temperature、Prompt Hash 和 Input Hash；
- Judge 失败时不能静默判定为 PASS 或 0；应标记 `INCOMPLETE`；
- Judge 的结论必须可以通过人工 Calibration 和负控制检查。

M0 离线 Evidence 的 `status` 可为 `fixture_observed`；这只表示确定性 Fixture Harness 已观察并重算结果，不表示真实 Agent Runtime 已执行。真实 Worker 运行后才允许使用 `runtime_observed`。Security Finding 必须保存 `severity`、`category`、`evidence_ref` 和 `scanner_version`；M0 的 `M0_FIXTURE_EVIDENCE.json` 必须明确 `runtime_probe_executed: false`。

## 6. Verdict Schema

```json
{
  "grade_id": "grade_01...",
  "grader_id": "output-json-schema",
  "grader_version": "1",
  "status": "scored",
  "score": 1.0,
  "passed": true,
  "assertions": [
    {
      "id": "valid-json",
      "passed": true,
      "score": 1.0,
      "evidence": "artifact://art_01...",
      "reason": "文件符合 JSON Schema。"
    }
  ],
  "input_hash": "sha256:...",
  "evidence_hash": "sha256:...",
  "usage": {
    "input_tokens": 0,
    "output_tokens": 0,
    "cost_usd": 0
  }
}
```

`status` 取值：

- `scored`：有足够 Evidence，已产生有效分数；
- `failed`：Grader 自身执行失败；
- `incomplete`：Evidence 缺失或不完整；
- `skipped`：根据 Policy 有意跳过；
- `invalid`：Grader 配置或输入不合法。

## 7. Score 聚合

一个 Trial 的总分只能由有效 Grader 计算：

```text
trial_score = sum(score_i * weight_i) / sum(valid_weight_i)
```

当必须的 Grader 缺失时，Trial 不得自动变成 0 分；应根据 Suite Policy 标记为 `INCOMPLETE`。

在跨 Arm 比较时，两个 Arm 必须使用相同的 Grader Version 和 Weight。

## 8. Answer Key 隔离

Expected Output、Ground Truth 和私有 Rubric 不能进入 Agent-visible Context。Grader 在 Agent Process 结束后才能读取这些数据。任何泄漏检查失败都应阻止 Experiment 进入 Release Claim。

## 9. Judge 校准

启用 LLM Judge 前需要完成：

1. 顺序扰动一致性检查；
2. 空输出、错误输出和 Prompt Injection Negative Control；
3. 人工 Label 样本；
4. Agreement、Cohen's Kappa、Precision、Recall 和 Confusion Matrix；
5. 至少一次 Judge Variant Sensitivity 比较。

如果 Judge 变化导致 Lift 符号改变，Release Decision 必须为 `HOLD`。

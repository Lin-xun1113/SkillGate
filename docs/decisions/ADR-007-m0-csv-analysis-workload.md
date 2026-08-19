# ADR-007：M0 采用 CSV/Data Analysis 离线评估纵向切片

**状态：** `ACCEPTED`  
**日期：** 2026-08-19（UTC）  
**影响范围：** Eval Suite、Experiment Manifest、Fixture/Grader Boundary、M0 验证流程

## 1. 背景

项目仍处于文档和架构阶段，需要在创建 Go/Python 应用实现前冻结一个足够小、可以本地复核且能展示实验有效性的首个 Workload。首个样例必须同时体现配对 Baseline/Candidate、Deterministic Grading、Autonomous Trigger 与 Security Hard Gate，且不能依赖真实 Provider Credential。

## 2. 决策

M0 采用合成的 CSV/Data Analysis Workload，Skill 名称为 `csv-analysis`，固定一份 8 Case Eval Suite：

- 4 个 `forced_injection` / `answer` Case，用文件、JSON Schema 和隐藏 Expected JSON 做 Deterministic Output 验证；
- 3 个 `autonomous_trigger` / `trigger` Case，其中 2 个 Hard Negative、1 个 Should-trigger 正例，使用结构化 Skill Load Event 统计 Routing；
- 1 个 `security_probe` / `answer` Case，使用含伪造 Prompt Injection 的 CSV，验证 Network、Credential 和 Workspace 边界。

Experiment 固定 `without_skill` / `with_skill` 配对、Fixture Model、LangGraph Harness、受限 Environment、3 次 Repetition 和关闭的 LLM Judge。唯一 Treatment Difference 是是否挂载 `csv-analysis` 的内容寻址 Skill Version。

M0 使用 Node Fixture Harness、离线校验脚本和 Artifact Lock 验证 YAML/JSON/Schema、相对引用、SHA-256、Case 覆盖、Grader-only 隔离、Security Evidence、Pair/Trial/Idempotency 示例和配对不变量。Fixture Harness 明确不执行真实 Model、Worker、Docker 或网络 Runtime Probe；这些行为留给 M2–M4。

## 3. 备选方案

### 方案 A：Code Review

优点：更贴近 Go 工程演示，可直接使用测试和编译器作为 Verifier。  
缺点：需要更大的仓库 Fixture，安全 Probe 和 Trigger 边界更难在 M0 内保持小而清晰。

### 方案 B：只做 CSV 正确性 Case

优点：实现和数据准备最简单。  
缺点：无法在第一个样例中验证 Trigger/Answer 分离和 Security Hard Gate，不能覆盖项目的核心有效性不变量。

## 4. 选择理由

CSV 输出可以通过文件存在性、封闭 JSON Schema、隐藏期望字段和数据质量计数做确定性判断；合成 Fixture 不需要外部服务，适合 CI 和后续 Fixture Worker。相同的 CSV 领域又足以构造不相关 Hard Negative 和带 Prompt Injection 的 Security Probe，因此能以有限范围展示完整实验语义，而不是堆叠基础设施。

## 5. 后果

### 正面后果

- M1 有明确、可解析、可编译的输入样例；
- 能在无 Provider Key 的情况下验证 Hash、引用和配对语义；
- Utility、Routing 和 Security 的报告分母从一开始就分离。

### 负面后果

- M0 的 Case 数量不能代表真实 Workload 分布；
- 离线 Fixture 不能证明真实 Model 的触发或分析能力；
- Node 校验器不是最终 Go Compiler，必须在 M1 重新实现 Contract Test。

### 新增风险

- 人工编写的 Case 可能过度贴合 Skill；通过逐 Case Leakage Review、Grader-only 隔离和 Holdout 延期缓解。
- 本地 Environment Descriptor 不是完整 Sandbox；报告必须明确 M0 未执行真实隔离和网络探测。

## 6. 验证方式

- `npm run validate:m0`：无 Credential 运行 YAML/JSON/Schema、引用、Hash、Case 覆盖和配对检查；
- `evals/csv-analysis/LEAKAGE_REVIEW.md`：逐 Case 手工审查；
- `evals/csv-analysis/M0_ARTIFACT_LOCK.json`：保存内容身份；
- Native Verifier：独立逐项验收 A1–A8 和完整 Capability Spec。

## 7. 相关文档

- [`docs/contracts/experiment-manifest.md`](../contracts/experiment-manifest.md)
- [`docs/contracts/grading-contract.md`](../contracts/grading-contract.md)
- [`docs/contracts/release-gate-policy.md`](../contracts/release-gate-policy.md)
- [`docs/comet/changes/m0-evaluation-baseline/specs/csv-analysis-evaluation/spec.md`](../comet/changes/m0-evaluation-baseline/specs/csv-analysis-evaluation/spec.md)
- [`evals/csv-analysis/suite.yaml`](../../evals/csv-analysis/suite.yaml)

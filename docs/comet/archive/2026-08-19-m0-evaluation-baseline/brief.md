# Outcome

冻结 SkillGate Milestone 0 的首个演示 Workload 和可复现评估基线，使后续 M1 能在不重新解释实验语义的前提下实现 Registry 与 Experiment Compiler。

# Scope

- 选择一个首个演示 Workload，并定义 6–12 个稳定 Eval Case。
- 提供 Baseline（`without_skill`）与 Candidate（`with_skill`）配对的具体 Example Experiment。
- 同时覆盖 Forced Injection 的答案质量、Autonomous Trigger 行为和至少一个 Security Probe，且分别标注评估总体。
- 为客观输出提供 Deterministic Assertion、Fixture、Schema 或 Trace Assertion。
- 冻结 M0 所需的 Manifest/Trial Identity 语义，并记录 Failure Mode 与人工 Answer Leakage 审查结论。
- 更新项目状态与下一里程碑入口。

# Non-goals

- 不创建 Go Module、Python Worker、数据库、调度器或 UI。
- 不执行真实 Model API 调用，也不引入 Provider Credential。
- 不实现 M1 Compiler；M0 仅提供可由后续实现消费和验证的正式输入与契约基线。
- 不扩展为跨多个 Workload 的通用 Benchmark。

# Acceptance examples

- A1：仓库包含一份具体 Eval Suite，共 6–12 个稳定 Case，至少覆盖 2 个 Explicit/Implicit Positive、1 个 Contextual、2 个 Hard Negative 和 1 个 Security Probe。
- A2：每个 Answer Case 至少有一个可机械判定的 Deterministic Assertion；Trigger Case 使用可观察的 Skill 加载证据，而不是从答案文本猜测是否触发。
- A3：Example Experiment 明确包含匹配的 `without_skill` / `with_skill` Arm、唯一 Treatment Difference、每个 Arm 至少 3 次 Repetition，并分开标注 Answer、Trigger 与 Security 评估。
- A4：Example Experiment、Suite、Fixture、Schema、Grader 与 Policy 引用均可解析，不包含 Secret、占位 Hash 或越界路径。
- A5：Baseline/Candidate 除 Skill Treatment 外的 Model、Prompt、Fixture、Harness、Environment、Grader 和 Repetition Identity 保持一致。
- A6：仓库记录预期 Failure Mode 和逐 Case 人工 Leakage Review；Candidate Skill 不包含 Task-specific Answer，审查状态不再为 `pending`。
- A7：M0 涉及的 Contract 状态、版本和 Trial/Pair Identity 规则一致，`PROJECT_STATUS.md` 明确记录 M0 结果及 M1 下一步。
- A8：文档或仓库提供一组无需外部 Credential 的可重复检查，能够验证 YAML/JSON/Schema、引用、Hash、Case 构成和配对不变量。

# Constraints and invariants

- 强制比较匹配的 Baseline 与 Candidate，不能只观察 Candidate。
- Autonomous Trigger 与 Forced Injection 必须分开；Security Finding 是独立硬门禁。
- 客观输出优先使用 Deterministic Verifier，M0 不依赖 LLM Judge。
- Skill、Prompt、Fixture、Tool Output 与 Model Response 均视为不可信数据。
- 示例必须可在无真实 Provider Key 的环境中复核。
- 描述性文字使用中文；时间使用 UTC；正式 Contract 变更需同步相关文档，必要时新增 ADR。

# Decisions

- 当前 change 使用现有目录，不创建分支或 worktree。
- 本 change 只推进 M0，不提前编写应用实现。
- 首个演示 Workload 采用 **CSV/Data Analysis**；选择理由是可以用文件、JSON Schema 和 Trace Assertion 做确定性验收，并能在同一套 Fixture 中覆盖答案、触发和安全边界。
- M0 固定 8 个 Case：4 个 Forced Injection 答案 Case、3 个 Autonomous Trigger Case、1 个 Security Probe；样本量只用于验证实验结构，不支撑普遍质量或发布阈值结论。
- 离线 Fixture 是 M0 的必备复核路径；真实 Provider 选择留给后续里程碑，不阻塞本 change。
- M0 的唯一 Treatment Difference 是 Candidate 是否挂载 `csv-analysis` 的不可变 Skill Version；Security Probe 不计入 Utility Lift 或 Trigger Metric。

# Open questions

- 无。用户已确认目标、范围、8 个 Case、三种评估模式隔离、离线 Fixture 约束、非目标和 A1–A8 验收标准。

# Verification expectations

- 校验所有新增 YAML/JSON/Schema 可解析且内部引用存在。
- 自动检查 Case 数量、类型/总体覆盖、Deterministic Assertion、配对 Arm 和 Repetition。
- 自动重算并比对 Fixture/Skill 等内容 Hash，确认无占位值。
- 人工复核 Candidate Skill 与 Eval Case，确认没有 Answer Leakage。
- 独立 Verifier 按 A1–A8 逐项验收，并检查未开始应用实现。

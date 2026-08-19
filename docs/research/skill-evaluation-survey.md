# Skill 质量评估调研报告

**范围：** AI Agent Skill，即 `SKILL.md` / 可复用能力 Package  形式的 Skill  
**截止时间：** 2026-08-19  
**状态：** SkillGate 的研究基线

## 摘要

目前没有被普遍接受的单一“Skill Quality”指标。已有工作主要分为以下几类：

1. **Intrinsic/Static Quality：** Schema、文档、Trigger Clarity、Workflow Completeness、冗余、Smell 和 Security Pattern。
2. **Routing/Trigger Quality：** 正向 Query 是否选择 Skill，Hard Negative 是否被正确拒绝。
3. **Task-based Causal Utility：** 配对执行有 Skill/无 Skill，通常使用 Deterministic Verifier。
4. **Arbitrary-Skill Auto-evaluation：** 根据 Skill Package 自动生成能力匹配的 Task 和 Security Probe。
5. **Reliability/Cost：** 多次 Trial、pass@k、pass^k、Latency、Token、Tool Call 和 Cost。
6. **Lifecycle Security：** Admission、Provenance、Retrieval、Planner Deception、Runtime Behavior 和 Evolution。

SkillGate 不应试图取代所有现有 Benchmark。它的差异化应当是：使用可靠的 Go Control Plane，把不同评估模式编译成可复现的 Experiment，并应用可解释的 Promotion Policy。

## 1. 什么是 Skill Quality

可以使用以下概念性分解：

```text
Quality = Intrinsic Document Quality
        × Routing Quality
        × Execution Utility
        × Reliability
        × Safety Suitability
        × Cost Suitability
```

这只是帮助分解问题的模型，不应直接把各项相乘。Critical Security Issue 即使出现在高 Utility Skill 上，也必须仍然是硬门禁。

Skill 的实际价值取决于 Agent Model、Harness、Task Distribution、Environment 和 Budget。因此每个 Skill 结果都必须携带 Identity Tuple 和 Workload 描述。

## 2. 代表性系统

### 2.1 SkillsBench

SkillsBench 是目前研究中最清晰的 First-class Skill Efficacy Benchmark 之一。它使用 Curated Real-work Task、Containerized Environment、Expert Skill、Deterministic Verifier 和匹配的 No-skill/Curated-skill Arm，报告 Task-macro Pass Rate、Absolute Lift、Normalized Gain 以及不同 Model/Harness 的差异。

对 SkillGate 来说，方法论比 Headline Result 更重要：

- Skill Content 必须与特定 Task 的答案解耦；
- 两个 Arm 必须使用相同的 Task 和 Verifier；
- 需要关注多个 Model/Harness 组合；
- 应该对 Skill 数量和文档长度做 Ablation；
- Negative Skill Delta 必须作为一等证据保留。

### 2.2 SWE-Skills-Bench

SWE-Skills-Bench 关注真实 Software Repository。它将 Requirement 固定到具体 Commit，并把 Acceptance Criteria 转换成 Execution-based Test。其结果说明：当 Base Model 已经掌握相关领域，或 Skill 中的 Example 与项目版本不匹配时，Skill 可能冗余甚至有害。

SkillGate 应借鉴它的 Requirement-to-verifier Traceability，并把 Token Overhead 与 Correctness 分开报告。

### 2.3 NVIDIA SkillEvaluator

NVIDIA 的 Framework 是 Product-level 参考，因为它将流程分成：

- Tier 1：Schema、Quality、Security、PII、License、Code Integrity、Unicode 和 Script Check；
- Tier 2：基于 Embedding 的 Skill 内部和 Skill 之间的冗余检查；
- Tier 3：Live Agent A/B、Dimension、Skill Lift 和 pass@k。

SkillGate 应借鉴这种分层思想，但 MVP 不需要复制所有 Check。Static Validation 可以作为 Adapter 或 Preflight Stage；Go Control Plane 的差异化仍然是 Experiment Reliability 和 Strategy Policy。

### 2.4 Anthropic skill-creator

官方 Workflow 强调 Human-in-the-loop 的迭代流程：

1. 创建少量、真实的 Eval Set；
2. 并行运行干净的有 Skill/无 Skill Agent；
3. 在 Run 进行时编写 Assertion；
4. 保存时间和 Token 数据；
5. Grading 并聚合；
6. 在 Viewer 中查看实际输出；
7. 针对正向和 Hard-negative Trigger Query 优化 Description；
8. 使用 Holdout Query 降低过拟合。

SkillGate 应保留这种纪律，但将临时本地目录替换为 Content-addressed Experiment 和 Durable Control Plane。

### 2.5 SkillAudit 与 SkillLens

这类系统解决 Coverage 问题：任意专业 Skill 不一定适合固定 Benchmark。它们使用 LLM 从 Package 生成 Utility/Security Scenario，编译 Sandbox Task，运行有/无 Skill Arm，并保存 Trace/Artifact。主要风险是 Task Leakage、Generated Verifier 不够严格和对 Judge 的依赖。

SkillGate 只有在 Suite 进入 Review State 后才应支持自动生成的 Suite，并且要标记 Evidence 的来源状态：Generated、Reviewed 或 Trusted。

### 2.6 SkillEval 与 Skill Smell Detector

SkillEval 尝试使用低成本 Intrinsic Signal：从受控的正负文档 Pair 中，在冻结 Model 的 Hidden Representation 中学习与特定 Metric 对应的方向。Skill Smell Detector 则把 Skill Authoring Best Practice 转化为 26 类 Smell，分为 Deterministic Detector 和 Semantic Detector。

这些方法适合作为低成本的 Preflight Diagnosis，不能取代真实执行。SkillGate 应把 Intrinsic Finding 与 Causal Utility 并列展示，而不是隐式混成一个分数。

### 2.7 R3-Skill

R3-Skill 认为 Routing 不只是 Pairwise Relevance：单独看都相关的 Skill，组合起来可能仍然冗余或不兼容。它保留被拒绝的组合信息，用 Bi-encoder 进行 Recall，再用 Cross-encoder Reranker 判断 Query-conditioned Compatibility。

这支持 SkillGate 未来的一个 Strategy Engine 能力：在执行前同时评估 Skill Bundle 的 Relevance 和 Set Compatibility。

## 3. 常见实现模式

### 配对 A/B 执行

```text
相同 Case + 相同 Model + 相同 Environment + 相同 Verifier
                         │
                 只有 Skill/Strategy Arm 不同
```

### Deterministic Verifier

只要成功条件可以机械定义，就使用文件检查、Schema 检查、Test、Compiler/Build Check 和 Trace Assertion。

### Blind Judge

确实需要 LLM Judge 时，隐藏 Arm Label 并随机化展示顺序。保存 Judge Prompt、Model、Version 和 Judge Input Hash。

### Repetition 与不确定性

运行多次 Attempt，在 Task 层面聚合，使用 Paired Bootstrap 或合适的 Paired Statistical Test，并保留每个 Task 的方差和缺失信息。

### Trigger Matrix

将 Skill 挂载到 Agent 的正常 Discovery Path 上，使用真实的用户 Query 自主执行。必须从 Tool/File Evidence 判断是否加载，不能因为最终答案提到了 Skill 名称就认为发生了加载。

### Security 双层证据

Static Detection 判断风险模式是否存在；Dynamic Probe 判断 Agent 在正常或对抗输入下是否真正触发风险。必须分别报告 Existence 和 Exploitability。

## 4. SkillGate 的新增价值

SkillGate 应定位为 Orchestration 与 Decision Layer：

- Versioned Experiment Manifest；
- Exact Arm Identity Validation；
- Durable Scheduling 和 Lease Recovery；
- 面向 Python/LangGraph、Go 以及未来 Runner 的统一 Protocol；
- Budget-aware Concurrency 和 Retry；
- Task-level Statistics 和置信区间；
- 围绕 Utility、Cost、Trigger 和 Security 的 Policy Rule；
- 可解释的 Promotion Decision；
- 可 Replay 的 Trace/Artifact Lineage。

## 5. 可以转化为项目 Feature 的研究空白

1. **Cross-harness Comparability：** 在 Claude Code、Codex、OpenHands 和 LangGraph-compatible Worker 下比较同一个 Skill。
2. **Trigger 与 Content Attribution：** 分开报告 Autonomous Trigger Rate 和 Forced-injection Lift。
3. **Context Interference：** 发现 Skill 造成 Token 上升或 Task Delta 变负的情况。
4. **Bundle Compatibility：** 评估多个 Skill 是否可以无冗余、无冲突地组合。
5. **Judge Trust：** 将 Robustness、Human Alignment 和 Judge Sensitivity 作为明确 Artifact。
6. **Ablation Attribution：** 对 Description、Instruction、Reference、Script 和 Runtime Policy 做真实删除实验。
7. **Security Lifecycle：** 不仅保存单次 Scan，还要保存 Provenance 和 Version Change Evidence。

## 6. 各方向成熟度

| 方向 | 成熟度 | SkillGate 立场 |
|---|---|---|
| `SKILL.md` Format | 新兴 Open Standard | 实现标准子集并严格校验 |
| Static Skill Lint | 已有可用工具 | 集成或适配，但不作为差异化核心 |
| Paired Utility Evaluation | 早期研究较强 | 作为 MVP 的主要 Evidence Model |
| Autonomous Trigger Evaluation | 早期且依赖 Model | 独立 Matrix，保守解释 |
| Arbitrary Skill Task Generation | 活跃研究方向 | 早期版本只做 Reviewed/Advisory |
| LLM Judge Calibration | 理论已知但实践不均衡 | 作为一等 Contract |
| Lifecycle Skill Security | 新兴 | Hard Gate + 明确说明限制 |
| Universal Scalar Score | 尚不成熟 | 明确避免 |

## 7. 参考资料

Canonical Link 和状态见 [`source-register.md`](source-register.md)。本报告是设计输入，不表示 SkillGate 已经复现任何外部结论。

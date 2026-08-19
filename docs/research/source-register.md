# 调研来源登记表

**调研截止时间：** 2026-08-19  
**用途：** 记录 SkillGate 设计所使用的权威来源。由于该领域变化很快，开始实现时必须重新核对外部结论。

## Agent Skills 标准与执行

| 来源 | 类型 | 与项目的关系 |
|---|---|---|
| [Agent Skills Specification](https://agentskills.io/specification) | Open Standard | `SKILL.md` 必需字段、Package Layout、Progressive Disclosure 和 Validation 指导 |
| [LangGraph Persistence](https://docs.langchain.com/oss/python/langgraph/persistence) | 官方文档 | Checkpointer/Store 边界；支持恢复和 Thread State，但不是 Go Experiment 的事实来源 |
| [LangGraph Interrupts](https://docs.langchain.com/oss/python/langgraph/interrupts) | 官方文档 | Human-in-the-loop 和可恢复 Graph 行为；实现 Worker 前阅读 |
| [Harbor](https://github.com/harbor-framework/harbor) | 开源 Evaluation Framework | Sandboxed Agent Trial 和 Artifact-oriented Evaluation 的参考实现 |

## 直接评估 Skill 的项目与论文

| 来源 | 类型/状态 | 可借鉴内容 |
|---|---|---|
| [SkillsBench](https://arxiv.org/html/2602.12670) / [仓库](https://github.com/benchflow-ai/skillsbench) | Research Preprint + Open Source | Curated Task Benchmark、配对 No-skill/Curated-skill、Deterministic Verifier、Skill Lift |
| [SWE-Skills-Bench](https://arxiv.org/html/2603.15401) / [仓库](https://github.com/GeniusHTX/SWE-Skills-Bench) | Preliminary Preprint + Open Source | Requirement-driven SWE Task、固定 Repository、Deterministic Acceptance Test、Token Overhead |
| [SkillAudit](https://arxiv.org/html/2606.22613) / [仓库](https://github.com/SkillAudit/skillaudit) | Research Preprint + Open Source | 任意 Skill 的 Task Generation、Utility/Cost/Safety、Static + Dynamic Risk Test、Audit Artifact |
| [SkillLens](https://github.com/SkillLens-AI/skilllens) | Open-source Framework + 配套 Preprint | Utility/Security Probe、Harbor Execution、`pass_rate_gain`、Exploitability Verdict |
| [NVIDIA SkillEvaluator](https://github.com/NVIDIA/SkillEvaluator) / [文档](https://docs.nvidia.com/skills/skillevaluator) | Experimental Open Source | 三层流程：Static/Security、Semantic Dedup、Live Agent Evaluation；CI/Report 思路 |
| [Anthropic skill-creator](https://claude.com/blog/improving-skill-creator-test-measure-and-refine-agent-skills) / [仓库](https://github.com/anthropics/skills/tree/main/skills/skill-creator) | 官方 Workflow + Open Source | 并行 Clean Eval、Blind A/B、Trigger Optimization、Benchmark Iteration |
| [Skillcheck](https://github.com/sx4im/skillcheck) | Open-source Tool | Forced-injection A/B、Blind Grading、Paired Bootstrap CI、Effect Verdict |
| [SkillBenchmark](https://github.com/TiesPetersen/SkillBenchmark) | Open-source Prototype | 配对文本 Run、Blind Judge、置信区间、成本比较 |
| [Skillgrade](https://github.com/mgechev/skillgrade) | Open-source Tool | Docker/Local Agent Trial、Deterministic + Rubric Grader、pass@k、CI Threshold |
| [agent-skills-eval](https://github.com/agile-lab-dev/agent-skills-eval) | Open-source TypeScript Tool | API/CLI Runner、Assertion、Artifact、HTML Report |
| [Skill Eval Harness](https://github.com/adewale/skill-eval-harness) | Open-source Python Harness | Paired Lift、Split Discipline、Materialized Ablation、Judge Calibration、Trigger Matrix |

## 内在质量与安全研究

| 来源 | 类型/状态 | 可借鉴内容 |
|---|---|---|
| [SkillEval](https://arxiv.org/html/2608.06891) | 新近 Preprint | 通过受控正负 Skill Pair，在 Hidden Representation 中学习质量方向 |
| [From Anatomy to Smells](https://arxiv.org/html/2607.01456) | Empirical Preprint | Semantic Component、26 类 Skill Smell、Static + LLM Detector、纵向质量分析 |
| [Agent Skills in the Wild](https://arxiv.org/html/2601.10338) | Security Empirical Study | SkillScan Taxonomy、Static/Semantic Security Scan、大规模普查方法 |
| [SkillSec-Eval](https://arxiv.org/html/2607.13987) | Lifecycle Security Framework/Preprint | Repository Admission、Retrieval、Planner、Runtime、Evolution 威胁面 |
| [R3-Skill](https://arxiv.org/html/2606.03565) | Retrieval Research Preprint | Query-conditioned Multi-skill Compatibility、两阶段 Retrieval/Rerank、Set-Compat |

## 基础设施参考

| 来源 | 类型 | 可借鉴内容 |
|---|---|---|
| [OpenTelemetry Go](https://opentelemetry.io/docs/languages/go/) | 官方文档 | Trace 和 Metric Instrumentation；当前文档标记 Trace/Metric 稳定，Log 处于 Beta |
| [PostgreSQL SELECT Locking](https://www.postgresql.org/docs/current/sql-select.html) | 官方文档 | Row Locking 和 `FOR UPDATE`，用于第一版 Queue |
| [cel-go](https://github.com/google/cel-go) | 开源 Go Library | Typed、Non-Turing-complete Expression Evaluation；Compile/Check/Cache Policy Rule |

## 负责任地引用

- 未确认同行评审状态的论文必须标记为 Preprint；
- 将论文作者的实验结果与 SkillGate 自己的设计决策分开；
- 没有在相同 Task/Model/Harness 条件下复现时，不能直接把外部 Benchmark 的 Headline Number 写成 SkillGate 的结果；
- 如果依赖某个开源工具的具体行为，记录其确切 Repository Commit。

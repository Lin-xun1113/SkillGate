# SkillGate

> Go-first Agent Strategy 评估与晋级平台

SkillGate 是一个面向作品集的系统项目，用于评估可复用的 AI Agent Skill 和完整 Agent Strategy 是否真正改善 Agent 的任务结果。项目由 Go Control Plane 负责可靠的实验编排与策略决策，由 Python/LangGraph Worker 负责执行单次 Agent Trial。

## 为什么要做这个项目

一个 Skill 在文档审查中看起来有用，但实际运行时可能不触发、增加 Token 消耗、与目标仓库冲突，或者引入安全风险。SkillGate 不把评估简化成一个 LLM 分数，而是把它作为受控实验处理：

- 配对执行 `with_skill` / `without_skill` Trial；
- 将 Autonomous Trigger 与 Forced Injection 作为不同评估总体；
- 在 LLM Judge 之前优先运行 Deterministic Verifier；
- 计算任务级置信区间和可靠性指标；
- 统计 Token、延迟、Tool Call 和成本；
- 使用静态与动态 Security Gate；
- 对 Model、Skill、Tool 和预算的 Strategy Engine 决策提供解释；
- 使用内容寻址的 Artifact 和可回放 Trace。

## 当前状态

**阶段：M0、M1、M2 已完成并归档；M3 Runner Protocol 与 Fixture Worker 已完成 Build 候选，正在等待 Native Verify，尚未归档。M4 及之后尚未开始。**

首个 CSV/Data Analysis 离线评估样例已通过 Native 独立验收并归档（A1–A89 全部通过）：包含 8 个 Case、配对 Baseline/Candidate、Forced Injection/Autonomous Trigger/Security Probe 分离、确定性 Schema/Trace 断言、Fixture Harness、Security Evidence、Pair/Trial Identity 示例和内容 Hash。`npm run validate:m0` 可在无 Provider Credential 的情况下复核这些输入。归档产物位于 `docs/comet/archive/2026-08-19-m0-evaluation-baseline/`。

M0 只验证离线实验编排和契约证据；真实 Model、LangGraph Worker、Docker Sandbox、Grading、Metrics 和 UI 仍未实现，按计划留给 M4–M7。M3 当前提供 Fixture Worker、Runner gRPC、PostgreSQL Lease/Event/Result 闭环和 Compose Bootstrap；M4 尚未开始。

M1 当前提供本地 Go CLI：`skill validate/register`、`suite validate/register` 和 `compile`；默认使用 `.skillgate/registry` 文件系统 CAS，仅用于离线开发，不替代后续 PostgreSQL 事实来源。M1 仍不执行 Trial。Environment Identity 绑定 URI 与 Descriptor Hash；Skill Package 保持 SKILL.md 原始文本，仅规范换行；Suite 引用使用 Declared Hash 语义。

M2 已建立 PostgreSQL Queue/Lease/Retry/幂等 Result Commit：`db migrate|status`、`experiment materialize|cancel`、`trial claim|start|heartbeat|complete` 和 `scheduler sweep`。M3 Build 候选新增 `RunnerControl` gRPC、Go/Python Protobuf 桩、Session/Capability 校验、Request Hash、Heartbeat/取消、PostgreSQL Event 持久化与去重、Artifact Manifest 校验、幂等 Result Commit、Python Fixture Worker 和 Docker Compose Bootstrap。M3 不引入真实 Model、Sandbox、Grading 或对象存储；这些留给 M4–M6。M3 仍需独立 Verify 通过后才能归档。

## 目标架构

```text
React UI
   │ REST/JSON 或 ConnectRPC
Go Control Plane
   ├── Experiment API 与编译器
   ├── 基于 CEL 的 Strategy Engine
   ├── 基于 PostgreSQL 的 Scheduler 与 Lease
   ├── 幂等结果提交与指标聚合
   ├── Release/Security Gate
   └── OpenTelemetry 埋点
   │ gRPC Worker Protocol
Python LangGraph Trial Worker
   ├── Agent Graph 与 Model Adapter
   ├── Skill Loader 与 Tool Policy
   ├── Deterministic/LLM Grader
   └── Trace 与 Artifact Collector
   │
Sandbox 执行环境
```

Go 服务负责长期存在的分布式工作流状态；LangGraph 负责单个 Agent Trial 内部的状态机。LangGraph Checkpoint 不是实验生命周期的事实来源，PostgreSQL 才是。

## 目标 MVP

1. 通过 SHA-256 注册不可变 Skill Version。
2. 定义 Eval Suite，并将其编译为配对 Trial。
3. 使用 PostgreSQL 行锁、Lease、Heartbeat、Retry、Cancellation 和幂等提交来调度 Trial。
4. 通过版本化 Runner Protocol 执行 Python/LangGraph Worker。
5. 使用确定性方法评估客观输出，并可选使用经过校准的 LLM Judge。
6. 生成 Skill Lift、置信区间、pass@k/pass^k、成本、延迟和 Trace 报告。
7. 评估 CEL Policy，返回可解释的 `PROMOTE`、`HOLD` 或 `REJECT` 决策。

## 推荐演示故事

> 给定一个新的 Skill Version 和固定的 Eval Suite，SkillGate 运行匹配的 Baseline 与 Candidate Trial，判断 Skill 是否产生具有统计可信度的提升，同时检查 Trigger 与 Security 行为，并根据成本与风险 Policy 自动决定是否晋级该 Strategy。

## 文档目录

- [`PROJECT_STATUS.md`](PROJECT_STATUS.md) —— 当前决策和下一步行动
- [`docs/00-project-brief.md`](docs/00-project-brief.md) —— 一页式产品与作品集简介
- [`docs/01-prd.md`](docs/01-prd.md) —— 产品需求和验收标准
- [`docs/research/`](docs/research/) —— 调研、理论、来源和竞品定位
- [`docs/architecture/`](docs/architecture/) —— 系统、领域、执行、存储、策略和威胁模型
- [`docs/contracts/`](docs/contracts/) —— Manifest、Runner Protocol、Event、Grading、API 和 Release Gate
- [`docs/implementation/`](docs/implementation/) —— 里程碑、任务拆解、测试和性能计划
- [`docs/operations/`](docs/operations/) —— 本地开发、可观测性、故障恢复和安全运维
- [`docs/portfolio/`](docs/portfolio/) —— 简历要点、面试主题和演示脚本
- [`docs/decisions/`](docs/decisions/) —— 架构决策记录
- [`docs/templates/`](docs/templates/) —— 模板和示例 Manifest

## 范围边界

SkillGate 在初期不是：

- 通用 Agent Framework；
- Harbor、SkillsBench 或现有 Skill Linter 的替代品；
- Model Training 平台；
- 生产级加密货币交易系统；
- 声称能够衡量跨所有 Model 和 Task 的普适 Skill Quality 的系统。

SkillGate 的差异化在于：围绕可复现的 Agent Evaluation，提供可靠的 Go Control Plane 和可解释的 Policy Layer。

## License 与外部依赖

License 选择会在实现启动时确定。外部参考资料和依赖选择会在加入代码前记录到 Research Report 和 ADR 中。

# SkillGate —— Agent 协作约定

## 项目当前状态

SkillGate 当前处于**文档、研究与架构设计阶段**。仓库暂时没有应用实现代码。在项目计划、契约和第一个里程碑通过确认前，不要开始大范围编写代码。

## 产品方向

SkillGate 是一个 **Go-first 的 Agent Strategy 评估与晋级平台**。它评估可复用的 Agent Skill（`SKILL.md` 能力包）以及完整的 Agent Strategy，通过可复现的配对实验判断它们是否真正改善 Agent 行为；单次 Agent Trial 则由 LangGraph/Python Worker 执行。

系统边界约定如下：

- **Go Control Plane：** API、实验编译、Strategy/Rule 评估、调度、Lease、幂等结果提交、预算、指标聚合、Release Gate 和运行时遥测。
- **Python LangGraph Worker：** 单个 Trial 的 Agent 状态机、Model/Tool Adapter、Skill 加载、Sandbox 交互、Grader 和 Trace 生成。
- **PostgreSQL：** 实验元数据和生命周期状态的事实来源（source of truth）。
- **Object Storage：** Transcript、Event Stream、Artifact 和大型报告的存储位置。

## 不可妥协的工程原则

1. 必须将 `with_skill` 与匹配的 `without_skill` Baseline 进行比较；不能只根据 Candidate Arm 推断 Skill 价值。
2. Autonomous Trigger 评估必须与 Forced Injection 的答案质量评估分开。
3. 对可以客观验证的输出，优先使用 Deterministic Verifier；只有无法确定性检查的属性才使用 LLM Judge。
4. 将 Skill Package、Eval Prompt、Tool Output 和 Model Response 都视为不可信数据。
5. 为 Skill、Model、Harness、Environment、Evaluator 和 Policy 保存内容寻址的版本身份。
6. 使用“至少一次执行 + 幂等提交”；不得声称实现了 Exactly-once Execution。
7. 每个 Release Decision 都必须能够通过已保存的证据解释和复现。
8. Security Finding 是硬门禁；Utility 不能抵消 Critical Security Issue。
9. 避免过早引入分布式系统复杂度。先使用模块化 Go 服务和 PostgreSQL 队列，只有在实测需要后再引入 Broker。
10. 如果实现改变了契约，必须同步更新相关文档并新增或更新 ADR。

## 新贡献者或 Agent 的推荐阅读顺序

1. `README.md`
2. `PROJECT_STATUS.md`
3. `docs/00-project-brief.md`
4. `docs/01-prd.md`
5. `docs/architecture/system-architecture.md`
6. `docs/contracts/experiment-manifest.md`
7. `docs/research/skill-evaluation-survey.md`
8. `docs/implementation/implementation-plan.md`

## 文档规则

- 使用 UTC 时间，并明确写出版本号。
- 临时方案必须标记为 `PROVISIONAL`，不能写成已经实现的行为。
- 规范性要求放在 PRD、Contracts 和已接受的 ADR 中；设计理由放在 Architecture 和 ADR 中。
- 引用外部项目或论文时，必须给出 canonical URL，并标明其性质（标准、官方指南、开源工具或预印本）。
- 仓库中不得放入真实 Provider Key、用户数据或生产 Trace。
- 除专业名词、协议名、代码标识符和状态码外，描述性文字统一使用中文。

## 实现工作的完成定义

一个功能不能只因为 Happy Path 能运行就算完成。它必须同时包含相关契约、失败行为、幂等行为、可观测字段、测试，以及对项目状态或里程碑记录的更新。

<comet-ambient-resume>
<!-- Managed by Comet. Edits inside this block may be replaced by comet init/update. -->
<!-- Contract: comet.resume_probe.v2 -->

## Comet Ambient Resume

在这个仓库中，开始处理需要改动或调查的任务前，如果可能存在活跃 Comet workflow，把当前用户请求传入只读探针：`comet resume-probe . --stdin --json`。

- 如果用户通过宿主明确调用任意 Comet Skill（例如 `@comet`、`/comet`、`@comet-native` 或 `/comet-hotfix`），显式调用优先于本恢复协议；不要运行 resume probe，直接进入被调用的 Skill。
- 只信任返回的 `workflow`、`skill` 和 `entrySource`；它们只由项目配置或无配置兼容回退决定。不得扫描或切换另一套 workflow。
- 如果 probe 返回 `auto_resume`，简短说明选中的 active change，并进入 `nextCommand` 指向的永久入口。不要把状态命令当作恢复入口直接推进。
- 如果 probe 返回 `ask_user`，只问一个简短问题并等待用户回复。
- 如果当前请求未明确调用 Comet Skill，且 probe 返回 `out_of_scope` 或 `none`，不要进入 Comet workflow。
- 如果配置或状态无效且没有 `nextCommand`，停止并报告原因；不要猜测另一个 workflow。
- 不能只因为存在 active change 就把无关任务挂到该 change。Native 的未提交改动由 Native 入口检查，不由探针自动归因。
</comet-ambient-resume>

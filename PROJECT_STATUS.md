# SkillGate 项目状态

**最后更新：** 2026-08-19（UTC）  
**阶段：** M0 已完成并归档；下一步为 M1 Shape（规划 / 架构 / 调研基线已完成）  
**实现状态：** 尚无应用代码；M0 离线 Fixture Harness、Security Evidence、Grader/Identity Hash 和失败语义 Fixture 已通过 Native 独立验收

## 已确认方向

项目将采用**混合型 Go-first Agent Strategy Evaluation Platform**：

- Go 是主要工程载体，负责 Control Plane。
- Python + LangGraph 是可替换的单 Trial 执行 Worker。
- 初始持久化事实来源是 PostgreSQL。
- 大型 Trace 和文件使用 S3-compatible Object Storage（本地使用 MinIO）。
- CEL-Go 暂作为 Policy Expression Engine。
- Docker/rootless Sandbox 是初始隔离目标。

## 选择这个方向的原因

目标岗位是 Binance 的 Accelerator Program —— Golang Engineer（Strategy Engine）。岗位重点包括 Go Backend Service、Rule Engine、可扩展且高效的方案、数据处理、测试和 AI 相关工作。纯 Python Skill Evaluator 能体现 AI 熟悉度，但对 Go 和 Rule Engine 的核心要求支持不足。混合架构让 Go 系统成为主体，同时保留可信的 Agent 项目内容。

## 已作出的决策

| ID | 决策 | 状态 |
|---|---|---|
| ADR-001 | Go Control Plane + Python LangGraph Trial Worker | MVP 已接受 |
| ADR-002 | 在引入 Message Broker 前，先使用 PostgreSQL Queue/Lease | MVP 已接受 |
| ADR-003 | 配对 Baseline/Candidate Evaluation 是强制要求 | 已接受 |
| ADR-004 | Deterministic Grading 优先于 LLM Judging | 已接受 |
| ADR-005 | Sandbox 和 Security 是独立的硬门禁 | 已接受 |
| ADR-006 | Forced Injection 与 Autonomous Trigger 是不同的评估总体 | 已接受 |
| ADR-007 | M0 首个 Workload 采用 CSV/Data Analysis 离线纵向切片 | 已接受 |

具体理由见 [`docs/decisions/`](docs/decisions/)。

## M0 结果

**结果：** M0 已完成并归档。Native 独立 Verifier 对 A1–A89 逐项判定为 `passed`；`npm run validate:m0`、两个 M0 脚本语法检查和 Hash 校验均通过。归档目录：`docs/comet/archive/2026-08-19-m0-evaluation-baseline/`。

已生成并冻结待验收的 CSV/Data Analysis 样例：

- `skills/csv-analysis/SKILL.md`：不包含 Task-specific Answer 的通用 Skill；
- `evals/csv-analysis/suite.yaml`：4 个 Forced Injection Answer、3 个 Autonomous Trigger、1 个 Security Probe；
- `experiments/csv-analysis-v1-demo.yaml`：`without_skill`/`with_skill` 配对、3 次 Repetition、Fixture Provider；
- `evals/csv-analysis/LEAKAGE_REVIEW.md`：逐 Case Builder 手工审查；
- `evals/csv-analysis/M0_ARTIFACT_LOCK.json`：内容身份清单；
- `scripts/run-m0-fixture.mjs`：无外部 Credential 的确定性输出、Security Evidence 和 F1–F9 离线契约 Fixture Harness；
- `scripts/validate-m0.mjs`：无外部 Credential 的 YAML/JSON/Schema、引用、Hash、Grader/Identity 和配对检查；
- `evals/csv-analysis/M0_FIXTURE_EVIDENCE.json`：输出 Hash、Security Finding 元数据和 Identity 示例数量。

最近一次检查：`npm run validate:m0`、`npm run validate:m0:hashes`、`node --check scripts/validate-m0.mjs` 和 `node --check scripts/run-m0-fixture.mjs` 通过。M0 没有执行真实 Model、Docker Sandbox 或网络 Runtime Probe；这些限制已记录为后续里程碑范围。

## 开始编码前仍需决定的事项

1. 首批 Model Provider，以及初始演示使用真实 API 还是 CI 中的录制 Fixture（M0 已选择 Fixture-only 路径）。
2. PostgreSQL Migration 工具（`goose`、`atlas` 或其他方案）。
3. gRPC/Protobuf 生成流程。
4. License 和本地开发中的 Secret 管理方式。
5. 第一个 MVP 是否必须包含 UI，还是先完成 CLI/Report。

这些决定不会改变已确认的产品方向；M1 Shape 时再逐项冻结。

## 下一次会话清单

1. 创建并确认 M1 Registry/Experiment Compiler Native Shape。
2. 以 `docs/contracts/experiment-manifest.md` 和已归档 CSV 样例为输入，冻结 Canonical Archive、Skill Registry、Suite Registry、Pair Identity 和 CLI 诊断的 Go Contract Test。
3. 在 M1 Shape 确认后创建 Go Module；Python Worker 仍按实施计划在 M3/M4 引入。
4. 只有 M1 Shape 确认后，创建 Go Module；Python Worker 仍按实施计划在 M3/M4 引入。

## 明确延期的事项

- 将 Kafka/NATS 作为强制依赖
- Kubernetes 部署
- Multi-tenant Billing
- 自动 Skill Rewrite/Optimization
- Model Training 或 RL
- 普适的单一 Skill Quality 分数
- 使用不可信生产 Credential

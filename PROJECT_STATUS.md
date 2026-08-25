# SkillGate 项目状态

**最后更新：** 2026-08-25（UTC）  
**阶段：** M0 已完成并归档；M1 已重新确认 Shape（验收项收敛为 A1–A9），当前进入 Build 阶段，尚未归档
**实现状态：** Go Module、Canonical Identity、文件系统 CAS、Manifest Compiler、CLI 和 Contract Tests 已建立；M1 Shape 已确认、验收项收敛为 9 项（仅来自 brief.md），当前处于 Build 阶段等待 Builder 候选提交。

### M1 暂停原因与修复

M1 在 Native Verify 阶段经历了 5 轮 Build/Verify 循环，产生了 83 个验收项（A1–A83），消耗了大量 Token 和时间。根因分析如下：

1. **验收来源建模过细**：Comet Runtime 的 `Bo()` 函数将 spec.md 中的所有段落、列表项和表格行都提取为独立 A 项，导致 9 个 brief 高层验收膨胀为 84 项；
2. **执行编排失误**：Builder 采用了逐条 patch 而非批量归类的修复方式，导致多轮不必要的 Verifier 循环；
3. **Native 是放大器，不是根因**：Native 工作流忠实地执行了 83 项验收。问题在于验收集合本身过大。

已采取的修正措施：

- 项目级 Runtime Patch（`tools/comet-native-patch/`）移除了 spec 文本自动提取为 A 项的逻辑；2026-08-25（UTC）修正了 Patch 目标——实际执行路径是 `comet-native-next/archive/spec/doctor.mjs`（fast-runtime-router 路由），而非 `comet-native-runtime.mjs`；修复后 A 项只从 brief.md 提取；
- Shape 阶段增加了验收项收敛步骤，限制 A 项在 8–12 项；
- 文档已明确区分验收项（brief.md）和规格说明（spec.md）。

下一轮进入 Build 前，先验收收敛到 A1–A9（仅来自 brief.md）。当前已完成该收敛并经用户确认。

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

## M1 当前实现与下一步

已建立：

- `go.mod`、`cmd/skillgate` 以及 `internal/identity`、`internal/registry`、`internal/validation`、`internal/manifest`、`internal/experiment`；
- Skill/Suite 内容寻址注册、双臂 Pair/Trial 编译和版本化 CLI JSON；
- M0 示例编译目标：24 Pair、48 Trial；
- M1 第二轮修复重点：完整引用 Root 边界、声明 Hash 投影、Suite 版本 API/原子写入、CLI 退出码与 Contract Tests。

M1 第五轮独立 Verify 后，用户要求重新核对设计并完整收敛。已确认三项设计：Environment Identity=URI+Descriptor Hash；Skill Package=原始文本+LF；Suite 引用=Declared Hash。第六轮工作区 checkpoint 已完成，最近一次父会话 Gate 全部通过：Go tests/vet/help、M0 compile（24 Pair/48 Trial）和 M0 validation。

2026-08-25（UTC）Native 重新确认 Shape：验收项收敛为 A1–A9（全部来自 brief.md，不再含 spec 普通文字），已使用 `--confirmed` 进入 Build。所有开发期检查通过：`go test ./...`、`go vet ./...`、`go run ./cmd/skillgate --help`、M0 compile（24 Pair/48 Trial）、`npm run validate:m0`。当前等待 Builder 候选提交并经独立 Verifier 验收。M1 尚未归档。M1 完成后下一入口为 M2：PostgreSQL Queue/Lease、至少一次执行、Retry 和幂等 Result Commit。M1 不实现 Worker、Scheduler Runtime、真实 Model、Sandbox、gRPC 或 UI。

## 下班 Checkpoint（2026-08-19 UTC）

- Native change：`m1-registry-manifest-compiler`；分支：`comet/m1-registry-manifest-compiler`。
- Native 当前：`phase=shape`、`stateVersion=27`、`next_action=confirm-shape`；尚未归档。
- 最近父会话 Gate：`go test ./...`、`go vet ./...`、`go run ./cmd/skillgate --help`、M1 M0 compile、`npm run validate:m0` 均退出码 0。
- M1 compile 输出：`version=skillgate.cli.v1`、`ok=true`、`compiled=true`、`pair_count=24`、`trial_count=48`、`diagnostics=0`；当前 normalized Manifest Hash 为 `sha256:248b0c6d1a3b0cb5d043c856c3a8f40e59a991554f8dcd8339f704092aac256d`。
- M0 回归仍使用并通过既有冻结 Hash：Skill `sha256:5a2153eaf0a11af8d6141b87755526503e02a3af99080464cae27f49e5164090`、Suite `sha256:b76002abd734a4c9051927508b2a1371b97f7a01eda076b180dcfa6682f5373e`、Manifest `sha256:1e304d926d6ff2c5ec883a4cb1d4520fb75f95f8c1a1977cdc5f51f866290184`。
- 工作区未提交；没有遗留 `.m1-*` Mutation 文件；M0 归档内容未修改。
- 明日入口：确认已记录的三项 Shape 设计，不重新调查已确认事实；随后按 Runtime continuation 进入 Build/Verify。

## 开始编码前仍需决定的事项

1. 首批 Model Provider，以及初始演示使用真实 API 还是 CI 中的录制 Fixture（M0 已选择 Fixture-only 路径）。
2. PostgreSQL Migration 工具（`goose`、`atlas` 或其他方案）。
3. gRPC/Protobuf 生成流程。
4. License 和本地开发中的 Secret 管理方式。
5. 第一个 MVP 是否必须包含 UI，还是先完成 CLI/Report。

这些决定不会改变已确认的产品方向；M1 Shape 时再逐项冻结。

## 下一次会话清单

1. 在 Native Shape 点确认已记录的三项 M1 设计（URI+Descriptor、Raw text+LF、Declared hashes）——已完成（2026-08-25）。
2. 重新提交 Builder 候选（验收项 A1–A9），执行 Runtime checks 并启动独立 Verifier。
3. 若 Verify 通过，归档 M1 change；若仍失败，只处理新的、可复现的契约缺口。
4. M1 归档后创建并确认 M2 Native Shape，冻结 PostgreSQL Queue/Lease、Retry 和幂等 Result Commit Contract。
5. Python Worker 仍按实施计划在 M3/M4 引入。

## 明确延期的事项

- 将 Kafka/NATS 作为强制依赖
- Kubernetes 部署
- Multi-tenant Billing
- 自动 Skill Rewrite/Optimization
- Model Training 或 RL
- 普适的单一 Skill Quality 分数
- 使用不可信生产 Credential

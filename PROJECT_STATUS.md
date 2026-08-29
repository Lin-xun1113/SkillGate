# SkillGate 项目状态

**最后更新：** 2026-08-30（UTC）
**阶段：** M0–M7 全部完成并通过独立 Verify 归档，进入收尾与巩固阶段。
**实现状态：** 内容寻址 Registry、Canonical Identity、Experiment Manifest Compiler、PostgreSQL Queue/Lease/Retry/幂等 Commit、gRPC Runner Protocol、Execution 投影、Docker Sandbox、Python LangGraph/Fixture Worker、Grading Pipeline、Metric 聚合与 Report、CEL Release Gate、Web UI 与 Docker Compose 一键部署均已建立。

各里程碑的独立 Verify 结论以 `docs/comet/archive/<日期>-<里程碑>/verification.md` 为准。

## 最近一次本地验证（2026-08-27 UTC）

以下命令均在当前工作区实际运行通过：

```bash
go build ./...          # 通过
go vet ./...            # 通过
go test -short ./...    # 全部包通过（不含需要外部 PostgreSQL/Docker 的长测试）

go run ./cmd/skillgate compile experiments/csv-analysis-v1-demo.yaml
# compiled=true pairs=24 trials=48 manifest_hash=sha256:248b0c6d…aac256d

go run ./cmd/skillgate skill validate skills/csv-analysis
# valid=true hash=sha256:617fae44…9a9a1d
```

## 收尾修复验证（2026-08-28 UTC）

本轮修复补齐了执行投影单测、共享 CAS/Grader 物化、LangGraph Worker gRPC 边界、只读 API 与租约过期回收。已在本地实际通过 `go test ./...`、`go vet ./...`、`go build ./...`、Manifest/CAS CLI 检查、Python Worker 测试和源码编译检查。2026-08-28 UTC 已完成一次干净 Docker Compose 验收：

- `postgres` 健康，bootstrap 完成迁移、CAS prepare 和 48 个 Trial materialize；
- 默认 `langgraph-worker` 完成 48/48 `SUCCEEDED`，48/48 Grader 结果为 `scored`；
- Report 生成 4 个有效 Pair，Trigger Recall/Specificity 均为 `1.0`，Security `missing_evidence=0`；
- Release Decision 为 `HOLD`（`utility.ci_lower=0`，Baseline/Candidate 在离线 Fixture 上得分相同），属于保守策略的预期结果，不是启动或证据缺失错误。

验收命令为 `docker compose -f deploy/docker-compose.yml down -v --remove-orphans` 后执行 `docker compose -f deploy/docker-compose.yml up -d --build`，并通过 PostgreSQL 查询和 `/app/artifacts/<experiment_id>/report.json` 核对结果。该验收仍然是 Fixture Provider，不代表真实 Provider API E2E 已通过。

## Provider 接口加固（2026-08-30 UTC）

已完成 OpenAI/Anthropic Adapter 的离线契约加固：统一 `invoke` 接口，冻结 `model.config` 四字段 allowlist（temperature `0..2`、max_tokens `1..1000000`、timeout_seconds `(0,600]`、max_retries `0..30`），保留 `complete` 兼容入口和受限 legacy aliases；Manifest Compiler 与 Worker 双重拒绝未知字段、错误类型、越界值以及由实验内容注入的 Base URL、Endpoint、Credential 或 Header。响应统一归一化文本、Tool Call、实际 Model 与 Token Usage，并将 Provider 429/5xx/timeout/鉴权/参数错误映射为稳定 Runner Failure Category；Provider 错误链、Trace、Event、Result Manifest 和 `FailTrial` 在持久化前脱敏。归一化 Usage 随 `CompleteTrial` 写入结果证据。

本轮实际通过 Python Worker `75 passed, 2 skipped`（包括 Injected OpenAI/Anthropic Contract Test）；两个跳过项分别是必须显式设置 `LIVE_PROVIDER_SMOKE=1` 的真实 Adapter Smoke 与 `LIVE_PROVIDER_FULL_CHAIN=1` 的 full-chain preflight marker。`./scripts/provider-live-smoke.sh` 默认输出 `SKIP`，显式开启但无 Credential 时 Fail Closed，并固定 1 Case、64 output token、30 秒、`0.05 USD` 上限。由于当前没有受控 Credential，未调用真实 OpenAI/Anthropic API，也未完成真实 Provider 的 gRPC → Grading → Report → Decision E2E；默认 48-trial Compose 仍为 Fixture Provider。契约见 `docs/contracts/provider-runtime.md`，剩余路线见 `docs/implementation/consolidation-roadmap.md`。

## 2026-08-30 复核

Provider 安全字段双重拒绝、Usage 上报和 Sandbox 本地镜像检查已补齐。当前实际通过 `go test ./...`、`go vet ./...`、`go build ./...`、两套 Python 环境的 `53 passed, 1 skipped`、`py_compile`、`docker compose config -q`、`npm run validate:m0` 与 `git diff --check`。重建后的 Compose 服务保持健康，UI API 仍返回 `COMPLETED`、48/48 Trial、4 个有效 Pair、Decision `HOLD`；Worker 无重启或 Provider/认证错误日志。

## P1 Secret 与许可证边界（2026-08-30 UTC）

已实现并补充测试：Go `internal/secrets` 与 Python Worker `secret_source.py` 统一按 `NAME_FILE` → `NAME` 读取，缺失/空值/不可读/非法名称分别返回稳定 `SECRET_*` 错误；Provider Key、数据库 URL 和密码不进入 Manifest、`execution_hash`、命令参数、日志、Trace、DB 或 Artifact。CLI 新增 `--database-url-file`，含密码的旧 `--database-url` 会 Fail Closed。

Compose 默认形态明确为 Local Insecure Demo（仅合成 Secret + Fixture）；`deploy/docker-compose.production.yml` 是要求外部 Secret 文件的 Production override。`scripts/verify-secrets.sh` 提供合成 Sentinel 的 Git、CLI、输出树、Docker build-context 和轮换检查；`scripts/check-licenses.sh` 提供依赖许可证检查入口。

项目所有者已确认 MIT 许可证，版权主体为 `Lin-xun1113`，许可证文本见根目录 `LICENSE`。个人项目暂不接入 CI；Secret/许可证检查脚本保留为手动验证入口。

## 里程碑总览

| 里程碑 | 内容 | 归档 |
|---|---|---|
| M0 | CSV/Data Analysis 评估基线：Skill、Eval Suite、配对 Manifest、Fixture、防泄漏审查与 Hash 锁定 | `docs/comet/archive/2026-08-19-m0-evaluation-baseline/` |
| M1 | Go Module、`internal/identity`/`registry`/`manifest`/`validation`/`experiment`、内容寻址 Registry 与 Manifest Compiler、CLI | `docs/comet/archive/2026-08-25-m1-registry-manifest-compiler/` |
| M2 | PostgreSQL Scheduler 可靠性核心：Lease/Fence、Heartbeat、有界 Retry、幂等 Result Commit、两阶段取消、CLI | `docs/comet/archive/2026-08-25-m2/` |
| M3 | gRPC Runner Protocol（Go/Python 双端生成）、Session/Capability、Request Hash、事件流、幂等提交、Python Fixture Worker、Compose Bootstrap | `docs/comet/archive/2026-08-25-m3-runner-protocol/` |
| M4 | Execution 投影（execution_hash）、Docker Sandbox（网络隔离/只读根/非 root/资源限制）、LangGraph Worker、Trace 与 Artifact | `docs/comet/archive/2026-08-26-m4/` |
| M5 | Grading Pipeline（Deterministic 优先 + LLM Rubric）、Case-level Metric、Bootstrap CI、pass@k、JSON/Markdown/HTML Report | `docs/comet/archive/2026-08-26-m5/` |
| M6 | CEL Release Gate：Policy 编译缓存、优先级规则评估、Trigger Recall/Specificity 聚合、Security 硬门禁、Fail Closed | `docs/comet/archive/2026-08-26-m6/` |
| M7 | Web UI：实验列表页、报告详情页、Release Decision 展示、dark theme 响应式布局、优雅关闭 | `docs/comet/archive/2026-08-27-m7-web-ui/` |

M5/M6 之后按 `docs/audits/` 中的审计完成了 P0/P1 缺陷修复（Grading Poller 集成、空 Grades 短路、Grader Hash 统一、Artifact 路径穿越防护、ValidatePairing 强制、Decision 字段补全等），详见 `docs/audits/M6-FIXES-SUMMARY.md`。

## 部署形态

- `deploy/docker-compose.yml`：postgres → bootstrap（迁移与编译）→ control-plane（gRPC `serve`）→ langgraph-worker（默认消费 48 个 Trial；`fixture-worker` 仅保留为可选 profile）→ ui（`:8080`）。
- `deploy/run-demo.sh`：本地 Compose 之外的一键演示脚本。
- `skillgate ui` 可独立启动 HTTP Server，通过 PostgreSQL 只读展示实验列表、报告与 Decision。

## 当前已知限制

以下事项为诚实记录，不代表已经或将要默认解决：

1. **不声称 Exactly-once Execution。** 执行语义始终是“至少一次执行 + 幂等提交”；这是设计立场而非缺陷。
2. **Artifact Storage 使用本地文件系统/Compose 卷。** MinIO/S3 兼容对象存储在架构文档中规划，尚未接入。
3. **OpenTelemetry SDK 在依赖中，但主流程尚未接入遥测导出。** 架构文档中的可观测性目标属于后续工作。
4. **真实 LLM Provider 未做端到端验收。** OpenAI/Anthropic Adapter 已完成离线 Contract Test 与显式 Live Smoke 入口，但全部已执行验证仍基于 Fixture/Fake Client，未消耗真实 Model Credential。
5. **Web UI 存在 Builder 声明的已知限制：** 无端到端集成测试、模板渲染未单元测试、响应式布局未经浏览器实际验证（见 M7 verification）。
6. **里程碑中途的文档描述可能与最终 Verify 结论不一致。** 例如旧版根 README 在 M4 中途将 A4（mock Provider 流程）与 A6（取消中断）标记为部分实现/待实现，而 M4 最终 verification.md 的结论是全部 8 项验收通过；两处冲突时以各里程碑 `verification.md` 为准。
7. **Go-managed Sandbox 的执行日志仍是占位信息。** `RunTrial` 当前返回固定完成文本；真实 stdout/stderr 捕获、大小限制和脱敏纳入后续完整 Event/诊断阶段。
8. **个人项目暂不接入 CI。** Secret Scan 与依赖许可证检查通过本地脚本手动运行；工具未安装时只能记录 `SKIP`，不能把它描述为已完成扫描。

## 已确认方向

项目采用**混合型 Go-first Agent Strategy Evaluation Platform**：

- Go 是主要工程载体，负责 Control Plane（API 边界、实验编译、调度、Lease、幂等提交、Metric 聚合、Release Gate、UI）。
- Python + LangGraph 是单 Trial 执行 Worker，通过语言无关的 gRPC 协议接入。
- PostgreSQL 是实验元数据和生命周期状态的事实来源；大型 Artifact 规划走 S3-compatible Object Storage。
- CEL-Go 作为 Policy Expression Engine；Docker Sandbox 是初始隔离手段。

选择该方向的背景与理由：目标是 Binance Accelerator Program —— Golang Engineer（Strategy Engine）岗位的作品集项目，混合架构让 Go Backend、Rule/Strategy Engine、可靠异步任务处理成为系统主体，同时保留可信的 Agent 评估方法论内容。

## 已作出的决策

| ID | 决策 | 状态 |
|---|---|---|
| ADR-001 | Go Control Plane + Python LangGraph Trial Worker | 已接受 |
| ADR-002 | 引入 Message Broker 前，先使用 PostgreSQL Queue/Lease | 已接受 |
| ADR-003 | 配对 Baseline/Candidate Evaluation 是强制要求 | 已接受 |
| ADR-004 | Deterministic Grading 优先于 LLM Judging | 已接受 |
| ADR-005 | Sandbox 和 Security 是独立的硬门禁 | 已接受 |
| ADR-006 | Forced Injection 与 Autonomous Trigger 是不同的评估总体 | 已接受 |
| ADR-007 | M0 首个 Workload 采用 CSV/Data Analysis 离线纵向切片 | 已接受 |
| ADR-008 | M1 Registry 与 Manifest Compiler 设计（Environment Identity、Skill Package、Declared Hash） | 已接受 |
| ADR-009 | M2 PostgreSQL Queue/Lease、严格 Fence、Retry 和幂等 Commit | 已接受 |
| ADR-010 | M3 Runner Protocol 的租约校验与证据持久化 | 已接受 |
| ADR-011 | Provider Runtime 的 Endpoint、Credential 与 Retry 安全边界 | 已接受 |
| ADR-012 | Secret Source 与 Compose 凭据边界 | 已接受 |

具体理由见 [`docs/decisions/`](docs/decisions/)。

## 过程记录：M1 暂停原因与修复（保留备查）

M1 在 Native Verify 阶段经历了 5 轮 Build/Verify 循环，产生了 83 个验收项（A1–A83）。根因是验收来源建模过细（spec 文本被逐段提取为独立 A 项）与逐条 patch 的编排失误；Native 工作流本身是忠实执行者，不是根因。修正措施：

- 项目级 Runtime Patch（`tools/comet-native-patch/`）移除了 spec 文本自动提取为 A 项的逻辑；
- Shape 阶段增加验收项收敛步骤，A 项限制在 8–12 项且仅来自 brief.md；
- 文档明确区分验收项（brief.md）与规格说明（spec.md）。

此后 M2–M7 的 Verify 循环明显收敛（多数里程碑一轮通过），该经验保留作为流程参考。

## 下一步建议（尚未承诺）

后续方向与阶段依赖见 [`docs/implementation/consolidation-roadmap.md`](docs/implementation/consolidation-roadmap.md)，以下是优先候选：

1. **P0 真实 Provider 验收**：用受控 Credential 先运行 Adapter Smoke，再完成最小匹配 Pair 的 gRPC/Grading/Report 闭环。
2. **P1 License 与 Secret 最小边界**：Secret/Redaction/Rotation Runbook 与 MIT `LICENSE` 已补齐；个人项目暂不接入 CI。
3. **P2 MinIO/S3 Artifact Store**：建立 Store 抽象、LocalFS Adapter 与对象存储集成。
4. **P3–P5 OTel、UI 浏览器 E2E 与完整 Event Stream**：按稳定的 Correlation、Artifact 和 API 契约依次推进。
5. **P6 Runtime Strategy Routing**：作为独立 M8，不与事后 Release Gate 混合。

## 明确延期的事项

- 将 Kafka/NATS 作为强制依赖
- Kubernetes 部署
- Multi-tenant Billing
- 自动 Skill Rewrite/Optimization
- Model Training 或 RL
- 普适的单一 Skill Quality 分数
- 使用不可信生产 Credential

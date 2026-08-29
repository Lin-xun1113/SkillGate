# SkillGate

**Go-first 的 Agent Strategy 评估与晋级平台。** 用可复现的配对实验回答一个实际问题：这个 Agent Skill（`SKILL.md` 能力包）或 Strategy 真的让 Agent 变好了吗——值不值得晋级上线？

> 当前状态：M0–M7 已全部完成并通过独立 Verify 归档。最新进展见 [`PROJECT_STATUS.md`](PROJECT_STATUS.md)。

## 它解决什么问题

Skill 作者和 Agent 平台工程师很难可靠回答：

1. 与没有 Skill 的同一个 Agent 相比，这个 Skill 是否真的改善了结果？
2. Skill 是否在应该触发时触发，并在相邻请求上保持安静？
3. 提升是否足够稳定，值得额外的 Token、延迟和运维成本？
4. 这个包是否安全，能否被晋级到下一环境？

给 `SKILL.md` 打一个文档级 LLM 分数回答不了这些。SkillGate 把问题变成受控实验：

```
同一个 Eval Suite（任务集）
    ├── without_skill 臂（Baseline）── 执行 N 次重复
    └── with_skill   臂（Candidate）── 执行 N 次重复
         ↓ 配对差值
    Skill Lift + 置信区间 + 安全审计 → 可解释的 PROMOTE / HOLD / REJECT
```

## 核心原则

1. **必须配对比较。** Candidate Arm 的绝对分数没有意义；Skill 价值只能由匹配的 Baseline 差值给出。
2. **评估总体分离。** Forced Injection（答案质量）、Autonomous Trigger（触发时机）、Security Probe（对抗行为）分开度量、分开报告。
3. **Deterministic Verifier 优先。** 能用 JSON Schema、文件校验、Trace 断言客观验证的，不使用 LLM Judge。
4. **至少一次执行 + 幂等提交。** 不声称 Exactly-once；Worker 崩溃恢复后结果仍不重复计分。
5. **Security Finding 是硬门禁。** Critical Security Issue 不能被 Utility 抵消。
6. **每个 Release Decision 都能由已保存的证据逐条解释和复现。**

## 架构

```text
                    ┌────────────────────────────┐
                    │   Web UI (skillgate ui)    │
                    └─────────────┬──────────────┘
                                  │ HTTP/JSON
        ┌─────────────────────────▼─────────────────────────┐
        │              Go Control Plane (模块化单体)          │
        │  Registry · Compiler · Scheduler/Lease · Runner    │
        │  Grading Pipeline · Metrics · Release Gate (CEL)   │
        └──────────┬─────────────────────────────┬──────────┘
                   │ SQL                         │ gRPC (proto/runner/v1)
        ┌──────────▼──────────┐    ┌─────────────▼───────────────┐
        │ PostgreSQL          │    │ Python Worker (LangGraph)   │
        │ 元数据 / 生命周期状态 │    │ Docker Sandbox 隔离执行      │
        └─────────────────────┘    └─────────────────────────────┘
```

一次实验的生命周期：

```
注册(SHA-256 内容寻址 CAS) → 编译(Case × Arm × Repetition 冻结 manifest_hash)
→ 调度(PG SKIP LOCKED 队列 + Lease + 幂等提交) → 执行(gRPC 下发 Trial Request,
Docker 沙箱内跑 LangGraph,产出 Trace/Artifact) → 评分(Deterministic 优先)
→ 聚合(Lift/CI/pass@k/Token Delta/Trigger Recall/Security Finding)
→ 门禁(CEL Policy 逐条评估) → 报告与 Trace 回放
```

## 快速开始

前置条件：Go 1.26+（本地演示），可选 Docker Compose。

```bash
# 1. 构建并验证 CLI（无需数据库）
go run ./cmd/skillgate skill validate skills/csv-analysis
go run ./cmd/skillgate compile experiments/csv-analysis-v1-demo.yaml
# compiled=true pairs=24 trials=48 manifest_hash=sha256:…

# 2. 单元测试（不需要外部依赖）
go test -short ./...

# 3. 一键端到端演示（Local Insecure Demo）：postgres → 迁移/编译 → gRPC Control Plane → LangGraph Worker（离线 Fixture Provider）消费 48 个 Trial → UI (:8080)
docker compose -f deploy/docker-compose.yml up --build

# 4. 本地脚本演示（需要可达的 PostgreSQL）
./deploy/run-demo.sh
```

Compose 默认只使用 `deploy/secrets/demo-*` 合成凭据，不能用于生产。Production 必须提供外部 Secret 文件并使用 `deploy/docker-compose.production.yml --profile production`；服务命令不会携带数据库密码。Provider Key 通过 `OPENAI_API_KEY_FILE`/`ANTHROPIC_API_KEY_FILE` 注入，读取和稳定错误见 [`docs/contracts/secret-source.md`](docs/contracts/secret-source.md)。

CLI 完整能力：

```text
skillgate skill validate|register <path>          # Skill 包校验与内容寻址注册
skillgate suite validate|register <path>          # Eval Suite 校验与注册
skillgate compile <manifest>                      # 编译配对 Experiment
skillgate db migrate|status                       # 数据库迁移
skillgate experiment materialize|cancel           # 物化/取消实验
skillgate trial claim|start|heartbeat|complete    # 手动驱动 Trial 生命周期
skillgate scheduler sweep                         # 租约过期回收
skillgate serve                                   # 启动 gRPC Runner Server
skillgate ui                                      # 启动 Web UI
```

更多参数请参考 `--help`，输出均支持 `--json` 稳定契约格式。

## 目录结构

```
cmd/skillgate/        # CLI 入口（所有子命令）
internal/
  identity/           # Canonical JSON、SHA-256 内容寻址身份
  registry/           # 本地文件系统不可变 Content-Addressed Registry
  validation/         # Suite/Skill 校验诊断
  manifest/           # Manifest Compiler（Pair/Trial 展开）
  experiment/         # Pair Identity 与 Trial 计划
  store/postgres/     # PostgreSQL Store、迁移、幂等 Result Commit
  lease/ retry/       # 高熵 Lease Token、有界指数退避重试分类
  scheduler/          # 逻辑 Trial/Attempt 身份与状态机
  runner/             # gRPC Runner Service、执行投影、Artifact 持久化
  sandbox/            # Docker 沙箱管理器
  grader/ grading/    # Deterministic Grader + LLM Rubric 执行引擎
  metrics/ statistics/# Metric 聚合、Bootstrap CI、pass@k、路由/安全证据
  releasegate/ strategy/ # CEL Policy 编译、快照构建、Decision 评估
  report/ ui/         # JSON/Markdown/HTML 报告、Web UI
workers/python/       # LangGraph Worker 与确定性 Fixture Worker
proto/runner/v1/      # 语言无关 Runner Protocol 契约
skills/ evals/ experiments/ policies/  # 示例 Workload：CSV/Data Analysis 全套样例
deploy/               # docker-compose、Dockerfile、演示脚本
docs/comet/archive/   # M0–M7 各里程碑 brief/spec/verification 归档
```

## 文档地图

| 文档 | 内容 |
|---|---|
| [`PROJECT_STATUS.md`](PROJECT_STATUS.md) | 最新进度、已验证结论、已知限制 |
| [`docs/00-project-brief.md`](docs/00-project-brief.md) | 项目简介与作品集主旨 |
| [`docs/01-prd.md`](docs/01-prd.md) | 产品需求（FR-1 ~ FR-10） |
| [`docs/architecture/system-architecture.md`](docs/architecture/system-architecture.md) | 架构、一致性模型、故障边界、不变量 |
| [`docs/contracts/`](docs/contracts/) | Experiment Manifest、Runner Protocol、Grading、Release Policy 等契约 |
| [`docs/decisions/`](docs/decisions/) | ADR-001 ~ ADR-012 |
| [`docs/implementation/implementation-plan.md`](docs/implementation/implementation-plan.md) | 里程碑计划与验收证据要求 |
| [`docs/comet/archive/`](docs/comet/archive/) | M0–M7 每个里程碑的独立 Verify 记录 |

## 已知限制

诚实记录，不代表已完成：

- 不声称 Exactly-once Execution（设计立场：至少一次执行 + 幂等提交）；
- Artifact Storage 目前是本地文件系统/Compose 卷，MinIO/S3 尚未接入；
- OpenTelemetry SDK 在依赖中但主流程尚未接入遥测导出；
- OpenAI/Anthropic Adapter 已有离线 Contract Test 与显式 Live Smoke 入口，但尚未使用真实 Credential 完成 gRPC/Grading/Report 端到端验收。
- 项目采用 MIT 许可证，版权主体为 `Lin-xun1113`，见根目录 [`LICENSE`](LICENSE)；第三方依赖检查流程见 [`docs/operations/license-management.md`](docs/operations/license-management.md)。个人项目暂不接入 CI。

完整清单见 [`PROJECT_STATUS.md`](PROJECT_STATUS.md) 的「当前已知限制」。

## 明确不做的事

Model Training、通用 Skill Marketplace、自动 Skill Rewrite、Live Trading、Multi-region Deployment、Billing、强制 Kafka/NATS/Kubernetes。理由与边界见项目简介。

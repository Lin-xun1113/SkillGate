# SkillGate 文档索引

本目录是 SkillGate 的工作设计记录。先写文档再实现，是为了让后续代码拥有稳定的产品、测量和可靠性 Contract。

## 推荐阅读路径

### 新贡献者

1. `../README.md`
2. `../PROJECT_STATUS.md`
3. `00-project-brief.md`
4. `01-prd.md`
5. `architecture/system-architecture.md`
6. `contracts/experiment-manifest.md`
7. `implementation/implementation-plan.md`

### Agent Evaluation 研究者

1. `research/skill-evaluation-survey.md`
2. `research/evaluation-theory.md`
3. `research/source-register.md`
4. `contracts/grading-contract.md`
5. `contracts/release-gate-policy.md`

### Go Backend 实现者

1. `architecture/system-architecture.md`
2. `architecture/strategy-engine.md`
3. `architecture/execution-lifecycle.md`
4. `architecture/storage-and-data.md`
5. `contracts/api-contract.md`
6. `contracts/runner-protocol.md`
7. `implementation/backend-work-breakdown.md`

### Python/LangGraph Worker 实现者

1. `architecture/execution-lifecycle.md`
2. `contracts/runner-protocol.md`
3. `contracts/event-schema.md`
4. `contracts/grading-contract.md`
5. `implementation/worker-work-breakdown.md`

### 运维与评审

1. `architecture/threat-model.md`
2. `architecture/observability.md`
3. `operations/failure-recovery.md`
4. `operations/security-operations.md`
5. `implementation/testing-and-validation.md`

## 文档权威层级

- **规范性文档：** `01-prd.md`、`contracts/` 和已接受的 ADR。
- **设计与理由：** `architecture/` 和 `decisions/`。
- **调研与证据：** `research/`。
- **执行计划：** `implementation/`。
- **运维手册：** `operations/`。
- **作品集叙事：** `portfolio/`。

如果两个文档发生冲突，不要自行选择。应先新增或更新 ADR，再更新规范性 Contract。

## 里程碑裁剪与文档状态

文档使用以下状态标记：

- `FUTURE`：描述当前里程碑尚未实现的后续系统设计。这些文档保留作为设计方向参考，但不作为当前实现的依据。
- `ACCEPTED`：当前里程碑已经选择的设计。
- `IMPLEMENTED`：已经在代码和测试中验证的契约。
- `DEPRECATED`：仅保留历史背景。

每个 `FUTURE` 文档在文件头标注目标 Milestone（如 `FUTURE → M6`），并在本索引中注明。普通文档若未标记则默认为 `ACCEPTED`。

如果两个文档发生冲突，先确认双方状态。`FUTURE` 文档不参与当前里程碑的决策；冲突应在 `ACCEPTED` 或 `IMPLEMENTED` 文档中解决。

## 状态词汇

- `PROPOSED`：仅用于讨论
- `ACCEPTED`：当前里程碑已经选择
- `IMPLEMENTED`：已经在代码和测试中验证
- `DEPRECATED`：仅保留历史背景

## 语言约定

除专业名词、协议名、代码标识符、状态码和外部项目名称外，文档描述性语句统一使用中文。代码块中的字段、命令和 API 名称保持原样。

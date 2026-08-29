# Changelog

All notable changes to SkillGate will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### P1 Secret 与许可证边界（2026-08-30 UTC）

- 新增 Go/Python `NAME_FILE` → `NAME` Secret Source，缺失、空值、不可读和非法名称返回稳定 `SECRET_*` 错误；Provider Key 与数据库凭据不进入 Manifest、执行身份、命令参数或持久化证据。
- Compose 默认路径明确为 Local Insecure Demo（合成 Secret + Fixture），新增要求外部 Secret 文件的 `docker-compose.production.yml`；Bootstrap、Control Plane 和 UI 不再在命令行携带密码 DSN。
- 新增 `scripts/verify-secrets.sh` 合成 Sentinel 检查与 `scripts/check-licenses.sh` 依赖许可证检查入口，补充 Secret Source Contract、ADR-012 和轮换/事故处理 Runbook。
- 项目所有者确认采用 MIT，新增根目录 `LICENSE`；个人项目暂不接入 CI，依赖许可证检查保留为手动脚本。

### Provider Runtime 接口加固（2026-08-29）

- 冻结 `invoke(messages, tools, params)` Provider Contract，并保留 `complete` 兼容入口；
- OpenAI/Anthropic 支持冻结的 temperature、max token、timeout 与显式 SDK retry 参数；
- 归一化实际 Model、Text/Tool Call 与 OpenAI/Anthropic Token Usage；
- 429/5xx/网络、timeout、鉴权/参数和非法 Response 映射为稳定 Runner Failure Category；
- Manifest 与 Worker 双重拒绝实验内容覆盖 Provider Endpoint、Credential 或 Header；
- Provider 异常进入日志/FailTrial 前脱敏，Usage 进入 CompleteTrial 与 Result Manifest；
- 新增默认跳过的真实 Provider Adapter Smoke；未声称真实 API E2E 已通过。

### Added

#### M7 - Web UI (2026-08-27)

Browser-based experiment monitoring, completing the end-to-end visibility loop.

**Core Features:**
- `skillgate ui` command: HTTP server for experiment monitoring
- Experiment list page: all experiments with status and basic metadata
- Experiment detail page: statistical summary, case results, and release decision
- Dark theme responsive layout (CSS clamp + 3 breakpoints)
- Graceful shutdown via signal handling

**Integration:**
- Reads from PostgreSQL via the shared store (`store.Pool()` accessor)
- Serves artifacts from the shared artifacts volume
- Added as `ui` service in `deploy/docker-compose.yml` (`:8080`)

**Verification:**
- Archived: `docs/comet/archive/2026-08-27-m7-web-ui/verification.md`
- Known limitations declared by Builder: no end-to-end integration tests, templates not unit-tested, responsive layout not browser-verified

#### M4 - Execution Projection & Docker Sandbox & LangGraph Worker (2026-08-26)

Paired trials now execute end-to-end without real LLM credentials.

- Go projects execution content (skill hash, case input, model, tool policy) into the Trial Request payload with an independent `execution_hash`; the frozen M3 `trial_request_hash` is unchanged
- Docker Sandbox Manager: one container per trial with network isolation, read-only root filesystem, non-root user, CPU/memory limits, CAS read-only mounts, and automatic cleanup
- Python LangGraph Worker: skill loading from CAS with hash validation, graph execution, trace/event capture, artifact writing with hashes, heartbeat, and idempotent result submission
- Trace and artifact persistence; docker compose orchestration for one-command acceptance

#### M5 - Grading Pipeline (2026-08-26)

Scoring and statistics on top of executed trials.

- Deterministic graders first (file existence/content, JSON Schema, command exit code, trace assertion), optional LLM rubric grading behind them
- Case-level scores with paired analysis: Skill Lift, bootstrap confidence intervals, pass@k (HumanEval formula), token/cost and latency deltas
- Trigger routing recall/specificity and security findings aggregated as separate populations
- Reports in JSON, Markdown, and HTML persisted to PostgreSQL
- Control-plane integration: grading poller, grader registry, PostgreSQL persistence for trials/grades/reports

#### M6 - Release Gate (2026-08-26)

Complete policy-driven release gate implementation for automated promotion decisions.

**Core Features:**
- Policy engine with CEL expression evaluation and strict YAML validation
- Trigger evidence aggregation (recall/specificity from autonomous trigger cases)
- Security evidence aggregation (critical/high/confirmed exploit counts)
- Hard gates: REJECT on critical security findings, HOLD on incomplete evidence or CI crossing zero
- Positive gates: PROMOTE when utility lift, routing, reliability, cost, and security thresholds pass
- Immutable metric snapshots with canonical hash-based persistence
- Full decision traceability with policy hash, matched/evaluated rules, failed conditions, and explanations

**Database:**
- New tables: `metrics_snapshots`, `release_decisions`
- Migration: `00005_release_gate.sql`

**Integration:**
- `ProcessExperiment` evaluates policy after report generation
- Reuses persisted snapshots across experiment reruns for consistency
- Projects decisions into `report.json`, `report.md`, `report.html`
- No-policy experiments complete normally without decision

**Packages:**
- `internal/strategy` - Policy parsing, validation, CEL compilation
- `internal/releasegate` - Snapshot building, hard gates, decision evaluation
- `internal/metrics/routing.go` - Trigger and security evidence aggregation
- `internal/store/postgres/releasegate.go` - Snapshot and decision persistence

**Documentation:**
- Specification: `docs/comet/specs/release-gate/spec.md`
- Archive: `docs/comet/archive/2026-08-26-m6/`

### Fixed

#### M6 Bug Fixes - P0/P1 Critical Issues (2026-08-27)

All P0 (critical) and P1 (important) bugs identified in the M6 implementation audit have been fixed and verified through end-to-end testing.

**P0 Critical Fixes:**
- **Binary Split**: Integrated grading poller into `skillgate serve` - both RPC and grading now run in single process
- **Empty Grades Short-Circuit**: Added semantic `hasActualGrades()` check - empty `{}` no longer skips executor
- **Grader Hash Mismatch**: Unified hash calculation to use canonical JSON - manifest and registry now consistent
- **DeterministicGraderSet Schema**: Added schema adapter to expand `DeterministicGraderSet` into standard `Grader` format
- **Fixture Worker Artifacts**: Implemented actual file writing to disk at `<ARTIFACTS_ROOT>/<experiment_id>/<trial_id>/`
- **LangGraph Worker Protocol**: Fixed syntax error, protocol_version, sandbox_profiles, hash format, and heartbeat cancel check
- **Docker Compose Cleanup**: Removed abandoned root compose files and created clear deployment documentation

**P1 Important Fixes:**
- **Artifact Path Traversal**: Added `isWithinArtifactsRoot()` validation with symlink resolution (13 security tests)
- **ValidatePairing Enforcement**: Added `PairCasesWithValidation()` that enforces identity consistency checks (22 tests)
- **Decision Audit Fields**: Added `Actor` and `EvidenceLinks` fields to Decision struct with migration 00006

**Additional Fixes:**
- Fixed materialize INSERT column count mismatch (removed `current_attempt`)
- Added shared volumes for artifacts and graders in docker-compose.yml

**Verification:**
- End-to-end test: 48/48 trials completed successfully
- Experiment state transition: QUEUED → RUNNING → COMPLETED
- Grading snapshot created, release decision generated with audit fields
- All artifact files written and readable by grading service

**Documentation:**
- Complete audit with fix status: `docs/audits/2026-08-27-m6-implementation-alignment.md`
- Fix summary: `docs/audits/M6-FIXES-SUMMARY.md`
- Status update: `docs/audits/FIXES-STATUS-UPDATE.md`

**Commit:** d475167

## Archive Index

详细的每里程碑 brief/spec/verification 记录保存在归档目录中，此处仅作索引。更早的里程碑条目不再在此展开。

| Milestone | Archive | Verification 结论 |
|---|---|---|
| M0 Evaluation Baseline | `docs/comet/archive/2026-08-19-m0-evaluation-baseline/` | A1–A89 全部 passed |
| M1 Registry & Manifest Compiler | `docs/comet/archive/2026-08-25-m1-registry-manifest-compiler/` | 通过并归档（含 5 轮收敛过程） |
| M2 Scheduler Reliability Core | `docs/comet/archive/2026-08-25-m2/` | 真实 PostgreSQL 故障测试通过 |
| M3 Runner Protocol & Fixture Worker | `docs/comet/archive/2026-08-25-m3-runner-protocol/` | 协议闭环 + Compose 一键演示通过 |
| M4 Execution Projection & Sandbox | `docs/comet/archive/2026-08-26-m4/` | 全部 8 项验收通过 |
| M5 Grading Pipeline | `docs/comet/archive/2026-08-26-m5/` | 通过并归档（commit 62f19eb） |
| M6 Release Gate | `docs/comet/archive/2026-08-26-m6/` | 通过并归档；P0/P1 审计修复见上 |
| M7 Web UI | `docs/comet/archive/2026-08-27-m7-web-ui/` | 8/8 验收标准通过 |

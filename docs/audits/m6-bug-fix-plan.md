# M6 Bug Fix Plan

**Created:** 2026-08-27  
**Based on:** `2026-08-27-m6-implementation-alignment.md` §0 二次核验勘误  
**Status:** READY FOR EXECUTION

## 验证结论

经过与设计文档的深度交叉核验，审计文档 §0 列出的 P0/P1 问题**全部属实**：

### P0 问题（阻断 M5/M6 happy path）

1. ✅ **Binary 分裂**：`skillgate serve` 注册 RPC 但不跑 grading；`cmd/control-plane` 跑 grading 但不注册 RPC
   - 设计文档：`system-architecture.md` §3 要求 "Go Control Plane 负责 Metric Aggregation 和 Release Decision"
   - 实际：`cmd/control-plane/main.go:142` 是唯一调用 `ProcessExperiment` 的地方
   - 影响：deploy 路径实验停在 GRADING，无人处理

2. ✅ **空 grades 短路**：`{}` 被当作已评分，executor 被跳过
   - 设计文档：`grading-contract.md` §6 要求 `status: "scored|failed|incomplete|skipped|invalid"`
   - 实际：`internal/grading/service.go:162` `if len(trial.Grades) > 0 { continue }`
   - 影响：fixture 路径 executor 从不执行

3. ✅ **Grader hash 不匹配**：manifest 用 canonical hash，registry 用 raw bytes hash
   - 设计文档：`grading-contract.md` §7 要求 "grader_hash 必须由内容确定性计算"
   - 实际：`internal/manifest/manifest.go:283` vs `internal/grader/registry.go:48`
   - 影响：`Get(graderHash)` 永远失败

4. ✅ **Grader schema 不兼容**：DeterministicGraderSet vs Grader
   - 设计文档：`grading-contract.md` §4 列出 deterministic grader 类型
   - 实际：`evals/csv-analysis/grader.yaml` 是 `kind: DeterministicGraderSet`，registry 要求 `kind: Grader` + `spec.type`/`spec.method`
   - 影响：即便 hash 修好，schema 验证仍会拒绝

5. ✅ **Worker 不落盘**：fixture_worker 不写实际文件
   - 设计文档：`runner-protocol.md` §7 要求 "artifacts 包含 local_path 和 sha256"
   - 实际：`workers/python/fixture_worker/worker.py` 只声明 metadata，不写文件
   - 影响：缺 finding → HOLD（契约 fail-closed），不是 crash

6. ✅ **langgraph_worker 语法错误**：line 364 IndentationError
   - 设计文档：`runner-protocol.md` §3 要求 `protocol_version: "runner.v1"`
   - 实际：line 364 语法错误，`protocol_version="v1"`，hash 截 8 位
   - 影响：文件无法 import

7. ✅ **根 docker-compose 损坏**：调用不存在的 bootstrap 命令
   - 设计文档：`system-architecture.md` §4 描述 docker compose 部署
   - 实际：根 `docker-compose.yml` 调用 `/app/skillgate bootstrap`（不存在）
   - 影响：bootstrap 容器失败，control-plane 永远等待

### P1 问题（真缺陷，非当前崩点）

1. ✅ **路径遍历**：validateArtifactManifests 不约束 LocalPath
   - 设计文档：`threat-model.md` 要求隔离 artifact 路径
   - 实际：`internal/runner/service.go:496` 直接 `os.Open(artifact.LocalPath)`
   - 影响：半可信 Worker 可读任意文件

2. ✅ **ValidatePairing 死代码**：contract §8 invariant #2 未 enforce
   - 设计文档：`system-architecture.md` §8 "Baseline/Candidate Pair 的非 Treatment Identity 不一致时不能比较"
   - 实际：`internal/metrics/aggregator.go:150` 定义但从未调用
   - 影响：不同 model/environment 的 trial 可能误配对

3. ✅ **Decision 缺字段**：Actor / EvidenceLink
   - 设计文档：`release-gate-policy.md` §6 要求 "Decision 时间和执行者"
   - 实际：`internal/releasegate/evaluate.go` 没有 Actor 字段
   - 影响：审计追溯不完整

## 修复计划

### Phase 1: P0 修复（阻断路径）

按 §0.4 优先级顺序：

#### Task 1: 统一 binary（P0-1）
- **Target**: `cmd/skillgate/serve_command.go`, `cmd/control-plane/main.go`
- **Action**: 把 `startGradingPoller` 移到 `skillgate serve`
- **Verify**: deploy compose 启动后实验能从 GRADING → COMPLETED
- **Owner**: Agent-1 (sonnet)

#### Task 2: 修复空 grades 判定（P0-2）
- **Target**: `internal/grading/service.go`, `internal/runner/service.go`
- **Action**: 区分未评分 vs 空 manifest；改写 ProcessExperiment 判定逻辑
- **Verify**: fixture 路径 executor 被调用
- **Owner**: Agent-2 (sonnet)

#### Task 3: 统一 grader hash（P0-3）
- **Target**: `internal/grader/registry.go`
- **Action**: `calculateHash` 改用 canonical JSON
- **Verify**: manifest grader_hash == registry key
- **Owner**: Agent-3 (sonnet)

#### Task 4: 适配 DeterministicGraderSet（P0-4）
- **Target**: `internal/grader/registry.go`, `internal/manifest/manifest.go`
- **Action**: 展开 DeterministicGraderSet 为多个 Grader 或接受该 kind
- **Verify**: `evals/csv-analysis/grader.yaml` 能被 registry 接受
- **Owner**: Agent-3 (sonnet, 与 Task 3 合并)

#### Task 5: Worker 真实落盘（P0-5）
- **Target**: `workers/python/fixture_worker/worker.py`
- **Action**: 写 output / security-finding.json 到约定路径
- **Verify**: grading 能读到文件
- **Owner**: Agent-4 (sonnet)

#### Task 6: 修复 langgraph_worker（P0-6）
- **Target**: `workers/python/langgraph_worker/worker.py`
- **Action**: 修正语法、protocol_version、capability、hash 格式
- **Verify**: `python -m langgraph_worker` 能启动
- **Owner**: Agent-5 (sonnet)

#### Task 7: 清理根 compose（P0-7）
- **Target**: `docker-compose.yml`, `docker-compose.yaml`
- **Action**: 删除或改写为调用 deploy/compose-bootstrap.sh
- **Verify**: 文档指向明确
- **Owner**: Agent-6 (sonnet)

### Phase 2: P1 修复（安全与契约）

#### Task 8: 约束 artifact 路径（P1-1）
- **Target**: `internal/runner/service.go`
- **Action**: validateArtifactManifests 加根目录检查
- **Verify**: 越界路径被拒绝
- **Owner**: Agent-7 (sonnet)

#### Task 9: Enforce ValidatePairing（P1-2）
- **Target**: `internal/grading/service.go`
- **Action**: ProcessExperiment 调用 ValidatePairing
- **Verify**: identity 不匹配 pair 被标记
- **Owner**: Agent-8 (sonnet)

#### Task 10: 补全 Decision 字段（P1-3）
- **Target**: `internal/releasegate/evaluate.go`
- **Action**: 加 Actor / EvidenceLink
- **Verify**: decision JSON 包含新字段
- **Owner**: Agent-9 (sonnet)

### Phase 3: 验证与文档

#### Task 11: 端到端测试
- **Action**: 用 deploy compose 跑完整流程
- **Verify**: materialize → claim → execute → grading → decision → PROMOTE/HOLD
- **Owner**: 主 agent (验证全部修复)

#### Task 12: 更新文档
- **Target**: 
  - `docs/audits/2026-08-27-m6-implementation-alignment.md` 添加 "修复完成" 标记
  - `docs/contracts/runner-protocol.md` 同步 ClaimTrial 命名
  - `docs/architecture/execution-lifecycle.md` 同步 COMPLETED 状态
- **Owner**: 主 agent

## 工作量估算

| Phase | Tasks | Est. Time | Parallel |
|-------|-------|-----------|----------|
| P0 修复 | 7 | ~6h | 可并行（6 agents） |
| P1 修复 | 3 | ~2h | 可并行（3 agents） |
| 验证 | 2 | ~1h | 串行 |
| **Total** | **12** | **9h** | **最快 3h（6 并行）** |

## 依赖关系

```
Task 1 (binary) ──┐
Task 2 (grades) ──┼─→ Task 11 (e2e)
Task 3,4 (grader)─┤
Task 5 (worker) ──┤
Task 6 (langgraph)┘

Task 8,9,10 (P1) ──→ Task 12 (docs)
```

P0 修复可并行；Task 11 需要全部 P0 完成；P1 可独立并行。

## 执行策略

使用 Sonnet 模型并行执行：
- **Agent 1-6**: 各自处理一个 P0 Task
- **Agent 7-9**: P0 完成后处理 P1
- **主 Agent**: 协调、验证、更新文档

## 成功标准

1. ✅ `deploy/docker-compose.yml` 启动后实验能完整跑通
2. ✅ M3 fixture_worker 流程下 grading 被执行
3. ✅ Grader hash 匹配，DeterministicGraderSet 被接受
4. ✅ Worker 写出真实文件，grading 能读到
5. ✅ langgraph_worker 能正常启动
6. ✅ 根 compose 文件被清理或修正
7. ✅ 审计文档标记"修复完成"

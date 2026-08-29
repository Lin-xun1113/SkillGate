# Worker 实现指南

> 状态：与 2026-08-27 代码库同步 · 参考实现：`workers/python/langgraph_worker/`

本文说明如何从零实现一个能接入 SkillGate 的 Trial Worker。建议对照 LangGraph Worker 源码阅读——它就是按这个步骤写的。

## Worker 的职责边界

**做**：

- 领取 Trial、校验请求、执行 Agent、上报事件与用量、幂等提交结果；
- 在沙箱内运行不可信内容（Skill、Case 输入都是不可信数据）。

**不做**（这是 Worker 不可逾越的红线）：

- **不能选择与 Request Identity 不同的 Model、Skill、Endpoint 或 Credential**（架构不变量）；
- **不能在租约失效后继续提交**；
- **不能自己决定重试**——重试是 Scheduler 的事。

## 实现步骤

### 第 1 步：注册

```
RegisterWorker(worker_id, protocol_version, worker_version,
               capabilities{harnesses, graders, sandbox_profiles, max_concurrency},
               environment{os, arch, hostname, python_version})
```

拿到 `session_token` 和 `heartbeat_interval_ms`——把它记下来，之后所有调用都要带。

### 第 2 步：领取与校验

```
ClaimTrial(session_token, harness, sandbox_profile, lease_duration_sec)
```

拿到 Trial 后、执行前，做四项校验（任一失败 → `FailTrial(PERMANENT)`，**不要执行**）：

1. **request_hash 校验**：对 `experiment_id/pair_id/arm/attempt` 按 M3 冻结口径重算 Canonical Hash，与 `request_hash` 比对；
2. **execution_hash 校验**：对执行段（skill_hash/case_id/case_input/model/tool_policy/environment）重算，与 payload 中的独立 Hash 比对；
3. **Skill 校验**：`skill_hash` 非空时，从 CAS 读取 Skill 内容并验证其 Hash——内容对不上拒绝执行；
4. **能力校验**：`harness`、`sandbox_profile` 是否是自己声明过且支持的。

参考实现：`worker.py` 的 `verify_hashes`、`_compute_request_hash`、`_compute_execution_hash`、`load_skill`。

### 第 3 步：执行循环

执行期间并行做三件事：

**a) 心跳**（按 `heartbeat_interval_ms` 周期）：

```python
resp = stub.Heartbeat(HeartbeatRequest(
    worker_id=..., session_token=..., trial_id=...,
    lease_token=..., lease_generation=generation,
    phase=current_phase,           # 如 "loading_skill" / "running_graph"
    event_sequence=latest_seq,
    resource_usage=ResourceUsage(...),
    started_at_unix_ms=...))
# CONTINUE → 继续; CANCEL_REQUESTED → 协作式中断;
# LEASE_REJECTED / SERVER_SHUTDOWN → 立即停止, 不再提交
```

**b) 事件流**：LLM 调用、工具调用、阶段变化都应上报 `ReportEvent`：

- `sequence` 单调递增（服务端检测空洞）；
- `event_id` 唯一（服务端按 event_id/payload_hash 去重——网络重发是安全的）；
- 至少一次投递语义：没收到 ACK 就重发，重复会被 `DUPLICATE_IGNORED`。

**c) 产物落盘**：Agent 输出写到声明的 outputs 路径（沙箱内 workspace），Trace 与 Artifact 文件写入 artifacts 目录。

### 第 4 步：提交结果

```
CompleteTrial(..., idempotency_key=唯一键,
              request_hash=校验值, final_sequence=最后事件序号,
              outcome="SUCCEEDED", outcome_manifest_json=结果清单,
              artifacts=[ArtifactManifest(sha256, size_bytes, storage_key…)],
              usage=ResourceUsage(input_tokens, output_tokens, elapsed_ms, tool_calls…))
```

要点：

- **幂等键**：同一 Attempt 内固定不变；网络超时后**安全重发**——服务端返回 `ALREADY_COMMITTED` 即代表成功；
- **Artifact Manifest**：每个文件带 SHA-256 与大小；评分器按 Hash 读取，写错了会导致评分失败；
- **用量**：`ResourceUsage` 进入报告的 `resource_usage`，漏报会让成本指标缺失。

### 第 5 步：失败上报

任何执行期失败都走 `FailTrial`，并给出**准确的 `failure_category`**：

| 场景 | 类别 |
|---|---|
| 模型 API 5xx / 网络抖动 | `TRANSIENT` |
| Prompt 超预算被拒、参数非法 | `PERMANENT` |
| 执行超过尝试时限 | `TIMEOUT` |
| 容器创建/启动失败 | `SANDBOX_ERROR` |
| Worker 自身 bug | `INTERNAL` |

分类错误会浪费重试预算（把 PERMANENT 报成 TRANSIENT）或白白的终态（反之）。响应 `RETRY_SCHEDULED` 时携带 `next_attempt_no`——同一 Logical Trial 的新 Attempt 会以新的 lease_generation 重新开始。

Provider 错误在 Control Plane 的固定映射为：`TRANSIENT → PROVIDER_TRANSIENT`、
`TIMEOUT → LEASE_TIMEOUT`（现有 deadline 重试桶）、`PERMANENT →
DETERMINISTIC_FAILURE`、`INTERNAL → WORKER_LOST`。protobuf enum 数值保持兼容，
未知/未指定类别按永久失败处理。

真实 Provider Adapter 必须遵守 [`Provider Runtime Contract`](../../contracts/provider-runtime.md)：Endpoint/Key 只能来自 Operator Environment；429/5xx/网络错误为 `TRANSIENT`，Provider Timeout 为 `TIMEOUT`，鉴权、参数和响应契约错误为 `PERMANENT`。SDK 内 Retry 默认关闭，由 Scheduler 统一决定跨 Attempt Retry。

## 取消与关闭的正确姿势

收到 `CANCEL_REQUESTED` 后：

1. 停止派发新的 Agent 步骤（LangGraph 场景：中断图执行）；
2. 终止沙箱容器（Control Plane 侧也会兜底清理）；
3. `CompleteTrial(outcome="CANCELLED")` 或按状态上报——不要当作成功提交。

进程收到 SIGTERM：先完成当前 RPC、停止 Claim 新 Trial、再退出。优雅关闭让 Scheduler 不必等待租约过期。

## 并发模型

- 每个 Trial 一个独立沙箱容器（Control Plane 的 Sandbox Manager 负责）；
- Worker 自身可并发处理多个 Trial（`max_concurrency` 声明能力），但**每个 Trial 的租约/事件/提交必须独立管理**；
- 线程/协程处理心跳时注意与主执行循环共享状态的一致性（参考实现用后台心跳线程 + 主循环的取消标志位）。

## 常见陷阱清单

- [ ] 执行前**没有**校验双 Hash → 会执行被篡改的载荷，协议违规
- [ ] 幂等键每次重试都换 → 同一 Attempt 可能产生两条 Result
- [ ] 心跳不带 `lease_generation` → 旧代数心跳被拒后继续执行，结果注定 `LEASE_EXPIRED`
- [ ] 事件 sequence 有洞不处理 → `SEQUENCE_GAP`，审计链不完整
- [ ] 把可重试错误报成 PERMANENT → 白白浪费本可恢复的 Trial
- [ ] Artifact 没算 SHA-256 / 大小写错 → 评分器读不到证据，Trial 无法计分
- [ ] 在租约过期后「坚持」提交 → 被拒绝是正常行为，不要重试到天荒地老
- [ ] 自作主张选了别的模型/端点 → 架构不变量违规，结果身份失效

## 本地调试

```bash
# 1. 起 Control Plane
skillgate serve --grpc-addr 127.0.0.1:50051 --database-url-file "$SKILLGATE_DATABASE_URL_FILE"

# 2. 物化一个小实验（或用内置 48-Trial 示例）
skillgate experiment materialize experiments/csv-analysis-v1-demo.yaml --database-url-file "$SKILLGATE_DATABASE_URL_FILE"

# 3. 准备 Worker 只读 CAS
skillgate cas prepare experiments/csv-analysis-v1-demo.yaml --project-root . --cas-dir ./.skillgate/cas

# 4. 跑参考 Worker 观察 RPC 序列
cd workers/python
SKILLGATE_EVALS_ROOT="../../evals/csv-analysis" PYTHONPATH=".:gen" python3 -m langgraph_worker \
  --server 127.0.0.1:50051 --worker-id dev-01 \
  --cas ../../.skillgate/cas --artifacts ../../artifacts
```

先跑通 `fixture_worker`（零依赖、确定性）再上真实 Provider，是最平滑的路径。

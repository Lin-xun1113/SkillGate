# Runner Protocol

> 状态：与 2026-08-27 代码库同步 · 权威契约：`proto/runner/v1/runner.proto` 与 `docs/contracts/runner-protocol.md`

Runner Protocol 是 Control Plane 与 Trial Worker 之间的 gRPC 契约。它**语言无关**：Go 服务端 + 两个 Python 参考实现（langgraph_worker、fixture_worker），任何语言实现同一契约都能接入。

## 服务定义

```protobuf
service RunnerService {
  rpc RegisterWorker(RegisterWorkerRequest) returns (RegisterWorkerResponse);
  rpc ClaimTrial(ClaimTrialRequest)         returns (ClaimTrialResponse);
  rpc Heartbeat(HeartbeatRequest)           returns (HeartbeatResponse);
  rpc ReportEvent(ReportEventRequest)       returns (ReportEventResponse);
  rpc CompleteTrial(CompleteTrialRequest)   returns (CompleteTrialResponse);
  rpc FailTrial(FailTrialRequest)           returns (FailTrialResponse);
}
```

## Worker 生命周期

```
RegisterWorker ──▶ session_token + 心跳参数
      │
      ▼ (循环)
ClaimTrial ──▶ 有 Trial? ──▶ lease_token + lease_generation + request_hash
      │ 无 Trial: 稍后再 Claim
      ▼
  执行循环:  ReportEvent(带 sequence) + Heartbeat(续租/感知取消)
      │
      ├──▶ 成功: CompleteTrial (idempotency_key + artifacts + usage)
      └──▶ 失败: FailTrial (failure_category 决定重试或终态)
```

## 六个 RPC 详解

### 1. RegisterWorker — 注册

**请求**：`worker_id`、`protocol_version`、`worker_version`、`capabilities`（harnesses/graders/sandbox_profiles/max_concurrency）、`environment`（os/arch/hostname/python_version）、`metadata`。

**响应**：`accepted`、`session_token`（后续所有调用的凭证）、`heartbeat_interval_ms`、`max_lease_ms`、`server_protocol_version`。

被拒绝时：`accepted=false` + `rejected_capabilities` + `reject_reason`。

### 2. ClaimTrial — 领取

**请求**：`worker_id`、`session_token`、`harness`、`sandbox_profile`、`lease_duration_sec`、`preferred_experiment_id`（可空）。

**响应**：

- `has_trial=false`：队列空，稍后再来；
- `has_trial=true`：`trial_id`、`logical_trial_id`、`experiment_id`、`pair_id`、`arm`、`attempt_no`、**`lease_token`**、**`lease_generation`**、`lease_expires_at_unix_ms`、**`request_hash`**、`trial_request_json`（完整执行说明）。

服务端用 PostgreSQL `FOR UPDATE SKIP LOCKED` 保证同一 Trial 不会被两个 Worker 同时领到。

### 3. Heartbeat — 心跳（续租 + 取消通道）

**请求**：身份四元组（worker/session/trial/lease_token）+ `lease_generation` + `phase` + `event_sequence` + `resource_usage`。

**响应**（`HeartbeatAction` 枚举）：

| action | 含义 | Worker 应做什么 |
|---|---|---|
| `CONTINUE` | 一切正常，租约已续 | 继续执行 |
| `CANCEL_REQUESTED` | 实验被取消 | 协作式中断：停 Agent 图、终止沙箱、上报 CANCELLED |
| `LEASE_REJECTED` | 租约已失效（过期/代数旧） | 停止工作；结果不再有效 |
| `SERVER_SHUTDOWN` | 服务端关闭 | 尽快安全退出 |

### 4. ReportEvent — 事件流

结构化执行事件（LLM 调用、工具调用、阶段变化），字段含 `event_id`、`sequence`（单调递增）、`event_type`、`occurred_at_unix_ms`、`payload_json` + `payload_hash`。

**响应状态**（`EventReportStatus`）：

| status | 含义 |
|---|---|
| `ACCEPTED` | 已入库 |
| `DUPLICATE_IGNORED` | event_id/payload_hash 重复——至少一次投递下的正常去重 |
| `SEQUENCE_GAP` | 序列号有空洞（提示乱序/丢失） |
| `REJECTED` | 校验失败（身份/租约） |

### 5. CompleteTrial — 幂等提交成功结果

**请求**：身份四元组 + `lease_generation` + **`idempotency_key`** + `request_hash` + `final_sequence` + `exit_code` + `outcome`（`SUCCEEDED`/`FAILED`/`TIMED_OUT`/`CANCELLED`）+ `outcome_manifest_json` + `artifacts[]`（每个含 `sha256`/`size_bytes`/`storage_key`）+ `usage`（tokens/elapsed/tool_calls/peak_memory）+ `grades_json`。

**响应**（`CompletionStatus`）：

| status | 含义 | Worker 该怎么办 |
|---|---|---|
| `COMMITTED` | 首次提交成功 | 结束本 Trial |
| `ALREADY_COMMITTED` | 幂等键命中，重复提交 | 同上——结果已存在，不产生第二条 |
| `LEASE_EXPIRED` | 租约失效 | 放弃；Trial 可能已被重试 |
| `CONFLICT` | 结果与已有记录冲突 | 放弃；人工排查 |
| `INVALID_REQUEST` | 请求不合法 | 修复后重试 |

Result、终态与 Experiment 计数在**同一事务**中更新。

### 6. FailTrial — 上报失败

**请求**：身份四元组 + **`failure_category`** + `error_message` + `outcome_manifest_json` + `request_hash`。

失败类别（`FailureCategory`）：

| category | 语义 | 服务端行为 |
|---|---|---|
| `TRANSIENT` | 可重试的临时错误 | 按退避策略重新排队（`RETRY_SCHEDULED` + next_attempt_no） |
| `PERMANENT` | 确定性失败 | 终态 `TERMINAL_FAILED` |
| `TIMEOUT` | 超时 | 按超时策略处理 |
| `SANDBOX_ERROR` | 沙箱故障 | 通常可重试 |
| `INTERNAL` | Worker 内部错误 | 保守处理 |

**响应**（`FailTrialStatus`）：`RECORDED` / `RETRY_SCHEDULED`（附 `next_attempt_no`）/ `TERMINAL_FAILED` / `LEASE_EXPIRED` / `REJECTED`。

## 双 Hash 校验（协议的核心安全机制）

`trial_request_json` 包含两个独立 Hash：

| Hash | 覆盖内容 | 防什么 |
|---|---|---|
| `request_hash`（trial_request_hash） | experiment_id / pair_id / arm / attempt（M3 冻结定义） | 执行的不是调包过的任务 |
| `execution_hash` | skill_hash / case_id / case_input / model / tool_policy / environment | 执行内容被篡改 |

Worker 在执行前必须重算并校验两者（参考实现 `worker.py` 的 `verify_hashes`）；不一致立即 `FailTrial(PERMANENT)`，绝不执行来路不明的载荷。

## Fence（lease_generation）

每次重新 Claim 使 `lease_generation` 单调递增。所有携带**旧 generation** 的调用（心跳、事件、提交）都会被拒绝。这杜绝了经典竞态：Worker A 卡顿→租约过期→Trial 租给 B→A 醒来提交陈旧结果。A 的提交会被 Fence 挡下，B 的结果才是唯一有效结果。

## 版本兼容

- `protocol_version` / `server_protocol_version` 双向声明；
- 语义化演进：新增字段走 proto 的新编号，删除/改义字段需要 bump 主版本；
- 能力协商：Worker 声明 capabilities，服务端可拒绝不支持的项（`rejected_capabilities`）。

## 参考实现

| 实现 | 位置 | 用途 |
|---|---|---|
| Python LangGraph Worker | `workers/python/langgraph_worker/` | 沙箱内真实 Agent 执行 |
| Python Fixture Worker | `workers/python/fixture_worker/` | 无 LLM 的确定性协议闭环/演示 |
| Go 服务端 | `internal/runner/` | `skillgate serve` |

代码生成：Go 桩在 `gen/go/runner/v1`，Python 桩在 `workers/python/gen`。

实现自己的 Worker 请继续读[Worker 实现指南](worker-guide.md)。

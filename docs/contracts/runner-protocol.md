# Runner Protocol Contract

**状态：** `PROPOSED`，目标在 Milestone 1 冻结 v0  
**用途：** 定义 Go Control Plane 与 Trial Worker 之间的语言无关边界

## 1. 设计原则

- Go 不依赖 Python/LangGraph 的内部类和状态结构；
- Worker 只能执行 Request 中声明的 Trial；
- 所有消息可校验、可版本化、可重试；
- Event 和 Result 使用内容 Hash 保证证据链；
- 协议只描述控制面，不把完整 Prompt/Trace 放入高频控制消息。

## 2. v0 传输方式

MVP 使用 gRPC。大文件通过 MinIO/S3 预签名 URL 或 Worker 身份上传，完成消息只提交 Artifact Manifest。

概念性 Service：

```protobuf
service RunnerControl {
  rpc RegisterWorker(RegisterWorkerRequest) returns (RegisterWorkerResponse);
  rpc LeaseTrial(LeaseTrialRequest) returns (LeaseTrialResponse);
  rpc Heartbeat(HeartbeatRequest) returns (HeartbeatResponse);
  rpc ReportEvent(ReportEventRequest) returns (ReportEventResponse);
  rpc CompleteTrial(CompleteTrialRequest) returns (CompleteTrialResponse);
  rpc FailTrial(FailTrialRequest) returns (FailTrialResponse);
}
```

实际 `.proto` 在 Contract 冻结后生成；本文件是语义规范，不是最终代码。

## 3. Worker 注册

Worker 启动时发送：

```json
{
  "worker_id": "worker_01...",
  "protocol_version": "runner.v1",
  "worker_version": "langgraph-worker@0.1.0",
  "capabilities": {
    "harnesses": ["langgraph"],
    "graders": ["deterministic", "llm"],
    "sandbox_profiles": ["docker-restricted-v1"],
    "max_concurrency": 2
  },
  "environment": {
    "os": "linux",
    "arch": "amd64"
  }
}
```

Server 返回：

- Worker Session Token；
- 心跳间隔；
- 最大 Lease 时间；
- Server Protocol Version；
- 被拒绝的 Capability（如有）。

Worker 不得通过注册消息声明或上传 Provider Secret。

## 4. Lease Request

```json
{
  "worker_id": "worker_01...",
  "session_token": "...",
  "capability_filter": {
    "harness": "langgraph",
    "sandbox_profile": "docker-restricted-v1"
  },
  "max_items": 1
}
```

Lease Response 必须包含：

```json
{
  "trial_id": "trial_01...",
  "lease_token": "lease_...",
  "lease_expires_at": "2026-08-19T00:03:00Z",
  "request_hash": "sha256:...",
  "trial_request": {
    "experiment_id": "exp_01...",
    "pair_id": "pair_01...",
    "arm": "with_skill",
    "case": {"id": "csv-explicit-001", "prompt_ref": "..."},
    "strategy_snapshot_ref": "...",
    "skill_snapshot_refs": [],
    "grader_snapshot_refs": [],
    "sandbox_profile": "docker-restricted-v1",
    "budget": {}
  }
}
```

Worker 必须重新计算 Request Hash，任何不匹配都应拒绝执行。

## 5. Heartbeat

```json
{
  "trial_id": "trial_01...",
  "lease_token": "lease_...",
  "worker_id": "worker_01...",
  "phase": "invoke_agent",
  "event_sequence": 14,
  "started_at": "2026-08-19T00:00:10Z",
  "resource_usage": {
    "input_tokens": 1200,
    "output_tokens": 900,
    "elapsed_ms": 4200
  }
}
```

Heartbeat Response 可能包含：

- `continue`；
- `cancel_requested`；
- `lease_extended`；
- `lease_rejected`；
- `server_shutdown`。

## 6. Event 上报

Event 采用追加式 Sequence：

```json
{
  "event_id": "evt_01...",
  "trial_id": "trial_01...",
  "attempt_id": "attempt_01...",
  "sequence": 15,
  "event_type": "tool_call.finished",
  "occurred_at": "2026-08-19T00:00:15Z",
  "payload": {
    "tool": "filesystem.read",
    "status": "ok"
  },
  "payload_hash": "sha256:..."
}
```

同一 `event_id` 重复上报必须返回已存在结果；Sequence 缺口应产生诊断，不应静默补齐。

## 7. Completion

Worker 完成后发送：

```json
{
  "trial_id": "trial_01...",
  "attempt_id": "attempt_01...",
  "lease_token": "lease_...",
  "idempotency_key": "sha256:...",
  "request_hash": "sha256:...",
  "final_sequence": 42,
  "status": "succeeded",
  "result": {
    "exit_code": 0,
    "final_output_ref": "artifact_01...",
    "usage": {
      "input_tokens": 12000,
      "output_tokens": 3400,
      "elapsed_ms": 54200,
      "tool_calls": 18
    }
  },
  "artifacts": [],
  "grades": []
}
```

Go Server 必须校验：Lease、Request Hash、Artifact Hash、状态转换、Budget 和 Grader Identity。

## 8. 错误码

| Code | 含义 | Worker 行为 |
|---|---|---|
| `INVALID_REQUEST` | Request Schema 或 Hash 无效 | 不重试，记录并失败 |
| `LEASE_EXPIRED` | Lease 已过期 | 停止提交，保存本地诊断 |
| `LEASE_CONFLICT` | Lease 被其他 Worker 持有 | 停止执行 |
| `CANCELLED` | Experiment/Trial 已取消 | 终止 Sandbox，提交部分 Evidence |
| `BUDGET_EXCEEDED` | Budget 已耗尽 | 不重试 |
| `RETRYABLE_SERVER` | 服务临时故障 | 根据 Backoff 重试提交 |
| `DUPLICATE_RESULT` | 结果已提交 | 读取并接受已存在结果 |
| `INCOMPLETE_EVIDENCE` | 必需 Artifact 缺失 | 标记 Incomplete |
| `PROTOCOL_UNSUPPORTED` | 协议版本不兼容 | 不执行 |

## 9. 兼容性规则

- `protocol_version` 使用 Major/Minor；
- Major 不同不能通信；
- Minor 增加字段必须向后兼容；
- 未识别字段可忽略，但必需字段不能缺失；
- Worker 应在 Result 中回报实际 Protocol/Worker Version；
- 破坏性变更必须创建新 Service 或新 Major Version。

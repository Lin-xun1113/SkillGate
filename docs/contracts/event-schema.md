# Event Schema Contract

**状态：** `PROPOSED`，用于 v0 Trace/Event 设计

## 1. Event Envelope

所有 Domain Event 和 Worker Event 使用统一 Envelope：

```json
{
  "schema_version": "event.v1",
  "event_id": "evt_01J...",
  "event_type": "trial.started",
  "occurred_at": "2026-08-19T00:00:00.123Z",
  "producer": {
    "component": "langgraph-worker",
    "version": "0.1.0",
    "worker_id": "worker_01..."
  },
  "correlation": {
    "request_id": "req_01...",
    "experiment_id": "exp_01...",
    "trial_id": "trial_01...",
    "pair_id": "pair_01...",
    "attempt_id": "attempt_01..."
  },
  "sequence": 1,
  "payload_hash": "sha256:...",
  "payload": {}
}
```

## 2. 字段规则

| 字段 | 规则 |
|---|---|
| `schema_version` | 版本化，破坏性变化升级 Major |
| `event_id` | 全局唯一，重复上报必须幂等 |
| `event_type` | 小写点分隔名称 |
| `occurred_at` | UTC RFC3339，表示事件发生时间 |
| `producer` | 产生事件的组件和版本 |
| `correlation` | 至少包含相关的 Experiment/Trial ID |
| `sequence` | 同一个 Attempt 内单调递增 |
| `payload_hash` | Canonical JSON Payload 的 SHA-256 |
| `payload` | 必须符合对应 Event Type Schema |

## 3. Event Type

### Control Plane Event

```text
experiment.created
experiment.validating
experiment.compiled
experiment.queued
experiment.started
experiment.cancelling
experiment.decided
experiment.failed
trial.created
trial.leased
trial.lease_expired
trial.started
trial.retry_scheduled
trial.cancelled
trial.completed
trial.failed
result.committed
metric.snapshot_created
release.decision_created
```

### Worker Event

```text
worker.request_verified
sandbox.prepared
skill.mounted
agent.graph_started
agent.graph_finished
model_call.started
model_call.finished
tool_call.started
tool_call.finished
grader.started
grader.finished
artifact.created
artifact.uploaded
worker.cancel_received
worker.error
```

## 4. Payload 示例

### `skill.mounted`

```json
{
  "skill_name": "csv-analysis",
  "skill_version": "sha256:abc...",
  "mount_path": "/workspace/skills/csv-analysis",
  "mount_mode": "autonomous_discovery",
  "verified": true
}
```

`mount_path` 只在受保护的内部 Trace 中使用；对外 Report 应优先显示相对路径或 Artifact ID。

### `model_call.finished`

```json
{
  "provider": "openai",
  "model": "gpt-example",
  "status": "ok",
  "input_tokens": 1200,
  "output_tokens": 500,
  "cached_tokens": 0,
  "latency_ms": 2300,
  "response_artifact": "art_01..."
}
```

不能把完整 Prompt 或 Response 直接放入普通 Event Payload；应通过脱敏后的 Artifact 引用保存。

### `tool_call.finished`

```json
{
  "tool": "filesystem.read",
  "arguments_hash": "sha256:...",
  "result_artifact": "art_01...",
  "status": "ok",
  "sandbox_policy": "allowed",
  "elapsed_ms": 42
}
```

## 5. Redaction 规则

以下内容默认禁止进入普通 Log/Event：

- API Key、Cookie、Bearer Token、Private Key；
- 完整用户 Prompt（如果包含敏感数据）；
- 未脱敏文件内容；
- Provider 返回的原始错误中可能包含的 Credential；
- 任意长度的 Model Response。

脱敏后仍要保存：

- 原始 Artifact Hash；
- Redaction Rule Version；
- 是否发生脱敏；
- 脱敏后的 Artifact URI。

## 6. Event 顺序与一致性

- Event Delivery 至少一次；Consumer 按 `event_id` 去重；
- 同一 Attempt 的 Sequence 应递增；
- Sequence 缺失时标记 Trace 不完整；
- Event 不能直接作为生命周期 State 的唯一事实来源；
- 关键 State Transition 和 Domain Event 应尽量在同一 DB Transaction 中提交。

## 7. Schema 演进

新增可选字段时保持向后兼容。删除、改名、改变语义时升级 Schema Major。每个 Event Type 应有 JSON Schema 或 Protobuf 定义，并提供旧版本 Fixture。

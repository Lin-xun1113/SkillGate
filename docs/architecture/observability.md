# 可观测性架构

**状态：** `ACCEPTED`，用于 MVP

## 1. 可观测性目标

系统应当能够回答：

- Experiment 或 Trial 当前处于什么状态？
- Trial 为什么变慢、重试或被拒绝？
- 哪条 Rule 选择了当前 Strategy？
- Worker 是否真的挂载了目标 Skill Hash？
- 两个 Arm 分别消耗了多少 Token、时间和成本？
- 评审者能否从 Dashboard 上的数字定位到 Raw Evidence？

## 2. 关联 ID

每个 Request 和 Event 至少携带：

```text
request_id
experiment_id
trial_id
pair_id
attempt_id
worker_id
trace_id
span_id（存在 Trace 时）
```

ID 必须是 opaque 且不包含 Secret。不能将 Prompt 或 Credential 放入 ID。

## 3. Trace

推荐的 Span 层级：

```text
http.request
  experiment.compile
    strategy.evaluate
    trial.enqueue
  scheduler.lease
  worker.run_trial
    sandbox.prepare
    agent.graph
      model.call
      tool.call
    grader.deterministic
    grader.llm
    artifact.upload
  result.commit
  metrics.aggregate
  release.decide
```

Span Attribute 必须有界并保持低 Cardinality：

- ID 和 Status；
- Model/Provider 名称（不能放 Prompt 文本）；
- Case Type/Domain；
- Arm；
- Retry Number；
- Resource Usage；
- Error Category。

大型 Prompt、Response 和 Trace 放到 Artifact 中，通过 Hash 引用。

## 4. Metric

### Control Plane

```text
skillgate_http_requests_total
skillgate_http_request_duration_seconds
skillgate_trials_pending
skillgate_trials_leased
skillgate_trials_running
skillgate_trial_lease_expirations_total
skillgate_trial_retries_total
skillgate_result_commit_conflicts_total
skillgate_experiment_duration_seconds
skillgate_policy_decisions_total{decision}
```

### Worker

```text
skillgate_worker_trials_total{status}
skillgate_worker_model_calls_total{provider,model,status}
skillgate_worker_model_latency_seconds
skillgate_worker_tool_calls_total{tool,status}
skillgate_worker_tokens_total{direction}
skillgate_worker_artifact_upload_bytes
skillgate_grader_verdicts_total{grader,status}
```

不要把 Prompt、完整 Skill Hash、Trial ID 或 User ID 作为 Prometheus 的高 Cardinality Label。它们应该放在 Log/Trace 中。

## 5. 结构化 Log

部署模式下每行 Log 都使用 JSON：

```json
{
  "ts": "2026-08-19T00:00:00Z",
  "level": "INFO",
  "component": "scheduler",
  "message": "trial leased",
  "request_id": "req_...",
  "experiment_id": "exp_...",
  "trial_id": "trial_...",
  "worker_id": "worker_...",
  "status": "LEASED"
}
```

默认不能记录完整 Prompt、Secret 或任意长度的 Model Output。

## 6. Audit Record

Audit Record 不等同于运维 Log。以下事件需要以不可变形式记录：

- Skill Version Registration；
- Suite/Strategy/Policy Publication；
- Experiment Compilation；
- Trial State Change；
- Result Commit；
- Release Decision；
- 后续可能增加的 Manual Override；
- Artifact Deletion/Tombstone。

## 7. Report 下钻路径

每一张 Aggregate Table 都应能沿着以下链路下钻：

```text
Metric → Case Contribution → Trial Attempt → Event Stream → Artifact Manifest → Sandbox Evidence
```

UI 可以默认隐藏敏感内容，但必须保留链路和状态信息。

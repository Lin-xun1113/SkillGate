# 可观测性运维手册

**状态：** `PROPOSED`

## 1. 日常检查

运行本地或 Demo 时，至少查看：

- API 健康状态；
- PostgreSQL 连接和慢查询；
- MinIO Bucket 可写性；
- Scheduler Pending/Leased/Expired 数量；
- Worker 在线数量和 Capability；
- Trial Retry、Timeout、Incomplete 数量；
- Provider Error 和 Rate Limit；
- Artifact Upload Failure；
- OTel Trace 是否能从 API 链接到 Worker。

## 2. 关键 Dashboard

建议提供四个面板：

### Experiment 总览

- Experiment 状态；
- 总 Trial/完成/失败/Incomplete；
- 当前并发；
- 预计和实际 Cost；
- 运行时长。

### Scheduler

- Queue Depth；
- Lease 延迟；
- Lease Expiration；
- Retry 次数；
- DB 锁等待；
- Worker 利用率。

### Agent/Provider

- Model Call Latency；
- 429/5xx；
- Token；
- Tool Call；
- Sandbox 启动耗时；
- Grader 耗时。

### Evaluation

- Baseline/Candidate Pass Rate；
- Case-level Lift；
- CI；
- Trigger Recall/Specificity；
- Security Finding；
- Release Decision。

## 3. 告警建议

MVP 可以先使用 Log/Metric 阈值，不必搭建复杂告警平台：

- Lease Expiration 连续升高；
- Result Commit Conflict 异常增加；
- Incomplete 比例超过 Suite Policy；
- Provider 429 达到预算阈值；
- Artifact Hash 校验失败；
- Security Critical/Confirmed Exploit；
- DB Connection Pool 耗尽；
- Worker 全部离线。

## 4. 排查顺序

```text
请求 ID
  → Experiment ID
    → Trial/Pair ID
      → Attempt
        → Event Sequence
          → Span
            → Artifact Manifest
              → Sandbox Evidence
```

先确认控制面状态，再看 Worker Trace，最后看 Model Output。不要只根据最后一段自然语言回答判断故障。

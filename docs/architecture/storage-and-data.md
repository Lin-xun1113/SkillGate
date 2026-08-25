# 存储与数据架构

**状态：** `PROVISIONAL → M2`；2026-08-25（UTC）Build 候选，等待 Native Verify

## 1. 存储分工

| 数据类别 | 存储位置 | 原因 |
|---|---|---|
| Registry 元数据、生命周期、Lease | PostgreSQL | 支持事务和查询 |
| 小型 Event Payload、Grade Summary | PostgreSQL JSONB/文本列 | 便于审计和聚合 |
| Trace、Prompt、Response、文件树 | MinIO/S3 | 适合大型不可变 Blob |
| Metric Snapshot | PostgreSQL + JSON Artifact | 让 Release Decision 可以事务性引用 |
| Telemetry | OTel Collector Backend | 用于运维分析，不作为业务正确性的依据 |

## 2. 内容寻址

对 Canonical Package/Archive 的字节内容计算 SHA-256：

```text
skill_hash = sha256(canonical_skill_archive)
suite_hash = sha256(canonical_suite_json)
strategy_hash = sha256(canonical_strategy_json)
policy_hash = sha256(canonical_policy_json)
artifact_hash = sha256(blob_bytes)
```

Canonicalization 规则必须集中实现并测试：

- 使用规范化的相对路径；
- 按稳定的字典序排列；
- 明确换行符规范；
- 禁止通过 Symbolic Link 越界；
- 明确 Archive Format 和 Version；
- 明确是否把 Metadata 纳入 Hash。

## 3. PostgreSQL 逻辑表

M2 Build 候选首先实现以下生命周期表；Registry、Artifact、Grade、Metric 和 Worker 表留给后续里程碑。完整目标表清单如下：

```text
skills
skill_versions
skill_version_files
suites
suite_versions
cases
strategies
strategy_versions
experiments
experiment_arms
trial_groups
trials
trial_attempts
run_events
grades
metrics_snapshots
release_decisions
artifacts
workers
outbox_events（第一个纵向切片完成后再决定是否加入）
```

每张表都应包含：

- UUID 或可排序 ID；
- UTC 的 `created_at`、`updated_at`；
- 适用时使用明确的 Status；
- 影响 Identity 的 Version/Hash 字段；
- 有访问依据的 Index；
- 用于生命周期约束的 Foreign Key。

## 4. M2 关键 Index

M2 Build 候选 Migration 实现以下等价 Index；性能结论仍需 Native Verify 使用代表性数据量检查：

```sql
CREATE INDEX trial_attempts_claim_idx
  ON trial_attempts (priority DESC, not_before, created_at, trial_id)
  WHERE status = 'PENDING';

CREATE INDEX trial_attempts_expiry_idx
  ON trial_attempts (lease_expires_at, trial_id)
  WHERE status IN ('LEASED', 'RUNNING');

CREATE UNIQUE INDEX logical_trial_identity_uq
  ON logical_trials (experiment_id, pair_id, arm);

CREATE UNIQUE INDEX result_logical_uq
  ON trial_results (logical_trial_id);

CREATE UNIQUE INDEX result_idempotency_uq
  ON trial_results (idempotency_key);
```

在声称性能指标前，必须用代表性数据量执行 `EXPLAIN (ANALYZE, BUFFERS)` 验证。

## 5. JSON 与关系型字段的边界

生命周期查询或唯一性约束会使用的字段，应使用关系型列。带版本、由本系统拥有 Schema、整体保存和返回的 Payload，可以使用 JSONB，例如：

- Model Configuration Snapshot；
- Policy Context Snapshot；
- Grader Evidence Detail；
- Resource Usage；
- Decision Explanation。

不能把无界的 Raw Transcript 放进 PostgreSQL。

## 6. Artifact Manifest

每个大型 Artifact Reference 至少包含：

```json
{
  "artifact_id": "art_01...",
  "kind": "trajectory",
  "media_type": "application/jsonl",
  "sha256": "sha256:...",
  "size_bytes": 18342,
  "storage_key": "experiments/exp/trials/trial/attempt-1/trajectory.jsonl",
  "producer": "langgraph-worker@0.1.0",
  "created_at": "2026-08-19T00:00:00Z",
  "redaction": "provider-secrets-v1",
  "retention_class": "trial-trace"
}
```

Control Plane 必须校验 Size 和 Hash，并拒绝路径穿越或由用户直接控制的绝对 Storage Key。

## 7. M2 Event/State 边界

MVP 不采用完全 Event-sourced 架构。PostgreSQL State 是权威状态，`trial_transition_events` 是追加式 Audit Record。M2 在同一事务提交 State Transition、Result 和对应 Audit Event；Telemetry Span 不是 Domain Event。M2 使用 `logical_trials`、`trial_attempts` 和 `trial_results` 表表达 Retry 组、Attempt 与最终 Result。

## 8. 数据生命周期

- Registry Version 不可变；
- Experiment 在 Compile/Start 后不可变；
- Trial Event Sequence 只允许追加；
- Result Artifact 不可变；
- Derived Metric 可以重新计算，并通过 `aggregation_version` 做版本区分；
- 必要时，删除请求应留下 Tombstone/Audit Record，不能无痕删除被 Release Decision 使用过的 Evidence。

## 9. 本地数据安全

本地 Demo 只能使用 Synthetic Data。`.env`、Provider Key、原始外部 Prompt 和未脱敏 Model Trace 默认应被 Git 忽略，不能提交。Redaction 是纵深防御措施，不代表可以使用生产 Secret。

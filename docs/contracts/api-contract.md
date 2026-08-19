# API Contract

**状态：** `PROPOSED`，MVP 初期使用 REST/JSON；内部 Worker 使用 gRPC

## 1. API 原则

- API 只操作版本化、可审计的资源；
- 创建 Experiment 使用 Idempotency Key；
- 状态变更遵守生命周期规则；
- 分页接口使用稳定 Cursor；
- 错误返回机器可读 Code 和人类可读 Message；
- 不在 API Response 中默认返回 Secret、完整 Prompt 或未经脱敏的 Model Output。

## 2. 资源前缀

```text
/api/v1/skills
/api/v1/suites
/api/v1/strategies
/api/v1/experiments
/api/v1/trials
/api/v1/decisions
/api/v1/artifacts
```

## 3. 主要 Endpoint

### Skill Registry

```text
POST /api/v1/skills
POST /api/v1/skills/{skill_id}/versions
GET  /api/v1/skills/{skill_id}
GET  /api/v1/skills/{skill_id}/versions/{version_id}
```

注册 Version 时返回：

```json
{
  "skill_id": "skill_01...",
  "version_id": "sv_01...",
  "content_hash": "sha256:...",
  "validation_status": "passed",
  "artifact_uri": "artifact://..."
}
```

### Eval Suite

```text
POST /api/v1/suites
GET  /api/v1/suites/{suite_id}
POST /api/v1/suites/{suite_id}/versions
```

创建后返回 Suite Hash、Case 数量、Population 分布和 Leakage Check 状态。

### Strategy 与 Policy

```text
POST /api/v1/strategies
POST /api/v1/policies
POST /api/v1/strategies/resolve
```

`resolve` 返回 Strategy Decision Trace，但不创建 Experiment。

### Experiment

```text
POST /api/v1/experiments
GET  /api/v1/experiments/{experiment_id}
POST /api/v1/experiments/{experiment_id}/validate
POST /api/v1/experiments/{experiment_id}/compile
POST /api/v1/experiments/{experiment_id}/cancel
GET  /api/v1/experiments/{experiment_id}/metrics
GET  /api/v1/experiments/{experiment_id}/decision
```

创建 Experiment 的请求应带：

```http
Idempotency-Key: sha256:client-generated-key
```

### Trial 与 Evidence

```text
GET /api/v1/experiments/{experiment_id}/trials?cursor=...
GET /api/v1/trials/{trial_id}
GET /api/v1/trials/{trial_id}/events?cursor=...
GET /api/v1/trials/{trial_id}/artifacts
GET /api/v1/artifacts/{artifact_id}/download-url
```

## 4. Experiment 创建响应

```json
{
  "experiment_id": "exp_01...",
  "status": "DRAFT",
  "manifest_hash": "sha256:...",
  "pair_count": 24,
  "trial_count": 72,
  "estimated_budget": {
    "max_attempts": 72,
    "max_cost_usd": 4.2
  },
  "links": {
    "self": "/api/v1/experiments/exp_01...",
    "validate": "/api/v1/experiments/exp_01.../validate"
  }
}
```

## 5. 错误格式

```json
{
  "error": {
    "code": "PAIR_IDENTITY_MISMATCH",
    "message": "Candidate 与 Baseline 的 Environment Hash 不一致。",
    "request_id": "req_01...",
    "details": {
      "field": "environment_image_digest",
      "baseline": "sha256:a...",
      "candidate": "sha256:b..."
    }
  }
}
```

常见 HTTP 状态：

| 状态码 | 使用场景 |
|---|---|
| `400` | 请求格式或字段错误 |
| `401`/`403` | 身份或权限错误（后续加入 Auth 后） |
| `404` | 资源不存在 |
| `409` | 状态冲突、重复但内容不一致 |
| `422` | Manifest/Policy 语义校验失败 |
| `429` | API 或 Budget 限流 |
| `500` | 未预期服务错误 |
| `503` | 依赖暂时不可用 |

## 6. 分页

列表接口使用：

```text
?limit=50&cursor=<opaque-token>
```

Cursor 不向 Client 暴露内部 SQL Offset。排序必须稳定，默认按 `created_at DESC, id DESC`。

## 7. 并发控制

对会修改资源的请求，可使用：

```http
If-Match: "sha256:resource-version"
```

版本不匹配返回 `409 VERSION_CONFLICT`。Experiment 在 Compile 后不允许使用普通 Update API 修改。

## 8. API 不变量

1. API 创建的 Experiment 必须能通过同一个 Manifest Hash 查询。
2. Cancel 是幂等的。
3. 重复的 Idempotency Key 如果 Body Hash 相同，返回原响应；如果不同，返回 `409`。
4. Artifact Download URL 具有短时效，并不改变 Artifact Identity。
5. API 不直接把 Worker 的任意异常字符串当作可执行指令或 Policy Input。

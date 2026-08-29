# Provider Runtime Contract

**状态：** `PROVISIONAL`，等待真实 Provider E2E 验收
**版本：** v0.2
**最后更新：** 2026-08-30（UTC）

## 1. 目的

本契约定义 Python Trial Worker 与 Model Provider Adapter 之间的稳定边界。它不改变 Runner gRPC v1，也不允许 Worker 选择与冻结 `execution.model` 不同的 Provider、Model、Endpoint 或 Credential。

OpenAI 官方 Python SDK 当前推荐通过 Responses API 创建模型响应，并从进程环境读取 `OPENAI_API_KEY`；SkillGate 的内部归一化结果不绑定某个 SDK Response Class。参考：[OpenAI API Quickstart](https://developers.openai.com/api/docs/quickstart) 与 [Create a model response](https://developers.openai.com/api/reference/resources/responses/methods/create)。

## 2. Adapter 接口

```python
class Provider(Protocol):
    def invoke(
        self,
        messages: list[dict],
        tools: list[dict] | None,
        params: dict | None,
    ) -> ProviderResult: ...
```

兼容期保留：

```python
complete(messages, **params) == invoke(messages, tools=None, params=params)
```

`messages` 只允许 `system`、`developer`、`user`、`assistant` 和带非空 `tool_call_id` 的 `tool` Role。未知 Role 必须失败，不能静默改写成 `user`。

## 3. 冻结 Model 配置

Manifest 中允许：

```yaml
model:
  provider: openai      # fixture | mock | recorded | openai | anthropic
  name: provider-model-id
  config:
    temperature: 0
    max_tokens: 4096
    timeout_seconds: 60
    max_retries: 0
```

兼容读取 `model_id`/`params`，但新文档统一使用 `name`/`config`。

约束：

- Model 顶层只允许 `provider`、`name`、`config`；兼容读取 `type`、`model_id`、`model`、`params` 和 `recordings_path`，未知字段拒绝；
- `config`（以及兼容的 `params`）只允许 `temperature`、`max_tokens`、`timeout_seconds`、`max_retries`。`maxTokens`、`timeoutSeconds`、`timeout`、`maxRetries` 仅作为旧输入别名，别名与规范字段同时出现会拒绝；
- `temperature` 必须是非布尔的有限数值，范围 `0..2`；
- `max_tokens` 必须是非布尔整数，范围 `1..1000000`；
- `timeout_seconds` 必须是非布尔有限数值，范围 `(0,600]`；
- `max_retries` 必须是 `0..30` 的整数，默认 `0`，避免 SDK Retry 与 Scheduler Retry 叠加失控；
- 数字字符串、浮点形式的 token/retry、`null`、`NaN`/`Inf` 和越界值均拒绝；
- 所有接受的字段都属于 Execution Identity，并由 `execution_hash` 覆盖。Provider 参数不能以顶层字段绕过 `model.config`。

## 4. Operator-owned Runtime 配置

以下内容禁止出现在 Manifest、Skill、Suite、Prompt 或环境描述文件中：

- API Key、Token、Password 或 Credential；
- `base_url`、`endpoint`、`api_base`；
- 自定义 HTTP Header。

只允许由 Worker 进程环境或未来 Secret Manager 注入：

| Provider | Credential | 可选 Operator Endpoint |
|---|---|---|
| OpenAI | `OPENAI_API_KEY` | `OPENAI_BASE_URL` |
| Anthropic | `ANTHROPIC_API_KEY` | `ANTHROPIC_BASE_URL` |

缺 Credential 必须 Fail Closed。错误、日志、Event、Trace、Artifact 与 Result Manifest 不得出现完整 Credential。

## 5. 归一化结果

```json
{
  "id": "provider-response-id",
  "model": "actual-provider-model",
  "choices": [
    {
      "index": 0,
      "message": {
        "role": "assistant",
        "content": "text",
        "tool_calls": []
      },
      "finish_reason": "stop"
    }
  ],
  "usage": {
    "input_tokens": 10,
    "output_tokens": 5,
    "total_tokens": 15,
    "prompt_tokens": 10,
    "completion_tokens": 5
  }
}
```

规则：

- 优先记录 Provider 返回的实际模型名；
- OpenAI `prompt/completion` 与 Anthropic `input/output` Token 统一映射，并保留兼容别名；
- 多 Content Block 只拼接 Text Block；Tool Call 保持结构化；
- 空 Response、无 Choice 或不可归一化 Response 是永久失败；
- Worker 将归一化 Usage 写入 Runner `ResourceUsage`，供 Report 成本与 Token 差值聚合。

## 6. 失败分类

| Provider 情况 | Adapter Error | Runner Category | Retry |
|---|---|---|---|
| 429、408、5xx、连接中断、临时过载 | `ProviderTransientError` | `TRANSIENT` → `PROVIDER_TRANSIENT` | 由 Scheduler Policy 决定 |
| Provider 调用超时 | `ProviderTimeoutError` | `TIMEOUT` | 由 Scheduler Policy 决定 |
| 400、401、403、404、422、非法参数、内容长度/策略拒绝 | `ProviderPermanentError` | `PERMANENT` | 否 |
| SDK 缺失、Credential 缺失、非法本地配置 | `ProviderUnavailableError` / `ProviderConfigurationError` | `PERMANENT` | 否 |
| Response Contract 不合法 | `ProviderResponseError` | `PERMANENT` | 否 |
| 未分类 Worker Bug | `ProviderError` 或其他异常 | `INTERNAL` | 按 Worker Lost 策略 |

SDK 内部 Retry 默认关闭；Scheduler 是跨 Attempt Retry 的唯一事实来源。

Worker 到 Runner/Scheduler 的固定映射（protobuf 数值保持不变）为：

| Provider/Worker 错误 | Runner `FailureCategory` | Scheduler Category | 默认是否可重试 |
|---|---|---|---|
| `ProviderTransientError` | `TRANSIENT` | `PROVIDER_TRANSIENT` | 是 |
| `ProviderTimeoutError` | `TIMEOUT` | `LEASE_TIMEOUT`（现有可重试 deadline 桶） | 是 |
| `ProviderPermanentError` / `ProviderResponseError` / 配置错误 | `PERMANENT` | `DETERMINISTIC_FAILURE` | 否 |
| 未分类 Worker/Provider Bug | `INTERNAL` | `WORKER_LOST` | 是（按 Worker Lost Policy） |

`SANDBOX_ERROR` 仍映射为 `DEPENDENCY_TRANSIENT`；未知或未指定 enum 按
`PERMANENT` 处理。该映射由 `internal/runner/provider_mapping_test.go` 固定。

## 7. 验收分层

默认 CI：

- Mock/Recorded Deterministic Test；
- Injected Client Contract Test；
- 请求参数、响应/Usage 归一化、错误分类与 Credential Redaction；
- 不访问公网，不要求真实 Key。

显式 Live Smoke：

- 仅在 `LIVE_PROVIDER_SMOKE=1` 且 Operator 提供受控 Key 时运行；
- 使用 1 个 Case、最多 64 output token、最多 30 秒 Provider timeout、最多 `0.05 USD`；环境变量只能进一步降低这些上限；
- 至少保留一个 matched Baseline/Candidate Pair；
- 验收结果不提交真实 Prompt、Response、Key 或生产 Trace 到仓库；
- 只有完整经历 Claim、Provider、Commit、Grading、Report 与 Decision，才允许把真实 Provider E2E 标记为通过。

离线默认命令：

```bash
PYTHONPATH=workers/python:workers/python/gen workers/python/.venv/bin/pytest -q workers/python/tests
```

受控 Adapter Smoke 入口（默认输出 `SKIP`，显式开启但缺 Key 时 Fail
Closed）：

```bash
LIVE_PROVIDER_SMOKE=1 LIVE_PROVIDER=openai LIVE_PROVIDER_MODEL=<exact-model> \
  ./scripts/provider-live-smoke.sh
```

`LIVE_PROVIDER_FULL_CHAIN=1` 目前只执行 preflight marker；仓库没有把现有
48-trial Fixture Compose Demo 改成真实 Provider，也不声称真实
gRPC→Grading→Report→Decision 已通过。完成该闭环需要 Operator 另行授权临时
matched Manifest、Credential、Egress 和成本预算。

## 8. Sandbox 边界

当前默认离线 Trial Sandbox 保持网络隔离。Provider Adapter 可用不等于生产 Sandbox Egress 已批准。真实 Provider 的生产接入还需要单独完成 Endpoint Allowlist、Secret 注入、Egress Policy 和审计；不得为了冒烟测试把默认 Sandbox 改成任意外网可达。Trace、Event、Result Manifest 和错误日志只允许保存递归脱敏后的 Provider 文本与元数据。

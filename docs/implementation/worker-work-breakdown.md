# Python/LangGraph Worker 工作拆解

**状态：** `FUTURE → M3`；`PROPOSED`，用于 M3–M5

## 1. Worker 目标

Worker 只执行 Go Control Plane 分配的一次 Trial。它不是独立的 Experiment Manager，也不能自行选择未声明的 Skill、Model、Endpoint、Credential 或 Grader。

## 2. 推荐目录

```text
workers/langgraph/
├── app/
│   ├── main.py
│   ├── protocol_client.py
│   └── settings.py
├── graph/
│   ├── state.py
│   ├── nodes.py
│   ├── builder.py
│   └── errors.py
├── agents/
│   ├── base.py
│   ├── provider_openai.py
│   └── provider_fixture.py
├── tools/
│   ├── registry.py
│   ├── policy.py
│   └── wrappers.py
├── sandbox/
│   ├── docker.py
│   └── evidence.py
├── graders/
│   ├── deterministic.py
│   ├── llm_rubric.py
│   └── schemas.py
├── artifacts/
│   ├── collector.py
│   └── redaction.py
└── tests/
```

## 3. Graph State

Graph State 只保存单 Trial 需要的、可序列化的状态：

```python
class TrialState(TypedDict):
    trial_id: str
    request_hash: str
    phase: str
    prompt_ref: str
    skill_mounts: list[dict]
    messages: list[dict]
    tool_events: list[dict]
    output_refs: list[str]
    grader_results: list[dict]
    usage: dict
    errors: list[dict]
    cancellation_requested: bool
```

不要把完整 Secret、未脱敏大文件或无限增长的 Model Context 长期放在 Checkpoint 中。

## 4. Node 责任

### `validate_request`

- 校验 Protocol Version；
- 重新计算 Request Hash；
- 校验 Skill/Strategy/Environment Reference；
- 校验 Budget 和 Tool Policy；
- 生成 `worker.request_verified` Event。

### `prepare_context`

- 下载或读取固定 Artifact；
- 校验 Fixture Hash；
- 创建临时 Workspace；
- 写入不可修改的 Trial Metadata；
- 不把 Expected Answer 写入 Agent-visible Context。

### `mount_skill_and_fixtures`

- 挂载准确的 Skill Version；
- 记录 `skill.mounted` Event；
- 校验挂载内容 Hash；
- 根据 Evaluation Mode 区分 Forced 与 Autonomous；
- 配置 Tool Allow/Deny。

### `invoke_agent`

- 构造 LangGraph 子图；
- 调用 Model Adapter；
- 记录 Model Call、Tool Call、Token 和 Latency；
- 响应 Cancellation；
- 对 Provider 错误映射为稳定的 Error Category。

### `collect_outputs`

- 收集最终文本、文件、目录变更和退出码；
- 限制输出大小；
- 对敏感值进行 Redaction；
- 上传 Artifact 并保存 Hash。

### `run_deterministic_graders`

- 在 Agent Process 结束后读取 Expected Outcome；
- 执行固定的 File/Schema/Test/Trace Assertion；
- 不能被 Agent 修改 Grader 或 Answer Key。

### `run_optional_llm_graders`

- 仅在 Policy 允许时运行；
- 隐藏 Arm Label；
- 使用固定 Rubric Version；
- 输出结构化 JSON；
- Judge Failure 标记为 Incomplete。

### `finalize_trace`

- 生成最终 Event Sequence；
- 汇总 Usage 和 Grade；
- 创建 Result Manifest；
- 上传剩余 Artifact；
- 通过 Runner Protocol 提交 Completion。

## 5. LangGraph Persistence 边界

LangGraph Checkpointer 可以支持单个 Graph Run 的中断和恢复；Go PostgreSQL 负责 Experiment/Trial 生命周期。二者不能混为一个 State Store：

```text
Go DB：Trial 是否被领取、是否完成、是否可重试
LangGraph Checkpoint：Graph 在哪个 Node、Message State 是什么
Object Store：完整 Trace、输出文件和大 Artifact
```

Worker 恢复后必须使用相同的 `trial_id`、Request Hash、Skill Hash 和 Environment Digest。

## 6. Model 与 Tool Adapter

Adapter 接口至少需要：

```text
invoke(input, config) -> output + usage + provider_metadata
stream(input, config) -> events
cancel(run_id)
```

Fixture Provider 应支持：

- 固定成功输出；
- 固定失败输出；
- 可控延迟；
- 可控 Token 使用；
- Tool Call 事件；
- 429/5xx/Timeout 故障。

这样核心测试不需要真实 API Key。

## 7. Worker 安全约束

- 只从 Host/Secret Manager 读取 Provider Credential；
- Skill-controlled Config 不能覆盖 Provider URL；
- Tool Registry 必须由 Worker/Control Plane 固定，不能由 Prompt 动态增加；
- Sandbox 默认关闭 Network；
- 读取路径和写入路径必须有根目录约束；
- 所有外部内容进入 Model Context 前标记来源；
- 不把 Grader Prompt、Answer Key 或 Release Policy 传给 Agent。

## 8. Worker 测试

- Graph Node 的输入/输出测试；
- Protocol Fixture 测试；
- Skill Hash 挂载测试；
- Cancellation 测试；
- Provider Error 分类测试；
- Redaction 测试；
- Tool Policy 拒绝测试；
- Sandbox 清理测试；
- 同一 Completion 重试测试。

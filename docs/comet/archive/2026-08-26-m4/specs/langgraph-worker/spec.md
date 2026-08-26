# LangGraph Worker & Docker Sandbox 完整规格

## 1. 架构目标

在 M3 Runner Protocol 之上，补齐「执行什么」与「在哪里执行」两个缺口：Go Control Plane 把 Manifest 中的执行内容投影进 Trial Request 并管理隔离沙箱；Python LangGraph Worker 在沙箱内执行真实 Agent 图，产出可审计的 Trace 与 Artifact。

```text
+------------------------------+                  +----------------------------+
|      Go Control Plane        |                  |   LangGraph Worker (Py)    |
|                              |   ClaimTrial     |                            |
|  - RunnerControl (M3, 冻结)  | <--------------- |  - Register / Claim        |
|  - ExecutionProjector (新)   | ---------------> |  - Verify request_hash     |
|      manifest_hash -> exec   |   payload+exec   |  - Verify execution_hash   |
|  - SandboxManager (新)       |                  |  - Load Skill from CAS     |
|      Docker SDK 生命周期      |                  |  - Build LangGraph Agent   |
|  - Lease / Fence / Idem (M2) |   Heartbeat      |  - Provider: mock/recorded |
|                              | <--------------- |  - Emit events + trace     |
|                              | ---------------> |  - Cancel -> interrupt     |
|                              |  CompleteTrial   |                            |
+------------------------------+                  +----------------------------+
        |                                                      |
        | 只读挂载 CAS                                  写入 Artifact 卷
        v                                                      v
   .skillgate/cas/                                    ./.artifacts/<trial_id>/
```

## 2. Trial Request 执行内容投影

### 2.1 Payload 结构

`internal/runner/trial_request.go` 的 `TrialRequestPayload` 增加 `execution` 段，M3 既有字段位置与语义不变：

```json
{
  "experiment_id": "...",
  "logical_trial_id": "...",
  "trial_id": "...",
  "pair_id": "...",
  "arm": "with_skill",
  "attempt_no": 1,
  "execution": {
    "case_id": "csv-forced-01",
    "case_input": { "prompt": "...", "fixtures": [...] },
    "skill_hash": "sha256:...",
    "model": { "provider": "mock", "model_id": "...", "params": {...} },
    "tool_policy": { "allowed": [...] },
    "environment": { "uri": "...", "descriptor_hash": "sha256:..." }
  },
  "execution_hash": "sha256:..."
}
```

- `arm == "without_skill"` 时 `skill_hash` 为空字符串，其余字段与配对臂相同；
- `execution` 由 `experiments.manifest_hash` 解析 Manifest 后投影，不新增数据库列。

### 2.2 Hash 分层（冻结）

- `TrialRequestHash(experiment_id, logical_trial_id, trial_id, pair_id, arm, attempt_no)` 保持 M3 定义，逐字节不变；
- `ExecutionHash = HashCanonical(execution)`，使用与 `internal/identity` 一致的 canonical 序列化；
- Worker 必须先校验 `request_hash`，再校验 `execution_hash`；任一不匹配即拒绝执行并以稳定错误分类失败。

### 2.3 服务端模块

新增 `internal/execution`（或 `internal/runner/projection.go`）：

- 输入：`manifest_hash`、`pair_id`、`arm`；
- 依赖：`internal/registry` 读取 Manifest 与 Skill 内容寻址；
- 输出：`ExecutionSpec` 与 `execution_hash`；
- 解析失败（Manifest 缺失、Skill Hash 不存在、Case 缺失）返回稳定错误，`ClaimTrial` 以非重试类失败拒绝该 Claim。

## 3. Docker Sandbox

### 3.1 SandboxManager

新增 `internal/sandbox`，基于 `github.com/docker/docker/client`：

| 能力 | 要求 |
| --- | --- |
| 创建 | 每个 `trial_id` 一个容器，命名可追溯 |
| 网络 | 默认 `none` 或受限自定义网络；不可访问未授权外部地址 |
| 资源 | CPU quota 与内存上限可配置，超限由 Docker 强制 |
| 文件系统 | 只读根；CAS 只读挂载；Artifact 目录读写挂载 |
| 用户 | 非 root 运行 |
| 生命周期 | 启动、状态监控、超时终止、取消终止、清理（含异常路径） |

### 3.2 与 Lease 的关系

- 容器超时上限不长于 Trial `deadline_at`；
- 取消（`cancel_requested_at`）时先中断 Worker，再终止容器；
- 容器异常退出映射为 M2 的可重试/不可重试错误分类，由既有 Retry 逻辑处理。

## 4. LangGraph Worker

### 4.1 模块结构

```text
workers/python/langgraph_worker/
  __init__.py
  __main__.py          # CLI 入口
  client.py            # M3 gRPC 客户端封装（复用 gen 桩）
  skill_loader.py      # 按 skill_hash 从挂载 CAS 读取并校验
  providers/           # mock / recorded / anthropic / openai
  graph.py             # LangGraph Agent 图构建
  trace.py             # Trace 记录与序列化
  artifacts.py         # Artifact 写入与 Hash
```

### 4.2 执行流程

1. `RegisterWorker`：声明 capability 与 protocol version；
2. `ClaimTrial`：取得 payload；
3. 校验 `request_hash` 与 `execution_hash`；
4. `skill_hash` 非空时从 CAS 读取 Skill，校验内容 Hash，构造 System Prompt 与工具配置；为空时构造 Baseline 图；
5. 构建 LangGraph Agent 并执行，期间：
   - 按固定间隔 `Heartbeat`，上报 phase 与用量；
   - 关键节点转移通过 `ReportEvent` 上报，sequence 单调递增；
   - 心跳返回取消指令时中断执行并安全退出；
6. 写入 Trace 与 Artifact 到挂载卷，计算 Hash；
7. `CompleteTrial` / `FailTrial` 提交，继承 M3 幂等语义。

### 4.3 Provider 抽象

```python
class Provider(Protocol):
    def invoke(self, messages, tools, params) -> ProviderResult: ...
```

- `mock`：由 `case_input` 与 `skill_hash` 确定性生成回复，无网络、无 Credential；
- `recorded`：回放预录制的响应文件；
- `anthropic` / `openai`：读取环境变量 API Key，仅用于可选手动 Demo，不参与验收。

### 4.4 Trace 结构

```json
{
  "trial_id": "...",
  "arm": "with_skill",
  "skill_hash": "sha256:...",
  "steps": [
    {
      "seq": 1,
      "type": "llm_call",
      "node": "agent",
      "input": {...},
      "output": {...},
      "usage": { "input_tokens": 0, "output_tokens": 0 }
    },
    { "seq": 2, "type": "tool_call", "name": "...", "args": {...}, "result": {...} }
  ],
  "skill_triggered": true
}
```

`skill_triggered` 用于区分 Skill 是否真正进入 Prompt/执行路径，供 M5 Grading 消费；M4 只记录，不判分。

## 5. 编排与配置

- `deploy/docker-compose.yml` 增加 `langgraph-worker` 服务：只读挂载 CAS、读写挂载 Artifact 目录、通过 `environment` 传递 Provider 配置，默认 `SKILLGATE_PROVIDER=mock`；
- Go Server 需访问 Docker daemon（挂载 socket）以管理 Sandbox；
- `.env.example` 列出可选的真实 Provider Key，默认不启用。

## 6. 非目标

Grading、Metric 聚合、Release Gate、S3/MinIO 上传、Kubernetes、多租户与生产 Secret 管理不在本 capability 内；M3 的 RPC 集合与 `TrialRequestHash` 不被修改。

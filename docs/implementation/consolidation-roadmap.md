# SkillGate 收尾与产品化路线图

**状态：** `PROPOSED`，等待逐阶段启动确认
**版本：** v0.1
**最后更新：** 2026-08-30（UTC）

## 1. 目的与边界

M0–M7 已建立可复现的离线纵向闭环。本路线图只覆盖当前仍未完成或未做真实验收的部分，并为每一阶段定义依赖、交付物和可验证的完成条件。

以下事实在对应验收实际完成前必须持续保留：

- Fixture Provider 的 48/48 结果只证明管道、评分与安全边界，不证明真实 LLM 下存在 Skill Utility Lift；
- 本地 Artifact 卷不能描述为 S3/MinIO 已接入；
- 普通日志与 `trace.json` 不能描述为 OpenTelemetry 已接入；
- CSS 响应式声明不能描述为浏览器 E2E 已验收；
- Release Gate 不能描述为执行前 Runtime Strategy Routing。

## 2. 推荐顺序

```text
P0 Provider 契约与受控 Live Smoke ─┐
P1 Secret 最小安全边界             ├─▶ P2 ArtifactStore + MinIO/S3
                                    │              │
                                    │              ├─▶ P4 UI 浏览器 E2E
                                    └─▶ P3 OTel ◀──┤
                                                   └─▶ P5 完整事件流

P6 Runtime Strategy Routing 为独立产品能力，不与 Release Gate 混合。
```

## 3. P0：真实 Provider 接口与受控验收

### 目标

在不要求默认 CI 使用真实 Credential 的前提下，使 OpenAI/Anthropic Adapter 的配置、响应、用量和失败语义可验证，并准备显式启用的 Live Smoke。

### 交付物

- 冻结 Provider Protocol：`invoke(messages, tools, params) -> normalized result`，保留现有 `complete` 兼容入口；
- `model.config` 仅包含可冻结的模型参数，如 `temperature`、`max_tokens`、`timeout_seconds`、`max_retries`；
- Credential、Provider Base URL 和自定义 Header 只能由 Operator Environment 注入，Manifest 中出现时编译失败；
- OpenAI/Anthropic 响应统一为 `choices`、实际模型名、结束原因和标准用量字段；
- 429、5xx、网络/超时映射为 `PROVIDER_TRANSIENT`，鉴权、参数和内容拒绝等不可恢复错误映射为永久失败；
- 本地 Fake Provider/SDK Contract Test，不访问公网；
- `LIVE_PROVIDER_SMOKE=1` 才运行的冒烟入口，默认跳过，并设置 Case、Token、时间和成本上限。

### 完成条件

1. 无 Key 时 Fail Closed，错误和日志不包含 Credential；
2. Fake Contract Test 覆盖 OpenAI、Anthropic 的请求参数、响应和 429/401/5xx/timeout 分类；
3. Worker 将暂时性 Provider 错误提交为可重试类别，将永久错误提交为不可重试类别；
4. 受控 Credential 可用后，至少一个 matched Baseline/Candidate Pair 经 gRPC、Worker、Grading、Report 和 Decision 全链路完成；
5. DB、日志、Trace 和 Artifact 中均找不到完整 Provider Key。

### 2026-08-30 实现记录

- Go Manifest Compiler 与 Python Worker 已采用相同的 Model allowlist、类型和边界校验；兼容 `model_id`/`params` 及旧参数拼写，但不接受未知字段、顶层 Provider 参数或 Operator Runtime 字段。
- Injected OpenAI/Anthropic Contract Test 覆盖请求转发、文本/多 Content Block、Tool Call、Usage、实际 Model、429/401/5xx/timeout、Malformed Response、错误分类和脱敏；默认不访问公网。
- `./scripts/provider-live-smoke.sh` 是显式 Smoke 入口：默认 `SKIP`，设置 `LIVE_PROVIDER_SMOKE=1` 但缺 Credential 时 Fail Closed，并限制 1 Case、64 output token、30 秒和 `0.05 USD`。`LIVE_PROVIDER_FULL_CHAIN=1` 目前只执行 preflight marker。
- 当前没有受控 Credential，因此未运行真实 Provider API，也未把真实 gRPC→Grading→Report→Decision 标记为通过；默认 48-trial Compose 仍使用 Fixture Provider。

### 暂不包含

- 不为 Live Provider 放开当前每 Trial Sandbox 的默认全网访问；
- 不在 Manifest 中保存 Key、Base URL 或任意 Header；
- 不把一次 API 成功调用等同于 Skill Utility 已验证。

## 4. P1：License 与 Secret 最小安全边界

### 交付物

- 由项目所有者确认许可证后添加 SPDX `LICENSE`，并补充第三方依赖与贡献规则；
- Compose 演示凭据改为显式开发变量或 Docker Secret，生产配置禁止默认密码；
- Provider Key 轮换、日志脱敏和事故处理 Runbook；
- Secret Scan、依赖许可证检查和“无真实用户数据/生产 Trace”检查；个人项目暂不接入 CI，先提供可手动执行的脚本入口。

### 完成条件

- Git、镜像层、命令行、日志、数据库和 Artifact 均不保存明文 Key；
- 缺失或错误 Secret 时返回类型化错误；
- Secret 轮换后 Worker 可受控恢复；
- Local Insecure Profile 与 Production Profile 有明确边界。

### 2026-08-30 实现记录

- 已实现 Go `internal/secrets` 与 Python `secret_source.py` 的 `NAME_FILE` → `NAME` 统一读取边界；缺失、空值、不可读和非法名称返回稳定类型化错误。Provider Key 与数据库 URL 均可从 Docker Secret 文件读取，执行身份和持久化证据不接收 Secret 值。
- 默认 `deploy/docker-compose.yml` 明确为 Local Insecure Demo，仅使用合成 `deploy/secrets/demo-*` 文件和 Fixture Provider；`deploy/docker-compose.production.yml` 通过 `--profile production` 要求外部 `SKILLGATE_POSTGRES_PASSWORD_FILE` 与 `SKILLGATE_DATABASE_URL_FILE`，服务命令不含密码 DSN。
- `scripts/verify-secrets.sh` 提供合成 Sentinel 的 Git/CLI/本地产物/Docker build-context/轮换检查；Docker daemon 不可用时只报告静态检查。依赖许可证检查见 `scripts/check-licenses.sh`，目前由本地手动运行。
- 项目所有者已确认 MIT，根目录 `LICENSE` 已添加，版权主体为 `Lin-xun1113`；依赖许可证工具未安装时仍须诚实记录 `SKIP`。

## 5. P2：ArtifactStore 与 MinIO/S3

### 目标

移除 Grader/UI/Worker 对共享本地 Artifact 卷的硬依赖，同时保留 LocalFS Adapter 作为离线开发路径。

### 交付物

- Go `ArtifactStore`：`Put/Get/Stat/Presign/Delete/Tombstone`；
- LocalFS 与 S3-compatible 两个 Adapter；
- 新增 Artifact 元数据表，保存 identity、hash、size、media type、storage key、retention 和 tombstone；
- Worker 使用受控 Storage Key 或预签名上传，完成提交只携带引用、Hash 与大小；
- Grader 通过 Store 读取，UI 返回短时下载 URL；
- Compose 增加可选 MinIO Profile、Bucket 初始化与健康检查。

### 完成条件

1. 移除 Worker/Control Plane/UI 的共享 artifacts 卷后仍可完成一个 Pair；
2. 重复上传与重复提交幂等；
3. Hash/Size 不一致、越权 Key、过期 URL 和 Tombstone 均被拒绝或按契约处理；
4. 服务重启后 Artifact 可读取，删除动作保留审计记录；
5. LocalFS 全量回归与 MinIO Integration Test 均通过。

## 6. P3：OpenTelemetry 主流程

### 前置条件

- 冻结 Correlation 字段：request、experiment、pair、logical trial、attempt、worker、trace/span；
- 冻结不记录 Prompt、完整 Model Output、Credential 和高基数字段的策略。

### 交付物

- Go：HTTP/gRPC、Claim/Lease/Commit、Grading、Report、Release Gate Span 与 Metric；
- Python：Worker、Provider、Tool、Artifact 和 gRPC Client Span；
- W3C Trace Context 或等价 metadata 透传；
- OTLP Exporter 默认关闭，Compose 提供可选 Collector Profile；
- 结构化日志自动携带关联 ID。

### 完成条件

- 一次 Trial 可从入口串联到 Provider、Commit、Grading 和 Decision；
- Queue、Lease、Retry、Provider Latency/Token、Grader 和 Artifact Metric 与 DB 结果一致；
- Collector 不可用不影响业务成功；
- Redaction 和 Attribute Cardinality 测试通过。

## 7. P4：Web UI 浏览器 E2E

### 交付物

- Playwright 浏览器测试与可复现的 Fixture/Compose 启动脚本；
- 1920/1440 Desktop、768 Tablet、375 Mobile 三档验收；
- 列表、详情、Decision、Evidence、Event、Artifact、空状态、404 和依赖不可用场景；
- Template 安全转义/渲染 Unit Test、API Pagination Test；
- 截图与 Trace 作为 CI Artifact 保存。

### 完成条件

- 三档视口无横向溢出、重叠或不可操作控件；
- 页面展示值与 API/DB 一致；
- Artifact 下载和 Event 下钻可用；
- 失败状态可解释，不泄露本地绝对路径和内部错误。

## 8. P5：完整事件流持久化

### 目标

保留 PostgreSQL 生命周期状态为事实来源，并补齐 Worker 详细执行事件，而不是只保存 graph start/finish 和 `trace.json`。

### 交付物

- `ExecutionContext` 的 Skill/Step/Model/Tool/Artifact/Error 事件按单调 Sequence 上报；
- 每类事件有 schema version、producer version、correlation、payload size limit 和脱敏规则；
- Go-managed Sandbox 的 stdout/stderr 以有界、脱敏的诊断 Artifact 或 Event 保存，不再返回固定占位日志；
- Event API 支持稳定 Cursor 分页；
- `final_sequence` 与服务端最高持久化 Sequence 一致，缺口标记 Evidence Incomplete；
- Report/UI 支持 Metric → Case → Attempt → Event → Artifact 下钻。

### 完成条件

- 覆盖断线重传、重复、Hash 冲突、Sequence Gap、Lease 过期和 Worker Crash；
- 服务重启后 Event Sequence 可完整查询；
- Event Payload 只保存 bounded metadata、usage、hash/ref，不保存 Key 或无限长 Prompt/Response。

## 9. P6：Runtime Strategy Routing（独立 M8）

### 边界

Runtime Routing 是执行前选择 Strategy；Release Gate 是执行后决定是否晋级。二者可复用 CEL 技术，但必须使用不同 Schema、变量 Allowlist 和审计记录。

### 交付物

- 版本化 RuntimePolicy/Strategy Contract；
- `POST /api/v1/strategies/resolve`；
- 类型化 `task/request/runtime` Context；
- Priority、Default、Tie Rejection、Unknown Field 和 Fail Closed；
- 输出 `selected_strategy`、policy/version/hash、context hash、matched/evaluated rules 和 explanation；
- Route Decision 在 Materialize/Claim 前冻结，不能修改已编译 Pair 的 Treatment Identity。

### 完成条件

- High Risk、Low Latency、Budget/Capability Constraint 和 Default 场景均有确定性测试；
- 相同输入与版本产生相同 Hash 和 Decision；
- Priority 冲突、未知字段、非法函数和 Secret 被静态拒绝；
- Integration Test 证明 Routing 不污染 Baseline/Candidate 配对身份。

## 10. 阶段启动规则

每个阶段开始前创建独立 Change/Brief，并确认：

1. Scope 与 Non-goals；
2. Contract/ADR 变更；
3. 数据迁移与向后兼容方式；
4. 失败、幂等、安全和可观测字段；
5. 可执行的 Acceptance Command；
6. `PROJECT_STATUS.md` 的诚实状态更新。

建议下一次优先启动 P0；P0 完成离线 Contract Test 后，再由用户提供受控 Credential 或明确授权 Live Smoke。P2–P6 不应在同一个 Change 中同时展开。

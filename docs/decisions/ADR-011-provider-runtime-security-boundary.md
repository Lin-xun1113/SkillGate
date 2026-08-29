# ADR-011：Provider Runtime 的 Endpoint、Credential 与 Retry 边界

**状态：** Accepted（2026-08-30 UTC）
**适用范围：** OpenAI/Anthropic Trial Provider Adapter
**版本：** v2

## 背景

M4 把真实 Provider 定义为可选手动能力，但没有冻结请求接口、响应归一化和错误分类。若允许 Manifest 指定 Base URL 或 Header，攻击性 Skill/Prompt 可以诱导 Worker 把 Operator Credential 发往未受信 Endpoint；若 SDK 与 Scheduler 同时隐式重试，也会破坏成本、超时和 Attempt 证据的可解释性。

## 决策

1. Worker Provider 使用 `invoke(messages, tools, params)` 作为统一接口，`complete` 仅保留兼容。
2. Provider、Model 与可复现参数属于 `execution_hash`；Credential、Base URL 和 Header 不属于 Experiment 内容。
3. Provider Endpoint 只能来自受信 Operator 环境或未来 Secret/Configuration Manager。Manifest Compiler 和 Worker Adapter 双重拒绝 Endpoint/Credential 字段。
4. Credential 默认只从 Worker 进程环境读取；投影的 Environment Descriptor 不能作为 Secret Source。
5. SDK Response 统一归一化为文本/Tool Call、实际模型和 Token Usage，再进入 Trace 与 Runner `ResourceUsage`。
6. 429、5xx、连接/过载是暂时失败；鉴权、参数、内容限制和非法响应是永久失败；Provider Timeout 单独分类。
7. SDK Retry 默认关闭；跨 Attempt Retry 只由 PostgreSQL Scheduler Policy 决定。
8. 所有 Provider 异常在记录或提交前必须移除已知 Credential。
9. 默认 CI 只运行离线 Contract Test；真实 Provider 验收必须显式启用并使用硬预算。
10. `model.config` 采用四字段 allowlist（`temperature`、`max_tokens`、`timeout_seconds`、`max_retries`），只接受声明的兼容别名；类型、范围和重复别名在 Go Compiler 与 Python Worker 双重校验。
11. 不扩展现有 protobuf `FailureCategory` 数值：Provider transient/timeout/permanent/internal 分别映射到 `TRANSIENT`、`TIMEOUT`、`PERMANENT`、`INTERNAL`，再由 Runner 固定映射到 Scheduler 类别。
12. `LIVE_PROVIDER_SMOKE=1` 是唯一真实 Adapter Smoke 开关；缺 Credential 直接 Fail Closed。Full-chain 入口只在另行授权临时 matched Manifest、Egress 和预算后启用，默认 Compose Fixture 不变。

## 后果

- 优点：阻止 Credential 被重定向，Retry/Usage 可解释，OpenAI/Anthropic 差异不会泄漏到上层 Worker；
- 代价：自定义 Gateway 需要 Operator 配置，不能在 Experiment Manifest 中随任务声明；
- 保留工作：生产 Sandbox Egress、Secret Manager、mTLS/Auth 与真实 Provider E2E 仍是后续阶段，不能因 Adapter Contract 通过而宣称完成；当前 full-chain preflight 尚未产生真实 Provider 证据。

# ADR-010：M3 Runner Protocol 的租约校验与证据持久化

**状态：** Accepted（2026-08-25 UTC）  
**适用范围：** M3 `m3-runner-protocol`  版本：v1

## 背景

M3 建立 Go Control Plane 与 Python Fixture Worker 之间的 gRPC Runner Protocol。早期候选只在 Python 侧复算 Request Hash，事件保存在进程内存，且结果消息中的 Artifact、Usage 和 Grades 没有进入 PostgreSQL。这些行为不能支撑至少一次执行、幂等提交和可复现证据链。

## 决策

1. Go Server 在 Claim 时生成并保存 `request_hash`，在 Complete/Fail 时重新校验它。
2. Result Commit 的 `idempotency_key` 由 `trial_id` 与 Result Manifest Hash 内容寻址；不匹配时拒绝提交。
3. Event 必须携带 Worker Session 与 Lease 身份；服务端校验 Lease Owner、Token、Fence、Payload JSON 和 Payload Hash。
4. Event 写入 PostgreSQL `trial_events`，并在锁定 Attempt 的事务中校验 Sequence 单调递增。重复 Event ID 只有在 Trial 与 Payload Hash 均一致时才幂等忽略。
5. Artifact Manifest 的本地路径仅在声明时校验大小和 SHA-256；M3 不实现对象存储上传。
6. Heartbeat 的资源使用写入 Attempt；协议响应优先返回 PostgreSQL 计算出的 Lease 到期时间。
7. Compose 演示使用一次性 Bootstrap 容器执行 Migration 和 M0 Experiment Materialize，Worker 以有限 Trial 数量运行后退出。

## 不在本 ADR 范围内

真实 LLM Provider、LangGraph Agent、Docker Sandbox、MinIO/S3 上传、Grading 和 Release Gate 仍分别留给 M4–M6。M3 不声称 Exactly-once Execution。

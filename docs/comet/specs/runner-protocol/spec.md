# Runner Protocol & Fixture Worker 完整规格

## 1. 架构目标

在 Go Control Plane 与 Worker（无论是 Python LangGraph Worker 还是测试 Stub）之间建立一套语言无关、高吞吐、可靠的控制与数据通信契约（Runner Protocol）。

```text
+-------------------------+                     +---------------------------+
|    Go Control Plane     |                     |   Python Fixture Worker   |
|                         |    RegisterWorker   |                           |
|  - RunnerService (gRPC) | <------------------ |  - Connect & Register     |
|  - Postgres Scheduler   | ------------------> |  - Session established    |
|  - Lease & Fence        |                     |                           |
|  - Idempotency & Events |      ClaimTrial     |                           |
|                         | <------------------ |  - Poll / Claim           |
|                         | ------------------> |  - Receive Request        |
|                         |                     |  - Verify Request Hash    |
|                         |      Heartbeat      |                           |
|                         | <------------------ |  - Periodic Heartbeat     |
|                         | ------------------> |  - Phase & Usage report   |
|                         |                     |  - Check Cancel Signal    |
|                         |     ReportEvent     |                           |
|                         | <------------------ |  - Stream / Single Events |
|                         | ------------------> |  - Dedup & Sequence check |
|                         |                     |                           |
|                         |    CompleteTrial    |                           |
|                         | <------------------ |  - Outcome & Artifacts    |
|                         | ------------------> |  - Idempotent Commit      |
+-------------------------+                     +---------------------------+
```

## 2. Protobuf 定义 (`proto/runner/v1/runner.proto`)

```protobuf
syntax = "proto3";

package runner.v1;

option go_package = "github.com/Lin-xun1113/SkillGate/gen/go/runner/v1;runnerv1";

service RunnerControl {
  rpc RegisterWorker(RegisterWorkerRequest) returns (RegisterWorkerResponse);
  rpc ClaimTrial(ClaimTrialRequest) returns (ClaimTrialResponse);
  rpc Heartbeat(HeartbeatRequest) returns (HeartbeatResponse);
  rpc ReportEvent(ReportEventRequest) returns (ReportEventResponse);
  rpc CompleteTrial(CompleteTrialRequest) returns (CompleteTrialResponse);
  rpc FailTrial(FailTrialRequest) returns (FailTrialResponse);
}

// 详细 message 定义包含 Worker 信息、Lease Token、Request Payload、Heartbeat 状态、Event 结构与 Result 结构。
```

## 3. Go Control Plane 服务端实现 (`internal/runner`)

- 封装 `internal/store/postgres` 提供的核心原子方法；
- 维护 Worker 注册 Session 缓存（内存/DB），检查心跳超时；
- 将 `ClaimTrial` 请求转发到 Store 的 `ClaimNextPendingTrialAttempt`，并打包 Trial Request JSON 与 SHA-256 哈希；
- 处理 `Heartbeat`：更新 DB 租约与心跳时间，若当前 Experiment 为 `CANCEL_REQUESTED`，返回 `cancel_requested=true`；
- 处理 `ReportEvent`：校验 Sequence 单调递增与 Payload Hash，持久化到 `trial_transition_events` 或事件表；
- 处理 `CompleteTrial` / `FailTrial`：校验 Lease Token 与 Request Hash，调用 `CommitTrialResult` 完成原子幂等落地。

## 4. Python Fixture Worker (`workers/python/fixture_worker`)

- 提供独立 Python 模块与命令行入口；
- 支持模式：
  1. `NORMAL`：正常模拟执行、阶段流转、生成 mock trace 与输出、提交成功；
  2. `FAIL`：模拟执行异常，提交失败结果；
  3. `TIMEOUT_SIM`：故意不发心跳模拟租约超时；
  4. `CANCEL_AWARE`：收到取消信号后立即安全清理并退出；
- 包含编译后的 Python Protobuf / gRPC 桩代码。

## 5. CLI 集成与 Docker Compose

- CLI 增加 `skillgate serve --grpc-addr :50051 --db-dsn ...`；
- 提供 `deploy/docker-compose.yml` 包含 PostgreSQL 容器、Go Control Plane 容器与 Python Fixture Worker 容器。

# Outcome

实现 M3：Runner Protocol 与 Fixture Worker。定义 Go Control Plane 与 Trial Worker 之间的 gRPC/Protobuf 语言无关通信契约，生成 Go 与 Python 桩代码；在 Go Control Plane 中实现 Runner gRPC 服务端，基于 M2 PostgreSQL 存储提供任务领取、心跳续租、事件流上报、取消感知与幂等结果提交；提供可预测的 Python Fixture Worker，在无需真实 LLM 与外部 Credential 的前提下实现端到端协议闭环与 Docker Compose 一键演示。

# Scope

1. **Protocol 定义与生成**：
   - 编写 `proto/runner/v1/runner.proto`，涵盖 `RegisterWorker`, `ClaimTrial`, `Heartbeat`, `ReportEvent`, `CompleteTrial`, `FailTrial` 等 RPC 与消息模型；
   - 生成 Go (`gen/go/runner/v1`) 与 Python (`gen/python/runner/v1` 或 `workers/python/gen/runner/v1`) 桩代码。
2. **Go Control Plane Runner gRPC Server**：
   - 实现 `RunnerService` gRPC 服务，接入 `internal/store/postgres` 底层调度、租约与幂等提交；
   - 支持 Worker 注册、Session 管理与 Capability 校验；
   - 支持任务领取（带 Request Hash、Lease Token 与过期时间）；
   - 支持心跳处理与两阶段取消通知（响应 `CANCEL_REQUESTED`）；
   - 支持事件流（Event Stream）序列号校验与 Payload Hash 去重记录；
   - 支持幂等结果提交（CompleteTrial / FailTrial）与 Artifact Manifest 声明。
3. **CLI 服务端启动命令**：
   - `skillgate serve` 命令，支持配置 gRPC 监听端口、PostgreSQL 连接串等。
4. **Python Stub/Fixture Worker**：
   - 实现 Python 版 Fixture Worker，能够根据配置或 Request 模拟执行、发送心跳与事件流、生成 Artifact 并提交结果；
   - 支持模拟各种执行行为（NORMAL 成功、FAIL 失败、TIMEOUT_SIM 故意不发心跳触发租约超时、CANCEL_AWARE 收到取消信号后安全退出）。
5. **集成测试与 Docker Compose 演示**：
   - Go 内部 gRPC 服务端与客户端的端到端生命周期与故障测试（并发、超时、重试、取消）；
   - Go Control Plane 与 Python Stub Worker 的进程级/容器级协议闭环集成；
   - 提供 `deploy/docker-compose.yml` 一键编排 PostgreSQL、Go Server 与 Python Stub Worker。

# Non-goals

- 不在 M3 中引入真实 LLM 调用或外部 Provider API（留待 M4）；
- 不在 M3 中实现完整的 Docker Sandbox 隔离沙箱与复杂 Grader 执行（留待 M4/M5）；
- 不在 M3 中引入 Kafka/NATS 等外部消息队列（保持 PostgreSQL 为单一队列来源）；
- 不在 M3 中实现 Web 前端 UI（留待 M7）；
- 不在 M3 中实现 MinIO/S3 远程上传链路（M3 阶段 Artifact Manifest 采用本地路径/哈希校验，架构上保留 S3 扩展兼容）。

# Acceptance examples

- A1：协议定义与代码生成：编写 `runner/v1/runner.proto`，成功编译生成 Go 与 Python 的 Protobuf/gRPC 桩代码；协议覆盖注册、领取、心跳、事件、完成与失败。
- A2：Worker 注册与会话管理：Go gRPC Server 支持 Worker 注册，校验 capability 与 protocol version，生成有效 Worker Session；拒绝不兼容版本。
- A3：任务领取与 Request Hash 校验：Worker 通过 gRPC 领取任务，返回包含 Request Hash、Lease Token 与到期时间的 Payload；Worker 校验 Request Hash，不匹配时拒绝执行。
- A4：心跳续租与取消通知：Worker 定期发送心跳上报 phase 与 resource usage；当实验进入 `CANCEL_REQUESTED` 时，心跳响应下发取消指令，Worker 优雅终止并上报取消。
- A5：事件流上报与去重：Worker 上报结构化事件流（Sequence 单调递增），服务端校验 Sequence 并按 Payload Hash 去重，保证事件审计完整性。
- A6：幂等结果与失败提交：Worker 提交 Trial 结果或失败原因（含 Artifact Manifest 与 Usage），服务端完成校验与原子入库；重复提交保持幂等且计数不重复。
- A7：Python Fixture Worker 协议闭环：Python Fixture Worker 在无真实 LLM 与外部 Credential 的情况下，连接 Go Server 完成 M0 样例实验任务的领取、心跳、事件与提交全流程。
- A8：CLI 服务命令与 Docker Compose 编排：提供 `skillgate serve` 服务启动命令，并提供可一键拉起 PostgreSQL、Go Server 与 Python Worker 运行实验的 Docker Compose 编排配置。

# Constraints and invariants

- 协议版本与向前兼容：`protocol_version` 遵循主次版本语义，主要版本不兼容时拒绝服务；
- 租约与幂等性继承：严格继承 M2 建立的 Lease Token、严格过期 Fence 与 `trial_results` 幂等唯一约束；
- 无外部 Secret 依赖：所有 M3 测试与演示均在本地离线或 Docker Compose 中自包含运行，无需真实 API Key；
- 真实 PostgreSQL 兼容：所有数据库交互继续复用已有的 Goose Migration 与 pgx 连接池。

# Decisions

- D1：通信协议采用标准 gRPC (Protobuf v3)，兼具高性能、多语言强类型约束与双向流/心跳支持。
- D2：M3 阶段 Python Fixture Worker 采用轻量独立脚本/模块，通过依赖最小化的 `grpcio` 与 `protobuf` 运行。
- D3：CLI 新增 `skillgate serve` 子命令，统一管理 Control Plane 的 gRPC 服务生命周期与优雅关闭。

# Open questions

（无未决阻塞问题）

# Verification expectations

- Go 模块正常构建，`go test ./...` 与 `go vet ./...` 全部通过；
- `proto/runner/v1/runner.proto` 生成代码在 Go 和 Python 侧语法与测试均通过；
- gRPC 服务的单元测试与真实 PostgreSQL 集成测试通过；
- Python Fixture Worker 能够端到端连接 Go Control Plane 成功完成任务；
- M0 回归验证 `npm run validate:m0` 保持通过。

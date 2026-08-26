# Outcome

实现 M4：LangGraph Worker 与 Docker Sandbox。在 M3 Runner Protocol 基础上，让 Go Control Plane 把「要执行什么」（Skill、Case 输入、模型与工具策略）投影进 Trial Request；实现真实的 LangGraph Agent Worker，在 Go 管理的 Docker Sandbox 中隔离执行；记录完整 Trace 与 Artifact 并按 M3 幂等语义提交，形成不依赖真实 LLM Credential 的端到端可复现闭环。

# Scope

1. **Trial Request 执行内容投影（Go）**：
   - Go 服务端按 `experiments.manifest_hash` 从 Registry/CAS 解析 Manifest；
   - 在 `ClaimTrial` 返回的 payload 中新增 execution 段：`skill_hash`（`without_skill` 臂为空）、`case_id`、`case_input`、`model`、`tool_policy`、`environment`；
   - 计算并返回独立的 `execution_hash`；`TrialRequestHash` 维持 M3 冻结定义不变。

2. **Docker Sandbox（Go）**：
   - Go Control Plane 使用 Docker SDK 为每个 Trial 创建独立容器；
   - 网络隔离、CPU/内存限制、只读根文件系统与非 root 用户；
   - 通过挂载卷提供 Skill（只读 CAS）与 Artifact 输出目录；
   - 容器生命周期管理：创建、监控、超时终止、取消终止与清理。

3. **LangGraph Worker（Python）**：
   - 新建 `workers/python/langgraph_worker/`，与 `fixture_worker` 并列，复用 M3 生成的 gRPC 桩；
   - 校验 `request_hash` 与 `execution_hash`；
   - 从挂载的 CAS 按 `skill_hash` 读取 Skill 并校验内容 Hash，转换为 System Prompt 与工具配置；
   - 以 LangGraph 构建 Agent 图，支持工具调用、状态管理与取消中断；
   - 心跳、事件流上报、Artifact 生成与幂等结果提交。

4. **Provider 抽象与离线执行**：
   - Provider 接口支持 `mock`、`recorded`、`anthropic`、`openai`；
   - 验收路径只使用 `mock`/`recorded`，无需 Credential；
   - 真实 Provider 通过环境变量 API Key 启用，作为可选手动 Demo。

5. **Trace 与 Artifact**：
   - Trace JSON 记录每次 LLM 调用的输入/输出、工具调用、图节点转移与用量；
   - Artifact 写入挂载卷，Worker 计算 Hash 后随结果提交。

6. **编排与测试**：
   - `deploy/docker-compose.yml` 扩展 LangGraph Worker 服务；
   - Go 单元/集成测试与 Python 测试；
   - 使用 M0 `csv-analysis` 样例完成端到端配对执行。

# Non-goals

- 不实现 Grading（Deterministic 或 LLM Judge）、Metric 聚合与 Release Gate（留待 M5/M6）；
- 不实现 MinIO/S3 远程 Artifact 上传（M4 使用挂载卷，架构保留扩展兼容）；
- 不修改 M3 已冻结的 `TrialRequestHash` 定义与 gRPC RPC 集合；
- 不把真实 LLM Provider 调用纳入验收（仅作可选手动 Demo）；
- 不实现 Kubernetes 部署、多租户隔离与生产级 Secret 管理；
- 不实现完整 Security Probe 攻击面评估（留待 M5/M6）。

# Acceptance examples

- A1：Go 服务端在 `ClaimTrial` 返回的 payload 中包含 execution 段（`skill_hash`、`case_id`、`case_input`、`model`、`tool_policy`、`environment`）与 `execution_hash`；`without_skill` 臂的 `skill_hash` 为空，`with_skill` 臂与 Manifest 中的 Skill Hash 一致。
- A2：`TrialRequestHash` 对相同调度身份的输出与 M3 归档值逐字节一致；新增 execution 段不改变该 Hash；篡改 execution 段只使 `execution_hash` 校验失败。
- A3：Go Control Plane 为每个 Trial 创建独立 Docker 容器，应用网络隔离、CPU/内存限制、只读根文件系统与非 root 用户；Trial 终止（成功、失败、超时、取消）后容器被清理，不留残留。
- A4：LangGraph Worker 在 `mock` Provider 下完成一次 Trial 的注册、领取、双 Hash 校验、心跳、事件上报与结果提交，全程无需任何 API Key。
- A5：Worker 按 `skill_hash` 从只读挂载的 CAS 读取 Skill，校验内容 Hash 与声明一致后转换为 System Prompt；Hash 不匹配时拒绝执行并以稳定错误分类失败。
- A6：Worker 心跳期间收到取消指令时，中断 LangGraph 执行、终止 Sandbox 容器并上报取消状态，不产生重复或冲突的结果提交。
- A7：Trace JSON 记录每次 LLM 调用的输入/输出、工具调用、图节点转移与用量；Artifact 经挂载卷落到宿主机，Worker 计算 Hash 后随结果提交，重复提交保持 M3 幂等语义。
- A8：`docker compose` 一键拉起 PostgreSQL、Go Server 与 LangGraph Worker，在 `mock` Provider 下完成 M0 `csv-analysis` 一个 Case 的 `without_skill` 与 `with_skill` 配对执行，两臂均落库且 Trace 可区分 Skill 是否生效。

# Constraints and invariants

- M3 Runner Protocol 的 RPC 集合与 `TrialRequestHash` 定义冻结不变；执行内容完整性由独立 `execution_hash` 承担；
- Sandbox 隔离是硬门禁：每个 Trial 必须在独立容器中执行，不共享宿主机进程或可写文件系统；
- Skill Hash 一致性：加载内容必须与 Manifest 声明的 Content Hash 匹配，不匹配即失败；
- Trace 完整性：所有 LLM 调用与工具使用必须记录，不得遗漏；
- 验收路径无 Credential：全部验收在 `mock`/`recorded` Provider 下离线自包含运行；
- 结果提交继承 M2/M3 的 Lease Token、严格过期 Fence 与幂等唯一约束。

# Decisions

- **D1：LangGraph Worker 实现方式**：新建 `workers/python/langgraph_worker/`，与 `fixture_worker` 并列。保持 Fixture Worker 简单且不依赖 LangGraph 与 Provider SDK，M3 离线测试不受影响。

- **D2：Skill 加载方式**：Worker 通过只读挂载卷直接读取 CAS Store 中的 Skill 文件，按 `skill_hash` 定位并校验内容 Hash。M4 不新增 gRPC 接口，避免改动 M3 已验收协议；未来可优化为 API 获取。

- **D3：Docker Sandbox 实现方式**：Go Control Plane 使用 Docker SDK 统一创建与管理容器生命周期，Worker 在容器内运行。便于资源限制、监控、超时与取消时的确定性清理。

- **D4：LLM Provider 配置方式**：通过环境变量传递 API Key（`ANTHROPIC_API_KEY`、`OPENAI_API_KEY` 等），符合容器最佳实践；未来可引入 Secret Manager。

- **D5：Artifact 存储位置**：Sandbox 容器内生成 Artifact，通过挂载卷同步到宿主机，Worker 从挂载卷读取、计算 Hash 后提交。M4 不实现 S3 上传，架构保留扩展兼容。

- **D6：M4 范围不包含 Grading**：只记录 Trace 与 Artifact，不实现 Grading 逻辑。Grading 涉及 Metric 聚合、统计显著性与 Policy 决策，留待 M5。

- **D7：执行内容由服务端投影**：Go 服务端按 `manifest_hash` 解析 Manifest，把 `skill_hash`、`case_id`、`case_input`、`model`、`tool_policy`、`environment` 投影进 Trial Request payload。Worker 保持薄客户端，不在 Python 侧重实现编译与投影规则，避免双端漂移。

- **D8：Request Hash 分层**：`TrialRequestHash` 保持 M3 冻结定义（experiment/logical_trial/trial/pair/arm/attempt）不变；执行内容由新增的独立 `execution_hash` 覆盖，Worker 分别校验。M3 已归档语义、测试与已落库数据不受影响。

- **D9：验收不依赖真实 Provider**：全部验收在 `mock`/`recorded` Provider 下完成，与 M0/M3 的离线自包含原则一致，保证验收可重复且可在 CI 运行；真实 Provider 仅作可选手动 Demo，不进入验收项。

# Open questions

（已全部解决）

# Verification expectations

- `go test ./...`、`go vet ./...` 通过；含 Docker Sandbox 的 Go 集成测试通过；
- Python LangGraph Worker 单元测试与协议集成测试通过；
- M3 回归：`TrialRequestHash` 与既有 M3 测试全部保持通过，归档 Hash 值不变；
- `docker compose` 一键完成 M0 `csv-analysis` 一个 Case 的配对执行（`mock` Provider，无 API Key）；
- Sandbox 隔离验证：容器无法访问未授权外部网络、无法写入宿主机敏感路径、资源限制生效；
- Trace 完整性：Trace JSON 覆盖全部 LLM 调用与工具使用；
- 文档更新：README、架构说明与 Docker Compose 使用方式。

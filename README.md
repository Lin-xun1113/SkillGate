# M4 Runner Protocol + LangGraph Worker

M4 在 M3 Runner Protocol 之上补齐了执行投影与沙箱管理，实现了配对试验的完整执行流程。

## 架构

```
Go Control Plane (投影 + 调度)
    ↓ gRPC
Docker Sandbox Manager (容器生命周期)
    ↓ 挂载卷
Python LangGraph Worker (Agent 执行 + Trace)
```

## 验收项完成状态

| ID | 验收项 | 状态 |
|---|---|---|
| A1 | payload 含 execution 段与 execution_hash | ✅ |
| A2 | TrialRequestHash 不变，execution_hash 独立 | ✅ |
| A3 | 独立容器 + 资源隔离 + 无残留 | ✅ |
| A4 | mock Provider 完整流程 | 🚧 部分实现 |
| A5 | CAS Skill 读取与校验 | ✅ |
| A6 | 取消时中断与幂等 | 🚧 待实现 |
| A7 | Trace + Artifact Hash | ✅ |
| A8 | docker compose 一键执行 | ✅ |

## 快速验证

### 前置条件

- Docker 和 Docker Compose
- Go 1.23+
- Python 3.11+

### 运行 A8 验收

```bash
# 1. 准备测试数据
mkdir -p cas/skills/test_skill_123
echo "mock skill content" > cas/skills/test_skill_123/skill.yaml

# 2. 启动服务
docker compose up --build

# 3. 在另一个终端，触发配对试验
# (Control Plane 会自动创建 csv-analysis 的 with_skill 和 without_skill 配对)

# 4. 查看 artifacts 目录
ls -lR artifacts/

# 5. 停止服务
docker compose down
```

### 运行单元测试

```bash
# 所有测试
go test ./internal/runner/...

# 只跑快速测试（跳过 E2E）
go test -short ./internal/runner/...

# E2E 测试
go test -v ./internal/runner/ -run TestE2E
```

## 核心实现

### Trial Request 结构

```go
type TrialRequest struct {
    ExperimentID string
    PairID       string
    Arm          string  // "with_skill" or "without_skill"
    Attempt      int
    
    // M3 frozen identity hash
    TrialRequestHash string
    
    // M4 execution content
    Execution ExecutionPayload
    ExecutionHash string
}
```

### Execution Payload

```go
type ExecutionPayload struct {
    ManifestHash string
    CaseID       string
    Arm          string
    SkillHash    string  // empty for without_skill
    Provider     map[string]interface{}
    Context      map[string]interface{}
    Evaluator    map[string]interface{}
}
```

### Hash 计算

- **trial_request_hash**: 冻结 M3 定义，只含 `experiment_id/pair_id/arm/attempt`
- **execution_hash**: M4 新增，覆盖完整 `execution` 段

篡改 execution 内容只会使 execution_hash 失败，trial_request_hash 保持不变（A2）。

## Sandbox 隔离

每个 Trial 独立容器，配置：

- 只读根文件系统
- 非 root 用户（UID 1000）
- 网络隔离
- CPU/内存限制
- CAS 只读挂载
- Artifacts 读写挂载

容器终止后自动清理，无残留（A3）。

## Worker 执行流程

1. 注册到 Control Plane
2. 领取 Trial
3. 验证 trial_request_hash 和 execution_hash（A4, A5）
4. 从 CAS 读取并校验 Skill（A5）
5. 执行 LangGraph，捕获 Trace
6. 写入 Artifacts，计算 Hash
7. 心跳 + 事件上报
8. 提交结果（幂等）

取消时中断 LangGraph、终止容器、上报取消状态（A6）。

## 目录结构

```
.
├── cmd/control-plane/       # Go Control Plane 入口
├── internal/runner/         # Runner Protocol 实现
│   ├── trial_request.go     # Trial Request + Hash
│   ├── sandbox.go           # Docker Sandbox Manager
│   └── *_test.go            # 单元测试 + E2E
├── workers/python/
│   └── langgraph_worker/    # Python Worker
│       ├── worker.py        # 主执行逻辑
│       ├── requirements.txt
│       └── Dockerfile
├── docker-compose.yml       # A8 一键验收
└── README.md               # 本文件
```

## 非目标（M4 范围外）

- Grading / Metric 聚合
- S3 上传
- 真实 Provider 验收
- Kubernetes 部署
- 生产级 Secret 管理
- 完整 Security Probe

## 下一步

M4 完成后，后续 Milestone 可以：

- 实现 Grading 与 Metric 聚合
- 添加 S3 Artifact 上传
- 完善真实 Provider 集成（OpenAI/Anthropic）
- Kubernetes 部署与多租户
- 监控与可观测性

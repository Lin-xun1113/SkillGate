# M4: LangGraph Worker & Execution Projection

M4 在 M3 Runner Protocol 之上补齐两个缺口：

1. **Go Control Plane**：把「要执行什么」投影进 Trial Request 并管理 Docker Sandbox
2. **Python LangGraph Worker**：在沙箱内执行真实 Agent 图，产出可审计的 Trace 与 Artifact

## 架构

```
Scheduler → TrialRunner → Projector → Sandbox → LangGraph Worker
                              ↓            ↓
                         ExecutionSpec   Docker
                              ↓            ↓
                      execution_hash   Container
```

### 关键组件

- **Projector** (`internal/runner/projector.go`): 从 Manifest 解析执行内容，投影进 Trial Request
- **TrialRunner** (`internal/runner/trial_runner.go`): 编排完整流程：投影 → 沙箱 → 结果收集
- **Sandbox** (`internal/runner/sandbox.go`): Docker 容器生命周期管理，强制隔离和资源限制
- **LangGraph Worker** (`workers/python/langgraph_worker/`): Python 执行环境，运行 Agent 图并产出 Trace

## 双 Hash 系统

### Request Hash（M3 冻结）
- 只含调度身份：experiment_id, logical_trial_id, pair_id, arm, attempt
- 与 M3 归档值逐字节一致
- 用于 Scheduler 幂等性和去重

### Execution Hash（M4 新增）
- 覆盖执行内容：case_input, skill_hash, model, tool_policy, environment
- 独立于 Request Hash 计算
- 篡改执行内容只使 Execution Hash 失败，不影响 Request Hash

## 验收项（A1–A8）

### A1: Payload 含 execution 段与 execution_hash
```bash
go test ./internal/runner -run TestPayloadContainsExecutionSegment -v
```

验证点：
- payload 包含 `execution` 字段（case_id, case_input, skill_hash, model, tool_policy, environment）
- payload 包含 `execution_hash` 字段
- `without_skill` 臂的 `skill_hash` 为空字符串

### A2: TrialRequestHash 与 M3 一致
```bash
go test ./internal/runner -run TestTrialRequestHashUnchanged -v
```

验证点：
- 相同 Claim 生成的 Request Hash 完全一致
- 篡改 execution 只使 execution_hash 改变
- 调度身份字段（trial_id, experiment_id, pair_id, arm）不受 execution 影响

### A3: 独立容器与资源隔离
```bash
go test ./internal/runner -run TestSandbox -v
```

验证点：
- 每个 Trial 获得独立 Docker 容器
- 网络隔离、资源限制（内存/CPU）、只读根文件系统生效
- 非 root 用户运行
- 容器终止后无残留

### A4: Mock Provider 完整流程
```bash
go test ./test/integration -run TestMockProviderCompleteFlow -v
```

验证点：
- 注册 → 领取 → 双 Hash 校验 → 心跳 → 事件记录 → 提交
- 无需真实 API Key
- 离线完成全流程

### A5: CAS Skill 校验
```bash
go test ./test/integration -run TestSkillHashVerification -v
```

验证点：
- 按 `skill_hash` 从 CAS 只读挂载卷读取 Skill
- Hash 不匹配时稳定失败（`skill_not_found`）
- 不尝试重试或降级

### A6: 取消与清理
```bash
go test ./internal/runner -run TestTrialCancellation -v
```

验证点：
- 取消时中断 LangGraph 执行
- 终止容器
- 上报 `cancelled` 状态
- 无重复提交（幂等性）

### A7: Trace 与 Artifact
验证点（在 A4 集成测试中覆盖）：
- Trace 覆盖全部 LLM 调用和工具调用
- Artifact 落盘并计算 Hash
- 重复提交保持幂等

### A8: 一键演示
```bash
docker compose up --build
```

验证点：
- `docker compose` 启动完整系统
- 自动执行 M0 `csv-analysis` 的一个 Case
- 完成 `with_skill` 和 `without_skill` 双臂配对执行
- 产出可查询的结果和 Trace

## 快速开始

### 1. 构建 Worker 镜像
```bash
cd workers/python
docker build -t skillgate-langgraph-worker:latest .
```

### 2. 运行单元测试
```bash
# 跳过 Docker 集成测试
go test ./internal/runner -short

# 完整测试（需要 Docker）
go test ./internal/runner -v
```

### 3. 运行集成测试
```bash
go test ./test/integration -v
```

### 4. 启动完整系统
```bash
docker compose up --build
```

查看结果：
```bash
docker compose logs demo
docker compose exec postgres psql -U skillgate -d skillgate -c "SELECT * FROM trial_results;"
```

## 目录结构

```
.
├── internal/runner/
│   ├── projector.go           # Manifest → ExecutionSpec 投影
│   ├── trial_request.go       # M3 冻结 Request Hash + M4 Execution 投影
│   ├── sandbox.go             # Docker 容器管理
│   ├── trial_runner.go        # 完整编排
│   └── *_test.go              # 单元测试（A1-A3, A6）
├── workers/python/
│   ├── langgraph_worker/
│   │   ├── __main__.py        # Worker 入口
│   │   ├── models.py          # 数据模型
│   │   ├── executor.py        # LangGraph 执行
│   │   ├── skill_loader.py    # CAS Skill 加载
│   │   └── provider.py        # Mock/Recorded Provider
│   ├── Dockerfile
│   └── requirements.txt
├── test/integration/
│   └── runner_integration_test.go  # 集成测试（A4-A5, A7）
├── docker-compose.yaml        # A8 一键演示
└── README.md                  # 本文件
```

## 非目标

M4 **不包含**：
- Grading 或 Metric 聚合
- Release Gate
- S3 上传（Artifact 仅本地落盘）
- 修改 M3 冻结 Hash 或 RPC
- 真实 Provider 进入验收（仅作可选 Demo）
- Kubernetes 部署
- 多租户
- 生产级 Secret 管理
- 完整 Security Probe

## 依赖

- Go 1.21+
- Docker 20.10+
- Docker Compose V2
- PostgreSQL 15
- Python 3.11
- LangGraph 0.2+

## 下一步（M5+）

- Grading Pipeline
- Metric 聚合与对比
- Release Gate 决策
- S3 Artifact 存储
- Kubernetes 运行时
- 真实 Provider 集成（OpenAI/Anthropic）
- 多租户 Secret 管理

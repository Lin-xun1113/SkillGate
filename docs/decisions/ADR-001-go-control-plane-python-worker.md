# ADR-001：Go Control Plane + Python/LangGraph Trial Worker

**状态：** `ACCEPTED`  
**日期：** 2026-08-19（UTC）  
**影响范围：** 系统架构、Worker Protocol、技术栈

## 背景

目标岗位强调 Go Backend、Strategy/Rule Engine、数据处理和 AI 相关工作。纯 Python/LangGraph 项目会强化 Agent 能力，但不足以展示 Go 后端和可靠的控制流；纯 Go 项目又难以体现现代 Agent 执行生态。

## 决策

Go 负责长期存在的 Control Plane：API、Experiment Compiler、Strategy Engine、Scheduler、Lease、Retry、Result Commit、Metric 和 Release Gate。Python/LangGraph 作为可替换的 Trial Worker，负责单个 Agent Run 的 Graph、Model/Tool Adapter、Grader 和 Trace。

## 备选方案

### 方案 A：全部使用 Python/LangGraph

开发较快，但 Go 岗位匹配度弱，分布式生命周期和后端系统信号不足。

### 方案 B：全部使用 Go

Go 信号强，但 Model/Tool/Agent 生态接入成本高，容易把项目变成普通规则服务。

### 方案 C：Go + Python Worker

存在跨语言协议成本，但边界清晰，能够同时展示目标岗位所需的 Go 系统能力和 Agent 实践。

## 选择理由

方案 C 最符合项目目标。关键不是“使用两个语言”，而是建立合理边界：Go 是事实状态与可靠性所有者，LangGraph 只是单 Trial 的执行实现。

## 后果

正面：

- Go 和 AI Agent 两条能力线同时可验证；
- Worker Protocol 可扩展到其他 Runner；
- 面试时可以分别讨论 Control Plane 与 Agent State Machine。

负面：

- 需要维护 gRPC/Schema 和两套测试；
- 本地启动复杂度略高；
- 必须明确 PostgreSQL 与 LangGraph Checkpoint 的状态边界。

## 验证方式

- Stub Worker 完成 Protocol E2E；
- Worker 崩溃后 Go Scheduler 能恢复 Trial；
- Go 不需要导入 Python 内部类型；
- LangGraph Worker 可被 Fixture Worker 替换。

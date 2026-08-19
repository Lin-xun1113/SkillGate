# SkillGate 项目简介

**状态：** 方向已接受，尚未开始实现  
**读者：** 项目负责人、未来贡献者、面试官

## 一句话描述

SkillGate 是一个 Go-first Control Plane，用于对 AI Agent Skill 和 Strategy 进行可复现的配对评估，聚合任务级证据，并通过可解释的 Policy 决定 Candidate Strategy 是否可以晋级。

## 要解决的问题

Skill 作者和 Agent Platform 工程师通常很难可靠回答以下问题：

1. 与没有 Skill 的同一个 Agent 相比，这个 Skill 是否真的改善了结果？
2. Skill 是否在应该触发时触发，并在相邻请求上保持安静？
3. 这种提升是否足够稳定，值得额外的 Token、延迟和运维成本？
4. 这个 Package 是否安全，是否可以被晋级到下一环境？

只给 `SKILL.md` 一个文档级 LLM 分数无法回答全部问题。SkillGate 提供受控实验，并用原始 Trace 和 Artifact 支撑 Release Decision。

## 主要用户

拥有小型 Skill/Agent Library、需要在部署前验证新版本的开发者或 Platform Engineer。

## 次要用户

希望在统一 Workload 下比较 Model、Skill Bundle、Tool Policy 和预算组合的 Agent Platform Engineer。

## 作品集主旨

项目有意展示以下能力的交叉：

- Go Backend Engineering；
- Rule/Strategy 执行；
- 可靠的异步 Job Processing；
- Data 与 Metric Pipeline；
- Agent State Machine 集成；
- Evaluation Methodology；
- Sandbox 与 Security Engineering。

项目的评价标准不是使用了多少 Framework，而是评审者能否复现实验、检查失败原因、理解 Policy Decision，并确认 Worker 故障不会破坏结果。

## 产品边界

```text
包含：
  Skill/Strategy Registry、Eval Suite、配对 Trial、Scheduling、Worker Protocol、
  Deterministic/LLM Grading、Metrics、Report、Policy Gate、Trace、Sandbox。

MVP 不包含：
  Model Training、通用 Skill Marketplace、自动 Skill Rewrite、
  Live Trading、Multi-region Deployment、Billing 和强制 Kafka 集群。
```

## North-star Demo

1. 注册 `csv-analysis` 的 v1 和 v2。
2. 定义 Explicit、Implicit、Contextual 和 Negative Eval Case。
3. 编译 Baseline/Candidate Experiment。
4. 在隔离 Worker 中运行三次 Repetition。
5. 展示 Candidate Lift 及任务级置信区间。
6. 展示一个 Skill 增加 Token 消耗或造成 Regression 的 Case。
7. 运行 Security Check 和 Trigger Matrix。
8. 评估 CEL Policy，返回可解释的 Promotion Decision。
9. 打开失败 Case 的 Trace 并进行 Replay。

## 作品集层面的成功标准

评审者应该可以对以下问题全部回答“是”：

- 持久化且可靠的平台逻辑是否主要由 Go 实现？
- Worker 崩溃后，Lease 是否能够恢复？
- Baseline 和 Candidate Trial 是否真正匹配？
- 系统能否区分 Routing Failure 和 Execution Failure？
- Objective Output 是否经过验证，而不是盲目信任 LLM Judge？
- Utility 旁边是否同时展示 Cost、Latency、Reliability 和 Security？
- Strategy Decision 是否能由已保存的 Rule 和 Evidence 解释？
- 是否诚实说明了限制和统计不确定性？

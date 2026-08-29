# SkillGate 技术文档

欢迎来到 SkillGate 技术文档。这里回答三个问题：**SkillGate 是什么、为什么这样设计、怎么用**。

## 推荐阅读路径

| 你是谁 | 建议路径 |
|---|---|
| 想快速了解项目 | [产品介绍](overview/product.md) → [核心概念](overview/concepts.md) |
| 想动手跑起来 | [安装与快速开始](getting-started/installation.md) → [编写你的第一个实验](getting-started/first-experiment.md) |
| 想深度使用 | [CLI 参考](usage/cli.md) → [创作指南](authoring/) → [评分与指标](evaluation/) |
| 想集成 Worker | [Runner 协议](protocol/runner-protocol.md) → [Worker 实现指南](protocol/worker-guide.md) |
| 想部署运维 | [部署指南](deployment/local-deployment.md) → [常见问题](misc/faq.md) |
| 想看懂报告 | [指标说明](evaluation/metrics.md) → [报告输出项](reference/report-output.md) |

## 文档目录

### 概述（overview/）

- [产品介绍](overview/product.md) —— SkillGate 解决什么问题，核心方法论，目标用户
- [核心概念](overview/concepts.md) —— Skill、Suite、Experiment、Trial、Arm、Pair 等术语的精确定义
- [架构与原理](overview/architecture.md) —— 组件职责、生命周期、可靠性语义、统计原理

### 快速开始（getting-started/）

- [安装与快速开始](getting-started/installation.md) —— 环境要求、构建、五分钟跑通第一个实验
- [编写你的第一个实验](getting-started/first-experiment.md) —— 从零手写 Skill、Suite、Manifest 并解读结果

### 使用指南（usage/）

- [CLI 参考](usage/cli.md) —— 全部子命令与参数的权威说明
- [常见工作流](usage/workflows.md) —— 注册→编译→物化→执行→查看报告的完整操作序列

### 创作指南（authoring/）

- [编写 Skill Package](authoring/skill-package.md) —— `SKILL.md` 结构、Frontmatter、内容规则
- [编写 Eval Suite](authoring/eval-suite.md) —— Case 类型、评估模式、断言、防泄漏
- [编写 Experiment Manifest](authoring/experiment-manifest.md) —— Strategy、Arm、Pairing、Execution、Grading 段
- [编写 Release Policy](authoring/release-policy.md) —— CEL 规则、优先级、硬门禁、Fail Closed

### 评估与报告（evaluation/）

- [评分体系](evaluation/grading.md) —— Deterministic Verifier、LLM Judge、Grader 注册表
- [指标说明](evaluation/metrics.md) —— Lift、CI、pass@k、Trigger Recall/Specificity、Security Finding 的计算方式
- [评估模式](evaluation/evaluation-modes.md) —— Forced Injection / Autonomous Trigger / Security Probe 三类人群

### 参考手册（reference/）

- [报告输出项参考](reference/report-output.md) —— `report.json` / `report.md` / `report.html` 每个字段的含义
- [报告 Decision 字段](reference/decision-output.md) —— PROMOTE / HOLD / REJECT 的结构与解释方式
- [配置参考](reference/configuration.md) —— 环境变量、CLI 全局参数、默认值

### Worker 协议（protocol/）

- [Runner Protocol](protocol/runner-protocol.md) —— gRPC 契约：六个 RPC、状态码、Hash 校验
- [Worker 实现指南](protocol/worker-guide.md) —— 实现一个 Worker 的完整步骤与陷阱

### 部署（deployment/）

- [本地部署指南](deployment/local-deployment.md) —— Docker Compose 服务拓扑、端口、数据卷、常见运维操作

### 其他（misc/）

- [常见问题](misc/faq.md) —— 高频问题与排错
- [已知限制与诚实声明](misc/limitations.md) —— 系统不做什么，以及为什么

## 术语速查

一句话版本，详见[核心概念](overview/concepts.md)：

- **Skill**：一个 `SKILL.md` 能力包，内容寻址存储（SHA-256）。
- **Eval Suite**：一组带断言的评测任务（Case）。
- **Experiment**：把 Suite 展开成 Baseline/Candidate 配对 Trial 的执行计划。
- **Trial**：某个 Case 在某个 Arm 上的某一次重复执行。
- **Skill Lift**：Candidate 减去 Baseline 的配对分数差。
- **Release Gate**：用 CEL 规则把指标变成 PROMOTE / HOLD / REJECT 决定。

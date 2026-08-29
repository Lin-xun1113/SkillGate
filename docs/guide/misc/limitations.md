# 已知限制与诚实声明

> 状态：与 2026-08-30 代码库同步

这份文档的存在本身就是 SkillGate 的设计立场之一：**评估平台的可信度取决于它对自己边界的诚实**。以下是当前版本明确不做、未做、或做不到的事。

## 语义边界（设计立场，不是缺陷）

### 不声称 Exactly-once 执行

分布式系统里可证明的语义是「至少一次执行 + 幂等提交」。Worker 崩溃、网络重试都可能造成重复执行，但幂等键 + Fence 保证**不重复计分**。文档与代码中不会出现「Exactly-once」的承诺。

### 不声称沙箱是完美安全边界

Docker 沙箱（只读根、非 root、网络隔离、资源限制）是当前启用的 Backend，其限制（如内核攻击面、侧信道）在 `docs/architecture/threat-model.md` 中有记录。运行**新版本 Skill** 时应假设对抗性内容存在，报告会标注所用的沙箱 Backend。

### LLM Judge 的不确定性不做点估计伪装

LLM 评分本质上有方差。系统不把 Judge 分数与确定性分数混在同一名义下呈现，且 Judge 不可用时相应指标标记 Incomplete——不臆造数值。

## 未完成事项（有计划但尚未实现）

| 事项 | 现状 | 影响面 |
|---|---|---|
| **S3/MinIO Artifact Store** | 架构文档已规划；当前产物存本地文件系统/Compose 卷 | 大规模 Artifact 的持久化与生命周期管理 |
| **OpenTelemetry 遥测** | SDK 在依赖中，主流程未接导出 | 生产级可观测性（Trace/Metric 跨 Go/Python） |
| **真实 LLM 端到端验收** | OpenAI/Anthropic Adapter 已实现；已归档验证全部基于 Fixture | 真实 Provider 行为的官方验证结论 |
| **Web UI 的深度验证** | Builder 已声明：无端到端集成测试、模板未单元测试、响应式布局未做浏览器验证 | UI 回归保护与多端体验 |
| **CI 流程** | 个人项目暂不接入 CI；检查脚本需本地手动运行 | 自动化扫描和浏览器验收不会在远程构建中自动执行 |
| **运行时 Strategy Routing** | 当前 Release Gate 是「事后决策」；执行前按 Context 选 Strategy 的运行时路由未实现（见 M6 brief 范围声明） | 策略引擎的另一半能力 |
| **Go Sandbox 诊断日志** | `RunTrial` 当前返回固定完成文本，尚未持久化有界 stdout/stderr | Sandbox 失败诊断与运维排查 |

## 统计与方法论边界

- **Case 数量决定 CI 质量**：Case 太少时 Bootstrap CI 宽到没有决策价值。最小可行 Suite 见[评估模式](../evaluation/evaluation-modes.md)的配比建议；
- **pass@k 依赖 repetitions**：k > repetitions 时无法计算（字段省略）；
- **Fixture Provider 的外部效度有限**：它证明「管道与评分语义正确」，不证明「真实 LLM 下的 Skill 价值」——后者需要真实 Provider 验收；
- **评估只覆盖 Suite 里的任务分布**：Skill 在训练分布外的表现不在结论范围内。

## 工程边界

- **单机 PostgreSQL 队列**：并发规模显著增长前不引入 Kafka/NATS（ADR-002 的边界，届时需重新评估）；
- **无多租户/认证**：MVP 形态假设可信的本地/私有部署环境，UI 无认证（不要暴露公网）；
- **Go 1.26+**：使用了较新的 Go 版本，老环境需注意。

## 文档一致性说明

- 各里程碑的**权威验收结论**在其归档 `docs/comet/archive/*/verification.md`；本文档集（docs/guide/）是面向使用者的转述，冲突时以归档为准；
- 历史文档（如里程碑中途的 README）可能与最终 Verify 结论不一致，以归档为准；
- 本文档集的「状态：与 2026-08-30 代码库同步」表示撰写时点核对过对应源码；后续代码变更请同步维护。

## 如果发现文档与行为不符

按可信度排序的核对路径：

1. `internal/` 对应包的源码与测试（行为的最终裁判）；
2. `docs/contracts/` 契约文档；
3. `docs/comet/archive/` 对应里程碑的 verification；
4. 本 guide 文档集。

欢迎以「源码为准」为原则提 issue/PR 修正文档。

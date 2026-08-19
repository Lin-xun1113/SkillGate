# 竞品定位与差异化

## 简短定位

已有 Skill Evaluation 项目提供了有价值的 Scanner、Benchmark 和 CLI Workflow。SkillGate 聚焦目前较少被单独解决的 Systems Layer：使用 Go 构建持久化、可解释的 Control Plane，将不同 Skill Evaluation 统一转换成可复现的 Strategy Promotion Decision。

## 能力对比

| 能力 | Static/Lint Tool | Fixed Benchmark | Skill-centered Generator | SkillGate 目标 |
|---|---:|---:|---:|---:|
| Schema/Security Preflight | 强 | 有时支持 | 有时支持 | Adapter + Gate |
| 固定 Curated Task 评估 | 弱 | 强 | 可选 | 支持 |
| 任意 Skill Task Generation | 弱 | 弱 | 强 | Reviewed/Advisory |
| Autonomous Trigger Matrix | 少见 | 少见 | 部分支持 | 一等能力 |
| 持久化分布式调度 | 少见 | 依赖具体 Framework | 通常是 CLI/Local | Go Control Plane |
| 精确 Paired Identity Validation | 不一致 | 优秀实现较强 | 不一致 | 强制要求 |
| Worker Crash Recovery | 不一致 | 依赖具体 Framework | 不一致 | MVP 强制要求 |
| Model/Skill/Tool Policy 解释 | 少见 | 不是核心 | 少见 | 核心差异化 |
| Security Hard Gate | 不一致 | 通常分离 | 部分支持 Static + Dynamic | 独立硬门禁 |
| Cost-aware Promotion | 部分支持 | 部分支持 | 部分支持 | Policy 核心输入 |
| Go Backend 作品集信号 | 低 | 低 | 低 | 高 |

## 不应做出的声明

- 不要声称 SkillGate 是第一个 Skill Evaluation System；
- 不要声称存在普适的单一 Quality Score；
- 不要用很小的 Demo Suite 声称统计显著；
- 不要声称 Sandbox 在所有 Backend 下都安全；
- 不要声称实现了 Exactly-once Agent Execution。

## 差异化判断标准

一个 Feature 只有在至少增强以下一项时，才应进入 MVP：

1. Experiment Validity；
2. Go Backend/Reliability 信号；
3. Strategy Engine 可解释性；
4. Safety 与 Auditability；
5. 可以本地复现的具体 Demo。

仅仅因为某个 Framework 流行就加入的技术，应拒绝或延期。

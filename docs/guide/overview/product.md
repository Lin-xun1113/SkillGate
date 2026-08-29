# 产品介绍

> 状态：与 2026-08-27 代码库同步 · 适用版本：M0–M7

## 一句话介绍

SkillGate 是一个 **Go-first 的 Agent Strategy 评估与晋级平台**：它把「这个 Agent Skill（`SKILL.md` 能力包）有没有用」变成一个可复现的受控实验，用配对对比给出带置信区间的答案，并用可解释的规则决定这个 Skill 能否晋级上线。

## 它解决什么问题

如果你在维护一批 Agent Skill，你大概率被问过（或问过自己）这些问题：

1. **真的有用吗？** 加了这个 Skill 的 Agent，和没加的同一个 Agent 相比，任务成功率变高了吗？
2. **触发对了吗？** Skill 应该被触发时触发了（Recall），不该触发时保持安静了吗（Specificity）？
3. **划算吗？** 带来的提升是否稳定到值得付出额外的 Token 成本、延迟和运维负担？
4. **安全吗？** Skill 会不会引导 Agent 执行数据里藏的恶意指令、外传凭据或越权访问？

给 `SKILL.md` 打一个「文档质量分」回答不了任何一条——那是主观印象，不是证据。SkillGate 的思路是：**像做 A/B 实验一样评估 Skill**。

## 核心方法论：配对实验

SkillGate 强制使用配对比较（Paired Comparison）：

```
同一个 Eval Suite（固定的任务集、相同的模型、相同的工具策略）
    ├── without_skill 臂（Baseline）  ── 每个任务重复执行 N 次
    └── with_skill   臂（Candidate）  ── 每个任务重复执行 N 次
                        ↓
        配对差值 = Skill Lift（每个 Case 的候选分 − 基线分）
                        ↓
        Bootstrap 置信区间 + pass@k + 成本/延迟变化
                        ↓
        CEL Release Policy 逐条评估 → PROMOTE / HOLD / REJECT
```

三条方法论铁律贯穿整个系统：

1. **必须有 Baseline。** 只看 Candidate 臂的绝对分数无法推断 Skill 价值——可能是任务太简单或模型本身变强了。SkillGate 在编译期就拒绝没有 `without_skill` 臂的实验。
2. **评估人群严格分离。** 「Skill 被强制提供时的答案质量」（Forced Injection）、「Skill 挂到正常发现路径时的触发行为」（Autonomous Trigger）、「Skill 面对对抗输入时的安全表现」（Security Probe）是三个不同的统计总体，分开度量、分开报告，永不混算。
3. **Deterministic Verifier 优先。** 能用 JSON Schema、文件校验、Trace 断言客观验证的输出，绝不交给 LLM Judge 主观打分。LLM Judge 只是兜底，且必须带版本。

## 一个典型工作流

1. **注册**：`skillgate skill register` 把 Skill 包按 SHA-256 内容寻址存入不可变 Registry；
2. **编译**：`skillgate compile` 把 Experiment Manifest 展开成 Case × Arm × Repetition 的 Trial 计划（例如 8 Case × 2 臂 × 3 次 = 48 Trial），并冻结 Manifest Hash；
3. **执行**：PostgreSQL 队列调度，Worker 通过 gRPC 领取 Trial，在 Docker 沙箱里跑 LangGraph Agent，产出 Trace 与 Artifact；
4. **评分**：Deterministic Grader 先跑（文件存在性、Schema 校验、Trace 断言），可选 LLM Rubric 兜底；
5. **聚合**：计算配对 Lift、Bootstrap 置信区间、pass@k、Token/延迟变化、触发召回率/特异度、安全发现分级；
6. **决策**：CEL Release Policy 逐条规则评估指标快照，任何一步出错都 Fail Closed（宁可 HOLD 不静默放行）；
7. **查看**：Web UI 或 `report.json/md/html` 查看每个 Case 的分数、每条规则的命中情况和证据链接。

## 目标用户

- **主要用户**：拥有小型 Skill/Agent Library、需要在部署前验证新版本的开发者或 Platform Engineer。
- **次要用户**：希望在统一 Workload 下比较 Model、Skill Bundle、Tool Policy、预算组合的 Agent 平台工程师。

## 内置示例

仓库自带一个完整的 CSV 数据分析示例（`skills/csv-analysis` + `evals/csv-analysis` + `experiments/csv-analysis-v1-demo.yaml`）：

- 8 个 Eval Case：4 个 Forced Injection 答案质量 Case、3 个 Autonomous Trigger Case（1 正 2 负）、1 个 Security Probe；
- Fixture 数据里埋了「看起来像指令」的单元格，专门测试 Skill 是否教 Agent 把数据当数据；
- 所有断言都是确定性验证（JSON Schema + 字段比对 + Trace 断言），无需任何 LLM 或 API Key 即可端到端跑通。

## 工程立场

SkillGate 明确不做的事（以及为什么）：

| 不做 | 理由 |
|---|---|
| 声称 Exactly-once 执行 | 分布式系统中诚实且可证明的语义是「至少一次执行 + 幂等提交」 |
| 用 LLM Judge 裁决一切 | 主观、不稳定、不可复现；确定性验证优先是硬原则 |
| 让 Utility 抵消安全问题 | Critical Security Finding 是硬门禁，任何分数都无法覆盖 |
| 过早引入 Kafka/NATS/K8s | PostgreSQL 队列 + Lease 在当前规模下更简单、更可审计 |
| 训练模型 / Skill 自动改写 | 这是评估平台，不是训练或生成平台 |

更多细节见[已知限制与诚实声明](../misc/limitations.md)。

## 相关文档

- [核心概念](concepts.md) —— 术语的精确定义
- [架构与原理](architecture.md) —— 系统如何实现上述方法论
- [安装与快速开始](../getting-started/installation.md) —— 动手试试

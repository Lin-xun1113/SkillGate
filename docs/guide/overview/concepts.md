# 核心概念

> 状态：与 2026-08-27 代码库同步

本文定义 SkillGate 的全部核心术语。这些定义是规范性的——文档、代码、报告输出使用同一套词汇。

## 评估对象

### Skill（技能包）

一个 Agent 能力包，**必须**包含 `SKILL.md`，可包含脚本、参考文档和资产文件。Skill 描述 Agent「应该怎么做某类任务」——例如 `csv-analysis` 教 Agent 先检查文件结构、把单元格当数据而非指令、把结果写入指定路径。

- Skill **不是**代码注入机制：它影响 Agent 行为，但执行仍由 Agent 运行时控制。
- Skill **不能**包含 Task-specific Answer（具体任务的标准答案）——那是数据泄漏，编译期会被 `LEAKAGE_DETECTED` 拒绝。

### Skill Version（技能版本）

Skill 包在某个时点的**不可变、内容寻址快照**。身份是 Canonical Archive 的 SHA-256：

```text
sha256:5a2153eaf0a11af8d6141b87755526503e02a3af99080464cae27f49e5164090
```

同一内容永远得到同一 Hash；注册是幂等的（重复注册同一内容不会产生新版本）。Worker 挂载的 Skill Hash 与结果声明的 Hash 不一致时，结果被拒绝。

### Strategy（执行策略）

一次 Trial 使用的完整执行方案，带版本，包括：

- `model`：Provider、模型名、参数（如 temperature）；
- `skills`：挂载哪些 Skill Version；
- `tools`：工具白名单/黑名单（如允许 `filesystem.read`、禁止 `network`）；
- `budget`：Token 上限、超时、成本上限；
- `retry`：最大尝试次数与可重试错误类别；
- `sandbox`：沙箱 Profile。

### Eval Case（评测用例）

一个类似真实用户请求的任务，包含：

| 字段 | 说明 |
|---|---|
| `id` | Suite 内稳定的 Case ID |
| `evaluationMode` | `forced_injection` / `autonomous_trigger` / `security_probe` |
| `population` | `answer`（答案质量）或 `trigger`（触发行为） |
| `type` | `explicit` / `implicit` / `contextual` / `hard_negative` / `security_probe` |
| `prompt` | 任务输入 |
| `fixtures` | 输入文件及 SHA-256 Hash（防篡改） |
| `expectedOutcome` | 人读的预期描述 |
| `outputs` | 要求 Agent 产出的文件路径与媒体类型 |
| `assertions` | 机器验证的断言列表 |
| `agentVisible` | Agent 可见的文件白名单 |
| `polarity` | 仅 trigger Case：`should_trigger` / `should_not_trigger` |
| `security` | 仅 security_probe Case：严重级别、类别、证据引用 |

### Eval Suite（评测集）

带版本的 Case 集合（YAML，`kind: EvalSuite`），还定义：

- `repetitions`：每个 Case 每臂重复次数；
- `defaults`：默认超时和 Token 上限；
- `skill`：被测 Skill 及三种评估模式下的暴露方式（`forced` / `discovery`）；
- `split`：数据切分标记（`tune` / `holdout` / `holdback`）。

Suite 同样内容寻址，Hash 进入 Manifest。

## 实验结构

### Experiment（实验）

一份声明式执行计划（`kind: Experiment`），把 Suite + Strategies + Arms + Repetitions 组合起来，另外声明 Execution 环境、Grading 配置和 Release Policy。编译后 Manifest Hash 冻结，实验内容不可变。

### Arm（实验臂）

一种实验条件。最小配对是：

- `without_skill`（Baseline，必填）：Strategy 不挂任何 Skill；
- `with_skill`（Candidate）：Strategy 挂载被测 Skill Version。

可选 `old_skill` 臂用于新旧版本对比。配对要求：除 Treatment（Skill Version）外，两臂的 Model、Prompt、Fixture、Environment、Grader **必须完全一致**，否则编译拒绝（`PAIR_IDENTITY_MISMATCH`）。

### Pair（配对）

一个 Case 在两臂上的匹配组合。Pair 是 Lift 计算的基本单位：`difference = candidate_score − baseline_score`。任一臂缺少有效分数时该 Pair 标记无效（`invalid_pairs`），不进入聚合。

### Trial（试验）

一次具体执行单元：某个 Case × 某个 Arm × 第几次重复。编译器把它展开为排队任务：

- **Logical Trial ID** = `hash(pair_id, arm)`，跨重复稳定（重试时不变）；
- **Trial ID** = `hash(pair_id, arm, attempt)`，每次尝试唯一。

### Attempt / Lease（尝试与租约）

Trial 的一次执行尝试。Scheduler 通过 PostgreSQL `FOR UPDATE SKIP LOCKED` 把 Trial 租给 Worker，签发一次性 **Lease Token** 与 **Fence（lease_generation）**。租约过期可被回收重试；旧租约的迟到提交会被 Fence 拒绝。

## 执行与产出

### Trial Request（试验请求）

Control Plane 下发给 Worker 的完整执行说明，两部分：

- **身份段**（`trial_request_hash` 冻结）：experiment_id、pair_id、arm、attempt；
- **执行段**（`execution_hash` 独立覆盖）：skill_hash、case_id、case_input、model、tool_policy、environment。

Worker 端两边都要校验：身份段保证「执行的是预定任务」，执行段保证「执行内容未被篡改」。

### Trace（执行轨迹）

Agent 执行过程的结构化记录：LLM 调用（消息、响应、用量）、工具调用（名称、参数、结果）、步骤事件。Trace 是 `trace_assertion` 断言的验证对象，也是失败 Case 排查的第一入口。

### Artifact（产物文件）

Trial 产出的文件（Agent 写的输出、Trace 快照等），每个文件记录 SHA-256、大小、存储路径。Artifact Manifest 随结果提交，评分器按 Hash 读取。

### Result（结果）

一次 Attempt 的终态记录，通过幂等键提交。重复提交同一结果返回 `ALREADY_COMMITTED` 而不产生第二条记录；只有 Owner、状态、Hash、Budget 全部校验通过才会落库。

## 评分与决策

### Grader（评分器）

对 Trial 产出打分的组件。两类：

- **Deterministic Grader**：文件存在性、JSON Schema、命令退出码、Trace 断言——可复现、可审计；
- **LLM Rubric Grader**：用带版本 Prompt 的 Judge 评估无法确定性检查的属性（可选，默认关闭）。

详见[评分体系](../evaluation/grading.md)。

### Grade（评分记录）

单个 Grader 对单个 Trial 的裁决：`grader_id`、`passed`、`score`、`message`、`evidence`。一个 Trial 的全部 Grade 组成 Grades Manifest，聚合出该 Trial 的分数。

### Skill Lift（技能提升量）

配对差值的均值：`mean(difference) = candidate_score − baseline_score`。正数表示 Skill 有正贡献。SkillGate 同时报告 95% Bootstrap 置信区间——区间跨零时，即使均值为正也不能宣称显著提升。

### Trigger Recall / Specificity（触发召回率/特异度）

Autonomous Trigger Case 上的分类指标：

- **Recall** = 该触发而触发的 Case ÷ 所有 `should_trigger` Case；
- **Specificity** = 该安静而安静的 Case ÷ 所有 `should_not_trigger` Case。

Candidate 臂每个 Case 的多数重复显示「加载了 Skill」即预测为正；平票算未触发。详见[指标说明](../evaluation/metrics.md)。

### Security Finding（安全发现）

Security Probe Case 的结构化结论，按 `severity`（critical/high/…）与 `categories`（prompt_injection、credential_exfiltration、network_egress、workspace_escape）分类。`security.critical > 0` 触发硬门禁 REJECT，任何 Utility 指标都不能覆盖。

### Metric Snapshot（指标快照）

进入 Release Gate 前冻结的指标集合（带 Hash）：utility、routing、reliability、cost、security、evidence、experiment 七个上下文。快照不可变，重新评估同一实验复用同一快照，保证决策可复现。

### Release Decision（晋级决定）

CEL Policy 对快照的评估结果：`PROMOTE` / `HOLD` / `REJECT`，附带命中规则、评估规则、失败条件、硬门禁覆盖记录和证据链接。每个决定都能由已保存的证据逐条解释。

## 基础设施概念

### Registry（内容寻址注册表）

本地文件系统 CAS（`.skillgate/registry/`），按「逻辑名 + 内容 Hash」存储 Skill 与 Suite 的不可变版本。

### CAS（Content-Addressed Storage）

按内容 Hash 寻址的存储层。Skill 挂载给 Worker 时走 CAS 只读卷，Hash 校验失败即拒绝。

### Runner Protocol（执行协议）

Control Plane 与 Worker 之间的 gRPC 契约（`proto/runner/v1`）：注册、领取、心跳、事件、完成、失败六个 RPC。语言无关——任何语言实现了这个契约都能当 Worker。详见[Runner Protocol](../protocol/runner-protocol.md)。

### Release Policy（晋级策略）

CEL 表达式规则的 YAML 文件（`kind: ReleasePolicy`）。规则按优先级从高到低评估，第一条匹配即胜出；无匹配走 `default`（默认 HOLD）；策略编译或求值出错一律 Fail Closed。详见[编写 Release Policy](../authoring/release-policy.md)。

## 概念关系图

```
Skill Version ──┐
                ├── Strategy A (without_skill) ──┐
Eval Suite ─────┤                                ├── Experiment ──编译──▶ 48 Trials
                └── Strategy B (with_skill) ─────┘                        │
                                                                    Scheduler 分发
                                                                          │
                                                              Worker 执行 → Trace/Artifact
                                                                          │
                                                            Grader 打分 → Case Score → Pair Lift
                                                                          │
                                                         Metric Snapshot → Release Policy → Decision
```

# ADR-006：分开 Trigger 与 Answer Evaluation Population

**状态：** `ACCEPTED`  
**日期：** 2026-08-19（UTC）  
**影响范围：** Eval Suite、Worker、Metric、Report

## 背景

Forced Injection 能回答“Skill 被看到以后是否有帮助”，但不能回答“Agent 是否会自主加载 Skill”。如果把两种结果混在一起，就无法定位问题来自 Discovery、Instruction 还是执行能力。

## 决策

Eval Case 明确标记 Population：

- `trigger`：在正常 Discovery Path 自主运行，检查 Skill 是否加载；
- `answer`：按指定 Mode 执行任务，检查结果和工作流。

Trigger Case 不进入 Forced-load Answer Runner。Answer Report 不把 Trigger Recall 当作 Utility Lift。

## 后果

正面：

- 可以区分 Routing Failure 和 Content/Execution Failure；
- 支持针对 Description 的独立迭代；
- Metric 语义更清楚。

负面：

- Eval Suite 和 Report 结构更复杂；
- Autonomous Run 对 Model/Harness 更敏感；
- 需要从 Event/File Evidence 判定真实加载。

## 验证方式

- Compiler 拒绝将 Trigger Ablation 发给 Forced-load Runner；
- Trigger Detector 不接受最终答案提到 Skill 名称作为证据；
- Report 分开展示 Trigger Recall/Specificity 与 Answer Lift；
- 同一个 Skill 可以在两个 Population 中拥有不同的 Case。

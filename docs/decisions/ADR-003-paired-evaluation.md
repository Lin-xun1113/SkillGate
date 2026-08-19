# ADR-003：将配对 Baseline/Candidate Evaluation 作为强制方法

**状态：** `ACCEPTED`  
**日期：** 2026-08-19（UTC）  
**影响范围：** Experiment Compiler、Metric、Report

## 背景

只看 Candidate Arm 的高通过率，无法区分 Skill 的边际贡献和 Base Model 的能力。Skill 的价值必须回答“加入它之后改变了什么”。

## 决策

所有 Utility Experiment 默认生成匹配的 `without_skill` 与 `with_skill` Arm。除声明的 Treatment 外，Case、Prompt、Fixture、Model、Harness、Environment、Grader、Repetition 和 Budget 必须一致。统计单位默认是 Case，使用 Task-level Pairing 和 Cluster Bootstrap。

## 后果

正面：

- 直接得到 Skill Lift；
- 能发现 Skill 无效、冗余或有害；
- 结果更适合做 Release Decision。

负面：

- Trial 数量和成本约翻倍；
- Pair Identity Compiler 变复杂；
- 需要处理缺失和无效 Pair。

## 验证方式

- 编译器故意改变 Environment/Grader Hash 时拒绝；
- 报告每个 Case 的双臂结果；
- 重复运行时不重复聚合；
- 统计 Test 验证按 Case 而不是按 Judge Vote 重采样。

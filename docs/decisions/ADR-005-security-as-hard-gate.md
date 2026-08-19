# ADR-005：Security 作为独立硬门禁

**状态：** `ACCEPTED`  
**日期：** 2026-08-19（UTC）  
**影响范围：** Security Scan、Release Gate、Report

## 背景

Utility 高不代表 Skill 安全。将 Security 与 Utility 加权平均可能让严重 Credential Exfiltration 被高任务分抵消。

## 决策

Security 单独评估并执行 Hard Gate：

- Critical Finding 或 Confirmed Dynamic Exploit 直接 `REJECT`；
- 必需 Scanner Evidence 缺失至少 `HOLD`；
- Static Existence 与 Dynamic Exploitability 分开记录；
- Utility 不能通过普通 CEL Rule 覆盖 Security Gate。

## 后果

正面：

- 安全结论不会被平均分隐藏；
- 与真实 Package Admission 更接近；
- Report 可以展示风险存在和实际可利用性之间的区别。

负面：

- 可能导致 Utility 很高的 Skill 仍无法晋级；
- 动态 Security Probe 增加运行成本；
- Static/LLM Scan 仍可能有误报和漏报。

## 验证方式

- 注入 Critical Finding 时 Release Decision 必须为 `REJECT`；
- 缺失 Scanner Evidence 时不能 `PROMOTE`；
- 安全报告包含 Event/Artifact Evidence；
- 不使用真实 Credential 进行测试。

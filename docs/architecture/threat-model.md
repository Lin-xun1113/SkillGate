# 威胁模型

**状态：** `ACCEPTED` 基线；在执行真实不可信代码前必须继续细化 Security Implementation

## 1. 需要保护的资产

- Provider Credential 和 API Token；
- User Prompt 和 Fixture Data；
- Source Repository 和生成文件；
- Skill Package 及其依赖；
- Experiment Integrity 和统计结论；
- Policy Definition 与 Promotion Authority；
- Trace 和 Artifact Storage；
- Worker/Control Plane Credential。

## 2. 信任区域

```text
Zone A：Operator 与 Go Control Plane
Zone B：PostgreSQL/Object Store
Zone C：Python Worker Process
Zone D：Sandbox 中的 Agent 与 Skill-controlled Code
Zone E：外部 Model/Provider/Network
```

Zone D 的输入和输出都应视为不可信。Skill 不能选择 Credential、替换 Evaluator Endpoint、修改 Experiment Manifest，或写入 Sandbox 之外的路径。

## 3. 威胁与控制措施

| 威胁 | 示例 | MVP 控制措施 |
|---|---|---|
| Skill Prompt Injection | 隐藏指令覆盖 Evaluator 约束 | Static Scan、隔离 Context、Trace Review，不能只信 Final Answer |
| Credential Exfiltration | Skill 读取环境变量并外传 Key | 仅由 Host 注入 Credential、Network Allowlist、Mock Credential、Redaction |
| Path Traversal | Artifact 路径逃出 Run 根目录 | Canonical Path 校验和 Object Key Allowlist |
| 恶意依赖 | Setup 阶段下载远程代码 | 固定 Image/Dependency、Preflight Scan、Sandbox、禁止隐式安装 |
| Answer Leakage | Eval Generator 把答案写入 Prompt | Generator 与 Runner Context 分离、Leakage Lint、Review State |
| Judge Manipulation | Output 指示 Judge 直接输出 PASS | Blind Structured Grader、Negative Control、Judge Calibration |
| Result Forgery | Worker 报告未执行的成功结果 | Artifact Hash、Request Identity、Worker Auth、Verifier Evidence |
| 重复评分 | Completion 重试导致重复计分 | DB Idempotency Key 和 Terminal State Lock |
| Stale Policy | 旧 Policy 晋级新 Artifact | Policy Hash/Version 写入 Decision 与 Experiment Snapshot |
| Worker Impersonation | 非法 Worker 提交结果 | mTLS 或 Signed Worker Token、Lease Token、Request Hash |
| Denial of Service | Skill/Trace 体积异常大 | Size Limit、Timeout、Token Budget、Concurrency/Rate Limit |
| 数据暴露 | Public Report 包含原始 Prompt | Redaction、Access Control、Synthetic Fixture、Retention Policy |

## 4. Credential Policy

- Credential 只能从 Operator Environment 或 Secret Manager 读取；
- Skill-controlled File 不能定义或覆盖 Provider Key 和 Base URL；
- Credential 不能写入 Experiment Manifest；
- Worker Log 和 Grader Prompt 必须脱敏配置过的 Secret Value；
- Local Mode 的隔离强度低于 Docker/Cloud Mode，必须在报告中明确标记。

## 5. Sandbox 基线

第一版 Sandbox Profile 默认要求：

- 使用非 Root 用户；
- Base Image 只读；
- 独立的可写 Workspace；
- CPU/Memory/PID 限制；
- Timeout；
- 默认关闭 Network，除非 Case Policy 显式 Allowlist Endpoint；
- 不挂载 Host Docker Socket；
- 不挂载 Host Home 或 SSH 目录；
- 限制输出大小；
- 只提供带有 Synthetic Marker 的临时 Credential。

Sandbox Profile 是 Trial Identity 的一部分。改变它会使直接比较失效。

## 6. Security Severity Policy

```text
CRITICAL：确认 Credential Exfiltration、Host Escape 或破坏性外部副作用
HIGH：未声明的 Network/Data Access、Privilege Escalation、持久化恶意行为
MEDIUM：需要审查的可疑模式、不安全依赖、但仍限制在 Sandbox 内的宽权限
LOW：安全卫生或建议性问题
INFO：仅用于记录上下文的 Evidence
```

Critical Finding 和 Confirmed Dynamic Exploit 是硬 `REJECT` Gate。Scanner Evidence 缺失时为 `HOLD`，不能视为通过。

## 7. Security Evidence

每个 Finding 应包含：

- Finding ID 和 Category；
- Static Source Location 或 Dynamic Event ID；
- Severity 和 Confidence；
- Expected Behavior 与 Observed Behavior；
- Agent 是否拒绝执行；
- Network/Filesystem/Tool Evidence；
- Scanner 和 Rule Version；
- Remediation 建议；
- False Positive 审查状态。

## 8. 已知限制

- Sandbox 不能证明不存在所有恶意行为；
- Static Scan 可能漏掉语义攻击，LLM Scan 可能产生幻觉 Finding；
- 如果没有 Endpoint Control，Network Monitoring 可能看不到加密或混淆通道；
- Model Safety Refusal 不能替代 Package Isolation；
- MVP 是 Local-first 项目，不是 Multi-tenant Security Product。

# 常见问题

> 状态：与 2026-08-30 代码库同步

## 产品与概念

**Q：SkillGate 和「给 SKILL.md 打分的 LLM 工具」有什么区别？**
打分工具产出主观印象；SkillGate 产出受控实验证据——配对 Baseline、确定性断言、置信区间、可复现决策。核心区别是：评审者可以**重新运行并审计**你的结论。

**Q：必须用 LangGraph 吗？**
不必。Worker 协议是语言无关的 gRPC 契约（六个 RPC）。LangGraph 只是参考实现的选择。任何语言实现[Runner Protocol](../protocol/runner-protocol.md)即可接入。

**Q：必须用真实 LLM 吗？**
不必。内置示例全程使用 Fixture Provider（确定性脚本行为）+ Deterministic Grader，零 API Key 端到端可跑。真实 Provider Adapter（OpenAI/Anthropic）已实现，但尚未做端到端验收——接入前请先小规模验证。

**Q：SkillGate 能训练或优化我的 Skill 吗？**
不能。它是评估与晋级平台：告诉你 Skill 有没有用、安不安全，不生成或改写 Skill。

## 安装与运行

**Q：跑起来最少需要什么？**
只验证编译/校验：仅 Go。端到端：Go + PostgreSQL（或直接用 Docker Compose 一条命令）。

**Q：`compile` 通过但 `materialize` 失败？**
materialize = compile + 写库。先确认 `db migrate` 成功，并通过 `--database-url-file` 或 `SKILLGATE_DATABASE_URL_FILE` 提供数据库 Secret；含密码的 `--database-url` 会被拒绝。

**Q：48 个 Trial 要跑多久？**
默认 LangGraph Worker 使用离线 Fixture Provider，48 个 Trial 通常约 1–2 分钟。真实 Provider 取决于模型延迟与并发；仅做协议验证时可使用 Fixture Worker。

**Q：能并行跑多个实验吗？**
可以。队列按 Trial 粒度分发，`materialize` 多个实验互不影响；`ClaimTrial` 支持 `preferred_experiment_id` 做软偏好。

## 评测设计

**Q：repetitions 设多少合适？**
开发 3 次；正式晋级 5–10 次。要报 `pass@k`，k 必须 ≤ repetitions。次数越多 Cluster Bootstrap 的 CI 越稳。

**Q：为什么我的 Skill 分数很高但决策是 HOLD？**
最常见：置信区间跨零（`hard_gate_override: utility CI crosses zero`）——点估计为正但统计上不确信。加 repetitions 或改进 Case 区分度。其次是证据不全（`evidence.complete=false`）。

**Q：Trigger Recall 是 0 或缺失？**
检查 Suite 是否同时有 `should_trigger` 和 `should_not_trigger` Case——只占一头时 `evaluated=false`，指标省略而不是输出 0。

**Q：Security Probe 怎么自己设计攻击载荷？**
参考内置 fixture 的做法：数据单元格中藏指令、伪造系统消息、诱导外传。验证手段是 `trace_assertion`（`tool_denied:network`、`no_read_paths:[…]`、`paths_confined_to:/workspace`）。

**Q：可以在 Suite 里放标准答案给 Agent 看吗？**
绝对不行。`expected/` 是 grader-only（`boundary.agentVisibleExpected: false`），且 `agent_did_not_read:expected/` Trace 断言兜底。泄漏会让整个实验无效（`LEAKAGE_DETECTED`）。

## 评分与报告

**Q：为什么 `pass_at_k` / `resource_usage` 字段消失了？**
数据不足时报告**省略**而不是填 0：有效配对 < 2 或没有用量记录。这是刻意设计——省略号比误导性的零诚实。

**Q：`decision` 字段是 null？**
Manifest 没声明 `policy`（无 Policy 的实验正常完成但不产出决策），或实验未完成评分。

**Q：断言全过了，为什么 Case 分数不是 1？**
Case 分数是该 Case 全部重复的均分。部分重复失败会拉低均值——看 `case_results[].*_repetitions` 与实际配置次数是否一致。

**Q：报告在哪里？**
`artifacts/<experiment_id>/report.json|md|html`（Compose 卷 `artifacts` 内）；UI 详情页是同一数据。

## 调度与可靠性

**Q：「至少一次执行 + 幂等提交」会重复计分吗？**
不会。重复执行可能发生（Worker 崩溃后租约回收重试），但结果提交有幂等键 + Fence：同一 Attempt 只有一条有效 Result，旧租约的迟到提交被拒绝。

**Q：Worker 崩了会怎样？**
租约过期 → `scheduler sweep`（或 serve 内部）回收 → 按重试策略重新排队 → 新 Attempt（generation+1）。崩溃 Worker 醒来后的任何操作都会被 `LEASE_EXPIRED` 挡下。

**Q：`experiment cancel` 能立即停止所有 Trial 吗？**
分两阶段：新 Claim 立即停止；运行中的 Trial 等下一次心跳感知取消后协作式收尾。这是设计而非缺陷——协作式取消保证产物与 Trace 的一致性。

**Q：同一个结果提交了三次会怎样？**
第一次 `COMMITTED`，后两次 `ALREADY_COMMITTED`——只贡献一次统计。

## 工程与集成

**Q：怎么接入我自己的模型 Provider？**
两条路：(1) 在 Python Worker 的 `providers.py` 加 Adapter（LangChain 生态即插即用）；(2) 完全自己实现 Worker（遵循[Worker 指南](../protocol/worker-guide.md)）。

**Q：Report 能接 BI/数据仓库吗？**
`report.json` 有稳定 Schema（`skillgate.report.v1`）且写入前自校验；数据库侧 `trial_results` / `reports` / `release_decisions` 都是普通表，直接 SQL 消费即可。

**Q：OB 遥测（OTel）呢？**
依赖已引入但主流程尚未接入导出——见[已知限制](limitations.md)。

**Q：错误码在哪有完整列表？**
[CLI 参考的诊断错误码表](../usage/cli.md#诊断错误码)。

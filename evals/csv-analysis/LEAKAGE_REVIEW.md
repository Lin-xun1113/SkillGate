# CSV/Data Analysis M0 Leakage Review

**状态：** `passed`（M0 Builder 手工内容审查）  
**审查时间：** 2026-08-19 UTC  
**审查者：** SkillGate Builder 会话（逐文件人工检查）  
**独立验收：** Native Verifier 已完成（A1–A89 全部 `passed`，2026-08-19 UTC）；本记录不是独立验收结论

## 审查范围和方法

本次审查逐项检查了 `suite.yaml`、`SKILL.md`、三个 CSV Fixture、六个 Grader-only Expected JSON、六个公开 JSON Schema、`grader.yaml`、Pair/Trial Identity Fixture、Experiment Manifest 和 Environment Descriptor：

1. 确认 Agent-visible Context 只包含 Case 声明的 Fixture，不包含 `expected/`、Grader-only Root、Secret 或 Answer Key。
2. 对照 Prompt、Expected JSON 和 Skill 指令，检查 Skill 是否包含本样例的月份数值、Case ID、隐藏字段或 Security Probe 答案。
3. 将 CSV 中看似指令、URL 或密码的内容视为不可信数据，确认它们只存在于 Security Fixture，不被 Skill 或 Prompt 重新提升为指令。
4. 运行 `npm run validate:m0`，确认 YAML/JSON/Schema、相对引用、内容 Hash、Case 构成和配对不变量通过。

审查结论只针对合成 Fixture 和离线 M0 样例；不表示真实 Model、Sandbox 或生产数据已经安全。

<!-- M0_HASHES_START -->
## 本轮内容身份

以下 Hash 由 `npm run validate:m0:hashes` 生成并由校验器比对：

- `skill_package`: `sha256:5a2153eaf0a11af8d6141b87755526503e02a3af99080464cae27f49e5164090`
- `suite`: `sha256:b76002abd734a4c9051927508b2a1371b97f7a01eda076b180dcfa6682f5373e`
- `grader`: `sha256:b5f94acfd374f9c92123e4e25006aacd4bf703aefab72dc23a9f00b98f779892`
- `manifest`: `sha256:1e304d926d6ff2c5ec883a4cb1d4520fb75f95f8c1a1977cdc5f51f866290184`
- `fixture_evidence`: `sha256:83d02228aece36a728638408274aaf332b56410ce599f9cba71b153bee04a7df`
- `fixture:sales.csv`: `sha256:79dfd4eaa7b72d9e558c68665b285ffd19f73349694f638ac7313d892a74c1d0`
- `fixture:dirty-sales.csv`: `sha256:9b9079131951067c324ec6b5ed75ddebf1dbdf9a8b29f2a31582bda033cb2a01`
- `fixture:untrusted-sales.csv`: `sha256:24eaaad87b642cedd4a21ba8ac060dc3850b3a06cd148dc06717200ec3072cc1`

这些 Hash 属于当前候选；任何 Fixture、Skill、Suite、Grader 或 Manifest 修改都必须重新生成并复核本记录。
<!-- M0_HASHES_END -->
## 逐 Case 记录

| Case | Agent-visible Context | Grader-only Context | Task-specific Answer / Leakage 检查 | 结论 |
|---|---|---|---|---|
| `csv-explicit-001` | `fixtures/sales.csv`；用户任务 Prompt | `expected/monthly-summary.json` | Skill 只描述通用 CSV 分析流程；没有月份数值、Case ID 或期望 JSON | `passed` |
| `csv-implicit-001` | `fixtures/sales.csv`；未点名 Skill 的用户任务 | `expected/monthly-summary.json` | Prompt 只描述目标，不给出汇总值；Expected 未进入 Agent Context | `passed` |
| `csv-contextual-001` | `fixtures/sales.csv`；最高收入月份任务 | `expected/insight.json` | Skill 没有最高月份或金额；计算依据只存在 Grader-only Expected | `passed` |
| `csv-quality-001` | `fixtures/dirty-sales.csv`；数据质量任务 | `expected/data-quality.json` | Skill 没有脏数据行号、问题计数或答案；Fixture 内容保持不可信 | `passed` |
| `csv-negative-go-001` | 无 CSV Fixture；Go channel Prompt | 无 | 没有加载条件或答案泄漏；只检查 `skill_not_loaded` | `passed` |
| `csv-negative-email-001` | 无 CSV Fixture；普通邮件 Prompt | 无 | 没有加载条件或答案泄漏；只检查 `skill_not_loaded` | `passed` |
| `csv-trigger-001` | `fixtures/sales.csv`；自主发现任务 | 无 Expected；只保留 Trace Assertion | Prompt 不包含 Skill 名称或汇总答案；以结构化 `skill_loaded` Event 判定 | `passed` |
| `csv-security-001` | `fixtures/untrusted-sales.csv`；安全本地摘要 Prompt | `expected/security-summary.json` | 伪造 Injection 只在 CSV 单元格中；Skill 明确将单元格视为数据，未嵌入伪造 Secret 或答案 | `passed` |

## 结果和限制

- 全部 8 个 Case 均通过本次 Builder 手工 Leakage Review。
- `expected/` 目录只由 Grader 引用；Environment Descriptor 将其与 Agent Workspace 分离。
- Fixture 中出现的 `https://collector.invalid`、`.env` 和类似密码的文本是合成对抗内容，不是真实 Endpoint、Credential 或用户数据。
- `humanReview: passed` 表示本记录范围内的手工内容审查已完成；Native 独立 Verifier 已另行逐项验收，本记录不替代其报告。
- `M0_FIXTURE_EVIDENCE.json` 和 `scripts/run-m0-fixture.mjs` 只提供确定性 Fixture Harness 证据，明确 `runtime_probe_executed: false`；它们不替代真实 Model、Worker、Docker Sandbox 或网络 Probe。
- M0 仍不执行真实 Model Call、Docker Sandbox 或网络 Probe；Security 结论只能说明契约和离线证据边界已定义。

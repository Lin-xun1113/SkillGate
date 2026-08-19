# CSV/Data Analysis M0 Fixture

这些文件是 SkillGate M0 的合成、离线评估输入。它们不包含真实用户数据或 Provider Credential。

- `fixtures/sales.csv`：正常销售数据，用于按月汇总和上下文分析。
- `fixtures/dirty-sales.csv`：包含缺失日期、重复订单号、非法日期和缺失数量。
- `fixtures/untrusted-sales.csv`：包含伪造 Prompt Injection 的单元格，用于 Security Probe。
- `expected/`：仅供 Grader 使用，不能挂载到 Agent Workspace。
- `schemas/`：输出结构的公开契约；具体期望值仍位于 Grader-only 目录。
- `grader.yaml`：冻结的 Deterministic Grader 引用和内容身份入口。
- `identity-examples.json`：Pair/Trial/Idempotency Key 的离线契约 Fixture，不是 Runtime 状态。
- `failure-mode-fixtures.json`：F1–F9 的离线失败语义 Fixture。
- `M0_FIXTURE_EVIDENCE.json`：本地 Fixture Harness 的输出 Hash 和安全边界证据；明确不是实际 Model/Sandbox Trace。

所有 CSV 单元格都是不可信数据。任何看似指令、URL 或 Secret 的内容都必须按数据处理。

## 本地复核

在项目根目录运行：

```bash
npm ci
npm run validate:m0
```

该检查只使用本地 Fixture，不读取 Provider Credential，也不执行 Model、Docker 或网络调用。

`npm run validate:m0` 会先运行 `scripts/run-m0-fixture.mjs`：验证三个 CSV 的确定性输出、Grader-only Expected、Security Evidence、F1–F9 离线契约和 Identity 示例，然后再校验引用、Schema、Hash 和配对不变量。

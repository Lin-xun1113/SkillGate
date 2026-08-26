# Outcome

实现 M6：Release Gate。在 M5 Grading/Report 之后，把冻结的 Metric Snapshot、Trigger 聚合与 Security Finding 交给 CEL Release Policy，输出可解释的 `PROMOTE` / `HOLD` / `REJECT`，并写入数据库与 Report。Runtime Routing（执行前选 Strategy）不在本里程碑实现。

# Scope

1. **CEL Policy 编译与评估**
   - 解析 `ReleasePolicy` YAML（沿用 `policies/conservative-release.yaml` 与 `docs/templates/policy.yaml`）。
   - 发布时 Parse / Type-check / Compile CEL；按 Policy Hash 缓存编译结果。
   - 按 Priority 从高到低评估；第一条匹配规则胜出；相同 Priority 在校验阶段拒绝。
   - 无匹配规则时使用 `default`（默认 `HOLD`）。
   - Policy Error 与 Security-sensitive 缺失字段 Fail Closed，不能静默 `PROMOTE`。

2. **Trigger Metric 与 Security Finding 聚合**
   - 从 Suite Case 的 `evaluationMode`/`population`/`polarity` 与 Trial grades/trace assertion 计算 `routing.recall` 与 `routing.specificity`。
   - Security Probe Case 不进入 Utility Lift 或 Trigger 分母。
   - 从 Security Probe Case 的 finding artifact / grades 聚合 `security.critical`、`security.high`、`security.confirmed_exploits`。
   - 缺失 Scanner Evidence 或必需 Trial 未完成时，`evidence.complete=false`。

3. **冻结 Metric Snapshot**
   - Grading 完成后构造不可变 Context Snapshot（utility / routing / reliability / cost / security / evidence / experiment）。
   - Snapshot 有 Canonical Hash；Decision 只引用该 Hash，不回读可变 Report。

4. **Release Decision**
   - Control Plane 在 `GRADING` 完成、进入 `COMPLETED` 之前自动评估 Policy。
   - 结果为 `PROMOTE` / `HOLD` / `REJECT`，含 Policy ID/Version/Hash、命中规则、已评估未命中规则、失败条件与实际值、Evidence 链接。
   - Decision 写入 `release_decisions`，并追加到 `report.json` / `report.md`。
   - Manifest 未引用 Policy 时只输出无 Decision 的报告，Experiment 仍可 `COMPLETED`。

5. **集成与 CLI**
   - `ProcessExperiment` 在保存 Report 后调用 Release Gate。
   - CLI 可查询 Decision（不作为本里程碑的独立重评入口）。
   - 单元测试覆盖 Policy 编译、优先级、硬门禁、缺失证据、正向晋级与可解释 Trace。

# Non-goals

- 不实现 Runtime Routing / Strategy 选择（留待 M7）。
- 不在运行时修改已编译 Experiment 的 Treatment、Skill、Model 或 Environment。
- 不实现人工 Override Security Hard Gate。
- 不实现完整 Security Probe 攻击面扫描或动态 Exploit 执行。
- 不实现 REST API、UI、S3 Artifact 上传、Kubernetes、多租户。
- 不把真实 LLM Judge 校准作为 Release 前置条件（M5 已默认禁用 LLM Grader）。
- 不引入 Kafka/NATS；不把 CEL 暴露为任意脚本执行器。

# Acceptance examples

- A1：Policy 编译。合法 `ReleasePolicy` 通过 Parse/Type-check/Compile；相同 Priority 被拒绝；未知字段、超长 Expression、Network/File Function 被拒绝；编译结果按 Policy Hash 可复用。
- A2：Trigger 聚合。Autonomous Trigger Case 按 polarity 计算 `routing.recall` 与 `routing.specificity`；Security Probe 与 Answer Case 不进入 Trigger 分母。
- A3：Security 聚合。Security Probe Case 的 finding 计入 `security.critical` / `security.high` / `security.confirmed_exploits`；Scanner Evidence 缺失时 `evidence.complete=false`。
- A4：硬门禁 REJECT。`security.critical > 0` 或 `security.confirmed_exploits > 0` 时 Decision 为 `REJECT`，即使 Utility Lift 为正。
- A5：证据不足 HOLD。`evidence.complete=false`、必需 Trial 未完成、或 Lift CI 跨越 0 时至少为 `HOLD`，不能 `PROMOTE`。
- A6：正向晋级 PROMOTE。`utility.ci_lower > 0` 且 routing / reliability / cost / security 门禁全部通过时 Decision 为 `PROMOTE`。
- A7：可解释 Decision Trace。输出包含 Policy Hash、Metric Snapshot Hash、命中规则、已评估未命中规则、失败条件与实际值；不能只返回一个枚举值。
- A8：端到端接入。`ProcessExperiment` 在保存 Report 后自动评估 Policy，Decision 写入数据库并出现在 `report.json`/`report.md`；无 Policy 引用时 Experiment 仍完成且报告不含 Decision。
- A9：Decision 引用冻结 Snapshot。同一 Snapshot 重复评估结果一致；Decision 不回读可变 Report 字段。
- A10：开发期检查。`go test ./internal/strategy/... ./internal/releasegate/... ./internal/metrics/... ./internal/grading/...`、`go vet ./...` 与 `go test ./...` 通过。

# Constraints and invariants

- Security 是独立硬门禁，不能被 Utility 加权覆盖（ADR-005）。
- 统计单位仍是 Case；Security Probe 不进入 Utility/Trigger 分母（ADR-003、ADR-006）。
- Release Gate 只读冻结 Snapshot，不重新执行 Trial，不改变已编译 Experiment。
- CEL 只暴露已声明字段；Unknown Field 校验失败；Security 缺失 Fail Closed。
- 相同 Priority 在校验阶段拒绝；第一条匹配规则胜出。
- Policy Error 返回 `HOLD` 或 `REJECT`，永不静默 `PROMOTE`。
- 验收路径离线自包含，不依赖真实 LLM Credential。
- A 项只来自本 brief，规格正文不自动膨胀为独立验收项。

# Decisions

- D1：M6 只实现 Release Gate，不实现 Runtime Routing。理由：用户确认；Routing 会改变执行前 Strategy，而 M0–M5 Experiment 已冻结 Treatment。
- D2：M6 补齐 Trigger 与 Security 聚合，再进入 Gate。理由：用户确认；否则 conservative Policy 的 routing/security 字段永远缺失。聚合只消费已有 Case 元数据、grades 与 finding artifact，不新增动态扫描。
- D3：Grading 完成后自动评估 Policy。理由：用户确认；Decision 是 COMPLETED 报告的一部分，不新增独立重评入口。
- D4：复用 CEL-Go 作为唯一 Expression Engine。发布时 Compile，按 Policy Hash 缓存；不引入自定义 DSL 或任意 Go 回调。
- D5：Decision 结果仅为 `PROMOTE` / `HOLD` / `REJECT`。Policy Error 与 Security 缺失至少 `HOLD`；Critical Finding 强制 `REJECT`。
- D6：Context Schema 冻结为 `utility`、`routing`、`reliability`、`cost`、`security`、`evidence`、`experiment` 七组已声明字段。Unknown Field 拒绝；缺失 Security/Evidence 不得填宽松默认后晋级。
- D7：Decision 持久化到 `release_decisions`，并引用 `metrics_snapshots` 的 Canonical Hash。Report 只附加 Decision 摘要，不以 Report JSON 作为评估输入。
- D8：沿用仓库已有 `policies/conservative-release.yaml` 作为默认验收 Policy；阈值保持文档默认值，不在 Metric Library 中硬编码。
- D9：不支持人工 Override Security Hard Gate（与 Release Gate Contract 一致）。
- D10：包边界为 `internal/strategy`（CEL compile/evaluate + Decision Trace）与 `internal/releasegate`（Snapshot 构造、Hard Gate、落库、Report 附加）。Control Plane 只调用 Release Gate，不直接拼 CEL。

# Open questions

（无未决阻塞问题。用户于 2026-08-26 确认 D1–D10、A1–A10、范围与非目标。）

# Verification expectations

- Policy 编译与拒绝用例的单元测试（相同 Priority、未知字段、非法 Function）。
- Trigger recall/specificity 与 Security Finding 聚合的固定 Fixture 测试。
- Hard Gate：Critical Finding → REJECT；证据缺失 → HOLD；正向门禁 → PROMOTE。
- Decision Trace 字段完整性测试。
- `ProcessExperiment` 在无 Policy / 有 Policy 两条路径上的集成测试。
- Snapshot Hash 稳定性与重复评估幂等测试。
- `go test ./...`、`go vet ./...`。

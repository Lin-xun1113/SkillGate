# Native 验收分层与 M1 收敛设计

**状态：** PROVISIONAL，待用户审阅  
**版本：** 0.1.0  
**时间基准：** UTC  
**日期：** 2026-08-20  
**关联能力：** `m1-registry-manifest-compiler`、项目级 Comet Native 集成

## 1. 背景与问题定义

SkillGate 当前使用 Comet Native。M1 的目标是实现本地 Skill/Suite Registry、Manifest Compiler、配对 Identity 和可审计 CLI，但上一轮 Native Loop 将 brief 的 9 个高层 Acceptance 与完整 M1 Spec 中的规范性段落、列表项、表格行合并为一个连续验收集合，历史上形成 A1–A83。

只读核查确认，当前项目安装的 Native Runtime 在构造 Acceptance 时合并三类来源：

1. `brief.md` 的 `# Acceptance examples` 一级列表；
2. Spec 中显式的 `Scenario:` 标题块；
3. Spec 中普通的 mandatory requirement 文本，包括段落、列表项和表格行。

因此模块说明、错误码表格、测试矩阵和实现细节会被当成独立 Runtime Acceptance。Native 官方文档只要求 Verifier 覆盖 Runtime 提供的完整验收集合，并未要求完整 Spec 的每个条款成为独立 Acceptance。问题属于项目集成的验收语义与 Runtime 提取策略不匹配，而非 M1 行为要求可以删除。

本设计解决四个问题：

- 将 Runtime Acceptance 与完整 Spec、测试计划和设计说明分层；
- 让验收来源显式、可追踪、可持久化，避免大型 Spec 再次膨胀；
- 保留 M1 完整行为契约和测试覆盖，不因减少 A 项而降低质量；
- 让 Native Loop 成为唯一正式流程控制器，减少重复的外部流程治理和无效 Verifier 轮次。

## 2. 已确认决策

以下决策已由用户在 2026-08-20（UTC）确认：

1. **继续使用 Native，不切换 Classic。** Native 适合本项目当前模型能力和可复现的 Runtime 验收；Classic 不能自动修复 Acceptance 来源建模问题。
2. **Runtime Acceptance 采用显式来源。** M1 以 brief 中 9 个高层、可观察、互不重复的 Acceptance 为主；Spec 普通段落、列表和表格不再默认展开为独立 Runtime Acceptance。
3. **完整 Spec 继续保留。** Identity、引用安全、错误码、失败语义、模块边界和实现限制仍是正式规范，不因 Runtime 项目数减少而删除。
4. **测试矩阵继续保留，但不作为 Runtime 粒度。** 测试矩阵映射到高层 Acceptance 和开发期 Gate；它不是一组额外的 A 项。
5. **Runtime 修正必须持久化在项目中。** 项目将纳入必要的 Comet Native Skill/Runtime 副本或等价的项目固定安装内容，并固定版本，避免只修改本机全局 npm 包后跨设备复发。
6. **M1 Identity 三项已冻结，不在 Build 中改变：** Environment Identity = URI + Descriptor Hash；Skill Package = 原始文本并仅规范换行；Suite 引用 = Declared Hash。
7. **M1 Build 采用五个内部模块一次性收敛：** Reference Validator、Identity Projection、Pair/Treatment Validator、Registry、CLI/Test Gate。它们是实施组织单元，不拆成多个并行 Native Change。

## 3. 设计目标与非目标

### 3.1 目标

- Runtime Acceptance 数量稳定在 9 项，必要时允许经过 Shape 明确确认的少量高层项，但不超过 12 项。
- 每个 Runtime Acceptance 都能描述一个用户可观察结果，并拥有完整 Spec 和测试证据映射。
- 同一份 brief/spec 在本地、恢复会话和另一台机器上生成相同 Acceptance ID/文本。
- Verifier 每轮收到精简且有界的验收上下文；完整 Spec 仍可按需读取。
- 对失败结果按根因模块归类，一次性修复，不按单条诊断进行 patch loop。
- 保留 Runtime 的独立只读 Verifier、必要检查、失败回 Build、次数上限和安全归档机制。

### 3.2 非目标

- 不降低 M1 的安全、Identity、幂等、失败行为或测试要求。
- 不把完整 Spec 改写成只有摘要的文档。
- 不实现 Exactly-once Execution，也不提前实现 M2 的 Queue/Lease/Result Commit。
- 不把五个内部模块创建为五个 Native Change，不引入并行合并协调成本。
- 不修改上游 Comet npm 包或声称改变官方 Comet 行为；只维护项目自己的固定集成副本/补丁。
- 不在本设计阶段修改 Go 实现或推进 M1 Build。

## 4. 分层验收模型

### 4.1 三层内容模型

| 层级 | 载体 | 用途 | 是否进入 Runtime Acceptance |
|---|---|---|---|
| L1 Runtime Acceptance | `brief.md` 的 `Acceptance examples` | 用户可观察的高层结果、Shape/Verify 主清单 | 是 |
| L2 Normative Spec | `spec.md` 的完整章节 | 字段、错误、Identity、边界和失败语义 | 否，除非显式 `Scenario:` 且 Shape 确认纳入 |
| L3 Evidence/Tests | Spec 的测试矩阵、测试文件、Runtime Check | 证明 L1/L2 的覆盖和执行证据 | 否，映射到 L1 或开发 Gate |

L2 不是非规范内容。它仍然是 Builder 和 Verifier 的调查依据；区别仅在于不把每一个条款独立计数为 Loop 目标。

### 4.2 M1 Runtime Acceptance 基线

M1 保留以下 9 项高层 Acceptance，文本应保持行为导向：

- **A1 构建与回归：** Go Module、`cmd/skillgate` 和 M1 内部包可构建，规定的 Go 检查与 M0 回归通过。
- **A2 Skill Identity 与路径安全：** Skill Package 的 Canonical Identity 在等价表示下稳定，并拒绝越界、绝对路径、符号链接、非法内容和缺少根级 `SKILL.md`。
- **A3 Registry 不可变性与幂等：** Skill/Suite 版本以内容 Hash 定位，重复注册幂等，不覆盖旧内容，重启或索引恢复后结果可复现。
- **A4 引用与输入安全：** Suite、Manifest 及其所有直接引用执行完整存在性、Root、Hash、Secret、Leakage 和 Answer-Key 隔离校验。
- **A5 Manifest Identity：** Manifest Projection 只纳入冻结的用户可见 Identity 字段，排除机器路径，并正确绑定 Suite、Skill、Grader、Policy 和 URI+Descriptor Environment Identity。
- **A6 配对编译：** Compiler 只生成冻结的 `without_skill`/`with_skill` 与 `skill_version` Treatment，非 Treatment 字段匹配，并生成稳定 Pair/Trial Plan。
- **A7 诊断与 CLI Contract：** CLI 的文本/JSON 输出、版本化 Envelope、稳定诊断 Code、错误分类和退出码符合 Contract。
- **A8 离线边界：** M1 不读取 Provider Credential、不访问网络、不依赖 PostgreSQL/Python/Worker，Contract Test 使用仓库内 Fixture。
- **A9 文档与边界同步：** M1 正式文档、ADR、README、PROJECT_STATUS 和 M2 入口一致，并明确 M1 不证明 Worker 执行、数据库可靠性或真实 Skill Utility。

A 项的验证可以使用大量测试和多个 Spec 条款，但这些证据不再各自生成 Runtime A 项。

### 4.3 显式 Acceptance 来源规则

项目固定的 Native Runtime 应采用以下来源规则：

```text
Runtime Acceptance =
  brief.md / # Acceptance examples 下的顶层列表
  + 用户明确确认并以 Scenario: 标记的少量补充场景
```

默认关闭：

```text
spec.md 普通段落 → 不自动生成 spec-must Acceptance
spec.md 普通列表 → 不自动生成 spec-must Acceptance
spec.md 表格行 → 不自动生成 spec-must Acceptance
```

`Scenario:` 只用于确实需要单独观察的用户可见场景；模块说明、字段表、错误码表和测试矩阵不得伪装成 Scenario。若未来需要扩大 Acceptance，必须在 Shape 中修改 brief 或显式 Scenario，并重新取得用户共享理解确认。

Runtime 仍必须读取完整 Spec 进行 Shape/Build/Verify 调查；“不生成 Acceptance”不等于“不检查规范”。Verifier 可以在 A4 下检查所有引用安全细节，并在 reason/evidence 中给出覆盖关系。

## 5. Spec 与测试矩阵结构

M1 完整 Spec 调整为以下语义分区，具体章节编号可在实施时保持最小改动：

```text
1. 目标与边界                         Normative Scope
2. 用户可观察行为与高层结果             Acceptance References
3. 模块与接口                           Design/Boundary
4. Canonical Identity                  Normative Contract
5. File System CAS Registry             Normative Contract
6. Manifest Compiler                   Normative Contract
7. 稳定诊断 Contract                    Failure Contract
8. CLI Contract                         User-visible Contract
9. 测试与证据矩阵                       Evidence/Development Plan
10. M2 延期边界                         Non-goals/Deferred
```

测试矩阵采用覆盖映射，不单独编号为 Runtime A 项。例如：

| Runtime Acceptance | Spec 覆盖 | 测试/证据覆盖 |
|---|---|---|
| A2 | Identity 4.1、路径安全 | Canonical 表格/换行/Unicode/二进制、路径和符号链接负向测试 |
| A3 | Registry 5 | 幂等、不同版本、重启读取、索引恢复、冲突测试 |
| A4 | 引用/安全校验 | Duplicate Case、Secret、Leakage、Expected 隔离、Hash mismatch |
| A5 | Manifest Projection 4.2 | URI/Descriptor、Declared Hash、路径排除和 Hash mutation |
| A6 | Compiler 6 | Pair mutation、Missing Arm、Mode/Population、稳定 ID Golden |
| A7 | Diagnostic/CLI 7–8 | JSON/Text Golden、退出码、I/O/冲突子进程测试 |
| A8 | 边界与非目标 | 离线命令、Credential/网络扫描、M0 回归 |
| A9 | 文档/ADR | 文档一致性检查和状态审阅 |

矩阵可以放在 M1 Spec 第 9 节，也可以在后续拆成 `docs/implementation/m1-test-plan.md`；它必须保持可读、可执行、可追踪，但不进入 Runtime Acceptance 集合。

## 6. Runtime 持久化方案

### 6.1 安装资产

当前 `.pi/skills/comet-native` 是指向 `.comet/skills/skills/comet-native` 的绝对路径符号链接，而 `.comet/*` 默认被忽略，无法保证另一台机器有同样 Runtime。实施时将：

1. 记录 Comet 版本（当前核查为 `@rpamis/comet 0.4.0-beta.18`）和项目 Skill 安装来源；
2. 将必要的 Native Skill/Reference/Script 文件转为仓库内可追踪的项目副本，避免绝对路径符号链接；
3. 只修改 Acceptance 提取相关实现和必要回归测试/说明，尽量不复制无关全局 Runtime；
4. 增加版本与补丁说明，明确上游更新时需重新审阅本地补丁；
5. 通过 `comet doctor --json`、Native status 和专门的 Acceptance extraction smoke test 验证安装。

具体文件选择必须以 Comet 的项目级安装机制和现有生成资产为准，不能手工修改 `.comet/runtime/native/` 机器状态。若 Comet 的安装器会覆盖项目副本，则增加可重复的补丁应用脚本或固定安装后校验；补丁必须能在新机器上从记录的上游版本重建。

### 6.2 提取器行为修改

优先修改 acceptance source assembly，而不是删除或模糊化完整 Spec：

- 保留 brief parser `deriveBriefAcceptanceCriteria`；
- 保留显式 Scenario parser，以支持未来少量场景；
- 从默认 `mu()`/等价汇总路径移除 `deriveSpecMandatoryAcceptanceCriteria`；
- 若上游代码结构要求保留函数，令其仅作为显式开发诊断 API，不接入 Runtime Acceptance 集合；
- 增加单元测试：当前 M1 Spec 生成 9 项；增加一个普通段落/表格不会增加项；增加显式 Scenario 会增加 1 项；Non-goals、代码块和测试矩阵不会生成项；来源和文本排序稳定。

不修改 Acceptance ID 算法、分页协议、Verifier 全覆盖约束或状态 Schema，避免无关迁移。

### 6.3 版本与升级策略

- 固定项目使用的 Comet Runtime 版本和本地补丁版本，例如 `comet-native-runtime-patch: acceptance-source-v1`。
- `comet update` 后不得静默覆盖本地补丁；更新流程必须先显示版本漂移并要求重新应用/验证补丁。
- 运行时校验失败时停止恢复，不自动退回全局安装或另一套 Comet Workflow。
- 上游升级时重新运行 Acceptance extraction smoke test、现有 Native 状态读取测试和 M1 本地 Gate。

## 7. M1 执行编排

### 7.1 Native 正式 Loop

后续 M1 只保留以下正式流程：

```text
Shape（确认 9 项 Acceptance 与冻结 Identity）
  → 一次 Builder Build
  → Runtime 本地必要检查
  → 一次新的独立只读 Verifier
  → 若失败：按根因模块批量修复并回 Build
  → 若通过：Archive
```

以下不再作为 Native Loop 的平行流程产物：

- 每个小问题单独启动 Verifier；
- 外部 Classic/Superpowers 计划状态；
- 重复的完整 JSON Handoff；
- 把测试矩阵逐条复制到 Builder handoff；
- 仅因工具服务失败就修改产品设计。

TDD、代码审查和调试仍可作为 Builder 的内部工程方法，但不创建第二套阶段状态机；其结果归并到本地检查和一次 Builder handoff。

### 7.2 五个内部模块

| 模块 | 主要责任 | 交付边界 |
|---|---|---|
| Reference Validator | 所有引用 Root、符号链接、Secret、Hash、Leakage、Suite 直接引用 | A4 |
| Identity Projection | Skill/Suite/Manifest/Environment/Pair/Trial 的稳定投影和兼容性 | A2、A5 |
| Pair/Treatment Validator | Arm、Strategy、Mode/Population、非 Treatment 一致性 | A6 |
| Registry | CAS、幂等、原子写、索引恢复和读取 | A3 |
| CLI/Test Gate | 诊断分类、JSON/Text Envelope、退出码、完整测试矩阵和回归 | A1、A7、A8、A9 |

模块之间共享 M1 核心模型，按依赖顺序在一个 Build 候选中完成，不进行无真实并行价值的多 Agent 并行写入。

### 7.3 失败分类规则

每次 Runtime/Verifier 结果先归类：

1. **产品/契约缺口：** 影响用户可见结果或已确认 Identity，进入五模块批量修复；必要时回 Shape。
2. **证据缺口：** 行为已有实现但缺少测试或可观察证据，在 CLI/Test Gate 一次性补齐。
3. **Runtime 执行故障：** 权限拦截、上游服务不可用、进程中断或检查环境故障，不改变代码/契约；按 Runtime continuation 重试或等待用户。
4. **Verifier 判断缺失：** 只能由新的独立 Verifier 补齐，不由 Builder 自行写入通过结论。

只有第 1 类中真正改变用户可见行为的事项才返回 Shape；实现遗漏和证据缺口留在 Build。

## 8. Handoff 与上下文预算

Builder handoff 采用精简摘要：

```text
候选版本/迭代：...
本轮批量处理模块：Reference Validator, ...
覆盖的高层 Acceptance：A2, A3, ...
本地检查：命令 + 结果摘要
未运行检查：明确列出
已知限制：仅列真正剩余风险
```

禁止重复粘贴：

- 完整 Spec；
- 完整 A 项列表；
- 每轮所有历史 reason；
- 完整命令 stdout/stderr；
- 与本轮无关的 M0 证据。

Verifier 读取完整目标 Spec 和当前实现，但 Runtime 只分页传递 9 项 Acceptance；检查输出进入本机日志，handoff 只保留摘要。若某个 A 项需要细节，Verifier 按 Spec 来源和测试映射定向读取。

## 9. 迁移与兼容策略

当前 M1 Native change 已停在 Shape，历史 A1–A83 结果不作为新 Acceptance 集合继续使用。实施时：

1. 更新当前 change 的 brief/spec 和项目级 Runtime 副本；
2. 运行只读 Acceptance extraction smoke test，确认集合为 9 项；
3. 使用 Comet Runtime 的 Shape 重新对齐/确认，不手工修改 `comet-state.yaml`；
4. 新 Shape 确认后，历史 Verify 结果不继承为新目标的通过证据；
5. Build 时不重算已冻结的三项 Identity 语义；
6. 只有在新集合确认后才进入下一次 Build/Verify。

不尝试把 A10–A83 逐项迁移成新的 A 项。它们的语义被映射到完整 Spec 和覆盖矩阵；若发现某条是独立用户可见行为，才提升为 brief Acceptance 或显式 Scenario，并重新确认。

## 10. 风险与缓解

| 风险 | 缓解 |
|---|---|
| 减少 A 项导致遗漏细节 | 完整 Spec 不删；覆盖矩阵映射到 A 项；Verifier 定向检查完整 Spec |
| 本地 Runtime 副本被上游更新覆盖 | 固定版本、补丁说明、安装后校验和 smoke test |
| 绝对路径符号链接在别的机器失效 | 仓库内相对/真实文件副本，不依赖 `/Users/...` |
| Acceptance 过少导致 Verifier 过度概括 | 每个 A 项定义可观察结果、Spec 来源和测试矩阵；必要时显式 Scenario |
| 历史状态与新 Acceptance Hash 不兼容 | 在 Shape 重新对齐，不继承旧 Verify 结论 |
| 复杂的本地 Runtime 分叉增加维护成本 | 只改验收来源汇总，不改状态/分页/Verifier 协议；记录上游版本 |
| Runtime/Verifier 服务再次不可用 | 将执行错误与产品失败分离，避免无效 patch 和设计漂移 |

## 11. 设计验收

在进入实现计划前必须满足：

- [ ] 用户确认本设计和 9 项 M1 Runtime Acceptance；
- [ ] 明确项目级 Runtime 副本/补丁的实际文件边界；
- [ ] 明确当前 M1 change 以 Shape 重新对齐，不继承历史 A1–A83 验收结果；
- [ ] 明确 M1 Spec 与总 Manifest Contract 的版本边界；
- [ ] 明确一次 Gate + 一次独立 Verifier 的执行策略。

## 12. 参考资料

- Comet Native vs Classic（官方说明）：<https://docs.comet.rpamis.com/zh/native/native-vs-classic>（官方工作流指南）
- Comet Native 快速开始：<https://docs.comet.rpamis.com/zh/native/quickstart>（官方工作流指南）
- Comet Native Loop：<https://docs.comet.rpamis.com/zh/native/native-loop>（官方工程原理说明）
- 项目本地 Native Skill：`.pi/skills/comet-native/SKILL.md`
- 项目本地 M1 Brief：`docs/comet/changes/m1-registry-manifest-compiler/brief.md`
- 项目本地 M1 Spec：`docs/comet/changes/m1-registry-manifest-compiler/specs/registry-manifest-compiler/spec.md`

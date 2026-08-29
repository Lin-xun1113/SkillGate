# 编写 Eval Suite

> 状态：与 2026-08-27 代码库同步 · 校验权威来源：`internal/validation`、`internal/manifest`

Eval Suite 定义「Agent 要完成什么任务、怎么算通过」。它是配对实验的测量仪器——仪器不准，一切结论作废。

## 文件结构

```text
evals/my-suite/
├── suite.yaml              # Suite 定义
├── grader.yaml             # 评分器绑定（DeterministicGraderSet）
├── fixtures/               # 输入数据（带 Hash）
├── schemas/                # 输出 JSON Schema
├── expected/               # 标准答案（grader-only，Agent 不可见）
└── README.md               # 人工审查记录（推荐）
```

## Suite 顶层结构

```yaml
apiVersion: skillgate.dev/v1alpha1
kind: EvalSuite
metadata:
  name: csv-analysis-suite
  version: 1
  description: …
spec:
  split: tune                       # tune / holdout / holdback
  repetitions: 3                    # 每 Case 每臂重复次数
  defaults:
    timeoutSeconds: 60
    maxInputTokens: 10000
    maxOutputTokens: 5000
  skill:
    name: csv-analysis
    exposure:                       # 三种模式下的暴露方式
      forced_injection: forced      # 强制注入
      autonomous_trigger: discovery # 走正常发现路径
      security_probe: forced
  cases: [ … ]
```

## Case 全字段参考

| 字段 | 必填 | 说明 |
|---|---|---|
| `id` | ✅ | Suite 内唯一（重复 → `DUPLICATE_CASE_ID`） |
| `evaluationMode` | ✅ | `forced_injection` / `autonomous_trigger` / `security_probe`（非法 → `INVALID_EVALUATION_MODE`） |
| `population` | ✅ | `answer` / `trigger` |
| `type` | ✅ | `explicit` / `implicit` / `contextual` / `hard_negative` / `security_probe` |
| `prompt` | ✅ | 任务输入 |
| `polarity` | trigger Case 必填 | `should_trigger` / `should_not_trigger` |
| `fixtures` | 按需 | `path` + `sha256`；Agent 输入数据 |
| `agentVisible` | 推荐 | Agent 可见文件白名单（Trace 断言据此检查越界） |
| `outputs` | answer Case 必填 | 要求产出的 `path` + `mediaType` |
| `assertions` | ✅ | 机器验证断言（见下） |
| `expectedOutcome` | 推荐 | 人读预期，写进报告便于理解 |
| `security` | probe Case 必填 | `severityIfBypassed`、`categories`、`evidence` |

### 三种评估模式的设计意图

| 模式 | population | 回答的问题 | 暴露方式 |
|---|---|---|---|
| `forced_injection` | `answer` | Skill 内容对答案质量的贡献 | 强制注入，必然可见 |
| `autonomous_trigger` | `trigger` | Agent 会不会在对的时机发现并加载 Skill | 正常发现路径 |
| `security_probe` | `answer` | 面对对抗输入是否守住安全边界 | 强制注入 |

**不要混用**：Trigger Case 的通过/失败不进 Utility Lift 的分母；Security Case 不进任何 Utility 指标。这是 Release Gate 硬编码的行为。

### Case 类型（`type`）的建议搭配

| 类型 | 用途 | 建议数量 |
|---|---|---|
| `explicit` | 用户明确点名任务 | ≥2 |
| `implicit` | 用户描述需求但没点名格式 | ≥1 |
| `contextual` | 需要从上下文推断意图 | ≥1 |
| `hard_negative` | 明显无关任务（Trigger 负例） | ≥2 |
| `security_probe` | 对抗/注入 | ≥1 |

## 断言类型参考

### `file_exists` — 文件存在性

```yaml
- id: summary-exists
  type: file_exists
  path: output/summary.json
```

### `json_schema` — 输出结构

```yaml
- id: summary-schema
  type: json_schema
  path: output/summary.json
  schemaRef: schemas/monthly-summary.schema.json   # 相对 Suite 根
```

Schema 文件是标准 JSON Schema。它定义「合格输出的形状」——字段名、类型、必填项。

### `json_field` — 输出值正确性

```yaml
- id: summary-values
  type: json_field
  path: output/summary.json
  expectedRef: expected/monthly-summary.json       # 标准答案
  fields: [months, total]                          # 比对哪些字段（支持 a.b.c 路径）
```

标准答案文件放在 `expected/`。**它是 grader-only 的**：`grader.yaml` 的 `boundary.agentVisibleExpected: false` 保证 Agent 看不到它；Trace 断言 `agent_did_not_read:expected/` 再兜底一层。

### `trace_assertion` — 行为验证

对执行 Trace（LLM 调用、工具调用序列）断言，验证「过程行为」而不只是「最终产物」：

| 断言 | 含义 |
|---|---|
| `skill_loaded:csv-analysis` | Trace 显示加载了该 Skill |
| `skill_not_loaded:csv-analysis` | 未加载（Trigger 负例核心断言） |
| `reads_only_declared_fixture:fixtures/sales.csv` | 只读了声明的输入文件 |
| `no_read_under:fixtures/` | 没有读 fixtures 下任何文件 |
| `agent_did_not_read:expected/` | 没有偷看标准答案 |
| `tool_denied:network` | 联网工具被策略拒绝且 Agent 未绕过 |
| `no_read_paths:[.env,credentials.json,~/.ssh]` | 未读取凭据路径 |
| `paths_confined_to:/workspace` | 所有文件访问限制在工作区内 |

## fixtures 的 Hash 纪律

```yaml
fixtures:
  - path: fixtures/sales.csv
    sha256: sha256:79dfd4eaa7b72d9e558c68665b285ffd19f73349694f638ac7313d892a74c1d0
```

- 生成：`shasum -a 256 fixtures/sales.csv`；
- 内容不符 → 编译拒绝（`CONTENT_HASH_MISMATCH`）；
- 这保证了所有 Trial（两臂、所有重复）面对的输入逐字节相同——配对比较的前提。

## 防泄漏审查

除编译器的自动检查外，发布 Suite 前做一次人工审查（内置示例是 `LEAKAGE_REVIEW.md`，逐 Case 记录）：

1. `expected/` 里的每个值，是否都能从 Skill **之外**的通用知识推导？如果标准答案只有拿到答案才知道，它就不该出现在 Skill 或 Agent 可见路径里；
2. Prompt 里是否无意泄露了答案线索（「按月汇总」vs「3 月销售额是多少」）；
3. 正负例是否对称——负例（hard_negative）是否真的与 Skill 域无关；
4. `agentVisible` 是否最小化——Agent 只应看到完成任务必需的文件。

## grader.yaml

Suite 与评分器的绑定文件：

```yaml
apiVersion: skillgate.dev/v1alpha1
kind: DeterministicGraderSet
metadata:
  name: csv-analysis-grader
  version: 1
spec:
  mode: deterministic_only
  llmJudge: false                  # 本 Suite 不使用 LLM Judge
  suiteRef: ./suite.yaml
  assertionSemantics: suite-assertions-v1
  schemaRefs: [schemas/…, …]       # 枚举全部 Schema（Hash 进入 Grader 身份）
  expectedRefs: [expected/…, …]
  boundary:
    expectedRoot: /grader-only/expected
    agentVisibleExpected: false    # 标准答案对 Agent 不可见
```

评分执行细节见[评分体系](../evaluation/grading.md)。

## 编写质量检查单

- [ ] 至少 2 个 explicit + 1 个 implicit + 1 个 contextual（answer）
- [ ] 至少 2 个 hard_negative（trigger 负例）+ 1 个 should_trigger 正例
- [ ] 至少 1 个 security_probe
- [ ] 每个 output 都有 `file_exists` + `json_schema`（+ 值比对 `json_field`）
- [ ] 行为红线都有 `trace_assertion` 覆盖
- [ ] 所有 fixture 带 sha256，所有 schemaRef/expectedRef 文件存在
- [ ] 正负例对称、agentVisible 最小化
- [ ] 留了人工防泄漏审查记录

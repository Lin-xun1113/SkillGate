# 编写 Experiment Manifest

> 状态：与 2026-08-27 代码库同步 · 权威契约：`docs/contracts/experiment-manifest.md`

Experiment Manifest 把 Skill、Suite、Strategy、Grading、Policy 组装成一份可执行的实验计划。编译器（`skillgate compile`）会校验它并展开为 Trial 队列。

## 顶层结构

```yaml
apiVersion: skillgate.dev/v1alpha1
kind: Experiment
metadata:
  name: csv-analysis-v1-demo          # 全局唯一实验名
  labels:
    domain: data-analysis
    purpose: portfolio-demo
spec:
  suite: …            # Eval Suite 引用 + Hash
  skills: […]         # 被测 Skill 引用 + 版本 Hash
  strategies: […]     # 两臂（或多臂）的执行策略
  arms: […]           # 臂 → 策略映射
  pairing: { … }      # 配对声明
  repetitions: 3      # 每 Case 每臂重复次数
  execution: { … }    # 执行环境
  grading: { … }      # 评分配置
  policy: …           # Release Policy 引用 + Hash
  provenance: { … }   # 来源与审查元数据
```

## 逐段说明

### suite / skills — 带验证的引用

```yaml
suite: ./evals/csv-analysis/suite.yaml
suiteHash: sha256:b76002ab…
skills:
  - name: csv-analysis
    path: ./skills/csv-analysis
    version: sha256:5a2153ea…
```

- `Hash` 与文件实际内容不符 → `CONTENT_HASH_MISMATCH`；
- 引用路径越出工作区根 → `PATH_OUTSIDE_ROOT`；
- 更新 Suite 或 Skill 后记得同步更新 Hash（注册命令会输出新 Hash）。

### strategies — 执行策略

每个 Strategy 描述「这一臂的 Agent 怎么跑」：

```yaml
strategies:
  - name: baseline
    model:
      provider: fixture              # fixture / openai / anthropic / …
      name: deterministic-agent-v1
      config:
        temperature: 0
        max_tokens: 4096
        timeout_seconds: 60
        max_retries: 0
    skills: []                       # ← Baseline 不挂 Skill
    tools:
      allow: [filesystem.read, filesystem.write]
      deny: [network, credential.read, workspace.escape]
    budget:
      maxInputTokens: 10000
      maxOutputTokens: 5000
      timeoutSeconds: 60
      maxCostUsd: 0
    retry:
      maxAttempts: 2
      retryableCategories: [provider_transient, worker_lost]
    sandbox:
      profile: fixture-restricted-v1

  - name: candidate
    model: …                         # 与 baseline 完全一致
    skills:                          # ← 唯一差异
      - name: csv-analysis
        version: sha256:5a2153ea…
    tools: …                         # 完全一致
```

**配对身份规则**：`pairing.treatment` 声明的维度（默认 `skill_version`）是两臂唯一允许的差异。Compiler 逐字段比对两臂的 Model、Tools、Budget、Environment、Grader——任何其他差异触发：

```text
PAIR_IDENTITY_MISMATCH: baseline 与 candidate 的 model.name 不一致
```

常见错误示例：给 candidate 更高的 token 预算「以防万一」。这会破坏因果解释——Lift 可能来自更大预算而不是 Skill。

真实 Provider 的 API Key 与 Endpoint 不得写进 Manifest。Key 从
`OPENAI_API_KEY` / `ANTHROPIC_API_KEY` 读取；自定义 Endpoint 只能由部署者在
Worker 进程环境设置 `OPENAI_BASE_URL` / `ANTHROPIC_BASE_URL`。Manifest 内的
`base_url` / `endpoint` 会被 Worker 拒绝，这是避免不可信实验内容重定向
Credential 的安全边界。Provider SDK 的 `timeout_seconds` 与 `max_retries` 可以放在冻结的
`model.config` 中；`max_retries` 默认 `0`，系统级重试仍由 Scheduler 控制。
`model.config` 只接受 `temperature`（`0..2`）、`max_tokens`（`1..1000000`）、
`timeout_seconds`（`(0,600]`）和 `max_retries`（`0..30`）；未知字段、数字字符串、
`null`、重复兼容别名和顶层 Provider 参数会被 Compiler 与 Worker 拒绝。
完整字段和错误分类见 [`Provider Runtime Contract`](../../contracts/provider-runtime.md)。

### arms / pairing — 配对声明

```yaml
arms:
  - { name: without_skill, strategy: baseline }
  - { name: with_skill,   strategy: candidate }
pairing:
  treatment: skill_version
  baselineArm: without_skill
  candidateArm: with_skill
  identityExamples: ./evals/csv-analysis/identity-examples.json   # 可选：身份示例记录
```

- 缺 `without_skill` 臂 → `MISSING_REQUIRED_ARM`；
- 臂名重复 → `DUPLICATE_ARM`；
- 可选第三臂 `old_skill` 用于新旧版本同台对比。

### repetitions

```yaml
repetitions: 3
```

每个 Case × 每臂执行 3 次。Trial 总数 = Case 数 × 臂数 × repetitions（内置示例 8 × 2 × 3 = 48）。

重复次数的选择：

| repetitions | 适合 |
|---|---|
| 3 | 开发迭代、示例（默认） |
| 5–10 | 正式晋级评估，CI 更稳定 |
| 更多 | pass@k/pass^k 的 k 值需要 ≤ repetitions |

### execution — 执行环境

```yaml
execution:
  harness: langgraph
  harnessVersion: provisional-m0
  environment: docker://skillgate/case-csv:0.1.0
  environmentDescriptor: ./environments/case-csv-v1.json
  environmentHash: sha256:32626be4…
  timeoutSeconds: 60
  maxConcurrent: 2
```

Environment Identity = URI + Descriptor Hash：同一环境声明必须逐字节可复现，Hash 进入 Trial 身份链。Worker 只能执行与 Request Identity 一致的环境（架构不变量）。

### grading — 评分配置

```yaml
grading:
  ref: ./evals/csv-analysis/grader.yaml
  hash: sha256:b5f94acf…
  deterministic:                     # 必填：确定性评分器列表
    - id: suite-assertions-v1
      type: suite_assertions         # 执行 Suite 里的全部断言
      version: 1
      suiteRef: ./evals/csv-analysis/suite.yaml
    - id: output-schema-v1
      type: json_schema
      version: 1
      schemaRefs: [./evals/csv-analysis/schemas/…, …]
  llm:
    enabled: false                   # LLM Judge 开关
```

原则：**确定性验证覆盖不了的属性才开 LLM Judge**（且 `llm.enabled: true` 需要配套版本化 Prompt）。评分语义见[评分体系](../evaluation/grading.md)。

### policy — Release 策略引用

```yaml
policy: ./policies/conservative-release.yaml
policyHash: sha256:273c9099…
```

Policy 的编写见[编写 Release Policy](release-policy.md)。无 Policy 的实验可以正常完成（不产出 Decision）。

### provenance — 来源元数据

```yaml
provenance:
  source: hand-authored              # hand-authored / generated
  generatorModel: null
  generatorPromptHash: null
  leakageCheck: passed               # 人工泄漏审查结论
  humanReview: passed
```

要求记录「这套评测是谁出的、审过没有」——评估仪器本身要有出处。

## 编译与产物

```bash
skillgate compile experiments/my-exp.yaml
# compiled=true pairs=24 trials=48 manifest_hash=sha256:248b0c6d…
```

- `pairs`：配对数（Case 数 × 可比臂数）；
- `trials`：执行单元数；
- `manifest_hash`：整份 Manifest 的 Canonical Hash——物化（materialize）后数据库中的实验与此 Hash 绑定，Trial Request 的身份段由它派生。

Manifest 之后的修改不影响已物化实验；要改就 materialize 一个新实验。

## 编写质量检查单

- [ ] 两臂 Strategy 除 `skills` 外逐字一致
- [ ] 所有引用（suite/skill/grader/policy/environment）带当前正确的 Hash
- [ ] repetitions 与你打算报告的 pass@k 的 k 匹配
- [ ] `llm.enabled` 为 true 时有明确的版本化 Prompt 方案
- [ ] provenance 的审查字段如实填写
- [ ] `compile` 输出 `diagnostics=0`

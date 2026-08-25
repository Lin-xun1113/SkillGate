# Experiment Manifest Contract

**状态：** `ACCEPTED` 作为 M0/M1 v0 基线；实现细节仍需由 M1 Compiler Contract Test 固化  
**用途：** 描述一次可复现的 Skill/Strategy Evaluation Experiment

## 1. 设计目标

Manifest 是 Experiment 的输入 Contract，而不是运行时脚本。它必须做到：

- 可以被 Go Control Plane 校验和编译；
- 不包含任何 Secret；
- 能够表达 Baseline/Candidate、旧版本和 Ablation；
- 能够明确 `forced_injection`、`autonomous_trigger` 与 `security_probe` 三种评估模式，并分别标记 Trigger/Answer Population；
- 能够生成稳定的 Identity Tuple 和 Pair ID；
- 能够在执行开始后冻结。

## 2. 顶层结构

```yaml
apiVersion: skillgate.dev/v1alpha1
kind: Experiment
metadata:
  name: csv-analysis-v2-eval
  labels:
    domain: data-analysis
spec:
  suite: ./evals/csv-analysis/suite.yaml
  skills:
    - ./skills/csv-analysis
  strategies:
    - name: baseline
      model: ...
      skills: []
    - name: candidate
      model: ...
      skills:
        - name: csv-analysis
          version: sha256:...
  arms:
    - name: without_skill
      strategy: baseline
    - name: with_skill
      strategy: candidate
  repetitions: 3
  execution:
    harness: langgraph
    environment: docker://skillgate/case-csv:0.1.0
    timeoutSeconds: 180
    maxConcurrent: 4
  grading:
    deterministic: ./graders/deterministic.yaml
    llm:
      enabled: false
  policy: ./policies/default-release.yaml
```

## 3. 顶层字段

| 字段 | 必填 | 规则 |
|---|---|---|
| `apiVersion` | 是 | 当前为 `skillgate.dev/v1alpha1` |
| `kind` | 是 | 必须为 `Experiment` |
| `metadata.name` | 是 | 小写 kebab-case，长度受限，作为展示名而非唯一事实来源 |
| `spec.suite` | 是 | Suite 文件或已注册 Suite 的引用 |
| `spec.skills` | 是 | 本 Experiment 允许使用的 Skill Source 列表，可为空但通常不应为空 |
| `spec.strategies` | 是 | 至少包含 Baseline 和 Candidate Strategy |
| `spec.arms` | 是 | 至少两个 Arm，且必须声明 Treatment 差异 |
| `spec.repetitions` | 是 | MVP 范围 1–30；Release Claim 默认至少 3 |
| `spec.execution` | 是 | Harness、Environment、Timeout 和并发限制 |
| `spec.grading` | 是 | Deterministic/LLM Grader 配置 |
| `spec.policy` | 否 | Release Policy 引用；缺失时只能输出无 Policy 的报告 |

## 4. Skill Source 规则

Skill Source 支持两种形式：

```yaml
# 本地目录，仅适合尚未注册的 DRAFT Experiment
- path: ./skills/csv-analysis

# 已注册的不可变版本，适合编译和执行
- name: csv-analysis
  version: sha256:abc...
```

进入 `COMPILED` 前必须解析为不可变版本。运行期间不允许直接读取一个可变目录的当前内容。

Skill Package 必须至少包含合法的 `SKILL.md`。`name` 和 `description` 的格式约束遵循 [Agent Skills Specification](https://agentskills.io/specification)。

## 5. Strategy 规则

Strategy 至少包含：

```yaml
name: candidate
model:
  provider: openai
  name: gpt-example
  config:
    temperature: 0
skills:
  - name: csv-analysis
    version: sha256:abc...
tools:
  allow:
    - filesystem.read
    - python.execute
  deny:
    - network
budget:
  maxInputTokens: 100000
  maxOutputTokens: 20000
  timeoutSeconds: 180
  maxCostUsd: 0.10
retry:
  maxAttempts: 2
  retryableCategories: [provider_transient, worker_lost]
sandbox:
  profile: docker-restricted-v1
```

Strategy 中禁止出现：

- API Key、Token 或密码；
- 任意 Shell Script；
- 未声明的 Endpoint；
- 能修改 Experiment Identity 的运行时变量。

## 6. Suite 与 Case 规则

每个 Case 必须有稳定 ID、`evaluationMode` 和 `population`：

```yaml
evaluationMode: forced_injection # forced_injection | autonomous_trigger | security_probe
population: answer                 # answer | trigger
```

模式与 Population 的合法组合为：

| `evaluationMode` | 合法 `population` | 统计用途 |
|---|---|---|
| `forced_injection` | `answer` | 比较 Skill Content 的 Deterministic Utility |
| `autonomous_trigger` | `trigger` | 只统计 Skill Load 的 Recall/Specificity |
| `security_probe` | `answer` | 产生独立 Security Finding，进入 Hard Gate |

`security_probe` 不进入 Utility Lift 或 Trigger Metric 的分母；Trigger 证据必须来自结构化 Load Event、Tool/File Evidence，不能从最终答案提到 Skill 名称推断。

示例 Case：

```yaml
cases:
  - id: csv-explicit-001
    evaluationMode: forced_injection
    population: answer
    type: explicit
    prompt: "使用 csv-analysis 生成销售汇总。"
    fixtures:
      - path: files/sales.csv
        sha256: sha256:...
    assertions:
      - id: output-json-valid
        type: json_schema
        schema: schemas/summary.json

  - id: csv-negative-001
    evaluationMode: autonomous_trigger
    population: trigger
    polarity: should_not_trigger
    prompt: "解释 Go channel 的基本用法。"
```

`trigger` Population 和 `answer` Population 不能在同一个 Ablation 中混用。Trigger Case 关注是否加载 Skill；Answer Case 关注加载后是否完成任务。

## 7. Arm 规则

标准 Arm：

| Arm | 含义 |
|---|---|
| `without_skill` | 匹配 Baseline，不挂载 Candidate Skill |
| `with_skill` | 挂载 Candidate Skill |
| `old_skill` | 挂载待比较的旧 Skill Version |
| `ablation:<id>` | 挂载真实删除组件后的 Skill Tree |

每个 Pair 的唯一 Treatment Difference 必须明确写入：

```yaml
pairing:
  treatment: skill_version
  baselineArm: without_skill
  candidateArm: with_skill
```

如果比较 Model、Harness、Environment 或 Grader，不能继续称为 Skill Lift，必须创建新的 Experiment 或明确使用不同的 Treatment 类型。

## 8. 编译校验

Compiler 必须拒绝以下情况：

1. Case ID 重复；
2. Arm 名称重复或缺少 Baseline/Candidate；
3. 两个配对 Arm 的 Prompt、Fixture、Model、Harness、Environment、Grader 或 Repetition 不一致；
4. Skill Version 解析后 Hash 变化；
5. `trigger` 与 `answer` Population 被错误混合；
6. 把 Expected Output 或 Answer Key 放入 Agent-visible Context；
7. Budget 为负数或超过系统上限；
8. Retry Policy 允许无限重试；
9. Grader 引用不存在或未冻结的 Prompt/Script；
10. Policy 引用与本 Experiment 不兼容；
11. `security_probe` 被错误计入 Utility 或 Trigger 聚合；
12. Security Case 没有声明 Network/Credential/Workspace 的预期 Deny Evidence。

## 9. Pair ID 与 Trial ID

M0/M1 v0 采用以下规范化输入（字段名和 canonical JSON 结构必须保持一致）：

```text
pair_identity = canonical_json({
  experiment_hash,
  suite_hash,
  case_id,
  evaluation_mode,
  repetition,
  treatment,
  baseline_arm,
  candidate_arm,
  model_hash,
  harness_hash,
  environment_hash,
  fixture_hash,
  grader_hash,
  tool_policy_hash
})
pair_id = sha256(pair_identity)
trial_identity = canonical_json({pair_id, arm, attempt})
trial_id = sha256(trial_identity)
```

`attempt` 只表示同一 Logical Trial 的至少一次执行尝试，不能改变 `pair_id`。M2 将 Logical Trial Identity 明确定义为：

```text
logical_trial_id = sha256(canonical_json({pair_id, arm}))
trial_id = sha256(canonical_json({pair_id, arm, attempt}))
result_idempotency_key = sha256(canonical_json({trial_id, result_manifest_hash}))
```

M1 只生成 Attempt 1 的 `trial_id`；M2 物化时新增稳定 Logical Trial 记录，Retry 创建新的 Attempt，并保证一个 Logical Trial 只有一个最终 Result。M0 只生成 `identity-examples.json` 的离线契约 Fixture 和校验，不声称已实现 Runtime Materialization 或 Result Commit。
实际 ID 可以使用可读前缀加 Hash，例如：

```text
pair_01J...
trial_01J...
```

Hash 输入必须经过 Canonical JSON 序列化，不能依赖 YAML 文本的空格或字段顺序。

## 10. 冻结规则

- `DRAFT` 可以修改；
- `VALIDATING` 只能由 Compiler 生成诊断；
- `COMPILED` 后保存 Manifest Hash；
- `QUEUED` 或更晚状态禁止修改 Suite、Strategy、Skill、Grader 和 Policy；
- 需要修改时创建新的 Experiment；
- Cancellation 只更新控制字段，不改变 Treatment Identity。

## 11. 评估来源状态

自动生成的 Suite 必须携带：

```yaml
provenance:
  source: generated_from_skill
  generatorModel: ...
  generatorPromptHash: sha256:...
  leakageCheck: passed
  humanReview: pending
```

`humanReview: pending` 的 Suite 可以用于开发 Smoke，但不能支撑 `PROMOTE`。

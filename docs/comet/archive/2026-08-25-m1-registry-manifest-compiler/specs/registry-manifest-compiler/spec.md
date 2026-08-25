# M1 Registry 与 Experiment Compiler 规格

**Capability：** `registry-manifest-compiler`
**版本：** `0.1.0`
**状态：** PROVISIONAL，M1 Shape 确认中；设计决策已由用户重新确认
**时间基准：** UTC

> **说明：** 本 spec 是完整的规范性规格说明。Runtime 验收项（A 项）仅来自 brief.md 的 Acceptance examples，本 spec 中的段落、列表、表格和测试矩阵不自动成为独立验收项。测试矩阵通过覆盖映射关联到 A 项的子条件。

## 1. 目标与边界

M1 将已归档 M0 的实验输入变成第一个可运行的 Go Control Plane 核心纵向切片。它提供内容寻址 Registry、Manifest 语义校验、配对 Identity 和 Trial Plan 编译，但不执行 Trial。

M1 的输入是不可信的：Skill 文件、Suite YAML、Prompt、Fixture 内容、Expected Output、Policy 文本和所有用户提供的路径都必须经过验证。M1 不读取 Provider Credential，不访问网络，不启动 Model、Worker 或 Sandbox。

## 2. 模块与接口

Go Module 使用：

```text
github.com/Lin-xun1113/SkillGate
```

首批目录和责任：

```text
cmd/skillgate/              CLI 入口、退出码和 JSON/文本渲染
internal/identity/          Canonical JSON、规范化路径、Package/Manifest Hash、Pair/Trial ID
internal/registry/          Registry 接口、文件系统 CAS 实现、幂等注册/读取
internal/manifest/          YAML 解析、引用解析、归一化 Manifest 模型
internal/validation/        稳定诊断 Code、路径/Secret/Leakage/Pair 校验
internal/experiment/        Arm 配对、Pair Plan、Trial Row 展开
```

包之间通过窄接口依赖。`internal/registry` 的文件系统实现不能泄漏到 `internal/experiment`；Compiler 只依赖 Registry/Identity/Validation 接口。

## 3. Canonical Identity

### 3.1 Skill Package

M1 不创建 TAR/ZIP。给定一个 Package Root：

1. 使用 `filepath.WalkDir` 收集普通文件；拒绝 Root 外路径、绝对引用、`..`、非法 UTF-8 相对路径和无法安全解析的 Symbolic Link；
2. 相对路径统一为 `/`，按 UTF-8 字节序升序排列；
3. 文本文件按 UTF-8 读取并将 CRLF/CR 统一为 LF；二进制文件保留原始 Bytes 的 Base64 表示；`SKILL.md` Frontmatter 作为不透明文本参与 Package Identity，不重新排序或重写字段。
4. 生成版本化 Canonical JSON：

```json
{
  "format": "skillgate.skill-package.v1",
  "files": [
    {"path": "SKILL.md", "encoding": "utf8", "content": "..."}
  ]
}
```

5. Canonical JSON 使用递归字典序 Key、无空白、UTF-8 编码；`skill_hash = sha256(canonical_json_bytes)`。

Package 必须包含且只允许一个根级 `SKILL.md`；M1 校验最低 Frontmatter：`name`、`description`，并要求 `name` 为规范 kebab-case、`description` 非空且不含 Secret。M1 不执行 Skill Script。

### 3.2 Suite 与 Manifest

Suite Hash 使用解析后的 YAML 数据模型转为同一 Canonical JSON，而不是 YAML 文本空白。Manifest Hash 使用归一化引用和已解析内容 Hash；不得包含机器绝对路径、临时目录或 Secret。Suite 引用采用 Declared Hash 语义：对声明了 Hash 的 Fixture/Schema/Expected/Security 引用校验其内容，Pair 保持引用声明列表 Identity，不将未声明的引用内容闭包重新注入 M0 Pair Identity。

Manifest Hash 的投影移除显示用 `metadata.name` 之外的可变运行字段，并将：

- `spec.suite` 替换为 `suite_hash`；
- `execution.environment` 与 Descriptor Hash 组合后替换为 `environment_hash`，原始 URI 不写入 Manifest Projection；
- Skill `path` 替换为 `skill_version`；
- Grader、Policy、Environment Descriptor 替换为其内容 Hash；Execution Environment URI 与 Descriptor Hash 组合成单一 Environment Identity 后参与 Manifest/Pair Identity；
- Strategy/Arm/Pair/Repetition 保留为 Identity 输入。

同一逻辑内容的 YAML 字段顺序、注释和换行变化必须产生相同 Hash；文件引用解析按 Manifest 所在目录优先，并允许受 Project Root 约束的冻结 M0 项目根相对引用回退。

### 3.3 Pair 与 Trial Identity

M1 只支持 `without_skill`/`with_skill` 双臂。每个 `(case, repetition)` 生成一个 Pair：

```text
pair_identity = canonical_json({
  experiment_hash,
  suite_hash,
  case_id,
  evaluation_mode,
  repetition,
  treatment: "skill_version",
  baseline_arm: "without_skill",
  candidate_arm: "with_skill",
  model_hash,
  harness_hash,
  environment_hash,
  fixture_hash,
  grader_hash,
  tool_policy_hash
})
pair_id = sha256(pair_identity)
trial_id = sha256(canonical_json({pair_id, arm, attempt: 1}))
```

`attempt` 不改变 Pair/Logical Trial Identity。M1 只生成 Attempt=1 的计划行；Result Idempotency Key 和 Commit 由 M2 实现。

## 4. File System CAS Registry

默认 Registry Root：`.skillgate/registry`；可由 CLI `--registry-root` 覆盖。该目录只保存合成/开发数据，生产事实来源仍为 PostgreSQL。

布局：

```text
<root>/
  skills/<skill-name>/<sha256>/canonical.json
  skills/<skill-name>/<sha256>/metadata.json
  suites/<suite-name>/<sha256>/canonical.json
  suites/<suite-name>/<sha256>/metadata.json
  index.json
```

- Content Blob 以 Hash 路径保存，写入采用临时文件 + `fsync` + 原子 Rename；目标已存在且内容相同则返回既有 Version；目标存在但内容不同不得覆盖。
- `index.json` 只用于人类发现，不是 Content Identity 来源；损坏或缺失时可从 Hash 目录重建。
- Registry API：`RegisterSkill`、`GetSkill`、`RegisterSuite`、`GetSuite`、`ListVersions`。
- 注册请求的重复 `(logical_name, content_hash)` 必须幂等返回同一 Version；同名不同 Hash 创建新 Version。
- M1 不实现删除、覆盖、远程推送或数据库迁移。

## 5. Manifest Compiler

### 5.1 输入

M1 必须能读取 M0 示例的以下结构：

- `apiVersion: skillgate.dev/v1alpha1`、`kind: Experiment`；
- local Suite/Skill/Grader/Policy/Environment references；
- 两个 Strategy、两个 Arm、`skill_version` Pairing；
- `repetitions >= 1`，M0 Release Claim 使用 3；
- Execution Harness/Environment/Timeout；
- Deterministic Grader 和 `llm.enabled: false`。

### 5.2 编译输出

成功输出 `CompiledExperiment`：

```json
{
  "manifest_hash": "sha256:...",
  "suite_hash": "sha256:...",
  "pairing": {"treatment": "skill_version", "baseline_arm": "without_skill", "candidate_arm": "with_skill"},
  "pair_count": 24,
  "trial_count": 48,
  "pairs": [
    {
      "pair_id": "sha256:...",
      "case_id": "csv-explicit-001",
      "repetition": 1,
      "trials": [
        {"trial_id": "sha256:...", "arm": "without_skill", "attempt": 1},
        {"trial_id": "sha256:...", "arm": "with_skill", "attempt": 1}
      ]
    }
  ],
  "diagnostics": []
}
```

Pair Plan 必须是确定性排序：Case ID UTF-8 字节序、repetition 升序、Arm 顺序 `without_skill` 后 `with_skill`。不得写入数据库或启动执行。

### 5.3 配对不变量

Compiler 必须比较 Baseline/Candidate 的非 Treatment 投影：Model、Prompt/Case、Fixture Hash、Harness、Environment Hash、Tool Policy、Budget、Retry、Grader Hash 和 Repetition。任何差异都返回 `PAIR_IDENTITY_MISMATCH`，并且不返回部分 Compiled Plan。

Compiler 必须将 `security_probe` 保持为独立 Case；不得将其计入 Utility 或 Trigger Aggregation 配置。Trigger/Answer 非法组合返回 `INVALID_EVALUATION_MODE`。

## 6. 稳定诊断 Contract

诊断结构：

```json
{
  "code": "PAIR_IDENTITY_MISMATCH",
  "severity": "error",
  "path": "spec.strategies.candidate.model",
  "message": "Baseline 与 Candidate 的非 Treatment Identity 不一致。",
  "details": {"baseline": "...", "candidate": "..."}
}
```

M1 最低 Code：

| Code | 含义 | 建议退出码 |
|---|---|---:|
| `INVALID_ARGUMENT` | CLI 参数或输入格式错误 | 2 |
| `FILE_NOT_FOUND` | 引用文件不存在 | 3 |
| `PATH_OUTSIDE_ROOT` | 引用越出 Manifest/Package Root | 4 |
| `SECRET_FIELD_PRESENT` | 配置包含 Secret/凭据字段或明显值 | 4 |
| `INVALID_SKILL_MANIFEST` | SKILL.md 缺失或 Frontmatter 非法 | 5 |
| `DUPLICATE_CASE_ID` | Suite Case ID 重复 | 5 |
| `DUPLICATE_ARM` | Arm 名称重复 | 5 |
| `MISSING_REQUIRED_ARM` | 缺少标准 Baseline/Candidate Arm | 5 |
| `INVALID_EVALUATION_MODE` | Mode/Population 组合非法 | 5 |
| `PAIR_IDENTITY_MISMATCH` | 配对非 Treatment Identity 不一致 | 6 |
| `LEAKAGE_DETECTED` | Expected/Answer Key 进入 Agent-visible Context | 6 |
| `CONTENT_HASH_MISMATCH` | 声明 Hash 与内容不一致 | 6 |
| `UNSUPPORTED_TREATMENT` | M1 不支持 old_skill/ablation 或其他 Treatment | 5 |
| `REGISTRY_CONFLICT` | 同一 Hash 路径已有不同内容 | 7 |
| `IO_ERROR` | Registry/文件系统不可用 | 8 |

同一输入的诊断 Code、Path、Severity 和 JSON 字段顺序必须稳定；人类 Message 可以包含本地化描述，但 Code 不可变。

## 7. CLI Contract

```text
skillgate skill validate <path> [--json]
skillgate skill register <path> [--registry-root <path>] [--json]
skillgate suite validate <path> [--json]
skillgate suite register <path> [--registry-root <path>] [--json]
skillgate compile <manifest> [--registry-root <path>] [--json]
```

- 无参数或未知命令返回 `INVALID_ARGUMENT`；
- 成功返回 0；诊断错误按表中退出码；
- `--json` 输出单个 JSON Document，不把日志混入 stdout；人类模式输出摘要和逐条诊断；
- CLI 不读取环境中的 Provider Key，不发起网络请求；
- 注册命令输出 `logical_name`、`version_hash`、`canonical_size_bytes`、`registry_path` 和 `idempotent`。

## 8. 验收项映射

以下映射将 brief.md 的 A1–A9 高层验收关联到本 spec 的具体条款和测试覆盖。

| A 项 | 关联条款 | 测试覆盖 |
|---|---|---|
| A1：可构建，go test 通过 | 模块与接口 | 构建测试、go vet |
| A2：Canonical Identity | 3.1 Skill Package | Canonical JSON 测试、换行/Unicode/路径测试 |
| A3：Skill Registry | 4. FS CAS Registry | CAS 幂等、索引恢复、冲突测试 |
| A4：Suite Registry | 4. FS CAS Registry | 引用校验、Secret/Leakage/Expected 隔离测试 |
| A5：Compiler 编译 M0 示例 | 5. Manifest Compiler | Golden Test、24 Pair/48 Trial |
| A6：非法输入错误 | 5.3 配对不变量、6. 诊断 | Pair Mutation、Arm Mapping、错误码测试 |
| A7：CLI 输出 | 7. CLI Contract | JSON/Text Golden、退出码测试 |
| A8：无外部依赖 | — | 离线边界、无 Credential、M0 回归 |
| A9：文档同步 | — | 文档契约、ADR、状态同步 |

## 9. M1 测试与 Gate

必须有：

- Canonical JSON/Package Hash 的表格、换行、Unicode、二进制和重复表示测试；
- 路径穿越、绝对路径、Symbolic Link、Secret 和缺少 SKILL.md 的负向测试；
- 文件 CAS 重复注册、不同版本、重启后读取和冲突测试；
- M0 Manifest Golden Compile Test，断言 `pair_count=24`、`trial_count=48` 和稳定 IDs；
- Environment/Fixture/Model/Grader/Tool Policy 逐项 Mutation Test，断言 `PAIR_IDENTITY_MISMATCH`；Candidate Skill 必须与顶层唯一冻结 Skill Version 完全一致；
- Duplicate Case/Arm、Missing Arm、Mode/Population、Expected Leakage、Hash mismatch、old_skill/ablation 负向测试；
- CLI JSON/Text Golden Test、退出码和无外部网络/Secret 测试；JSON 成功/失败必须包含 `version`、`ok` 和 `diagnostics` Envelope；
- `go test ./...`、`go vet ./...`、`go run ./cmd/skillgate --help`。

M1 完成只证明 Registry/Compiler Contract 和本地确定性编译，不证明 Worker 执行、数据库可靠性或真实 Skill Utility。

# Native Acceptance Layering Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将 SkillGate 的 Comet Native 验收来源改为显式、可持久化的高层 Acceptance，保留 M1 完整契约和测试覆盖，并以一次批量 Build/一次独立 Verify 恢复 M1。

**Architecture:** 保留 Comet Native 的 Shape → Build → Verify → Archive 状态机，只修改项目固定 Native 资产中的 Acceptance source assembly，使 brief 顶层 Acceptance 和显式 `Scenario:` 成为 Runtime 验收来源，Spec 普通段落/列表/表格不再自动生成 `spec-must`。项目通过可追踪的 Native 资产副本、版本/补丁清单和 smoke test 固定运行时行为；M1 文档通过高层 Acceptance、完整 Normative Spec 与测试覆盖矩阵三层映射保持完整性。

**Tech Stack:** Node.js 22+、Comet `@rpamis/comet 0.4.0-beta.18` Native Runtime、Markdown/JavaScript、Go 1.x、Go `testing`、现有 `gopkg.in/yaml.v3`、Shell/Node 验证脚本。

**Spec:** `docs/superpowers/specs/2026-08-20-native-acceptance-layering-design.md`

## Global Constraints

- 保留 Native，不切换 Classic；Native Runtime 是唯一正式流程控制器。
- Runtime Acceptance 默认只来自 `brief.md` 的 `# Acceptance examples` 顶层列表和显式 `Scenario:`；Spec 普通段落、列表、表格和测试矩阵不得默认生成 `spec-must` Acceptance。
- M1 Runtime Acceptance 固定为 9 个高层、可观察、互不重复的结果；必要时不得超过 12 项，新增项必须回 Shape 确认。
- 完整 M1 Spec、错误码、失败语义、Identity 约束和测试矩阵必须保留，并通过 Acceptance→Spec→Test 映射追踪。
- M1 Identity 决策冻结为：Environment Identity = URI + Descriptor Hash；Skill Package = 原始文本并仅规范换行；Suite 引用 = Declared Hash。
- 不修改 `.comet/runtime/native/` 机器状态，不手工编辑 `comet-state.yaml` 或 `verification.md`；状态推进只能使用 `comet native` Runtime 命令。
- 不依赖 `/Users/...` 绝对路径符号链接；项目资产必须能在另一台机器重建。
- 描述性文档使用中文；时间使用 UTC；临时行为明确标记 `PROVISIONAL`。
- 不实现 M2 PostgreSQL Queue/Lease/Retry/Result Commit，不声称 Exactly-once Execution。
- 每项实现先写失败测试，再写最小实现；每个任务结束运行对应验证并提交独立 commit。

---

## 文件与责任总览

| 文件/目录 | 责任 |
|---|---|
| `.pi/skills/comet-native/` | Pi 入口；从绝对路径链接改为项目可追踪的 Native 资产副本或项目固定入口 |
| `.comet/skills/skills/comet-native/` | 当前 Comet 项目安装副本；不得继续作为仅本机、被忽略的唯一事实来源 |
| `tools/comet-native/` | 项目维护的 Runtime 版本/补丁元数据、复制/校验/应用脚本和 Acceptance smoke test |
| `.comet/skills/skills/comet-native/scripts/comet-native-*.mjs` | 运行时脚本；只调整 Acceptance source assembly，保留状态/分页/Verifier 协议 |
| `.pi/skills/comet-native/SKILL.md` | Native 执行约束；补充显式 Acceptance 来源、一次 Gate/一次 Verifier 和不叠加正式流程规则 |
| `docs/comet/changes/m1-registry-manifest-compiler/brief.md` | 9 项高层 M1 Acceptance 和 Shape 决策 |
| `docs/comet/changes/m1-registry-manifest-compiler/specs/registry-manifest-compiler/spec.md` | 完整 Normative Spec、失败 Contract、测试覆盖矩阵 |
| `docs/contracts/experiment-manifest.md` | 总体 Contract 与 M1 支持/延期兼容矩阵 |
| `docs/implementation/implementation-plan.md` | 从“尚未编码”的历史描述更新为 M1 实际状态和 Gate |
| `README.md`、`PROJECT_STATUS.md`、`docs/decisions/ADR-008-m1-registry-manifest-compiler.md` | 状态、边界、Runtime 版本和 M1 结果同步 |
| `package.json` | 增加不依赖外部 Credential/网络的 Native smoke/asset 校验脚本 |
| `tools/comet-native/tests/acceptance-source.test.mjs` | 提取规则回归测试：9 项、普通文本不膨胀、Scenario 可显式增加 |
| `tools/comet-native/tests/asset-integrity.test.mjs` | 资产版本、补丁标记、入口路径和非绝对链接检查 |

---

### Task 1: 固定 Comet 版本与项目资产清单

**Files:**
- Create: `tools/comet-native/manifest.json`
- Create: `tools/comet-native/README.md`
- Create: `tools/comet-native/check-assets.mjs`
- Modify: `package.json`
- Test: `tools/comet-native/tests/asset-integrity.test.mjs`

**Interfaces:**
- Produces `tools/comet-native/manifest.json` with fields:
  - `schema: "skillgate.comet-native-assets.v1"`
  - `cometVersion: "0.4.0-beta.18"`
  - `acceptancePolicy: "brief-plus-explicit-scenarios-v1"`
  - `sourceRoot: "@rpamis/comet/assets/skills-zh/comet-native"`
  - `trackedFiles: string[]`
  - `patchedFiles: string[]`
  - `patchVersion: "acceptance-source-v1"`
- Produces `node tools/comet-native/check-assets.mjs`, exit 0 only when tracked files exist, no relevant `.pi/skills/comet-native` symlink points outside the repository, and every patched file contains the expected patch marker.

- [ ] **Step 1: Write the failing asset contract test**

Create `tools/comet-native/tests/asset-integrity.test.mjs` using Node’s built-in `node:test` and `node:assert/strict`:

```js
import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';

const root = path.resolve(import.meta.dirname, '../../..');
const manifestPath = path.join(root, 'tools/comet-native/manifest.json');

test('asset manifest pins the supported Native runtime and policy', () => {
  const manifest = JSON.parse(fs.readFileSync(manifestPath, 'utf8'));
  assert.equal(manifest.schema, 'skillgate.comet-native-assets.v1');
  assert.equal(manifest.cometVersion, '0.4.0-beta.18');
  assert.equal(manifest.acceptancePolicy, 'brief-plus-explicit-scenarios-v1');
  assert.equal(manifest.patchVersion, 'acceptance-source-v1');
  assert.ok(manifest.trackedFiles.includes('scripts/comet-native-runtime.mjs'));
  assert.ok(manifest.patchedFiles.includes('scripts/comet-native-runtime.mjs'));
});

test('project Pi entry does not depend on an absolute native skill link', () => {
  const entry = path.join(root, '.pi/skills/comet-native');
  assert.equal(fs.lstatSync(entry).isSymbolicLink(), false);
});
```

- [ ] **Step 2: Run the test and verify it fails**

Run:

```bash
node --test tools/comet-native/tests/asset-integrity.test.mjs
```

Expected: FAIL because the asset manifest and real project-local files do not yet exist and `.pi/skills/comet-native` is currently an absolute symlink.

- [ ] **Step 3: Add the pinned manifest and checker contract**

Create `tools/comet-native/manifest.json` with the exact schema above. List the five Native reference/entry files and all twelve Runtime command scripts currently installed by Comet, using paths relative to the Native skill root. Mark the seven scripts that contain acceptance assembly (`archive`, `doctor`, `next`, `runtime`, `select`, `spec`, `status`) as patched.

Implement `check-assets.mjs` to:

1. parse the manifest with `JSON.parse`;
2. resolve `tools/comet-native/vendor/comet-native/` as the project-local asset root;
3. assert each `trackedFiles` path is a regular file inside that root;
4. assert each `patchedFiles` file contains `skillgate:acceptance-source-v1`;
5. assert `.pi/skills/comet-native` is a real directory;
6. print a short JSON result with `version`, `assetRoot`, `trackedCount`, and `patchedCount`.

Do not inspect or modify `.comet/runtime/native/` in this checker.

- [ ] **Step 4: Add the package script and run the focused test**

Modify `package.json`:

```json
{
  "scripts": {
    "check:comet-native": "node tools/comet-native/check-assets.mjs",
    "test:comet-native": "node --test tools/comet-native/tests/*.test.mjs"
  }
}
```

Run:

```bash
npm run test:comet-native
```

Expected: the test remains red only for the not-yet-vendored files; do not mark the task complete until Task 2 supplies them.

- [ ] **Step 5: Commit the manifest/checker groundwork**

```bash
git add tools/comet-native package.json
git commit -m "test: pin project native runtime assets"
```

---

### Task 2: Vendor the minimum Native asset set without absolute links

**Files:**
- Create: `tools/comet-native/vendor/comet-native/` files listed in `manifest.json`
- Modify: `.pi/skills/comet-native` (replace symlink with real directory)
- Modify: `.comet/skills/skills/comet-native` only if required by the project installation contract
- Test: `tools/comet-native/tests/asset-integrity.test.mjs`

**Interfaces:**
- Produces a repository-local Native asset root with the same relative layout expected by the Pi Skill.
- Preserves the public Native command filenames and the existing `comet native` argument contract.
- Does not claim that Comet’s globally installed npm package has been modified.

- [ ] **Step 1: Capture the exact upstream asset set**

From the installed `@rpamis/comet@0.4.0-beta.18` package, copy only the files listed in `manifest.json` into `tools/comet-native/vendor/comet-native/`. Verify source and destination SHA-256 values before applying any patch:

```bash
node - <<'NODE'
import fs from 'node:fs';
import crypto from 'node:crypto';
import path from 'node:path';
const root = process.cwd();
const source = '/Users/linxun/.nvm/versions/node/v24.10.0/lib/node_modules/@rpamis/comet/assets/skills-zh/comet-native';
const target = path.join(root, 'tools/comet-native/vendor/comet-native');
for (const relative of JSON.parse(fs.readFileSync('tools/comet-native/manifest.json')).trackedFiles) {
  const src = path.join(source, relative);
  const dst = path.join(target, relative);
  fs.mkdirSync(path.dirname(dst), { recursive: true });
  fs.copyFileSync(src, dst);
  const hash = file => crypto.createHash('sha256').update(fs.readFileSync(file)).digest('hex');
  console.log(`${relative}\n  source=${hash(src)}\n  target=${hash(dst)}`);
}
NODE
```

If the package path differs on another machine, use `npm root -g` and package metadata to resolve it; do not write the absolute path into project files.

- [ ] **Step 2: Replace the absolute Pi symlink with a project-local copy**

Remove `.pi/skills/comet-native` only after preserving its target content, then copy the vendored Native Skill/Reference/Script tree into `.pi/skills/comet-native`. Keep the tracked Pi entry self-contained and use regular files, not a symlink:

```bash
rm .pi/skills/comet-native
cp -R tools/comet-native/vendor/comet-native .pi/skills/comet-native
```

The implementation must not copy generated `.comet/runtime/native/` state or change `docs/comet/changes/*/comet-state.yaml`.

- [ ] **Step 3: Run the asset integrity tests**

Run:

```bash
npm run test:comet-native
npm run check:comet-native
comet doctor . --json
```

Expected: asset tests pass; Doctor reports project Pi installation or the documented fixed project asset state without following an external absolute link. If `comet doctor` insists on overwriting the local copy, stop and update the installer/maintenance step rather than accepting silent overwrite.

- [ ] **Step 4: Commit the vendored baseline before patching**

```bash
git add .pi/skills/comet-native tools/comet-native/vendor
 git commit -m "build: vendor project native skill assets"
```

---

### Task 3: Add acceptance extraction regression tests before changing behavior

**Files:**
- Create: `tools/comet-native/tests/acceptance-source.test.mjs`
- Create: `tools/comet-native/acceptance-fixtures/m1-brief.md`
- Create: `tools/comet-native/acceptance-fixtures/m1-spec.md`
- Modify: `package.json` only if the test command needs an explicit fixture path

**Interfaces:**
- Tests the vendored Runtime’s public/available extraction behavior without importing private machine state.
- Produces a deterministic expected acceptance projection:
  - M1 brief only: 9 criteria;
  - M1 brief + current Spec: still 9 criteria after the policy patch;
  - one explicit `## Scenario: ...`: 10 criteria;
  - ordinary paragraphs, lists, tables, code fences, Non-goals and test matrix: no additional criteria.

- [ ] **Step 1: Write the failing extraction tests**

Use a subprocess or a small test-only module import to load the vendored extraction functions. The test must not depend on `comet-state.yaml`. A subprocess fallback is acceptable if the bundled file is not importable as a module:

```js
import test from 'node:test';
import assert from 'node:assert/strict';
import { deriveBriefAcceptanceCriteria, deriveSpecAcceptanceCriteria, deriveSpecMandatoryAcceptanceCriteria } from '../../tools/comet-native/vendor/comet-native/scripts/native-acceptance.mjs';
```

If the bundled single-file Runtime does not expose those functions, first create `tools/comet-native/test-support/extract-acceptance.mjs` by extracting only the pure parser section from the pinned source, and document that it is test support—not the production Runtime. The tests must assert:

```js
assert.equal(deriveBriefAcceptanceCriteria(brief).length, 9);
assert.equal(assembleAcceptance(brief, spec).length, 9);
assert.equal(assembleAcceptance(brief, `${spec}\n## Scenario: User-visible extra\nThe result is observable.`).length, 10);
```

- [ ] **Step 2: Run the focused test and verify the policy failure**

Run:

```bash
node --test tools/comet-native/tests/acceptance-source.test.mjs
```

Expected: FAIL before the production assembly change because the current behavior includes ordinary Spec requirements and produces more than 9 criteria.

- [ ] **Step 3: Add explicit fixture assertions**

Ensure the fixture contains all false-positive classes from the incident:

```markdown
# Normative Details
A prose requirement must not become a Runtime Acceptance.

- A list detail must remain a Spec detail.

| Code | Meaning |
| --- | --- |
| X | Table detail |

## Non-goals
- This text must be excluded.

## Test Matrix
- Mutation test detail must be mapped, not independently numbered.

```yaml
ordinary: code
```
```

Add an explicit `## Scenario:` block only in the positive test fixture. Assert criteria IDs and normalized text are stable across LF/CRLF input.

- [ ] **Step 4: Commit the red regression tests**

```bash
git add tools/comet-native/tests/acceptance-source.test.mjs tools/comet-native/acceptance-fixtures
 git commit -m "test: define explicit native acceptance sources"
```

---

### Task 4: Change the vendored Runtime to stop implicit Spec expansion

**Files:**
- Modify: `tools/comet-native/vendor/comet-native/scripts/comet-native-runtime.mjs`
- Modify: `tools/comet-native/vendor/comet-native/scripts/comet-native-archive.mjs`
- Modify: `tools/comet-native/vendor/comet-native/scripts/comet-native-doctor.mjs`
- Modify: `tools/comet-native/vendor/comet-native/scripts/comet-native-next.mjs`
- Modify: `tools/comet-native/vendor/comet-native/scripts/comet-native-select.mjs`
- Modify: `tools/comet-native/vendor/comet-native/scripts/comet-native-spec.mjs`
- Modify: `tools/comet-native/vendor/comet-native/scripts/comet-native-status.mjs`
- Modify: `.pi/skills/comet-native/scripts/comet-native-*.mjs` corresponding to the vendored production scripts
- Test: `tools/comet-native/tests/acceptance-source.test.mjs`
- Modify: `tools/comet-native/manifest.json`

**Interfaces:**
- Keeps existing acceptance parser functions and IDs intact.
- Changes only the assembly function so `deriveSpecMandatoryAcceptanceCriteria` is not appended to the Runtime contract.
- Keeps explicit `deriveSpecAcceptanceCriteria` (`Scenario:`) available.
- Keeps `acceptanceHash`, pagination, verification status, and state schemas unchanged.

- [ ] **Step 1: Add a production patch marker assertion**

Extend `asset-integrity.test.mjs`:

```js
for (const relative of manifest.patchedFiles) {
  const source = fs.readFileSync(path.join(assetRoot, relative), 'utf8');
  assert.match(source, /skillgate:acceptance-source-v1/);
  assert.doesNotMatch(source, /acceptance\.push\(\.\.\.deriveSpecMandatoryAcceptanceCriteria/);
}
```

- [ ] **Step 2: Patch only the acceptance assembly call site**

In each generated/bundled production script, locate the assembly sequence equivalent to:

```js
acceptance.push(...deriveSpecAcceptanceCriteria(...));
acceptance.push(...deriveSpecMandatoryAcceptanceCriteria(...));
```

Replace it with:

```js
// skillgate:acceptance-source-v1
// Runtime Acceptance is explicit: brief examples plus user-confirmed Scenario blocks.
acceptance.push(...deriveSpecAcceptanceCriteria(
  markdown,
  snapshot.source,
  NATIVE_CONTRACT_LIMITS.maxAcceptanceCriteria - acceptance.length,
));
```

Remove the `deriveSpecMandatoryAcceptanceCriteria` import only when it is no longer referenced elsewhere in the same bundle. Do not alter the parser implementation, ID hashing, sorting, limits, or state transitions.

Because Comet distributes bundled single-file scripts rather than the readable `dist/domains` modules, apply the same minimal replacement to each tracked command asset that contains the copied contract assembly. Keep a patch script or exact replacement manifest so a future upstream update can be reapplied deterministically; do not edit the globally installed npm file as the source of truth.

- [ ] **Step 3: Mirror the patched files into the Pi production entry**

Copy the patched vendored scripts into `.pi/skills/comet-native/scripts/` using the same relative names. Verify the two copies have identical SHA-256 hashes. Keep `tools/comet-native/vendor` as the canonical source and document the mirror relationship in `tools/comet-native/README.md`.

- [ ] **Step 4: Run the extraction and asset tests**

Run:

```bash
npm run test:comet-native
npm run check:comet-native
```

Expected: all asset and extraction tests pass; M1 brief plus full Spec produces exactly 9 Runtime Acceptance criteria; explicit Scenario adds exactly one.

- [ ] **Step 5: Run a real Native status projection without advancing state**

Run:

```bash
comet native status m1-registry-manifest-compiler --details --json
```

Expected: command succeeds or reports the existing Shape continuation; it must not mutate the change or report a new Build/Verify transition. If the installed global `comet` command bypasses the vendored Pi asset, record that as an integration failure and complete Task 5 before proceeding.

- [ ] **Step 6: Commit the Runtime policy patch**

```bash
git add tools/comet-native/vendor .pi/skills/comet-native tools/comet-native/manifest.json
 git commit -m "fix: make native acceptance sources explicit"
```

---

### Task 5: Make project execution use the pinned Native assets

**Files:**
- Create: `tools/comet-native/run-native.mjs`
- Create: `tools/comet-native/apply-patch.mjs`
- Create: `tools/comet-native/update-assets.mjs`
- Modify: `tools/comet-native/README.md`
- Modify: `.pi/extensions/comet-commands.ts` only if the Pi command needs to route explicitly
- Test: `tools/comet-native/tests/asset-integrity.test.mjs`
- Test: `tools/comet-native/tests/native-command-smoke.test.mjs`

**Interfaces:**
- `node tools/comet-native/run-native.mjs <native-command> ...` invokes the project-pinned Native command script with the existing command arguments and project root.
- `node tools/comet-native/apply-patch.mjs --check|--apply` verifies/applies the exact upstream-version patch without network or credentials.
- `node tools/comet-native/update-assets.mjs --check` reports version drift and refuses silent overwrite.

- [ ] **Step 1: Write command routing tests**

Create `native-command-smoke.test.mjs`:

```js
import test from 'node:test';
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';

const run = (...args) => spawnSync(process.execPath, ['tools/comet-native/run-native.mjs', ...args], {
  cwd: process.cwd(), encoding: 'utf8',
});

test('pinned native status uses the project asset and remains read-only', () => {
  const result = run('status', 'm1-registry-manifest-compiler', '--details', '--json');
  assert.equal(result.error, undefined);
  assert.equal(result.status, 0);
  assert.match(result.stdout, /m1-registry-manifest-compiler/);
});
```

- [ ] **Step 2: Run the routing test and verify it fails or detects bypass**

Run:

```bash
node --test tools/comet-native/tests/native-command-smoke.test.mjs
```

Expected: fail until the project-pinned runner is implemented and the Pi command uses it for Native operations.

- [ ] **Step 3: Implement the pinned runner**

`run-native.mjs` must:

1. reject empty or unsafe command names;
2. map `status`, `next`, `show`, `select`, `archive`, `doctor`, `new`, `spec`, `root`, and `init` to the corresponding regular file under `tools/comet-native/vendor/comet-native/scripts/`;
3. set `process.argv` to the same shape expected by the copied script;
4. preserve stdout/stderr and exit code;
5. never invoke a global script by fallback;
6. pass `--project-root <repo>` when the caller did not supply it;
7. return a clear error if the asset manifest/check fails.

- [ ] **Step 4: Implement deterministic patch/update checks**

`apply-patch.mjs` records exact source and target hashes and applies only the expected text replacement for Comet `0.4.0-beta.18`; it must fail closed if the source context occurs zero or multiple times. `update-assets.mjs --check` compares the installed package version and the manifest version, prints `version-drift`, and exits nonzero without changing project files when they differ. Neither script runs network commands.

- [ ] **Step 5: Route Pi’s Comet command to the pinned runner**

Update `.pi/extensions/comet-commands.ts` only if necessary so `/comet-native` sends a project-local command invocation or an explicit skill argument that causes the vendored scripts to run. Do not route Classic commands through the Native runner. Preserve `/comet`, `/comet-classic`, and other command names.

If the Pi host cannot execute a project runner directly from the extension, update `.pi/skills/comet-native/SKILL.md` with the exact project runner command and require it for every Runtime continuation; document that the globally installed `comet native` is a compatibility check, not the source of truth.

- [ ] **Step 6: Run all routing and health checks**

Run:

```bash
npm run test:comet-native
npm run check:comet-native
node tools/comet-native/run-native.mjs status m1-registry-manifest-compiler --details --json
comet doctor . --json
```

Expected: project runner reports the same Shape state with the 9-item acceptance projection; no `.comet/runtime/native/` state is modified by read-only commands.

- [ ] **Step 7: Commit the pinned execution path**

```bash
git add tools/comet-native .pi/extensions/comet-commands.ts .pi/skills/comet-native/SKILL.md
 git commit -m "build: route native workflow through pinned assets"
```

---

### Task 6: Update Native Skill instructions and M1 documentation taxonomy

**Files:**
- Modify: `.pi/skills/comet-native/SKILL.md`
- Modify: `.pi/skills/comet-native/reference/artifacts.md`
- Modify: `docs/comet/changes/m1-registry-manifest-compiler/brief.md`
- Modify: `docs/comet/changes/m1-registry-manifest-compiler/specs/registry-manifest-compiler/spec.md`
- Modify: `docs/contracts/experiment-manifest.md`
- Modify: `docs/decisions/ADR-008-m1-registry-manifest-compiler.md`
- Modify: `README.md`
- Modify: `PROJECT_STATUS.md`
- Modify: `docs/00-project-brief.md`
- Modify: `docs/implementation/implementation-plan.md`
- Create: `docs/implementation/m1-test-plan.md`
- Test: `tools/comet-native/tests/documentation-contract.test.mjs`

**Interfaces:**
- Brief’s `# Acceptance examples` contains exactly the 9 high-level M1 Acceptance items and no test-matrix subitems.
- Spec remains complete and explicitly labels its testing section as evidence/coverage, not Runtime Acceptance.
- Compatibility matrix states M1 supports only `without_skill`/`with_skill` + `skill_version`; total Manifest Contract’s `old_skill`/`ablation` remain future capability.

- [ ] **Step 1: Write documentation contract tests**

Create `documentation-contract.test.mjs`:

```js
import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';

const brief = fs.readFileSync('docs/comet/changes/m1-registry-manifest-compiler/brief.md', 'utf8');
const spec = fs.readFileSync('docs/comet/changes/m1-registry-manifest-compiler/specs/registry-manifest-compiler/spec.md', 'utf8');

test('M1 brief exposes exactly nine top-level acceptance items', () => {
  const section = brief.split('# Acceptance examples')[1].split('\n# ')[0];
  const items = section.match(/^\- A\d+：/gmu) ?? [];
  assert.deepEqual(items.map((item) => item.match(/A\d+/u)[0]), ['A1','A2','A3','A4','A5','A6','A7','A8','A9']);
});

test('M1 spec keeps test matrix separate from runtime acceptance', () => {
  assert.match(spec, /测试与证据矩阵|Test.*Matrix|覆盖矩阵/u);
  assert.match(spec, /不.*Runtime Acceptance|不.*独立.*验收/u);
});
```

- [ ] **Step 2: Run documentation tests and verify the current taxonomy failure**

Run:

```bash
node --test tools/comet-native/tests/documentation-contract.test.mjs
```

Expected: fail until the brief/spec and project status documents are updated.

- [ ] **Step 3: Rewrite the brief Acceptance section without deleting detailed requirements**

Keep `# Outcome`, `# Scope`, `# Non-goals`, `# Constraints and invariants`, `# Decisions`, `# Open questions`, and `# Verification expectations`. Replace only `# Acceptance examples` with the nine behavior-level items from the approved design. Move test-specific details to `Verification expectations` or the new `docs/implementation/m1-test-plan.md`.

Update `Decisions` to state:

```markdown
- Runtime Acceptance 来源固定为 brief 顶层 Acceptance 与显式 Scenario；完整 Spec 普通段落、列表、表格和测试矩阵不自动生成 Runtime Acceptance。
- M1 Runtime Acceptance 固定为 A1–A9；细节通过 Acceptance→Spec→Test 覆盖映射验证。
```

- [ ] **Step 4: Reclassify the complete Spec**

Retain all normative details from the current Spec, but add explicit labels and a final coverage table. Do not change the three frozen Identity semantics. In the test section, state that each row is a development/evidence requirement mapped to A1–A9, not an independent Runtime Acceptance.

- [ ] **Step 5: Add M1 test plan and compatibility matrix**

Create `docs/implementation/m1-test-plan.md` with one row per A1–A9, each row listing:

- observable result;
- exact Spec sections;
- Go test packages/files;
- negative/mutation cases;
- Runtime command checks;
- evidence expected from the independent Verifier.

Update `docs/contracts/experiment-manifest.md` with a version boundary table:

| Capability | M1 | Later |
|---|---|---|
| `without_skill` / `with_skill` + `skill_version` | supported | supported |
| `old_skill` | rejected as `UNSUPPORTED_TREATMENT` | future |
| `ablation` | rejected as `UNSUPPORTED_TREATMENT` | future |

- [ ] **Step 6: Synchronize project status documents**

Update README, PROJECT_STATUS, project brief, implementation plan, and ADR-008 to distinguish:

- M0 archived;
- M1 code exists but is not yet archived;
- M1 Native Shape is being realigned to 9 Acceptance items;
- the Comet Runtime policy is project-pinned at `0.4.0-beta.18` with `acceptance-source-v1` patch;
- M1 does not prove Worker execution, PostgreSQL reliability, Sandbox, or Skill Utility.

Do not claim M1 passed before a new independent Verifier accepts it.

- [ ] **Step 7: Run documentation checks and commit**

Run:

```bash
node --test tools/comet-native/tests/documentation-contract.test.mjs
npm run test:comet-native
```

Expected: PASS.

```bash
git add .pi/skills/comet-native/SKILL.md .pi/skills/comet-native/reference/artifacts.md \
  docs/comet/changes/m1-registry-manifest-compiler docs/contracts/experiment-manifest.md \
  docs/decisions/ADR-008-m1-registry-manifest-compiler.md README.md PROJECT_STATUS.md \
  docs/00-project-brief.md docs/implementation/implementation-plan.md docs/implementation/m1-test-plan.md \
  tools/comet-native/tests/documentation-contract.test.mjs
git commit -m "docs: separate native acceptance from m1 specification"
```

---

### Task 7: Validate the five M1 implementation modules in one local Gate

**Files:**
- Modify: `tools/comet-native/tests/native-command-smoke.test.mjs` if command assertions need expansion
- Create: `tools/comet-native/run-m1-gate.mjs`
- Modify: `package.json`
- Modify: `PROJECT_STATUS.md` only after all commands pass

**Interfaces:**
- `node tools/comet-native/run-m1-gate.mjs` executes the fixed local Gate and returns nonzero on the first failed command.
- Gate order is deterministic and output is concise; full command logs remain in the invoking terminal/Runtime log.

- [ ] **Step 1: Write the Gate script test**

Create a test that stubs no product behavior and checks the command list/order exposed by the Gate module:

```js
import test from 'node:test';
import assert from 'node:assert/strict';
import { gateCommands } from '../run-m1-gate.mjs';

test('M1 Gate has one deterministic command plan', () => {
  assert.deepEqual(gateCommands.map(({ name }) => name), [
    'go-test', 'go-vet', 'cli-help', 'm0-compile', 'm0-regression',
    'native-asset-tests', 'native-extraction-tests', 'documentation-tests',
  ]);
});
```

- [ ] **Step 2: Implement the fixed Gate**

Use `child_process.spawnSync` with `shell: false` and these exact checks:

```text
go test ./...
go vet ./...
go run ./cmd/skillgate --help
M0 compile command producing pair_count=24 and trial_count=48
npm run validate:m0
npm run test:comet-native
node --test tools/comet-native/tests/documentation-contract.test.mjs
```

The M0 compile check must parse JSON and assert `ok=true` (or the current documented success envelope), `pair_count=24`, and `trial_count=48`; it must not compare a mutable new Manifest Hash to an old hash unless the current contract explicitly requires it. The Gate must report command name, exit code, and a bounded stdout/stderr preview.

- [ ] **Step 3: Run the Gate and fix only grouped module defects**

Run:

```bash
node tools/comet-native/run-m1-gate.mjs
```

If it fails, group failures under the five approved modules before editing:

1. Reference Validator;
2. Identity Projection;
3. Pair/Treatment Validator;
4. Registry;
5. CLI/Test Gate.

Do not start a separate Verifier or change Identity semantics for an isolated test failure. Add/modify tests in the same grouped repair batch.

- [ ] **Step 4: Record the successful local Gate**

After all commands return 0, update `PROJECT_STATUS.md` with the UTC timestamp, exact Gate command, and summary only. Do not mark Native Verify passed or archive M1 from this task.

- [ ] **Step 5: Commit the Gate and status evidence**

```bash
git add tools/comet-native/run-m1-gate.mjs package.json PROJECT_STATUS.md
 git commit -m "test: add deterministic m1 local gate"
```

---

### Task 8: Re-align Native Shape through Runtime and prepare one Builder handoff

**Files:**
- Modify: `docs/comet/changes/m1-registry-manifest-compiler/brief.md` and Spec only if Runtime re-alignment reports a concrete mismatch
- Runtime-owned: `docs/comet/changes/m1-registry-manifest-compiler/comet-state.yaml`, `.comet/runtime/native/`
- Test: `tools/comet-native/tests/native-command-smoke.test.mjs`

**Interfaces:**
- Uses only public `comet native` continuation commands.
- Does not hand-edit Runtime-owned YAML/state.
- Produces a Shape state whose acceptance total is exactly 9 and whose next action is the Runtime-provided Shape confirmation.

- [ ] **Step 1: Run the project-pinned read-only extraction and status check**

```bash
node tools/comet-native/run-native.mjs status m1-registry-manifest-compiler --details --json
comet native status m1-registry-manifest-compiler --details --json
```

Compare acceptance sources and totals. If global Comet still reports A10+, do not proceed; fix the command routing/asset source and rerun Task 5 checks.

- [ ] **Step 2: Inspect the Runtime continuation, not guessed commands**

```bash
comet native status m1-registry-manifest-compiler --details --json
```

Use only the returned `continuation.commandArgs`, `inputOptions`, and `disposition`. Do not manually edit `comet-state.yaml` or run an undocumented transition.

- [ ] **Step 3: Confirm the approved Shape once**

When Runtime reports `phase=shape`, `next_action=confirm-shape`, and exactly 9 Acceptance items, execute the exact continuation with a concise summary and `--confirmed`. The summary must state:

```text
已确认 M1 采用 9 项高层 Runtime Acceptance；完整 Spec 与测试矩阵保留为规范/证据映射；Native Runtime 使用项目固定的 acceptance-source-v1 规则；Environment URI+Descriptor、Skill raw text+LF、Suite Declared Hash 已冻结；M1 仍只支持双臂 skill_version，M2 及 Worker/DB/Sandbox/UI 为非目标。
```

- [ ] **Step 4: Read the new Build continuation and stop at its boundary**

After the command, query status again. Do not start implementation until Runtime says Build and supplies its current continuation. This task ends with Shape confirmed and a clean implementation boundary.

- [ ] **Step 5: Commit only user-owned formal artifact edits, if any**

Runtime owns state changes. Commit only brief/spec changes already accepted by Runtime’s guard; never add `.comet/runtime/native/` logs or manually staged state.

---

### Task 9: One batch M1 Build, one local Gate, one independent Verifier

**Files:**
- Modify: `internal/validation/**` (Reference Validator)
- Modify: `internal/identity/**`, `internal/manifest/**` (Identity Projection)
- Modify: `internal/experiment/**`, `internal/manifest/**` (Pair/Treatment Validator)
- Modify: `internal/registry/**` (Registry)
- Modify: `cmd/skillgate/**`, tests across `internal/**` (CLI/Test Gate)
- Modify: `docs/decisions/ADR-008-m1-registry-manifest-compiler.md`, `README.md`, `PROJECT_STATUS.md` only when implementation changes the accepted contract

**Interfaces:**
- Preserve the existing package boundaries and narrow interfaces.
- Keep M0 frozen hashes compatible where the M1 contract requires it.
- Produce no partial compiled plan on semantic failure.
- Keep CLI JSON envelope and human output stable.

- [ ] **Step 1: Build a single grouped repair inventory from the last Verify history**

Use the current historical findings only as a batch inventory:

```text
Reference Validator: all direct Suite/Schema/Expected/Security/Grader references, root and intermediate symlink checks, Secret/Leakage scan.
Identity Projection: URI+Descriptor Environment, raw Skill text+LF, Declared Hash closure boundary, path-free Manifest projection.
Pair/Treatment Validator: exact Arm→Strategy mapping, frozen Candidate Skill, full non-Treatment projection and mutation cases.
Registry: Suite validation through the same boundary, metadata JSON, atomic exclusive writes, index rebuild/recovery.
CLI/Test Gate: versioned success/failure envelopes, text summaries, error-to-exit-code mapping, Golden/negative/mutation/restart tests.
```

- [ ] **Step 2: Write/extend grouped failing tests before implementation**

Add tests in the existing package test files for every item in the inventory. At minimum include:

```text
intermediate symlink escape
all direct Suite reference kinds
referenced Secret and Expected leakage
Manifest path/metadata projection
Environment URI and Descriptor mutation
Candidate Skill mismatch and Arm mapping
Suite index deletion/rebuild and tampered canonical conflict
CLI success/error JSON version, human text, exit-code subprocess cases
fixed M0 Pair/Trial ID Golden values
```

Run the smallest affected test package after each test group and record the expected failure.

- [ ] **Step 3: Implement all grouped repairs in dependency order**

1. Reference Validator and safe path reader;
2. Identity projection and Environment/Skill/Suite hash semantics;
3. Pair/Treatment invariant enforcement;
4. Registry atomic/recovery behavior;
5. CLI envelope/error classification and test fixtures.

Do not change the three frozen Identity decisions. Do not dispatch a Verifier during this step.

- [ ] **Step 4: Run the single deterministic local Gate**

```bash
node tools/comet-native/run-m1-gate.mjs
```

Expected: all eight Gate commands pass. If not, keep the task in Build and repair the grouped failure; do not submit a partial handoff.

- [ ] **Step 5: Submit one concise Builder handoff through Runtime**

Use the exact `continuation.inputOptions` template returned by Runtime. Address only `A1`–`A9` and list the five grouped modules, command checks, known limits, and no duplicated full Spec/JSON. Runtime must accept the handoff before Verify.

- [ ] **Step 6: Dispatch exactly one fresh independent Verifier**

Use the Runtime-provided `dispatch-verifier` continuation and the same local Gate checks. Launch one new read-only Verifier execution. Do not run ad hoc reviewer loops in parallel with it.

- [ ] **Step 7: Classify the result before any repair**

If the Verifier fails, classify every finding as:

- product/contract gap;
- evidence/test gap;
- Runtime infrastructure failure;
- Verifier unavailable/blocked.

Batch all product/evidence gaps by the five modules, then return to Build using the Runtime continuation. Only user-visible contract changes return to Shape. If all A1–A9 pass and Runtime allows Archive, stop and request the final archive boundary rather than starting another review.

---

## Execution Order and Checkpoints

1. Tasks 1–2: asset manifest and vendored baseline; checkpoint: no absolute Native asset link.
2. Task 3: red extraction tests; checkpoint: test demonstrates current over-expansion.
3. Task 4: production extraction patch; checkpoint: exactly 9 Acceptance items from M1 brief/spec.
4. Task 5: pinned project execution path; checkpoint: read-only status uses patched assets.
5. Task 6: documentation taxonomy and compatibility matrix; checkpoint: documentation tests pass.
6. Task 7: one local Gate; checkpoint: Go/M0/Native/docs checks all pass.
7. Task 8: Runtime Shape re-alignment; checkpoint: Runtime-owned state reports 9 Acceptance items and Build continuation.
8. Task 9: one grouped M1 Build and one independent Verify; checkpoint: Archive-ready or grouped repair returned by Runtime.

## Plan Self-Review

- **Spec coverage:** Runtime source policy is covered by Tasks 1–5; M1 Acceptance/spec/test taxonomy by Task 6; deterministic Gate by Task 7; state-safe Shape recovery by Task 8; five-module implementation and one Verify loop by Task 9.
- **Placeholder scan:** No `TBD`, `TODO`, or unspecified “add appropriate handling” steps are used. Every task names files, commands, expected outcomes, and commit boundaries.
- **Type/interface consistency:** `manifest.json`, `check-assets.mjs`, `run-native.mjs`, `apply-patch.mjs`, `update-assets.mjs`, and Gate/test file names are consistent across tasks. Runtime-owned state is never treated as a project-owned implementation interface.
- **Known integration constraint:** The installed Comet CLI’s fast router resolves Native assets package-relatively, so project-pinned execution must be verified explicitly. If the Pi host cannot route to the project runner, Task 5 must stop and document the limitation rather than silently relying on the global Runtime.

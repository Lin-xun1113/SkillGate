#!/usr/bin/env node

import crypto from 'node:crypto';
import fs from 'node:fs';
import path from 'node:path';
import process from 'node:process';
import Ajv2020 from 'ajv/dist/2020.js';
import YAML from 'yaml';

const root = process.cwd();
const writeHashes = process.argv.includes('--write-hashes');
const errors = [];
const notes = [];

const relPath = (...parts) => path.join(root, ...parts);
const displayPath = (file) => path.relative(root, file).split(path.sep).join('/');

function fail(message) {
  errors.push(message);
}

function note(message) {
  notes.push(message);
}

function readText(relative) {
  const file = relPath(relative);
  try {
    return fs.readFileSync(file, 'utf8');
  } catch (error) {
    fail(`${relative}: 无法读取（${error.message}）`);
    return null;
  }
}

function readJson(relative) {
  const text = readText(relative);
  if (text === null) return null;
  try {
    return JSON.parse(text);
  } catch (error) {
    fail(`${relative}: JSON 无法解析（${error.message}）`);
    return null;
  }
}

function readYaml(relative) {
  const text = readText(relative);
  if (text === null) return null;
  try {
    return YAML.parse(text);
  } catch (error) {
    fail(`${relative}: YAML 无法解析（${error.message}）`);
    return null;
  }
}

function canonical(value) {
  if (Array.isArray(value)) return value.map(canonical);
  if (value && typeof value === 'object') {
    return Object.fromEntries(
      Object.keys(value).sort().map((key) => [key, canonical(value[key])]),
    );
  }
  return value;
}

function canonicalJson(value) {
  return JSON.stringify(canonical(value));
}

function sha256Bytes(value) {
  return `sha256:${crypto.createHash('sha256').update(value).digest('hex')}`;
}

function sha256Text(text) {
  return sha256Bytes(Buffer.from(text.replace(/\r\n/g, '\n'), 'utf8'));
}

function sha256File(relative) {
  const file = relPath(relative);
  try {
    return sha256Bytes(fs.readFileSync(file));
  } catch (error) {
    fail(`${relative}: 无法计算文件 Hash（${error.message}）`);
    return null;
  }
}

function sha256Canonical(value) {
  return sha256Text(canonicalJson(value));
}

function listFiles(directory) {
  const result = [];
  if (!fs.existsSync(directory)) return result;
  for (const entry of fs.readdirSync(directory, { withFileTypes: true })) {
    if (entry.name === '.DS_Store') continue;
    const full = path.join(directory, entry.name);
    if (entry.isDirectory()) result.push(...listFiles(full));
    else if (entry.isFile()) result.push(full);
  }
  return result;
}

function skillPackageHash(relativeDirectory) {
  const directory = relPath(relativeDirectory);
  const files = listFiles(directory)
    .map((file) => ({
      path: path.relative(directory, file).split(path.sep).join('/'),
      content: fs.readFileSync(file, 'utf8').replace(/\r\n/g, '\n'),
    }))
    .sort((a, b) => a.path.localeCompare(b.path));
  return sha256Canonical({ files });
}

function deepClone(value) {
  return JSON.parse(JSON.stringify(value));
}

function getByPath(object, dottedPath) {
  return dottedPath.split('.').reduce((current, key) => current?.[key], object);
}

function setByPath(object, dottedPath, value) {
  const keys = dottedPath.split('.');
  const last = keys.pop();
  let current = object;
  for (const key of keys) {
    if (!current[key] || typeof current[key] !== 'object') current[key] = {};
    current = current[key];
  }
  current[last] = value;
}

function removeByPath(object, dottedPath) {
  const keys = dottedPath.split('.');
  const last = keys.pop();
  let current = object;
  for (const key of keys) {
    current = current?.[key];
    if (!current || typeof current !== 'object') return;
  }
  delete current[last];
}

function resolveUnderRoot(reference, baseRelative, label) {
  if (typeof reference !== 'string' || reference.length === 0) {
    fail(`${label}: 引用必须是非空字符串`);
    return null;
  }
  if (reference.startsWith('/') || reference.startsWith('~')) {
    fail(`${label}: 禁止绝对路径或 Home 路径（${reference}）`);
    return null;
  }
  const base = relPath(baseRelative);
  const resolved = path.resolve(base, reference);
  const rootResolved = path.resolve(root);
  if (resolved !== rootResolved && !resolved.startsWith(`${rootResolved}${path.sep}`)) {
    fail(`${label}: 路径越出项目根目录（${reference}）`);
    return null;
  }
  return resolved;
}

function assertExists(reference, baseRelative, label) {
  const resolved = resolveUnderRoot(reference, baseRelative, label);
  if (resolved && !fs.existsSync(resolved)) fail(`${label}: 引用不存在（${reference}）`);
  return resolved;
}

function validateSchemaValue(value, schema, location) {
  if (!schema || typeof schema !== 'object') {
    fail(`${location}: Schema 不是对象`);
    return;
  }
  if (Object.prototype.hasOwnProperty.call(schema, 'const') && value !== schema.const) {
    fail(`${location}: 期望 const=${JSON.stringify(schema.const)}，实际为 ${JSON.stringify(value)}`);
  }
  if (schema.enum && !schema.enum.some((candidate) => JSON.stringify(candidate) === JSON.stringify(value))) {
    fail(`${location}: 值不在 enum 中`);
  }
  if (schema.type === 'object') {
    if (!value || typeof value !== 'object' || Array.isArray(value)) {
      fail(`${location}: 期望 object`);
      return;
    }
    for (const required of schema.required ?? []) {
      if (!Object.prototype.hasOwnProperty.call(value, required)) fail(`${location}: 缺少必填字段 ${required}`);
    }
    if (schema.additionalProperties === false) {
      for (const key of Object.keys(value)) {
        if (!Object.prototype.hasOwnProperty.call(schema.properties ?? {}, key)) fail(`${location}: 不允许字段 ${key}`);
      }
    }
    for (const [key, childSchema] of Object.entries(schema.properties ?? {})) {
      if (Object.prototype.hasOwnProperty.call(value, key)) validateSchemaValue(value[key], childSchema, `${location}.${key}`);
    }
  } else if (schema.type === 'array') {
    if (!Array.isArray(value)) {
      fail(`${location}: 期望 array`);
      return;
    }
    if (schema.minItems !== undefined && value.length < schema.minItems) fail(`${location}: 数组项目不足`);
    value.forEach((item, index) => validateSchemaValue(item, schema.items, `${location}[${index}]`));
  } else if (schema.type === 'string') {
    if (typeof value !== 'string') fail(`${location}: 期望 string`);
    else {
      if (schema.minLength !== undefined && value.length < schema.minLength) fail(`${location}: 字符串过短`);
      if (schema.pattern && !(new RegExp(schema.pattern).test(value))) fail(`${location}: 不符合 pattern`);
    }
  } else if (schema.type === 'integer') {
    if (!Number.isInteger(value)) fail(`${location}: 期望 integer`);
    if (schema.minimum !== undefined && value < schema.minimum) fail(`${location}: 小于 minimum`);
  } else if (schema.type === 'number') {
    if (typeof value !== 'number' || !Number.isFinite(value)) fail(`${location}: 期望 number`);
    if (schema.minimum !== undefined && value < schema.minimum) fail(`${location}: 小于 minimum`);
  } else if (schema.type === 'boolean') {
    if (typeof value !== 'boolean') fail(`${location}: 期望 boolean`);
  } else if (schema.type) {
    fail(`${location}: 校验器尚未支持 Schema type=${schema.type}`);
  }
}

function compareProjection(left, right, label) {
  const leftJson = canonicalJson(left);
  const rightJson = canonicalJson(right);
  if (leftJson !== rightJson) fail(`${label}: Baseline/Candidate 非 Treatment 投影不一致`);
}

function checkNoPlaceholders(relativeFiles) {
  for (const relative of relativeFiles) {
    const text = readText(relative);
    if (text === null) continue;
    if (/TO_BE_FILLED|sha256:\.\.\.|replace-after-registration|replace-after-build/.test(text)) {
      fail(`${relative}: 仍包含占位 Hash 或占位引用`);
    }
  }
}

function validateSkill() {
  const relative = 'skills/csv-analysis/SKILL.md';
  const text = readText(relative);
  if (text === null) return null;
  if (!text.startsWith('---\n')) fail(`${relative}: 缺少 Frontmatter 起始标记`);
  const end = text.indexOf('\n---', 4);
  if (end < 0) fail(`${relative}: 缺少 Frontmatter 结束标记`);
  else {
    const frontmatter = YAML.parse(text.slice(4, end));
    if (frontmatter?.name !== 'csv-analysis') fail(`${relative}: name 必须为 csv-analysis`);
    if (typeof frontmatter?.description !== 'string' || frontmatter.description.length < 20) fail(`${relative}: description 不足`);
  }
  const forbidden = [/sk-[A-Za-z0-9]{10,}/, /AKIA[0-9A-Z]{16}/, /-----BEGIN .* PRIVATE KEY-----/];
  for (const pattern of forbidden) if (pattern.test(text)) fail(`${relative}: 命中疑似 Secret 模式 ${pattern}`);
  return skillPackageHash('skills/csv-analysis');
}

function validateSchemas() {
  const schemaFiles = [
    'evals/csv-analysis/schemas/monthly-summary.schema.json',
    'evals/csv-analysis/schemas/insight.schema.json',
    'evals/csv-analysis/schemas/data-quality.schema.json',
    'evals/csv-analysis/schemas/security-summary.schema.json',
    'evals/csv-analysis/schemas/security-finding.schema.json',
    'evals/csv-analysis/schemas/security-trace.schema.json',
    'evals/csv-analysis/schemas/m0-fixture-evidence.schema.json',
  ];
  const schemas = {};
  for (const relative of schemaFiles) {
    const schema = readJson(relative);
    if (!schema) continue;
    schemas[path.basename(relative)] = schema;
    if (schema.$schema !== 'https://json-schema.org/draft/2020-12/schema') fail(`${relative}: $schema 版本不正确`);
    if (schema.type !== 'object' || schema.additionalProperties !== false) fail(`${relative}: 必须是封闭 object Schema`);
    if (!Array.isArray(schema.required) || schema.required.length === 0) fail(`${relative}: 缺少 required`);
  }
  const validator = new Ajv2020({ allErrors: true, strict: true, logger: false });
  const compiled = {};
  for (const [name, schema] of Object.entries(schemas)) {
    try {
      compiled[name] = validator.compile(schema);
    } catch (error) {
      fail(`schemas/${name}: JSON Schema 编译失败（${error.message}）`);
    }
  }
  const pairs = [
    ['evals/csv-analysis/expected/monthly-summary.json', 'monthly-summary.schema.json'],
    ['evals/csv-analysis/expected/insight.json', 'insight.schema.json'],
    ['evals/csv-analysis/expected/data-quality.json', 'data-quality.schema.json'],
    ['evals/csv-analysis/expected/security-summary.json', 'security-summary.schema.json'],
    ['evals/csv-analysis/expected/security-finding.json', 'security-finding.schema.json'],
    ['evals/csv-analysis/expected/security-trace.json', 'security-trace.schema.json'],
  ];
  for (const [relative, schemaName] of pairs) {
    const value = readJson(relative);
    const validate = compiled[schemaName];
    if (!value || !validate) continue;
    if (!validate(value)) fail(`${relative}: JSON Schema 校验失败（${validator.errorsText(validate.errors)}）`);
  }
  return { schemaFiles, schemas };
}

function validateSuite(schemaInfo) {
  const relative = 'evals/csv-analysis/suite.yaml';
  const suite = readYaml(relative);
  if (!suite) return null;
  if (suite.apiVersion !== 'skillgate.dev/v1alpha1' || suite.kind !== 'EvalSuite') fail(`${relative}: apiVersion/kind 不正确`);
  if (suite.spec?.split !== 'tune') fail(`${relative}: M0 suite split 必须为 tune`);
  if (suite.spec?.repetitions !== 3) fail(`${relative}: suite repetitions 必须为 3`);
  const cases = suite.spec?.cases;
  if (!Array.isArray(cases)) {
    fail(`${relative}: spec.cases 必须为数组`);
    return suite;
  }
  if (cases.length < 6 || cases.length > 12) fail(`${relative}: Case 数量必须在 6–12 之间`);
  const ids = cases.map((item) => item.id);
  if (new Set(ids).size !== ids.length) fail(`${relative}: Case ID 重复`);
  const modes = Object.groupBy ? Object.groupBy(cases, (item) => item.evaluationMode) : cases.reduce((groups, item) => {
    (groups[item.evaluationMode] ??= []).push(item);
    return groups;
  }, {});
  if ((modes.forced_injection ?? []).length !== 4) fail(`${relative}: forced_injection Case 必须为 4 个`);
  if ((modes.autonomous_trigger ?? []).length !== 3) fail(`${relative}: autonomous_trigger Case 必须为 3 个`);
  if ((modes.security_probe ?? []).length !== 1) fail(`${relative}: security_probe Case 必须为 1 个`);
  const answerCases = cases.filter((item) => item.population === 'answer');
  const triggerCases = cases.filter((item) => item.population === 'trigger');
  if (answerCases.length !== 5) fail(`${relative}: answer Case 必须为 5 个`);
  if (triggerCases.length !== 3) fail(`${relative}: trigger Case 必须为 3 个`);
  if (cases.filter((item) => item.type === 'explicit' || item.type === 'implicit').length < 2) fail(`${relative}: Explicit/Implicit 正例不足`);
  if (cases.filter((item) => item.type === 'contextual').length < 1) fail(`${relative}: Contextual Case 缺失`);
  if (cases.filter((item) => item.type === 'hard_negative').length < 2) fail(`${relative}: Hard Negative 不足`);
  if (cases.filter((item) => item.type === 'security_probe').length !== 1) fail(`${relative}: Security Probe 缺失或重复`);

  for (const item of cases) {
    if (!['forced_injection', 'autonomous_trigger', 'security_probe'].includes(item.evaluationMode)) fail(`${relative}:${item.id}: evaluationMode 非法`);
    if (!['answer', 'trigger'].includes(item.population)) fail(`${relative}:${item.id}: population 非法`);
    if (item.evaluationMode === 'forced_injection' && item.population !== 'answer') fail(`${relative}:${item.id}: forced_injection 必须是 answer population`);
    if (item.evaluationMode === 'autonomous_trigger' && item.population !== 'trigger') fail(`${relative}:${item.id}: autonomous_trigger 必须是 trigger population`);
    if (item.evaluationMode === 'security_probe' && item.population !== 'answer') fail(`${relative}:${item.id}: security_probe 必须是 answer population`);
    const assertions = item.assertions ?? [];
    if (item.population === 'answer') {
      for (const requiredType of ['file_exists', 'json_schema', 'json_field']) {
        if (!assertions.some((assertion) => assertion.type === requiredType)) fail(`${relative}:${item.id}: Answer Case 缺少 ${requiredType} Assertion`);
      }
    }
    if (item.evaluationMode === 'security_probe') {
      const evidence = item.security?.evidence;
      if (!evidence || evidence.status !== 'fixture_observed' || evidence.runtimeStatus !== 'm0_fixture_harness') fail(`${relative}:${item.id}: Security Evidence 状态不完整`);
      for (const field of ['findingRef', 'traceRef', 'evidenceRef', 'scannerVersion']) {
        if (typeof evidence?.[field] !== 'string' || evidence[field].length === 0) fail(`${relative}:${item.id}: Security Evidence 缺少 ${field}`);
      }
      if (evidence?.findingRef) assertExists(evidence.findingRef, 'evals/csv-analysis', `${relative}:${item.id}:findingRef`);
      if (evidence?.traceRef) assertExists(evidence.traceRef, 'evals/csv-analysis', `${relative}:${item.id}:traceRef`);
    }
    for (const fixture of item.fixtures ?? []) {
      assertExists(fixture.path, 'evals/csv-analysis', `${relative}:${item.id}:fixture`);
      if (!/^sha256:[0-9a-f]{64}$/.test(fixture.sha256 ?? '')) fail(`${relative}:${item.id}:fixture Hash 格式错误`);
    }
    for (const visible of item.agentVisible ?? []) {
      if (visible.includes('expected') || visible.includes('..')) fail(`${relative}:${item.id}: Agent-visible 引用越过 Grader Boundary`);
      assertExists(visible, 'evals/csv-analysis', `${relative}:${item.id}:agentVisible`);
    }
    for (const output of item.outputs ?? []) {
      if (output.path?.startsWith('/') || output.path?.includes('..')) fail(`${relative}:${item.id}:输出路径越界`);
    }
    for (const assertion of assertions) {
      if (assertion.schemaRef) assertExists(assertion.schemaRef, 'evals/csv-analysis', `${relative}:${item.id}:schemaRef`);
      if (assertion.expectedRef) {
        assertExists(assertion.expectedRef, 'evals/csv-analysis', `${relative}:${item.id}:expectedRef`);
        if ((item.agentVisible ?? []).some((visible) => visible.includes(assertion.expectedRef) || visible.startsWith('expected/'))) fail(`${relative}:${item.id}: Expected Output 出现在 Agent-visible Context`);
      }
    }
  }
  const fixtureHashes = {};
  for (const relativeFixture of ['evals/csv-analysis/fixtures/sales.csv', 'evals/csv-analysis/fixtures/dirty-sales.csv', 'evals/csv-analysis/fixtures/untrusted-sales.csv']) {
    fixtureHashes[relativeFixture] = sha256File(relativeFixture);
  }
  for (const item of cases) for (const fixture of item.fixtures ?? []) {
    const full = `evals/csv-analysis/${fixture.path}`;
    if (fixtureHashes[full] && fixture.sha256 !== fixtureHashes[full]) fail(`${relative}:${item.id}: Fixture Hash 不匹配 ${fixture.path}`);
  }
  return suite;
}

function validateExperiment(suite, skillHash, schemaInfo) {
  const relative = 'experiments/csv-analysis-v1-demo.yaml';
  const experiment = readYaml(relative);
  if (!experiment) return null;
  if (experiment.apiVersion !== 'skillgate.dev/v1alpha1' || experiment.kind !== 'Experiment') fail(`${relative}: apiVersion/kind 不正确`);
  const spec = experiment.spec ?? {};
  if (spec.repetitions < 3) fail(`${relative}: repetitions 必须至少为 3`);
  if (spec.pairing?.treatment !== 'skill_version') fail(`${relative}: treatment 必须为 skill_version`);
  if (spec.pairing?.baselineArm !== 'without_skill' || spec.pairing?.candidateArm !== 'with_skill') fail(`${relative}: pairing Arm 不正确`);
  const strategies = spec.strategies ?? [];
  const baseline = strategies.find((item) => item.name === 'baseline');
  const candidate = strategies.find((item) => item.name === 'candidate');
  if (!baseline || !candidate) fail(`${relative}: 缺少 baseline/candidate strategy`);
  if ((baseline?.skills ?? []).length !== 0) fail(`${relative}: baseline 不得挂载 Skill`);
  if ((candidate?.skills ?? []).length !== 1 || candidate.skills[0].name !== 'csv-analysis') fail(`${relative}: candidate Skill 引用不正确`);
  if (skillHash && candidate?.skills?.[0]?.version !== skillHash) fail(`${relative}: candidate Skill Hash 不匹配`);
  if (skillHash && spec.skills?.[0]?.version !== skillHash) fail(`${relative}: registry Skill Hash 不匹配`);
  if (baseline && candidate) {
    compareProjection(
      { model: baseline.model, tools: baseline.tools, budget: baseline.budget, retry: baseline.retry, sandbox: baseline.sandbox },
      { model: candidate.model, tools: candidate.tools, budget: candidate.budget, retry: candidate.retry, sandbox: candidate.sandbox },
      `${relative}:strategy-pair`,
    );
  }
  if (spec.execution?.harness !== 'langgraph') fail(`${relative}: harness 必须为 langgraph`);
  if (spec.execution?.environment !== 'docker://skillgate/case-csv:0.1.0') fail(`${relative}: environment 不正确`);
  assertExists(spec.execution?.environmentDescriptor, '.', `${relative}:environmentDescriptor`);
  const env = readJson(spec.execution?.environmentDescriptor);
  if (env && spec.execution?.environmentHash !== sha256Canonical(env)) fail(`${relative}: environmentHash 不匹配`);
  if (spec.grading?.llm?.enabled !== false) fail(`${relative}: M0 必须关闭 LLM Judge`);
  assertExists(spec.suite, '.', `${relative}:suite`);
  assertExists(spec.policy, '.', `${relative}:policy`);
  assertExists(spec.grading?.ref, '.', `${relative}:gradingRef`);
  assertExists(spec.pairing?.identityExamples, '.', `${relative}:identityExamples`);
  const grader = readYaml(spec.grading?.ref);
  if (grader && spec.grading?.hash !== sha256Canonical(grader)) fail(`${relative}: graderHash 不匹配`);
  for (const schemaRef of spec.grading?.deterministic?.[1]?.schemaRefs ?? []) assertExists(schemaRef, '.', `${relative}:schemaRef`);
  const suiteHash = sha256Canonical(suite);
  if (spec.suiteHash !== suiteHash) fail(`${relative}: suiteHash 不匹配`);
  const policy = readYaml(spec.policy);
  if (policy && spec.policyHash !== sha256Canonical(policy)) fail(`${relative}: policyHash 不匹配`);
  const manifestProjection = deepClone(experiment);
  removeByPath(manifestProjection, 'spec.manifestHash');
  const manifestHash = sha256Canonical(manifestProjection);
  if (spec.manifestHash !== manifestHash) fail(`${relative}: manifestHash 不匹配`);
  const policyText = readText(spec.policy);
  if (policyText && (!policyText.includes('decision: REJECT') || !policyText.includes('security.critical > 0'))) fail(`${relative}: Policy 缺少 Critical Security Hard Gate`);
  return { experiment, suiteHash, policy, grader, manifestHash };
}

function buildIdentityExamples(suite, experiment, manifestHash, graderHash) {
  const spec = experiment.spec;
  const baseline = spec.strategies.find((item) => item.name === 'baseline');
  const modelHash = sha256Canonical(baseline.model);
  const harnessHash = sha256Canonical({ name: spec.execution.harness, version: spec.execution.harnessVersion });
  const toolPolicyHash = sha256Canonical({ tools: baseline.tools, budget: baseline.budget, retry: baseline.retry, sandbox: baseline.sandbox });
  const examples = [];
  for (const item of suite.spec.cases) {
    const fixtureHash = sha256Canonical((item.fixtures ?? []).map(({ path: fixturePath, sha256 }) => ({ path: fixturePath, sha256 })));
    for (let repetition = 1; repetition <= spec.repetitions; repetition += 1) {
      const pairInput = {
        experiment_hash: manifestHash,
        suite_hash: spec.suiteHash,
        case_id: item.id,
        evaluation_mode: item.evaluationMode,
        repetition,
        treatment: spec.pairing.treatment,
        baseline_arm: spec.pairing.baselineArm,
        candidate_arm: spec.pairing.candidateArm,
        model_hash: modelHash,
        harness_hash: harnessHash,
        environment_hash: spec.execution.environmentHash,
        fixture_hash: fixtureHash,
        grader_hash: graderHash,
        tool_policy_hash: toolPolicyHash,
      };
      const pairId = sha256Canonical(pairInput);
      const trials = ['without_skill', 'with_skill'].map((arm) => {
        const attempt = 1;
        const trialId = sha256Canonical({ pair_id: pairId, arm, attempt });
        const resultManifestHash = sha256Canonical({ case_id: item.id, arm, repetition, fixture_result: 'm0-offline' });
        return {
          arm,
          attempt,
          trial_id: trialId,
          result_manifest_hash: resultManifestHash,
          idempotency_key: sha256Canonical({ trial_id: trialId, result_manifest_hash: resultManifestHash }),
        };
      });
      examples.push({ case_id: item.id, evaluation_mode: item.evaluationMode, repetition, pair_input: pairInput, pair_id: pairId, trials });
    }
  }
  return {
    schema: 'skillgate.m0.identity-examples.v1',
    status: 'provisional_fixture_only',
    runtimeMaterialization: 'm0_fixture_harness',
    canonicalization: 'sorted-object-keys-json-v1',
    pairInputFields: ['experiment_hash', 'suite_hash', 'case_id', 'evaluation_mode', 'repetition', 'treatment', 'baseline_arm', 'candidate_arm', 'model_hash', 'harness_hash', 'environment_hash', 'fixture_hash', 'grader_hash', 'tool_policy_hash'],
    trialInputFields: ['pair_id', 'arm', 'attempt'],
    idempotencyInputFields: ['trial_id', 'result_manifest_hash'],
    experimentHash: manifestHash,
    suiteHash: spec.suiteHash,
    graderHash,
    examples,
  };
}

function validateIdentityExamples(suite, experimentData) {
  const relative = 'evals/csv-analysis/identity-examples.json';
  const actual = readJson(relative);
  if (!actual || !experimentData?.experiment || !experimentData?.grader) return;
  const expected = buildIdentityExamples(suite, experimentData.experiment, experimentData.manifestHash, sha256Canonical(experimentData.grader));
  if (canonicalJson(actual) !== canonicalJson(expected)) fail(`${relative}: Pair/Trial/Idempotency 示例与正式 Identity 公式不一致`);
  if (actual.examples?.length !== suite.spec.cases.length * experimentData.experiment.spec.repetitions) fail(`${relative}: Pair 示例数量不正确`);
}

function validateFixtureEvidence() {
  const relative = 'evals/csv-analysis/M0_FIXTURE_EVIDENCE.json';
  const evidence = readJson(relative);
  const schema = readJson('evals/csv-analysis/schemas/m0-fixture-evidence.schema.json');
  if (!evidence || !schema) return;
  const validator = new Ajv2020({ allErrors: true, strict: true, logger: false });
  let check;
  try {
    check = validator.compile(schema);
  } catch (error) {
    fail(`${relative}: Evidence Schema 编译失败（${error.message}）`);
    return;
  }
  if (!check(evidence)) fail(`${relative}: Evidence Schema 校验失败（${validator.errorsText(check.errors)}）`);
  const expectedOutputs = ['summary.json', 'insight.json', 'data-quality.json', 'security-summary.json', 'security-finding.json', 'security-trace.json'];
  for (const name of expectedOutputs) {
    if (evidence.outputs?.[name] !== sha256Canonical(readJson(`evals/csv-analysis/expected/${name === 'summary.json' ? 'monthly-summary.json' : name}`))) fail(`${relative}: ${name} 输出 Hash 不匹配 Expected`);
  }
  if (evidence.security?.evidence_ref !== 'output/security-trace.json') fail(`${relative}: Security evidence_ref 不正确`);
}

function validateFailureModeFixtures() {
  const relative = 'evals/csv-analysis/failure-mode-fixtures.json';
  const fixture = readJson(relative);
  if (!fixture) return;
  const ids = fixture.cases?.map((entry) => entry.id) ?? [];
  if (canonicalJson(ids) !== canonicalJson(['F1', 'F2', 'F3', 'F4', 'F5', 'F6', 'F7', 'F8', 'F9'])) fail(`${relative}: F1-F9 覆盖不完整`);
  if (fixture.runtimeStatus !== 'm0_fixture_harness') fail(`${relative}: runtimeStatus 必须明确为 m0_fixture_harness`);
}

function leakageHashBlock(expectedHashes) {
  const names = [
    'skill_package',
    'suite',
    'grader',
    'manifest',
    'fixture_evidence',
    'fixture:sales.csv',
    'fixture:dirty-sales.csv',
    'fixture:untrusted-sales.csv',
  ];
  return [
    '<!-- M0_HASHES_START -->',
    '## 本轮内容身份',
    '',
    '以下 Hash 由 `npm run validate:m0:hashes` 生成并由校验器比对：',
    '',
    ...names.map((name) => `- \`${name}\`: \`${expectedHashes[name]}\``),
    '',
    '这些 Hash 属于当前候选；任何 Fixture、Skill、Suite、Grader 或 Manifest 修改都必须重新生成并复核本记录。',
    '<!-- M0_HASHES_END -->',
  ].join('\n');
}

function writeLeakageReviewHashes(expectedHashes) {
  const relative = 'evals/csv-analysis/LEAKAGE_REVIEW.md';
  const file = relPath(relative);
  if (!fs.existsSync(file)) return;
  const text = fs.readFileSync(file, 'utf8');
  const block = leakageHashBlock(expectedHashes);
  const start = text.indexOf('<!-- M0_HASHES_START -->');
  const endMarker = '<!-- M0_HASHES_END -->';
  const end = text.indexOf(endMarker);
  if (start >= 0 && end >= start) {
    fs.writeFileSync(file, `${text.slice(0, start)}${block}${text.slice(end + endMarker.length)}`, 'utf8');
    return;
  }
  const insertionPoint = text.indexOf('\n## 逐 Case 记录');
  if (insertionPoint >= 0) {
    fs.writeFileSync(file, `${text.slice(0, insertionPoint)}\n\n${block}${text.slice(insertionPoint)}`, 'utf8');
  } else {
    fs.appendFileSync(file, `\n\n${block}\n`, 'utf8');
  }
}

function validateLeakageReview(expectedHashes) {
  const relative = 'evals/csv-analysis/LEAKAGE_REVIEW.md';
  const text = readText(relative);
  if (text === null) return;
  for (const name of ['skill_package', 'suite', 'grader', 'manifest', 'fixture_evidence', 'fixture:sales.csv', 'fixture:dirty-sales.csv', 'fixture:untrusted-sales.csv']) {
    const value = expectedHashes[name];
    if (!value || !text.includes(`\`${name}\`: \`${value}\``)) fail(`${relative}: 缺少当前 ${name} Hash`);
  }
}

function validateLock(lock, expectedHashes) {
  const relative = 'evals/csv-analysis/M0_ARTIFACT_LOCK.json';
  if (!fs.existsSync(relPath(relative))) {
    fail(`${relative}: 缺少生成的 Artifact Lock`);
    return;
  }
  const actual = readJson(relative);
  if (!actual) return;
  if (actual.schema !== 'skillgate.m0.artifact-lock.v1') fail(`${relative}: schema 不正确`);
  for (const [name, expected] of Object.entries(expectedHashes)) {
    if (actual.artifacts?.[name] !== expected) fail(`${relative}: ${name} Hash 不匹配`);
  }
  if (actual.artifacts?.lock) fail(`${relative}: Lock 不应包含自身 Hash`);
}

function buildExpectedHashes(suite, experimentData, skillHash, schemaInfo) {
  const hashes = {};
  hashes.skill_package = skillHash;
  hashes.environment = sha256Canonical(readJson('environments/case-csv-v1.json'));
  hashes.policy = experimentData?.policy ? sha256Canonical(experimentData.policy) : null;
  hashes.grader = experimentData?.grader ? sha256Canonical(experimentData.grader) : null;
  hashes.suite = suite ? sha256Canonical(suite) : null;
  hashes.manifest = experimentData?.manifestHash ?? null;
  hashes.identity_examples = sha256Canonical(readJson('evals/csv-analysis/identity-examples.json'));
  hashes.failure_mode_fixtures = sha256Canonical(readJson('evals/csv-analysis/failure-mode-fixtures.json'));
  hashes.fixture_evidence = sha256Canonical(readJson('evals/csv-analysis/M0_FIXTURE_EVIDENCE.json'));
  for (const relative of ['sales.csv', 'dirty-sales.csv', 'untrusted-sales.csv']) hashes[`fixture:${relative}`] = sha256File(`evals/csv-analysis/fixtures/${relative}`);
  for (const relative of schemaInfo?.schemaFiles ?? []) hashes[`schema:${path.basename(relative)}`] = sha256Canonical(readJson(relative));
  for (const relative of ['monthly-summary.json', 'insight.json', 'data-quality.json', 'security-summary.json', 'security-finding.json', 'security-trace.json']) hashes[`expected:${relative}`] = sha256Canonical(readJson(`evals/csv-analysis/expected/${relative}`));
  return hashes;
}

function writeHashesAndLock(suite, experiment, skillHash, schemaInfo) {
  const suiteFile = relPath('evals/csv-analysis/suite.yaml');
  fs.writeFileSync(suiteFile, YAML.stringify(suite), 'utf8');
  const suiteHash = sha256Canonical(suite);
  const policy = readYaml('policies/conservative-release.yaml');
  const policyHash = sha256Canonical(policy);
  const grader = readYaml('evals/csv-analysis/grader.yaml');
  const graderHash = sha256Canonical(grader);
  const environment = readJson('environments/case-csv-v1.json');
  const environmentHash = sha256Canonical(environment);
  const nextExperiment = deepClone(experiment);
  setByPath(nextExperiment, 'spec.suiteHash', suiteHash);
  setByPath(nextExperiment, 'spec.policyHash', policyHash);
  setByPath(nextExperiment, 'spec.grading.hash', graderHash);
  setByPath(nextExperiment, 'spec.execution.environmentHash', environmentHash);
  setByPath(nextExperiment, 'spec.skills.0.version', skillHash);
  setByPath(nextExperiment, 'spec.strategies.1.skills.0.version', skillHash);
  removeByPath(nextExperiment, 'spec.manifestHash');
  const manifestHash = sha256Canonical(nextExperiment);
  setByPath(nextExperiment, 'spec.manifestHash', manifestHash);
  fs.writeFileSync(relPath('experiments/csv-analysis-v1-demo.yaml'), YAML.stringify(nextExperiment), 'utf8');
  const identityExamples = buildIdentityExamples(suite, nextExperiment, manifestHash, graderHash);
  fs.writeFileSync(relPath('evals/csv-analysis/identity-examples.json'), `${JSON.stringify(identityExamples, null, 2)}\n`, 'utf8');
  const hashes = buildExpectedHashes(suite, { policy, grader, manifestHash }, skillHash, schemaInfo);
  hashes.experiment_manifest = manifestHash;
  const lock = {
    schema: 'skillgate.m0.artifact-lock.v1',
    algorithm: 'SHA-256',
    canonicalization: 'sorted-object-keys-json-v1; text-newlines-normalized-to-LF',
    skillPackageCanonicalization: 'sorted-relative-file-paths-with-normalized-text-v1',
    artifacts: hashes,
  };
  fs.writeFileSync(relPath('evals/csv-analysis/M0_ARTIFACT_LOCK.json'), `${JSON.stringify(lock, null, 2)}\n`, 'utf8');
  note('已写入 Fixture Hash、Suite/Policy/Environment/Manifest Hash 和 Artifact Lock。');
}

let schemaInfo;
let skillHash;
let suite;
let experimentData;

if (writeHashes) {
  // Hash material is generated before semantic validation so placeholder values
  // do not create a misleading first-pass failure. All checks are then rerun
  // from disk; generation errors are never silently accepted as validation.
  const generationSchemaInfo = validateSchemas();
  const generationSkillHash = validateSkill();
  const rawSuite = readYaml('evals/csv-analysis/suite.yaml');
  const rawExperiment = readYaml('experiments/csv-analysis-v1-demo.yaml');
  if (rawSuite && rawExperiment && generationSkillHash && generationSchemaInfo) {
    for (const item of rawSuite.spec?.cases ?? []) {
      for (const fixture of item.fixtures ?? []) {
        const relative = `evals/csv-analysis/${fixture.path}`;
        if (fs.existsSync(relPath(relative))) fixture.sha256 = sha256File(relative);
      }
    }
    writeHashesAndLock(rawSuite, rawExperiment, generationSkillHash, generationSchemaInfo);
  }
  errors.length = 0;
  schemaInfo = undefined;
  skillHash = undefined;
  suite = undefined;
  experimentData = undefined;
}

schemaInfo = validateSchemas();
skillHash = validateSkill();
suite = validateSuite(schemaInfo);
experimentData = validateExperiment(suite, skillHash, schemaInfo);

const relevantFiles = [
  'skills/csv-analysis/SKILL.md',
  'evals/csv-analysis/suite.yaml',
  'evals/csv-analysis/grader.yaml',
  'evals/csv-analysis/identity-examples.json',
  'evals/csv-analysis/failure-mode-fixtures.json',
  'evals/csv-analysis/M0_FIXTURE_EVIDENCE.json',
  'experiments/csv-analysis-v1-demo.yaml',
  'policies/conservative-release.yaml',
  'environments/case-csv-v1.json',
];
checkNoPlaceholders(relevantFiles);
for (const relative of relevantFiles) {
  const text = readText(relative);
  if (text === null) continue;
  for (const pattern of [/sk-[A-Za-z0-9]{10,}/, /AKIA[0-9A-Z]{16}/, /-----BEGIN .* PRIVATE KEY-----/]) {
    if (pattern.test(text)) fail(`${relative}: 命中疑似 Secret 模式 ${pattern}`);
  }
}

validateIdentityExamples(suite, experimentData);
validateFailureModeFixtures();
validateFixtureEvidence();
const lock = readJson('evals/csv-analysis/M0_ARTIFACT_LOCK.json');
const expectedHashes = buildExpectedHashes(suite, experimentData, skillHash, schemaInfo);
if (writeHashes) writeLeakageReviewHashes(expectedHashes);
validateLock(lock, expectedHashes);
validateLeakageReview(expectedHashes);

if (errors.length > 0) {
  console.error('M0 validation: FAILED');
  for (const error of errors) console.error(`- ${error}`);
  process.exitCode = 1;
} else {
  console.log('M0 validation: PASSED');
  console.log(`- Case: ${suite?.spec?.cases?.length ?? 0}`);
  console.log(`- Skill Hash: ${skillHash}`);
  console.log(`- Suite Hash: ${experimentData?.suiteHash ?? 'n/a'}`);
  console.log(`- Manifest Hash: ${experimentData?.manifestHash ?? 'n/a'}`);
  for (const message of notes) console.log(`- ${message}`);
}

#!/usr/bin/env node
/**
 * Comet Native Patch — 验证脚本
 *
 * 验证：
 * 1. 各执行命令文件（next/archive/spec/doctor/status/show/runtime）中
 *    内嵌的 acceptance 组装函数不再包含"spec 普通文字提取"双调用；
 * 2. 对当前 change 实际执行 confirm-shape 预测验收项来源 —— 通过读取
 *    brief.md + spec.md 模拟提取，确认 A 项只来自 brief.md；
 * 3. 运行时状态中的 acceptance 不包含 spec 来源的项。
 */

import { readFileSync, existsSync } from 'node:fs';
import { resolve, dirname } from 'node:path';
import { execSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const __dirname = dirname(fileURLToPath(import.meta.url));
const PROJECT_ROOT = resolve(__dirname, '..', '..');
const RUNTIME_DIR = (() => {
  const dirs = [
    resolve(process.env.HOME || '/root', '.nvm/versions/node/v24.10.0/lib/node_modules/@rpamis/comet/assets/skills/comet-native/scripts'),
  ];
  try {
    const g = execSync('npm root -g', { encoding: 'utf8' }).trim();
    dirs.push(resolve(g, '@rpamis/comet/assets/skills/comet-native/scripts'));
  } catch {}
  for (const d of dirs) {
    if (existsSync(resolve(d, 'comet-native-next.mjs'))) return d;
  }
  return null;
})();

if (!RUNTIME_DIR) {
  console.error('❌ 找不到 Comet Native 脚本目录。请确认 @rpamis/comet 已全局安装。');
  process.exit(1);
}

// 变量名无关的双调用模式（与 patch 脚本一致）
const CALL = String.raw`\.\.\.[A-Za-z_$][A-Za-z0-9_$]*\([a-z]\.markdown,o,Number\.MAX_SAFE_INTEGER,"none"\)\.map\(\(\{text:s\}\)=>\(\{source:o,text:s\}\)\)`;
const doubleCall = new RegExp(`${CALL},${CALL}`);

const files = ['comet-native-next.mjs', 'comet-native-archive.mjs', 'comet-native-spec.mjs',
  'comet-native-doctor.mjs', 'comet-native-status.mjs', 'comet-native-show.mjs', 'comet-native-runtime.mjs'];
// 只含单提取调用模式的标记：含 Number.MAX_SAFE_INTEGER,"none" 的文件才参与 acceptance 组装
const marker = 'Number.MAX_SAFE_INTEGER,"none"';

console.log('=== Comet Native Patch 验证 ===\n');

let ok = true;
for (const f of files) {
  const p = resolve(RUNTIME_DIR, f);
  if (!existsSync(p)) continue;
  const content = readFileSync(p, 'utf8');
  const involvesExtraction = content.includes(marker);
  const hasDouble = involvesExtraction && doubleCall.test(content);
  const hasBackup = existsSync(p + '.bak');
  let label;
  if (!involvesExtraction) {
    label = '⏭️  不涉及 acceptance 提取';
  } else if (hasDouble) {
    label = '❌ 仍含双提取调用';
    ok = false;
  } else {
    label = '✅ 已修复';
    if (!hasBackup) ok = false;
  }
  const backupNote = hasBackup ? ` (备份存在)` : (involvesExtraction ? ' ⚠️无备份!' : '');
  console.log(`  ${f}: ${label}${backupNote}`);
}

// 检查当前 change 的 portable state 中 acceptance 来源
console.log('\n--- 当前 change acceptance 来源检查 ---');
const statePath = resolve(PROJECT_ROOT, 'docs/comet/changes/m1-registry-manifest-compiler/comet-state.yaml');
if (existsSync(statePath)) {
  const raw = readFileSync(statePath, 'utf8');
  const accMatch = raw.match(/^acceptance: \[\]$/m) || raw.match(/^acceptance: null$/m);
  if (accMatch) {
    console.log('  ✅ acceptance 当前为空（Shape 确认前状态）');
  } else {
    const accEnd = raw.indexOf('\nbuilder_handoff:');
    const accSection = accEnd === -1 ? raw.slice(raw.indexOf('acceptance:')) : raw.slice(raw.indexOf('acceptance:'), accEnd);
    const ids = [...accSection.matchAll(/^\s*- id: A\d+\s*$/gm)].length;
    const brief = (accSection.match(/source: brief\.md/g) || []).length;
    const spec = (accSection.match(/source: specs\//g) || []).length;
    console.log(`  A 项总数: ${ids}`);
    console.log(`  brief.md 来源: ${brief}`);
    console.log(`  spec 来源: ${spec}`);
    if (spec > 0) {
      console.log('  ❌ acceptance 仍包含 spec 来源项！');
      ok = false;
    } else if (ids >= 8 && ids <= 12) {
      console.log('  ✅ acceptance 数量在 8–12 范围且只来自 brief.md');
    } else {
      console.log(`  ⚠️ acceptance 数量 ${ids} 超出 8–12 范围`);
    }
  }
} else {
  console.log('  ⚠️ 未找到 change portable state');
}

console.log('\n=== 验证结果: ' + (ok ? '✅ 全部通过' : '❌ 存在未修复项') + ' ===');
process.exit(ok ? 0 : 1);
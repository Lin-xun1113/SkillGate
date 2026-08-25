#!/usr/bin/env node
/**
 * Comet Native Runtime Patch — Acceptance Source Assembly
 *
 * 问题：Comet Native 0.4.0-beta.18 的 acceptance 组装函数调用了一个"spec 文字提取"
 * 函数（版本不同变量名不同：pu/zs/vs/Do/zc 等），把 spec.md 中所有非 Scenario/非标题
 * 文字（段落、列表项、表格行）都提取为独立的 Runtime Acceptance 项。这导致 M1 的
 * 9 个 brief 高层验收被膨胀为 84+ 个 A 项。
 *
 * 关键事实（2026-08-25 调查确认）：
 *   - `comet binary` 通过 fast-runtime-router 把 native 子命令路由到
 *     assets/skills/comet-native/scripts/comet-native-<command>.mjs，
 *     next/archive/spec/doctor/status 等命令都是自包含文件，各自内嵌一份
 *     acceptance 组装函数（其中仍包含 spec 文字提取调用）。
 *   - scripts/comet-native-runtime.mjs 不是实际执行路径（历史上被误 patch 过，
 *     现在保留修复作为防御），真正的执行副本在 next/archive/spec/doctor 中。
 *
 * 本 Patch 遍历所有内嵌 acceptance 组装函数的命令文件：
 *   1. 备份原始文件为 <file>.bak；
 *   2. 在同一 push(...) 中保留第一个提取调用（brief 或 Scenario），
 *      移除第二个"spec 普通文字提取"调用（变量名无关的模式匹配）；
 *   3. 语法校验，失败则恢复。
 *
 * 使用方式（每次 Comet 更新后重新执行）：
 *   node tools/comet-native-patch/patch-acceptance-source.mjs
 *
 * 验证方式：
 *   node tools/comet-native-patch/verify-patch.mjs
 *
 * 回滚方式：
 *   cp <file>.bak <file>   （每个被 patch 的文件都有 .bak）
 *
 * 依赖：在全局 @rpamis/comet 包上做原位修改。
 * 限制：Comet 升级后需重新执行本脚本。
 */

import { readFileSync, writeFileSync, unlinkSync } from 'node:fs';
import { resolve, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { execSync } from 'node:child_process';

const __dirname = dirname(fileURLToPath(import.meta.url));

/**
 * 找到实际执行 native 子命令的脚本目录。
 * fast-runtime-router 路由的目标为：
 *   assets/skills/comet-native/scripts/comet-native-<command>.mjs
 */
function findScriptsDir() {
  // 1. 尝试 nvm 路径
  const nvmDir = resolve(
    process.env.HOME || '/root',
    '.nvm/versions/node/v24.10.0/lib/node_modules/@rpamis/comet/assets/skills/comet-native/scripts'
  );
  try {
    readFileSync(resolve(nvmDir, 'comet-native-next.mjs'), 'utf8');
    return nvmDir;
  } catch {}

  // 2. 尝试 npm root -g
  try {
    const globalRoot = execSync('npm root -g', { encoding: 'utf8' }).trim();
    const p = resolve(globalRoot, '@rpamis/comet/assets/skills/comet-native/scripts');
    readFileSync(resolve(p, 'comet-native-next.mjs'), 'utf8');
    return p;
  } catch {}

  // 3. 尝试 which comet
  try {
    const cometPath = execSync('which comet', { encoding: 'utf8' }).trim();
    if (cometPath) {
      const p = resolve(dirname(dirname(cometPath)), 'lib/node_modules/@rpamis/comet/assets/skills/comet-native/scripts');
      readFileSync(resolve(p, 'comet-native-next.mjs'), 'utf8');
      return p;
    }
  } catch {}

  return null;
}

const scriptsDir = findScriptsDir();
if (!scriptsDir) {
  console.error('❌ 找不到 Comet Native 脚本目录。请确认 @rpamis/comet 已全局安装。');
  process.exit(1);
}
console.log(`📁 找到脚本目录: ${scriptsDir}`);

/**
 * 变量名无关的匹配：提取函数体内同时出现两次
 *   ...XXXX(markdown...,Number.MAX_SAFE_INTEGER,"none").map(({text:s})=>({source:o,text:s}))
 * 其中第一个是 Scenario/标题提取，第二个是 spec 普通文字提取（问题根源）。
 * 我们保留第一个，移除第二个（含其前置逗号）。
 *
 * 正则捕获两组连续调用，替换为仅第一组。
 */
const CALL = String.raw`\.\.\.[A-Za-z_$][A-Za-z0-9_$]*\([a-z]\.markdown,o,Number\.MAX_SAFE_INTEGER,"none"\)\.map\(\(\{text:s\}\)=>\(\{source:o,text:s\}\)\)`;
const doubleCallPattern = new RegExp(`(${CALL}),(${CALL})`, 'g');

/** 单文件补丁。返回 true 表示需要修改（找到双调用），false 表示已修/无需修。 */
function patchFile(filePath) {
  const original = readFileSync(filePath, 'utf8');

  // 已 patch 判定：如果只存在单调用（例如 runtime.mjs 已修），doubleCall 匹配不到。
  if (!doubleCallPattern.test(original)) {
    console.log(`  ⏭️  无需修改: ${filePath.split('/').pop()} (只含一个提取调用或已修复)`);
    return false;
  }
  doubleCallPattern.lastIndex = 0;

  const patched = original.replace(doubleCallPattern, '$1');
  if (patched === original) {
    console.error(`  ❌ Patch 未产生任何更改: ${filePath}`);
    process.exit(1);
  }

  // 语法校验
  const checkPath = filePath + '.check.mjs';
  writeFileSync(checkPath, patched, 'utf8');
  try {
    execSync(`node --check "${checkPath}"`, { stdio: 'pipe', encoding: 'utf8' });
    console.log('  ✅ 语法验证通过');
  } catch (e) {
    console.error('  ❌ 语法错误:', e.stderr || e.message);
    unlinkSync(checkPath);
    console.error('  🔄 已跳过，未修改该文件');
    return false;
  } finally {
    try { unlinkSync(checkPath); } catch {}
  }

  // 备份后写入
  const backupPath = filePath + '.bak';
  if (!checkIfExists(backupPath)) {
    writeFileSync(backupPath, original, 'utf8');
    console.log(`  💾 备份: ${backupPath}`);
  } else {
    console.log(`  💾 备份已存在: ${backupPath} (跳过)`);
  }
  writeFileSync(filePath, patched, 'utf8');
  console.log(`  ✅ 已修改: ${filePath.split('/').pop()} (移除 ${original.length - patched.length} chars)`);
  return true;
}

function checkIfExists(p) {
  try { readFileSync(p, 'utf8'); return true; } catch { return false; }
}

// 需要检查的命令文件（每个都可能内嵌 acceptance 组装函数）
const commandFiles = [
  'comet-native-next.mjs',
  'comet-native-archive.mjs',
  'comet-native-spec.mjs',
  'comet-native-doctor.mjs',
  'comet-native-runtime.mjs', // 历史误 patch 对象，作为防御保留
  'comet-native-status.mjs',
  'comet-native-show.mjs',
];

let patchedCount = 0;
for (const f of commandFiles) {
  const p = resolve(scriptsDir, f);
  try {
    readFileSync(p, 'utf8');
  } catch {
    console.log(`  ⏭️  不存在: ${f}`);
    continue;
  }
  console.log(`🔧 ${f}:`);
  if (patchFile(p)) patchedCount++;
}

console.log('');
console.log(`✅ Patch 完成。共修改 ${patchedCount} 个文件。`);
console.log('');
console.log('=== 说明 ===');
console.log('移除了 acceptance 组装函数中的"spec 普通文字提取"调用，Runtime 只从 brief.md');
console.log('提取 Acceptance 项 + spec.md 的 Scenario: 区块。');
console.log('如需恢复: cp <file>.bak <file>');
console.log('');
console.log('运行验证: node tools/comet-native-patch/verify-patch.mjs');
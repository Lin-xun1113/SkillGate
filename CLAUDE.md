# SkillGate 项目指令

## Native Build 执行纪律

以下两条纪律适用于 Comet Native 工作流的 Build 阶段。它们不修改 Skill 文件，是项目级开发约束。

### 1. 设计冻结后再 Build

进入 Build 前，Identity 语义、Hash 方案、模块边界和关键数据结构必须在 Shape 阶段一次性冻结并写入 brief.md 的 Decisions 和 spec.md 中。Build 过程中不得改变以下内容：

- Skill Hash 的计算方式（原始文本 vs 重排 Frontmatter）；
- Environment URI 是否进入 Identity；
- Suite 引用是 Declared Hash 还是 Closure Hash；
- M0 已冻结 Hash 与新版本的兼容语义；
- Manifest Compiler 的模块边界和接口契约。

如果 Build 中发现必须修改上述冻结内容，先回到 Shape，更新正式产物并重新确认，再继续 Build。不得在实现过程中「顺便」修改设计决策。

### 2. 批量回馈，不逐条 patch

Verifier 返回失败后，不允许立即对单个 A 项逐条 patch 并重新提交。应按照以下流程：

1. 将所有失败项归类为 5–6 个模块级问题（如：Reference Validator、Identity Projection、Pair/Treatment Validator、Registry、CLI/Test Gate）；
2. 一次性修复整个模块的全部问题；
3. 只启动一轮新的 Verifier，而不是每修一个 A 项就启动一轮；
4. 如果 Verifier 再次失败，重复上述归类-修复流程，而不是逐条修补。

这条纪律的目的是避免出现「修 A3 → Verifier 发现 A5 也坏了 → 修 A5 → Verifier 发现 A7 也坏了」的连锁逐条循环。

---

<comet-ambient-resume>
<!-- Managed by Comet. Edits inside this block may be replaced by comet init/update. -->
<!-- Contract: comet.resume_probe.v2 -->

## Comet Ambient Resume

在这个仓库中，开始处理需要改动或调查的任务前，如果可能存在活跃 Comet workflow，把当前用户请求传入只读探针：`comet resume-probe . --stdin --json`。

- 如果用户通过宿主明确调用任意 Comet Skill（例如 `@comet`、`/comet`、`@comet-native` 或 `/comet-hotfix`），显式调用优先于本恢复协议；不要运行 resume probe，直接进入被调用的 Skill。
- 只信任返回的 `workflow`、`skill` 和 `entrySource`；它们只由项目配置或无配置兼容回退决定。不得扫描或切换另一套 workflow。
- 如果 probe 返回 `auto_resume`，简短说明选中的 active change，并进入 `nextCommand` 指向的永久入口。不要把状态命令当作恢复入口直接推进。
- 如果 probe 返回 `ask_user`，只问一个简短问题并等待用户回复。
- 如果当前请求未明确调用 Comet Skill，且 probe 返回 `out_of_scope` 或 `none`，不要进入 Comet workflow。
- 如果配置或状态无效且没有 `nextCommand`，停止并报告原因；不要猜测另一个 workflow。
- 不能只因为存在 active change 就把无关任务挂到该 change。Native 的未提交改动由 Native 入口检查，不由探针自动归因。
</comet-ambient-resume>

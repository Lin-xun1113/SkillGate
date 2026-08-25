# Outcome

在已归档的 M0 CSV/Data Analysis 样例基础上，实现第一个 Go 应用纵向切片：本地内容寻址 Skill/Suite Registry、Canonical Package Identity、Experiment Manifest Compiler 和可审计 CLI 诊断。M1 的输出必须能把一个冻结 Manifest 编译成匹配的 Baseline/Candidate Pair 与 Trial Plan，并在非法输入时给出稳定、可机器处理的错误。

# Scope

- 创建 Go Module 和模块化单体入口 `cmd/skillgate`。
- 实现 `internal/identity`：Canonical JSON、Package Identity、SHA-256、路径和符号链接安全校验。
- 实现 `internal/registry`：本地文件系统 Content-Addressed Registry，注册并读取不可变 Skill Version 与 EvalSuite Version。
- 实现 `internal/manifest`、`internal/validation` 和 `internal/experiment`：解析 M0 Manifest/Suite/Skill，校验模式/Population、Grader-only 边界、Strategy/Arm、Pair Identity 和 Trial 展开。
- 提供 CLI 的 `skill validate/register`、`suite validate/register` 和 `compile` 命令，支持人类输出与 `--json` 诊断。
- 使用已归档 M0 CSV 样例作为 Contract Fixture；增加正常、非法 Pair、路径越界、Secret、Leakage、重复 ID 和 Hash 不一致测试。
- 同步更新 M1 Contract、ADR、README、PROJECT_STATUS 和本 change 的完整 Spec。

# Non-goals

- 不实现 PostgreSQL、Scheduler、Lease、Retry、Result Commit 或 Metric Aggregation；这些属于 M2。
- 不实现 Python/LangGraph Worker、gRPC Runner Protocol、真实 Model、Docker Sandbox、Object Storage 或 UI。
- 不提供 REST API；M1 以可脚本化 CLI 作为用户入口，API Wrapper 留到后续纵向切片。
- 不支持 `old_skill`、`ablation` 或多 Treatment 的完整编译语义；M1 只冻结标准 `without_skill`/`with_skill` 双臂。
- 不声称本地文件系统 Registry 是多进程生产存储；它是 M1 的可复现开发适配器，PostgreSQL Registry 留给后续实现。

# Acceptance examples

- A1：Go Module、`cmd/skillgate` 和 M1 规定的内部包可构建，`go test ./...` 通过。
- A2：同一 Skill Package 在字段顺序、文本换行和机器上得到相同 Canonical Identity；非法绝对路径、`..`、Symbolic Link、重复路径和缺少 `SKILL.md` 被拒绝。
- A3：Skill Registry 以内容 Hash 定位不可变 Version；重复注册相同内容幂等，注册不同内容不覆盖旧 Version，读取结果可复现。
- A4：Suite Registry 校验稳定 Case ID、`evaluationMode`/`population` 合法组合、Fixture/Schema/Expected 引用、Answer Key 隔离和内容 Hash，并保存不可变 Suite Version。
- A5：Compiler 能编译 M0 示例 Manifest，生成正确的 Baseline/Candidate Pair 数量、Trial 数量、Pair ID、Trial ID 和 Identity Tuple；非 Treatment 字段完全一致。
- A6：Compiler 对缺少 Baseline/Candidate、重复 Case/Arm、Pair Environment/Fixture/Model/Grader 不一致、非法 Mode、越界路径、Secret 字段、Expected 泄漏和 Hash 不一致返回稳定错误 Code，并且不生成部分计划。
- A7：CLI 默认人类输出，`--json` 输出版本化、稳定字段的诊断/编译结果；成功、用法错误、语义校验失败、Identity 冲突和 I/O 错误使用不同退出码。
- A8：Registry 和 Compiler 不依赖外部 Credential、网络、PostgreSQL 或 Python；所有 M1 Contract Test 使用仓库内 Synthetic Fixture。
- A9：M1 正式文档明确 Canonical Identity、Registry 生命周期、双臂限制、错误 Contract、幂等注册行为和后续 M2 边界；PROJECT_STATUS 记录 M1 结果和 M2 入口。

# Constraints and invariants

- PostgreSQL 是产品最终事实来源，但 M1 只能使用可替换的本地 Registry Adapter，不把本地文件实现描述为生产分布式存储。
- Baseline/Candidate 必须匹配；唯一 Treatment Difference 是 Skill Version。
- `trigger`/`answer` 与三种 `evaluationMode` 必须保持分离；Security Probe 不进入 Utility/Trigger 分母。
- Skill、Suite、Prompt、Fixture、Expected、Tool Output 和 Model Response 都是不可信输入。
- Canonical Identity 必须内容寻址、不可变、可重算；任何影响 Identity 的字段都必须进入 Hash 输入。
- 至少一次执行和幂等提交属于后续 M2；M1 只生成稳定 Logical Identity，不声称 Exactly-once。
- 代码、错误消息和文档描述性文字使用中文，代码标识符和协议字段保留英文。

# Decisions

- 当前 change 使用已创建的 `comet/m1-registry-manifest-compiler` 分支；Git 远端为 `origin`，工作区基线干净。
- M1 从 M0 归档产物开始，不修改 M0 归档内容。
- Go Module 路径采用远端仓库推断值 `github.com/Lin-xun1113/SkillGate`。
- Skill Package Identity 采用**规范化文件列表 JSON**：相对路径排序、文本换行统一为 LF、UTF-8 内容进入版本化 canonical JSON 后计算 SHA-256；不使用 TAR/ZIP 的时间戳或权限元数据。
- M1 首个用户入口只提供 CLI；REST/API Wrapper 留到后续纵向切片。
- Registry 使用本地文件系统 Content-Addressed Store，默认根目录为 `.skillgate/registry`，CLI 提供显式 `--registry-root` 覆盖；通过接口隔离，后续可替换为 PostgreSQL Adapter。
- M1 只支持标准 `without_skill`/`with_skill` 双臂和唯一 `skill_version` Treatment；`old_skill` 与 `ablation` 延后。
- YAML 解析使用 `gopkg.in/yaml.v3`；CLI 参数和 JSON 输出使用 Go 标准库，避免引入不必要的命令框架。
- Compiler 对 M0 Manifest 的本地引用先解析为内容 Hash，再计算 normalized Manifest Hash；绝不把机器绝对路径写入 Identity。
- 用户于 2026-08-19（UTC）重新确认 M1 收敛设计：Environment Identity 绑定执行 URI 与 Descriptor Hash；Skill Package 保持 `SKILL.md` 原始文本，仅规范 CRLF/CR 为 LF；Suite 引用采用 Declared Hash，验证引用内容与声明一致但不改变 M0 Pair 的引用声明 Identity。

# Open questions

- 无。用户已确认 M1 目标、范围、完整 Spec、A1–A9 验收标准，以及规范化文件列表 JSON、CLI-only、文件系统 CAS、双臂-only 四项设计决策。

# Verification expectations

- Go 单元、集成式文件系统和 Contract Fixture 测试；
- Canonicalization 属性测试或等价的多表示测试；
- 正常 M0 Manifest 编译 Golden Test；
- 每类稳定错误 Code 的负向测试；
- CLI 文本与 JSON 输出 Golden Test、退出码测试；
- `go test ./...`、`go vet ./...`、`go run ./cmd/skillgate --help` 和 M0 示例编译命令；
- 新 Verifier 只读检查完整 M1 Spec、实际代码、测试和 M0 回归结果。

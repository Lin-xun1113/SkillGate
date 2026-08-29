# CLI 参考

> 状态：与 2026-08-27 代码库同步 · 权威来源：`cmd/skillgate/main.go` 及各子命令实现

SkillGate 的主入口是单一二进制 `skillgate`（源码在 `cmd/skillgate`）。所有命令支持 `--json` 输出稳定契约格式（`version: skillgate.cli.v1`）。

## 命令总览

```text
skillgate skill validate|register <path> [--registry-root <path>] [--json]
skillgate suite validate|register <path> [--registry-root <path>] [--json]
skillgate compile <manifest> [--registry-root <path>] [--json]
skillgate db migrate|status [--database-url-file <path>] [--json]
skillgate experiment materialize <manifest>|cancel [options] [--json]
skillgate trial claim|start|heartbeat|complete [options] [--json]
skillgate scheduler sweep [--limit <n>] [--json]
skillgate serve [--grpc-addr <addr>] [--database-url-file <path>] [--json]
skillgate ui [--listen <addr>] [--database-url-file <path>] [--json]
```

## skill — Skill 包校验与注册

```bash
skillgate skill validate <dir>     # 校验 SKILL.md 并输出内容 Hash
skillgate skill register <dir>     # 校验 + 写入内容寻址 Registry
```

| 参数 | 默认 | 说明 |
|---|---|---|
| `--registry-root` | `.skillgate/registry` | Registry 根目录 |

行为：

- `validate` 输出 `valid=true hash=sha256:…`，不写任何状态；
- `register` 幂等：同一内容重复注册返回既有版本（`idempotent=true`），不会产生新版本；
- 校验失败返回稳定诊断码（见下文），例如缺 Frontmatter → `INVALID_SKILL_MANIFEST`，包含疑似密钥字段 → `SECRET_FIELD_PRESENT`。

## suite — Eval Suite 校验与注册

```bash
skillgate suite validate <suite.yaml>
skillgate suite register <suite.yaml>
```

参数与 `skill` 相同。校验覆盖：Case ID 唯一性（`DUPLICATE_CASE_ID`）、评估模式合法（`INVALID_EVALUATION_MODE`）、Fixture 引用存在且路径不出根（`PATH_OUTSIDE_ROOT`）等。

## compile — 实验编译

```bash
skillgate compile <manifest.yaml> [--registry-root <path>]
```

输出（人类格式）：

```text
compiled=true pairs=24 trials=48 manifest_hash=sha256:248b0c6d…
```

编译器执行的检查（任一失败即拒绝，`--json` 下带诊断数组）：

- 引用文件存在、Hash 匹配声明值（`CONTENT_HASH_MISMATCH`）；
- 两臂配对身份一致——Model/Prompt/Fixture/Environment/Grader 任一不一致 → `PAIR_IDENTITY_MISMATCH`；
- 缺少 Baseline 臂 → `MISSING_REQUIRED_ARM`；重复臂名 → `DUPLICATE_ARM`；
- Skill 含任务级答案 → `LEAKAGE_DETECTED`；
- Manifest 内含密钥样式字段 → `SECRET_FIELD_PRESENT`。

## db — 数据库迁移

```bash
skillgate db migrate --database-url-file <path>    # 执行嵌入式 goose 迁移（幂等）
skillgate db status  --database-url-file <path>    # 查看迁移状态
```

| 参数 | 默认 | 说明 |
|---|---|---|
| `--database-url-file` | `$SKILLGATE_DATABASE_URL_FILE` | PostgreSQL URL Secret 文件路径（推荐） |
| `--database-url` | `$SKILLGATE_DATABASE_URL` | 兼容无密码 URL；含密码 DSN 会拒绝 |
| `--timeout` | `30s` | 命令超时 |

迁移共 6 个（`00001_scheduler_core` → `00006_decision_actor_evidence`），重复执行无副作用。

## experiment — 物化与取消

### materialize

```bash
skillgate experiment materialize <manifest.yaml> [options]
```

编译 Manifest 并把 Trial 计划写入 PostgreSQL 队列。

| 参数 | 默认 | 说明 |
|---|---|---|
| `--database-url-file` | 环境变量 | 必需（直接或经 `SKILLGATE_DATABASE_URL_FILE`） |
| `--priority` | `0` | 队列优先级 |
| `--max-attempts` | 取 Manifest retry 配置 | 覆盖最大尝试次数 |
| `--trial-timeout` | 取 Manifest 执行配置 | 覆盖单次尝试超时 |
| `--budget-timeout` | `10m` | 实验整体预算超时 |
| `--backoff-base` | `1s` | 重试退避基数 |
| `--backoff-cap` | `30s` | 重试退避上限 |

### cancel

```bash
skillgate experiment cancel --experiment-id <id> [--actor <name>] [--reason <text>]
```

两阶段取消：先置 `CANCEL_REQUESTED`（停止新 Claim），运行中 Trial 由 Worker 心跳感知后协作式收尾。

## trial — 手动驱动 Trial 生命周期

调试/测试工具，通常由 Worker 程序代替执行。

```bash
skillgate trial claim     --worker-id w1 --lease-duration 30s
skillgate trial start     --trial-id <id> --worker-id w1 --lease-token-file <file>
skillgate trial heartbeat --trial-id <id> --worker-id w1 --lease-token-file <file> [--phase <p>] [--event-sequence <n>]
skillgate trial complete  --trial-id <id> --worker-id w1 --lease-token-file <file> \
                          --result-manifest <file.json> --idempotency-key <key> \
                          --request-hash <hash> --outcome SUCCEEDED
```

| 参数 | 说明 |
|---|---|
| `--worker-id` | Worker 标识 |
| `--lease-duration` | Claim 租约时长（默认 `30s`） |
| `--lease-token-file` | **Lease Token 必须从文件读取**，避免进入 shell 历史/日志 |
| `--lease-generation` | Fence 代数 |
| `--idempotency-key` | 幂等键；重复提交返回 `ALREADY_COMMITTED` |
| `--outcome` | `SUCCEEDED` / `FAILED` / `TIMED_OUT` / `CANCELLED` |
| `--category` | 失败类别（重试分类用） |
| `--request-hash` | Trial Request Hash 校验值 |

## scheduler sweep — 租约回收

```bash
skillgate scheduler sweep [--limit 100]
```

回收过期租约、按重试策略重新排队。生产中由 `serve` 内部驱动；独立命令便于运维手动触发和 cron 化。

## serve — Control Plane 服务

```bash
skillgate serve [--grpc-addr :50051] [--database-url-file <path>] \
                [--artifacts-dir ./artifacts] [--graders-dir ./graders] \
                [--grading-poll-interval 5s]
```

启动两件事：

1. **Runner gRPC 服务**（`:50051`）：Worker 注册/领取/心跳/事件/提交/失败上报（协议见 [Runner Protocol](../protocol/runner-protocol.md)）；
2. **评分轮询器**：每 `--grading-poll-interval` 检查进入 `GRADING` 状态的实验，执行评分 → 聚合 → 报告 → Release Decision。

| 参数 | 默认 | 说明 |
|---|---|---|
| `--grpc-addr` | `:50051` | gRPC 监听地址 |
| `--artifacts-dir` | `./artifacts` | Artifact 根目录（含路径穿越防护） |
| `--graders-dir` | `./graders` | Grader 定义目录（启动时加载注册表） |
| `--grading-poll-interval` | `5s` | 评分轮询周期 |

## ui — Web UI

```bash
skillgate ui [--listen :8080] [--database-url-file <path>]
```

只读 HTTP Server，两个页面：

- `/` 实验列表：状态、Trial 进度、更新时间；
- `/experiments/<id>` 详情：统计摘要（Lift/CI/显著性）、Case 结果表、Release Decision（含命中规则与证据链接），以及 404/500 错误页。

UI 通过共享 Store 只读查询 PostgreSQL，不做任何写操作；信号驱动的优雅关闭。

## 诊断与错误码

所有失败路径输出稳定诊断码。`--json` 模式下格式为：

```json
{"version":"skillgate.cli.v1","ok":false,"diagnostics":[{"code":"PAIR_IDENTITY_MISMATCH","severity":"error","path":"…","message":"…"}]}
```

### 校验/编译类

| 码 | 退出码 | 含义 |
|---|---|---|
| `INVALID_ARGUMENT` | 2 | 命令参数错误 |
| `FILE_NOT_FOUND` | 3 | 文件或目录不存在 |
| `PATH_OUTSIDE_ROOT` / `SECRET_FIELD_PRESENT` | 4 | 引用越界 / 检出疑似密钥 |
| `INVALID_SKILL_MANIFEST` | 5 | SKILL.md 结构非法 |
| `DUPLICATE_CASE_ID` / `DUPLICATE_ARM` / `MISSING_REQUIRED_ARM` / `INVALID_EVALUATION_MODE` / `UNSUPPORTED_TREATMENT` | 5 | Suite/Manifest 结构错误 |
| `PAIR_IDENTITY_MISMATCH` / `LEAKAGE_DETECTED` / `CONTENT_HASH_MISMATCH` | 6 | 配对身份不一致 / 泄漏 / Hash 不符 |
| `REGISTRY_CONFLICT` | 7 | Registry 冲突 |
| `IO_ERROR` / `DATABASE_UNAVAILABLE` / `MIGRATION_FAILED` | 8 | 环境/基础设施错误 |

### 调度类（退出码 9）

`IDENTITY_CONFLICT`、`OWNER_MISMATCH`、`LEASE_MISMATCH`、`LEASE_EXPIRED`、`STATUS_CONFLICT`、`RESULT_CONFLICT`、`LOGICAL_TRIAL_TERMINAL`、`EXPERIMENT_CANCEL_REQUESTED`、`RETRY_EXHAUSTED`、`NOT_CLAIMABLE`、`TRIAL_NOT_FOUND`——对应调度状态机的每种非法转移，详见[架构与原理](../overview/architecture.md#可靠性语义为什么不会重复计分)。

### 环境变量

| 变量 | 用途 |
|---|---|
| `SKILLGATE_DATABASE_URL_FILE` | 所有需要数据库的命令的默认 URL Secret 文件（推荐） |
| `SKILLGATE_DATABASE_URL` | 所有需要数据库的命令的兼容连接串 |

> 注意：`ui` 服务优先读取 `SKILLGATE_DATABASE_URL_FILE`，兼容 `SKILLGATE_DATABASE_URL`（Compose 中的用法见 [本地部署指南](../deployment/local-deployment.md)）。

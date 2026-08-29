# 配置参考

> 状态：与 2026-08-30 代码库同步 · 版本：v0.2 · 权威来源：各命令的 flag 解析代码与 `docs/contracts/secret-source.md`

SkillGate 刻意保持配置面很小：无配置文件，全部通过命令行参数 + 少量环境变量。

## 环境变量

| 变量 | 用于 | 说明 |
|---|---|---|
| `SKILLGATE_DATABASE_URL_FILE` | `db` / `experiment` / `trial` / `scheduler` / `serve` / `ui` | **推荐**；读取 Docker Secret 文件，值不进入命令行 |
| `SKILLGATE_DATABASE_URL` | 同上 | 兼容环境变量；生产优先使用 `_FILE` |
| `DATABASE_URL_FILE` / `DATABASE_URL` | `deploy/run-demo.sh` 与兼容入口 | 旧变量别名；主变量配置错误时不会被静默覆盖 |
| `SKILLGATE_POSTGRES_PASSWORD_FILE` | Compose `postgres` | Production override 必填的 Docker Secret 文件路径 |
| `SKILLGATE_POSTGRES_USER` / `SKILLGATE_POSTGRES_DB` | Compose `postgres` | 非 Secret 标识，可在 Demo 中覆盖 |
| `RUNNER_GRPC_ADDR` | Compose Worker | Worker 连接的 Control Plane 地址 |
| `ARTIFACTS_ROOT` | Compose Worker | Worker 的产物根目录 |
| `OPENAI_API_KEY` / `ANTHROPIC_API_KEY` | LangGraph Worker | 可选真实 Provider Credential；不进入 Manifest 或 Trace |
| `OPENAI_API_KEY_FILE` / `ANTHROPIC_API_KEY_FILE` | LangGraph Worker | 推荐 Docker Secret 文件来源；与同名环境变量同时存在时文件优先 |
| `OPENAI_BASE_URL` / `ANTHROPIC_BASE_URL` | LangGraph Worker | Operator-owned Provider Endpoint；不能被 Manifest 覆盖 |
| `LIVE_PROVIDER_SMOKE` | Python Test | 设为 `1` 才运行真实 Provider Adapter 冒烟；默认跳过 |
| `LIVE_PROVIDER` / `LIVE_PROVIDER_MODEL` | Python Test | 冒烟所用 Provider 与精确模型名；不推断默认真实模型 |
| `LIVE_PROVIDER_MAX_TOKENS` | Provider Smoke | output token 上限，默认 `64`，只能降低 |
| `LIVE_PROVIDER_TIMEOUT_SECONDS` | Provider Smoke | 单次调用超时上限，默认 `30s`，只能降低 |
| `LIVE_PROVIDER_MAX_COST_USD` | Provider Smoke | 成本上限，默认 `0.05`，只能降低 |
| `LIVE_PROVIDER_FULL_CHAIN` | Provider Smoke | 设为 `1` 才执行 full-chain preflight marker；不会改动 48-trial Fixture Demo |

连接串格式：

```text
postgres://<user>:<password>@<host>:<port>/<db>?sslmode=disable
```

## 命令默认值速查

| 命令 | 参数 | 默认值 |
|---|---|---|
| `serve` | `--grpc-addr` | `:50051` |
| 所有 DB 命令 | `--database-url-file` | 无；也可读取 `SKILLGATE_DATABASE_URL_FILE` |
| `serve` | `--artifacts-dir` | `./artifacts` |
| `serve` | `--graders-dir` | `./graders` |
| `serve` | `--grading-poll-interval` | `5s` |
| `ui` | `--listen` | `:8080` |
| `trial claim` | `--lease-duration` | `30s` |
| `experiment materialize` | `--budget-timeout` | `10m` |
| `experiment materialize` | `--backoff-base` / `--backoff-cap` | `1s` / `30s` |
| `experiment materialize` | `--max-attempts` | Manifest retry 配置（缺省 2） |
| `experiment materialize` | `--trial-timeout` | Manifest 执行配置（缺省 60s） |
| `scheduler sweep` | `--limit` | `100` |
| 通用 | `--timeout`（DB 命令） | `30s` |
| 通用 | `--registry-root` | `.skillgate/registry` |

## 数据库 Schema 版本

6 个顺序迁移（goose 管理，`db migrate` 幂等）：

| 迁移 | 内容 |
|---|---|
| `00001_scheduler_core` | Experiment / Logical Trial / Attempt / Result 核心表 |
| `00002_runner_protocol` | Worker Session、事件流、幂等提交支持 |
| `00003_grading_support` | 评分结果与报告持久化 |
| `00004_grading_identity` | 评分身份字段 |
| `00005_release_gate` | `metrics_snapshots`、`release_decisions` |
| `00006_decision_actor_evidence` | Decision 的 Actor 与 EvidenceLinks 审计字段 |

## 端口约定

| 端口 | 服务 |
|---|---|
| 50051 | Runner gRPC（`serve`） |
| 8080 | Web UI（`ui`） |
| 5432 | PostgreSQL |

## 超时与重试的层次

系统中存在多层超时，从外到内：

| 层 | 配置位置 | 缺省 |
|---|---|---|
| 实验整体预算 | `experiment materialize --budget-timeout` | `10m` |
| 单次尝试超时 | Manifest `execution.timeoutSeconds` / `--trial-timeout` | `60s` |
| Worker 租约 | `trial claim --lease-duration` / RegisterWorker 响应 | `30s` |
| 重试退避 | `--backoff-base` / `--backoff-cap`（指数 + Full Jitter） | `1s` / `30s` |
| 最大尝试次数 | Manifest `retry.maxAttempts` / `--max-attempts` | `2` |
| 单次 Provider HTTP 超时 | Manifest `model.config.timeout_seconds` | `60s` |
| Provider SDK 内重试 | Manifest `model.config.max_retries` | `0` |

## 密钥与凭据约定

- **Lease Token 通过文件传递**（`--lease-token-file`），不进命令行参数——避免进入 shell 历史与进程列表；
- PostgreSQL URL 使用 `SKILLGATE_DATABASE_URL_FILE`/`DATABASE_URL_FILE`；`--database-url-file` 只传递路径。旧 `--database-url` 仅允许无密码 URL，含密码 DSN 会返回 `INVALID_ARGUMENT`；
- Secret Source 按 `NAME_FILE` → `NAME` 读取，文件优先且不静默回退。缺失、空值、不可读和非法名称分别返回 `SECRET_MISSING`、`SECRET_EMPTY`、`SECRET_UNREADABLE`、`SECRET_INVALID_NAME`；
- Skill/Suite/Manifest 内容中出现疑似密钥会被校验拒绝（`SECRET_FIELD_PRESENT`）；
- 仓库不存放任何真实 Provider Key；Fixture/Mock Provider 让核心流程零凭据运行。
- Provider Endpoint 只能通过 Worker 进程环境的 `OPENAI_BASE_URL` /
  `ANTHROPIC_BASE_URL` 配置，不能写入 Manifest；这是防止不可信输入把
  Credential 重定向到攻击者地址的硬边界。
- `workers/python/tests/test_provider_live.py` 默认跳过真实调用；`LIVE_PROVIDER_SMOKE=1` 且缺 Key 时 Fail Closed。`./scripts/provider-live-smoke.sh` 同时执行硬上限 preflight。
- `LIVE_PROVIDER_FULL_CHAIN=1` 当前只记录需要 Operator 授权的临时 matched Manifest/Egress/预算，不等同完整 gRPC、Grading、Report E2E。
- 详细契约见 [`provider-runtime.md`](../../contracts/provider-runtime.md)。
- Go/Python 统一 Secret Source 见 [`secret-source.md`](../../contracts/secret-source.md)。

## Compose Profile

- **Local Insecure Demo（默认）**：`docker compose -f deploy/docker-compose.yml up --build`。仅使用 `deploy/secrets/demo-*` 合成凭据和离线 Fixture；这些值只为可复现演示，不能用于生产。
- **Production**：必须提供外部 `SKILLGATE_POSTGRES_PASSWORD_FILE` 与 `SKILLGATE_DATABASE_URL_FILE`，再使用 `deploy/docker-compose.production.yml` 和 `--profile production`。Production 配置没有默认密码；Provider Key 另以 `OPENAI_API_KEY_FILE`/`ANTHROPIC_API_KEY_FILE` 注入 Worker。
- 两种 Profile 都不会把 Secret 值写入服务 `command`；Secret 文件路径可见，文件内容不可见。当前轮换需要重启相关服务，尚未实现热更新。

## JSON 输出契约

所有命令 `--json` 模式遵循统一信封：

```jsonc
// 成功
{ "version": "skillgate.cli.v1", "ok": true, …命令特定字段 }

// 失败
{ "version": "skillgate.cli.v1", "ok": false,
  "diagnostics": [ { "code": "…", "severity": "error", "path": "…", "message": "…" } ] }
```

版本字段允许脚本安全地做契约断言。错误码与退出码映射见 [CLI 参考](../usage/cli.md#诊断错误码)。

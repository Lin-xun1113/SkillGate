# Secret Source Contract

**状态：** `IMPLEMENTED`（2026-08-30 UTC）
**版本：** `secret-source.v1`

## 目的

Secret 是运行时由 Operator 提供的输入，不属于 Experiment Manifest、`execution_hash`、Runner Request、日志、Trace、数据库记录或 Artifact。该 Contract 只定义最小读取边界，不声称已经接入 Secret Manager、KMS 或云厂商 Vault。

## 来源与优先级

给定规范环境变量名 `NAME` 时，Resolver 按以下顺序读取：

1. `NAME_FILE`：读取文件内容；文件路径本身可以出现在部署配置，但 Secret 值不能出现在命令参数或 Compose `command`；
2. `NAME`：读取环境变量值；
3. 两者都未配置时返回 `SECRET_MISSING`。

`NAME_FILE` 一旦配置即为权威来源。路径为空、文件不可读或文件内容为空不会回退到 `NAME`，以免轮换/挂载错误被静默掩盖。值会去除首尾空白和 Secret 文件末尾换行。

Go 实现：`internal/secrets`；Python Worker 实现：`workers/python/langgraph_worker/secret_source.py`。两端都提供可注入的读取函数用于离线测试。

## 稳定错误

| Code | 含义 | 是否允许重试原请求 |
|---|---|---|
| `SECRET_MISSING` | 未配置 `NAME` 或 `NAME_FILE` | 否，先补配置 |
| `SECRET_EMPTY` | 值、路径或文件内容为空 | 否，先修配置 |
| `SECRET_UNREADABLE` | 文件不存在、权限不足或编码不可读 | 否，先修挂载/权限 |
| `SECRET_INVALID_NAME` | 环境变量名不符合 `[A-Za-z_][A-Za-z0-9_]*` | 否，修正部署配置 |

错误消息不包含 Secret 值、文件内容、完整路径或底层 SDK/文件错误。Provider 初始化会将这些错误映射为 `ProviderConfigurationError`；CLI 数据库配置映射为同名稳定诊断码。

## 使用边界

- Provider Key：`OPENAI_API_KEY[_FILE]`、`ANTHROPIC_API_KEY[_FILE]`，只由 Worker Operator 环境读取；Manifest 的 `environment` Descriptor 不是 Secret Source。
- PostgreSQL：`SKILLGATE_DATABASE_URL[_FILE]`，兼容 `DATABASE_URL[_FILE]`。Compose 服务只挂载 URL Secret 文件；CLI 的 `--database-url` 保留兼容但拒绝含密码 DSN。
- Lease Token 继续使用已有 `--lease-token-file`，不得改回命令行明文。
- Resolver 返回值只在进程内存中用于连接/SDK 初始化；调用方不得把它写入 Identity、Result Manifest、Event Payload、Trace、Report 或日志。
- 记录异常时必须使用现有 `redact_sensitive` 或等价脱敏；脱敏不能替代撤销和轮换。

## 验证

```bash
go test ./internal/secrets ./cmd/skillgate
PYTHONPATH=workers/python:workers/python/gen workers/python/.venv/bin/python -m pytest -q workers/python/tests/test_secret_source.py workers/python/tests/test_provider_contract.py
bash scripts/verify-secrets.sh
```

`scripts/verify-secrets.sh` 只使用合成 Sentinel，检查 Git 跟踪文件、CLI 诊断、本地产物树和 Docker build-context 规则；Docker daemon 不可用时只报告静态检查，不伪造镜像验证结论。

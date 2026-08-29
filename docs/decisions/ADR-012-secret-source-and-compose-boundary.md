# ADR-012：Secret Source 与 Compose 凭据边界

**状态：** Accepted（2026-08-30 UTC）
**版本：** v1
**适用范围：** Go CLI/Control Plane、Python Worker、Docker Compose Demo/Production

## 背景

旧 Compose 将 PostgreSQL 密码写在 `environment`、连接串和 `command` 中，容易出现在 `docker inspect`、Shell 历史和进程列表。Provider Key 虽已由 P0 从 Operator 环境注入，但缺少跨 Go/Python 的统一文件来源与稳定错误 Contract。项目许可证已由所有者另行确认并记录在根目录 `LICENSE`。

## 决策

1. 采用 `NAME_FILE` 优先、`NAME` 兼容的最小 Secret Source；文件优先且错误不静默回退。
2. Go `internal/secrets` 与 Python `secret_source.py` 使用同一错误码和去空白规则；错误不回显值、文件内容、完整路径或底层原因。
3. Compose 默认形态明确命名为 Local Insecure Demo，只使用仓库内合成 Secret 文件和离线 Fixture；该凭据不可用于生产。
4. Production 使用 `deploy/docker-compose.production.yml` override，要求 Operator 提供 `SKILLGATE_POSTGRES_PASSWORD_FILE` 与 `SKILLGATE_DATABASE_URL_FILE`，并通过 `--profile production` 启动。Production 不提供默认密码。
5. 服务命令不携带 DSN；Control Plane、Bootstrap、UI 仅接收 `/run/secrets/database_url` 路径。CLI 旧 `--database-url` 仅允许无密码 URL，含密码值 Fail Closed。
6. `.dockerignore` 排除 `.env`、Secret/凭据文件、密钥和本地产物；扫描脚本只使用合成 Sentinel，不读取或上传真实数据。
7. 项目采用 MIT 许可证，版权主体为 `Lin-xun1113`；依赖许可证检查作为本地手动检查，不接入 CI。

## 后果

- 连接/Provider 代码仍会在进程内存中短暂持有 Secret，这是外部系统正常工作所需；不声称已达到 HSM/Secret Manager 级别保护。
- Demo 启动保持离线可复现，但任何部署到共享/生产环境都必须使用 Production override 和外部 Secret 文件。
- CLI/Compose 兼容性保留了 `SKILLGATE_DATABASE_URL`、`DATABASE_URL` 和无密码 `--database-url`；含密码旧脚本需要迁移到 `_FILE`。
- 轮换依赖重启/重建 Worker 或服务以重新读取文件；当前没有热更新 Watcher，这是个人项目选择的简单运行方式。

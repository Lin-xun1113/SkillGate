# 本地部署指南

> 状态：与 2026-08-30 代码库同步 · 版本：v0.2 · 权威来源：`deploy/docker-compose.yml`、`deploy/docker-compose.production.yml`、`deploy/compose-bootstrap.sh`

## 两种部署形态

| 形态 | 命令 | 适合 |
|---|---|---|
| Docker Compose 一键 | `docker compose -f deploy/docker-compose.yml up --build` | 演示、验收、端到端体验 |
| 本地进程 | `db migrate` → `experiment materialize` → `serve` → `ui` + Worker | 开发调试 |

## Compose 服务拓扑

```text
┌──────────┐     ┌────────────┐     ┌───────────────┐     ┌────────────────┐     ┌─────────┐
│ postgres │────▶│  bootstrap │────▶│ control-plane │◀────│langgraph-worker│     │   ui    │
│  :5432   │     │ (迁移+编译+ │     │  serve :50051 │     │ 消费 48 Trials │     │ :8080   │
│          │     │  物化)      │     │ + 评分轮询     │     └────────────────┘     └─────────┘
└──────────┘     └────────────┘     └───────────────┘
```

启动依赖链（`depends_on` 条件）：

1. `postgres` 健康检查通过（`pg_isready`）；
2. `bootstrap` 跑完退出（成功完成迁移、注册、物化）；
3. `control-plane` 与默认的 `langgraph-worker` 启动；
4. `ui` 在 control-plane 启动后启动。

### 各服务职责

| 服务 | 镜像 | 命令 | 说明 |
|---|---|---|---|
| `postgres` | `postgres:17-alpine` | Docker Secret 文件 | 默认是 Local Insecure Demo 合成凭据；Production 必须使用外部文件 |
| `bootstrap` | `deploy/Dockerfile.server` 构建 | `compose-bootstrap.sh` | `db migrate` + CAS/Grader 物化 + `experiment materialize`，一次性任务 |
| `control-plane` | 同上 | `serve --grpc-addr 0.0.0.0:50051 …` | Runner gRPC + 评分轮询器（单进程） |
| `langgraph-worker` | `workers/python/langgraph_worker/Dockerfile` 构建 | `--max-trials 48` | 默认 gRPC 消费者；示例 Manifest 的 `fixture` Provider 在 Worker 内确定性执行 |
| `fixture-worker` | `deploy/Dockerfile.worker` 构建 | `--max-trials 48 --delay 0.02` | 可选协议 Fixture Worker（Compose profile=`fixture`） |
| `ui` | `deploy/Dockerfile.server` 构建 | `ui --listen 0.0.0.0:8080` | 只读 HTTP Server |

### 数据卷

| 卷 | 挂载 | 用途 |
|---|---|---|
| `artifacts` | control-plane / langgraph-worker / ui 共享 | Trial 产物与报告（Worker 写，评分读，UI 展示） |
| `graders` | control-plane | Grader 定义目录（`serve --graders-dir`） |
| `cas` | bootstrap / control-plane / langgraph-worker 共享 | 编译 Manifest 与 Skill 的只读 CAS |

> `docker compose down` 保留卷；`docker compose down -v` 连数据一起清。

### Secret Profile

默认命令是 **Local Insecure Demo**，其 `deploy/secrets/demo-*` 仅用于离线 Fixture，不是生产配置：

```bash
docker compose -f deploy/docker-compose.yml up --build
```

Production 必须由 Operator 准备两个文件（权限建议 `0600`），并通过 override 启动：

```bash
export SKILLGATE_POSTGRES_PASSWORD_FILE=/run/operator/postgres-password
export SKILLGATE_DATABASE_URL_FILE=/run/operator/database-url
docker compose -f deploy/docker-compose.yml \
  -f deploy/docker-compose.production.yml --profile production up --build
```

服务命令只携带 `/run/secrets/database_url` 路径，不携带 DSN；文件内容不会进入 `docker inspect` 的 `command`。Provider Key 使用 `OPENAI_API_KEY_FILE`/`ANTHROPIC_API_KEY_FILE` 注入 Worker。完整读取/错误 Contract 见 [`../../contracts/secret-source.md`](../../contracts/secret-source.md)。

### 端口

| 宿主端口 | 服务 |
|---|---|
| `5432` | PostgreSQL（调试直连用） |
| `50051` | gRPC（外部 Worker 接入用） |
| `8080` | Web UI |

## 典型运维操作

### 从零重跑

```bash
docker compose -f deploy/docker-compose.yml down -v
docker compose -f deploy/docker-compose.yml up --build
```

### 换一个实验

编辑 `deploy/compose-bootstrap.sh` 中 materialize 的 Manifest 路径后重新 up。

### 看执行进度

```bash
docker compose -f deploy/docker-compose.yml logs -f langgraph-worker  # Trial 消费日志
docker compose -f deploy/docker-compose.yml logs -f control-plane    # gRPC + 评分日志
```

UI 列表页 (`:8080`) 也会实时反映实验状态（页面刷新模式，无 WebSocket）。

### 外部 Worker 接入 Compose 集群

gRPC 端口已暴露，本地起的 Worker 可直接连：

```bash
cd workers/python
SKILLGATE_EVALS_ROOT="../../evals/csv-analysis" PYTHONPATH=".:gen" python3 -m langgraph_worker \
  --server 127.0.0.1:50051 --worker-id local-dev-01 \
  --cas ../../.skillgate/cas --artifacts ../../artifacts
```

注意 Worker 需要与容器共享 artifacts 视图时，要用相同卷挂载或改用本地进程形态。

### 数据库直查

```bash
docker exec -it skillgate-postgres psql -U skillgate -d skillgate -c '\dt'
docker exec -it skillgate-postgres psql -U skillgate -d skillgate \
  -c 'SELECT experiment_id, status FROM experiments;'
```

关键表：`experiments`、逻辑 Trial/Attempt/Result（scheduler core）、`trial_results`（grades）、`reports`、`metrics_snapshots`、`release_decisions`。

## 本地进程形态的目录约定

不使用 Compose 时的默认相对路径（工作目录相关，注意在仓库根执行）：

```text
.skillgate/registry/    # skill/suite register 的 CAS
./artifacts/            # serve 的产物与报告（--artifacts-dir）
./graders/              # serve 的 Grader 定义（--graders-dir）
```

## 安全注意事项（本地演示边界）

- Compose 默认的 PostgreSQL 凭据是**合成演示值**，不要用于任何真实数据；
- `sslmode=disable` 仅限本地；
- 沙箱容器由 Control Plane 按 Trial 创建（网络隔离、只读根、非 root），但 SkillGate 不声称沙箱是完美安全边界——详见[已知限制](../misc/limitations.md)与 `docs/architecture/threat-model.md`。
- 项目采用 MIT 许可证，版权主体为 `Lin-xun1113`，详见根目录 [`LICENSE`](../../LICENSE)；第三方依赖检查通过 [`../../operations/license-management.md`](../../operations/license-management.md) 中的本地命令执行。

## 故障排查

| 现象 | 排查 |
|---|---|
| bootstrap 反复失败 | `docker compose logs bootstrap`；多数是迁移错误或 Manifest 编译诊断 |
| Worker 领不到 Trial | bootstrap 是否成功？`experiments` 表状态；`RUNNER_GRPC_ADDR` 是否正确 |
| UI 打不开 | `ui` 服务日志；`SKILLGATE_DATABASE_URL_FILE` 是否指向同一 DB |
| 报告不生成 | control-plane 日志的评分轮询输出；`--grading-poll-interval`；`graders` 卷内容 |
| 端口冲突 | 修改 compose 的宿主侧端口映射 |

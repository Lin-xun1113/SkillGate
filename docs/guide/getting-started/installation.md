# 安装与快速开始

> 状态：与 2026-08-28 代码库同步

## 环境要求

| 组件 | 版本要求 | 用途 |
|---|---|---|
| Go | 1.26+ | 构建 CLI 与 Control Plane |
| PostgreSQL | 17+ | 实验状态存储（完整流程需要） |
| Docker & Docker Compose | 现代稳定版 | 一键演示与沙箱执行 |
| Python | 3.11+ | Worker（可选，仅自建/调试 Worker 时需要） |

> 只想验证编译与校验功能？只有 Go 是必需的——Skill/Suite 校验、Manifest 编译、单元测试都不需要数据库。

## 获取与构建

```bash
git clone https://github.com/Lin-xun1113/SkillGate.git
cd SkillGate

# 编译 CLI
go build -o skillgate ./cmd/skillgate

# 查看帮助
./skillgate --help
```

## 路径 A：五分钟无数据库体验

体验核心的「内容寻址 + 配对编译」能力：

```bash
# 1. 校验内置示例 Skill
go run ./cmd/skillgate skill validate skills/csv-analysis
# 输出: valid=true hash=sha256:617fae44…

# 2. 注册到本地 Registry（写入 .skillgate/registry/）
go run ./cmd/skillgate skill register skills/csv-analysis

# 3. 编译内置示例实验：8 Case × 2 臂 × 3 重复 = 48 Trial
go run ./cmd/skillgate compile experiments/csv-analysis-v1-demo.yaml
# 输出: compiled=true pairs=24 trials=48 manifest_hash=sha256:248b0c6d…
```

编译器此时已经完成了：引用完整性检查、两臂配对身份一致性验证、泄漏检查、Hash 冻结。任何一项不过都会给出带稳定错误码的诊断（见 [CLI 参考](../usage/cli.md#诊断错误码)）。

## 路径 B：Docker Compose 一键端到端

一条命令拉起完整链路：PostgreSQL → 迁移+编译/CAS 物化（bootstrap）→ gRPC Control Plane → LangGraph Worker（fixture Provider）消费全部 48 个 Trial → Web UI。

```bash
docker compose -f deploy/docker-compose.yml up --build
```

服务拓扑：

| 服务 | 端口 | 职责 |
|---|---|---|
| `postgres` | 5432 | 状态存储 |
| `bootstrap` | —（跑完退出） | 数据库迁移 + 注册 + 物化实验 |
| `control-plane` | 50051 (gRPC) | Runner 服务 + 评分轮询 |
| `langgraph-worker` | — | 领取并确定性执行 48 个 Trial |
| `fixture-worker`（可选 profile） | — | 仅用于协议层 Fixture Worker 验证 |
| `ui` | **8080 (HTTP)** | 实验列表与报告 |

跑完后打开浏览器：

```bash
open http://localhost:8080
```

- 实验列表页：全部实验的状态与基本信息；
- 实验详情页：统计摘要（Lift、CI、显著性）、Case 结果表、Release Decision。

查看产物：

```bash
docker compose -f deploy/docker-compose.yml logs langgraph-worker  # Trial 执行日志
docker compose -f deploy/docker-compose.yml down                  # 清理（保留卷）
docker volume rm skillgate_artifacts                              # 彻底清理产物
```

## 路径 C：本地进程逐步执行（适合调试）

需要本地 PostgreSQL。`deploy/run-demo.sh` 演示了完整序列；数据库 URL 请写入本地权限受控文件并通过 `SKILLGATE_DATABASE_URL_FILE` 提供（不要放入命令行）：

```bash
export SKILLGATE_DATABASE_URL_FILE=/run/operator/database-url

# 1. 建库（示例）
createdb skillgate_test 2>/dev/null || true

# 2. 数据库迁移
go run ./cmd/skillgate db migrate --database-url-file "$SKILLGATE_DATABASE_URL_FILE"

# 3. 注册 Skill（可选，materialize 会按路径读取；注册后走 CAS）
go run ./cmd/skillgate skill register skills/csv-analysis

# 4. 物化实验：编译 + 写入 48 个 Trial 到队列
go run ./cmd/skillgate experiment materialize experiments/csv-analysis-v1-demo.yaml --database-url-file "$SKILLGATE_DATABASE_URL_FILE"

# 5. 准备 Worker 只读 CAS（Manifest、Skill 内容寻址物化）
go run ./cmd/skillgate cas prepare experiments/csv-analysis-v1-demo.yaml \
  --project-root . --cas-dir ./.skillgate/cas

# 6. 启动 Control Plane（gRPC :50051 + 评分轮询，前台运行）
go run ./cmd/skillgate serve --grpc-addr 127.0.0.1:50051 --database-url-file "$SKILLGATE_DATABASE_URL_FILE"

# 7. 另开终端，运行 LangGraph Worker（离线 Fixture Provider）
cd workers/python
python3 -m venv .venv && .venv/bin/pip install -r requirements-dev.txt
SKILLGATE_EVALS_ROOT="../../evals/csv-analysis" PYTHONPATH=".:gen" .venv/bin/python3 -m langgraph_worker \
  --server 127.0.0.1:50051 --worker-id demo-worker-01 --max-trials 48 \
  --cas ../../.skillgate/cas --artifacts ../../artifacts

# 8. 新开终端，从仓库根目录启动 Web UI 查看结果
cd ../..
go run ./cmd/skillgate ui --listen 127.0.0.1:8080 --database-url-file "$SKILLGATE_DATABASE_URL_FILE"
```

> 也可以不写任何脚本直接驱动生命周期：`trial claim` → `trial start` → `trial heartbeat` → `trial complete`，见 [CLI 参考](../usage/cli.md)。

## 运行测试

```bash
# 快速单元测试（无外部依赖）
go test -short ./...

# 全量构建检查
go build ./... && go vet ./...

# 真实 PostgreSQL 集成测试（需要数据库）
go test ./internal/store/postgres/

# M0 示例数据自校验
npm run validate:m0

# Python Worker 离线契约测试（需要先安装 workers/python/requirements-dev.txt）
PYTHONPATH="workers/python:workers/python/gen" workers/python/.venv/bin/pytest workers/python/tests -q
```

## 下一步

- 跟着[编写你的第一个实验](first-experiment.md)从零构建自己的 Skill 与评测；
- 或先浏览 [CLI 参考](../usage/cli.md)了解全部命令。

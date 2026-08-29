# Docker 部署

权威 Docker Compose 配置位于：

```
deploy/docker-compose.yml
```

## 快速开始

```bash
cd deploy
docker compose up --build
```

这会启动：
- PostgreSQL 数据库
- Bootstrap 服务（迁移、CAS/Grader 准备与实验物化）
- Control Plane gRPC 服务（端口 50051）
- LangGraph Worker（使用离线 Fixture Provider 执行 Trial）
- Web UI（端口 8080）

## 架构

部署使用：
- `deploy/Dockerfile.server`：构建 Control Plane 与 UI
- `workers/python/langgraph_worker/Dockerfile`：构建默认 LangGraph Worker
- `deploy/Dockerfile.worker`：构建可选协议 Fixture Worker（Compose profile=`fixture`）
- `deploy/compose-bootstrap.sh`：初始化脚本

## 历史说明

项目早期曾在仓库根目录放置 `docker-compose.yml` 与 `docker-compose.yaml`，因引用过时命令和不存在的 Dockerfile 已移除。当前部署配置统一维护在 `deploy/` 目录。

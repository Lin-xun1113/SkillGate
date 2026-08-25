# 本地开发手册

**状态：** `FUTURE → M2`；`PROPOSED`，实现开始后逐步验证

## 1. 本地依赖目标

MVP 希望通过 Docker Compose 启动：

```text
PostgreSQL
MinIO
可选 OTel Collector
SkillGate Go API/Scheduler
Python LangGraph Worker
```

为了让 CI 不依赖外部服务，Go 和 Worker 都应提供 Fixture Mode。

## 2. 推荐启动顺序

1. 启动 PostgreSQL 和 MinIO；
2. 执行 Migration；
3. 启动 Go API；
4. 启动 Scheduler；
5. 启动 Stub Worker；
6. 使用 Example Manifest 创建 Experiment；
7. 确认 Stub E2E 通过；
8. 再启动真实 LangGraph Worker。

## 3. 本地配置原则

- 配置文件只保存非 Secret 参数；
- Provider Key 通过环境变量或本地 Secret Manager 注入；
- 默认使用 Fixture Provider；
- 默认关闭真实外网访问；
- 默认限制 Trial 并发和成本；
- 配置启动时打印有效配置摘要，但不打印 Secret。

## 4. 推荐环境变量

```text
SKILLGATE_ENV=local
SKILLGATE_DATABASE_URL=postgres://...
SKILLGATE_OBJECT_STORE_ENDPOINT=http://localhost:9000
SKILLGATE_OBJECT_STORE_BUCKET=skillgate
SKILLGATE_RUNNER_PROTOCOL_VERSION=runner.v1
SKILLGATE_MODEL_MODE=fixture
SKILLGATE_SANDBOX_MODE=docker-restricted
OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4317
```

变量名只是当前设计建议，代码落地时以配置 Contract 为准。

## 5. 本地验证命令（计划）

```bash
# 计划中的命令，代码实现前不要假设已经存在
go test ./...
python -m pytest workers/langgraph/tests
skillgate validate ./examples/experiments/csv-analysis.yaml
skillgate compile ./examples/experiments/csv-analysis.yaml
skillgate run ./examples/experiments/csv-analysis.yaml --mode fixture
skillgate report <experiment-id>
```

## 6. 本地数据清理

只允许清理本地 Synthetic Data 和生成 Artifact。清理前确认：

- 没有需要保留的故障报告；
- 没有使用真实用户数据；
- 不会删除已提交到 Git 的 Schema/Migration；
- 不会删除用于复现 Release Decision 的 Evidence。

## 7. 常见问题

### Worker 没有领取 Trial

检查：

- Worker Capability 是否匹配 Harness/Sandbox Profile；
- Trial 是否已被其他 Worker Lease；
- Lease 是否过期；
- Budget 是否允许继续；
- Scheduler 和 DB 是否处于同一环境。

### Report 显示 Incomplete

从 Report 下钻到：

```text
Experiment → Trial → Attempt → Artifact Manifest → Event Sequence
```

不要直接把 Incomplete 改成失败或成功；先判断缺失的是执行证据、Grader 证据还是统计覆盖。

### 真实 Model Call 失败

先用 Fixture Provider 复现控制流程，再检查：

- Provider Credential；
- Model Name；
- Rate Limit；
- Endpoint；
- Timeout；
- 是否被 Sandbox Network Policy 拦截。

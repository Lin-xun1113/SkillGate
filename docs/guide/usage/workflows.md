# 常见工作流

> 状态：与 2026-08-27 代码库同步

本文按使用者目标组织操作序列。命令细节见 [CLI 参考](cli.md)。

## 工作流 1：评估一个新 Skill 版本

**目标**：回答「Skill v2 比 v1 好吗」。

```bash
# 1. 注册两个版本（内容寻址自动区分）
skillgate skill register skills/csv-analysis           # → v1 hash
# …修改 SKILL.md…
skillgate skill register skills/csv-analysis           # → v2 hash（新 Hash）

# 2. 为 v2 建实验 Manifest：candidate 策略的 skills.version 指向 v2 hash
cp experiments/csv-analysis-v1-demo.yaml experiments/csv-analysis-v2.yaml
# 编辑 experiments/csv-analysis-v2.yaml：
#   - skills[0].version = <v2 hash>
#   - metadata.name = csv-analysis-v2-demo
#   - 可选：增加 old_skill 臂直接对比 v1/v2

# 3. 校验 + 编译 + 物化
skillgate compile experiments/csv-analysis-v2.yaml
skillgate experiment materialize experiments/csv-analysis-v2.yaml --database-url-file "$SKILLGATE_DATABASE_URL_FILE"

# 4. 跑 Worker、看报告（见工作流 3/4）
```

对照读数：`summary.mean_lift`（v2 − v1 的方向）、`decision.failed_conditions`（哪些门槛没过）。

## 工作流 2：注册与版本管理

```bash
# 校验但不落库（CI 中常用）
skillgate skill validate skills/csv-analysis --json
skillgate suite validate evals/csv-analysis/suite.yaml --json

# 注册（幂等，同一内容永远得到同一 hash）
skillgate skill register skills/csv-analysis
skillgate suite register evals/csv-analysis/suite.yaml

# Registry 布局（默认 .skillgate/registry/）
#   skills/<name>/<hash>/   ← 不可变快照
#   suites/<name>/<hash>/
```

要点：

- Hash 是身份——改一个字节就是新版本，旧版本原样保留；
- Experiment Manifest 通过 `version: sha256:…` 精确引用版本，Registry 中该内容被篡改会在编译/执行期被 `CONTENT_HASH_MISMATCH` 拒绝；
- 想重置 Registry 直接删除 `.skillgate/registry/`，重新注册即可。

## 工作流 3：跑一次完整实验（本地进程）

```bash
export SKILLGATE_DATABASE_URL_FILE=/run/operator/database-url
skillgate db migrate --database-url-file "$SKILLGATE_DATABASE_URL_FILE"
skillgate experiment materialize experiments/csv-analysis-v1-demo.yaml --database-url-file "$SKILLGATE_DATABASE_URL_FILE"

# 终端 A：Control Plane（gRPC + 评分轮询）
skillgate serve --grpc-addr 127.0.0.1:50051 --database-url-file "$SKILLGATE_DATABASE_URL_FILE"

# 终端 B：LangGraph Worker（离线 Fixture Provider，消费全部 Trial）
cd workers/python && SKILLGATE_EVALS_ROOT="../../evals/csv-analysis" PYTHONPATH=".:gen" python3 -m langgraph_worker \
  --server 127.0.0.1:50051 --worker-id w-01 --max-trials 48 \
  --cas ../../.skillgate/cas --artifacts ../../artifacts

# 仅验证旧版协议 Fixture Worker 时，可改用 fixture_worker/worker.py。

# 完成后：UI 查看报告
skillgate ui --listen 127.0.0.1:8080 --database-url-file "$SKILLGATE_DATABASE_URL_FILE"
```

等价的单命令方式：`docker compose -f deploy/docker-compose.yml up --build`。

## 工作流 4：解读报告并决定是否晋级

1. 打开 UI 详情页或 `artifacts/<experiment_id>/report.json`；
2. 按顺序检查：

| 检查项 | 位置 | 不合格时的含义 |
|---|---|---|
| 配对有效性 | `invalid_pairs` | 非空 → 部分 Case 无法比较，先看 reason |
| 统计显著性 | `summary.statistically_significant` | false → 提升可能是噪声，考虑加 repetitions |
| 资源代价 | `resource_usage.token_delta.delta_ratio` | 过高 → 权衡提升是否值回成本 |
| 触发行为 | Decision 上下文 `routing.recall/specificity` | 低 → Skill 该触发不触发/不该触发乱触发 |
| 安全 | `decision` 硬门禁记录 | critical → 无条件 REJECT，无商量余地 |
| 最终结论 | `decision.result` + `matched_rules` | 由哪条规则决定，证据是否充分 |

3. 决策存疑时，用 `decision.evidence_links`（snapshot/experiment URI）回溯冻结的指标快照。

## 工作流 5：调试失败的 Trial

```bash
# 1. 查 Worker 日志/事件
docker compose -f deploy/docker-compose.yml logs langgraph-worker

# 2. 找到 Trial 的 Artifact 与 Trace
ls artifacts/<experiment_id>/<trial_id>/

# 3. 手动驱动单个 Trial 观察每步返回（详见 CLI 参考 trial 段）
skillgate trial claim --worker-id debug-1 --lease-duration 60s --database-url-file "$SKILLGATE_DATABASE_URL_FILE"
skillgate trial heartbeat --trial-id <id> --worker-id debug-1 \
  --lease-token-file /tmp/lease --phase debugging --database-url-file "$SKILLGATE_DATABASE_URL_FILE"
```

常见失败模式与定位：

| 现象 | 可能原因 | 定位手段 |
|---|---|---|
| Claim 拿不到 Trial | 队列空 / 实验已取消 | `db status`、实验状态 |
| 提交返回 `LEASE_EXPIRED` | 执行超租约时长 | 加大 `--lease-duration` 或提高心跳频率 |
| 提交返回 `ALREADY_COMMITTED` | 幂等键重复 | 预期行为，取既有 `result_id` 即可 |
| 报告不生成 | 实验未到 GRADING / 评分器缺 Schema | `serve` 日志、`--graders-dir` 内容 |
| 断言全挂 | Agent 输出路径/Schema 不符 | 对照 Trace 的工具调用与 outputs 配置 |

## 工作流 6：取消实验

```bash
skillgate experiment cancel --experiment-id <id> --actor ops --reason "发现 Suite 配置错误"
```

- 新 Trial 立即停止发放；
- 运行中的 Worker 在下一次心跳收到 `CANCEL_REQUESTED`，中断 Agent 图、终止沙箱容器、上报 `CANCELLED`；
- 取消是终态，不可恢复；需要重跑请重新 materialize。

## 工作流 7：CI 中做回归校验

无外部依赖的最小校验集（适合每个 PR）：

```bash
go build ./... && go vet ./...
go test -short ./...
go run ./cmd/skillgate skill validate skills/csv-analysis
go run ./cmd/skillgate compile experiments/csv-analysis-v1-demo.yaml
npm run validate:m0        # 示例数据 Hash/引用/配对自检
```

带 PostgreSQL 的集成验证（CI 容器内）：

```bash
go test ./internal/store/postgres/
docker compose -f deploy/docker-compose.yml up --build --abort-on-container-exit
```

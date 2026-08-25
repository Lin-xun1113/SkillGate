# 性能与容量验证计划

**状态：** `FUTURE → M4`；`PROPOSED`；所有目标数值需在第一轮 Benchmark 后校准

## 1. 为什么要单独做性能计划

SkillGate 的目标岗位包含 Strategy Engine、可扩展和高效方案。性能展示不能只写“支持并发”，必须能说明测量对象、方法、瓶颈和权衡。

## 2. 需要测量的路径

### Strategy Engine Hot Path

- CEL Expression Evaluation Latency；
- Rule Cache Hit/Miss；
- Decision Trace 序列化成本；
- 并发 Evaluation Throughput；
- Policy 数量对 Memory 的影响。

### Scheduler Path

- Trial Claim Throughput；
- Lease/Heartbeat DB 压力；
- Expiry Sweeper 延迟；
- Queue 深度和 Backpressure；
- 并发 Worker 数量对锁等待的影响。

### Result Commit Path

- 单次 Completion 延迟；
- 重复 Completion 冲突率；
- Artifact Hash 校验成本；
- Aggregate 更新耗时。

### Agent Path

- Model Call Latency；
- Tool Call 数量；
- Token 速度和成本；
- Sandbox 启动时间；
- Trace/Artifact 上传吞吐。

## 3. 初始目标（临时）

以下只作为开发阶段的方向，不得未经实测写入简历：

| 路径 | 初始目标 |
|---|---|
| CEL 单规则热评估 | p99 < 1 ms（不含网络） |
| 本地 Scheduler Claim | 在 8 个 Worker 下无明显锁饥饿 |
| Result Commit | p95 < 100 ms（不含大型 Artifact Upload） |
| 本地 Stub E2E | 100 个 Trial 能稳定完成 |
| Worker Recovery | Lease 过期后 1 个 Sweep 周期内恢复 |

目标是否合理，取决于硬件、Schema 和数据量；以 Benchmark 结果为准。

## 4. Benchmark 方法

1. 固定 Go、PostgreSQL、Python 和 Docker 版本；
2. 分别执行 Cold Start 和 Warm Run；
3. 使用 1、2、4、8、16、32 并发档位；
4. 每档先 Warm-up，再采集稳定窗口；
5. 记录 p50/p95/p99、吞吐、错误率、CPU、Memory 和 DB 指标；
6. 使用 OTel Trace 定位慢 Span；
7. 至少重复三轮；
8. 将结果和原始配置作为 Artifact 保存。

## 5. 性能风险与对策

| 风险 | 对策 |
|---|---|
| PostgreSQL Queue 行锁竞争 | 优化 Claim Index、缩短 Transaction、实测后再考虑 Broker |
| 大 Event 写入 DB | 大 Payload 转 Artifact，只在 DB 保存摘要和 Hash |
| Artifact Hash/Upload 阻塞 Worker | 分段上传、限制大小、明确 Completion 前置条件 |
| LangGraph Checkpoint 过大 | 控制 State 大小，压缩或只保存引用 |
| Model Provider 限流 | Per-provider Semaphore、Token Budget、Backoff、Circuit Breaker |
| 高 Cardinality Telemetry | 限制 Label，把详细身份放 Trace/Log |
| Sandbox 启动占比过高 | 复用只读 Image，测量后再考虑 Warm Pool |

## 6. 正确性优先级

任何性能优化都不能破坏：

- Pair Identity；
- Result Idempotency；
- Security Boundary；
- Budget Accounting；
- Artifact Hash；
- Metric 可重算性。

如果优化需要牺牲这些性质，必须先开 ADR 并说明替代方案。

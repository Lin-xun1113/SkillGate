# 故障恢复手册

**状态：** `PROVISIONAL → M2`；2026-08-25（UTC）Build 候选，等待 Native Verify

## 1. 恢复原则

- 不手工修改 Trial 状态来“让页面变绿”；
- 优先通过 Scheduler Reconcile 和有审计的 Admin Command 恢复；
- 历史 Result 和 Artifact 不覆盖；
- 任何人工动作都记录 Actor、原因、时间和影响范围；
- 无法确认的数据保持 `INCOMPLETE`。

## 2. M2 Worker/Fixture 崩溃

现象：Trial 长时间处于 `LEASED`/`RUNNING`。

处理：

1. 查询 Worker Heartbeat 和 Lease Expiry；
2. 确认 Worker Process 已停止或不可达；
3. 等待 Expiry Sweeper；
4. 检查是否存在已上传但未 Commit 的 Artifact；
5. 按 Retry Policy 进入 `RETRY_WAIT` 或 `FAILED`；
6. 检查旧 Worker 的迟到 Completion 是否返回 `LEASE_EXPIRED`；只有 Lease 有效期间已经提交但响应丢失的同一 Completion 才返回幂等成功；
7. 记录故障注入或真实故障 Evidence。

## 3. PostgreSQL 中断

处理：

1. 停止新的 Claim；
2. 确认数据库恢复并通过健康检查；
3. 检查最近的 Transaction 是否回滚；
4. 运行 Reconcile，比较 Trial Counter 与 Result Row；
5. 不重复提交已存在的 Idempotency Key；
6. 恢复 Scheduler 和 Worker。

## 4. Object Store 中断

处理：

1. 停止将新 Trial 标记为最终成功；
2. 保留 Worker 本地的失败诊断；
3. 恢复 MinIO/S3 后校验 Artifact Hash；
4. 对缺失 Artifact 的 Trial 标记 `INCOMPLETE` 或按 Policy Retry；
5. 重新生成 Metric Snapshot，不覆盖旧 Snapshot。

## 5. Result 重复提交

M2 正式行为：

- 相同 Attempt + 相同 Result Hash：返回已存在结果并标记 `idempotent=true`；
- 相同 Attempt + 不同 Result Hash：返回 `RESULT_CONFLICT`，产生 Audit Event，不覆盖；
- Lease 已过期：返回 `LEASE_EXPIRED`，不写 Result、不计数；
- 已 Terminal Logical Trial 收到其他 Attempt Completion：返回 `LOGICAL_TRIAL_TERMINAL`，不能修改原 Result。

不要通过直接修改数据库 Row 让页面变绿；使用 `skillgate scheduler sweep` 和可审计 CLI 恢复。

## 6. 指标不一致

如果页面显示的 Aggregate 与重新计算结果不同：

1. 锁定 Release Decision，不要继续 Promote；
2. 对比 `aggregation_version`；
3. 检查是否有重复 Grade 或漏掉 Invalid Trial；
4. 从 Trial/Grade 原始记录重算；
5. 生成新的 Metric Snapshot；
6. 若结论改变，原 Decision 标记为需复审。

## 7. Provider 大面积失败

- 识别是 Rate Limit、Credential、Endpoint 还是 Model 不可用；
- 触发 Circuit Breaker，避免继续消耗 Budget；
- 保留已完成 Trial，不将未执行 Trial 当成失败结果混入统计；
- 根据 Suite Policy 重新安排或终止 Experiment；
- 在 Report 中显示 Provider Incident。

## 8. 数据泄漏响应

如果发现 Credential 或敏感数据进入 Trace：

1. 立即停止相关 Worker/Experiment；
2. 撤销或轮换 Credential；
3. 限制 Artifact 访问；
4. 保存必要的 Incident Metadata，但不要复制泄漏内容；
5. 将 Artifact 标记为需要删除/隔离；
6. 分析入口（Skill、Tool、Log、Grader 或 Provider）；
7. 修复并增加回归 Test；
8. 重新运行 Security Gate。

## 9. 不能做的恢复操作

- 直接把 `FAILED` 改成 `SUCCEEDED`；
- 删除失败 Trial 让 Pass Rate 变高；
- 用新版本 Skill 覆盖旧 Artifact；
- 在没有 Pair Identity 的情况下合并两个 Arm；
- 关闭 Security Gate 来完成 Demo；
- 将真实 Credential 放入 Fixture 复现。

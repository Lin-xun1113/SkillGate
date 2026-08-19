# 测试与验证计划

**状态：** `ACCEPTED` 测试原则，具体命令随代码落地

## 1. 测试目标

测试不仅要证明 Happy Path 能运行，还要证明：

- Experiment 比较是有效的；
- Worker 故障不会破坏状态；
- Result Commit 不会重复计分；
- Skill/Prompt/Artifact 不能越权；
- Metric 和 Release Decision 可以重算；
- LLM Judge 不会轻易被操纵；
- 代码具有足够的性能和可观测性。

## 2. 测试分层

```text
纯函数 Unit Test
    ↓
Go Repository/Transaction Integration Test
    ↓
Runner Protocol Contract Test
    ↓
Python Worker Graph Test
    ↓
Docker Compose End-to-end Test
    ↓
故障注入与性能 Test
    ↓
真实 Model 的小规模 Smoke/Calibration
```

CI 默认使用 Fixture Provider；真实 Model Test 显式通过环境变量开启，不能成为普通 PR 的必需条件。

## 3. Go 测试清单

### 纯函数

- Canonicalization 和 Hash；
- Manifest 校验；
- Pair Identity；
- State Transition；
- Retry 分类；
- CEL Policy；
- Metric 公式；
- Error Mapping。

### 数据库集成

- 并发 `FOR UPDATE SKIP LOCKED` Claim；
- Lease Expiry；
- Heartbeat Token 校验；
- Result Idempotency；
- Cancellation；
- Transaction Rollback；
- 唯一约束和 Foreign Key。

### Fuzz/Property

- 任意 JSON 字段顺序 Canonical 后 Hash 一致；
- 非法路径不能越出根目录；
- Pair 编译没有重复 Logical Identity；
- 重复 Commit 与单次 Commit 的最终状态一致；
- CEL Evaluation 不修改输入。

## 4. Python/LangGraph 测试清单

- 每个 Graph Node 的状态 Contract；
- Checkpoint 恢复后 Identity 不变；
- Tool Policy；
- Skill Mount Hash；
- Fixture Provider；
- Grader 输出 Schema；
- Redaction；
- Cancellation 和 Timeout；
- Artifact 上传失败；
- 迟到 Completion。

## 5. End-to-end 验收

一个最小 E2E Test 应完成：

1. 注册 Skill Version；
2. 注册 Eval Suite；
3. 创建 Baseline/Candidate Strategy；
4. 编译 Experiment；
5. 启动 Go API/Scheduler、PostgreSQL、MinIO 和 Stub Worker；
6. 执行两组 Trial；
7. 提交 Grade/Artifact；
8. 生成 Metric Snapshot；
9. 运行 Release Policy；
10. 查询 Decision 和 Trace。

## 6. 故障注入矩阵

| 故障 | 预期结果 |
|---|---|
| Worker 领取后被杀死 | Lease 过期后按 Policy 重试 |
| Worker 完成后重复提交 | 返回已存在结果，不重复聚合 |
| Provider 429 | 有界 Backoff，记录 Retry |
| Provider 永久 4xx | 不重试，Trial Failed |
| MinIO 暂时不可用 | Result Incomplete，不生成虚假成功 |
| PostgreSQL 重启 | 已提交状态保留，未提交 Transaction 回滚 |
| 用户取消 Experiment | 停止新 Claim，运行中的 Sandbox 收到取消 |
| Agent 超时 | Sandbox 被终止，生成 Typed Timeout Evidence |
| Grader JSON 格式错误 | Grading Incomplete，不静默 Pass |
| Skill 试图读 `.env` | 按 Sandbox/Tool Policy 拒绝或产生 Security Finding |

## 7. Evaluation Validity Test

每次修改 Experiment Compiler 时，必须验证：

- Baseline/Candidate Prompt Hash 一致；
- Fixture Hash 一致；
- Model/Harness/Environment/Grader Version 一致；
- 只有声明的 Treatment 不同；
- Agent 看不到 Expected Answer；
- Judge 看不到 Arm Label；
- Trigger Case 没有被 Forced-load Runner 执行；
- 生成 Suite 标记了 Provenance 和 Review 状态。

## 8. Judge Calibration Test

在允许 LLM Judge 进入 Release Path 前：

1. 使用固定的正、负、空、恶意注入 Output；
2. 改变 Assertion 顺序和 A/B 展示顺序；
3. 使用人工 Label；
4. 计算 Cohen's Kappa、Precision、Recall 和 Confusion Matrix；
5. 比较不同 Judge Model 的 Lift；
6. 将结果保存为 Calibration Artifact。

## 9. 性能验证

所有性能数字都必须附带：

- 硬件/容器规格；
- PostgreSQL 版本；
- 数据规模；
- 并发数；
- Warm-up；
- 测试时长；
- p50/p95/p99；
- 错误率；
- 是否使用 Fixture 或真实 Provider。

没有这些上下文，不在简历中写性能数字。

## 10. 发布前清单

```text
[ ] go test ./...
[ ] Python Worker Test 全部通过
[ ] Contract Fixture 校验通过
[ ] Docker Compose E2E 通过
[ ] 故障注入报告已生成
[ ] Metric 重算与原结果一致
[ ] Security Scan 无未处理 Critical/High
[ ] Judge Calibration 状态可查询
[ ] 文档和 ADR 已更新
[ ] README Demo 可以从零启动
```

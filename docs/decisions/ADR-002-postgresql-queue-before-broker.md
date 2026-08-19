# ADR-002：先用 PostgreSQL Queue/Lease，不把 Broker 作为 MVP 必需依赖

**状态：** `ACCEPTED`  
**日期：** 2026-08-19（UTC）  
**影响范围：** Scheduler、Storage、部署复杂度

## 背景

SkillGate 需要调度 Trial、处理 Lease 和故障恢复。引入 Kafka/NATS 可以扩展吞吐，但会增加本地部署、消息一致性、重放和运维复杂度。项目当前首先需要证明实验有效性和 Result Idempotency，而不是证明消息系统规模。

## 决策

MVP 使用 PostgreSQL Transaction、`FOR UPDATE SKIP LOCKED`、Lease Token 和 Expiry Sweeper 实现 Queue。只有在实测出现吞吐、锁竞争或跨服务解耦瓶颈后，才评估 NATS/Kafka。

## 后果

正面：

- 本地启动依赖少；
- Trial 状态和 Queue Claim 在同一事务边界；
- 更容易展示幂等与恢复；
- 适合个人项目的第一个纵向切片。

负面：

- 超大规模 Queue 可能受 DB 锁竞争影响；
- Event Delivery 与 Task Claim 不是天然分离；
- 后续切换 Broker 需要保留领域接口。

## 验证方式

- 并发 Claim Test；
- Lease Expiry/Fault Injection；
- `EXPLAIN (ANALYZE, BUFFERS)`；
- 记录吞吐和 p99；
- 如果指标无法达到目标，再提出 Broker ADR。

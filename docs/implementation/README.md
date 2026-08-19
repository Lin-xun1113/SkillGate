# 实施文档索引

## 推荐阅读顺序

1. `implementation-plan.md` —— 总体里程碑和 Gate
2. `backend-work-breakdown.md` —— Go Control Plane 任务拆解
3. `worker-work-breakdown.md` —— Python/LangGraph Worker 任务拆解
4. `testing-and-validation.md` —— 测试、故障注入和评估有效性
5. `performance-plan.md` —— Benchmark 和容量验证

## 开发约束

- 先完成 M0 的 Workload 和 Eval Suite，再创建代码骨架；
- 每个 Milestone 都必须有失败路径和恢复路径；
- 真实 Model Call 不得成为默认 CI 依赖；
- 未通过 Contract Test 的 Worker 不得接入 Scheduler；
- 未通过 Security/Pairing Gate 的结果不能进入 Promotion Decision。

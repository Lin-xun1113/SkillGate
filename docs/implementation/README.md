# 实施文档索引

## 推荐阅读顺序

1. `implementation-plan.md` —— 总体里程碑和 Gate
2. `backend-work-breakdown.md` —— **`FUTURE`** Go Control Plane 任务拆解（M1 代码已部分偏离，需在 M1 归档后重新对齐）
3. `worker-work-breakdown.md` —— **`FUTURE → M3`** Python/LangGraph Worker 任务拆解
4. `testing-and-validation.md` —— 测试、故障注入和评估有效性
5. `performance-plan.md` —— **`FUTURE → M4`** Benchmark 和容量验证
6. `consolidation-roadmap.md` —— **`PROPOSED`** M0–M7 之后的 Provider、Artifact、OTel、UI E2E、Event 与 Runtime Routing 收尾路线

## 开发约束

- 先完成 M0 的 Workload 和 Eval Suite，再创建代码骨架；
- 每个 Milestone 都必须有失败路径和恢复路径；
- 真实 Model Call 不得成为默认 CI 依赖；
- 未通过 Contract Test 的 Worker 不得接入 Scheduler；
- 未通过 Security/Pairing Gate 的结果不能进入 Promotion Decision。

**标记为 `FUTURE` 的文档描述当前里程碑尚未实现的后续系统设计，保留作为方向参考，不作为当前实现的依据。**

# Contract 文档索引

- `experiment-manifest.md` —— Experiment 输入 Manifest 与编译校验（当前 M1 依据）
- `grading-contract.md` —— Deterministic/LLM Grader、Verdict 和 Answer Key 隔离（当前 M1 依据）
- `runner-protocol.md` —— **`FUTURE → M3`** Go Control Plane 与 Worker 的 gRPC 语义边界
- `event-schema.md` —— **`FUTURE → M3`** Event Envelope、Event Type、Sequence 和脱敏
- `release-gate-policy.md` —— **`FUTURE → M6`** PROMOTE/HOLD/REJECT 和 Security Hard Gate
- `scheduler-reliability-contract.md` —— **`PROVISIONAL → M2`** PostgreSQL Queue/Lease/Retry/幂等 Commit 与 CLI Contract
- `api-contract.md` —— **`FUTURE → M2+`** REST/JSON 资源、Endpoint、错误和并发控制

这些文档是实现时的规范性依据。代码行为改变 Contract 前，必须先更新 ADR 和对应文档。

**标记为 `FUTURE` 的文档描述当前里程碑尚未实现的后续系统设计，保留作为方向参考，不作为当前实现的依据。**

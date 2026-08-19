# Contract 文档索引

- `experiment-manifest.md` —— Experiment 输入 Manifest 与编译校验
- `runner-protocol.md` —— Go Control Plane 与 Worker 的 gRPC 语义边界
- `event-schema.md` —— Event Envelope、Event Type、Sequence 和脱敏
- `grading-contract.md` —— Deterministic/LLM Grader、Verdict 和 Answer Key 隔离
- `release-gate-policy.md` —— PROMOTE/HOLD/REJECT 和 Security Hard Gate
- `api-contract.md` —— REST/JSON 资源、Endpoint、错误和并发控制

这些文档是实现时的规范性依据。代码行为改变 Contract 前，必须先更新 ADR 和对应文档。

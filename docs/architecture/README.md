# 架构文档索引

- `system-architecture.md` —— 组件、边界、部署模式和请求流程
- `domain-model.md` —— 核心实体、Identity Tuple 和数据保留
- `execution-lifecycle.md` —— **`PROVISIONAL → M2`** Experiment/Trial 状态、Lease、Retry、Commit 和 Replay
- `strategy-engine.md` —— **M6 已实现** CEL Rule、Release Policy；Runtime Routing 仍是后续方向
- `storage-and-data.md` —— **M2 已实现** PostgreSQL、Hash 和数据生命周期；MinIO/S3 仍是后续方向
- `threat-model.md` —— **M4 已实现初始** 信任区域、Sandbox 和安全限制；完整安全边界仍需持续加固
- `observability.md` —— **部分实现** Trace、Metric、Log 和 Evidence 下钻；OTel 导出仍是后续方向
- `adr-guidance.md` —— 什么时候需要写 ADR

架构文档描述设计理由和边界；如果与 `docs/contracts/` 冲突，以 Contract 和已接受的 ADR 为准。

**标记为 `FUTURE` 的文档描述当前里程碑尚未实现的后续系统设计，保留作为方向参考，不作为当前实现的依据。**

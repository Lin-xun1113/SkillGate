# ADR-008：M1 Registry 与 Manifest Compiler Contract

**状态：** PROVISIONAL（M1 Build iteration 6，用户已确认设计，UTC 2026-08-19）

## 决策

M1 使用规范化文件列表 JSON 和 SHA-256 作为 Skill Identity；本地 Registry 采用文件系统 CAS。Canonical Blob 通过临时文件、Sync 和 exclusive create 写入，重复内容幂等返回，冲突不得覆盖。`SKILL.md` Frontmatter 保持原始文本，仅规范换行。Manifest Compiler 只生成 `without_skill`/`with_skill` 双臂和 `skill_version` Treatment，并将 Suite、Skill、Grader、Policy、Environment 的内容 Hash 纳入 Pair/Manifest Identity；Environment Identity 同时绑定执行 URI 与 Descriptor Hash。Suite 引用采用 Declared Hash，不改变 M0 Pair 引用声明语义。

所有外部引用必须在项目 Root 内解析，逐段拒绝符号链接和路径穿越；Suite/Grader/Policy/Environment/Fixture/Schema/Expected/Security 引用统一校验存在性、声明 Hash 和配置 Secret；Expected/Answer Key/Grader-only 内容不得进入 Agent-visible Context。CLI 提供统一版本化 JSON Envelope、文本摘要和稳定诊断退出码。

## 边界

本决策只覆盖 M1 本地离线 Registry、校验和确定性编译。PostgreSQL 事实来源、Queue/Lease、Retry、Result Commit、Worker、Sandbox、真实 Model、gRPC 和 UI 留给 M2+。

## 证据

- `internal/identity`
- `internal/registry`
- `internal/validation`
- `internal/manifest`
- `internal/experiment`
- `cmd/skillgate`
- `go test ./...`
- `go vet ./...`
- `npm run validate:m0`
- `go test ./...` 中的 Canonical、引用、Mutation、Registry Recovery 和 CLI subprocess Contract Tests

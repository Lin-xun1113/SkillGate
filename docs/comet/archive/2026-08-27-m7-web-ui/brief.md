# Outcome

构建 M7 Web UI，提供实验监控和结果查看能力。用户可以通过浏览器查看所有实验的状态、详细报告和 Release Decision。

# Scope

1. `skillgate ui` 命令 - 启动 HTTP server
2. 实验列表页 - 展示所有实验基本信息
3. 实验详情页 - 展示完整报告和 decision
4. PostgreSQL 数据读取 - 复用现有 store
5. 响应式设计 - dark theme

# Non-goals

- 实时 WebSocket 更新（Phase 1 仅页面刷新）
- 用户认证和授权
- Artifact 文件在线查看器
- 交互式图表库（Phase 1 使用简单 HTML/CSS）
- 实验创建和管理功能
- Prometheus metrics（Phase 2）
- OpenTelemetry tracing（Phase 2）

# Acceptance examples

- A1：运行 `skillgate ui --listen :8080` 成功启动 HTTP server，监听指定端口，输出 "Listening on :8080"
- A2：访问 `http://localhost:8080/` 返回实验列表页面，展示 experiments 表中的所有实验
- A3：列表页展示每个实验的 experiment_id、status、created_at、进度（terminal_count/total_logical_trials）
- A4：点击实验 ID 导航到 `/experiments/{experiment_id}` 详情页，页面加载成功
- A5：详情页展示实验基本信息、统计摘要（mean_lift、CI、显著性）、case 结果表格（来自 report.json）
- A6：COMPLETED 状态的实验在详情页显示 release_decisions 表的 result（PROMOTE/HOLD/REJECT），包含 explanation 和 evidence_links
- A7：页面使用深色主题（near-black background + cyan/emerald accent），响应式布局在桌面（≥1024px）、平板（768-1023px）、手机（<768px）正常显示
- A8：数据库连接失败、实验不存在、report 文件缺失时显示友好错误页面，而不是空白或 panic

# Constraints and invariants

- 必须使用现有 PostgreSQL 数据库连接（复用 internal/store/postgres.Store）
- 不修改现有数据库 schema（只读访问）
- HTTP server 独立于 gRPC server，可以单独运行
- 使用 Go 标准库 html/template
- CSS inline 在 HTML 中（Phase 1 无需 webpack/asset pipeline）
- 只读访问，不提供实验写入、删除等操作

# Decisions

- D1：部署方式为独立命令 `skillgate ui`，不集成到 `skillgate serve`。理由：保持关注点分离，UI 服务可以独立部署和扩展。
- D2：使用 Go html/template 标准库。理由：无需外部依赖，与项目技术栈一致。
- D3：直接使用 internal/store/postgres.Store。理由：复用现有连接池和查询逻辑，避免重复实现。
- D4：样式采用 inline CSS，dark theme（near-black #0a0a0a, cyan #06b6d4, emerald #10b981）。理由：Phase 1 快速交付，无需构建工具。
- D5：使用标准库 net/http，Go 1.22+ pattern-based routing。理由：简单直接，无需框架依赖。

# Open questions

# Verification expectations

- 启动命令成功运行并监听指定端口，无 panic
- 列表页正确查询和展示 experiments 表数据
- 详情页正确关联和展示 experiment_reports、release_decisions 表数据，并读取 report.json
- 响应式布局在桌面（1920px）、平板（768px）、手机（375px）屏幕下正常显示
- 数据库连接错误、表不存在、数据格式错误等情况有明确提示
- `go test ./internal/ui/...`、`go vet ./...` 通过

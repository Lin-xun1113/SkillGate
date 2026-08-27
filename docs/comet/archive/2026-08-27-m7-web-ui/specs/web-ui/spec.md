# Web UI 完整规格

## 概述

SkillGate Web UI 提供实验监控和结果查看能力。用户通过浏览器访问实验列表和详情页面。

## 命令行接口

### `skillgate ui`

启动 HTTP server，提供 Web 界面访问。

**参数**：
- `--listen <address>` - HTTP 监听地址，格式 `host:port` 或 `:port`（默认 `:8080`）
- `--database-url <url>` - PostgreSQL 连接字符串（默认读取环境变量 `DATABASE_URL`）

**行为**：
- 成功启动时输出 "Listening on <address>"
- 数据库连接失败时输出错误并退出（exit code 1）
- Ctrl+C 优雅关闭，等待现有请求完成（最长 10 秒）

## 路由

### `GET /`
实验列表页。

**查询参数**：无

**响应**：HTML 页面，展示 `experiments` 表中所有实验的：
- `experiment_id` - 点击后导航到详情页
- `status` - 实验状态（COMPILED/QUEUED/RUNNING/GRADING/COMPLETED/CANCELLED/FAILED）
- `created_at` - 创建时间（本地时区，格式 `YYYY-MM-DD HH:MM:SS`）
- 进度 - `terminal_count / total_logical_trials`

**排序**：按 `created_at DESC`（最新实验在前）

### `GET /experiments/{experiment_id}`
实验详情页。

**路径参数**：
- `experiment_id` - 实验 ID

**响应**：HTML 页面，展示：

1. **实验基本信息**
   - Experiment ID
   - Status
   - Created At
   - Total Trials / Terminal Count / Succeeded / Failed / Timed Out / Cancelled

2. **统计摘要**（来自 `experiment_reports` 表）
   - Mean Lift（百分比，保留 2 位小数）
   - 95% CI（`[ci_lower, ci_upper]`，保留 2 位小数）
   - Statistically Significant（是/否）
   - Valid Pairs / Invalid Pairs

3. **Case 结果表格**（来自 report.json 的 `case_results`）
   - Case ID
   - Without Skill（baseline 指标）
   - With Skill（treatment 指标）
   - Lift（百分比）

4. **Release Decision**（来自 `release_decisions` 表，仅 COMPLETED 状态显示）
   - Result（PROMOTE/HOLD/REJECT，颜色区分：green/yellow/red）
   - Policy（ID + Version）
   - Explanation
   - Evidence Links（可点击 URI 列表）
   - Created At

**错误处理**：
- Experiment 不存在 → 404 页面
- Report 不存在但 experiment 存在 → 显示基本信息，提示 "Report not available"
- Decision 不存在 → 不显示 Decision 卡片

## 样式

### 主题
- **背景色**：#0a0a0a（near-black）
- **Surface 色**：#1a1a1a（cards）、#2a2a2a（hover）
- **文本色**：#fafafa（primary）、#a0a0a0（secondary）
- **Accent 色**：#06b6d4（cyan，links/buttons）、#10b981（emerald，success）
- **状态色**：
  - COMPLETED/SUCCEEDED → #10b981（emerald）
  - RUNNING/QUEUED → #06b6d4（cyan）
  - FAILED/CANCELLED → #ef4444（red）
  - HOLD → #eab308（yellow）

### 布局
- **容器最大宽度**：1200px，居中
- **字体**：系统字体栈（`-apple-system, BlinkMacSystemFont, "Segoe UI", ...`）
- **字体大小**：`clamp(14px, 1vw + 12px, 18px)`（流畅缩放）
- **间距**：8px 基数（8、16、24、32、48px）
- **圆角**：8px（cards）、4px（buttons）

### 响应式
- **桌面（≥1024px）**：grid 2 列布局（列表项），详情页 sidebar + main
- **平板（768px-1023px）**：grid 1 列，详情页垂直堆叠
- **手机（<768px）**：单列，字体缩小，表格横向滚动

## 数据读取

### 数据库连接
- 使用 `internal/store/postgres.Store`
- 连接池配置：max_conns=10, min_conns=2
- 查询超时：5 秒

### 查询
- 列表页：`SELECT experiment_id, status, created_at, total_logical_trials, terminal_count FROM experiments ORDER BY created_at DESC`
- 详情页：
  1. 查询 `experiments` 表（基本信息）
  2. 查询 `experiment_reports` 表（统计摘要）
  3. 读取 `file_path` 指向的 report.json（case 结果）
  4. 查询 `release_decisions` 表（decision）

## 错误处理

### 数据库错误
- 连接失败 → 启动时退出
- 查询超时 → HTTP 500，页面提示 "Database query timeout"
- 约束违反 → HTTP 500，页面提示 "Data integrity error"

### 文件错误
- report.json 不存在 → 显示基本信息，提示 "Report file not found"
- report.json 格式错误 → 显示基本信息，提示 "Report format invalid"

### HTTP 错误
- 404 → "Experiment not found" 页面
- 500 → "Internal server error" 页面，包含友好提示和返回首页链接

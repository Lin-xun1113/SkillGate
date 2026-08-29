# M5: Grading Pipeline & Metric Aggregation

M5 在 M4 的 Trace/Artifact 基础上实现完整的 Grading Pipeline、Metric 聚合与 Report 生成。

## 功能概览

### 1. Grader Registry
- 独立存储 Grader 配置，通过 Hash 引用
- 支持 5 种 Grader 类型：
  - `file_content`: 文件内容精确匹配
  - `json_schema`: JSON Schema 校验
  - `command_exit_code`: 命令执行与退出码检查
  - `trace_assertion`: Trace 事件断言
  - `llm_rubric`: LLM Judge 评分（接口实现，M5 暂不使用）

### 2. Grader Executor
- Control Plane 直接执行 Deterministic Grader
- 读取 Trial Artifacts，执行 Grader 逻辑
- 结果写入 `trial_results.grades` JSON 字段

### 3. Metric 聚合
- Trial Score → Case Score 聚合
- Baseline/Candidate Pairing（严格配对规则）
- Paired Difference 计算

### 4. 统计分析
- **Cluster Bootstrap CI**: 并行 Bootstrap（2000 次重采样，可复现）
- **pass@k / pass^k**: HumanEval 标准公式
- **Resource Delta**: Token/Latency 对比

### 5. Report 生成
- **JSON Report**: 结构化数据，供 API 消费
- **Markdown Report**: 人类可读摘要
- **HTML Report**: 基础交互式展示

## 架构

```
Trial Completion (M4)
  ↓
Grader Registry
  ↓
Grader Executor (Control Plane)
  ↓
trial_results.grades (DB)
  ↓
Metric Aggregator
  ↓
Statistical Analyzer (Bootstrap CI, pass@k)
  ↓
Report Generator (JSON/Markdown/HTML)
  ↓
artifacts/<experiment_id>/report.*
```

## 快速开始

### 1. 运行单元测试

```bash
# Grader Registry
go test ./internal/grader -v

# Metrics Aggregator
go test ./internal/metrics -v

# Statistical Analyzer
go test ./internal/statistics -v

# Report Generator
go test ./internal/report -v
```

### 2. 创建 Grader 配置

参见 `examples/graders/` 目录中的示例：

```yaml
apiVersion: skillgate.dev/v1alpha1
kind: Grader
metadata:
  name: json-schema-validator
spec:
  type: deterministic
  method: json_schema
  config:
    schema_path: ./evals/schemas/output.json
    artifact_pattern: "output.json"
  scoring:
    passed_score: 1.0
    failed_score: 0.0
```

### 3. 运行完整 Grading Pipeline（待集成）

```bash
# 启动 docker compose（包含 M4 + M5）
docker compose up --build

# 查看 Report
cat artifacts/<experiment_id>/report.json
cat artifacts/<experiment_id>/report.md
```

## 数据库变更

### 新增 GRADING 状态

```sql
-- experiments.status 新增 GRADING
ALTER TABLE experiments
    ADD CONSTRAINT experiments_status_check
    CHECK (status IN (..., 'GRADING', ...));
```

### 新增 experiment_reports 表

```sql
CREATE TABLE experiment_reports (
    report_id text PRIMARY KEY,
    experiment_id text NOT NULL UNIQUE,
    report_type text NOT NULL,
    file_path text NOT NULL,
    valid_pairs int NOT NULL,
    invalid_pairs int NOT NULL,
    mean_lift float8,
    ci_lower float8,
    ci_upper float8,
    statistically_significant boolean,
    created_at timestamptz NOT NULL
);
```

## 验收标准

### A1: Deterministic File Grader 执行
- Grader 读取 Artifact 文件
- 执行 JSON Schema 校验
- 结果写入 `trial_results.grades`

### A2: Case-level Score 聚合
- 同一 Case 的多次 Repetition 聚合为 Case Score

### A3: Baseline/Candidate Pairing
- 成功匹配 Baseline/Candidate Pair
- 无效 Pair 标记并排除

### A4: Bootstrap CI 计算
- 2000 次 Cluster Bootstrap（并行执行）
- 报告点估计、CI 上下界

### A5: pass@k 计算
- HumanEval 标准公式
- pass@k 和 pass^k 均计算

### A6-A7: Report 生成
- JSON Report（Schema 校验通过）
- Markdown Report（人类可读）

### A8: 端到端 Grading 流程
- docker compose 一键演示
- Experiment 状态 RUNNING → GRADING → COMPLETED

## 性能

- **Grader 执行**: < 1 秒 per Trial
- **Metric 聚合**: < 5 秒 per Experiment (100 Trials)
- **Bootstrap CI**: < 2 秒 (2000 resamples, 10 Cases, 并行)
- **Report 生成**: < 1 秒

## 依赖

- Go 1.21+
- github.com/xeipuuv/gojsonschema (JSON Schema 校验)
- gopkg.in/yaml.v3 (YAML 解析)
- 无其他新增外部依赖

## 目录结构

```
.
├── internal/
│   ├── grader/
│   │   ├── types.go           # Grader 类型定义
│   │   ├── registry.go        # Grader Registry
│   │   ├── executor.go        # Grader Executor
│   │   └── *_test.go
│   ├── metrics/
│   │   ├── aggregator.go      # Case Score 聚合、Pairing
│   │   └── *_test.go
│   ├── statistics/
│   │   ├── bootstrap.go       # Bootstrap CI、pass@k
│   │   └── *_test.go
│   └── report/
│       ├── generator.go       # Report 生成器
│       └── *_test.go
├── examples/
│   └── graders/               # 示例 Grader 配置
└── docs/
    └── m5/
        └── README.md          # 本文件
```

## 实现状态

> 本节已按当前代码与 M5/M6 Verify 结果同步。前文的目录和“下一步”保留为 M5 立项时的设计记录；跨里程碑的最新进度以 [`PROJECT_STATUS.md`](../../PROJECT_STATUS.md) 为准。

- [x] Grader Registry
- [x] Grader Executor (Deterministic)
- [x] Metrics Aggregator
- [x] Statistical Analyzer (Bootstrap CI, pass@k)
- [x] Report Generator (JSON, Markdown, HTML)
- [x] 数据库 Schema 变更
- [x] 单元测试
- [x] 集成到 Control Plane
- [x] 端到端测试（Fixture Provider）
- [x] docker compose 演示

## 下一步

1. 集成 Grader Executor 到 Control Plane
2. 实现 Experiment 状态转换（RUNNING → GRADING → COMPLETED）
3. 添加集成测试
4. 更新 docker compose 配置
5. 端到端验证

## 非目标（留待 M6+）

- Strategy Engine 或 Release Gate
- S3 Artifact 上传
- 完整 UI
- 实时流式 Metric 更新
- Kubernetes 部署
- 真实 LLM Grader 执行
- 完整 Security Probe 攻击面评估

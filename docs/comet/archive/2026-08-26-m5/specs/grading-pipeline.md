# Grading Pipeline 完整规格

**状态：** M5 Shape 阶段  
**版本：** v1  
**作者：** Claude (Comet Native)

## 1. 概述

Grading Pipeline 负责对 Trial 结果进行评分、聚合统计并生成报告。它在 M4 的 Trace/Artifact 基础上，实现完整的评估分析流程。

## 2. 架构

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

## 3. 组件详细设计

### 3.1 Grader Registry

**位置：** `internal/grader/registry.go`

**职责：**
- 存储和检索 Grader 配置
- 计算 Grader Hash（内容 SHA-256）
- 校验 Grader 有效性

**Grader 配置格式：**
```yaml
apiVersion: skillgate.dev/v1alpha1
kind: Grader
metadata:
  name: json-schema-validator
spec:
  type: deterministic
  method: json_schema
  config:
    schema_path: ./schemas/output.json
    required_fields: ["summary", "data"]
  scoring:
    passed_score: 1.0
    failed_score: 0.0
```

**支持的 Grader 类型：**

1. **file_content**: 文件内容精确匹配
   ```yaml
   type: deterministic
   method: file_content
   config:
     expected_file: expected_output.txt
     actual_file_pattern: "output.txt"
   ```

2. **json_schema**: JSON Schema 校验
   ```yaml
   type: deterministic
   method: json_schema
   config:
     schema_path: ./schemas/output.json
     artifact_pattern: "output.json"
   ```

3. **command_exit_code**: 执行命令，检查退出码
   ```yaml
   type: deterministic
   method: command_exit_code
   config:
     command: ["python", "validate.py", "{artifact_path}"]
     expected_exit_code: 0
     timeout_seconds: 30
   ```

4. **trace_assertion**: Trace 事件断言
   ```yaml
   type: deterministic
   method: trace_assertion
   config:
     assertions:
       - type: tool_called
         tool_name: "python_execute"
       - type: skill_loaded
         skill_hash: "sha256:abc..."
   ```

5. **llm_rubric**: LLM Judge 评分（可选）
   ```yaml
   type: llm
   method: rubric
   config:
     rubric: |
       评估输出是否准确回答了问题。
       - 优秀 (1.0): 完全正确且详细
       - 良好 (0.7): 基本正确但有遗漏
       - 差 (0.3): 部分正确
       - 失败 (0.0): 完全错误
     model: "gpt-4"
     temperature: 0.0
   ```

**Registry 接口：**
```go
type GraderRegistry interface {
    Register(graderPath string) (string, error) // 返回 Hash
    Get(graderHash string) (*Grader, error)
    Validate(graderHash string) error
}
```

### 3.2 Grader Executor

**位置：** `internal/grader/executor.go`

**职责：**
- 读取 Trial Artifacts
- 执行 Grader 逻辑
- 写入 grades 到数据库

**执行流程：**
1. Trial 完成，Control Plane 收到 CompleteTrial
2. 读取 Experiment Manifest 中的 Grader Hash
3. 从 Registry 获取 Grader 配置
4. 读取 Trial Artifacts（从本地 artifacts/<experiment_id>/<trial_id>/）
5. 执行 Grader 逻辑
6. 构造 grades JSON
7. 写入 `trial_results.grades`

**Grader 执行接口：**
```go
type GraderExecutor interface {
    Execute(ctx context.Context, trial *Trial, graderConfig *Grader) (*GradeResult, error)
}

type GradeResult struct {
    GraderID    string                 `json:"grader_id"`
    GraderType  string                 `json:"grader_type"`
    Passed      bool                   `json:"passed"`
    Score       float64                `json:"score"`
    Message     string                 `json:"message"`
    Evidence    map[string]interface{} `json:"evidence"`
    ExecutedAt  time.Time              `json:"executed_at"`
}

type GradesManifest struct {
    Graders          []GradeResult `json:"graders"`
    AggregatedScore  float64       `json:"aggregated_score"`
}
```

**聚合分数计算：**
- 默认：所有 Grader Score 的平均值
- 可配置权重：`aggregated_score = sum(score_i * weight_i) / sum(weight_i)`

### 3.3 Metric Aggregator

**位置：** `internal/metrics/aggregator.go`

**职责：**
- 聚合 Trial 级别的 grades 为 Case 级别 Score
- 匹配 Baseline/Candidate Pair
- 计算 Paired Difference

**Case Score 聚合：**
```go
type CaseScore struct {
    CaseID       string  `json:"case_id"`
    Arm          string  `json:"arm"`
    Repetitions  int     `json:"repetitions"`
    MeanScore    float64 `json:"mean_score"`
    StdDev       float64 `json:"std_dev"`
    TrialScores  []float64 `json:"trial_scores"`
}

func AggregateCaseScores(trials []Trial) ([]CaseScore, error) {
    // 按 (case_id, arm, repetition) 分组
    // 计算每个 Case 的 mean score
}
```

**Pairing 逻辑：**
```go
type Pair struct {
    CaseID          string     `json:"case_id"`
    BaselineScore   float64    `json:"baseline_score"`
    CandidateScore  float64    `json:"candidate_score"`
    Difference      float64    `json:"difference"` // candidate - baseline
    Valid           bool       `json:"valid"`
    InvalidReason   string     `json:"invalid_reason,omitempty"`
}

func PairCases(baseline, candidate []CaseScore) ([]Pair, error) {
    // 匹配 case_id
    // 校验 Pairing 规则（见 brief）
    // 计算 difference
}
```

**无效 Pair 原因：**
- `case_id_mismatch`: Case ID 不匹配
- `repetition_mismatch`: Repetition 数量不一致
- `model_mismatch`: Model 配置不一致
- `environment_mismatch`: Environment 不一致
- `grader_mismatch`: Grader Hash 不一致

### 3.4 Statistical Analyzer

**位置：** `internal/statistics/bootstrap.go`, `internal/statistics/passk.go`

**Bootstrap CI 实现：**
```go
type BootstrapConfig struct {
    NumResamples int     `json:"num_resamples"` // 默认 2000
    CILevel      float64 `json:"ci_level"`      // 默认 0.95
    Seed         int64   `json:"seed"`          // 固定种子确保可复现
    Parallel     bool    `json:"parallel"`      // 并行执行
}

type BootstrapResult struct {
    MeanEstimate float64 `json:"mean_estimate"`
    CILower      float64 `json:"ci_lower"`
    CIUpper      float64 `json:"ci_upper"`
    NCases       int     `json:"n_cases"`
}

func ClusterBootstrapCI(pairs []Pair, config BootstrapConfig) (*BootstrapResult, error) {
    // 1. 提取有效 Pair 的 Difference
    // 2. 设置随机数种子
    // 3. 并行执行重采样（每个 goroutine 独立随机数生成器）
    // 4. 计算 2.5% 和 97.5% 分位数
    // 5. 返回结果
}
```

**并行重采样实现：**
```go
func parallelResample(diffs []float64, numResamples int, seed int64) []float64 {
    numWorkers := runtime.NumCPU()
    perWorker := numResamples / numWorkers
    
    results := make([]float64, numResamples)
    var wg sync.WaitGroup
    
    for i := 0; i < numWorkers; i++ {
        wg.Add(1)
        go func(workerID int, workerSeed int64) {
            defer wg.Done()
            rng := rand.New(rand.NewSource(workerSeed))
            
            start := workerID * perWorker
            end := start + perWorker
            if workerID == numWorkers-1 {
                end = numResamples
            }
            
            for j := start; j < end; j++ {
                sample := resampleWithReplacement(diffs, rng)
                results[j] = mean(sample)
            }
        }(i, seed+int64(i))
    }
    
    wg.Wait()
    return results
}
```

**pass@k 实现：**
```go
func PassAtK(attempts []bool, k int) (float64, error) {
    n := len(attempts)
    c := countTrue(attempts)
    
    // pass@k = 1 - C(n-c, k) / C(n, k)
    if k > n {
        return 0, fmt.Errorf("k (%d) > n (%d)", k, n)
    }
    
    numerator := binomial(n-c, k)
    denominator := binomial(n, k)
    
    return 1.0 - float64(numerator)/float64(denominator), nil
}

func PassPowerK(attempts []bool, k int) float64 {
    // pass^k = (c/n)^k
    c := countTrue(attempts)
    n := len(attempts)
    p := float64(c) / float64(n)
    return math.Pow(p, float64(k))
}
```

### 3.5 Report Generator

**位置：** `internal/report/generator.go`

**Report JSON Schema：**
```json
{
  "schema": "skillgate.report.v1",
  "experiment_id": "exp-001",
  "metadata": {
    "experiment_name": "csv-analysis-v2-eval",
    "created_at": "2026-08-26T16:00:00Z",
    "total_trials": 60,
    "valid_pairs": 10,
    "invalid_pairs": 0
  },
  "summary": {
    "mean_lift": 0.15,
    "ci_lower": 0.05,
    "ci_upper": 0.25,
    "n_cases": 10,
    "statistically_significant": true,
    "method": "cluster_bootstrap",
    "num_resamples": 2000
  },
  "case_results": [
    {
      "case_id": "csv-001",
      "baseline_score": 0.7,
      "candidate_score": 0.9,
      "difference": 0.2,
      "baseline_repetitions": 3,
      "candidate_repetitions": 3
    }
  ],
  "pass_at_k": {
    "pass@1": 0.6,
    "pass@3": 0.95,
    "pass^3": 0.216
  },
  "resource_usage": {
    "token_delta": {
      "baseline_mean": 5000,
      "candidate_mean": 6000,
      "delta": 1000,
      "delta_ratio": 0.2
    },
    "latency_delta": {
      "baseline_mean_ms": 2000,
      "candidate_mean_ms": 2500,
      "delta_ms": 500,
      "delta_ratio": 0.25
    }
  },
  "invalid_pairs": [],
  "statistical_method": {
    "bootstrap_method": "cluster_bootstrap",
    "ci_method": "percentile",
    "significance_level": 0.05
  }
}
```

**Markdown Report 模板：**
```markdown
# Experiment Report: {experiment_name}

**Experiment ID:** {experiment_id}  
**Generated:** {timestamp}  
**Status:** {status}

## Executive Summary

- **Skill Lift:** {mean_lift:.3f} (95% CI: [{ci_lower:.3f}, {ci_upper:.3f}])
- **Statistically Significant:** {yes/no}
- **Valid Pairs:** {valid_pairs} / {total_pairs}

## Case-level Results

| Case ID | Baseline | Candidate | Difference |
|---------|----------|-----------|------------|
| csv-001 | 0.700    | 0.900     | +0.200     |
| ...     | ...      | ...       | ...        |

## Resource Usage

- **Token Delta:** +{delta} tokens (+{delta_ratio:.1%})
- **Latency Delta:** +{delta_ms} ms (+{delta_ratio:.1%})

## Statistical Method

- Bootstrap: Cluster Bootstrap (2000 resamples)
- CI: Percentile method
- Significance level: α=0.05

## Failure Analysis

{invalid_pair_details}
```

## 4. 数据库 Schema 变更

### 4.1 新增 experiments.status 值

```sql
ALTER TABLE experiments
    DROP CONSTRAINT experiments_status_check;

ALTER TABLE experiments
    ADD CONSTRAINT experiments_status_check
    CHECK (status IN ('COMPILED', 'QUEUED', 'RUNNING', 'GRADING', 'COMPLETED', 'CANCEL_REQUESTED', 'CANCELLED', 'FAILED'));
```

### 4.2 新增 experiment_reports 表

```sql
CREATE TABLE experiment_reports (
    report_id text PRIMARY KEY,
    experiment_id text NOT NULL UNIQUE REFERENCES experiments(experiment_id) ON DELETE RESTRICT,
    report_type text NOT NULL CHECK (report_type IN ('json', 'markdown', 'html')),
    file_path text NOT NULL,
    file_hash text NOT NULL,
    valid_pairs int NOT NULL CHECK (valid_pairs >= 0),
    invalid_pairs int NOT NULL CHECK (invalid_pairs >= 0),
    mean_lift float8,
    ci_lower float8,
    ci_upper float8,
    statistically_significant boolean,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
```

## 5. 错误处理

### 5.1 Grader 执行失败

- **原因：** Artifact 文件不存在、JSON 格式错误、命令执行超时
- **处理：** 记录错误到 `trial_results.grades`，标记 `passed: false`，继续其他 Trial

### 5.2 Pairing 失败

- **原因：** Case ID 不匹配、Repetition 不一致
- **处理：** 标记为 Invalid Pair，记录原因，排除出统计分析

### 5.3 Bootstrap 失败

- **原因：** 有效 Pair 数量 < 2
- **处理：** Report 中标记 `ci_lower: null, ci_upper: null`，说明样本不足

## 6. 性能要求

- **Grader 执行：** < 1 秒 per Trial（Deterministic）
- **Metric 聚合：** < 5 秒 per Experiment（100 Trials）
- **Bootstrap CI：** < 2 秒（2000 resamples, 10 Cases, 并行）
- **Report 生成：** < 1 秒

## 7. 测试计划

### 7.1 单元测试

- `grader_registry_test.go`: Registry CRUD 操作
- `grader_executor_test.go`: 各类型 Grader 执行
- `aggregator_test.go`: Case Score 聚合、Pairing
- `bootstrap_test.go`: Bootstrap CI 计算（固定种子，可复现）
- `passk_test.go`: pass@k / pass^k 公式
- `generator_test.go`: Report JSON Schema 校验

### 7.2 集成测试

- `grading_pipeline_test.go`: Trial → Grader → Metric → Report 完整流程
- `invalid_pair_test.go`: 无效 Pair 处理
- `parallel_bootstrap_test.go`: 并行 Bootstrap 可复现性

### 7.3 端到端测试

- docker compose 启动，运行 M0 csv-analysis Experiment
- 验证 Report 生成
- 人工检查 Report 内容正确性

## 8. 实现顺序

1. **Day 1-2**: Grader Registry + 基础 Executor（file_content, json_schema）
2. **Day 3**: Metric Aggregator（Case Score, Pairing）
3. **Day 4**: Statistical Analyzer（Bootstrap CI, pass@k）
4. **Day 5**: Report Generator（JSON, Markdown）
5. **Day 6**: 集成到 Control Plane（状态转换、自动触发）
6. **Day 7**: 端到端测试、文档、docker compose 演示

## 9. 依赖

- Go 1.21+
- 无新增外部依赖（使用标准库 + 已有依赖）
- M4 Artifact 存储结构
- M0-M3 数据库 Schema

## 10. 文档

- `docs/m5/README.md`: M5 概述和快速开始
- `internal/grader/README.md`: Grader Registry 使用指南
- `docs/contracts/grader-contract.md`: Grader 配置 Contract
- `docs/contracts/report-schema.md`: Report JSON Schema

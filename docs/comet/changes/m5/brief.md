# Outcome

实现 Grading Pipeline、Metric 聚合与 Report 生成，使 SkillGate 能够：
1. 对 Trial 结果执行 Deterministic 和 LLM Grader
2. 计算 Case-level Score 并进行配对统计分析
3. 生成包含 Skill Lift、CI、pass@k、资源消耗的可解释报告

# Scope

## 核心功能

### 1. Grader 执行引擎
- Deterministic Grader：文件内容、JSON Schema、命令退出码、Trace Assertion
- LLM Rubric Grader（可选）：使用 LLM Judge 评估输出质量
- Grader 结果写入 `trial_results.grades` JSON 字段

### 2. Metric 聚合
- Trial Score 计算：从 grades 聚合为单一分数
- Case-level Pairing：匹配 Baseline/Candidate 的同 Case 结果
- Repetition 聚合：多次重复的平均值
- Paired Difference：`d_i = Y_i(1) - Y_i(0)`

### 3. 统计分析
- Bootstrap CI：Case-level Cluster Bootstrap（默认 2000 次）
- pass@k / pass^k：多次尝试成功率
- Token/Latency Delta：资源消耗对比
- Trigger Metric：Recall/Specificity/Precision/FPR（Autonomous Case）

### 4. Report 生成
- JSON Report：结构化数据，供 API 和后续工具消费
- Markdown Report：人类可读的摘要
- HTML Report（可选）：交互式展示

## 数据流

```
Trial Results (M4) 
  → Grader Executor
  → trial_results.grades
  → Metric Aggregator
  → Statistical Analysis
  → Report Generator
```

# Non-goals

- M5 **不实现**：
  - Strategy Engine 或 Release Gate（留待 M6）
  - S3 Artifact 上传（仅本地文件）
  - 完整 UI（仅生成静态报告）
  - 实时流式 Metric 更新
  - Kubernetes 部署
  - 多租户
  - 完整 Security Probe 攻击面评估（仅记录 security_probe Case 结果）

# Acceptance examples

## A1: Deterministic File Grader 执行
给定一个 Trial Result 包含 Artifact `output.json`，Grader 配置要求 JSON Schema 校验：
- Grader 读取 Artifact 文件
- 执行 JSON Schema 校验
- 结果写入 `trial_results.grades`，格式：`{"grader_id": "json-schema-001", "passed": true, "score": 1.0}`

## A2: Case-level Score 聚合
给定同一个 Case 的 3 次 Repetition，grades 分别为 `[1.0, 0.5, 1.0]`：
- Case Score = mean([1.0, 0.5, 1.0]) = 0.833

## A3: Baseline/Candidate Pairing
给定 Experiment 包含 10 个 Case，每个 Case 有 without_skill 和 with_skill 两臂：
- Pairing 成功匹配 10 对
- 计算 Paired Difference：`d_i = candidate_score_i - baseline_score_i`
- 无效 Pair（Case ID 不匹配、Repetition 不一致）被标记并排除

## A4: Bootstrap CI 计算
给定 10 个 Case 的 Paired Difference：
- 执行 2000 次 Cluster Bootstrap（以 Case 为单位重采样）
- 报告点估计、2.5% 和 97.5% 分位数
- 输出：`{"mean_lift": 0.15, "ci_lower": 0.05, "ci_upper": 0.25, "n_cases": 10}`

## A5: pass@k 计算
给定一个 Case 有 5 次 Attempt，其中 3 次成功：
- pass@1 = 3/5 = 0.6
- pass@3 = 1 - C(2,3)/C(5,3) = 1 - 0 = 1.0（至少 3 次中有 1 次成功）
- pass^3 = (3/5)^3 = 0.216（严格 3 次都成功的概率）

## A6: JSON Report 生成
Experiment 完成后：
- 生成 `report.json` 包含：
  - Experiment Metadata
  - Case-level Results（Baseline/Candidate Score, Difference）
  - Aggregate Metrics（Mean Lift, CI, pass@k, Token Delta）
  - Valid/Invalid Pair 计数
- JSON Schema 校验通过

## A7: Markdown Report 生成
生成 `report.md` 包含：
- Executive Summary（Lift, CI、统计显著性）
- Case-level Table
- Resource Usage（Token/Latency Delta）
- Failure Analysis
- 统计方法说明

## A8: 端到端 Grading 流程
启动 docker compose，运行一个包含 Deterministic Grader 的 Experiment：
- Trial 完成后自动触发 Grading
- Grades 写入数据库
- Report 生成并保存到 `artifacts/<experiment_id>/report.{json,md}`
- Experiment 状态从 `RUNNING` → `GRADING` → `COMPLETED`

# Constraints and invariants

## 统计约束
- 统计单位是 Case，不是单个 Trial 或 LLM Judge Vote
- Repetition 聚合后才进行 Pairing
- Bootstrap 必须以 Case 为单位重采样（Cluster Bootstrap）
- CI 跨越 0 时不能声称"有效"

## Pairing 规则
只有以下字段完全一致时 Pair 才有效：
- Case ID 和 Suite Version
- Prompt/Fixture Hash
- Model 和 Model Configuration
- Agent Harness 和 Version
- Environment Image/Configuration
- Grader 和 Rubric Version
- Repetition Index
- Resource Limit

唯一允许的 Treatment Difference：Skill Hash 或 Strategy ID

## Grader 约束
- Grader 必须是幂等的（同样输入产生同样输出）
- Grader 不能看到 Arm Label（避免 bias）
- Security Probe Case 不进入 Utility Lift 计算
- Trigger Case 只统计加载证据，不统计最终答案提到 Skill 名称

## 数据完整性
- 所有 Trial 必须有 grades，即使为空对象 `{}`
- Report 必须说明统计方法和版本
- 无效 Pair 必须记录原因
- 缺失 Case 必须在报告中说明

# Decisions

## D1: Grader 执行时机
**决定：** Trial 完成后立即执行 Deterministic Grader，LLM Grader 可选延迟执行。

**理由：** 
- Deterministic Grader 快速且无成本，立即执行可以尽早发现问题
- LLM Grader 有 API 成本和延迟，允许用户选择是否启用

**影响：**
- Experiment 状态需要区分 `RUNNING`（Trial 执行中）和 `GRADING`（Grader 执行中）
- Worker 完成 Trial 后不负责 Grading，由 Control Plane 调度 Grader 执行

## D2: Grader 结果 Schema
**决定：** `trial_results.grades` 使用以下 JSON Schema：
```json
{
  "graders": [
    {
      "grader_id": "deterministic-file-001",
      "grader_type": "file_content",
      "passed": true,
      "score": 1.0,
      "message": "Output matches expected content",
      "evidence": {"file": "output.txt", "expected_hash": "sha256:..."}
    }
  ],
  "aggregated_score": 1.0
}
```

**理由：**
- 支持多个 Grader 对同一 Trial 评分
- 保留每个 Grader 的 pass/fail 判断和详细证据
- `aggregated_score` 用于后续 Metric 计算

## D3: Bootstrap 实现方式
**决定：** 使用 Go 实现 Cluster Bootstrap，2000 次重采样为默认值。

**理由：**
- Go 性能足够（2000 次重采样 < 1秒）
- 避免引入 Python/R 依赖
- Cluster Bootstrap 逻辑简单（以 Case 为单位带放回抽样）

## D4: Report 存储位置
**决定：** Report 存储在 `artifacts/<experiment_id>/report.{json,md,html}`，路径写入数据库。

**理由：**
- 与 Trace/Artifact 保持一致的存储结构
- 便于后续 S3 上传（M6+）
- 数据库只存储路径，避免 JSONB 膨胀

## D5: pass@k 计算公式
**决定：** 使用 HumanEval 标准公式：`pass@k = 1 - C(n-c, k) / C(n, k)`

**理由：**
- 业界标准，与其他 Agent Benchmark 可比
- 数学上正确（无偏估计）
- 文档中已明确说明

## D6: LLM Grader 是否必须
**决定：** M5 实现 LLM Grader **接口**，但默认禁用，不要求 MVP 验收必须使用 LLM Grader。

**理由：**
- Deterministic Grader 足以验证核心 Pipeline
- LLM Grader 需要真实 API Key 和成本
- 保留扩展性，M6+ 可启用

## D7: Grader 配置来源
**决定：** Grader 配置独立存储在 Registry，Experiment Manifest 通过 Grader Hash 引用。

**理由：**
- Grader 可以跨 Experiment 复用
- 版本化管理，保证可复现性
- 与 Skill Registry 保持一致的架构

**影响：**
- M5 需要实现 Grader Registry（类似 Skill Registry）
- Manifest Compiler 需要校验 Grader Hash 有效性

## D8: Grader 执行者
**决定：** Control Plane 在 Trial 完成后直接执行 Deterministic Grader。

**理由：**
- 简单直接，无需额外进程或 RPC 通信
- Deterministic Grader 是本地文件操作，无需隔离
- 适合 M5 MVP 范围

**影响：**
- Control Plane 需要读取 Artifact 文件
- Grader 执行失败会阻塞 Experiment 进入 COMPLETED 状态

## D9: Experiment 状态转换
**决定：** Experiment 状态转换为 `RUNNING` → `GRADING` → `COMPLETED`。

**理由：**
- 明确 Grading 阶段，用户可以看到进度
- 与 Trial 执行阶段清晰分离
- 便于调试和监控

**影响：**
- 数据库 schema 需要支持 `GRADING` 状态
- 所有 Trial 完成后，Experiment 进入 `GRADING` 而非 `COMPLETED`

## D10: 无效 Pair 处理
**决定：** 排除无效 Pair，对有效 Pair 进行统计分析，在 Report 中说明无效原因。

**理由：**
- 容错性好，部分失败不影响整体结果
- 实际场景中可能出现个别 Trial 失败，不应使整个 Experiment 作废
- 保留诊断信息，便于后续改进

**影响：**
- Report 必须包含 Valid/Invalid Pair 统计
- 无效 Pair 必须记录详细原因（Case ID 不匹配、Repetition 不一致等）

## D11: Bootstrap 并发策略
**决定：** 使用多 goroutine 并行执行 Bootstrap 重采样。

**理由：**
- 提高性能，充分利用多核 CPU
- 2000 次重采样可以分批并行处理
- Go 的 goroutine 轻量且易于管理

**影响：**
- 需要使用独立的随机数生成器（每个 goroutine 一个），确保可复现性
- 设置固定随机数种子，保证结果可复现

# Open questions

（无未解决问题）

# Verification expectations

## 单元测试
- Deterministic Grader 各类型（文件、JSON、命令、Trace Assertion）
- Case Score 聚合逻辑
- Pairing 匹配逻辑（有效/无效 Pair）
- Bootstrap CI 计算（固定种子，可复现结果）
- pass@k / pass^k 公式
- Report JSON Schema 校验

## 集成测试
- 完整 Grading Pipeline：Trial → Grader → Metric → Report
- 多 Repetition 聚合
- 无效 Pair 处理
- Report 文件生成

## 端到端测试
- docker compose 启动 → 运行 Experiment → 生成 Report
- Report 内容正确性人工检查

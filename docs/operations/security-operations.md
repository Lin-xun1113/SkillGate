# 安全运维手册

**状态：** `PROVISIONAL → P1`；2026-08-30 UTC  服务器级 Secret Manager、KMS、mTLS 仍未接入
**版本：** v0.2

## 1. 安全默认值

- Sandbox 默认关闭 Network；
- 使用非 Root 用户；
- 不挂载 Host Docker Socket；
- 不挂载 Host Home、`.ssh`、`.aws`、`.config`；
- 使用 Synthetic Credential；
- 限制 CPU、Memory、PID、文件大小和运行时间；
- 所有 Skill/Fixture/Artifact 走路径与 Hash 校验；
- Provider Credential 只能从 Operator Environment 注入。

## 2. Skill Admission

Skill 注册前：

1. 检查 `SKILL.md` Schema；
2. 检查路径和 Symbolic Link；
3. 计算 Canonical Hash；
4. 运行静态 Security/PII/Secret 检查；
5. 记录 License/Provenance；
6. 对包含 Script/Dependency/Network 的 Package 提高审查级别；
7. 保存 Scanner Version 和原始 Finding。

静态 Scan 不通过时，不应直接删除 Evidence；应根据 Severity 进入 `REJECT` 或 `HOLD`。

## 3. 动态 Security Probe

在 Restricted Sandbox 中使用带唯一 Marker 的 Mock Credential：

- 环境变量；
- 模拟配置文件；
- Case 明确的认证参数。

观察：

- Outbound Network；
- 未声明的 File Read/Write；
- stdout/stderr 中的 Credential；
- Tool Call 越权；
- Prompt Injection 触发；
- Agent 是否拒绝危险路径。

不要用真实 Credential 验证 Skill 是否会泄漏。

## 4. Provider Credential 轮换

- 本地开发只使用最小权限测试 Key；默认 Compose 使用 Fixture，不需要 Provider Key；
- Key 只能通过 `OPENAI_API_KEY`/`OPENAI_API_KEY_FILE` 或 `ANTHROPIC_API_KEY`/`ANTHROPIC_API_KEY_FILE` 注入 Worker；`*_FILE` 优先；
- Key 不进入 Manifest、`execution_hash`、Runner Request、DB、Artifact、Trace、命令参数或 Git；
- 轮换步骤：创建新 Key → 写入新的 Secret 文件（权限 `0600`）→ 原子替换/更新挂载 → 重启 Worker → 用 `scripts/verify-secrets.sh` 和受控 Provider Smoke 验证 → 撤销旧 Key；当前 Worker 在启动/Provider 初始化时读取，**不支持热更新**；
- 发现泄漏时先暂停相关 Worker/出口并立即撤销旧 Key，再保存最小元数据（时间、版本、受影响实验 ID），不得复制原始日志/Trace；
- Redaction 不能替代 Key Rotation；SDK 错误、FailTrial、Event 和报告写入前必须脱敏。

数据库 URL 也应使用 `SKILLGATE_DATABASE_URL_FILE`（兼容 `DATABASE_URL_FILE`）。CLI 的 `--database-url` 仅为兼容无密码 URL，含密码 DSN 会 Fail Closed。

Local Insecure Demo 与 Production 的边界、Secret Source 错误码见 [`../contracts/secret-source.md`](../contracts/secret-source.md)。

## 5. Artifact 访问

- Artifact 默认私有；
- Download URL 使用短时效；
- Report 对外只显示必要摘要；
- Raw Trace 需要更高访问权限；
- 删除使用 Tombstone/Audit，而非静默删除；
- 定期执行 Retention 清理并保留清理记录。

## 6. 依赖安全

- 固定 Go/Python/Node 依赖版本；
- 记录 Lockfile Hash；
- 不在运行时从 Skill 目录隐式安装依赖；
- 对 Remote Script、未固定依赖和动态下载生成 Finding；
- 在 CI 中执行依赖漏洞扫描。

## 7. 事件响应最小流程

```text
发现 → 隔离 → 撤销 Credential → 保存元数据 → 分析根因
→ 增加回归测试 → 修复 → 重新扫描 → 重新评估 → 复核 Release Decision
```

### 7.1 最小验证命令

```bash
bash scripts/verify-secrets.sh
bash scripts/check-licenses.sh   # 本地手动检查；依赖工具未安装时会报告 SKIP
```

脚本只使用合成 Sentinel，不读取、上传或打印真实用户数据、生产 Trace 或 Provider Key。

## 8. 安全限制声明

SkillGate MVP 不是多租户安全产品。Local Mode 的隔离弱于 Docker/Cloud Mode；Sandbox 不能证明没有所有恶意行为；Model Refusal 不能替代权限边界和运行时隔离。

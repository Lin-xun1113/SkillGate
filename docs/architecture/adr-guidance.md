# Architecture Decision Record 使用规则

当一个决策会改变以下内容之一时，必须编写或更新 ADR：

- 持久化 Schema；
- Public API 或 Worker Protocol；
- Experiment Validity 假设；
- Security Boundary；
- Deployment Strategy；
- 具有明显运维成本的依赖；
- 统计定义或 Release Gate。

普通的实现层选择可以直接记入 Implementation Plan。

## 决策检查清单

接受一个决策前，需要回答：

1. 要解决什么问题？
2. 考虑过哪些替代方案？
3. 如果选择错误，最可能出现什么失败？
4. 这个选择是否容易撤销？
5. 哪个 Contract 或 Test 能发现偏离？
6. 是否改变项目范围或目标岗位信号？

新 ADR 使用 `docs/templates/adr.md`。

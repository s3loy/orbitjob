---
title: "API 索引"
---

<a id="17-api-index"></a>

完整 schema、请求字段与响应模型：

- 仓库文件：`api/openapi.yaml`
- 运行时接口：`GET /openapi.json`

生成的参考文档列出全部操作、参数、安全要求、请求 schema 和响应模型。本索引按操作流程分组，不另行定义 schema。下表路径均带 `/api/v1` 前缀。

| 领域 | 支持的操作／指南 |
|---|---|
| 身份与发现 | `GET /identity`、`GET /workspaces`；[认证与作用域](/orbitjob/docs/usage/authentication-and-request-conventions/) |
| 个人账户 | `/auth/*` setup、登录、邀请、会话管理、密码确认与重置；[账户与工作区访问](/orbitjob/docs/accounts/)。这些路由使用个人会话或显式一次性能力令牌，不使用 API key。 |
| 工作区与凭据 | Tenant 创建／列表／详情；租户 API key 创建／列表；key 重命名／撤销；[租户与 key 流程](/orbitjob/docs/usage/tenants-and-api-keys/) |
| 策略与资源组 | Policy 创建／列表／详情／删除；group 创建／列表；显式租户 policy/group 创建／列表；[管理作用域](/orbitjob/docs/usage/tenants-and-api-keys/) |
| 在线定义 | Job/Workflow 类型化 GET 与 PUT；[校验、应用与核对](/orbitjob/docs/usage/jobs-runs-functions-and-workflows/#104-create-or-update-a-typed-kubernetes-definition) |
| Job | 活动修订列表／详情；手动触发；[发布与准确 occurrence 查询](/orbitjob/docs/usage/jobs-runs-functions-and-workflows/#102-manual-trigger) |
| Instance | 列表、汇总、详情、attempt 与取消；[执行阶段](/orbitjob/docs/usage/jobs-runs-functions-and-workflows/#103-runs-and-phases) |
| 来源执行控制 | Jobs、Workflows、Functions 和 Checks 的 GET/PATCH `/<kind>/<id>/execution-controls`；[维护与安全恢复](/orbitjob/docs/local-maintenance/) |
| Function | 创建／列表／详情／更新、调用、终态历史列表／详情；[版本限定调用](/orbitjob/docs/usage/jobs-runs-functions-and-workflows/#105-manage-and-invoke-a-function) |
| Workflow | 定义列表／详情、计划预览、执行触发／列表／详情／取消；[Workflow 流程](/orbitjob/docs/usage/jobs-runs-functions-and-workflows/#106-trigger-observe-and-cancel-a-workflow) |
| 日志 | Instance 与 Function 归档、下载及受审计删除；[执行日志](/orbitjob/docs/execution-logs/)。Kubernetes 资源保留期间，旧版 [attempt 快照](/orbitjob/docs/usage/jobs-runs-functions-and-workflows/#107-read-retained-attempt-logs)仍可用。 |
| Check 与结果 | Check 创建／列表／详情／暂停／恢复／删除；结果列表／详情；[Check 流程](/orbitjob/docs/usage/checks-slis-and-slos/#121-checks) |
| SLI | 创建／列表／详情／删除；[来源身份与判定条件](/orbitjob/docs/usage/checks-slis-and-slos/#122-slis) |
| SLO、预算与告警 | SLO 创建／列表／详情／暂停／恢复／删除；当前与历史预算读取；告警列表／详情；[可靠性流程](/orbitjob/docs/usage/checks-slis-and-slos/#123-slos-budgets-and-alerts) |

无需认证的 `/healthz`、`/metrics` 与 `/openapi.json` 不使用上述前缀。Chart 可选的 Ingress 排除健康和指标端点。API 不支持任意 Kubernetes 资源访问、定义删除、Function payload 或结果正文、已有 API key 的策略重新绑定、告警确认或实验。

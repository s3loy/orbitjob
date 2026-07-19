# 安全策略

## 报告漏洞

请勿在公开 Issue、Discussion 或 PR 中披露安全漏洞。发送邮件至 <justs3loy@gmail.com>，主题标注 `[OrbitJob Security]`。

报告需包含：

- 受影响的 commit、版本或部署方式
- 所需权限与攻击路径
- 最小复现步骤
- 涉及的 tenant、数据库 role、Kubernetes namespace 或网络边界
- 实际影响与建议修复方案

禁止在未授权环境中进行测试。禁止发送真实 API key、数据库密码、Secret 内容或生产数据，使用替代值代替。

## 响应与披露

维护者确认收到报告后，在复现漏洞的基础上协调修复与披露时间。项目尚未发布稳定版本，不承诺固定 SLA。修复提交公开前，请勿发布利用细节。

## 当前安全模型

OrbitJob 的安全边界由四层组成：

1. Bearer API key 认证
2. Admin API tenant 校验与统一输入验证
3. PostgreSQL 最小权限 role 与 RLS
4. Compose/Kubernetes 中的 Secret、ServiceAccount、RBAC 与 Pod Security

上述各层不可相互替代。RLS 可减少越权查询的影响范围，但不能修复错误授权；Kubernetes RBAC 可限制 worker 创建 Job 的权限，但不能验证业务 tenant。

### API Key

API key 使用 `otj_` 前缀。服务端保存 bcrypt 摘要，不保存可恢复的明文。创建 API key 和 bootstrap 时仅返回或写入一次明文。

认证函数需在 tenant 未知时查询候选 key。`orbitjob_auth_api_key` 使用 `SECURITY DEFINER`、固定 `search_path`，仅向 `orbitjob_admin` 授予执行权限。函数返回候选摘要后，应用层执行 bcrypt 比较。

吊销、过期、未知 key、suspended tenant 与错误密码均返回统一 `401`，避免泄露 key 或 tenant 的存在性。

### PostgreSQL Role

部署使用以下 role：

| Role | 用途 |
|---|---|
| `orbitjob_table_owner` | NOLOGIN；持有 table、sequence 与安全函数 |
| `orbitjob_migrator` | migration runner；可 `SET ROLE orbitjob_table_owner` |
| `orbitjob_admin` | Admin API 与 bootstrap |
| `orbitjob_runtime` | scheduler、dispatcher、worker |
| `orbitjob_operator` | 为后续 Operator 预留的受限写权限 |

Runtime 进程不可使用 bootstrap owner、table owner 或 PostgreSQL superuser DSN。Compose 与 Helm 分开保存 admin/runtime/migrator 凭据。

v0.2.0 baseline 撤销 `PUBLIC` 在 `public` schema 上的 `CREATE` 权限，安全函数显式撤销 `PUBLIC EXECUTE`。新增函数时需重复这两个约束，不可依赖数据库默认权限。

### RLS

v0.2.0 baseline 对以下表执行 `ALTER TABLE ... ENABLE ROW LEVEL SECURITY`：

```text
jobs
job_instances
job_instance_attempts
workers
audit_events
job_change_audits
checks
check_runs
slis
slos
sli_snapshots
budgets
budget_alerts
api_keys
tenants
```

baseline 仅 `ENABLE` 不 `FORCE` RLS。`orbitjob_table_owner` 为 NOLOGIN，无进程以 owner 身份登录；`orbitjob_admin`、`orbitjob_runtime`、`orbitjob_operator` 均非 table owner，`ENABLE` 已对它们强制 RLS。`FORCE` 仅在 owner 本身也需受 RLS 约束时才有意义，当前身份分离模型下不需要。

策略读取事务局部变量 `app.tenant_id`。repository 必须在同一事务中设置 tenant 后再执行 query。不可使用 session 级 `SET app.tenant_id`，连接池会复用连接。

跨 tenant 操作仅允许通过经过审查的 `SECURITY DEFINER` 函数：

- `orbitjob_auth_api_key`
- `orbitjob_list_active_tenant_ids`
- `orbitjob_bootstrap_default`

新增跨 tenant query 前，优先调整数据模型或使用 tenant-scoped transaction。确实需要函数时，固定 `search_path`、收窄返回列、撤销 `PUBLIC`、仅授权指定 role，并增加真实 PostgreSQL 集成测试。

### Kubernetes Container Handler

Container handler 创建 `batch/v1 Job`。Pod 默认限制：

- UID 65534，`runAsNonRoot: true`
- read-only root filesystem
- `allowPrivilegeEscalation: false`
- drop ALL Linux capabilities
- RuntimeDefault seccomp
- 不自动挂载 ServiceAccount token
- 默认 request：100m CPU、64Mi memory
- 默认 limit：1 CPU、512Mi memory

生产环境建议：

```yaml
worker:
  containerExecution:
    requireDigest: true
```

该设置要求 `image@sha256:...`，防止可变 tag 在重复执行时指向不同内容。

Chart 将 worker 与 task workload 置于独立的 ServiceAccount/namespace 权限边界内。不可为 task ServiceAccount 授予集群级权限。若 workload 需访问 API，显式创建另一个受限 ServiceAccount，并在 Job payload 中引用。

### HTTP 与 Webhook

HTTP、webhook 和 `http_health` handler 拦截 loopback、RFC1918、link-local、metadata 及 IPv6 private 地址，并在 DNS 解析与连接阶段重复检查。redirect 默认关闭。

SSRF 防护不等同于网络隔离。生产集群应额外使用 NetworkPolicy、egress proxy 或防火墙限制 worker 与 task namespace 的出口流量。

Webhook secret 当前随 Job payload 存储。不可将长期凭据直接写入 payload。优先让接收端使用短期 token，或在后续 Secret 引用能力交付前通过受控网关转发。

### Secret 与日志

禁止提交以下内容：

- `.env` 文件
- bootstrap API key
- PostgreSQL role 密码与 DSN
- Kubernetes Secret 明文
- 包含真实标识符或错误内容的 `smoke-test-results.md`

`make docker-up` 生成随机本地凭据。bootstrap key 写入 `bootstrap_secrets` volume；通过 `make bootstrap-key` 读取。不可将命令输出粘贴到 CI log 或公开 Issue。

应用使用结构化日志与 trace ID。新增日志时不可记录 `Authorization` header、API key、DSN、handler secret 或完整请求 body。

## 已知边界

- 项目尚无正式 release 与长期支持版本。
- Kubernetes Lease 与 PG epoch fencing 尚未交付；需要多进程协调时使用 etcd。
- API 目前没有完整的管理 RBAC。不可将 tenant/API-key 管理接口直接暴露到不可信网络；在 ingress 或 API gateway 层增加访问控制。
- OpenAPI 尚未完整表达 Bearer security 与部分 DELETE version body。认证行为以 middleware 为准，删除 Check/SLI/SLO 时需提供当前 `version`。
- Claim/Lease 为至少一次语义，handler 与外部接收端需实现幂等。
- PostgreSQL `NOTIFY` 不保留离线消息，不可用于安全审计或关键事件投递。

## 支持的版本

| 版本 | 安全更新 |
|---|---|
| 尚未正式发布 | 维护者优先修复 `dev` 分支上可复现的问题，不承诺长期支持 |

## 致谢

修复公开后列出报告者，除非报告者要求匿名。

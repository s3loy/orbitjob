# 安全策略

## 报告漏洞

不要在公开 Issue、Discussion 或 PR 中披露漏洞。发送邮件到 <justs3loy@gmail.com>，主题写明 `[OrbitJob Security]`。

请提供：

- 受影响的 commit、版本或部署方式
- 前置权限和攻击路径
- 最小复现步骤
- tenant、数据库 role、Kubernetes namespace 或网络边界
- 实际影响和建议修复方式

不要在未授权环境中测试。不要发送真实 API key、数据库密码、Secret 内容或生产数据；请使用替代值。

## 响应与披露

维护者会确认收到报告，并在复现后协调修复和披露时间。项目尚未发布稳定版本，我们不承诺固定 SLA。修复提交公开前，请不要发布利用细节。

## 当前安全模型

OrbitJob 的安全边界由四层组成：

1. Bearer API key 认证
2. Admin API tenant 校验和统一输入验证
3. PostgreSQL 最小权限 role 与 RLS
4. Compose/Kubernetes 中的 Secret、ServiceAccount、RBAC 和 Pod Security

任何一层都不能替代其他层。RLS 减少越权查询的影响，但不能修复错误授权；Kubernetes RBAC 限制 worker 创建 Job，也不能验证业务 tenant。

### API key

API key 使用 `otj_` 前缀。服务端保存 bcrypt 摘要，不保存可恢复明文。创建 API key 和 bootstrap 时只返回或写入一次明文。

认证函数需要在 tenant 未知时查询候选 key。`orbitjob_auth_api_key` 使用 `SECURITY DEFINER`，固定 `search_path`，并只向 `orbitjob_admin` 授予执行权限。函数返回候选摘要后，应用层执行 bcrypt 比较。

吊销、过期、未知 key、suspended tenant 和错误密码都返回统一 `401`，避免泄露 key 或 tenant 是否存在。

### PostgreSQL role

部署使用以下 role：

| Role | 用途 |
|---|---|
| `orbitjob_table_owner` | NOLOGIN；持有 table、sequence 和安全函数 |
| `orbitjob_migrator` | migration runner；可以 `SET ROLE orbitjob_table_owner` |
| `orbitjob_admin` | Admin API 与 bootstrap |
| `orbitjob_runtime` | scheduler、dispatcher、worker |
| `orbitjob_operator` | 为后续 Operator 预留的受限写权限 |

runtime 进程不要使用 bootstrap owner、table owner 或 PostgreSQL superuser DSN。Compose 和 Helm 分开保存 admin/runtime/migrator 凭据。

v0.2.0 baseline 撤销 `PUBLIC` 在 `public` schema 上的 `CREATE`，安全函数也显式撤销 `PUBLIC EXECUTE`。新增函数时要重复这两个约束，不要依赖数据库默认权限。

### RLS

v0.2.0 baseline 对这些表执行 `ALTER TABLE ... ENABLE ROW LEVEL SECURITY`：

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

baseline 只 `ENABLE` 不 `FORCE` RLS。`orbitjob_table_owner` 是 NOLOGIN，没有任何进程以 owner 身份登录；`orbitjob_admin`、`orbitjob_runtime`、`orbitjob_operator` 都不是 table owner，`ENABLE` 已对它们强制 RLS。`FORCE` 只在 owner 也需受 RLS 约束时有意义，当前身份分离模型下不需要。

策略读取事务局部变量 `app.tenant_id`。repository 必须在同一事务中设置 tenant，再执行 query。不要使用 session 级 `SET app.tenant_id`，连接池会复用连接。

跨 tenant 操作只允许走经过审查的 `SECURITY DEFINER` 函数：

- `orbitjob_auth_api_key`
- `orbitjob_list_active_tenant_ids`
- `orbitjob_bootstrap_default`

新增跨 tenant query 前，优先调整数据模型或 tenant-scoped transaction。确实需要函数时，固定 `search_path`、收窄返回列、撤销 `PUBLIC`、只授权指定 role，并增加真实 PostgreSQL 集成测试。

### Kubernetes container handler

container handler 创建 `batch/v1 Job`。默认 Pod 限制：

- UID 65534，`runAsNonRoot: true`
- read-only root filesystem
- `allowPrivilegeEscalation: false`
- drop ALL Linux capabilities
- RuntimeDefault seccomp
- 不自动挂载 ServiceAccount token
- 默认 request：100m CPU、64Mi memory
- 默认 limit：1 CPU、512Mi memory

生产环境设置：

```yaml
worker:
  containerExecution:
    requireDigest: true
```

这会要求 `image@sha256:...`，避免可变 tag 在重复执行时指向不同内容。

Chart 把 worker 和 task workload 放在独立 ServiceAccount/namespace 权限边界中。不要给 task ServiceAccount 集群级权限。若 workload 需要访问 API，显式创建另一个受限 ServiceAccount，并在 Job payload 中选择它。

### HTTP 与 webhook

HTTP、webhook 和 `http_health` handler 拦截 loopback、RFC1918、link-local、metadata 和 IPv6 private 地址，并在 DNS 解析与连接阶段重复检查。redirect 默认关闭。

SSRF 防护不是网络隔离。生产集群仍应使用 NetworkPolicy、egress proxy 或防火墙限制 worker 和 task namespace 的出口。

webhook secret 当前随 Job payload 存储。不要把长期凭据直接放进 payload。优先让接收端使用短期 token，或在后续 Secret 引用能力交付前通过受控网关转发。

### Secret 与日志

不要提交：

- `.env`
- bootstrap API key
- PostgreSQL role 密码和 DSN
- Kubernetes Secret 明文
- 带真实标识符或错误内容的 `smoke-test-results.md`

`make docker-up` 生成随机本地凭据。bootstrap key 写入 `bootstrap_secrets` volume；使用 `make bootstrap-key` 读取。不要把命令输出粘贴到 CI log 或公开 Issue。

应用使用结构化日志和 trace ID。新增日志时不要记录 `Authorization` header、API key、DSN、handler secret 或完整请求 body。

## 已知边界

- 项目没有正式 release，也没有长期支持版本。
- Kubernetes Lease 和 PG epoch fencing 尚未交付；需要多进程协调时使用 etcd。
- API 目前没有完整的管理 RBAC。不要把 tenant/API-key 管理接口直接暴露到不可信网络；在 ingress 或 API gateway 增加访问控制。
- OpenAPI 尚未完整表达 Bearer security 和部分 DELETE version body。认证行为以 middleware 为准，删除 Check/SLI/SLO 时需要当前 `version`。
- Claim/Lease 是至少一次语义。handler 和外部接收端需要幂等。
- PostgreSQL `NOTIFY` 不保存离线消息，不适合安全审计或关键事件投递。

## 支持的版本

| 版本 | 安全更新 |
|---|---|
| 尚未正式发布 | 维护者优先修复 `dev` 上可复现的问题，不承诺长期支持 |

## 致谢

我们会在修复公开后列出报告者，除非你要求匿名。

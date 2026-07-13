# OrbitJob 使用手册

本文对应当前 `feat/helm-runtime-security` 分支。代码和 `api/openapi.yaml` 是接口契约；本文解释怎么部署、调用和排障。

## 1. 当前能力

OrbitJob 已包含：

- `admin-api`、`scheduler`、`dispatcher`、`worker` 四个独立进程
- PostgreSQL 17 持久状态、事务抢占和乐观锁
- Bearer API key、多 tenant 数据隔离、PG role 与 RLS
- cron/manual Job，Claim/Lease 至少一次执行
- `exec`、`http`、`webhook`、`pg_notify`、`container` handler
- Check、CheckRun、SLI、SLO、错误预算和燃烧率告警
- Docker Compose 本地栈
- Helm Chart、kind 验证脚本和 Kubernetes Job container 执行
- Prometheus、Grafana；Loki/Promtail 可选
- memory 与 etcd 协调

当前没有 Operator、CRD、Kubernetes Lease、Workflow、Serverless。Kubernetes 模式仍由 Helm 部署普通 workload，不是 CRD 控制面。

## 2. Docker Compose 快速启动

### 2.1 前置条件

- Docker Engine 或 Docker Desktop
- Docker Compose v2
- `make`
- 空闲端口：`8080`、`9090`、`3000`；development override 还会发布 `5432`

不要手工复制 `.env.example` 中的空占位符。我们用脚本生成独立的 PG role 密码、Grafana 密码和 bootstrap API key：

```bash
make docker-up
```

`make docker-up` 会依次：

1. 生成或校验 `.env`
2. 校验 Compose 配置
3. 编译镜像并启动容器
4. 等待 migration、bootstrap 和 runtime 就绪
5. 检查 Prometheus 与 Grafana

启动后：

```text
Admin API   http://localhost:8080
Prometheus  http://localhost:9090
Grafana     http://localhost:3000
```

读取凭据：

```bash
export ORBITJOB_API_KEY="$(make --no-print-directory bootstrap-key)"
make grafana-password
```

设置后续示例变量：

```bash
export ORBITJOB_API=http://localhost:8080
export AUTH="Authorization: Bearer $ORBITJOB_API_KEY"
```

验证：

```bash
curl -fsS "$ORBITJOB_API/healthz"
curl -fsS -H "$AUTH" "$ORBITJOB_API/api/v1/tenants"
make docker-status
```

### 2.2 服务与启动顺序

| 服务 | 用途 | 主机端口 |
|---|---|---:|
| `pg` | PostgreSQL 17 | `5432`，仅 development override |
| `pgbouncer` | transaction pooling | 不发布 |
| `owner-init` | 创建和收紧数据库 role，设置部署密码 | 一次性任务 |
| `migrate` | 以 migrator 身份执行 checksum migration；拒绝旧开发 schema | 一次性任务 |
| `secret-init` | 准备非 root secret volume | 一次性任务 |
| `bootstrap` | 创建默认 tenant 和初始 API key | 一次性任务 |
| `admin-api` | HTTP 控制面 | `8080` |
| `scheduler` | cron 触发 | 不发布 |
| `dispatcher` | instance 分发 | 不发布 |
| `worker` | handler 执行 | 不发布 |
| `prometheus` | metrics | `9090` |
| `grafana` | dashboard | `3000` |
| `etcd` | 可选协调 | profile 启用后 `2379` |
| `loki`、`promtail` | 可选日志栈 | profile 启用 |

`owner-init` 使用 bootstrap database owner 创建 cluster-level role、收紧属性和 membership，并设置部署生成的密码。随后 `migrate` 以 `orbitjob_migrator` 登录，通过 `SET ROLE orbitjob_table_owner` 执行唯一的 `0001_v020_baseline.up.sql`。

v0.2.0 不支持旧开发数据库原地升级，也不提供 legacy rebase。升级前重建本地数据：

```bash
make docker-reset
# 输入 delete-volumes
make docker-up
```

Runner 使用单连接 advisory lock、逐 migration 事务和 SHA-256 checksum。重复运行为 no-op。v0.2.0 发布后的 migration 只能追加，不能重写 baseline。

查看初始化日志：

```bash
docker compose logs migrate bootstrap
docker compose logs -f admin-api scheduler dispatcher worker
```

一次性任务以退出码 0 结束是正常状态。

### 2.3 可选 profile

启用 etcd 容器：

```bash
docker compose --profile coordination-etcd up -d
```

这只启动 etcd。runtime 是否使用 etcd，仍取决于 `ETCD_ENABLED=true` 和 `ETCD_ENDPOINTS`。默认 Compose 使用 memory/PG 路径。

启用 Loki 与 Promtail：

```bash
docker compose --profile logs up -d
```

Docker Desktop for macOS 无法直接读取 Linux VM 内的 container log 文件。macOS 上优先使用：

```bash
docker compose logs -f admin-api scheduler dispatcher worker
```

### 2.4 停止与重置

```bash
make docker-down       # 保留 named volumes
make docker-reset      # 需要输入 delete-volumes；删除数据库和 secret
make env-clean         # 需要输入 delete-env；删除 .env
```

`docker-reset` 会删除 PG、Grafana、Prometheus、Loki、etcd 和 bootstrap secret 数据。

## 3. Helm 与 kind

### 3.1 Chart 内容

Chart 位于 `charts/orbitjob`，包含：

- owner-init、migration、bootstrap Job
- admin-api、scheduler、dispatcher、worker
- 独立的 admin/runtime/migrator/operator PG 凭据
- runtime ServiceAccount 与 RBAC
- container task namespace、ServiceAccount 与 Job 权限
- migration ConfigMap

Chart 不安装 PostgreSQL。你需要先创建 `values.yaml` 引用的数据库 Secret。默认名称和 key：

```text
Secret: orbitjob-database
bootstrap-owner-dsn
migrator-dsn
admin-dsn
runtime-dsn
migrator-password
admin-password
runtime-password
operator-password
bootstrap-api-key
```

检查 Chart 与 migration 镜像副本：

```bash
make helm-migrations-check
make helm-check
```

修改 `db/migrations/*.up.sql` 后同步 Chart：

```bash
make helm-migrations-sync
```

### 3.2 本地 kind 验证

需要 `kind`、`kubectl`、`helm`、Docker：

```bash
make kind-up
make kind-v020-verify
```

验证脚本使用 `deploy/kind/` 下的 PostgreSQL、values 和失败 migration fixture，覆盖首次安装、重复升级与 migration 失败阻断。

若 PostgreSQL PVC 包含发布前开发 schema，删除本地集群后重建：

```bash
make kind-down
# 输入 delete-kind
make kind-up
```

Chart 不会删除外部 PostgreSQL 数据，也不会把旧开发 history rebase 为正式 baseline。

删除集群：

```bash
make kind-down
# 输入 delete-kind
```

### 3.3 安装到已有 Kubernetes

先准备镜像和数据库 Secret，再安装：

```bash
helm upgrade --install orbitjob charts/orbitjob \
  --namespace orbitjob-system \
  --create-namespace \
  -f values.production.yaml
```

关键 values：

```yaml
worker:
  containerExecution:
    namespace: orbitjob-tasks
    requireDigest: true
    ttlSecondsAfterFinished: 600
    serviceAccountName: orbitjob-task

taskNamespace:
  create: true
  name: orbitjob-tasks
```

生产环境建议 `requireDigest: true`，Job image 必须使用 `image@sha256:...`。代价是发布流程要先解析 digest，不能只传可变 tag。

读取 bootstrap key：

```bash
kubectl -n orbitjob-system get secret orbitjob-bootstrap \
  -o jsonpath='{.data.api-key}' | base64 --decode
printf '\n'
```

Secret 名可由 `bootstrap.secretName` 修改。

## 4. 本地 Go 开发

`devserver` 把 admin-api、scheduler、dispatcher 和 worker 放在一个进程中，适合调试，不代表生产拓扑。

```bash
make dev DEV_DSN='postgres://postgres:postgres@localhost:5432/orbitjob?sslmode=disable'
```

你需要提前启动 PostgreSQL、执行 migration 并完成 bootstrap。

常用质量命令：

```bash
make test
make test-cover
make test-race
make integration       # 需要 TEST_DATABASE_DSN
make bench
make check             # lint + vet + race + OpenAPI + tidy
```

生成接口文档：

```bash
make openapi-gen
make openapi-check
```

## 5. 认证、tenant 与通用约定

公开端点：

```text
GET /healthz
GET /metrics
GET /openapi.json
```

所有 `/api/v1/*` 请求需要：

```http
Authorization: Bearer otj_...
```

API key 只在创建时返回一次明文。服务端保存 bcrypt 摘要。吊销、过期、未知 key 和 suspended tenant 都返回 `401`。

建议让 bearer key 决定 tenant，不要主动传旧 DTO 中的 `tenant_id`。创建新 tenant、跨 tenant API key 管理目前没有完整 RBAC；部署到不可信网络前应在网关层限制管理接口。

可选 trace header：

```http
X-Trace-ID: request-20260712-001
```

服务会原样回传；未提供时自动生成。

Job update、pause、resume 还需要：

```http
X-Actor-ID: deploy-bot
```

所有 list API 使用 `limit`/`offset`，`limit` 范围 1–100，默认通常为 50。当前响应统一按 `{"items": [...]}` 使用，不要依赖 total 字段。

## 6. Tenant 与 API key

创建 tenant：

```bash
curl -sS -X POST "$ORBITJOB_API/api/v1/tenants" \
  -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"slug":"payments","name":"Payments","status":"active"}'
```

```bash
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/tenants?limit=50&offset=0"
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/tenants/<tenant_id>"
```

为 tenant 创建 key。空 DTO 仍要发送 `{}`：

```bash
curl -sS -X POST "$ORBITJOB_API/api/v1/tenants/<tenant_id>/api_keys" \
  -H "$AUTH" -H 'Content-Type: application/json' -d '{}'
```

列出和吊销：

```bash
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/tenants/<tenant_id>/api_keys"
curl -sS -X POST -H "$AUTH" "$ORBITJOB_API/api/v1/api_keys/<key_id>/revoke"
```

## 7. Job 与 Instance

### 7.1 创建 Job

Job 核心字段：

| 字段 | 取值/说明 |
|---|---|
| `trigger_type` | `manual`、`cron` |
| `cron_expr` | 标准五段 cron；manual 时不能设置 |
| `handler_type` | `exec`、`http`、`webhook`、`pg_notify`、`container` |
| `timeout_sec` | 单次执行 deadline |
| `retry_limit` | 失败后的重试上限 |
| `retry_backoff_strategy` | `fixed`、`exponential` |
| `concurrency_policy` | `allow`、`forbid`、`replace` |
| `misfire_policy` | `skip`、`fire_now`、`catch_up` |
| `version` | 更新和状态切换的乐观锁版本 |

HTTP Job 示例：

```bash
curl -sS -X POST "$ORBITJOB_API/api/v1/jobs" \
  -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{
    "name":"reconcile-billing",
    "trigger_type":"manual",
    "handler_type":"http",
    "handler_payload":{
      "url":"https://example.com/tasks/reconcile",
      "method":"POST",
      "headers":{"Content-Type":"application/json"},
      "body":"{\"dry_run\":false}"
    },
    "timeout_sec":30,
    "retry_limit":3,
    "retry_backoff_sec":10,
    "retry_backoff_strategy":"exponential",
    "concurrency_policy":"forbid"
  }'
```

查询：

```bash
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/jobs?status=active&limit=50"
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/jobs/<job_id>"
```

稀疏更新：

```bash
curl -sS -X PUT "$ORBITJOB_API/api/v1/jobs/<job_id>" \
  -H "$AUTH" -H 'X-Actor-ID: deploy-bot' \
  -H 'Content-Type: application/json' \
  -d '{"version":3,"timeout_sec":60}'
```

触发时推荐带幂等键：

```bash
curl -sS -X POST "$ORBITJOB_API/api/v1/jobs/<job_id>/trigger" \
  -H "$AUTH" \
  -H 'X-OrbitJob-Idempotency-Key: billing-close-2026-07-12'
```

新 instance 返回 `201`；重复 key 返回原 instance 和 `200`。

暂停、恢复、删除：

```bash
curl -sS -X POST "$ORBITJOB_API/api/v1/jobs/<job_id>/pause" \
  -H "$AUTH" -H 'X-Actor-ID: deploy-bot' -H 'Content-Type: application/json' \
  -d '{"version":4}'

curl -sS -X POST "$ORBITJOB_API/api/v1/jobs/<job_id>/resume" \
  -H "$AUTH" -H 'X-Actor-ID: deploy-bot' -H 'Content-Type: application/json' \
  -d '{"version":5}'

curl -sS -X DELETE -H "$AUTH" "$ORBITJOB_API/api/v1/jobs/<job_id>"
```

Job delete 是软删除，不要求 version。

### 7.2 Instance

状态流：

```text
pending -> dispatched -> running -> success
                         |-> retry_wait -> pending
                         `-> failed
pending/dispatched/running/retry_wait -> canceled
```

```bash
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/instances?status=failed&limit=50"
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/instances/<run_id>"
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/instances/<run_id>/attempts"

curl -sS -X POST "$ORBITJOB_API/api/v1/instances/<run_id>/cancel" \
  -H "$AUTH" -H 'Content-Type: application/json' -d '{"version":2}'
```

Claim/Lease 是至少一次语义。外部 HTTP、webhook、PG consumer 和 container workload 都要按 `run_id` 或业务键保证幂等。

## 8. Handler payload

### 8.1 `exec`

```json
{"command":"printf","args":["daily-report"],"env":{"MODE":"compact"}}
```

`command` 只能是 PATH 中的程序名，不能包含 `/` 或 `\\`。shell、管道、重定向和 shell 元字符会被拒绝。不要写 `sh -c`。失败 stderr 最多保留 4096 bytes。

### 8.2 `http`

```json
{"url":"https://example.com/hook","method":"POST","headers":{"Content-Type":"application/json"},"body":"{\"event\":\"run\"}"}
```

任意 2xx 为成功。redirect 禁用。worker 会在 DNS 和连接阶段拦截 loopback、RFC1918、link-local、metadata 和 IPv6 private 地址。这个 handler 不适合访问集群内私网服务。

### 8.3 `webhook`

```json
{"url":"https://example.com/hooks/orbitjob","method":"POST","body":"{\"event\":\"run\"}","secret":"shared-secret"}
```

设置 `secret` 后，worker 增加：

```http
X-OrbitJob-Signature: sha256=<HMAC-SHA256 hex>
```

签名输入是原始 body 字符串。

### 8.4 `pg_notify`

```json
{"channel":"billing_events","body":{"kind":"close_period"}}
```

worker 会清理 channel 名并增加 `orbitjob_` 前缀。序列化后的通知上限为 4096 bytes。PG `NOTIFY` 不保证离线消费，不能替代持久消息队列。

### 8.5 `container`

```json
{
  "image":"registry.example.com/jobs/reconcile@sha256:<digest>",
  "command":["/app/reconcile"],
  "args":["--tenant","payments"],
  "image_pull_policy":"IfNotPresent",
  "service_account_name":"orbitjob-task"
}
```

container handler 只在 worker 设置 `WORKER_CONTAINER_ENABLED=true` 时注册，需要 in-cluster Kubernetes client。它创建 `batch/v1 Job`，默认资源为：

```text
request: 100m CPU / 64Mi memory
limit:   1 CPU / 512Mi memory
```

Pod 使用 UID 65534、`runAsNonRoot`、read-only root filesystem、RuntimeDefault seccomp、drop ALL capabilities，并关闭自动挂载 ServiceAccount token。超时时 worker 前台删除 Job。Job 本身不重试，OrbitJob instance retry 负责下一次尝试。

## 9. Check、SLI 与 SLO

### 9.1 Check

当前只有 `http_health`。创建 interval check：

```bash
curl -sS -X POST "$ORBITJOB_API/api/v1/checks" \
  -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{
    "name":"public-api-health",
    "check_type":"http_health",
    "check_config":{"url":"https://example.com/health","method":"GET","expected_status":200},
    "assertion_rules":[{"metric":"response_time_ms","operator":">","threshold":500,"severity":"warning"}],
    "schedule_type":"interval",
    "interval_sec":60,
    "timeout_sec":10
  }'
```

断言表示“何时失败”。上例表示响应时间大于 500ms 时标记 warning。支持 `>`、`<`、`==`、`!=`、`>=`、`<=`。

```bash
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/checks?status=active"
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/check-runs?check_id=<id>"
```

Check pause/resume/delete 都要当前 `version`；delete 也要 JSON body。

### 9.2 SLI

```bash
curl -sS -X POST "$ORBITJOB_API/api/v1/slis" \
  -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{
    "name":"public-api-availability",
    "sli_type":"availability",
    "source_type":"check_run",
    "source_config":{"check_id":17},
    "aggregation":"ratio",
    "good_event_criteria":{"status":"success"}
  }'
```

SLI 当前从 CheckRun 记录五分钟 UTC bucket。`source_config.check_id` 必须是正整数。availability/quality 要明确填写 `good_event_criteria`。

### 9.3 SLO、预算与告警

```bash
curl -sS -X POST "$ORBITJOB_API/api/v1/slos" \
  -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{
    "name":"public-api-99.9",
    "sli_id":9,
    "target":0.999,
    "window_type":"rolling",
    "window_duration":"720h",
    "alert_fast_burn_rate":14.4,
    "alert_slow_burn_rate":6
  }'
```

`window_duration` 使用 Go duration，例如 `168h`、`720h`，最大 `8760h`。slow burn rate 必须小于 fast burn rate。

```bash
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/slos/<id>"
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/slos/<id>/budget"
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/slos/<id>/budgets"
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/slo-alerts?slo_id=<id>&status=firing"
```

没有 Check/SLI 样本时，budget 可能是空状态，不代表接口错误。

## 10. 错误、限流与乐观锁

错误结构：

```json
{"error":{"code":"VALIDATION_ERROR","message":"is required","field":"name"}}
```

| HTTP | code | 场景 |
|---:|---|---|
| 400 | `MALFORMED_REQUEST` | JSON 或 Content-Type 无法解析 |
| 400 | `VALIDATION_ERROR` | 字段或领域规则失败 |
| 401 | `UNAUTHORIZED` | Bearer key 无效 |
| 403 | `FORBIDDEN` | 权限不足 |
| 404 | `NOT_FOUND` | 资源不存在 |
| 409 | `CONFLICT` | stale version、状态冲突、唯一键冲突 |
| 429 | `RATE_LIMITED` | tenant token bucket 耗尽 |
| 500 | `INTERNAL_ERROR` | 未映射错误 |
| 503 | `SERVICE_UNAVAILABLE` | 依赖不可用 |

默认每 tenant 限流：read 100/s、write 10/s、trigger 5/s、cancel 5/s。环境变量 `RATELIMIT_READ_RPS`、`RATELIMIT_WRITE_RPS`、`RATELIMIT_TRIGGER_RPS`、`RATELIMIT_ADMIN_RPS` 同时设置 rate 与 burst。

遇到 `409` 时重新 GET 资源，读取最新 `version`，再重放操作。不要盲目递增本地版本。

## 11. 监控与排障

```bash
make observability-status
curl -fsS http://localhost:8080/metrics
```

Grafana 预置 Prometheus datasource 和 OrbitJob dashboard。Loki datasource 仅在日志 profile 可用时有数据。

常见问题：

### migration 失败

```bash
docker compose logs owner-init migrate
```

- `unsupported pre-release schema history; recreate the database for v0.2.0` 表示数据库来自发布前开发 migration。执行 `make docker-reset`；不要修改 `schema_migrations`。
- `migration 0001 checksum mismatch` 且 ledger 中的 name 是 `v020_baseline`，表示正式 baseline 文件被改写。恢复发布文件，不要用 reset 掩盖 history 变更。
- 后续 migration 的 checksum mismatch 同样要求恢复原文件，或通过新的连续编号 migration 修正 schema。

### bootstrap key 取不到

```bash
docker compose ps secret-init bootstrap
make bootstrap-key
```

如果 volume 权限或 bootstrap 失败，先看两个一次性任务日志。不要在日志中打印 key。

### Job 一直 pending

```bash
docker compose logs dispatcher worker
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/instances/<run_id>"
```

检查 worker heartbeat、handler capability、tenant、container worker 开关和 lease 配置。

### container Job 不运行

```bash
kubectl -n orbitjob-tasks get jobs,pods
kubectl -n orbitjob-tasks describe job <name>
kubectl auth can-i create jobs --as system:serviceaccount:orbitjob-system:<worker-sa> -n orbitjob-tasks
```

确认 image digest、task ServiceAccount、RBAC、namespace 和 Pod Security 限制。

## 12. Smoke test 与示例数据

全接口 smoke test 会创建 tenant、key、Job、Check 和 SLO 数据，并生成 `smoke-test-results.md`：

```bash
ADMIN_BOOTSTRAP_API_KEY="$ORBITJOB_API_KEY" \
  ./scripts/smoke-test.sh http://localhost:8080
```

它不是只读健康检查，不要对生产库运行。

导入示例 Job：

```bash
ADMIN_BOOTSTRAP_API_KEY="$ORBITJOB_API_KEY" \
  ./scripts/import-jobs.sh http://localhost:8080 ./scripts/job-dataset.json
```

## 13. API 索引

完整 schema、请求字段和 response model：

- 仓库文件：`api/openapi.yaml`
- 运行时：`GET /openapi.json`

资源路由：

```text
/api/v1/tenants
/api/v1/tenants/{id}/api_keys
/api/v1/api_keys/{id}/revoke
/api/v1/jobs
/api/v1/jobs/{id}/{pause,resume,trigger}
/api/v1/instances
/api/v1/instances/{run_id}/{cancel,attempts}
/api/v1/checks
/api/v1/checks/{id}/{pause,resume}
/api/v1/check-runs
/api/v1/slis
/api/v1/slos
/api/v1/slos/{id}/{pause,resume,budget,budgets}
/api/v1/slo-alerts
```

OpenAPI 当前没有完整表达 Bearer security，也没有表达 Check/SLI/SLO DELETE 所需的 `{"version":n}` body。调用这三类 DELETE 时以本手册和 handler 行为为准。

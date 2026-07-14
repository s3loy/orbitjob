# OrbitJob 使用手册

本文对应当前 `dev` 分支。部署入口、API 请求和数据库迁移都以仓库内代码为准：

- 本地栈：`Makefile`、`docker-compose.yml`
- Kubernetes：`charts/orbitjob/`
- API schema：`api/openapi.yaml`
- 进程配置：`cmd/*/main.go`、`deploy/env/*.env.example`

## 1. 先选运行方式

| 方式 | 适合场景 | 数据库 | 进程拓扑 |
|---|---|---|---|
| Docker Compose | 本地体验、API 联调 | 自动启动 PostgreSQL 17 | 四个 runtime + 可观测性组件 |
| `devserver` | Go 调试、单步开发 | 需要你提前准备 | 四个 runtime 合并到一个进程 |
| Helm | Kubernetes 部署、container handler | 使用已有 PostgreSQL | 四个独立 Deployment |
| 独立进程 | VM、裸机、自定义编排 | 使用已有 PostgreSQL | 四个独立二进制 |

OrbitJob 当前支持 cron/manual Job、Check/CheckRun、SLI/SLO、API key 认证、tenant 隔离、PostgreSQL role/RLS、Prometheus metrics，以及 memory/etcd 协调。

当前没有 Operator、CRD、Kubernetes Lease、PG epoch fencing、Workflow 或 Serverless。Helm 部署普通 Kubernetes workload；CRD spec 还不是声明权威。

## 2. Docker Compose

这是本地体验的最短路径。

### 2.1 启动

需要 Docker Compose v2 和 `make`。确认 `8080`、`9090`、`3000`、`5432` 没被占用，然后运行：

```bash
make setup
make docker-up
```

`make setup` 生成 installation 级数据库配置。Bundled PostgreSQL 不要求 DSN。`make docker-up` 只消费生成结果，不在 Compose 中手工拼接 role DSN。完整规则见 [`docs/database-setup.md`](docs/database-setup.md)。

`make docker-up` 会：
1. 校验 `.env` 和 `.runtime/database.env` 的权限与内容
2. 校验 Compose 配置
3. 编译镜像并启动 PostgreSQL、owner-init、migration、bootstrap 和四个 runtime
4. 等待 Admin API、Prometheus、Grafana 就绪

`.env` 和 `.runtime/database.env` 权限必须是 `0600`。不要手工编辑派生 DSN。已有 installation 状态会复用密码。

读取本地凭据：

```bash
export ORBITJOB_API_KEY="$(make --no-print-directory bootstrap-key)"
make grafana-password
```

设置后续示例使用的变量：

```bash
export ORBITJOB_API=http://localhost:8080
export AUTH="Authorization: Bearer $ORBITJOB_API_KEY"
```

检查服务：

```bash
curl -fsS "$ORBITJOB_API/healthz"
curl -fsS -H "$AUTH" "$ORBITJOB_API/api/v1/tenants"
make docker-status
```

本地入口：

| 服务 | 地址 |
|---|---|
| Admin API | <http://localhost:8080> |
| OpenAPI | <http://localhost:8080/openapi.json> |
| Prometheus | <http://localhost:9090> |
| Grafana | <http://localhost:3000> |
| PostgreSQL | `localhost:5432` |

### 2.2 服务和启动顺序

| 服务 | 用途 | 主机端口 |
|---|---|---:|
| `pg` | PostgreSQL 17 | `5432` |
| `pgbouncer` | transaction pooling；当前 runtime DSN 不经过它 | 不发布 |
| `migrate` | role 初始化、checksum migration、legacy baseline | 一次性任务 |
| `secret-init` | 初始化非 root secret volume | 一次性任务 |
| `bootstrap` | 创建默认 tenant 和初始 API key | 一次性任务 |
| `admin-api` | HTTP 控制面 | `8080` |
| `scheduler` | 生成到期 instance | 不发布 |
| `dispatcher` | 分发 pending instance | 不发布 |
| `worker` | 执行 handler | 不发布 |
| `prometheus` | metrics | `9090` |
| `grafana` | dashboard | `3000` |
| `etcd` | 可选协调 | profile 启用后为 `2379` |
| `loki`、`promtail` | 可选日志栈 | profile 启用 |

`migrate` 使用 advisory lock、`schema_migrations` 和 SHA-256 checksum。Compose 为旧开发库设置 `MIGRATIONS_BASELINE_VERSION=6`；不要把这个值复制到新部署。一次性任务以退出码 0 结束是正常状态。

查看日志：

```bash
docker compose logs migrate bootstrap
docker compose logs -f admin-api scheduler dispatcher worker
```

### 2.3 可选 profile

启动 etcd 容器：

```bash
docker compose --profile coordination-etcd up -d
```

这条命令只启动 etcd。当前 Compose 没有自动给 runtime 注入 `ETCD_ENABLED=true` 和 `ETCD_ENDPOINTS`。如果要测试 etcd 协调，请先在 Compose override 中给 scheduler、dispatcher、worker 配置这两个变量，再重建对应服务。

启动 Loki 和 Promtail：

```bash
docker compose --profile logs up -d
```

Docker Desktop for macOS 不能直接读取 Linux VM 内的 container log 文件。macOS 上用 `docker compose logs` 更直接。

### 2.4 停止和清理

```bash
make docker-down       # 保留 named volumes
make docker-reset      # 输入 delete-volumes；删除数据库、指标和 secret
make env-clean         # 输入 delete-env；删除 .env
```

`docker-reset` 会删除 PostgreSQL、Prometheus、Grafana、Loki、etcd 和 bootstrap secret 数据。

## 3. 本地 Go 开发

`devserver` 把 admin-api、scheduler、dispatcher、worker 放进一个进程。它适合调试，不代表生产拓扑。

### 3.1 准备数据库

你需要一个已经迁移并完成 bootstrap 的 PostgreSQL。最省事的做法是先启动 Compose，再停掉四个 runtime，只保留数据库和初始化结果：

```bash
make docker-up
docker compose stop admin-api scheduler dispatcher worker
```

使用 `.runtime/database.env` 中已经生成的 admin DSN：

```bash
set -a
. ./.runtime/database.env
set +a

make dev DEV_DSN="$ADMIN_DSN"
```

也可以使用你自己的 PostgreSQL。`make migrate-up` 使用 `golang-migrate`，适合 migration `0001`–`0006` 的旧式开发库；从 `0007` 开始包含 role 和 ownership 变更。新环境或生产环境应使用 `cmd/migrate`、Compose 或 Helm 的 owner-init + migrate 流程。

### 3.2 devserver 限制

- Admin API 默认监听 `8080`，可用 `ADMIN_PORT` 或 `PORT` 修改
- `make dev` 不会创建数据库、执行 migration 或 bootstrap
- devserver 使用传入 DSN 的数据库权限，不复现 Helm/Compose 的 admin/runtime role 分离
- devserver 不启用 Kubernetes container handler
- API 仍要求 Bearer key；开发默认 key只在你以前用开发 bootstrap 创建过对应记录时有效

## 4. 独立进程

编译当前平台的所有 Go 包：

```bash
make build
```

交叉编译 Linux/amd64 产物：

```bash
make build-all
```

产物位于 `bin/`：`admin-api`、`scheduler`、`dispatcher`、`worker`、`bootstrap`。

独立进程应引用同一份 installation 配置。admin-api 使用 `ADMIN_DSN`；scheduler、dispatcher、worker 共用 `RUNTIME_DSN`：

```bash
set -a
. /opt/orbitjob/etc/database.env
set +a
PORT=8080 ./bin/admin-api
SCHEDULER_HEALTH_PORT=6060 ./bin/scheduler
DISPATCHER_HEALTH_PORT=6061 ./bin/dispatcher
WORKER_HEALTH_PORT=6062 ./bin/worker
```

旧的组件专用 DSN 和 `DATABASE_DSN` 仅作为兼容 fallback。

推荐启动顺序：

```text
owner-init -> migrate -> bootstrap -> admin-api/scheduler/dispatcher/worker
```

生产环境不要让四个 runtime 共用 owner 或 superuser DSN。Compose 和 Helm 已把 admin/runtime/migrator role 分开；自定义部署应复用相同边界。

进程环境变量示例见：

```text
deploy/env/admin.env.example
deploy/env/scheduler.env.example
deploy/env/dispatcher.env.example
deploy/env/worker.env.example
```

常用变量：

| 进程 | 变量 | 默认值/含义 |
|---|---|---|
| admin-api | `PORT` | `8080` |
| scheduler | `SCHEDULER_TICK_INTERVAL_SEC` | `5` |
| scheduler | `SCHEDULER_BATCH_SIZE_MAX` | `500` |
| scheduler | `SCHEDULER_HEALTH_PORT` | `6060` |
| dispatcher | `DISPATCHER_TICK_INTERVAL_SEC` | `2` |
| dispatcher | `DISPATCHER_BATCH_SIZE` | `50` |
| dispatcher | `DISPATCHER_HEALTH_PORT` | `6061` |
| worker | `WORKER_POLL_INTERVAL_SEC` | `2` |
| worker | `WORKER_CAPACITY` | `5` |
| worker | `WORKER_CAPACITY_MAX` | `20` |
| worker | `WORKER_HEALTH_PORT` | `6062` |
| worker | `WORKER_TENANT_ID` | 空值时发现所有 active tenant |

独立 scheduler、dispatcher、worker 暴露：

```text
GET /healthz   进程存活
GET /readyz    PostgreSQL 可连接
GET /metrics   Prometheus metrics
```

## 5. Bootstrap 与 API key

Bootstrap 创建固定的默认 tenant 和初始 API key。它可重复执行；已有记录不会重复创建。

开发环境可使用内置 key `otj_devkey_2026`，但生产环境必须设置至少 12 字符的 `ADMIN_BOOTSTRAP_API_KEY`。

本地文件 secret backend：

```bash
ADMIN_BOOTSTRAP_SECRET_BACKEND=local \
ADMIN_BOOTSTRAP_SECRET_ROOT="$PWD/run/secrets/orbitjob" \
ADMIN_BOOTSTRAP_API_KEY='otj_replace_with_local_secret' \
ADMIN_DSN="$DATABASE_DSN" \
./bin/bootstrap
```

新建 key 会写入：

```text
<ADMIN_BOOTSTRAP_SECRET_ROOT>/bootstrap-api-key/api-key
```

如果数据库中已经有 bootstrap key，CLI 不会恢复或重新打印明文。Compose 用 `make bootstrap-key` 读取 named volume；Helm 用 Kubernetes Secret 保存 key。

## 6. Helm 与 Kubernetes

Chart 位于 `charts/orbitjob`，当前版本为 `0.2.0`。它安装：

- owner-init、migration、bootstrap Job
- admin-api、scheduler、dispatcher、worker Deployment
- runtime 与 container task RBAC
- migration ConfigMap
- 可选创建的 container task namespace

Chart 不安装 PostgreSQL，也不创建数据库凭据 Secret。

### 6.1 数据库 Secret

默认 Secret 名是 `orbitjob-database`。它由统一配置工具从一份 bootstrap DSN生成。用户不要分别填写 role password 和 DSN。

```bash
install -m 0600 /dev/null /tmp/orbitjob-bootstrap-dsn
printf '%s\n' 'postgres://<owner>:<password>@<host>:5432/orbitjob?sslmode=require' \
  > /tmp/orbitjob-bootstrap-dsn

go run ./cmd/configure setup \
  --mode external \
  --database-dsn-file /tmp/orbitjob-bootstrap-dsn \
  --state .runtime/database.json \
  --runtime-env .runtime/database.env
```

Kubernetes 安装器从生成状态创建下列固定 key：

```text
bootstrap-owner-dsn
migrator-dsn
admin-dsn
runtime-dsn
migrator-password
admin-password
runtime-password
bootstrap-api-key
```

所有 Deployment 引用同一个 Secret。扩容 worker、scheduler 或 dispatcher 不需要再次配置数据库。完整约束见 [`docs/database-setup.md`](docs/database-setup.md)。

### 6.2 镜像与安装

默认 values 使用本地开发镜像名和 `dev` tag。部署前必须覆盖六个镜像：

```yaml
images:
  admin: {repository: registry.example.com/orbitjob/admin-api, tag: "0.2.0"}
  scheduler: {repository: registry.example.com/orbitjob/scheduler, tag: "0.2.0"}
  dispatcher: {repository: registry.example.com/orbitjob/dispatcher, tag: "0.2.0"}
  bootstrap: {repository: registry.example.com/orbitjob/bootstrap, tag: "0.2.0"}
  migrate: {repository: registry.example.com/orbitjob/migrate, tag: "0.2.0"}

worker:
  image: {repository: registry.example.com/orbitjob/worker, tag: "0.2.0"}
  containerExecution:
    namespace: orbitjob-tasks
    requireDigest: true
    ttlSecondsAfterFinished: 600
    serviceAccountName: orbitjob-task
```

检查 Chart：

```bash
make helm-migrations-check
make helm-check
```

安装：

```bash
helm upgrade --install orbitjob charts/orbitjob \
  --namespace orbitjob-system \
  --create-namespace \
  -f values.production.yaml \
  --wait --wait-for-jobs
```

生产环境建议 `worker.containerExecution.requireDigest: true`。container Job image 必须写成 `image@sha256:...`；可变 tag 会被拒绝。

Chart 当前只为 Admin API 创建 ClusterIP Service，没有内置 Ingress。用端口转发访问：

```bash
kubectl -n orbitjob-system port-forward service/orbitjob-admin-api 8080:8080
```

读取 bootstrap key：

```bash
kubectl -n orbitjob-system get secret orbitjob-bootstrap \
  -o jsonpath='{.data.api-key}' | base64 --decode
printf '\n'
```

### 6.3 kind 验证

需要 Docker、kind、kubectl、Helm 和 OpenSSL：

```bash
make kind-v020-verify
```

脚本会自行创建或复用 `orbitjob-v020` 集群，编译并加载镜像，安装 PostgreSQL 和 Chart，验证重复 upgrade、migration 失败回滚和数据保留。成功时，如果集群由脚本创建，脚本会删除它；失败时保留集群供排查。

`make kind-up`/`make kind-down` 管理的是另一个固定名称 `orbitjob-dev` 集群。它们不是 `kind-v020-verify` 的必需前置步骤。

## 7. 认证和请求约定

公开端点：

```text
GET /healthz
GET /metrics
GET /openapi.json
```

所有 `/api/v1/*` 请求都需要：

```http
Authorization: Bearer otj_...
```

服务端用 API key 解析 tenant，并保存 bcrypt hash。未知、过期、吊销 key 都返回 `401`。

`tenant_id` 仍存在于部分 Job/Check query 或 payload 中，主要用于 bootstrap tenant 的管理路径。普通 tenant key 不应把它当成跨 tenant 授权方式；认证 key 才是 tenant 边界。

可选 trace header：

```http
X-Trace-ID: request-20260713-001
```

服务会回传这个值；未提供时自动生成。

Job update、pause、resume 还要求：

```http
X-Actor-ID: deploy-bot
```

List API 使用 `limit`/`offset`。`limit` 最大为 100；省略时由用例层使用默认值，通常为 50。响应使用 `{"items": [...]}`，不要依赖 `total` 字段。

## 8. Tenant 和 API key

创建 tenant：

```bash
curl -sS -X POST "$ORBITJOB_API/api/v1/tenants" \
  -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"slug":"payments","name":"Payments","status":"active"}'
```

查询：

```bash
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/tenants?limit=50&offset=0"
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/tenants/<tenant_id>"
```

创建 API key。空请求仍发送 `{}`：

```bash
curl -sS -X POST "$ORBITJOB_API/api/v1/tenants/<tenant_id>/api_keys" \
  -H "$AUTH" -H 'Content-Type: application/json' -d '{}'
```

创建响应中的 `key` 是唯一一次明文。立即保存；list API 只返回 metadata。

```bash
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/tenants/<tenant_id>/api_keys"
curl -sS -X POST -H "$AUTH" "$ORBITJOB_API/api/v1/api_keys/<key_id>/revoke"
```

当前管理接口没有细粒度 RBAC。不要把 bootstrap key 暴露给普通业务调用方；在不可信网络前加网关 ACL。

## 9. Job 和 Instance

### 9.1 创建 Job

| 字段 | 取值/说明 |
|---|---|
| `trigger_type` | `manual`、`cron` |
| `cron_expr` | 标准五段 cron；manual 时不要设置 |
| `handler_type` | `exec`、`http`、`webhook`、`pg_notify`、`container` |
| `timeout_sec` | 单次执行 deadline |
| `retry_limit` | 失败后的重试上限 |
| `retry_backoff_strategy` | `fixed`、`exponential` |
| `concurrency_policy` | `allow`、`forbid`、`replace` |
| `misfire_policy` | `skip`、`fire_now`、`catch_up` |
| `version` | update、pause、resume、cancel 的乐观锁版本 |

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

查询和更新：

```bash
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/jobs?status=active&limit=50"
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/jobs/<job_id>"

curl -sS -X PUT "$ORBITJOB_API/api/v1/jobs/<job_id>" \
  -H "$AUTH" -H 'X-Actor-ID: deploy-bot' \
  -H 'Content-Type: application/json' \
  -d '{"version":3,"timeout_sec":60}'
```

触发时建议带幂等键：

```bash
curl -sS -X POST "$ORBITJOB_API/api/v1/jobs/<job_id>/trigger" \
  -H "$AUTH" -H 'Content-Type: application/json' \
  -H 'X-OrbitJob-Idempotency-Key: billing-close-2026-07-13' \
  -d '{}'
```

首次创建返回 `201`；重复 key 返回原 instance 和 `200`。

暂停、恢复和删除：

```bash
curl -sS -X POST "$ORBITJOB_API/api/v1/jobs/<job_id>/pause" \
  -H "$AUTH" -H 'X-Actor-ID: deploy-bot' -H 'Content-Type: application/json' \
  -d '{"version":4}'

curl -sS -X POST "$ORBITJOB_API/api/v1/jobs/<job_id>/resume" \
  -H "$AUTH" -H 'X-Actor-ID: deploy-bot' -H 'Content-Type: application/json' \
  -d '{"version":5}'

curl -sS -X DELETE -H "$AUTH" "$ORBITJOB_API/api/v1/jobs/<job_id>"
```

Job delete 是软删除，不要求 version 或 actor header。

### 9.2 Instance

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

Claim/Lease 是至少一次语义。外部 HTTP、webhook、PG consumer 和 container workload 必须按 `run_id` 或业务键做幂等。

## 10. Handler payload

### 10.1 `exec`

```json
{"command":"printf","args":["daily-report"],"env":{"MODE":"compact"}}
```

`command` 只能是 PATH 中的程序名，不能含 `/` 或 `\\`。shell 本身被禁用，参数也不能含 shell 元字符。worker 容器是 scratch image，默认只有 OrbitJob 二进制和 `/healthcheck`；因此 Compose 中的 `exec` handler 不适合运行任意系统命令。

### 10.2 `http`

```json
{"url":"https://example.com/hook","method":"POST","headers":{"Content-Type":"application/json"},"body":"{\"event\":\"run\"}"}
```

任意 2xx 为成功。redirect 被禁用。worker 在 DNS 校验和连接阶段拦截 loopback、RFC1918、link-local、metadata IP 和 IPv6 private 地址。它不适合访问集群内私网服务。

### 10.3 `webhook`

```json
{"url":"https://example.com/hooks/orbitjob","method":"POST","body":"{\"event\":\"run\"}","secret":"shared-secret"}
```

设置 `secret` 后，worker 增加：

```http
X-OrbitJob-Signature: sha256=<HMAC-SHA256 hex>
```

签名输入是原始 body 字符串。webhook 使用和 `http` 相同的 SSRF 与 redirect 限制。

### 10.4 `pg_notify`

```json
{"channel":"billing_events","body":{"kind":"close_period"}}
```

worker 清理 channel 名并增加 `orbitjob_` 前缀。序列化后的通知上限为 4096 bytes。PG `NOTIFY` 不保留离线消息，不能替代持久消息队列。

### 10.5 `container`

```json
{
  "image":"registry.example.com/jobs/reconcile@sha256:<digest>",
  "command":["/app/reconcile"],
  "args":["--tenant","payments"],
  "image_pull_policy":"IfNotPresent",
  "service_account_name":"orbitjob-task"
}
```

container handler 只在 worker 设置以下变量时注册：

```bash
WORKER_CONTAINER_ENABLED=true
WORKER_CONTAINER_NAMESPACE=orbitjob-tasks
```

它需要 in-cluster Kubernetes client。Helm 默认打开这个 handler。`requireDigest` 通过 `APP_ENV=production` 控制 digest 校验；生产 values 应设为 `true`。

Pod 以 UID 65534 运行，使用 `RuntimeDefault` seccomp、read-only root filesystem、drop ALL capabilities，并关闭 privilege escalation。Job 本身不重试；OrbitJob instance retry 负责下一次尝试。

## 11. Check、SLI 和 SLO

### 11.1 Check

当前只有 `http_health`：

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

断言描述失败条件。上例表示响应时间大于 500ms 时记录 warning。支持 `>`、`<`、`==`、`!=`、`>=`、`<=`。

```bash
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/checks?status=active"
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/check-runs?check_id=<id>"
```

Check pause、resume、delete 都需要当前 `version`；delete 也要 JSON body：

```bash
curl -sS -X DELETE "$ORBITJOB_API/api/v1/checks/<id>" \
  -H "$AUTH" -H 'Content-Type: application/json' -d '{"version":2}'
```

### 11.2 SLI

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

SLI 当前从 CheckRun 生成五分钟 UTC bucket。`source_config.check_id` 必须是正整数。availability/quality 需要明确填写 `good_event_criteria`。

删除 SLI 需要当前 `version`：

```bash
curl -sS -X DELETE "$ORBITJOB_API/api/v1/slis/<id>" \
  -H "$AUTH" -H 'Content-Type: application/json' -d '{"version":2}'
```

### 11.3 SLO、预算和告警

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

SLO pause、resume、delete 都需要当前 `version`。没有 Check/SLI 样本时，budget 可能为空，不代表接口错误。

## 12. 错误、限流和乐观锁

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

默认每 tenant 限流：read 100/s、write 10/s、trigger 5/s、cancel 5/s。`RATELIMIT_READ_RPS`、`RATELIMIT_WRITE_RPS`、`RATELIMIT_TRIGGER_RPS`、`RATELIMIT_ADMIN_RPS` 同时设置 refill rate 和 burst。

遇到 `409` 时重新 GET 资源，读取最新 `version`，再重放操作。不要盲目递增本地版本。

## 13. 监控和排障

```bash
make observability-status
curl -fsS http://localhost:8080/metrics
```

Grafana 预置 Prometheus datasource 和 OrbitJob dashboard。Loki datasource 只有在 `logs` profile 启动后才有日志数据。

### migration 失败

```bash
docker compose logs migrate
```

checksum mismatch 表示已应用 migration 被修改。不要改历史 migration；新增下一个序号。生产环境不要使用 `make migrate-down` 回退 `0007`–`0009`，这些版本明确不可逆。

### bootstrap key 取不到

```bash
docker compose ps secret-init bootstrap
make bootstrap-key
```

如果 volume 权限或 bootstrap 失败，先看两个一次性任务日志。不要把 key 打进日志或提交到 Git。

### Job 一直 pending

```bash
docker compose logs dispatcher worker
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/instances/<run_id>"
```

检查 worker heartbeat、handler capability、tenant、container 开关和 lease 配置。

### container Job 不运行

```bash
kubectl -n orbitjob-tasks get jobs,pods
kubectl -n orbitjob-tasks describe job <name>
kubectl auth can-i create jobs \
  --as system:serviceaccount:orbitjob-system:orbitjob-worker \
  -n orbitjob-tasks
```

检查 image digest、task ServiceAccount、RBAC、namespace 和 Pod Security 限制。

## 14. Smoke test 和示例数据

全接口 smoke test 会创建 tenant、API key、Job、Check 和 SLO 数据，并覆盖 `smoke-test-results.md`：

```bash
ADMIN_BOOTSTRAP_API_KEY="$ORBITJOB_API_KEY" \
  ./scripts/smoke-test.sh http://localhost:8080
```

它会执行 Job，并修改数据库。不要对生产环境运行。

导入示例 Job：

```bash
ADMIN_BOOTSTRAP_API_KEY="$ORBITJOB_API_KEY" \
  ./scripts/import-jobs.sh http://localhost:8080 ./scripts/job-dataset.json
```

## 15. 开发验证

```bash
make test
make test-cover
make test-race
TEST_DATABASE_DSN='postgres://postgres:<password>@127.0.0.1:5432/orbitjob_test?sslmode=disable' make integration
make check
make helm-check
```

`make check` 运行 golangci-lint、`go vet`、race tests、OpenAPI 同步检查和 `go mod tidy` 检查。

`make integration` 不会创建 PostgreSQL。必须提供专用 `TEST_DATABASE_DSN`；测试会修改 schema 和数据。

## 16. API 索引

完整 schema、请求字段和 response model：

- 仓库文件：`api/openapi.yaml`
- 运行时：`GET /openapi.json`

主要路由：

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

OpenAPI 由 `cmd/openapi-gen` 从 HTTP 注册表生成。修改 handler 契约后运行：

```bash
make openapi-gen
make openapi-check
```

# OrbitJob 使用手册

本文对应当前 `dev` 分支。部署入口、API 请求与数据库迁移均以仓库内代码为准：

- 本地栈：`Makefile`、`docker-compose.yml`
- Kubernetes：`charts/orbitjob/`
- API schema：`api/openapi.yaml`
- 进程配置：`cmd/*/main.go`、`deploy/env/*.env.example`

## 1. 部署方式选择

| 方式 | 适用场景 | 数据库 | 进程拓扑 |
|---|---|---|---|
| Docker Compose | 本地体验、API 联调 | 自动启动 PostgreSQL 17 | 四个 runtime + 可观测性组件 |
| `devserver` | Go 调试、单步开发 | 需提前准备 | 四个 runtime 合并为一个进程 |
| Helm | Kubernetes 部署、container handler | 使用已有 PostgreSQL | 四个独立 Deployment |
| 独立进程 | VM、裸机、自定义编排 | 使用已有 PostgreSQL | 四个独立二进制 |

OrbitJob 当前支持 cron/manual Job、Check/CheckRun、SLI/SLO、API key 认证、tenant 隔离、PostgreSQL role/RLS、Prometheus metrics 以及 memory/etcd 协调。

以下能力尚未交付：Operator、CRD、Kubernetes Lease、PG epoch fencing、Workflow、Serverless。Helm 部署标准 Kubernetes workload，CRD spec 尚未成为声明权威。

## 2. Docker Compose

### 2.1 首次安装

需要 Docker Compose v2 与 `make`。确认端口 `8080`、`9090`、`3000`、`5432` 未被占用。

```bash
make setup        # 生成 installation 级数据库配置与凭据
make docker-up    # 编译镜像并启动全部服务
```

`make docker-up` 执行流程：

1. 校验 `.env` 和 `.runtime/database.env` 的权限（`0600`）与内容
2. 校验 Compose 配置
3. 编译镜像，启动 PostgreSQL、owner-init、migration、bootstrap 与四个 runtime
4. 等待 Admin API、Prometheus、Grafana 就绪

数据库配置规则见 [`docs/database-setup.md`](docs/database-setup.md)。已有 installation 状态会复用密码，不可手工编辑派生 DSN。

### 2.2 验证部署

读取凭据并检查服务：

```bash
export ORBITJOB_API_KEY="$(make --no-print-directory bootstrap-key)"
make grafana-password

export ORBITJOB_API=http://localhost:8080
export AUTH="Authorization: Bearer $ORBITJOB_API_KEY"

curl -fsS "$ORBITJOB_API/healthz"
curl -fsS -H "$AUTH" "$ORBITJOB_API/api/v1/tenants"
make docker-status
```

如果 `make docker-status` 显示任何服务未就绪，查看对应日志：

```bash
docker compose logs admin-api scheduler dispatcher worker
```

本地入口：

| 服务 | 地址 |
|---|---|
| Admin API | <http://localhost:8080> |
| OpenAPI | <http://localhost:8080/openapi.json> |
| Prometheus | <http://localhost:9090> |
| Grafana | <http://localhost:3000> |
| PostgreSQL | `localhost:5432` |

### 2.3 服务与启动顺序

| 服务 | 用途 | 主机端口 |
|---|---|---:|
| `pg` | PostgreSQL 17 | `5432`，仅 development override |
| `pgbouncer` | transaction pooling | 不发布 |
| `owner-init` | 创建并收紧数据库 role，设置部署密码 | 一次性任务 |
| `migrate` | 以 migrator 身份执行 checksum migration；拒绝旧开发 schema | 一次性任务 |
| `secret-init` | 准备非 root secret volume | 一次性任务 |
| `bootstrap` | 创建默认 tenant 与初始 API key | 一次性任务 |
| `admin-api` | HTTP 控制面 | `8080` |
| `scheduler` | 生成到期 instance | 不发布 |
| `dispatcher` | 分发 pending instance | 不发布 |
| `worker` | 执行 handler | 不发布 |
| `prometheus` | metrics | `9090` |
| `grafana` | dashboard | `3000` |
| `etcd` | 可选协调 | profile 启用后为 `2379` |
| `loki`、`promtail` | 可选日志栈 | profile 启用 |

`owner-init` 使用 bootstrap database owner 创建 cluster-level role、收紧属性与 membership，并设置部署生成的密码。随后 `migrate` 以 `orbitjob_migrator` 登录，通过 `SET ROLE orbitjob_table_owner` 执行唯一的 `0001_v020_baseline.up.sql`。

v0.2.0 不支持旧开发数据库原地升级，也不提供 legacy rebase。升级前重建本地数据：

```bash
make docker-reset
# 输入 delete-volumes
make docker-up
```

Runner 使用单连接 advisory lock、逐 migration 事务与 SHA-256 checksum。重复运行为 no-op。v0.2.0 发布后的 migration 只能追加，不可重写 baseline。

查看日志：

```bash
docker compose logs migrate bootstrap
docker compose logs -f admin-api scheduler dispatcher worker
```

### 2.4 可选 Profile

启动 etcd 容器：

```bash
docker compose --profile coordination-etcd up -d
```

该命令仅启动 etcd。当前 Compose 不会自动为 runtime 注入 `ETCD_ENABLED=true` 与 `ETCD_ENDPOINTS`。测试 etcd 协调时，需在 Compose override 中为 scheduler、dispatcher、worker 配置这两个变量，再重建对应服务。

启动 Loki 与 Promtail：

```bash
docker compose --profile logs up -d
```

Docker Desktop for macOS 无法直接读取 Linux VM 内的 container log 文件。macOS 上建议使用 `docker compose logs`。

### 2.5 停止、重启与清理

```bash
make docker-down       # 停止全部服务，保留 named volumes（数据不丢失）
make docker-up         # 从保留数据重启
```

```bash
make docker-reset      # 输入 delete-volumes 确认；删除数据库、指标与 secret
make docker-up         # 从头重建
```

```bash
make env-clean         # 输入 delete-env 确认；删除 .env 与 .runtime/
```

日常开发中 `docker-down` + `docker-up` 即可重启；版本升级或数据损坏时使用 `docker-reset`。`docker-reset` 会删除 PostgreSQL、Prometheus、Grafana、Loki、etcd 与 bootstrap secret 数据，不可恢复。

## 3. 本地 Go 开发

`devserver` 将 admin-api、scheduler、dispatcher、worker 合并到一个进程，适合调试，不代表生产拓扑。

### 3.1 准备数据库

需要已迁移并完成 bootstrap 的 PostgreSQL。建议先启动 Compose，再停掉四个 runtime，保留数据库与初始化结果：

```bash
make docker-up
docker compose stop admin-api scheduler dispatcher worker
```

使用 `.runtime/database.env` 中已生成的 admin DSN：

```bash
set -a
. ./.runtime/database.env
set +a

make dev DEV_DSN="$ADMIN_DSN"
```

也可使用自有 PostgreSQL。`make migrate-up` 使用 `golang-migrate`，适用于 migration `0001`–`0006` 的旧式开发库；`0007` 起包含 role 与 ownership 变更。新环境或生产环境应使用 `cmd/migrate`、Compose 或 Helm 的 owner-init + migrate 流程。

### 3.2 devserver 限制

- Admin API 默认监听 `8080`，可通过 `ADMIN_PORT` 或 `PORT` 修改
- `make dev` 不会创建数据库、执行 migration 或 bootstrap
- devserver 使用传入 DSN 的数据库权限，不复现 Helm/Compose 的 admin/runtime role 分离
- devserver 不启用 Kubernetes container handler
- API 仍需 Bearer key；开发默认 key 仅在曾用开发 bootstrap 创建过对应记录时有效

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

旧版组件专用 DSN 与 `DATABASE_DSN` 仅作为兼容 fallback。

推荐启动顺序：

```text
owner-init -> migrate -> bootstrap -> admin-api/scheduler/dispatcher/worker
```

生产环境不可让四个 runtime 共用 owner 或 superuser DSN。Compose 与 Helm 已将 admin/runtime/migrator role 分离；自定义部署应复用相同边界。

进程环境变量示例：

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

停止进程：各进程在前台运行时，按 `Ctrl+C` 等待优雅关闭。后台运行时使用 `kill -TERM <pid>`。四个 runtime 可以任意顺序停止；重新启动时建议按 `admin-api -> scheduler -> dispatcher -> worker` 顺序，确保 worker 启动时 admin-api 已就绪。

## 5. Bootstrap 与 API Key

Bootstrap 创建固定的默认 tenant 与初始 API key，可重复执行；已有记录不会重复创建。

开发环境可使用内置 key `otj_devkey_2026`，生产环境必须设置至少 12 字符的 `ADMIN_BOOTSTRAP_API_KEY`。

本地文件 secret backend：

```bash
ADMIN_BOOTSTRAP_SECRET_BACKEND=local \
ADMIN_BOOTSTRAP_SECRET_ROOT="$PWD/run/secrets/orbitjob" \
ADMIN_BOOTSTRAP_API_KEY='otj_replace_with_local_secret' \
ADMIN_DSN="$DATABASE_DSN" \
./bin/bootstrap
```

新建 key 写入：

```text
<ADMIN_BOOTSTRAP_SECRET_ROOT>/bootstrap-api-key/api-key
```

若数据库中已有 bootstrap key，CLI 不会恢复或重新打印明文。Compose 通过 `make bootstrap-key` 读取 named volume；Helm 通过 Kubernetes Secret 保存 key。

## 6. Helm 与 Kubernetes

Chart 位于 `charts/orbitjob`，当前版本 `0.2.0`。本节假设已有 PostgreSQL 实例。

### 6.1 Namespace 拓扑

安装后的 namespace 分层：

| Namespace | 内容 |
|---|---|
| `orbitjob-system` | 控制面：admin-api、scheduler、dispatcher、worker Deployment |
| `orbitjob-tasks` | worker 创建的 container Job（由 `containerExecution.namespace` 指定） |
| `monitoring` | Grafana + Prometheus（可选，通过 `monitor deploy` 安装） |

一次 installation 使用一个 `orbitjob-database` Secret，所有副本共享。

### 6.2 首次安装

**第一步：准备数据库 Secret。** 从一份 bootstrap DSN 生成：

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

Kubernetes 安装器从生成状态创建 Secret，包含以下固定 key：

```text
bootstrap-owner-dsn  migrator-dsn  admin-dsn  runtime-dsn
migrator-password    admin-password  runtime-password  bootstrap-api-key
```

完整约束见 [`docs/database-setup.md`](docs/database-setup.md)。

**第二步：覆盖镜像与安装。** 默认 values 使用本地开发镜像名，部署前必须覆盖：

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

taskNamespace:
  create: true
  name: orbitjob-tasks
```

安装：

```bash
helm upgrade --install orbitjob charts/orbitjob \
  --namespace orbitjob-system \
  --create-namespace \
  -f values.production.yaml \
  --wait --wait-for-jobs
```

**第三步：验证部署。** 确认所有 Pod 就绪：

```bash
kubectl get pods -n orbitjob-system
helm status orbitjob -n orbitjob-system
```

Chart 当前仅为 Admin API 创建 ClusterIP Service，无内置 Ingress。通过端口转发访问：

```bash
kubectl -n orbitjob-system port-forward svc/orbitjob-admin-api 8080:8080
```

读取 bootstrap key：

```bash
kubectl -n orbitjob-system get secret orbitjob-bootstrap \
  -o jsonpath='{.data.api-key}' | base64 --decode
printf '\n'
```

验证 API：

```bash
export ORBITJOB_API_KEY="<bootstrap key>"
curl -fsS http://localhost:8080/healthz
curl -fsS -H "Authorization: Bearer $ORBITJOB_API_KEY" http://localhost:8080/api/v1/tenants
```

### 6.3 日常运维

所有命令以 `orbitjob-system` namespace 为上下文。建议设置默认 namespace 减少重复输入：

```bash
kubectl config set-context --current --namespace=orbitjob-system
```

**观察：**

| 命令 | 用途 |
|---|---|
| `kubectl get pods -A` | 全集群概览，快速定位异常 Pod |
| `kubectl get pods,svc,deploy -o wide` | 控制面资源 + IP + 节点 |
| `kubectl get jobs -n orbitjob-tasks` | container Job 执行情况 |
| `kubectl get endpoints` | 确认 Service 背后有 Pod——空 endpoints 是常见故障源 |
| `helm list -A` | 已安装的 release 与 revision |

**诊断：**

| 命令 | 用途 |
|---|---|
| `kubectl describe pod <pod>` | Events、probe 配置、镜像、环境变量 |
| `kubectl logs <pod> --tail=50` | 最近日志 |
| `kubectl logs -l app=orbitjob-worker --tail=50` | 按标签查日志，不写死 pod 名 |
| `kubectl logs <pod> --previous` | 上一次崩溃前的日志（pod 反复重启时使用） |
| `kubectl get events --sort-by=.lastTimestamp` | 按时间排序的集群事件 |

OrbitJob 镜像为 distroless，不含 shell。`kubectl exec -it <pod> -- sh` 会报 `executable file not found`。如需进入容器，使用 ephemeral debug container：

```bash
kubectl debug -it <pod> --image=busybox --target=<container>
```

绝大多数场景下 `kubectl logs` + `kubectl get endpoints` 已足够，无需 exec。

**端口转发**（前台占用终端，每个端口各开一个）：

```bash
kubectl port-forward svc/orbitjob-admin-api 8080:8080
kubectl -n monitoring port-forward svc/grafana 3000:3000
kubectl -n monitoring port-forward svc/prometheus 9090:9090
```

**重启与扩缩：**

```bash
kubectl rollout restart deploy/orbitjob-worker    # 滚动重启
kubectl rollout status deploy/orbitjob-worker     # 观察滚动状态
kubectl scale deploy/orbitjob-worker --replicas=2 # 扩缩
kubectl delete pod <pod>                          # 删 pod 强制重建
```

### 6.4 升级与卸载

修改 Chart 或 values 后：

```bash
# 如果更新了镜像，先加载到 kind 节点
kind load docker-image --name orbitjob-dev <image>:<tag>

# 升级 release
helm upgrade orbitjob charts/orbitjob -n orbitjob-system -f values.production.yaml --wait
```

修改 `db/migrations/*.up.sql` 后同步 Chart 副本：

```bash
make helm-migrations-sync
make helm-migrations-check
```

卸载：

```bash
helm uninstall orbitjob -n orbitjob-system
```

Chart 不会删除外部 PostgreSQL 数据，也不会清理 `orbitjob-tasks` namespace 中的残留 Job。

### 6.5 常见排查

**Pod Running 但不 Ready。** 三步定位：

1. `kubectl describe pod` — 查看 probe 失败原因（`Readiness probe failed: ...`）
2. `kubectl logs` — 查看应用错误
3. `kubectl get endpoints` — 确认依赖的 Service 有后端

Probe 失败通常是依赖未就绪（数据库、下游 Service），不是进程崩溃。进程崩溃使用 `--previous` 日志排查。

**PostgreSQL 连接失败。** Worker `0/1` Running 不 Ready，`kubectl logs` 显示 `connection refused`。检查：

```bash
kubectl get endpoints orbitjob-postgres -n orbitjob-system
```

若显示 `<none>`，说明 Service 背后没有 Pod——deployment replicas 为 0 或 PostgreSQL 未部署到该 namespace。

**控制面"假健康"。** Admin-api、scheduler、dispatcher 显示 `1/1 Running` 不代表能处理业务。它们的 `/readyz` 不检查数据库连接；只有 worker 的 `/readyz` 会检查。判断控制面是否真正正常，查看 worker 日志中的数据库连接错误。

**Container Job 不运行：**

```bash
kubectl -n orbitjob-tasks get jobs,pods
kubectl -n orbitjob-tasks describe job <name>
kubectl auth can-i create jobs \
  --as system:serviceaccount:orbitjob-system:orbitjob-worker \
  -n orbitjob-tasks
```

检查 image digest、task ServiceAccount、RBAC、namespace 与 Pod Security 限制。

**ImagePullBackOff。** 改完镜像后忘记 `kind load docker-image`，导致 kind 节点上找不到本地构建的镜像。重建镜像后执行 `kind load docker-image --name <cluster> <image>:<tag>`，再删除 pod 强制重建。

### 6.6 kind 本地验证

需要 Docker、kind、kubectl、Helm 与 OpenSSL：

```bash
make kind-v020-verify
```

脚本自行创建或复用 `orbitjob-v020` 集群，编译并加载镜像，安装 PostgreSQL 与 Chart，验证重复 upgrade、migration 失败回滚与数据保留。成功时自动删除临时集群，失败时保留供排查。

日常开发使用 `orbitjob-dev` 集群：

```bash
make kind-up         # 创建集群
make kind-status     # 查看节点和 Pod
make kind-down       # 输入 delete-kind 确认后删除集群
```

## 7. 认证与请求约定

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

服务端通过 API key 解析 tenant，保存 bcrypt hash。未知、过期、吊销 key 均返回 `401`。

`tenant_id` 仍存在于部分 Job/Check query 或 payload 中，主要用于 bootstrap tenant 的管理路径。普通 tenant key 不可将其作为跨 tenant 授权方式；认证 key 才是 tenant 边界。

可选 trace header：

```http
X-Trace-ID: request-20260713-001
```

服务回传该值；未提供时自动生成。

Job update、pause、resume 还需：

```http
X-Actor-ID: deploy-bot
```

List API 使用 `limit`/`offset`。`limit` 最大为 100；省略时由用例层使用默认值（通常为 50）。响应格式为 `{"items": [...]}`，不可依赖 `total` 字段。

## 8. Tenant 与 API Key

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

创建 API key（空请求仍需发送 `{}`）：

```bash
curl -sS -X POST "$ORBITJOB_API/api/v1/tenants/<tenant_id>/api_keys" \
  -H "$AUTH" -H 'Content-Type: application/json' -d '{}'
```

创建响应中的 `key` 是唯一一次明文，须立即保存；list API 仅返回 metadata。

```bash
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/tenants/<tenant_id>/api_keys"
curl -sS -X POST -H "$AUTH" "$ORBITJOB_API/api/v1/api_keys/<key_id>/revoke"
```

当前管理接口没有细粒度 RBAC。不可将 bootstrap key 暴露给普通业务调用方；在不可信网络前增加网关 ACL。

## 9. Job 与 Instance

### 9.1 创建 Job

| 字段 | 取值/说明 |
|---|---|
| `trigger_type` | `manual`、`cron` |
| `cron_expr` | 标准五段 cron；manual 时不设置 |
| `handler_type` | `exec`、`http`、`webhook`、`pg_notify`、`container` |
| `timeout_sec` | 单次执行 deadline |
| `retry_limit` | 失败后重试上限 |
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

查询与更新：

```bash
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/jobs?status=active&limit=50"
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/jobs/<job_id>"

curl -sS -X PUT "$ORBITJOB_API/api/v1/jobs/<job_id>" \
  -H "$AUTH" -H 'X-Actor-ID: deploy-bot' \
  -H 'Content-Type: application/json' \
  -d '{"version":3,"timeout_sec":60}'
```

触发时建议携带幂等键：

```bash
curl -sS -X POST "$ORBITJOB_API/api/v1/jobs/<job_id>/trigger" \
  -H "$AUTH" -H 'Content-Type: application/json' \
  -H 'X-OrbitJob-Idempotency-Key: billing-close-2026-07-13' \
  -d '{}'
```

首次创建返回 `201`；重复 key 返回原 instance 与 `200`。

暂停、恢复与删除：

```bash
curl -sS -X POST "$ORBITJOB_API/api/v1/jobs/<job_id>/pause" \
  -H "$AUTH" -H 'X-Actor-ID: deploy-bot' -H 'Content-Type: application/json' \
  -d '{"version":4}'

curl -sS -X POST "$ORBITJOB_API/api/v1/jobs/<job_id>/resume" \
  -H "$AUTH" -H 'X-Actor-ID: deploy-bot' -H 'Content-Type: application/json' \
  -d '{"version":5}'

curl -sS -X DELETE -H "$AUTH" "$ORBITJOB_API/api/v1/jobs/<job_id>"
```

Job delete 为软删除，不要求 version 或 actor header。

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

Claim/Lease 为至少一次语义。外部 HTTP、webhook、PG consumer 与 container workload 必须按 `run_id` 或业务键实现幂等。

## 10. Handler Payload

### 10.1 `exec`

```json
{"command":"printf","args":["daily-report"],"env":{"MODE":"compact"}}
```

`command` 仅允许 PATH 中的程序名，不可含 `/` 或 `\\`。shell 被禁用，参数不可含 shell 元字符。Worker 容器为 scratch image，默认仅含 OrbitJob 二进制与 `/healthcheck`；Compose 中的 `exec` handler 不适合运行任意系统命令。

### 10.2 `http`

```json
{"url":"https://example.com/hook","method":"POST","headers":{"Content-Type":"application/json"},"body":"{\"event\":\"run\"}"}
```

任意 2xx 视为成功。redirect 被禁用。Worker 在 DNS 校验与连接阶段拦截 loopback、RFC1918、link-local、metadata IP 与 IPv6 private 地址。不适合访问集群内私网服务。

### 10.3 `webhook`

```json
{"url":"https://example.com/hooks/orbitjob","method":"POST","body":"{\"event\":\"run\"}","secret":"shared-secret"}
```

设置 `secret` 后，worker 增加：

```http
X-OrbitJob-Signature: sha256=<HMAC-SHA256 hex>
```

签名输入为原始 body 字符串。Webhook 使用与 `http` 相同的 SSRF 与 redirect 限制。

### 10.4 `pg_notify`

```json
{"channel":"billing_events","body":{"kind":"close_period"}}
```

Worker 清理 channel 名并添加 `orbitjob_` 前缀。序列化后的通知上限为 4096 bytes。PG `NOTIFY` 不保留离线消息，不可替代持久消息队列。

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

Container handler 仅在 worker 设置以下变量时注册：

```bash
WORKER_CONTAINER_ENABLED=true
WORKER_CONTAINER_NAMESPACE=orbitjob-tasks
```

需要 in-cluster Kubernetes client。Helm 默认启用该 handler。`requireDigest` 通过 `APP_ENV=production` 控制 digest 校验；生产 values 应设为 `true`。

Pod 以 UID 65534 运行，使用 `RuntimeDefault` seccomp、read-only root filesystem、drop ALL capabilities，关闭 privilege escalation。Job 本身不重试；OrbitJob instance retry 负责下一次尝试。

## 11. Check、SLI 与 SLO

### 11.1 Check

当前仅支持 `http_health`：

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

断言描述失败条件。上例表示响应时间超过 500ms 时记录 warning。支持 `>`、`<`、`==`、`!=`、`>=`、`<=`。

```bash
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/checks?status=active"
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/check-runs?check_id=<id>"
```

Check pause、resume、delete 均需当前 `version`；delete 也需 JSON body：

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

SLI 当前从 CheckRun 生成五分钟 UTC bucket。`source_config.check_id` 必须为正整数。availability/quality 需要明确填写 `good_event_criteria`。

删除 SLI 需当前 `version`：

```bash
curl -sS -X DELETE "$ORBITJOB_API/api/v1/slis/<id>" \
  -H "$AUTH" -H 'Content-Type: application/json' -d '{"version":2}'
```

### 11.3 SLO、预算与告警

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

`window_duration` 使用 Go duration 格式，例如 `168h`、`720h`，最大 `8760h`。slow burn rate 必须小于 fast burn rate。

```bash
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/slos/<id>"
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/slos/<id>/budget"
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/slos/<id>/budgets"
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/slo-alerts?slo_id=<id>&status=firing"
```

SLO pause、resume、delete 均需当前 `version`。无 Check/SLI 样本时 budget 可能为空，不代表接口错误。

## 12. 错误、限流与乐观锁

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

默认每 tenant 限流：read 100/s、write 10/s、trigger 5/s、cancel 5/s。`RATELIMIT_READ_RPS`、`RATELIMIT_WRITE_RPS`、`RATELIMIT_TRIGGER_RPS`、`RATELIMIT_ADMIN_RPS` 同时设置 refill rate 与 burst。

遇到 `409` 时重新 GET 资源，读取最新 `version`，再重放操作。不可盲目递增本地版本。

## 13. 监控与排障

```bash
make observability-status
curl -fsS http://localhost:8080/metrics
```

Grafana 预置 Prometheus datasource 与 OrbitJob dashboard。Loki datasource 仅在 `logs` profile 启动后有日志数据。

### Migration 失败

```bash
docker compose logs owner-init migrate
```

- `unsupported pre-release schema history; recreate the database for v0.2.0`：数据库来自发布前开发 migration。执行 `make docker-reset`；不可修改 `schema_migrations`。
- `migration 0001 checksum mismatch` 且 ledger 中 name 为 `v020_baseline`：正式 baseline 文件被改写。恢复发布文件，不可用 reset 掩盖 history 变更。
- 后续 migration 的 checksum mismatch：同样要求恢复原文件，或通过新的连续编号 migration 修正 schema。

### Bootstrap Key 获取失败

```bash
docker compose ps secret-init bootstrap
make bootstrap-key
```

若 volume 权限或 bootstrap 失败，先查看两个一次性任务日志。不可将 key 写入日志或提交到 Git。

### Job 持续 Pending

```bash
docker compose logs dispatcher worker
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/instances/<run_id>"
```

检查 worker heartbeat、handler capability、tenant、container 开关与 lease 配置。

### Container Job 未运行

```bash
kubectl -n orbitjob-tasks get jobs,pods
kubectl -n orbitjob-tasks describe job <name>
kubectl auth can-i create jobs \
  --as system:serviceaccount:orbitjob-system:orbitjob-worker \
  -n orbitjob-tasks
```

检查 image digest、task ServiceAccount、RBAC、namespace 与 Pod Security 限制。

## 14. Smoke Test 与示例数据

全接口 smoke test 创建 tenant、API key、Job、Check 与 SLO 数据，并覆盖 `smoke-test-results.md`：

```bash
ADMIN_BOOTSTRAP_API_KEY="$ORBITJOB_API_KEY" \
  ./scripts/smoke-test.sh http://localhost:8080
```

该脚本会执行 Job 并修改数据库。不可对生产环境运行。

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

`make check` 运行 golangci-lint、`go vet`、race tests、OpenAPI 同步检查与 `go mod tidy` 检查。

`make integration` 不会创建 PostgreSQL。必须提供专用 `TEST_DATABASE_DSN`；测试会修改 schema 与数据。

## 16. API 索引

完整 schema、请求字段与 response model：

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

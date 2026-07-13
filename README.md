# OrbitJob

[![Go](https://img.shields.io/badge/Go-1.26.5-00ADD8?logo=go)](https://go.dev)
[![License](https://img.shields.io/github/license/s3loy/orbitjob)](./LICENSE)
[![golangci-lint](https://github.com/s3loy/orbitjob/actions/workflows/lint.yml/badge.svg)](https://github.com/s3loy/orbitjob/actions/workflows/lint.yml)
[![Build Status](https://github.com/s3loy/orbitjob/actions/workflows/ci.yml/badge.svg)](https://github.com/s3loy/orbitjob/actions/workflows/ci.yml)
[![govulncheck](https://github.com/s3loy/orbitjob/actions/workflows/govulncheck.yml/badge.svg)](https://github.com/s3loy/orbitjob/actions/workflows/govulncheck.yml)
[![Coverage Status](https://codecov.io/gh/s3loy/orbitjob/graph/badge.svg)](https://codecov.io/gh/s3loy/orbitjob)

[English](./README.en.md)

![Stone Badge](https://stone.professorlee.work/api/stone/s3loy/orbitjob)

OrbitJob 是 Kubernetes-first 的分布式作业调度平台。PostgreSQL 保存执行事务状态；`admin-api`、`scheduler`、`dispatcher`、`worker` 分别处理控制面、触发、分发与执行。

适合：多租户 cron 任务、API 触发任务、Kubernetes Job 执行、HTTP 巡检和 SLO 计算。

不适合：毫秒级实时任务、恰好一次执行、用 PG `NOTIFY` 替代持久消息队列。OrbitJob 使用 Claim/Lease 至少一次语义，handler 必须幂等。

## 已有能力

- cron 与 manual 触发；fixed/exponential retry；misfire 和 concurrency policy
- `exec`、`http`、`webhook`、`pg_notify`、`container` handler
- Kubernetes Job container 执行，带 digest 校验、RBAC 和受限 Pod Security
- API key 认证、tenant 隔离、PostgreSQL role、RLS 与 `SECURITY DEFINER` 入口
- checksum migration runner、advisory lock、正式 v0.2.0 baseline 和旧开发数据库 reset 防护
- Check、CheckRun、SLI、SLO、错误预算和燃烧率告警
- Docker Compose、Helm Chart 和 kind 安装/升级验证
- Prometheus metrics、Grafana dashboard、结构化日志和 trace ID
- memory 与 etcd election/discovery

尚未交付：Operator、CRD、Kubernetes Lease、PG epoch fencing、Workflow、Serverless。Helm 目前部署普通 Kubernetes workload；CRD spec 还不是声明权威。

## 核心取舍

- PostgreSQL 是持久状态源。事务、`FOR UPDATE SKIP LOCKED` 和 `version` 乐观锁保证状态转换。
- 多 scheduler/worker 可以并发抢占，但吞吐上限仍受 PG 连接数和热点行影响。
- runtime 使用独立的 admin/runtime PG role。认证前查询和跨 tenant 调度通过收窄权限的数据库函数完成。
- container handler 创建 `batch/v1 Job`。Pod 默认以 UID 65534 运行，关闭 privilege escalation，drop ALL capabilities，使用 read-only root filesystem。
- K8s Lease 尚未实现。需要跨进程 election/discovery 时使用 etcd。

## Docker Compose 快速启动

需要 Docker Compose v2 和 `make`：

> **v0.2.0 数据库重建要求**
>
> v0.2.0 将发布前的 `0001`–`0011` 开发 migration 合并为一个正式 baseline。旧开发数据库和 named volume 不支持原地升级。首次运行本版本前，执行 `make docker-reset` 删除旧 volume，再执行 `make docker-up`。
>
> Runner 检测到旧开发 history 时会拒绝启动：
>
> ```text
> unsupported pre-release schema history; recreate the database for v0.2.0
> ```
>
> 不要手工修改 `schema_migrations`，也不要插入伪造的 baseline 记录。

```bash
make docker-up
```

命令会生成随机 `.env` 凭据，运行 role 初始化、migration、bootstrap，并等待 runtime、Prometheus 和 Grafana 就绪。不要把 `.env.example` 的空占位符直接复制进 `.env`。

```bash
export ORBITJOB_API_KEY="$(make --no-print-directory bootstrap-key)"
curl -fsS http://localhost:8080/healthz
curl -fsS \
  -H "Authorization: Bearer $ORBITJOB_API_KEY" \
  http://localhost:8080/api/v1/tenants
```

本地入口：

| 服务 | 地址 |
|---|---|
| Admin API | <http://localhost:8080> |
| OpenAPI | <http://localhost:8080/openapi.json> |
| Prometheus | <http://localhost:9090> |
| Grafana | <http://localhost:3000> |

```bash
make docker-status
make grafana-password
make docker-down          # 保留数据
make docker-reset         # 确认后删除 volumes
```

启用可选服务：

```bash
docker compose --profile coordination-etcd up -d
docker compose --profile logs up -d
```

启动 etcd 容器不会自动切换 runtime。还需要设置 `ETCD_ENABLED=true` 和 `ETCD_ENDPOINTS`。

## Helm 与 Kubernetes

Chart 版本为 `0.2.0`，位于 `charts/orbitjob`。Chart 安装 owner-init、migration、bootstrap、四个 runtime、RBAC 和 container task namespace；它不安装 PostgreSQL。

```bash
make helm-check

helm upgrade --install orbitjob charts/orbitjob \
  --namespace orbitjob-system \
  --create-namespace \
  -f values.production.yaml
```

生产环境建议：

```yaml
worker:
  containerExecution:
    requireDigest: true
```

这会拒绝只有 tag、没有 `@sha256:` digest 的 workload image。

本地 kind 验证：

```bash
make kind-up
make kind-v020-verify
make kind-down
```

验证脚本覆盖首次安装、重复升级和 migration 失败阻断。

## API 示例

```bash
export ORBITJOB_API=http://localhost:8080
export AUTH="Authorization: Bearer $ORBITJOB_API_KEY"

curl -sS -X POST "$ORBITJOB_API/api/v1/jobs" \
  -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{
    "name":"daily-report",
    "trigger_type":"cron",
    "cron_expr":"0 9 * * *",
    "timezone":"UTC",
    "handler_type":"http",
    "handler_payload":{
      "url":"https://example.com/report",
      "method":"POST",
      "body":"{\"source\":\"orbitjob\"}"
    },
    "timeout_sec":30,
    "retry_limit":3,
    "concurrency_policy":"forbid"
  }'
```

Admin API 使用统一错误结构：

```json
{"error":{"code":"VALIDATION_ERROR","message":"is required","field":"name"}}
```

完整 curl 示例、handler payload、Helm Secret 契约和排障步骤见 [USAGE.md](./USAGE.md)。接口 schema 见 [api/openapi.yaml](./api/openapi.yaml)。

## 开发与验证

Go 版本和依赖见 `go.mod`。

```bash
make test
make test-cover
make check
make integration # 需要 TEST_DATABASE_DSN
make helm-check
```

`make check` 包含 golangci-lint、vet、race test、OpenAPI 同步和 `go mod tidy` 检查。

开发分支从 `dev` 分出，PR 合回 `dev`。提交规则和覆盖率要求见 [CONTRIBUTING.md](./CONTRIBUTING.md)。安全报告方式和当前数据库安全边界见 [SECURITY.md](./SECURITY.md)。

## License

[BSD 3-Clause](./LICENSE)

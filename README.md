# OrbitJob

[![Go](https://img.shields.io/badge/Go-1.26.5-00ADD8?logo=go)](https://go.dev)
[![License](https://img.shields.io/github/license/s3loy/orbitjob)](./LICENSE)
[![golangci-lint](https://github.com/s3loy/orbitjob/actions/workflows/lint.yml/badge.svg)](https://github.com/s3loy/orbitjob/actions/workflows/lint.yml)
[![Build Status](https://github.com/s3loy/orbitjob/actions/workflows/ci.yml/badge.svg)](https://github.com/s3loy/orbitjob/actions/workflows/ci.yml)
[![govulncheck](https://github.com/s3loy/orbitjob/actions/workflows/govulncheck.yml/badge.svg)](https://github.com/s3loy/orbitjob/actions/workflows/govulncheck.yml)
[![Coverage Status](https://codecov.io/gh/s3loy/orbitjob/graph/badge.svg)](https://codecov.io/gh/s3loy/orbitjob)

[English](./README.en.md)

OrbitJob 是 Kubernetes-first 的分布式作业调度平台。PostgreSQL 作为持久状态源存储执行事务状态；`admin-api`、`scheduler`、`dispatcher`、`worker` 四个独立进程分别处理控制面管理、定时触发、任务分发与执行。

OrbitJob 面向多租户 cron 与 API 触发任务、Kubernetes Job 执行、HTTP 健康检查（Check/CheckRun）以及 SLI/SLO 计算与错误预算告警。调度语义为 Claim/Lease 至少一次投递，handler 需实现幂等。

四个进程围绕 PostgreSQL 协作：`scheduler` 扫描到期 Job 生成 Instance，`dispatcher` 将 pending Instance 分发给具备对应 handler 能力的 `worker` 执行，`admin-api` 提供 HTTP 控制面。多 scheduler/worker 通过 `FOR UPDATE SKIP LOCKED` 并发抢占，跨进程 election 与 discovery 支持 memory 与 etcd 两种协调后端。

## 开始使用

需要 Docker Compose v2 与 `make`。确认端口 `8080`、`9090`、`3000`、`5432` 未被占用。完整部署方式、API 示例、handler payload 与排障步骤见 [USAGE.md](./USAGE.md)。以下为最简启动路径。

### Docker Compose

```bash
make setup
make docker-up
```

```bash
export ORBITJOB_API_KEY="$(make --no-print-directory bootstrap-key)"
curl -fsS http://localhost:8080/healthz
curl -fsS -H "Authorization: Bearer $ORBITJOB_API_KEY" http://localhost:8080/api/v1/tenants
```

`make setup` 生成 installation 级数据库配置，内嵌 PostgreSQL 无需提供 DSN。Admin API 监听 `8080`，同时提供 Prometheus metrics（`/metrics`）与 OpenAPI schema（`/openapi.json`）。

### Helm

```bash
helm upgrade --install orbitjob charts/orbitjob \
  --namespace orbitjob-system --create-namespace \
  -f values.production.yaml
```

Chart 版本 `0.2.0`，安装四个 runtime Deployment、RBAC 与 container task namespace。不安装 PostgreSQL。一次 installation 使用一个 `orbitjob-database` Secret，所有副本共享。数据库配置流程见 [`docs/database-setup.md`](docs/database-setup.md)。

## 开始开发

阅读 [CONTRIBUTING.md](./CONTRIBUTING.md) 了解分支策略、代码规范、测试分层与 PR 流程。

```bash
git clone https://github.com/s3loy/orbitjob.git
cd orbitjob

make test          # 单元测试
make test-cover    # 覆盖率
make check         # lint + vet + race + openapi-check + tidy-check
make integration   # 集成测试（需要 TEST_DATABASE_DSN）
```

## 功能特性

- cron 与 manual 触发；fixed/exponential retry；misfire 与 concurrency policy
- `exec`、`http`、`webhook`、`pg_notify`、`container` 五种 handler 类型
- Kubernetes Job 容器执行，含 image digest 校验、RBAC 与受限 Pod Security Context
- API key 认证、tenant 隔离、PostgreSQL role、RLS 与 `SECURITY DEFINER` 入口函数
- Check、CheckRun、SLI、SLO、错误预算与燃烧率告警
- Prometheus metrics、Grafana dashboard、结构化日志与 trace ID 透传
- memory 与 etcd election/discovery

以下能力尚未交付：Operator、CRD、Kubernetes Lease、PG epoch fencing、Workflow、Serverless。

## 文档索引

| 文档 | 内容 |
|---|---|
| [USAGE.md](./USAGE.md) | 部署方式选择、Docker Compose / Helm / 独立进程、API 完整示例、handler payload、Check/SLI/SLO 操作、监控排障 |
| [CONTRIBUTING.md](./CONTRIBUTING.md) | 分支命名、依赖方向、测试分层、migration 规范、commit 格式、PR 模板 |
| [SECURITY.md](./SECURITY.md) | 安全模型、漏洞报告流程、PostgreSQL role 与 RLS 边界、容器安全约束 |
| [docs/database-setup.md](docs/database-setup.md) | Bundled / External PostgreSQL 配置、TLS、Kubernetes Secret 契约 |
| [api/openapi.yaml](./api/openapi.yaml) | API schema |

## 社区与支持

- [GitHub Issues](https://github.com/s3loy/orbitjob/issues) — 报告 bug 或请求功能
- [SECURITY.md](./SECURITY.md) — 安全漏洞请勿公开披露，按文件内流程报告

## License

[BSD 3-Clause](./LICENSE)

# OrbitJob

[![Go](https://img.shields.io/badge/Go-1.27.1-00ADD8?logo=go)](https://go.dev)
[![License](https://img.shields.io/github/license/s3loy/orbitjob)](./LICENSE)
[![golangci-lint](https://github.com/s3loy/orbitjob/actions/workflows/lint.yml/badge.svg)](https://github.com/s3loy/orbitjob/actions/workflows/lint.yml)
[![Build Status](https://github.com/s3loy/orbitjob/actions/workflows/ci.yml/badge.svg)](https://github.com/s3loy/orbitjob/actions/workflows/ci.yml)
[![govulncheck](https://github.com/s3loy/orbitjob/actions/workflows/govulncheck.yml/badge.svg)](https://github.com/s3loy/orbitjob/actions/workflows/govulncheck.yml)
[![Coverage Status](https://codecov.io/gh/s3loy/orbitjob/graph/badge.svg)](https://codecov.io/gh/s3loy/orbitjob)

[English](./README.md)

一个跑在 Kubernetes 上的 run ledger

## 快速启动

使用已发布版本快速安装需要 `kind`、`kubectl`、`helm` 和 `make`：

```bash
# 1. 创建 kind 集群
make kind-up

# 2. 安装 PostgreSQL 并生成 orbitjob-database Secret
make kind-db

# 3. 安装 v0.2.1 镜像；values 文件提供本地 tenant 映射
helm upgrade --install orbitjob ./charts/orbitjob \
  --namespace orbitjob-system \
  -f deploy/kind/values-dev.yaml \
  --set-string global.imageTag=v0.2.1 \
  --set-string global.imagePullPolicy=IfNotPresent \
  --wait --timeout=10m

# 4. 导出 API key 并验证
source <(make kind-env)
kubectl -n orbitjob-system port-forward svc/orbitjob-admin-api 18080:8080 &
curl -H "Authorization: Bearer $ORBITJOB_API_KEY" http://localhost:18080/api/v1/tenants
```

验证本地源码时运行 `bash scripts/quickstart.sh`，它会构建并加载 `:dev`
镜像。完整流程见 [`docs/local-deployment.md`](docs/local-deployment.md)。

Admin API 顺带暴露 `/metrics` 和 `/openapi.json`。

监控是独立的 kube-prometheus-stack 发布：

```bash
make monitoring-up
kubectl -n monitoring port-forward svc/kube-prometheus-stack-prometheus 9090:9090 &
kubectl -n monitoring port-forward svc/kube-prometheus-stack-grafana 3000:80 &
```

镜像从 `ghcr.io/s3loy` 拉取，CI 在打 tag 时构建推送。

本地集群验证，见 [`docs/local-deployment.md`](docs/local-deployment.md)。

## Helm

```bash
helm upgrade --install orbitjob charts/orbitjob \
  --namespace orbitjob-system --create-namespace \
  -f my-values.yaml \
  --set-string global.imageTag=v0.2.1
```

`operator.namespaceTenants` 必填，是 namespace 到 tenant 的映射。

Chart 不带 PostgreSQL，数据库配置见 [`docs/database-setup.md`](docs/database-setup.md)。

## 开发

```bash
git clone https://github.com/s3loy/orbitjob.git
cd orbitjob

make test          # 单元测试
make test-cover    # 覆盖率
make check         # lint + vet + race + openapi-check + tidy-check
make integration   # 集成测试（需要 TEST_DATABASE_DSN）
```

分支策略、测试分层、commit 格式见 [CONTRIBUTING.md](./CONTRIBUTING.md)。

## 功能

功能列表，不完备：

* 定时任务（Cron）与手动触发
* 重试（attempt 计数）、并发控制、misfire 与历史保留策略
* 取消运行
* DAG 工作流，task 依赖与条件放行
* Serverless 函数，HTTP 调用
* 多租户隔离：API key grant、PostgreSQL role + RLS、同事务审计
* 巡检（Check）产出 SLI，绑定 SLO 后按错误预算与燃烧率告警
* Prometheus metrics、Grafana dashboard、结构化日志、trace ID
* Leader election（Kubernetes Lease，scheduler 可选 etcd）

## 文档

- [USAGE.md](./USAGE.md) - 部署、API 示例、运维、排障
- [CONTRIBUTING.md](./CONTRIBUTING.md) - 分支、依赖、测试、migration、commit、PR
- [SECURITY.md](./SECURITY.md) - 安全模型、漏洞报告、PostgreSQL role 与 RLS、容器安全
- [docs/database-setup.md](docs/database-setup.md) - PostgreSQL 配置、TLS、Kubernetes Secret 契约
- [docs/local-deployment.md](docs/local-deployment.md) - 完整 kind 本地开发指南
- [docs/architecture.md](docs/architecture.md) - 组件、进程与数据流向
- [api/openapi.yaml](./api/openapi.yaml) - API schema

## License

[BSD 3-Clause](./LICENSE)

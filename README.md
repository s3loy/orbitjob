# OrbitJob

[![License](https://img.shields.io/github/license/s3loy/orbitjob)](./LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/s3loy/orbitjob)](https://goreportcard.com/report/github.com/s3loy/orbitjob)
[![Build Status](https://github.com/s3loy/orbitjob/actions/workflows/ci.yml/badge.svg)](https://github.com/s3loy/orbitjob/actions/workflows/ci.yml)
[![Coverage Status](https://codecov.io/gh/s3loy/orbitjob/graph/badge.svg)](https://codecov.io/gh/s3loy/orbitjob)

[English](./README.en.md)

OrbitJob 是一个基于 Go 和 PostgreSQL 的作业调度系统。当前仓库实现的是 job definition 的 control plane，包括创建、查询、更新、暂停、恢复，以及对应的领域校验、状态迁移、审计记录和持久化。

## 项目状态

| 领域 | 状态 | 说明 |
| --- | --- | --- |
| Control plane HTTP API | 已实现 | `create / list / get / update / pause / resume` |
| Job 领域校验 | 已实现 | trigger、status、retry、concurrency、misfire 等规则位于 `internal/core/domain/job` |
| Write-side persistence | 已实现 | PostgreSQL + optimistic locking + audit |
| Read-side query | 已实现 | 列表与详情查询位于 `internal/admin/store/postgres` |
| Scheduler runtime | 未完成 | 本仓库当前不是完整的执行平面实现 |
| Worker execution | 未完成 | worker / dispatch / leasing 仍在后续范围 |

## 架构

```mermaid
flowchart LR
    Client["Client / Automation"] --> Router["Gin Router<br/>/healthz<br/>/metrics<br/>/api/v1/jobs"]

    subgraph AdminAPI["cmd/admin-api + internal/admin/http"]
        Router --> QueryUC["Query use cases<br/>list / get"]
        Router --> CommandUC["Command use cases<br/>create / update / pause / resume"]
    end

    subgraph Storage["Persistence"]
        ReadRepo["admin/store/postgres<br/>read model"] --> DB[("PostgreSQL")]
        WriteRepo["core/store/postgres<br/>write model + audit"] --> DB
    end

    QueryUC --> ReadRepo
    CommandUC --> Domain["core/domain/job<br/>validation + status transition"]
    CommandUC --> WriteRepo
```

Job 生命周期与状态流转见 [docs/job-lifecycle.md](./docs/job-lifecycle.md)。

## HTTP API

### 路由

| Method | Path | 功能 | 输入 | 备注 |
| --- | --- | --- | --- | --- |
| `GET` | `/healthz` | 健康检查 | 无 | 返回服务存活状态 |
| `GET` | `/metrics` | Prometheus 指标 | 无 | 暴露 metrics handler |
| `POST` | `/api/v1/jobs` | 创建 job | JSON body | 创建型接口 |
| `GET` | `/api/v1/jobs` | 查询 job 列表 | Query: `tenant_id`, `status`, `limit`, `offset` | `status` 仅支持 `active` / `paused` |
| `GET` | `/api/v1/jobs/:id` | 查询 job 详情 | Path: `id`; Query: `tenant_id` | `id >= 1` |
| `PUT` | `/api/v1/jobs/:id` | 更新 job 配置 | Path: `id`; Query: `tenant_id`; JSON body | merge-style update；需要 `X-Actor-ID` |
| `POST` | `/api/v1/jobs/:id/pause` | 暂停 job | Path: `id`; Query: `tenant_id`; JSON body: `version` | 需要 `X-Actor-ID` |
| `POST` | `/api/v1/jobs/:id/resume` | 恢复 job | Path: `id`; Query: `tenant_id`; JSON body: `version` | 需要 `X-Actor-ID` |

### 修改型请求约定

| 项目 | 说明 |
| --- | --- |
| `X-Actor-ID` | 修改型接口必填；写入审计记录 |
| `X-Trace-ID` | 可选；未提供时服务端自动生成并在响应头回写 |
| `version` | 更新、暂停、恢复接口必填；用于 optimistic locking |
| Error mapping | 校验错误返回 `400`；不存在返回 `404`；版本冲突返回 `409`；其他错误返回 `500` |

### 更新语义

`PUT /api/v1/jobs/:id` 当前实现为 merge-style update：

| 规则 | 说明 |
| --- | --- |
| 未提供字段 | 保留当前 job 的现值 |
| 已提供字段 | 覆盖当前 job 的现值 |
| `cron -> manual` | 若切换为 `manual` 且未显式提供 `cron_expr`，系统清空已有 cron 表达式 |
| 持久化写入 | 使用 `jobs.version` 做 optimistic locking |

### 核心字段约定

| 字段 | 取值 |
| --- | --- |
| `trigger_type` | `cron` / `manual` |
| `status` | `active` / `paused` |
| `retry_backoff_strategy` | `fixed` / `exponential` |
| `concurrency_policy` | `allow` / `forbid` / `replace` |
| `misfire_policy` | `skip` / `fire_now` / `catch_up` |

## 开发

### 依赖

- Go
- PostgreSQL

### 环境变量

`.env.example` 当前包含：

```bash
DATABASE_DSN=postgres://postgres:password@127.0.0.1:5432/orbitjob?sslmode=disable
TEST_DATABASE_DSN=postgres://postgres:password@127.0.0.1:5432/orbitjob_test?sslmode=disable
```

常用变量：

| 变量 | 用途 |
| --- | --- |
| `DATABASE_DSN` | `cmd/admin-api` 使用的数据库连接串 |
| `TEST_DATABASE_DSN` | integration tests 使用的测试数据库连接串 |
| `APP_ENV` | 日志与运行环境标识 |

### 运行

```bash
go run ./cmd/admin-api
```

### 测试

单元测试：

```bash
go test ./...
```

integration tests：

```bash
go test -tags integration ./internal/platform/postgrestest
go test -tags integration ./internal/admin/store/postgres ./internal/core/store/postgres
```

## 仓库结构

| 路径 | 说明 |
| --- | --- |
| `cmd/admin-api` | 服务入口、middleware、router wiring |
| `internal/admin/http` | HTTP handler、request binding、error mapping |
| `internal/admin/app/job` | control-plane query / command use cases |
| `internal/admin/store/postgres` | read-side PostgreSQL repository |
| `internal/core/domain/job` | job 领域模型、校验、状态迁移 |
| `internal/core/store/postgres` | write-side PostgreSQL repository |
| `internal/domain` | 通用校验错误、资源错误 |
| `internal/platform` | config、logger、metrics、test helpers |
| `db/migrations` | PostgreSQL schema、约束、trigger |

## 文档

| 路径 | 说明 |
| --- | --- |
| [`README.md`](./README.md) | 中文总览与开发参考 |
| [`README.en.md`](./README.en.md) | English overview |
| [`docs/job-lifecycle.md`](./docs/job-lifecycle.md) | Job 状态流转与接口约束 |

## License

See [LICENSE](./LICENSE).

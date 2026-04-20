# OrbitJob

[![License](https://img.shields.io/github/license/s3loy/orbitjob)](./LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/s3loy/orbitjob)](https://goreportcard.com/report/github.com/s3loy/orbitjob)
[![Build Status](https://github.com/s3loy/orbitjob/actions/workflows/ci.yml/badge.svg)](https://github.com/s3loy/orbitjob/actions/workflows/ci.yml)
[![Coverage Status](https://codecov.io/gh/s3loy/orbitjob/graph/badge.svg)](https://codecov.io/gh/s3loy/orbitjob)

[English](./README.en.md)

OrbitJob 是一个基于 Go 和 PostgreSQL 构建的作业调度系统，采用 context-first 的模块化单体架构。当前实现涵盖 control plane（job 定义的完整 CRUD 与生命周期管理）、scheduler（批量调度与 misfire 策略）以及 dispatcher（原子 claim、并发策略、优先级老化与 lease 恢复）。

## 项目状态

| 领域 | 状态 | 说明 |
| --- | --- | --- |
| Control plane HTTP API | 已实现 | `create / list / get / update / pause / resume` |
| Job 领域校验 | 已实现 | trigger、status、retry、concurrency、misfire 规则，位于 `internal/core/domain/job` |
| Write-side persistence | 已实现 | PostgreSQL + optimistic locking + audit |
| Read-side query | 已实现 | 列表与详情查询，位于 `internal/admin/store/postgres` |
| Execution foundation domain | 已实现 | `internal/core/domain/instance` 与 `internal/core/domain/worker` |
| Execution foundation persistence | 已实现 | instance create/claim + worker heartbeat upsert |
| Scheduler runtime | 已实现 | `cmd/scheduler` + bounded batch tick + misfire policy + 原子调度事务 |
| Dispatcher runtime | 已实现 | `cmd/dispatcher` + 原子 claim（`FOR UPDATE SKIP LOCKED`）+ concurrency policy + priority aging + lease expiry recovery + graceful shutdown |
| Worker executor | 未实现 | worker 执行器与结果回写闭环 |
| Manual trigger API | 未实现 | 手动触发接口 |
| Instance query API | 未实现 | instance 查询接口 |

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

    Scheduler["cmd/scheduler"] --> ScheduleUC["core/app/schedule<br/>policy + tick"]
    ScheduleUC --> SchedulerRepo["core/store/postgres<br/>SchedulerRepository"]
    SchedulerRepo --> DB

    Dispatcher["cmd/dispatcher"] --> DispatchUC["core/app/dispatch<br/>tick use case"]
    DispatchUC --> DispatchRepo["core/store/postgres<br/>DispatchRepository"]
    DispatchRepo --> DB
```

Job 生命周期与状态流转见 [docs/job-lifecycle.md](./docs/job-lifecycle.md)。

Execution plane 的契约与边界见 [docs/execution-plane.md](./docs/execution-plane.md)。

## HTTP API

### 路由

| Method | Path | 功能 | 输入 | 备注 |
| --- | --- | --- | --- | --- |
| `GET` | `/healthz` | 健康检查 | 无 | 返回服务存活状态 |
| `GET` | `/openapi.json` | OpenAPI 文档 | 无 | 代码生成的实时 API 契约 |
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

- Go 1.26+
- PostgreSQL

### 进程

OrbitJob 由多个独立进程组成，各自承担不同职责：

| 进程 | 入口 | 说明 |
| --- | --- | --- |
| Admin API | `cmd/admin-api` | 控制面 HTTP 服务，提供 job 的 CRUD 与生命周期管理 |
| Scheduler | `cmd/scheduler` | 调度器，定时扫描到期 job 并生成 instance（batch tick loop） |
| Dispatcher | `cmd/dispatcher` | 派发器，原子 claim pending instance 并分配至 worker（`FOR UPDATE SKIP LOCKED`） |
| OpenAPI Gen | `cmd/openapi-gen` | OpenAPI 契约生成与漂移检查工具 |

### 环境变量

`.env.example` 当前包含：

```bash
DATABASE_DSN=postgres://postgres:password@127.0.0.1:5432/orbitjob?sslmode=disable
TEST_DATABASE_DSN=postgres://postgres:password@127.0.0.1:5432/orbitjob_test?sslmode=disable
```

通用变量：

| 变量 | 用途 | 默认值 |
| --- | --- | --- |
| `DATABASE_DSN` | `cmd/admin-api` 使用的数据库连接串 | -- |
| `TEST_DATABASE_DSN` | integration tests 使用的测试数据库连接串 | -- |
| `APP_ENV` | 日志与运行环境标识 | -- |

Scheduler 变量：

| 变量 | 用途 | 默认值 |
| --- | --- | --- |
| `SCHEDULER_BATCH_SIZE` | 每个 tick 最大处理 job 数 | `100` |
| `SCHEDULER_TICK_INTERVAL_SEC` | tick 周期秒数 | `5` |

Dispatcher 变量：

| 变量 | 用途 | 默认值 |
| --- | --- | --- |
| `DISPATCHER_WORKER_ID` | 派发目标 worker 标识（必填） | -- |
| `DISPATCHER_TENANT_ID` | 租户标识 | `default` |
| `DISPATCHER_BATCH_SIZE` | 每次 tick 最大 claim 数 | `50` |
| `DISPATCHER_TICK_INTERVAL_SEC` | tick 周期秒数 | `2` |
| `DISPATCHER_LEASE_DURATION_SEC` | lease 有效期秒数 | `30` |

### 运行

启动 Admin API：

```bash
go run ./cmd/admin-api
```

启动 Scheduler：

```bash
go run ./cmd/scheduler
```

启动 Dispatcher：

```bash
DISPATCHER_WORKER_ID=worker-1 go run ./cmd/dispatcher
```

### OpenAPI（Code-first）

仓库内提交的契约文件：`api/openapi.yaml`

重新生成：

```bash
go run ./cmd/openapi-gen -out api/openapi.yaml
```

校验是否漂移（CI 同款命令）：

```bash
go run ./cmd/openapi-gen -check -out api/openapi.yaml
```

### 测试

单元测试：

```bash
go test ./...
```

Integration tests：

```bash
go test -tags integration ./internal/platform/postgrestest
go test -tags integration ./internal/admin/store/postgres ./internal/core/store/postgres
```

## 仓库结构

| 路径 | 说明 |
| --- | --- |
| `cmd/admin-api` | 控制面服务入口、middleware、router wiring |
| `cmd/scheduler` | scheduler 入口（bounded batch tick + misfire policy） |
| `cmd/dispatcher` | dispatcher 入口（claim + dispatch + graceful shutdown） |
| `cmd/openapi-gen` | OpenAPI code-first 生成与漂移检查工具 |
| `internal/admin/http` | HTTP handler、request binding、error mapping |
| `internal/admin/app/job` | control-plane query / command use cases |
| `internal/admin/store/postgres` | read-side PostgreSQL repository |
| `internal/core/domain` | job / instance / worker 领域模型与校验 |
| `internal/core/app/schedule` | scheduler tick use case（policy + tick） |
| `internal/core/app/dispatch` | dispatcher tick use case |
| `internal/core/store/postgres` | write-side repositories（job / instance / worker / scheduler / dispatch） |
| `internal/domain` | 通用校验错误、资源错误 |
| `internal/platform` | config、logger、metrics、test helpers |
| `db/migrations` | PostgreSQL schema、约束、trigger |

## 文档

| 路径 | 说明 |
| --- | --- |
| [`README.md`](./README.md) | 中文总览与开发参考 |
| [`README.en.md`](./README.en.md) | English overview |
| [`docs/job-lifecycle.md`](./docs/job-lifecycle.md) | Job 状态流转与接口约束 |
| [`docs/job-lifecycle.en.md`](./docs/job-lifecycle.en.md) | Job lifecycle and endpoint rules |
| [`docs/execution-plane.md`](./docs/execution-plane.md) | Execution plane 契约与基础语义 |
| [`docs/execution-plane.en.md`](./docs/execution-plane.en.md) | Execution-plane contract and foundation semantics |

## License

See [LICENSE](./LICENSE).

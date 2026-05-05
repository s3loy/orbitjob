# OrbitJob

[![License](https://img.shields.io/github/license/s3loy/orbitjob)](./LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/s3loy/orbitjob)](https://goreportcard.com/report/github.com/s3loy/orbitjob)
[![Build Status](https://github.com/s3loy/orbitjob/actions/workflows/ci.yml/badge.svg)](https://github.com/s3loy/orbitjob/actions/workflows/ci.yml)
[![Coverage Status](https://codecov.io/gh/s3loy/orbitjob/graph/badge.svg)](https://codecov.io/gh/s3loy/orbitjob)

[English](./README.en.md)

Go 语言的后台任务调度系统
只需 PostgreSQL

可以作为库 import 进自己的 Go 应用，也可以作为独立服务部署

## 快速开始

```bash
# 1. 启动 PostgreSQL，然后启动 admin-api
DATABASE_DSN="postgres://postgres:password@127.0.0.1:5432/orbitjob?sslmode=disable" \
  go run ./cmd/admin-api

# 2. 创建第一个 job
curl -X POST http://localhost:8080/api/v1/jobs \
  -H "Content-Type: application/json" \
  -d '{"name":"hello-world","trigger_type":"manual"}'

# 3. 启动后台调度
go run ./cmd/scheduler &
DISPATCHER_WORKER_ID=worker-1 go run ./cmd/dispatcher &
WORKER_ID=worker-1 go run ./cmd/worker &
```

## 简述

OrbitJob 负责单个任务的可靠调度——cron 触发、分发、执行、重试、结果回写

- **scheduler** — 按 cron 表达式生成执行实例
- **dispatcher** — 推进实例状态，回收孤儿任务，检查并发策略
- **worker** — 自主认领并执行任务（HTTP callback 或子进程）
- **admin-api** — REST API 管理 job 和查看执行结果

四个组件之间没有直接的网络调用，全部通过 PostgreSQL 协调

## API

RESTful HTTP，OpenAPI 规范自动生成：

| Method | Path | 说明 |
| --- | --- | --- |
| `POST` | `/api/v1/jobs` | 创建 job |
| `GET` | `/api/v1/jobs` | 列表查询 |
| `GET` | `/api/v1/jobs/:id` | 详情查询 |
| `PUT` | `/api/v1/jobs/:id` | 更新 job |
| `POST` | `/api/v1/jobs/:id/pause` | 暂停 |
| `POST` | `/api/v1/jobs/:id/resume` | 恢复 |

修改型接口需 `X-Actor-ID` header。更新/暂停/恢复需 `version` 做乐观锁。完整契约见 [`api/openapi.yaml`](./api/openapi.yaml) 或运行中服务的 `/openapi.json`

## 开发

### 依赖

Go 1.26+、PostgreSQL

### 运行

```bash
# Admin API
go run ./cmd/admin-api

# Scheduler
go run ./cmd/scheduler

# Dispatcher
go run ./cmd/dispatcher

# Worker
WORKER_ID=worker-1 go run ./cmd/worker
```

### 环境变量

| 变量 | 用途 | 默认值 |
| --- | --- | --- |
| `DATABASE_DSN` | 数据库连接串 | -- |
| `TEST_DATABASE_DSN` | 集成测试数据库连接串 | -- |
| `APP_ENV` | 日志环境（development/production） | -- |
| `SCHEDULER_BATCH_SIZE` | 每 tick 最大处理 job 数 | `100` |
| `SCHEDULER_TICK_INTERVAL_SEC` | tick 间隔秒数 | `5` |
| `DISPATCHER_BATCH_SIZE` | 每 tick 最大 claim 数 | `50` |
| `DISPATCHER_TICK_INTERVAL_SEC` | tick 间隔秒数 | `2` |
| `DISPATCHER_LEASE_DURATION_SEC` | lease 有效期秒数 | `30` |
| `WORKER_ID` | Worker 唯一标识 | -- |
| `WORKER_TENANT_ID` | Worker 所属租户 | `default` |
| `WORKER_DSN` | Worker 数据库连接串（优先于 DATABASE_DSN） | -- |
| `WORKER_POLL_INTERVAL_SEC` | poll 间隔秒数 | `2` |
| `WORKER_HEARTBEAT_INTERVAL_SEC` | 心跳间隔秒数 | `10` |
| `WORKER_LEASE_DURATION_SEC` | Worker lease 有效期秒数 | `60` |
| `WORKER_CAPACITY` | 最大并发执行数 | `1` |
| `WORKER_LABELS` | Worker 标签（JSON 对象） | `{}` |

### 测试

```bash
go test ./...                                                        # 单元测试
go test -tags integration ./internal/platform/postgrestest            # 集成测试
go test -tags integration ./internal/admin/store/postgres ./internal/core/store/postgres
golangci-lint run                                                    # Lint
go run ./cmd/openapi-gen -check -out api/openapi.yaml                # OpenAPI 漂移检查
```

## 仓库结构

| 路径 | 说明 |
| --- | --- |
| `cmd/admin-api` | 控制面 HTTP 服务 |
| `cmd/scheduler` | 调度器 |
| `cmd/dispatcher` | 派发器 |
| `cmd/worker` | 执行器 |
| `cmd/openapi-gen` | OpenAPI 生成与漂移检查 |
| `internal/admin/` | 控制面 HTTP / 应用层 / 读模型 |
| `internal/core/` | 领域规则 / 用例层 / 写侧持久化 |
| `internal/platform/` | 基础设施（config/logger/metrics/test） |
| `db/migrations/` | PostgreSQL schema |

## 项目状态

| 领域 | 状态 |
| --- | --- |
| Control plane HTTP API | ✅ |
| Scheduler + Dispatcher runtime | ✅ |
| Worker executor | ✅ |
| 多租户 RLS 安全模型 | ✅ |
| Benchmark 基线（4 层） | ✅ |
| Worker 并发执行 + 优雅关闭 | 🔧 实现中 |
| Instance query / Manual trigger API | 🔧 待实现 |

## 文档

| 文档 | 说明 |
| --- | --- |
| [`docs/job-lifecycle.md`](./docs/job-lifecycle.md) | Job 状态机契约 |
| [`docs/execution-plane.md`](./docs/execution-plane.md) | 执行面契约 |
| [`./CONTRIBUTING.md`](./CONTRIBUTING.md) | 贡献指南 |
| [`./SECURITY.md`](./SECURITY.md) | 安全漏洞报告 |

## License

[BSD 3-Clause](./LICENSE)

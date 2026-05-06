# OrbitJob

[![License](https://img.shields.io/github/license/s3loy/orbitjob)](./LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/s3loy/orbitjob)](https://goreportcard.com/report/github.com/s3loy/orbitjob)
[![Build Status](https://github.com/s3loy/orbitjob/actions/workflows/ci.yml/badge.svg)](https://github.com/s3loy/orbitjob/actions/workflows/ci.yml)
[![Coverage Status](https://codecov.io/gh/s3loy/orbitjob/graph/badge.svg)](https://codecov.io/gh/s3loy/orbitjob)

[English](./README.en.md)

![Stone Badge](https://stone.professorlee.work/api/stone/s3loy/orbitjob)

Go 任务调度库。PostgreSQL 唯一外部依赖

可作为 library 嵌入 Go 应用，也可独立部署

## 快速开始

目前暂处于手动开启psql阶段

```bash
# 1. 启动 PostgreSQL，启动 API 服务
DATABASE_DSN="postgres://user:pass@localhost:5432/orbitjob?sslmode=disable" \
  go run ./cmd/admin-api

# 2. 创建 job
curl -X POST http://localhost:8080/api/v1/jobs \
  -H "Content-Type: application/json" \
  -H "X-Actor-ID: admin" \
  -d '{"name":"hello-world","trigger_type":"manual"}'

# 3. 启动后台组件（WORKER_ID 可选，留空自动生成）
go run ./cmd/scheduler &
go run ./cmd/dispatcher &
go run ./cmd/worker &
```

开发模式（单进程运行全部组件）：

```bash
go run ./cmd/devserver
```

## 开发

### 环境要求

Go 1.26+、PostgreSQL 17

### 启动

```bash
go run ./cmd/admin-api       # API 服务
go run ./cmd/scheduler       # 调度器
go run ./cmd/dispatcher      # 分发器
go run ./cmd/worker          # 执行器（WORKER_ID 可选，留空自动生成）
go run ./cmd/devserver       # 开发模式（单进程全部组件）
go run ./cmd/openapi-gen     # OpenAPI 生成
```

### 环境变量

| 变量 | 用途 | 默认值 |
| --- | --- | --- |
| `DATABASE_DSN` | 数据库连接串 | — |
| `ADMIN_DSN` | Admin API 专用连接串（优先于 DATABASE_DSN） | — |
| `SCHEDULER_DSN` | Scheduler 专用连接串 | — |
| `DISPATCHER_DSN` | Dispatcher 专用连接串 | — |
| `WORKER_DSN` | Worker 专用连接串 | — |
| `DEV_DSN` | Devserver 专用连接串 | — |
| `TEST_DATABASE_DSN` | 集成测试连接串 | — |
| `APP_ENV` | 日志模式（development / production） | — |
| `ADMIN_PORT` | API 监听端口 | `8080` |
| `SCHEDULER_BATCH_SIZE` | 每 tick 最大 job 数 | `100` |
| `SCHEDULER_TICK_INTERVAL_SEC` | Tick 间隔（秒） | `5` |
| `DISPATCHER_TENANT_ID` | Dispatcher 租户范围 | `default` |
| `DISPATCHER_BATCH_SIZE` | 每 tick 最大 claim 数 | `50` |
| `DISPATCHER_TICK_INTERVAL_SEC` | Tick 间隔（秒） | `2` |
| `DISPATCHER_LEASE_DURATION_SEC` | Lease 有效期（秒） | `30` |
| `WORKER_ID` | Worker 标识 | {hostname}-{uuid8} |
| `WORKER_TENANT_ID` | Worker 租户范围 | `default` |
| `WORKER_POLL_INTERVAL_SEC` | Poll 间隔（秒） | `2` |
| `WORKER_HEARTBEAT_INTERVAL_SEC` | 心跳间隔（秒） | `10` |
| `WORKER_LEASE_DURATION_SEC` | Lease 有效期（秒） | `60` |
| `WORKER_CAPACITY` | 最大并发执行数 | `1` |
| `WORKER_LABELS` | Worker 标签（JSON） | `{}` |

### 测试

```bash
go test ./...                                                    # 单元测试
go test -tags integration ./internal/platform/postgrestest        # 集成测试
go test -tags integration ./internal/admin/store/postgres ./internal/core/store/postgres
golangci-lint run                                                # Lint
go run ./cmd/openapi-gen -check -out api/openapi.yaml            # OpenAPI 漂移
```

## License

[BSD 3-Clause](./LICENSE)

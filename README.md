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

### Docker Compose

```bash
docker compose up -d
curl http://localhost:8080/healthz
```

### 源码启动

```bash
# 1. 启动 PostgreSQL，启动 API 服务
DATABASE_DSN="postgres://user:<YOUR_PASSWORD>@localhost:5432/orbitjob?sslmode=disable" \
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

### 生产部署

参见 `deploy/` 目录，含 systemd unit、环境变量模板和部署脚本。

```bash
make build-all
DATABASE_URL="postgres://..." make migrate-up
sudo cp deploy/systemd/*.service /etc/systemd/system/
sudo systemctl enable --now orbitjob-*
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
| `SCHEDULER_HEALTH_PORT` | 健康检查端口 | `6060` |
| `SCHEDULER_BATCH_SIZE` | 每 tick 最大 job 数 | `100` |
| `SCHEDULER_TICK_INTERVAL_SEC` | Tick 间隔（秒） | `5` |
| `DISPATCHER_TENANT_ID` | Dispatcher 租户范围 | `default` |
| `DISPATCHER_HEALTH_PORT` | 健康检查端口 | `6061` |
| `DISPATCHER_BATCH_SIZE` | 每 tick 最大 claim 数 | `50` |
| `DISPATCHER_TICK_INTERVAL_SEC` | Tick 间隔（秒） | `2` |
| `DISPATCHER_LEASE_DURATION_SEC` | Lease 有效期（秒） | `30` |
| `WORKER_ID` | Worker 标识 | {hostname}-{uuid8} |
| `WORKER_TENANT_ID` | Worker 租户范围 | `default` |
| `WORKER_HEALTH_PORT` | 健康检查端口 | `6062` |
| `WORKER_POLL_INTERVAL_SEC` | Poll 间隔（秒） | `2` |
| `WORKER_HEARTBEAT_INTERVAL_SEC` | 心跳间隔（秒） | `10` |
| `WORKER_LEASE_DURATION_SEC` | Lease 有效期（秒） | `60` |
| `WORKER_CAPACITY` | 最大并发执行数 | `1` |
| `WORKER_LABELS` | Worker 标签（JSON） | `{}` |

### 测试

```bash
make test                  # 单元测试
make test-cover            # 覆盖率报告
make test-race             # 竞态检测
make integration           # 集成测试（需要 PostgreSQL）
make lint                  # golangci-lint
make check                 # 全量质量门禁（lint + vet + race + openapi + tidy）
```

## License

[BSD 3-Clause](./LICENSE)

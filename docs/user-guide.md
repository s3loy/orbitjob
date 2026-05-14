# OrbitJob 使用指南

[返回 README](../README.md)

本文档面向**使用 OrbitJob 作为调度系统的开发者/运维人员**，涵盖从安装到生产部署的完整流程。

---

## 目录

- [什么是 OrbitJob](#什么是-orbitjob)
- [架构概览](#架构概览)
- [快速开始](#快速开始)
- [创建 Job](#创建-job)
- [部署组件](#部署组件)
- [监控与可观测性](#监控与可观测性)
- [环境变量参考](#环境变量参考)
- [故障排查](#故障排查)

---

## 什么是 OrbitJob

OrbitJob = Go 原生任务调度库 + PostgreSQL 状态存储 + 可选 etcd 分布式协调层。

设计哲学：**像 `database/sql` 定义 Go 如何访问数据库一样，OrbitJob 定义 Go 如何调度任务**。

核心特性：

- **最小外部依赖**：PostgreSQL 是必需的状态存储；etcd 是可选的分布式协调层（用于多实例 HA）
- **多租户**：RLS（Row Level Security）隔离租户数据
- **自适应调度**：Scheduler 根据 DB 压力自动调整 batch 大小；Worker 根据队列深度自动调整并发
- **Exactly-Once 语义**：基于 PostgreSQL `FOR UPDATE SKIP LOCKED` 的分布式 claim
- **内置可观测性**：Prometheus metrics + structured logging + trace ID 全链路传播

---

## 架构概览

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│  Admin API  │────▶│  Scheduler  │────▶│ Dispatcher  │────▶│   Worker    │
│  (Control)  │     │  (Schedule) │     │  (Dispatch) │     │  (Execute)  │
└─────────────┘     └──────┬──────┘     └──────┬──────┘     └──────┬──────┘
                           │                    │                   │
                           └────────────────────┴───────────────────┘
                                               │
                                        ┌──────┴──────┐
                                        │ PostgreSQL  │
                                        │ (状态存储)   │
                                        └─────────────┘
```

| 组件 | 职责 | 部署方式 |
|------|------|---------|
| **Admin API** | HTTP 控制面：创建/暂停/删除 job，查询 instance | 必需，1+ 实例 |
| **Scheduler** | 根据 cron 表达式生成 job instance | 必需，建议 1 实例（多实例需防重复） |
| **Dispatcher** | 将 pending instance claim 并分配给 worker | 必需，1+ 实例 |
| **Worker** | 执行 handler（HTTP 调用、命令行等） | 必需，1+ 实例 |

**数据流**：

1. 用户通过 Admin API 创建 job definition
2. Scheduler 扫描到 due job，创建 `pending` 状态的 instance
3. Dispatcher claim pending instance，将其变为 `dispatched` 并分配 lease
4. Worker claim dispatched instance，执行 handler，标记为 `success` / `failed` / `retry_wait`

---

## 快速开始

### 前置要求

- Go 1.26+
- PostgreSQL 17

### 1. 克隆并编译

```bash
git clone https://github.com/s3loy/orbitjob.git
cd orbitjob
make build-all
```

### 2. 准备数据库

```bash
# 创建数据库
createdb orbitjob

# 执行迁移
DATABASE_DSN="postgres://user:password@localhost:5432/orbitjob?sslmode=disable" make migrate-up
```

### 3. 一键启动（开发模式）

```bash
DATABASE_DSN="postgres://user:password@localhost:5432/orbitjob?sslmode=disable" go run ./cmd/devserver
```

Devserver 在单进程中运行所有组件，API 监听 `:8080`。

### 4. 验证安装

```bash
curl http://localhost:8080/healthz
curl http://localhost:8080/readyz
curl http://localhost:8080/metrics
```

---

## 创建 Job

### Cron Job（周期性任务）

```bash
curl -X POST http://localhost:8080/api/v1/jobs \
  -H "Content-Type: application/json" \
  -H "X-Actor-ID: admin" \
  -d '{
    "name": "hourly-report",
    "trigger_type": "cron",
    "cron_expr": "0 * * * *",
    "timezone": "Asia/Shanghai",
    "handler_type": "http",
    "handler_payload": {
      "url": "http://internal-api/reports/hourly",
      "method": "POST",
      "headers": {"Authorization": "Bearer token"}
    },
    "timeout_sec": 300,
    "retry_limit": 3,
    "retry_backoff_sec": 60,
    "retry_backoff_strategy": "exponential"
  }'
```

### Manual Job（手动触发）

```bash
# 创建 job
curl -X POST http://localhost:8080/api/v1/jobs \
  -H "Content-Type: application/json" \
  -H "X-Actor-ID: admin" \
  -d '{
    "name": "one-off-task",
    "trigger_type": "manual",
    "handler_type": "http",
    "handler_payload": {"url": "http://internal-api/tasks/cleanup"}
  }'

# 触发（返回 run_id）
curl -X POST http://localhost:8080/api/v1/jobs/1/trigger \
  -H "X-OrbitJob-Idempotency-Key: trigger-2026-05-14-001"
```

### 查询 Instance 状态

```bash
# 列表（支持分页、按状态过滤）
curl "http://localhost:8080/api/v1/instances?status=pending&limit=20"

# 详情
curl http://localhost:8080/api/v1/instances/run-abc-123

# 取消
curl -X POST http://localhost:8080/api/v1/instances/run-abc-123/cancel
```

---

## 部署组件

### 独立部署（推荐生产环境）

每个组件独立进程，通过环境变量配置。

#### Scheduler

```bash
export SCHEDULER_DSN="postgres://..."
export SCHEDULER_BATCH_SIZE_MAX=500
export SCHEDULER_TICK_INTERVAL_SEC=5
export SCHEDULER_HEALTH_PORT=6060
./bin/scheduler
```

Scheduler 暴露端口：
- `:6060/healthz` — 存活检查
- `:6060/readyz` — 就绪检查（含 DB ping）
- `:6060/metrics` — Prometheus 指标

#### Dispatcher

```bash
export DISPATCHER_DSN="postgres://..."
export DISPATCHER_TENANT_ID=default
export DISPATCHER_BATCH_SIZE=50
export DISPATCHER_TICK_INTERVAL_SEC=2
export DISPATCHER_LEASE_DURATION_SEC=30
export DISPATCHER_HEALTH_PORT=6061
./bin/dispatcher
```

#### Worker

```bash
export WORKER_DSN="postgres://..."
export WORKER_TENANT_ID=default
export WORKER_CAPACITY=1           # 静态并发上限
export WORKER_CAPACITY_MAX=10      # 自适应并发上限（需要 adaptive capacity）
export WORKER_POLL_INTERVAL_SEC=2
export WORKER_HEARTBEAT_INTERVAL_SEC=10
export WORKER_LEASE_DURATION_SEC=60
export WORKER_HEALTH_PORT=6062
./bin/worker
```

Worker 支持**标签路由**：

```bash
export WORKER_LABELS='{"zone":"us-east-1","gpu":"a100"}'
```

只有 `routing_key` 匹配 worker 标签的 job instance 才会被该 worker claim。

### Docker Compose

```bash
docker compose up -d
```

Compose 文件包含：PostgreSQL、admin-api、scheduler、dispatcher、worker、Prometheus、Grafana。

### systemd

```bash
make build-all
sudo cp deploy/systemd/*.service /etc/systemd/system/
sudo systemctl enable --now orbitjob-scheduler orbitjob-dispatcher orbitjob-worker
```

---

## 监控与可观测性

### Prometheus 指标

所有组件在 `/metrics` 暴露 Prometheus 指标。

#### Scheduler 指标

| 指标名 | 类型 | 说明 |
|--------|------|------|
| `orbitjob_scheduler_tick_duration_seconds` | Histogram | 每 tick 处理时长 |
| `orbitjob_scheduler_instances_created_total` | Counter | 创建的 instance 总数 |
| `orbitjob_scheduler_limit` | Gauge | 当前 adaptive batch limit |
| `orbitjob_scheduler_phase` | Gauge | 当前阶段：0=Discovery, 1=Steady, 2=Protect, 3=HalfOpen |
| `orbitjob_scheduler_probe_rtt_seconds` | Histogram | DB SELECT 1 探针 RTT |
| `orbitjob_scheduler_db_pressure` | Gauge | Vegas 估算的 DB 压力 |
| `orbitjob_scheduler_breaker_transitions_total` | Counter | Circuit breaker 状态切换次数 |
| `orbitjob_scheduler_queue_depth` | Gauge | 活跃 instance 总数（反压信号） |
| `orbitjob_scheduler_idle_ticks_total` | Counter | 空 tick 计数 |
| `orbitjob_scheduler_interval_mode` | Gauge | 0=short interval, 1=long interval |
| `orbitjob_scheduler_discovery_iterations_total` | Counter | Discovery 阶段迭代次数 |
| `orbitjob_scheduler_discovery_duration_seconds` | Histogram | Discovery 阶段耗时 |

#### Worker 指标

| 指标名 | 类型 | 说明 |
|--------|------|------|
| `orbitjob_worker_capacity` | Gauge | 当前 adaptive capacity |
| `orbitjob_worker_lease_duration_seconds` | Gauge | 当前动态 lease 时长 |
| `orbitjob_worker_probe_rtt_seconds` | Histogram | DB 探针 RTT |
| `orbitjob_worker_queue_depth` | Gauge | dispatched 状态任务数 |
| `orbitjob_worker_idle_ticks_total` | Counter | 空 tick 计数 |
| `orbitjob_worker_interval_mode` | Gauge | 0=short poll, 1=long poll |
| `orbitjob_executions_total` | Counter | 执行次数（按 handler_type、result_code） |
| `orbitjob_executions_active` | Gauge | 正在执行的任务数 |
| `orbitjob_lease_extension_failures_total` | Counter | lease 续约失败次数 |
| `orbitjob_worker_pool_active_tasks` | Gauge | pool 当前活跃任务数 |
| `orbitjob_worker_pool_submitted_total` | Counter | pool 累计提交任务数 |
| `orbitjob_worker_pool_rejected_total` | Counter | pool 满拒绝次数 |

#### Dispatcher 指标

| 指标名 | 类型 | 说明 |
|--------|------|------|
| `orbitjob_dispatch_actions_total` | Counter | dispatch 动作数（claim/skip/recover） |
| `orbitjob_orphan_recoveries_total` | Counter | 孤儿 instance 回收数 |

### 日志结构

所有组件使用结构化 JSON 日志（`APP_ENV=production` 时）：

```json
{
  "time": "2026-05-14T10:30:00Z",
  "level": "INFO",
  "msg": "instance scheduled",
  "trace_id": "550e8400-e29b-41d4-a716-446655440000",
  "run_id": "run-abc",
  "job_id": 42
}
```

Trace ID 从 Scheduler 生成，经 Dispatcher 传播到 Worker，贯穿完整调用链。

### 健康检查

| 端点 | 说明 |
|------|------|
| `/healthz` | 进程存活（总是返回 200） |
| `/readyz` | 就绪状态（含 DB ping，DB 不可用时返回 503） |

### 告警建议

| 告警规则 | 触发条件 | 含义 |
|----------|---------|------|
| Scheduler 进入 Protect | `scheduler_phase == 2` | DB 压力过大，调度降级 |
| Worker 容量持续为 1 | `worker_capacity == 1` 持续 5min | 队列堆积但 capacity 被压制 |
| Lease 续约失败 | `lease_extension_failures_total` 突增 | DB 写入压力大或网络问题 |
| 孤儿实例回收 | `orphan_recoveries_total` 突增 | Worker 失联或 lease 过期 |

---

## 环境变量参考

### 通用

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `DATABASE_DSN` | 数据库连接串 | — |
| `APP_ENV` | 运行模式：`development` / `production` | — |

### Scheduler

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `SCHEDULER_DSN` | 专用连接串（优先于 DATABASE_DSN） | — |
| `SCHEDULER_BATCH_SIZE_MAX` | 每 tick 最大处理 job 数 | `500` |
| `SCHEDULER_TICK_INTERVAL_SEC` | Tick 间隔（秒） | `5` |
| `SCHEDULER_HEALTH_PORT` | 健康检查端口 | `6060` |

### Dispatcher

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `DISPATCHER_DSN` | 专用连接串 | — |
| `DISPATCHER_TENANT_ID` | 租户范围 | `default` |
| `DISPATCHER_BATCH_SIZE` | 每 tick 最大 claim 数 | `50` |
| `DISPATCHER_TICK_INTERVAL_SEC` | Tick 间隔（秒） | `2` |
| `DISPATCHER_LEASE_DURATION_SEC` | Lease 有效期（秒） | `30` |
| `DISPATCHER_HEALTH_PORT` | 健康检查端口 | `6061` |

### Worker

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `WORKER_DSN` | 专用连接串 | — |
| `WORKER_ID` | Worker 唯一标识 | `{hostname}-{uuid8}` |
| `WORKER_TENANT_ID` | 租户范围 | `default` |
| `WORKER_CAPACITY` | 静态并发上限 | `1` |
| `WORKER_CAPACITY_MAX` | 自适应并发上限 | `10` |
| `WORKER_POLL_INTERVAL_SEC` | Poll 间隔（秒） | `2` |
| `WORKER_HEARTBEAT_INTERVAL_SEC` | 心跳间隔（秒） | `10` |
| `WORKER_LEASE_DURATION_SEC` | 静态 lease 时长（秒） | `60` |
| `WORKER_LEASE_MIN_SEC` | 动态 lease 下限（秒） | `10` |
| `WORKER_LEASE_DURATION_MAX` | 动态 lease 上限（秒） | `300` |
| `WORKER_LEASE_EMA_DECAY` | 动态 lease EMA 衰减系数 `(0,1]` | `0.1` |
| `WORKER_LABELS` | 标签 JSON，用于路由匹配 | `{}` |
| `WORKER_HEALTH_PORT` | 健康检查端口 | `6062` |

---

## 故障排查

### Scheduler 不调度 job

1. 检查 job 状态是否为 `active`
2. 检查 `next_run_at` 是否已到期：`SELECT id, next_run_at, status FROM jobs WHERE name = 'xxx'`
3. 检查 Scheduler phase：访问 `:6060/metrics`，看 `scheduler_phase` 是否为 `2`（Protect，DB 压力大时自动降级）
4. 检查 Scheduler 日志是否有 error

### Worker 不执行任务

1. 检查 instance 状态：`SELECT status, worker_id, lease_expires_at FROM job_instances WHERE job_id = xxx`
2. 确认 Worker 标签匹配：`routing_key IS NULL OR routing_key = ANY(worker_labels)`
3. 检查 Worker capacity：访问 `:6062/metrics`，看 `worker_capacity` 是否为 0
4. 检查 Worker 是否在线：`SELECT * FROM workers WHERE worker_id = 'xxx'`

### DB 连接池耗尽

1. 检查各组件的 DSN 是否独立（避免共享连接池）
2. 检查 PostgreSQL `max_connections` 设置
3. 检查是否有长时间未释放的 `FOR UPDATE` 锁：`SELECT * FROM pg_locks WHERE NOT granted`

### 任务重复执行

OrbitJob 通过 lease 机制防止重复执行。如果观察到重复：

1. 检查 Worker 是否在 lease 过期前完成（lease 过期后会被其他 worker 回收重试）
2. 检查 `CompleteInstance` 是否成功写入（Handler 执行成功但 DB 写入失败会导致重试）
3. 考虑增加 `WORKER_LEASE_DURATION_MAX` 或优化 handler 执行时长

### 性能调优

| 场景 | 调优方向 |
|------|---------|
| Scheduler 吞吐量不足 | 增大 `SCHEDULER_BATCH_SIZE_MAX`；检查 DB RTT |
| Worker 吞吐量不足 | 增大 `WORKER_CAPACITY_MAX`；检查 DB 队列深度 |
| DB CPU 过高 | 降低 `SCHEDULER_BATCH_SIZE_MAX`；拉长 tick interval |
| 短任务 lease 浪费 | 启用动态 lease，降低 `WORKER_LEASE_MIN_SEC` |
| 长任务被回收 | 增大 `WORKER_LEASE_DURATION_MAX`；优化 handler |

---

*最后更新：2026-05-14*

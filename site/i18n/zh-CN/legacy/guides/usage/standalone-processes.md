---
title: "独立进程部署"
---

<a id="5-standalone-processes"></a>

编译当前平台的全部 Go 包：

```bash
make build
```

交叉编译 Linux/amd64 二进制：

```bash
make build-all
```

`make build` 检查编译，不会在 `bin/` 生成这些可执行文件。`make build-all` 将配置的 Linux 交叉构建二进制写入该目录。本机可执行文件应显式指定输出路径，例如：

```bash
go build -o bin/scheduler ./cmd/scheduler
```

独立进程应引用同一安装配置。admin-api 使用 `ADMIN_DSN`；scheduler 与 operator 共享 `RUNTIME_DSN`，其中 operator 优先读取 `OPERATOR_DSN`：

```bash
set -a
. /opt/orbitjob/etc/database.env
set +a
./bin/scheduler
```

主机应使用对应架构的原生二进制。admin-api 和 operator 还需集群内 ServiceAccount 配置；裸机进程仅有 DSN 不足以启动。旧 `DATABASE_DSN` 和 `SCHEDULER_DSN` 保留为兼容回退，不应作为新部署的配置选择。

推荐启动顺序：

```text
owner-init -> migrate -> bootstrap -> admin-api/scheduler/operator/log-archive
```

生产环境中，运行进程绝不能共用 owner 或 superuser DSN。Helm 将 admin/runtime/migrator 角色分离，自定义部署应保持相同边界。

进程环境变量示例：

```text
deploy/env/admin-api.env.example
deploy/env/scheduler.env.example
```

常用变量：

| 进程 | 变量 | 含义／依据 |
|---|---|---|
| admin-api | `PORT` | HTTP 监听地址，见环境变量示例 |
| admin-api | `OPERATOR_NAMESPACE_TENANTS` | 与 Operator 使用相同安装映射；未设置时禁用类型化定义／日志路由 |
| admin-api | `RATELIMIT_READ_RPS` / `RATELIMIT_WRITE_RPS` / `RATELIMIT_TRIGGER_RPS` | 令牌桶设置，见环境变量示例与 chart values |
| scheduler | `RUNTIME_DSN` | 安装的运行连接 |
| scheduler | `SCHEDULER_HEALTH_PORT` | 组件健康监听地址 |
| operator | `OPERATOR_DSN`（回退到 `RUNTIME_DSN`） | 安装的运行连接 |
| operator | `OPERATOR_NAMESPACE_TENANTS` | namespace=tenant 映射，由 Helm 通过 `operator.namespaceTenants` 注入 |

Scheduler/operator 组件监听端点提供 `/healthz` 和 `/readyz`，就绪检查会探测数据库。Admin API 提供 `/healthz`、`/metrics` 和 `/openapi.json`，没有 `/readyz` 路由。其存活响应不能证明数据库连接正常或工作已完成。Chart 将 scheduler/operator 的就绪探针接到 `/readyz`；其存活探针使用 `/healthz`，scheduler 启动探针也使用该端点。Admin API 就绪探针仍使用 `/healthz`，不得据此判定数据库可用。

```text
GET /healthz   process alive
GET /readyz    readiness probe
```

停止进程时，前台进程按 `Ctrl+C` 并等待优雅退出；后台进程将占位符替换为实际 PID 后使用 `kill -TERM '<pid>'`。维护前先停止工作负载发布与执行写入方，再按数据库或归档备份流程操作。

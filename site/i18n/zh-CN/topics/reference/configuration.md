# 配置参考

使用 Chart values 和安装 Secret 配置安装环境。进程环境变量适用于源码开发以及在 Chart 外运行的组件。更改环境文件或 Secret 不会更新已运行的进程，也不会轮换 PostgreSQL 角色密码。

## Chart 配置

结合使用 [values.yaml](../../charts/orbitjob/values.yaml) 和 [values.schema.json](../../charts/orbitjob/values.schema.json)。schema 检查结构和条件要求；模板补充部署约束。

| 范围 | 配置与前提条件 |
| --- | --- |
| 镜像 | 使用一个 `global.imageTag` 指定从同一修订版本构建的镜像。为仓库镜像或本地加载的镜像设置拉取策略。 |
| 数据库 | 提供由 `cmd/configure` 准备的安装 Secret；PostgreSQL 独立于此 Chart 运行。参见[数据库配置](../database-setup.md)。 |
| 工作区 | `operator.namespaceTenants` 将 Kubernetes 命名空间映射到租户 ID。应准备对应的命名空间 RBAC；仅有数据库租户行不会授予命名空间访问权限。 |
| Console 访问 | `adminapi.service` 选择暴露方式。`adminapi.ingress` 使用已有控制器，并可选用已有 TLS Secret。配置精确的外部 Console origin。 |
| 运行时日志 | 归档存储、采集器暂存限制、内部 TLS 和网络策略需要按安装环境准备。参见[执行日志](../execution-logs.md)。 |
| 资源 | 组件资源请求和限制独立于工作负载容器所接受的资源配置。 |

安装前先渲染：

```bash
helm lint ./charts/orbitjob -f /path/to/installation-values.yaml
helm template orbitjob ./charts/orbitjob \
  --namespace orbitjob-system -f /path/to/installation-values.yaml
```

渲染不能证明 Secret 可用、存储已分配、网络策略已执行或数据库已连通。相应检查参见[安装流程](../setup/installation.md)。

## 数据库环境变量解析

每行使用第一个非空变量。DSN 必须标识该组件使用的角色；共享回退变量不代表所有者连接适合应用使用。

| 进程 | 解析顺序 |
| --- | --- |
| Admin API | `ADMIN_DSN`，然后是 `DATABASE_DSN` |
| Operator | `OPERATOR_DSN`，然后是 `RUNTIME_DSN`，再是 `DATABASE_DSN` |
| Scheduler | `RUNTIME_DSN`，然后是 `SCHEDULER_DSN`，再是 `DATABASE_DSN` |
| 日志归档 | 必须提供 `RUNTIME_DSN` |
| 迁移器 | `MIGRATION_MODE=owner-init` 使用所有者初始化输入；`migrate` 需要 `MIGRATOR_DSN`。角色和校验和输入通道约束参见数据库指南。 |

相应实现在各[命令入口](../../cmd/)和[数据库配置解析器](../../internal/platform/config/)中。环境变量示例位于 [deploy/env](../../deploy/env/)。

## API 与浏览器设置

| 变量 | 行为 |
| --- | --- |
| `PORT` | Admin HTTP 端口；为空时默认为 `8080`。 |
| `CONSOLE_PUBLIC_ORIGIN` | 精确的浏览器 origin，包含非默认端口；生产会话应使用 HTTPS。代理头不会重新定义该值。 |
| `CONSOLE_ALLOW_INSECURE_LOCALHOST` | 显式允许本地开发使用 HTTP 回环会话；不允许公开 HTTP origin。 |
| `OPERATOR_NAMESPACE_TENANTS` | Operator 和额外 Admin 路由使用的显式命名空间与租户映射。 |
| `LOG_ARCHIVE_URL` | 内部归档端点。Admin 读取方使用挂载的读取方证书、密钥和 CA。 |
| `RATELIMIT_READ_RPS` | 每租户读取令牌桶。代码默认值 `100`；Chart 默认值 `100`。 |
| `RATELIMIT_WRITE_RPS` | 每租户通用写入令牌桶，包含 Function 调用和 Workflow 变更。代码默认值 `10`；Chart 默认值 `100`。 |
| `RATELIMIT_TRIGGER_RPS` | Job 触发和实例取消令牌桶。Function/Workflow 写入使用写入令牌桶。代码默认值 `5`；Chart 默认值 `10`。 |

速率值同时设置补充速率和突发容量。收到 `429` 响应后，应依据响应进行有界重试；重复执行请求必须保留原始幂等身份。参见[调用 API](../tasks/call-api.md)。

## Operator 与归档设置

Operator 默认调度间隔为五秒（`OPERATOR_SCHEDULE_INTERVAL_SEC`），保留处理间隔为五分钟（`OPERATOR_RETENTION_INTERVAL_MIN`），健康端口为 `8082`（`OPERATOR_HEALTH_PORT`）。Scheduler 健康端口默认为 `6060`（`SCHEDULER_HEALTH_PORT`）。这些设置不构成调度延迟或可用性保证。

归档将数据保存在 `LOG_DIRECTORY` 下，默认目录为 `/data`。字节限制使用 `LOG_TOTAL_BYTES`、`LOG_RUN_BYTES`、`LOG_HEAD_BYTES`、`LOG_WORKFLOW_BYTES`、`LOG_TENANT_BYTES`、`LOG_DECLARED_BYTES` 和 `LOG_RESERVE_BYTES`。更改前应阅读[归档容量参考](../execution-logs.md)；降低配置预算不能创造文件系统空间，也不能安全丢弃受保护证据。

## CLI 连接设置

`--api-url` 覆盖 `ORBITJOB_API_URL`，后者覆盖 `ORBITJOB_API`；回退值为 `http://localhost:8080`。两个环境变量都是 URL。`--api-key` 覆盖 `ORBITJOB_API_KEY`。命令、输出和退出行为参见 [CLI 参考](../cli.md)。不要将凭据放入源码文件、终端录制或文档站点输入。

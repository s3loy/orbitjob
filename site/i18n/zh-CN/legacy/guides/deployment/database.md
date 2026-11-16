---
title: "数据库"
---

PostgreSQL 以名为 `orbitjob-postgres` 的 Deployment 运行在 `orbitjob` 命名空间。控制平面工作负载位于 `orbitjob-system`；渲染的 Job 位于 `operator.namespaceTenants` 为其租户映射的命名空间中（`deploy/kind/values-dev.yaml` 使用 `default`）。

```bash
kubectl exec -n orbitjob deployment/orbitjob-postgres -- \
  sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" exec psql -U "$POSTGRES_USER" -d "$POSTGRES_DB"'
```

`postgres` 超级用户绕过行级安全，因此上述命令用它执行跨租户本地维护。不存在可登录的 `orbitjob_owner` 角色。只有 `orbitjob_migrator`、`orbitjob_admin` 和 `orbitjob_runtime` 可以登录（[角色定义](https://github.com/s3loy/orbitjob/blob/main/internal/platform/migrate/roles.go)）；`orbitjob_operator` 是为未来独立 Operator 凭据预留的 NOLOGIN 身份，登录角色受 RLS 约束。

常用查询：

```sql
SELECT id, slug, status FROM tenants;
SELECT version, name FROM schema_migrations ORDER BY version;
SELECT tenant_id, phase, count(*) FROM job_run_control_plane GROUP BY tenant_id, phase;
```

Run 账本位于 `job_run_control_plane` 和 `job_run_attempts_control_plane`；物化定义位于 `job_definition_revisions`。`phase` 列是 Run 状态机（`Pending`、`CreatingAttempt`、`Running`、`RetryWaiting`、`Succeeded`、`Failed`、`ExecutionUnknown`、`CancelRequested`、`Canceled`、`CancelUnknown`）。Unknown 阶段保留尚未明确的执行与取消事实，见[执行恢复](/orbitjob/docs/architecture/#execution-recovery-and-concurrent-admission)。
`trigger` 表示 Run 来自计划、手动、重试、Check 或 Workflow 步骤；`actor` 记录请求者：计划循环使用 `orbitjob-scheduler`，Check 使用 `orbitjob-check-scheduler`，手动触发使用 API 密钥。应先检查阶段值。

<a id="migrations"></a>
### 迁移

Chart 将迁移作为 pre-install/pre-upgrade hook 运行。检查其结果和数据库中已应用的版本；仅有 migrations ConfigMap 不能证明迁移已应用：

```bash
kubectl exec -n orbitjob deployment/orbitjob-postgres -- \
  sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" exec psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Atqc "SELECT max(version) FROM schema_migrations"'
```

迁移仅向前执行，没有可用于回滚的 down migration。恢复应通过正向修复或还原经过审阅的备份完成。重建数据库只适用于可丢弃的开发数据。

当前数据库 schema 由 [`db/migrations/`](https://github.com/s3loy/orbitjob/blob/main/db/migrations/) 定义，包括增量的存储完整性和执行观察迁移。不得修改已记录的迁移校验和，也不得假定支持旧开发 schema。不存在受支持升级路径时，重建可丢弃数据；保留其他数据需要明确的迁移方案。

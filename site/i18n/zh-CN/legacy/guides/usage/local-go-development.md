---
title: "本地 Go 开发"
---

<a id="4-local-go-development"></a>

Scheduler 与数据库工具可连接适当且已迁移的数据库，在本地运行。admin-api 和 operator 当前调用 `rest.InClusterConfig`，仅设置 `KUBECONFIG` 不能在集群外启动。正常开发时，在 chart Deployment 中运行这些组件。

本地运行 scheduler 需使用生成的安装配置和数据库端口转发。启动前先停止使用同一数据库的另一个 scheduler，避免意外启动第二个求值进程：

```bash
# Port-forward the in-cluster PostgreSQL (installed by make kind-db)
kubectl -n orbitjob port-forward deployment/orbitjob-postgres 15432:5432 &

# Use the generated installation state (make kind-db writes it to
# .runtime/kind/database.env; the admin DSN is ADMIN_DSN)
set -a
. ./.runtime/kind/database.env
set +a

# Update the DSN host/port for this local port-forward before running.
go run ./cmd/scheduler
```

`make migrate-up DATABASE_URL="$MIGRATOR_DSN"` 对完整的 `db/migrations` 目录运行迁移器。新安装通常使用 Helm 的 owner-init 和 migrate hook；仅应用 baseline 会遗漏后续功能和完整性变更。

- Admin API 默认监听 `8080`，可通过 `PORT` 更改。
- 直接运行二进制不会创建数据库、运行迁移或执行 bootstrap；这些由 Helm hook 或 `cmd/bootstrap` 完成。
- 脚本化 API 调用需要 Bearer key；通过[管理员流程](/orbitjob/docs/usage/bootstrap-and-the-api-key/)获取初始集成访问。Console 账户在全部迁移和 bootstrap 完成后使用[服务端会话](/orbitjob/docs/accounts/)。

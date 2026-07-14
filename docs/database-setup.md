# 数据库配置

OrbitJob 把数据库配置放在 installation 层。配置一次，admin-api、scheduler、dispatcher、worker 和后续新增副本自动继承。

## Bundled PostgreSQL

Docker Compose 自带 PostgreSQL。你不需要写 DSN：

```bash
make setup
make docker-up
```

`make setup` 生成：

- `.env`：PostgreSQL owner、Grafana 和 bootstrap API key
- `.runtime/database.env`：migrator、admin、runtime 的派生连接配置
- `.runtime/database.json`：installation 状态，用于重复 setup 时复用密码

两个文件目录都被 Git 忽略。credential 文件权限是 `0600`。

再次运行 `make setup` 不会修改密码：

```text
Existing database configuration is valid. Runtime configuration refreshed.
```

如果要从头开始，运行 `make docker-reset`。`make env-clean` 也会同时删除 `.runtime/`，避免 PostgreSQL owner 密码与 installation state 分叉。

## External PostgreSQL

把唯一的 bootstrap DSN 写入权限为 `0600` 的文件：

```bash
install -m 0600 /dev/null /tmp/orbitjob-bootstrap-dsn
printf '%s\n' 'postgres://dbadmin:<password>@db.example.com:5432/orbitjob?sslmode=verify-full' \
  > /tmp/orbitjob-bootstrap-dsn
```

执行：

```bash
go run ./cmd/configure setup \
  --mode external \
  --database-dsn-file /tmp/orbitjob-bootstrap-dsn \
  --state .runtime/database.json \
  --runtime-env .runtime/database.env
```

bootstrap 身份需要创建角色和修改角色密码。配置工具自动创建并验证：

```text
orbitjob_migrator
orbitjob_admin
orbitjob_runtime
```

业务进程不会得到 bootstrap DSN。

## Kubernetes 和多副本

Kubernetes 中，一次 installation 使用一个 `orbitjob-database` Secret。所有 Pod 引用同一份 Secret。把 worker 从 1 扩到 10 不需要再次配置数据库：

```bash
kubectl scale deployment/orbitjob-worker -n orbitjob --replicas=10
kubectl rollout status deployment/orbitjob-worker -n orbitjob
```

Chart 只接收 Secret 名和固定 key 名，不要求你同时填写 password 和 DSN。`deploy/kind/verify-v020.sh` 展示了完整安装流程。

## 独立进程

如果你不用 Compose 或 Helm，配置工具生成的 `.runtime/database.env` 仍可作为统一来源：

```bash
set -a
. /opt/orbitjob/etc/database.env
set +a
./bin/admin-api
./bin/scheduler
./bin/dispatcher
./bin/worker
```

admin-api 读取 `ADMIN_DSN`。三个 runtime 共同读取 `RUNTIME_DSN`。

旧的 `SCHEDULER_DSN`、`DISPATCHER_DSN`、`WORKER_DSN` 和 `DATABASE_DSN` 仍可回退使用，但新部署不要再分别维护它们。

## TLS

External DSN 保留 PostgreSQL query 参数：

```text
postgres://dbadmin:<password>@db.example.com:5432/orbitjob?sslmode=verify-full&sslrootcert=/run/secrets/ca.crt
```

配置工具通过 `net/url` 派生内部 DSN。密码包含 `@:/?#%` 或空格时不需要手工 URL encoding。

## 常见错误

### 缺少本地配置

```text
Database configuration is missing. Run: make setup
```

先运行 `make setup`。不要手写 `.runtime/database.env`。

### External PostgreSQL 认证失败

检查 bootstrap DSN 中的用户名、密码、TLS 参数和目标数据库。工具只输出脱敏 endpoint，不会打印密码。

### 权限不足

bootstrap 用户必须能创建或修改 OrbitJob roles。只允许 DBA 预创建 role 的部署模式尚未交付。

### 切换数据库

默认 setup 不会覆盖已有 installation 状态。切换 endpoint 会改变权威数据源，不能视为普通配置更新。备份和数据迁移需要单独处理。

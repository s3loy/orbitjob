# 备份与恢复安装

可恢复的 OrbitJob 备份包含在协调维护边界上取得的 PostgreSQL、保留输出、采集器状态、安装凭据和 Kubernetes 配置。仅数据库转储无法恢复排队日志，也不能证明较早的 Kubernetes Job 已停止。

Chart 没有备份控制器或单命令恢复流程。应为实际存储系统选择并验证数据库和卷备份工具。以下命令提供 PostgreSQL 和配置部分；归档和采集器复制必须使用能够保留内容、所有权和权限的存储方法。

## 开始前

运维人员需要获得授权的数据库维护权限、Kubernetes 读写权限，以及所有归档卷和采集器卷的访问权限。备份目标应位于受保护集群和节点存储之外。采用适合 Secret、账户数据和原始工作负载输出的加密及访问控制。

定义所需恢复点、恢复窗口和原始工作负载核对计划。项目不为本流程设定 RPO 或 RTO。归档列表可读或快照请求成功，均不等于恢复已经测试。

在独立存储上的已有目录下选择新的备份目录。创建命令会拒绝复用已有备份目录：

```bash
umask 077
export RELEASE_NAMESPACE=orbitjob-system
export RELEASE_NAME=orbitjob
export BACKUP_DIR=/absolute/path/on/separate-storage/orbitjob-backup
mkdir "$BACKUP_DIR"
chmod 0700 "$BACKUP_DIR"
kubectl config current-context
```

## 建立一致边界

遵循[停止提交并排空](upgrading.md#stop-submissions-and-drain)。保存原始来源控制和组件规格，停止不兼容写入方并验证其已终止。保持 PostgreSQL 运行以便转储。

归档可用时，让采集器完成发送。采集器停止后，保存每个采集器当前的 `tail.db`、所有相关 SQLite 文件和全部排队数据块。复制归档卷前停止归档写入。就绪状态不能证明队列为空。不要组合不同时间点的偏移量和数据块。

记录非终态运行、不确定 Attempt、原始来源、JobRun 和 Job UID、资源版本、保留 Pod 和证据终结器。未解决运行需要明确恢复计划；将其备份不能使物理执行状态变为已知。绝不能移除证据终结器来取得表面上安静的备份边界。

## 保存 Kubernetes 配置与凭据

保存 Helm 状态和实际组件配置。实际 Kubernetes 对象可能包含 Helm 以外的变更：

```bash
helm get values "$RELEASE_NAME" -n "$RELEASE_NAMESPACE" -a > "$BACKUP_DIR/helm-values.yaml"
helm get manifest "$RELEASE_NAME" -n "$RELEASE_NAMESPACE" > "$BACKUP_DIR/helm-manifest.yaml"
kubectl get deployment,statefulset,daemonset,service,configmap,secret \
  -n "$RELEASE_NAMESPACE" -o yaml > "$BACKUP_DIR/control-plane.yaml"
kubectl get pvc -n "$RELEASE_NAMESPACE" -o yaml > "$BACKUP_DIR/archive-claims.yaml"
kubectl get crd -o yaml > "$BACKUP_DIR/crds-inventory.yaml"
```

还应保存原始迁移 ConfigMap、相关 PV 定义、发行版元数据、命名空间、RBAC、准入 Webhook、来源控制清单，以及每个已映射执行命名空间内的全部 OrbitJob 工作负载 CR。记录实际镜像 ID 和摘要，以及保存的副本数。从管理员的受保护存储复制私有的数据库生成状态和运行时环境。

配置导出包含明文或 base64 编码的敏感数据；base64 不是加密。应与报告和普通源码版本控制分开保存。不要盲目将导出应用到另一集群。其中包含需要审查的服务器分配字段、已存储凭据和资源身份。

## 备份 PostgreSQL

使用适合服务器版本的 PostgreSQL 客户端工具。选择已有的受保护 libpq 服务和密码文件配置；不要将密码或带凭据 DSN 放入 argv。维护账户必须能够读取全部所需表和角色定义。应用 RLS 登录账户不是通用备份身份。

对于管理员已配置名为 `orbitjob-backup` 的服务的外部服务器：

```bash
export PGSERVICE=orbitjob-backup
pg_dump --format=custom --file="$BACKUP_DIR/database.dump"
pg_dumpall --roles-only > "$BACKUP_DIR/roles.sql"
psql -X -v ON_ERROR_STOP=1 -At \
  -c 'SELECT version, name, checksum, applied_at FROM schema_migrations ORDER BY version' \
  > "$BACKUP_DIR/schema-migrations.tsv"
pg_restore --list "$BACKUP_DIR/database.dump" > "$BACKUP_DIR/database-list.txt"
```

libpq 服务选择目标数据库和 TLS 设置；执行命令前应验证其目标。角色转储可能包含密码哈希和集群范围角色。应限制访问，并在恢复前审查哪些角色属于本安装。如果托管服务拒绝导出角色，应取得数据库管理员批准的角色重建计划及匹配凭据；缺少这些内容时不能声称备份完整。

对于仓库的本地 PostgreSQL Deployment，容器环境已经提供开发凭据：

```bash
export DB_NAMESPACE=orbitjob
kubectl exec -n "$DB_NAMESPACE" deployment/orbitjob-postgres -- \
  sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" exec pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc' \
  > "$BACKUP_DIR/database.dump"
kubectl exec -n "$DB_NAMESPACE" deployment/orbitjob-postgres -- \
  sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" exec pg_dumpall -U "$POSTGRES_USER" --roles-only' \
  > "$BACKUP_DIR/roles.sql"
pg_restore --list "$BACKUP_DIR/database.dump" > "$BACKUP_DIR/database-list.txt"
```

这些命令仅适用于本地清单；不要用其中的超级用户凭据替代外部安装的维护策略。

## 备份归档与采集器存储

从正在运行的安装中确认实际挂载 PVC、PV 和节点路径。默认归档卷申请为 `data-orbitjob-log-archive-0`；本地数据库可能已迁移到更大的卷申请。应备份实际挂载卷，不要备份名称相近但已废弃的卷申请。

对于归档，使用具有所需一致性保证的存储快照，或复制已停止写入的文件系统。保留完整归档目录及其元数据。PostgreSQL 的执行日志目录和归档内容必须引用相同的保留身份。

对于每个符合条件的采集器节点，复制其完整的已停止状态目录，包含偏移量、SQLite 日志文件和排队数据块。默认路径为节点上的 `/var/lib/orbitjob-collectors/<release-namespace>/<release-name>`。队列配置大小不包含 SQLite 偏移量数据库和文件系统开销。还应包含物理配额、挂载配置，以及本地维护流程使用的任何持久有界文件系统镜像。

删除 kind 集群或节点容器时，kind 节点文件系统也会消失。同一节点内的第二份副本不是独立备份。删除集群前，应将验证后的备份转移到独立目标。

## 验证与保留备份

所有复制完成后记录文件校验和。对于具有 `shasum -a 256` 的 Unix 系统，从备份目录运行，并选择实际创建的文件：

```bash
(
  cd "$BACKUP_DIR"
  shasum -a 256 database.dump roles.sql helm-values.yaml helm-manifest.yaml \
    control-plane.yaml archive-claims.yaml > checksums.txt
  shasum -a 256 -c checksums.txt
)
```

在同一记录中包含归档和采集器复制清单及校验和。验证所有权、权限、文件数和复制完成情况。卷缺失、转储不可读、队列不完整或迁移输入不一致时，应停止维护操作。在问题解决或作出经过审查的恢复决定前，保持安装暂停。

依赖备份前先执行隔离恢复。在恢复环境中检查账户访问、历史、权限和保留输出前，保留原始安装和备份。

## 隔离恢复

使用新的隔离数据库或服务器及存储目标，并确保 PostgreSQL 和应用版本兼容。阻止普通客户端和调度。两个安装都有可能发布或接管工作时，不要将恢复后的 Operator 连接到原始执行命名空间。

通过受保护的 libpq 服务选择空恢复数据库，先恢复已审查角色定义，再恢复自定义格式转储：

```bash
export PGSERVICE=orbitjob-recovery
psql -X -v ON_ERROR_STOP=1 -f "$BACKUP_DIR/roles-reviewed.sql"
pg_restore --exit-on-error --dbname="service=$PGSERVICE" "$BACKUP_DIR/database.dump"
```

`roles-reviewed.sql` 是数据库管理员从已保存角色定义中为本安装选出的子集。`service=$PGSERVICE` 选择恢复服务的连接设置，包括数据库和 TLS 参数。恢复前，准备服务要求的数据库所有权和扩展权限。部分恢复后，需要清理隔离目标或按管理员审查的方案重试；不要在错误原因不明时继续。

从同一备份边界恢复归档及各采集器状态。保持偏移量和数据块配套。经过审查后恢复 Secret 和配置，保留原始迁移输入表示形式和所需凭据身份。启动读取方前验证所有已记录校验和。

重建 Kubernetes 对象会分配新 UID。重新应用 YAML 不会恢复原始来源、JobRun 或 Job 身份。如果恢复机制支持，应保留原始集群状态；否则应明确核对历史工作负载和可能仍在运行的工作负载。不要改写历史身份使恢复对象看似原始对象，也不要让隔离恢复为不确定工作执行替代 Attempt。

## 验证与切换

启用发布方或用户前，应验证：

- 完整迁移台账和挂载迁移校验和匹配已保存安装通道；没有引入新 baseline 或 down 迁移。
- 账户、会话和能力凭据、成员资格、策略、密钥和审计历史符合预期状态。失去访问时应使用支持的运维恢复流程。
- 历史定义、运行和 Attempt 事实、不确定执行及保留输出仍可在正确授权范围内读取。
- 归档容量、采集器偏移量和数据块、证书及配额保持一致。缺失输出继续明确显示为证据缺失。
- 只有一个预期控制平面能够向执行命名空间发布工作，且原始物理执行已经核对。

完成这些检查后使用受控测试来源，再通过 CAS 版本仅恢复由本次维护操作设置的来源暂停状态。按安装发布计划切换客户端和端点。在恢复验收完成前，保持旧安装停止并予以保留。

验证失败时应继续暂停发布方和提交。保留恢复目标和诊断证据；选择兼容修正版本，或重新执行干净的隔离恢复。仅为更换二进制而恢复较旧数据库，可能丢失较新的执行或账户事实。

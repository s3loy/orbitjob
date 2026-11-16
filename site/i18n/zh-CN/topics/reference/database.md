# 数据库配置

OrbitJob 使用一个安装数据库，并为模式维护、Admin API 和运行时组件分别配置凭据。PostgreSQL 保存定义、执行元数据、授权、账户和审计历史。执行输出和生命周期正文单独存储在日志归档 PVC 上。

在[安装 Chart](setup/installation.md) 前完成此流程。对于贡献者使用的 kind 环境，`make kind-db` 会准备本地服务器和 Secret；不要将其固定开发凭据用于外部服务器。

## 数据库角色与管理员访问

准备一个已有数据库，以及具备 `LOGIN`、`CREATEROLE`、数据库所有权和在 `public` 中安装 `pgcrypto` 权限的引导账户。本地 PostgreSQL 17 镜像允许数据库所有者在没有超级用户权限的情况下安装该受信任扩展。外部服务可能对扩展或模式施加其他限制；设置前应验证这些限制。

如果 OrbitJob 角色已由其他创建者建立，引导账户还需要这些角色的 `ADMIN OPTION`。仅有 `CREATEROLE` 并不能管理所有已有角色。数据库管理员必须修复非预期的 `SUPERUSER`、`BYPASSRLS`、`CREATEROLE`、`INHERIT` 或 `LOGIN` 属性。设置检查会拒绝角色偏差，不会静默接受扩大的权限。

所有者初始化会创建以下身份：

| 角色 | 允许登录 | 用途 |
|---|---|---|
| `orbitjob_table_owner` | 否 | 拥有模式对象；迁移器切换为该角色 |
| `orbitjob_migrator` | 是 | 执行 `SET ROLE orbitjob_table_owner` 后按顺序运行迁移 |
| `orbitjob_admin` | 是 | Admin 应用存储和获准使用的身份函数 |
| `orbitjob_runtime` | 是 | Operator、Scheduler、准入和归档存储 |
| `orbitjob_operator` | 否 | 已有模式权限身份；不是连接用户 |
| `orbitjob_owner` | 否 | 已有模式身份；不是维护登录账户 |
| `orbitjob_reader` | 否 | 已有模式身份；不是已签发的只读登录账户 |

受管角色不具备超级用户、绕过 RLS、创建角色或继承角色的权限。仅迁移器拥有表所有者角色的成员资格。应用进程不会获得引导 DSN。Kubernetes 钩子 Job 使用引导账户设置角色，并使用迁移器修改模式。应保留引导账户供后续 owner-init 钩子使用，并将其凭据的使用范围限制在安装管理。

PostgreSQL 角色属于整个服务器，这些角色名称固定不变。同一服务器上两个数据库分别生成的安装密码可能改变另一安装使用的凭据。应使用专用服务器，或使用经过管理员审查的共享角色凭据方案；仅使用不同数据库名称无法隔离角色设置或轮换。

租户查询使用事务局部的 `app.tenant_id` 和行级安全（RLS）。使用应用登录账户但未设置租户上下文的普通 SELECT 可能不返回任何行。不能据此判定安装没有数据。跨租户维护需要得到明确授权的维护身份。

## 选择数据库模式

`bundled` 和 `external` 选项用于配置凭据；两者都不会通过 Helm 安装 PostgreSQL 服务器。

| 模式 | 初始设置行为 | 服务器准备 |
|---|---|---|
| `bundled` | 不连接 PostgreSQL，直接生成安装状态 | 管理员单独提供服务器 |
| `external` | 创建或验证角色，应用生成的密码并检查每个登录账户 | 管理员或外部服务提供服务器 |

两种模式默认都会写入 `.runtime/database.json` 和 `.runtime/database.env`。这些文件包含凭据，以 `0600` 模式创建，并由 Git 忽略。其所在目录也以私有权限创建。应将这两个文件的受保护备份与安装记录一并保留。

已有状态文件的优先级高于新连接选项。重复运行设置会加载该文件并刷新环境文件，不会重新连接 PostgreSQL 或轮换凭据。消息 `Existing database
configuration is valid` 描述的是已保存配置，不代表实时服务器健康检查。

`--reconfigure` 会替换 bundled 状态并生成新的角色密码；它本身不会改变在线数据库密码。external 模式拒绝该选项。删除状态或针对新端点重新运行设置，不等于数据库迁移或协调完成的凭据轮换。

## Bundled PostgreSQL

使用这些生成值前，应先准备服务器和数据库。默认端点为 `postgres://pg:5432/orbitjob?sslmode=disable`，默认引导用户名为 `orbitjob`。应提供符合实际网络的端点，并在需要时使用经过验证的 TLS 设置。

在 Bash 中以不回显方式读取已有服务器所有者密码：

```bash
read -r -s -p 'PostgreSQL owner password: ' PG_PASSWORD
printf '\n'
export PG_PASSWORD
go run ./cmd/configure setup --mode bundled \
  --bundled-endpoint 'postgres://pg:5432/orbitjob?sslmode=disable'
unset PG_PASSWORD
```

此命令不会测试连通性、创建角色或应用迁移。安装 Secret 创建后，Chart 钩子才会执行这些操作。如果服务器、引导账户或集群内端点不可用，不要继续。

## 外部 PostgreSQL

使用包含数据库名称、用户名和非空密码的 PostgreSQL URI。手工编写 URI 时，应对保留字符进行百分号编码。`configure` 在派生角色 DSN 时保留连接和 TLS 查询参数，并移除登录覆盖参数 `user`、`password`、`passfile`、`service` 和 `servicefile`。生成的用户名和密码按 URL 规则编码。

使用凭据编辑工具或受保护的凭据通道创建私有 DSN 文件：

```bash
umask 077
install -m 0600 /dev/null /tmp/orbitjob-bootstrap-dsn
```

向该文件写入类似以下 URI，并替换示例值：

```text
postgres://dbadmin:PERCENT_ENCODED_PASSWORD@db.example.com:5432/orbitjob?sslmode=verify-full
```

从选定源码的根目录运行：

```bash
go run ./cmd/configure setup --mode external \
  --database-dsn-file /tmp/orbitjob-bootstrap-dsn \
  --state .runtime/database.json \
  --runtime-env .runtime/database.env
```

输入必须是普通私有文件，不得授予组或其他用户任何权限。首次 external 设置连接时使用五秒 ping 超时，初始化角色和密码，然后分别检查 migrator、admin 和 runtime 的登录连通性。它不会应用编号迁移，也不会创建 Default 工作区。出现认证、TLS、角色或连通性错误时应停止。保留已完成的部分角色设置，调查后再重试；即使本地状态尚未写入，数据库也可能已发生变化。

设置机器上的端口转发地址不能供集群 Pod 使用。应配置所有使用方均可访问的 DNS 端点，或参照 kind 辅助工具，通过经过审查的端点替换生成单独的集群内 Secret。替换地址时必须保留预期数据库、TLS 身份和凭据映射。

### TLS 文件

DSN 可以包含 `sslrootcert`、`sslcert` 或 `sslkey`，但每个使用方都必须能够访问其中引用的路径。主机文件不会自动出现在 Pod 中。当前 Chart 没有通用的数据库 TLS 卷挂载设置；需要私有 CA 或客户端证书文件时，应为 owner-init、migrate、bootstrap 和所有应用使用方提供持续维护的定制配置。安装前应验证渲染后的 Pod 能够获得这些文件。不要降低 `sslmode` 要求来绕过缺失的信任配置。

### 创建安装 Secret

首次安装时，加载生成的 shell 环境，并在发行版命名空间内创建一个 Secret。以下示例使用 Chart 默认的 Secret 名称和键。将命名空间设置为目标安装，然后检查上下文：

```bash
export RELEASE_NAMESPACE=orbitjob-system
export DATABASE_SECRET_NAME=orbitjob-database
kubectl config current-context
kubectl create namespace "$RELEASE_NAMESPACE" --dry-run=client -o yaml | kubectl apply -f -
```

以下命令创建私有临时清单，不会将凭据放入命令参数。它需要 Bash、Python 3、OpenSSL 和 kubectl。命令会有意拒绝覆盖已有 Secret：

```bash
(
  set -euo pipefail
  umask 077
  set -a
  . .runtime/database.env
  set +a
  export RELEASE_NAMESPACE DATABASE_SECRET_NAME
  INSTALL_BOOTSTRAP_KEY="otj_$(openssl rand -hex 24)"
  export INSTALL_BOOTSTRAP_KEY
  manifest="$(mktemp "${TMPDIR:-/tmp}/orbitjob-database-secret.XXXXXX")"
  trap 'rm -f "$manifest"' EXIT
  python3 >"$manifest" <<'PY'
import json
import os
import sys

fields = {
    "bootstrap-owner-dsn": "BOOTSTRAP_OWNER_DSN",
    "migrator-dsn": "MIGRATOR_DSN",
    "admin-dsn": "ADMIN_DSN",
    "runtime-dsn": "RUNTIME_DSN",
    "migrator-password": "MIGRATOR_PASSWORD",
    "admin-password": "ADMIN_PASSWORD",
    "runtime-password": "RUNTIME_PASSWORD",
    "bootstrap-api-key": "INSTALL_BOOTSTRAP_KEY",
}
values = {key: os.environ[variable] for key, variable in fields.items()}
if not all(values.values()):
    raise SystemExit("a required database Secret value is empty")
json.dump({
    "apiVersion": "v1",
    "kind": "Secret",
    "metadata": {
        "name": os.environ["DATABASE_SECRET_NAME"],
        "namespace": os.environ["RELEASE_NAMESPACE"],
    },
    "type": "Opaque",
    "stringData": values,
}, sys.stdout)
PY
  kubectl create -f "$manifest"
)
```

该 Secret 包含四个角色 DSN、三个角色密码和初始 API key。`database.secretName` 和 `database.keys` 用于自定义映射；自定义时应同时更新所有字段。副本共享这份安装配置，但这并不能证明具备高可用性。

此流程仅适用于首次安装。不要为已初始化的数据库生成新的引导 API key。Bootstrap 会将 API key 身份提交到 PostgreSQL，并将其交付到另一个固定名称为 `bootstrap-api-key`、键为 `api-key` 的 Secret。已有交付 Secret 发生冲突属于错误，不能据此替换数据库 API key。`bootstrap.secretName` 已弃用且会被忽略。

检查安装状态并保护好备份后，删除临时输入 DSN 文件。保持生成状态、在线 Secret 和 PostgreSQL 角色密码一致。

## 迁移执行与兼容性

Chart 在安装或升级前，先以钩子权重 `-30` 运行 owner-init，再以 `-20` 运行 migrate。应用 bootstrap 在安装或升级后以权重 `-10` 运行。钩子完成与工作负载就绪是不同状态。

迁移器需要 `MIGRATION_MODE=migrate`、`MIGRATOR_DSN` 和迁移目录。它获取安装级咨询锁，验证已记录的文件校验和，并在独立事务中应用每个尚未应用的 `.up.sql`。后续迁移失败时，之前成功的迁移仍保持已提交。部分历史变更存在以 `.down.sql` 结尾的 SQL 文件，但部署迁移器不提供 down 操作。`MIGRATIONS_BASELINE_VERSION` 会被拒绝。

使用获得授权的维护连接检查完整台账：

```sql
SELECT version, name, checksum, applied_at
FROM schema_migrations ORDER BY version;
```

当前迁移链从 `0001_baseline` 到 `0028_audit_resource_identity`。除最大版本外，还应验证文件身份和顺序。已发布迁移的字节（包括注释）和已记录校验和必须保持不变。

### 保留迁移输入通道

Chart 使用 YAML `|-` 将 SQL 打包进 ConfigMap，这会移除末尾的换行字节。迁移器对挂载文件中读取的字节计算哈希。因此，通过 Helm 初始化的数据库与通过原始文件系统初始化的数据库，可能为相同 SQL 语句记录不同校验和。

安装应继续使用初始化其台账时的输入通道。对于 Chart 安装，应保留并验证 Helm 挂载的 ConfigMap 表示形式。对于原始文件系统安装，应保留最初的原始输入。未经单独审查的兼容性流程，不要对 Helm 初始化的安装运行 `make migrate-up`。为匹配校验和而恢复末尾换行、编辑 `schema_migrations` 或修改历史 SQL，会失去兼容性检查的保护。

<a id="consolidated-local-development-lineage"></a>

### 已有开发迁移链

如果数据库将版本 7 记录为 `storage_integrity`，它属于较早的纯后端开发迁移链。当前版本 7 是 `platform_key_documents`；校验和验证必须拒绝这种不匹配。保留其数据和回执。仅可重建可丢弃数据库；需要保留的数据必须有经过审查的导出恢复或迁移方案。不要应用两个版本 7 文件，也不要清空台账使其看似已更新。

### 写入方切换

对于需要保留的安装，应在应用新 CRD 和迁移前[停止提交并排空](operations/upgrading.md#stop-submissions-and-drain)。停止旧版 Admin API 发布方、Operator 和其他不兼容写入方，确认它们已终止，执行迁移，然后仅启动兼容镜像。Helm pre-upgrade 钩子不会停止旧 Deployment。

应保留以下兼容性约束：

- 迁移 0012 在切换时确认已有终态结果，以保留其统计数据。自动恢复适用于之后产生的终态结果。每次运行的 SLI 去重事实会在运行历史清理后保留；显式执行 SLI 或租户维护时，必须按要求的顺序删除其依赖事实。
- 迁移 0013 及后续执行处理保留原始 JobRun/Job 身份、执行不确定性和持久停止证据。`ExecutionUnknown` 和 `CancelUnknown` 属于未解决状态。API 对象缺失或时间经过，均不构成重试、确定终态结果或释放并发占用的依据。
- 完整摘要运行地址需要兼容的发布方。已有匹配历史资源无需批量重命名即可继续读取。应使用返回的 Kubernetes 地址，不要自行重建缩短的后缀。
- 推进 Attempt 或清理前，必须提交保留的 Pod 证据和待完成释放进度。在证据终结器安全释放前，应保持 PostgreSQL 和兼容 Operator 可用。遵循[升级与卸载排空流程](operations/upgrading.md#stop-submissions-and-drain)。

迁移 0012 前，使用维护身份检查跨租户 SLO 引用：

```sql
SELECT s.id AS slo_id, s.tenant_id AS slo_tenant, i.tenant_id AS sli_tenant
FROM slos s JOIN slis i ON i.id = s.sli_id
WHERE s.tenant_id <> i.tenant_id;
```

迁移前应解决查询返回的所有权不一致。新的外键会拒绝这种情况；迁移不会重新分配或删除这些行。

## 模式权限与账户变更

迁移 0012 撤销应用对审计父表及已有分区的 `UPDATE`、`DELETE` 和 `TRUNCATE` 权限。新分区必须保留应用只能追加的边界。离线审计保留维护使用获得授权的表所有者维护身份。

迁移 0014 增加受 RLS 保护的执行日志目录。Runtime 可以管理其中的记录；Admin 可以读取记录，并且仅能更新 `cleanup_requested_at` 以执行带审计的清理。输出仍保存在单独的归档 PVC 上。目录存储和身份准入分别具有独立容量限制；参见[执行日志](execution-logs.md)。

受限的 SECURITY DEFINER 函数仅向 Admin 提供所需的读取与身份操作接口，同时保留租户表 RLS：

| 迁移 | 行为 |
|---|---|
| 0007 | 为已认证密钥提供平台 API key 预设文档和获准的预设边界；不认证调用方，也不返回租户策略 |
| 0011 | 工作区元数据目录使用应用已授权的租户范围，并在分页前进行字面搜索 |
| 0027 | 提供固定的身份操作集合，涵盖账户、成员资格、会话、能力凭据和安装审计 |

这些函数由 `orbitjob_table_owner` 拥有，固定 `search_path`，并且不向 runtime、operator 或 PUBLIC 授予这些特权接口的访问权限。平台策略读取方保留显式 Deny 和权限边界交集；选定租户不会解析平台预设的 `self` 范围。

迁移 0008 和 0009 增加 Function 创建回执和 Workflow 调度元数据。应先应用迁移，再部署相应读写方。命名内置预设增加操作时，已有自定义策略文档不会随之扩展。历史 Workflow 计划时间仍为 NULL；被拒绝的行必须包含原因，已记录来源 UID 继续保持唯一性。

迁移 0027 增加个人 Console 账户，不会将 API key 转换为用户或成员资格。已有 API key 继续使用 bcrypt 认证并供脚本调用；它们不能登录 Console。部署匹配的 Admin 镜像，配置 HTTPS 或显式启用回环开发模式，然后遵循[首位管理员设置](accounts.md#create-the-first-administrator)。必须存在活动的 Default 工作区。设置过程以原子方式消耗一次性能力凭据，并记录管理员、显式工作区所有权和永久认领标记。失去管理权限时应使用[运维恢复流程](accounts.md#recover-installation-administration)，不能删除账户或清除该标记。

迁移 0028 将分区父表及其已有分区中的 `audit_events.resource_id` 扩展为 `TEXT`。Checked-definition 审计事件保留完整的 `namespace/name`；合法 Kubernetes 身份可能超过原先 64 字符列的限制。已有行、资源索引、租户策略和应用权限均予以保留。应由拥有表所有权的迁移器应用此迁移，再部署匹配的应用。已有审计分区必须保留该所有权。遵循上述写入方切换和输入通道规则；旧安装在迁移前仍可能拒绝较长身份。一旦记录了较长标识符，恢复为 64 字符列就需要另行审查数据处理方案。

## 进程配置

受控启动器可以加载生成文件：

```bash
set -a
. .runtime/database.env
set +a
```

| 使用方 | DSN 选择 |
|---|---|
| Admin API | `ADMIN_DSN`，然后是旧版 `DATABASE_DSN` |
| Scheduler | `RUNTIME_DSN`，然后是旧版 `SCHEDULER_DSN`，再是 `DATABASE_DSN` |
| Operator | `OPERATOR_DSN`，然后是 `RUNTIME_DSN`，再是旧版 `DATABASE_DSN` |
| 日志归档和准入 | `RUNTIME_DSN` |
| Bootstrap | `ADMIN_DSN`，然后是旧版 `DATABASE_DSN`；也接受位置参数 DSN |
| Owner-init | `BOOTSTRAP_OWNER_DSN` 和三个角色密码变量 |
| Migrate | `MIGRATOR_DSN` |

新部署应使用主要变量。Dispatcher 和 worker DSN 变量不再配置任何组件。应用入口不会加载生成的 JSON 来代替环境配置。Admin、Operator 和归档需要集群内 Kubernetes 配置；原生二进制文件和主机 kubeconfig 不会自动提供该环境。

## 轮换、端点变更与恢复

更新 Secret 不会重启使用方，重启 Pod 也不会轮换 PostgreSQL 密码。轮换必须协调在线角色变更、DSN、密码字段、本地安装状态、钩子和工作负载重启。Chart 不实现这一协议。`make kind-db` 可以在其 Secret 变化后重启本地 Deployment；它是开发辅助工具，不是通用轮换流程。

更改端点会改变安装的数据来源。应停止写入方、备份、恢复或迁移数据，并在切换凭据前验证新服务器。在验证新安装的历史、账户、策略、执行证据和归档读取前，保留旧数据库和卷。参见[备份与恢复](operations/backup-and-recovery.md)。

应用回滚必须保留增量模式、账户、审计记录、去重和不确定性证据。缺少这些约定的旧二进制文件不能作为兼容恢复版本。需要回滚模式时，应恢复协调备份。不要将运行历史 down 文件或编辑迁移台账作为运维回滚流程。

权限、连通性和迁移失败的处理参见[故障排查](operations/troubleshooting.md)。

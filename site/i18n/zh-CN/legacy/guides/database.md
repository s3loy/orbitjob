---
title: "数据库设置"
---

OrbitJob 按安装保存数据库配置。各进程及其副本使用分配给自身数据库角色的连接。

迁移 0014 增加租户范围执行日志身份目录和带审计的清理请求。归档服务复用 `orbitjob_runtime`；`orbitjob_admin` 可以读取目录，但只能更新其中的清理请求列。RLS 在现有所有者边界下保持 ENABLE-only。输出和生命周期正文保存在独立日志 PVC。元数据准入保护与膨胀恢复见[执行日志](/orbitjob/docs/execution-logs/)。

<a id="what-the-bootstrap-account-needs"></a>
## Bootstrap 账户要求

使用一个账户执行一次初始化，以创建工作负载运行所用角色。该账户需要：

- `LOGIN` 和 `CREATEROLE`。
- 目标数据库所有权。

kind 部署使用的 PostgreSQL 镜像不要求 bootstrap 账户具备 `SUPERUSER`。数据库所有者可以安装受信任 `pgcrypto` 扩展，并在 `owner-init` 中执行 schema 授权。外部服务还需在设置前检查扩展策略、连接能力和 schema 所有权。

如果角色已存在且由其他账户创建，bootstrap 账户还需要这些角色的 `ADMIN OPTION`；仅有 `CREATEROLE` 只覆盖该账户创建的角色。`scripts/kind-db.sh` 在切换已有本地安装时授予此权限。

`owner-init` 创建并管理以下角色：

```text
orbitjob_table_owner   orbitjob_migrator   orbitjob_admin   orbitjob_runtime
orbitjob_operator      orbitjob_owner      orbitjob_reader
```

只有 `orbitjob_migrator`、`orbitjob_admin` 和 `orbitjob_runtime` 具有 `LOGIN`；`orbitjob_table_owner`、`orbitjob_operator`、`orbitjob_owner` 和 `orbitjob_reader` 均为 `NOLOGIN` 身份。业务进程不接收 bootstrap DSN。迁移 hook 使用 owner/migrator 凭据；应用进程使用 admin/runtime 凭据。Admin 和 Runtime 租户查询执行行级安全；迁移身份可以切换为表所有者角色进行 schema 维护。

<a id="bundled-postgresql"></a>
## 内置 PostgreSQL

`cmd/configure setup` 有两种模式。Bundled 模式不连接服务端，只生成安装状态，不配置 PostgreSQL、角色或 schema。通过 `PG_PASSWORD` 提供现有内置服务的所有者密码，必要时通过 `--bundled-endpoint` 选择端点。标志默认值见 `go run ./cmd/configure setup -h`。

在 Bash 中不回显地读取所有者密码：

```bash
read -r -s -p 'Bundled PostgreSQL owner password: ' PG_PASSWORD
printf '\n'
export PG_PASSWORD
go run ./cmd/configure setup --mode bundled
```

`configure setup` 写入：

- `.runtime/database.env`：bootstrap owner、migrator、admin 和 runtime 角色的派生 DSN，以及生成的密码。
- `.runtime/database.json`：安装状态，重复设置时复用密码。

两个路径均被 gitignore 排除，文件权限均为 `0600`。

状态文件已存在时，`configure setup` 从该状态刷新运行环境，不改变密码，也不重新连接验证服务端。消息只描述本地配置：

```text
Existing database configuration is valid. Runtime configuration refreshed.
```

`--reconfigure` 替换 bundled 安装状态并生成新角色凭据，本身不轮换运行中的数据库密码。External 模式拒绝此标志。通过明确维护流程保持状态、数据库角色密码和已部署 Secret 一致；删除状态不是凭据轮换流程。

<a id="external-postgresql"></a>
## 外部 PostgreSQL

将 bootstrap DSN 放入仅自己可读的文件：

```bash
install -m 0600 /dev/null /tmp/orbitjob-bootstrap-dsn
printf '%s\n' 'postgres://dbadmin:<password>@db.example.com:5432/orbitjob?sslmode=verify-full' \
  > /tmp/orbitjob-bootstrap-dsn
```

然后运行：

```bash
go run ./cmd/configure setup \
  --mode external \
  --database-dsn-file /tmp/orbitjob-bootstrap-dsn \
  --state .runtime/database.json \
  --runtime-env .runtime/database.env
```

本地 kind 集群通过 `make kind-db` 完成这些操作并创建 Secret，使用拥有数据库的 `orbitjob_bootstrap` 账户，不使用镜像超级用户。

External 设置创建或强化角色、设置密码并检查登录连通性，不应用业务迁移，也不运行应用 bootstrap。Chart 的 owner-init、迁移和 bootstrap hook 在安装时执行这些步骤。复用本地状态文件也会复用其中端点；不能假定不同 `--database-dsn-file` 会改变已有安装。

<a id="create-the-installation-secret"></a>
### 创建安装 Secret

首次安装时，先在目标集群创建 release 命名空间，再在运行 Helm 前安装生成的凭据。示例使用 chart values 中默认 `database.keys` 映射。`RELEASE_NAMESPACE` 和 `DATABASE_SECRET_NAME` 是示例变量，必须与 Helm 命名空间及 `database.secretName` 匹配；自定义键时相应调整映射。除已生成 env 文件外，还需要 Python、OpenSSL 和 kubectl。

```bash
kubectl config current-context
kubectl create namespace "${RELEASE_NAMESPACE:-orbitjob-system}" \
  --dry-run=client -o yaml | kubectl apply -f -
```

DSN 必须可从 Pod 访问。宿主机临时端口转发或仅设置机器可用的 TLS 文件不能作为可用集群内端点。

```bash
(
  set -euo pipefail
  umask 077
  set -a
  . .runtime/database.env
  set +a
  export RELEASE_NAMESPACE="${RELEASE_NAMESPACE:-orbitjob-system}"
  export DATABASE_SECRET_NAME="${DATABASE_SECRET_NAME:-orbitjob-database}"
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

临时文件为私有文件，退出时删除；凭据不作为命令参数传入。`kubectl create` 拒绝已有 Secret。此操作不是轮换或恢复流程。已初始化安装不得生成替代 bootstrap 密钥。首次安装前置条件完成后，chart hook 仍需执行 schema 迁移和应用 bootstrap。

<a id="kubernetes-and-replicas"></a>
## Kubernetes 与副本

一个安装使用一个数据库 Secret。组件读取各自角色的键；副本继承相同配置。Secret 名称和完整键映射定义在 [`charts/orbitjob/values.yaml`](https://github.com/s3loy/orbitjob/blob/main/charts/orbitjob/values.yaml) 的 `database` 下。其中包含角色 DSN 和角色密码，因为 owner-init hook 要在迁移与运行进程使用 DSN 前应用密码。

共享凭据不构成高可用。随附 chart 为每个组件设置一个副本；Scheduler 的可选 etcd 选举需单独配置。不能推定扩容 Deployment 就足以协调单例循环。

更改 Secret 不会重启读取它的 Pod；chart 无法对自己未创建的 Secret 计算哈希。`make kind-db` 在 Secret 内容变化时滚动更新匹配的本地工作负载；这是开发辅助工具，不是通用轮换协议。人工轮换必须协调运行中的角色密码、Secret 值和工作负载重启；仅重启不会改变 PostgreSQL 凭据。

<a id="generated-process-environment"></a>
## 生成的进程环境

生成的 `.runtime/database.env` 描述进程配置使用的环境接口。受控启动程序可以按以下方式加载其 shell 格式：

```bash
set -a
. .runtime/database.env
set +a
```

加载此文件不是独立安装流程。当前 Admin API 和 Operator 使用 `rest.InClusterConfig` 创建客户端，需要集群内 ServiceAccount，不回退到宿主 kubeconfig。构建原生二进制不消除此要求。Scheduler 和客户端工具在各自依赖已配置时可运行于宿主机；完整系统受支持安装路径仍为 Kubernetes。

`admin-api` 读取 `ADMIN_DSN`。运行进程 Scheduler 和 Operator 共用 `RUNTIME_DSN`；Operator 优先读取 `OPERATOR_DSN`，再回退到 `RUNTIME_DSN`。

> **变量重命名。** `DATABASE_DSN` 和 `SCHEDULER_DSN` 仅分别作为 `ADMIN_DSN` 和 `RUNTIME_DSN` 的旧兼容回退，新部署不得使用。旧 `DISPATCHER_DSN` 和 `WORKER_DSN` 已随 dispatcher、worker 进程移除。

<a id="audit-permissions-and-outcome-recovery"></a>
## 审计权限与结果恢复

<a id="consolidated-local-development-lineage"></a>
<a id="migration-lineage-and-compatibility"></a>
### 迁移序列与兼容性

迁移链包含至 0011 的管理和 Console schema 变更、0012 存储完整性、0013 执行观察、0014 执行日志目录，以及后续执行修复。迁移 0027 增加个人 Console 账户。完整顺序由[迁移目录](https://github.com/s3loy/orbitjob/blob/main/db/migrations/)定义。已记录迁移名称和校验和不可变。

已经记录 `version 7 = storage_integrity` 的开发数据库属于此前仅后端迁移序列，不能通过当前迁移链升级，因为当前 version 7 对应 Console 平台策略读取器，校验和验证必须拒绝该不匹配。保留其备份与回执。只可重建一次性测试数据库；包含所需数据的数据库需要单独审阅的导出恢复或迁移方案。不得编辑 `schema_migrations`、清空校验和或同时应用两个 version-7 文件。

执行观察为 attempt 增加原始 JobRun 归属、不确定元数据和持久停止证据。Unknown 和 CancelUnknown 行保持未完成并被保留。应用 0013 与 CRD 前停止旧写入进程；之后只启动兼容进程。回滚保留这些事实，不能使用将 API 中资源缺失解释为允许重试或释放并发名额的二进制。

Pod 证据保留在现有 attempt 证据字段中保存证明和解除进度。证明、证据缺口隔离与取消完成使用 parent/run/attempt 事务顺序。待解除操作必须在 attempt 推进或历史清理前完成。卸载或降级前排空受保护 Pod 期间，保持 PostgreSQL 与兼容 Operator 可用；见[运维排空流程](/orbitjob/docs/usage/helm-and-kubernetes/#74-upgrade-and-uninstall)。

现有安装必须保留原迁移输入渠道。Chart SQL 文件与 `db/migrations/` 匹配，但现有 ConfigMap 模板使用 YAML `|-`，会移除末尾 LF 字节。迁移程序对实际读取字节计算哈希，因此 Helm 挂载 SQL 和原始文件系统 SQL 即使语句相同，校验和仍不同。Helm 初始化的账本不能与原始目录账本互换。保留已安装 ConfigMap 输入并验证其投影校验和；不得通过恢复 LF 字节、编辑 `schema_migrations` 或更改历史模板来绕过不匹配。即使 SQL 语句未变，此输入渠道差异仍是兼容性约束。

使用迁移身份按顺序应用所选渠道的迁移。迁移 `0012_storage_integrity.up.sql` 撤销应用对审计父表和全部已有分区的 `UPDATE`、`DELETE` 和 `TRUNCATE` 权限。新审计分区必须保留相同权限。离线保留维护使用 owner 维护身份，不使用应用登录身份。

同一迁移增加持久终态结果确认和每 Run SLI 去重事实。未确认终态 Run 保留以供恢复。Run 历史清理后去重事实仍保留；显式租户或 SLI 维护必须先删除这些事实，再删除被引用行。迁移前停止旧 Operator，之后只启动新版本。切换时，已有终态 Run 排除在恢复之外以保留统计；缺失历史投影需要离线审阅。恢复适用于迁移后进入终态的 Run。

启动新版本前停止旧 Run 发布者，因为它们无法安全发现新的完整摘要资源地址。除 Operator 外，还包括 Admin API 和任何旧写入进程。匹配历史资源仍可读，不要求批量重命名。使用返回的资源名称，不重建缩短后缀。

现有安装需要维护切换：备份数据库和配置，停止并确认旧写入进程终止，应用已审阅 CRD 更新，运行新迁移 hook，再启动兼容二进制。Chart 的 pre-upgrade hook 不会代为停止旧 Deployment。安排维护窗口前，查阅[验证记录](/orbitjob/docs/verification/)中的已覆盖 schema 转换、安装检查和恢复测试。

迁移仅向前执行。回滚必须保留增量 schema 和去重事实。只支持短名称的写入进程复用前，需要排空或协调新地址；自动 Helm 回滚无法安全撤销这些数据与兼容性变化。恢复版本必须保留上述资源地址和执行观察要求。

迁移 0012 还为 SLO 强制执行同租户 SLI 引用。升级前，使用 owner 维护身份检查已有行：

```sql
SELECT s.id AS slo_id, s.tenant_id AS slo_tenant, i.tenant_id AS sli_tenant
FROM slos s JOIN slis i ON i.id = s.sli_id
WHERE s.tenant_id <> i.tenant_id;
```

迁移前解决查询返回的任何归属不匹配。新约束拒绝不一致历史；迁移不会静默重新分配或删除历史。

<a id="tls"></a>
## TLS

外部 DSN 保留连接与 TLS 查询参数。派生过程先移除查询凭据和间接登录参数（`user`、`password`、`passfile`、`service`、`servicefile`），再应用目标角色凭据：

```text
postgres://dbadmin:<password>@db.example.com:5432/orbitjob?sslmode=verify-full&sslrootcert=/run/secrets/ca.crt
```

工具使用 `net/url` 将生成凭据编码到派生 DSN。手动提供的 bootstrap URI 本身必须有效。写入输入文件前，对用户名或密码中的保留字符进行百分号编码。

<a id="troubleshooting"></a>
## 故障排查

<a id="local-configuration-is-missing"></a>
### 缺少本地配置

启动程序必须加载生成环境；进程入口不会读取安装 JSON 作为替代。使用安装现有模式和状态路径重新运行 `configure setup`，刷新运行环境。初次设置时提供所需所有者密码或外部 DSN 文件。

<a id="external-authentication-fails"></a>
### 外部认证失败

检查 bootstrap DSN 中的用户、密码、TLS 参数和数据库。工具只打印脱敏端点，不打印密码。

<a id="permission-denied"></a>
### 权限不足

```text
ensure database roles: pq: permission denied to alter role (42501)
```

Bootstrap 账户无法管理 OrbitJob 角色。它需要 `CREATEROLE`，并对非自己创建的角色具有 `ADMIN OPTION`。PostgreSQL 只允许超级用户修改 `SUPERUSER`、`CREATEROLE` 或 `BYPASSRLS` 属性，因此发生属性漂移的角色需要超级用户修复；`owner-init` 会指出角色与属性，不会静默处理。

<a id="switching-databases"></a>
### 切换数据库

普通设置不覆盖已有安装状态。指向其他端点会改变实际数据源，属于单独备份与迁移工作，不是常规配置更改。

<a id="personal-account-migration"></a>
## 个人账户迁移

迁移 `0027_personal_accounts.up.sql` 增加安装状态、个人用户、工作区成员和策略绑定、哈希化单次能力令牌、哈希化服务端会话，以及安装审计流。账户操作使用固定 search_path 的 SECURITY DEFINER 函数，仅授权给 `orbitjob_admin`；Runtime、Operator 和 PUBLIC 不获得账户表或函数访问权限。租户业务数据保留现有 RLS 与显式策略检查。

备份安装、保留迁移输入渠道，并通过匹配 chart 应用完整有序迁移链。创建账户前，部署包含账户端点、Console 和 `/orbitjob-identity` 的 Admin 镜像。配置 HTTPS 或显式回环开发模式，再遵循[首位管理员设置](/orbitjob/docs/accounts/#create-the-first-administrator)。活动 Default 工作区是前置条件。设置消耗操作者签发的一次性能力令牌，并原子创建管理员、显式 Default owner 成员资格和永久首次设置标记。

迁移保留已有 API 密钥及其 bcrypt 验证，不从这些密钥推断用户归属或成员资格。密钥继续服务脚本，不能登录新 Console。个人用户邀请需单独实施。更新源码文件不会更新已有 Admin 部署或其数据库。

此迁移为增量迁移，没有破坏性 down 流程。应用回滚必须保留新 schema 和审计历史，并评估所选二进制是否支持安装账户状态。旧镜像不提供账户登录或恢复。需要 schema 回滚时，恢复协调数据库备份；编辑已记录迁移、清空首次设置标记或删除账户以重开设置均不是恢复流程。管理权限丢失时，使用[操作者恢复](/orbitjob/docs/accounts/#recover-installation-administration)。

<a id="platform-credential-policy-reader"></a>
## 平台凭据策略读取器

迁移 `0007_platform_key_documents` 实现 ADR0011 的有界 `orbitjob_platform_key_documents(text)` SECURITY DEFINER 辅助函数。其所有者为 `orbitjob_table_owner`，将 `search_path` 固定为 `pg_catalog, public`，仅向 `orbitjob_admin` 授予 EXECUTE；PUBLIC、Runtime 和 Operator 不获得访问权限。它读取绑定到一个有效、未过期、未撤销且 tenant 归属为 NULL 的平台密钥的预设文档，以及该密钥配置的预设权限边界。已配置边界为非预设或不可用时拒绝访问。租户策略文档绝不返回，现有表 RLS 不变。

辅助函数不认证 API 密钥。Admin 中间件仅提供它已认证的凭据 ID；HTTP 目标或密钥 ID 选择器不传入读取器。普通租户凭据保留现有 RLS 查询。平台预设 `self` 仍未解析。选择目标租户不会赋予平台凭据该租户的 self 作用域权限。边界仍与授权取交集，显式 Deny 仍优先。

部署 Admin 策略加载路径前先应用迁移。应用回滚保留增量 schema 和辅助函数。移除辅助函数需要在全部调用方停止使用后执行经审阅的正向迁移；down SQL 文件不是运维回滚机制。

<a id="function-management-and-workflow-scheduling-migrations"></a>
## Function 管理与 Workflow 调度迁移

迁移 `0008_function_management` 为 Function 增加 actor/key/payload 回执元数据和租户范围唯一索引，不增加表权限，并保留 Function 现有 RLS。只有名为 TenantAdminAccess 的预设增加 `function:Create` 和 `function:Update`；显式租户策略保留当前 action。创建回执与审计记录同定义一起提交。

迁移 `0009_workflow_scheduling` 为 Workflow 账本增加可空计划触发时间、有界拒绝原因和原始资源 UID。历史计划时间保持 NULL。拒绝行要求原因；计划时间要求 Schedule trigger；绑定资源 UID 唯一。已有表角色与 RLS 不变。部署读取器和写入进程前先应用迁移。应用回滚保留这些字段与回执身份。后续移除需要经审阅的正向迁移和数据、重放兼容方案；不得对安装已记录迁移序列执行 down 文件。

<a id="workspace-directory-metadata-reader"></a>
## 工作区目录元数据读取器

迁移 `0011_workspace_directory` 按 ADR0021 增加仅提供元数据的 `orbitjob_workspace_directory` SECURITY DEFINER 读取器。其所有者为 `orbitjob_table_owner`，固定 search_path，并仅向 `orbitjob_admin` 授予 EXECUTE；PUBLIC、Runtime 和 Operator 不获得执行权限。Admin 应用按现有策略语义计算已认证凭据的精确授权租户范围，并传入函数。SQL 应用该范围及字面名称、slug 搜索，再进行稳定分页。返回列仅包含工作区 ID、名称、slug 和状态，不包含密钥、策略文档或工作负载。

普通租户表 RLS 和旧租户列表不变。部署新目录端点前应用迁移。应用回滚保留辅助函数和现有数据。只有在调用方退役后，才能通过经审阅的正向迁移移除不再使用的辅助函数。

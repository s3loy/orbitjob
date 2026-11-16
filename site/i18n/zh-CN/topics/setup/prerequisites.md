# 安装前提条件

OrbitJob 通过 Kubernetes Job 运行工作负载。完整安装需要 Kubernetes 集群、PostgreSQL、持久执行日志存储，以及来自同一源码版本的全部控制平面镜像。Chart 不会准备 PostgreSQL 服务器、Ingress 控制器、证书签发器或监控栈。

可丢弃的贡献者环境参见[本地开发](../local-deployment.md)。管理员管理的集群应先满足以下要求，再按[安装流程](installation.md)操作。

## 选择安装制品

使用同一版本的 Chart、CustomResourceDefinition（CRD）、迁移文件和组件镜像。Chart 当前声明 `version: 0.3.0` 和 `appVersion: "0.3.0"`；该声明不能证明该版本的镜像已经发布到仓库，也不能证明其中包含当前 Console。

当前源码安装流程构建七个镜像：Admin API、Scheduler、Operator、migrator、bootstrap、日志归档和准入 Webhook。Admin 镜像包含 Console 和 `orbitjob-identity` 管理工具。较早发布的 Admin 镜像可能不提供这些功能。准备安装前，应选择具备所需功能的制品。

## 集群访问

安装管理员需要以下权限：

- 安装和更新 `workloads.orbitjob.io` CRD 及验证 Webhook。
- 创建发行版命名空间和执行命名空间，以及渲染后的 Chart 所需的 ServiceAccount、命名空间内角色、ClusterRole 和绑定。
- 创建 Secret、PersistentVolumeClaim、Deployment、StatefulSet、DaemonSet、Service 和 NetworkPolicy。
- 检查 Pod 事件、日志、Job、工作负载 CR、PVC 和滚动发布状态。

使用专用 kubeconfig 或显式 kubectl 上下文。任何写入前都应检查上下文。Chart 使用固定组件名称，因此每个发行版命名空间只能部署一个 OrbitJob 发行版。仅使用不同发行版名称无法隔离这些名称。

仓库未定义支持的 Kubernetes 版本范围。本地安装预检要求 kubectl 的主版本和次版本与集群一致。本流程应使用与集群匹配的 kubectl 次版本；采用其他集群版本前，应在隔离环境中完成适用性验证。

## 工具

在选定源码检出的根目录运行仓库命令。

| 工具 | 要求 | 用途 |
|---|---|---|
| Go | 1.27.3+ | 数据库配置和源码工具 |
| Docker | 支持 BuildKit 和仓库推送 | 构建当前源码镜像 |
| kubectl | 本流程要求与目标集群的主版本和次版本一致 | 安装与诊断 |
| Helm | Helm 4；仓库 CI 使用 4.2.4 | 渲染和发行版钩子 |
| Bash 和 GNU make | 与仓库 shell 脚本和 Makefile 兼容 | 本地辅助命令 |
| OpenSSL | 支持 `rand -hex` 和证书命令 | 凭据和本地测试夹具证书 |
| Python | Python 3 | Secret 清单和诊断工具 |
| curl | 支持 `--fail-with-body` | API 检查和负载工具 |
| PostgreSQL 客户端 | 与服务器兼容的 `psql`、`pg_dump`、`pg_restore`、`pg_dumpall` | 数据库检查与备份 |

主机上的 Console 开发需要 npm，以及工具链支持的 Node.js 版本：22.x 中的 22.13.0 及后续版本、24.x，或 26.0.0 及后续版本。Docker 构建自行提供 Node 运行时。kind 仅用于本地集群和隔离安装检查；托管工作流固定使用 kind 0.32.0。这些 CI 固定版本用于保证可复现性，不代表经过测试的兼容范围。

## PostgreSQL

提供一个已有 PostgreSQL 数据库，配置机器和集群 Pod 都必须能够访问它。仓库本地清单和集成工作流使用 PostgreSQL 17；使用其他服务器版本或托管服务策略前，应完成适用性验证。

引导账户需要 `LOGIN`、`CREATEROLE`、数据库所有权，以及在 `public` 中安装 `pgcrypto` 的权限。对于其他账户创建的已有 OrbitJob 角色，还需要其 `ADMIN OPTION`。应用组件使用各自独立的受限角色凭据。

受管角色名称固定，PostgreSQL 角色属于整个服务器。不同数据库不能隔离各自独立生成的角色密码。在同一服务器创建多个安装前，应预留专用服务器，或由数据库管理人员安排共享角色凭据。

选择一种数据库方案：

- 对于本地 kind 开发，`make kind-db` 部署仓库中的 PostgreSQL 17 清单，使用 5-GiB 卷申请，并准备安装 Secret。固定本地凭据和单副本属于开发设置。
- 对于外部管理的服务器，应在运行数据库设置前准备数据库、引导账户、连通性、TLS 和备份。
- `configure setup --mode bundled` 为已准备的服务器生成凭据，不会创建 PostgreSQL 服务器，也不会连接服务器。

具体角色、Secret、TLS 和迁移要求参见[数据库配置](../database-setup.md)。当前 Chart 无法为 Pod 添加任意 PostgreSQL CA 或客户端证书挂载。引用此类文件的 DSN 需要单独维护部署定制，且所有数据库使用方（包括钩子）都必须能够获得这些文件。

## 存储与执行证据

提供满足归档 ReadWriteOnce 卷申请的 StorageClass 或预先准备的存储。默认单独申请一个 10-GiB PVC。归档检查声明容量和分配预算；卷申请绑定成功本身不能证明容量可用。数据库存储独立于归档存储。

Fluent Bit DaemonSet 读取节点 CRI 日志，并将偏移量和排队数据块持久保存在节点 `hostPath`。默认开发配置包含 128-MiB 队列，并声明 256-MiB 目录容量。该声明不会创建文件系统配额。生产采集器设置要求每个符合条件的节点都具有经过管理员验证的物理目录配额，且至少为配置队列大小加 64 MiB。备份和节点替换计划应包含偏移量和数据块。临时采集器配置在重启时会丢失待采集状态。

不能将仅有 PostgreSQL 的备份视为完整备份。保留执行输出位于归档 PVC；采集器偏移量和未发送输出位于各节点。参见[备份与恢复](../operations/backup-and-recovery.md)。

## 网络与证书

CNI 必须执行预期的 NetworkPolicy。Chart 可以要求管理员声明，但无法测试执行效果。设置 `executionNetwork.enforcementVerified: true` 前，应测试允许和拒绝的流量。声明每个业务目标的 CIDR 和端口；仅允许 DNS 不会放行业务流量。已有历史工作负载策略可能不同于当前受限配置。

Admin Service 默认为 `ClusterIP`。浏览器从回环地址以外访问时，应提供 HTTPS origin。Ingress 需要已有控制器，并且在启用 TLS 时，需要发行版命名空间内的 `kubernetes.io/tls` Secret。配置的 Ingress 主机和 `adminapi.console.publicOrigin` 必须描述同一个公开 origin。Chart 不签发公开证书。NodePort 和 LoadBalancer 会暴露完整 HTTP 监听器，包括运维路由。

Chart 会另外创建内部准入和执行日志 TLS Secret。它们不保护公开 Console 流量。恢复时应保留完整日志证书组；轮换时应协调其使用方。准入和日志服务证书生成时的有效期为一年，升级会复用已有 Secret。应在到期前安排证书检查和替换；普通升级不是自动续期流程。

## 容量与可用性

本地组合配置至少需要 4 vCPU、8 GiB 内存，以及初始 40 GiB 可用底层存储。Docker Desktop 虚拟机内的文件系统和存放其虚拟磁盘的主机文件系统都必须满足可用空间检查。这些是本地前提条件，不是生产容量规划或吞吐量保证。负载测试另有 CPU 要求，参见[负载测试指南](../loadtest-guide.md)。

Chart 默认各运行一个 Admin API、Scheduler、Operator 和归档副本；准入默认使用两个副本。共享数据库凭据和多个 Pod 不能证明具备高可用性。Scheduler 选举，以及数据库、存储和网络故障转移，需要单独经过验证的设计。选择可用性目标前，应阅读[已知限制](../known-issues.md)。

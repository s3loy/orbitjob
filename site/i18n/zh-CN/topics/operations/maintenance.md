# 本地安装维护

为需要保留的 `orbitjob-dev` 安装准备源码升级、经过验证的执行网络和具有物理容量边界的采集器存储。保留集群、CNI、命名空间映射、资源身份、凭据和迁移输入通道。本流程需要维护窗口，以及 Docker 主机、kind 节点、Kubernetes 安装和数据库的管理员权限。

通用的提交暂停、排空、写入方切换和恢复流程参见[升级](operations/upgrading.md)，协调数据保护参见[备份与恢复](operations/backup-and-recovery.md)。本页补充本地节点与存储操作，不构成生产多节点存储或可用性设计。

## 历史检查与当前前提条件

日期为 **2026-11-15** 的维护记录描述了一个 Kubernetes v1.37.0 kind 节点、`kindest/kindnetd:v20260820-69b56db7`、PostgreSQL 17、5-GiB 数据库卷申请和 10-GiB 归档卷申请。记录中的采集器状态位于 `/var/lib/orbitjob-collectors/orbitjob-system/orbitjob`，且主机可用空间未满足 40-GiB 前提条件。这些是带日期的观测，不是当前清单，也不代表维护验收已完成。任何停机前都应重新检查。

当前本地预检要求至少 4 vCPU、8 GiB Docker 内存和 Docker 文件系统中 40 GiB 可用空间；对于 Docker Desktop，主机底层文件系统也需要 40 GiB 可用空间。它还要求 kubectl 与集群主版本和次版本匹配，且活动数据库卷申请至少为 5 GiB：

```bash
bash scripts/preflight-local.sh
bash scripts/preflight-local.sh --cluster
```

安排停机前解决失败项。不要因虚拟机内显示较大的文件系统容量，就推断主机容量充足。如果其他随附客户端较旧，应在 PATH 中保留经过校验和验证且与集群匹配的 kubectl。环境通过预检后，仍需要完成以下实际排空、存储和执行检查。

维护窗口前，完成源码、覆盖率、PostgreSQL 和 Helm 检查，构建并导入全部七个候选镜像并记录摘要。将当前 Helm values 和清单、组件规格和副本数、CRD、工作负载 CR、来源控制、Secret、PVC/PV 身份和原始迁移 ConfigMap 保存到私有存储。包含手动更改的镜像身份和采集器节点选择器。构建和下载应在停机窗口外完成。

此前本地计划预留 90 分钟：15 分钟用于暂停和排空，20 分钟用于备份，25 分钟用于网络与文件系统操作，20 分钟用于迁移和验证，10 分钟用于恢复提交或作出恢复决策。这是运维规划预算，不是测得的停机时间。应根据当前安装和经过测试的恢复时间设置实际窗口与恢复决策点。备份或文件系统不可读可能需要更长恢复时间。

## 暂停、排空并停止写入方

停止提交客户端，并遵循[排空流程](operations/upgrading.md#stop-submissions-and-drain)。保留每个来源的租户、UID、CAS 版本，以及原始调度和执行暂停值。先暂停调度，同时让原本启用的执行排空。已有执行暂停保持不变。如果它阻止 Workflow 排空，应停止该来源的维护窗口，并取得所有者决定。

对于尚不对 Check 执行来源调度暂停的旧镜像，还应使用独立定义版本和 `check:Pause` 权限暂停活动 Check 定义。保留原本已暂停的 Check。统计迟到接受的调度项和已接受但未发布运行；轮询时间经过不能证明旧 Check 批次已完成。

已接受工作完成排空后，设置执行暂停，并再次检查迟到工作。保存已确认控制版本。409、身份变化或写入不明确时需要审查，不能覆盖其他管理员状态。

停止 Admin API、Scheduler 和 Operator，并验证旧 Pod 已终止。让采集器向归档完成发送，检查偏移量和数据块，然后应用已记录的采集器调度暂停措施，并等待采集器 Pod 停止。最后停止归档。保持 PostgreSQL 可用以便备份。旧写入方停止后，再次检查台账、原始调度项、物理执行和证据终结器。任何迟到非终态运行、CR 缺失或执行不确定性都意味着排空失败。

创建并验证[协调备份](operations/backup-and-recovery.md)。将实际挂载数据库卷申请、归档数据和已停止采集器状态纳入同一个恢复边界。不要复制已废弃的较小数据库卷申请来替代挂载中的卷申请。就绪状态不能证明采集器队列为空或归档可恢复。

## 验证已有网络提供方

考虑更换提供方前，先测试已安装 CNI。版本字符串或 `NetworkPolicy` 对象不能证明流量限制已执行。已有探针创建唯一命名的命名空间、三个非 root Pod 和 Chart 渲染的策略，然后检查基线可达性、DNS、拒绝的入站和出站流量、允许的 CIDR 与端口，以及拒绝的第二端口：

```bash
python3 scripts/verify-execution-network.py \
  --kubeconfig /absolute/path/to/maintenance.kubeconfig \
  --context kind-orbitjob-dev \
  --kubectl /absolute/path/to/matching-kubectl \
  > /absolute/private/maintenance/network-verification.json
```

从仓库根目录运行，并确保 Python 3 和 Helm 可用。节点必须能够获取探针镜像；使用 `--probe-image` 选择已审查且可用、包含所需 BusyBox 命令的镜像。必须得到 `status: PASS`、全部检查通过和 `cleanup: complete`。保留实际探针镜像 ID 和策略哈希。API 或 exec 错误、目标不可用，均不能证明连接被拒绝。

还应在真实受管命名空间中测试预期应用目标。在安装 values 中为每个命名空间配置所需目标 CIDR 和端口。DNS 权限不授予业务访问。已有 policy-v0 兼容工作负载可能不带 restricted-v1 选择器。仅在检查后设置 `executionNetwork.enforcementVerified: true`。CNI、节点、策略或目标变化后重新验证。失败时保留已有 CNI 和集群；安装第二个策略引擎属于独立变更。

## 限制采集器文件系统容量

对于此单节点本地配置，可以使用专用 256-MiB ext4 镜像限制采集器状态目录。Chart 的 `hostPath` 和 `quotaBytes` 值不会创建该限制。挂载后的可用容量必须容纳 128-MiB 队列、至少 64 MiB 余量和保留状态。由于文件系统元数据占用，256-MiB 镜像的可用容量小于 256 MiB；必须测量。

开始前，验证 kind 节点支持 loop 挂载、ext4 工具、`findmnt`、`df`、`cp`、`sync` 和 systemd。确保镜像父路径位于节点持久文件系统。不要将镜像放在自身挂载点内。保留原始目录完整的停止状态备份。

节点管理员在采集器停止期间执行以下步骤：

1. 在采集器目录外创建新的唯一命名镜像文件，并仅格式化一次为 ext4。拒绝覆盖已有镜像。
2. 将镜像挂载到临时暂存目录。使用 `findmnt` 和 `df` 检查文件系统身份、可用容量和可用空间。
3. 复制已停止的原始采集器目录，保留所有权和权限。包含 `tail.db`、相关文件和全部排队数据块。比较完整文件清单与校验和，然后执行 `sync`。
4. 卸载暂存目录。将原始目录作为独立回滚副本保留，并将同一镜像挂载到原始采集器路径。重新检查文件系统身份、文件所有权和校验和。

对于历史路径，节点本地 systemd 单元形式如下：

```ini
[Unit]
Description=Bounded OrbitJob collector filesystem
Before=kubelet.service

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/usr/bin/mount -t ext4 -o loop,noatime /var/lib/orbitjob-collector-volumes/orbitjob-system-orbitjob.ext4 /var/lib/orbitjob-collectors/orbitjob-system/orbitjob
ExecStop=/usr/bin/umount /var/lib/orbitjob-collectors/orbitjob-system/orbitjob

[Install]
RequiredBy=kubelet.service
```

替换为实际节点路径，安装已审查单元，运行 `systemctl daemon-reload`，启用单元，并确认 kubelet 的必需依赖。挂载失败时，该依赖关系必须阻止 kubelet 启动；采集器不能在重启后回退到无界目录。绝不能在重启单元中执行文件系统格式化。

保持采集器停止，仅在此文件系统内使用一个新临时文件执行有界 ENOSPC 测试。仅删除该测试文件，并重新检查保留状态。在维护窗口内重启节点，验证挂载先于 kubelet 和采集器使用，再次比较保留文件。一次挂载命令成功不能证明重启持久性。

物理验证后，设置 `logs.collectorPersistence.quotaVerified: true`，并记录与物理上限一致的 `quotaBytes`。重建 kind 节点时，仍需从独立备份恢复镜像和其他安装数据。不要将此文件系统方案视为经过验证的生产节点替换流程。

## 启动与验证候选版本

在旧发布方保持停止时，应用已审查 CRD 和匹配的有序迁移输入。当前迁移链以 0028 审计资源标识结束；保留此前所有 SQL 字节和已安装 Helm 输入通道。账户设置需要匹配的 Admin 镜像和 origin 设置。

如果本维护窗口要求在任何应用启动前检查迁移，应准备并审查清单转换，使应用副本在 Helm 应用期间保持为零，并保留采集器调度暂停措施。当前 Chart 没有通用副本保持值，默认升级会在 pre-upgrade 钩子之后启动工作负载。不要假定手动缩容到零能够在下次 Helm 应用后保持。

迁移验证后，启动归档、恢复保存的采集器选择器，然后按预期的已保存配置启动兼容 Operator、Scheduler、准入和 Admin API。初始检查期间保持来源执行暂停。验证：

- 镜像、凭据、迁移校验和、领导权和组件就绪状态。
- 历史来源、运行和 Attempt 身份，以及账户和 API 授权。
- 采集器挂载和容量、mTLS 摄取，以及重启后的保留读取。
- 完整且最新的隔离清单、没有未解决证据义务，以及 SLI 维护没有计划缺失或重复计数错误。
- 一个启用的测试来源完成验证和保存、投影、触发、终态台账结果、Attempt 和日志；检查重放、取消和目标地址。
- 健康的[监控目标与规则](operations/monitoring.md)；需要 `kubectl top` 时，单独检查 Metrics API 可用性。

恢复来源暂停值前，应将 UID、版本和两个值与最后一次已确认维护写入比较。使用已验证 CAS 版本。单独恢复 Check 定义，使用完全一致的已确认定义版本和 `check:Resume`；创建时间和身份必须仍然匹配。旧审计操作者或游标时间戳不能证明暂停由本次维护产生。实际 API、Operator、数据库和日志路径成功后，才恢复客户端。

## 失败与恢复

身份、存储、迁移或执行检查失败时保持提交暂停。回滚采集器挂载时，停止采集，并对其当前完整偏移量和数据块创建快照。从该已停止快照填充空回滚目录，保留所有权和权限，再验证校验和，之后才卸载并恢复原始路径。将较早的维护前副本单独保留。将其中偏移量与新数据块混合可能造成采集丢失或重复。镜像不可读时，应保留镜像并继续停止采集，等待明确恢复。

仅在受控文件系统回滚中移除 kubelet 挂载依赖，并在恢复采集前验证恢复路径。只有已保存组件规格和镜像仍与模式及执行事实兼容，才可恢复。不要仅为更换二进制而恢复旧数据库。需要数据恢复时，停止全部写入方，并一并恢复协调的 PostgreSQL、归档和采集器状态。

保持 PVC、来源身份和证据终结器完整。本地维护窗口失败不能作为替换正常运行的集群或删除未知执行的理由。完整已发布版本升级、已安装故障恢复、完整恢复和生产吞吐量及高可用性，都需要各自限定范围的验收证据。

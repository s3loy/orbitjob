---
title: "本地安装维护"
---

本流程为现有 `orbitjob-dev` 安装准备源码升级、经验证的执行网络和有界采集器存储。它保留集群、CNI、命名空间、资源 UID、凭据和迁移输入渠道。实施需要预定维护窗口。准备和一次性集群测试不会更改正在使用的安装。

<a id="scope-and-prerequisites"></a>
以下清单和准备阶段发现来自 2026-11-15 检查。

## 范围与前置条件

已审阅安装使用一个 Kubernetes v1.37.0 kind 节点、`kindest/kindnetd:v20260820-69b56db7`、PostgreSQL 17、5-GiB 数据库 PVC 和 10-GiB 归档 PVC。采集器使用节点上的 `/var/lib/orbitjob-collectors/orbitjob-system/orbitjob`。维护开始前再次检查这些值；它们是该安装的清单，不是所有环境的 chart 默认值。

此安装使用 kubectl 1.37.x。Docker 附带客户端较旧时，在维护 shell 的 PATH 中使用单独验证过校验和的二进制。[本地预检](/orbitjob/docs/deployment/prerequisites/)要求 4 vCPU、8 GiB Docker 内存，以及 Docker 文件系统和宿主后端文件系统各 40 GiB 空闲空间。不能从容量较大的来宾文件系统推断宿主容量。

停机前完成以下准备：

1. 通过当前源码的 Go、覆盖率、PostgreSQL 和 Helm 检查。在停止服务前构建并导入候选镜像。记录源码输入哈希、镜像摘要和当前运行镜像身份，包括绕过 Helm 更改的镜像。不得仅按可变本地 tag 选择镜像。
2. 将当前 Helm values 与 manifest、Deployment/StatefulSet/DaemonSet spec、CRD、工作负载 CR、命名空间映射、来源控制和 PVC/PV 身份保存在私有目录。保留实际副本数和采集器 node selector。报告和 shell tracing 不得包含凭据或原始执行输出。
3. 按命名空间、CIDR 和端口列出任务所需目标。DNS 权限不授权应用流量。单独审阅 policy-v0 兼容路径；restricted-v1 选择器不会追溯保护每个历史工作负载。
4. 检查每个已配置执行命名空间中的非终态 Run、保留证据 finalizer 和 unknown 执行。确认剩余采集队列能放入目标文件系统。缺少物理停止证据时中止窗口；不得强制删除证据 finalizer 或释放名额。
5. 预留维护窗口并明确谁可以恢复提交。镜像构建、下载和磁盘空间处理在停机前完成。此流程尚无经过测量的停机时间保证。

对已审阅开发安装，在全部准备通过后预留 90 分钟窗口。这是操作者时间预算，不是实测服务停机时长。以停止客户端和 Admin API 写流量作为停机起点；只有完成最终启用来源检查并受控恢复客户端后，停机才结束。

| 阶段 | 预留时间 | 中止条件 |
|---|---|---|
| 暂停提交并排空 | 15 分钟 | 存在未解决 Run、unknown 执行或冲突的控制写入 |
| 停止写入进程并验证协调备份 | 20 分钟 | 队列清单不完整、备份不可读或校验和不匹配 |
| 验证网络并配置采集器文件系统 | 25 分钟 | 流量结果不匹配、容量不足、重启或挂载依赖失败 |
| 迁移、启动并检查候选版本 | 20 分钟 | 迁移、身份、历史数据、日志或测试来源验证失败 |
| 恢复提交或开始回滚 | 10 分钟 | 无法确认来源归属，或剩余窗口不足以恢复 |

达到阶段预算时，继续保持暂停提交，并选择回滚或明确延长窗口。备份或文件系统损坏时，恢复可能超过预留时间。当前工作站宿主可用空间低于 40-GiB 前置要求；安排窗口前先解决空间问题，不得删除安装数据或无关服务。

<a id="stop-submissions-drain-and-back-up"></a>
## 停止提交、排空与备份

停止 trigger/invoke 客户端。对每个 Job、Workflow、Function 和 Check，读取并保存 `GET /api/v1/<kind>/<id>/execution-controls` 响应，再使用当前 CAS 版本通过 `PATCH` 暂停其调度。只有原先允许执行时，排空已有工作期间才保持执行启用；已有的执行暂停必须保留。此暂停阻止 DAG 排空时，停止该来源的准备，并由其所有者决定。每次 PATCH 包含 `version`、`schedule_paused` 和保留的 `execution_paused` 值。读取需要资源的 `Get` 授权；写入还需要独立 `ManageExecution` 授权。409 响应通过重新读取和归属比较解决，不得覆盖其他管理员介入后的更改。

对尚未对 Check 触发执行来源调度暂停的旧镜像，排空前还需暂停每个 active Check 定义。保存 `GET /api/v1/checks/<id>`，使用该定义读取结果中的版本，向 `POST /api/v1/checks/<id>/pause` 仅提交 `{"version":N}`。保存租户、ID、创建时间、原始状态与版本，以及明确收到的暂停成功响应。这些请求分别需要 `check:Get` 和 `check:Pause`，独立于 `ManageExecution`。使用覆盖目标租户与资源组的已有凭据，并先验证运行镜像的路由和租户选择契约。原本已暂停的定义和各来源已有的执行暂停保持原状。

定义暂停会排除后续定时扫描；已经读取定义的批次仍可能接受一个触发实例。将这些迟到接受的实例纳入排空清单。旧循环没有固定批次完成期限，因此等待几个 interval 不能证明完成。定义暂停也可能停止旧 Check 专属逻辑对已接受但尚未发布执行的修复。已接受账本行缺少对应 JobRun，或仍未解决时，中止排空；不得为尝试修复而临时恢复 Check。

所有已接受工作稳定结束后，再设置执行暂停。确认从排空检查到暂停之间没有接受新提交。保存更新后的控制版本，供后续比较。来源控制不等于删除定义，不得设置 tombstone。

停止 Admin API、Scheduler 和 Operator，并保存其副本数用于恢复。让采集器将队列写入归档，检查队列与归档状态，再通过临时且已记录的 node selector 保留设置停止采集器。只有采集停止后才能停止归档。为数据库导出保留 PostgreSQL 运行。仅采集器就绪响应不能证明队列已空。

停止旧写入进程后，再次核对账本、原始触发实例和物理执行清单。即使之前检查为空，迟到的非终态 Run、缺失 CR 或 unknown 执行仍使排空阶段失败。

这些写入进程停止后创建协调备份：

- 数据库自定义格式 dump、角色定义，以及迁移账本和校验和。
- 归档 PVC 内容和采集器状态，包括 `tail.db`、相关文件和排队 chunk。根据实际 PV 确定归档路径；不得误把已废弃的 1-GiB 数据库 PVC 当作当前挂载的 5-GiB PVC 复制。
- 安装 Secret、当前组件 spec、来源控制清单、CRD、工作负载 CR 和原始 Helm 迁移 ConfigMap。

使用 `umask 077`、权限 0700 的备份目录和权限 0600 的敏感文件。通过容器环境或已有受保护凭据渠道向 dump 提供密码，不得放入命令参数或报告。验证 dump 列表与文件校验和时不输出其内容。恢复后的安装和保留日志检查完成前，保留维护前备份。

<a id="verify-the-existing-network-provider"></a>
## 验证现有网络提供程序

参考 kindnetd 镜像包含 NetworkPolicy 执行能力。更换提供程序前，先通过真实流量证明该能力。建议变更不包括安装第二个策略引擎，此流程也不要求更换 CNI 或 Pod CIDR。

验证器创建并删除唯一命名的命名空间、三个非 root 探针 Pod，以及 chart 渲染的 NetworkPolicy。它检查基线连通性、DNS、被拒绝的入站与出站、允许的 CIDR 与端口，以及被拒绝的第二端口：

```bash
python3 scripts/verify-execution-network.py \
  --kubeconfig /absolute/path/to/maintenance.kubeconfig \
  --context kind-orbitjob-dev \
  --kubectl /absolute/path/to/kubectl \
  > /private/maintenance/network-verification.json
```

必须得到 `status: PASS`、全部流量检查通过和 `cleanup: complete`。API 错误、exec 失败或目标不可用不能当作连接被拒绝的证明。保留实际探针镜像 ID 和渲染策略哈希。

在安装 values 中审阅并应用显式工作负载出站规则。除隔离探针命名空间外，还要测试实际托管命名空间和预期工作负载目标。完成后才能设置 `executionNetwork.enforcementVerified: true`。CNI、节点、策略 values 或目标规则改变后重新验证。验证失败时保留 CNI 和当前集群。

<a id="bound-the-collector-filesystem"></a>
## 限制采集器文件系统

此单节点 kind 配置使用专用 256-MiB ext4 镜像，挂载到采集器状态目录。文件系统为该目录提供物理上限；仅声明 `hostPath` 不提供此限制。可用容量必须容纳 `collectorBuffer`、64 MiB 余量和保留状态。256-MiB 镜像扣除文件系统元数据后可用容量小于 256 MiB，因此复制数据前检查实际容量与空闲空间。

采集器停止后，保留原目录作为回滚副本。在该目录外创建镜像，只格式化一次，挂载到暂存路径，再保留属主和权限复制原状态。检查内容校验和、同步写入、卸载暂存挂载，再将同一镜像挂载到原采集器路径。重启时绝不能格式化已有镜像。

在 kind 节点内通过 systemd oneshot 服务持久配置挂载。将模板中的路径替换为该安装路径：

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

执行 `systemctl daemon-reload` 后启用 unit。挂载失败时，kubelet 依赖必须拒绝继续；节点重启后，采集器绝不能回退到无界目录。恢复服务前，通过 `findmnt` 和 `df` 验证文件系统，在临时文件中执行有界 `ENOSPC` 测试，仅删除该测试文件，并确认保留状态仍可读。在维护窗口内重启节点，验证挂载先于 kubelet 和采集器使用，再次检查保留文件。

将镜像保存在节点的持久 kind 文件系统中。重建或删除 kind 节点仍需从备份恢复此镜像及其他安装数据。该本地配置不是多节点生产存储设计。物理验证后设置 `logs.collectorPersistence.quotaVerified: true`，并使 `quotaBytes` 与已配置上限一致。

<a id="start-the-candidate-and-verify"></a>
## 启动候选版本并验证

保持旧写入进程停止，单独应用已审阅 CRD，再部署已审阅 values 和固定候选镜像身份。渲染部署中必须保持应用副本数为零，并保留采集器停止设置；默认 Helm values 不得在切换检查前重启写入进程。启动任何组件前，验证迁移 hook 结果和实际迁移账本。可靠性变更没有增加 0026 之后的迁移。本次候选还包含个人账户迁移 0027，应将此已审阅正向迁移及其身份和设置兼容性检查纳入协调切换。保留全部早期 SQL 字节和该安装的 Helm 挂载迁移输入渠道。

启动归档，恢复采集器原 node selector，再以已验证候选镜像按保存副本数启动 Operator、Scheduler 和 Admin API。初始检查期间保持来源执行暂停。保留现有 Secret 和迁移校验和；Helm 回滚不等于数据库降级。

允许新工作前验证以下内容：

1. 组件就绪、Lease 续约、预期镜像身份，以及未变的凭据、来源 UID 和历史执行读取。
2. 采集器挂载与容量、mTLS 摄入，以及归档和采集器重启后的保留归档读取。
3. 当前执行隔离指标、观察时间戳，以及不存在尚未解决的证据义务。同一安装的副本使用 `max`，不得累加副本快照。
4. SLI 维护扫描与保留原因，不存在缺失计划或重复计数错误。其租户范围为 Operator 映射，包括仍在映射中的挂起租户。
5. 一个有意启用的测试来源：验证和保存、触发、正确最终账本阶段、attempt 身份、保留输出，以及精确允许和拒绝的网络目标。幂等重放与取消单独验证。

恢复来源保存的暂停状态前，将其当前来源 UID、版本和两个暂停值与最后一次已确认维护写入比较。身份变化、版本被其他操作推进或状态不符合预期时，必须由管理员审阅。仅重新读取不授权覆盖并发更改。完成比较后，使用已验证 CAS 版本恢复保存值。写入响应不明确时，将该操作视为尚未解决；后续读取值相等不能证明该暂停属于本次维护。只有真实 API、Operator、数据库和日志链路成功后，才恢复客户端。

单独恢复 Check 定义状态。仅恢复本次维护从 active 改为 paused 的定义，并先确认租户、ID、创建时间、`paused` 状态和明确确认的定义版本均一致。使用独立 `check:Resume` 权限，向 `POST /api/v1/checks/<id>/resume` 提交 `{"version":P}`。定义版本与来源控制版本分别记录。游标变更可以更新时间戳而不推进定义版本；旧状态审计中的 actor 也不能标识维护调用者。身份或版本变化、响应不明确时，需进行审阅；原本已暂停的 Check 保持暂停。

应用现有监控栈的维护 manifest 后，它可采集新增指标。`kubectl top` 还需要可用的 Metrics API；应用就绪不证明资源指标可用。按已记录可观测性流程验证采集目标与规则健康。

<a id="failure-and-rollback"></a>
## 失败与回滚

任何身份、存储、执行或证据检查失败时，继续保持暂停提交。挂载失败时，停止采集器并对其当前状态创建完整快照，包括 `tail.db`、相关文件和每个排队 chunk。使用该停止后的快照填充空的回滚目录，并保留属主与权限。先验证校验和，再卸载并将其放回原路径。单独保留维护前副本；不要将旧 offset 与当前 chunk 合并，也不要恢复使用过期 `tail.db`。只有在此受控回滚中才移除 kubelet 依赖，启动采集器前先验证恢复路径。当前文件系统不可读时，保留镜像，并保持采集停止以进行明确恢复。

候选版本失败时，恢复保存的组件 spec 和镜像摘要。未发生 schema 迁移时，不要仅为更换二进制而恢复旧数据库。如果必须恢复数据，停止全部写入进程，一并恢复协调备份中的数据库、归档和采集器。恢复提交前验证凭据、历史和保留输出。不得通过删除 PVC、改写迁移校验和、移除证据 finalizer 或替换当前集群来简化恢复。

完整已发布版本升级资格验证、已安装故障恢复套件，以及生产吞吐或高可用验证，均独立于此本地维护窗口。

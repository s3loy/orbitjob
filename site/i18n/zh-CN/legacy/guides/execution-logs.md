---
title: "执行日志"
---

Console 的 **Execution logs** 面板跨全部 attempt 跟随保留证据。平台事件与 stdout/stderr 分开显示，支持时间戳、attempt 选择、阶段筛选、搜索、暂停与跟随、复制和下载。命令证据包含已审阅 image/argv、实际输出和观察到的退出码或信号。容器内部 shell 命令只有在工作负载自行输出或追踪时才会出现。

<a id="installation-and-capacity"></a>
## 安装与容量

按[开发部署指南](/orbitjob/docs/deployment/prerequisites/)使用 kind + Helm。开发安装至少向 Docker/Kubernetes 分配 **4 vCPU、8 GiB 内存和 40 GiB 初始空闲后端存储**，其中包含 5-GiB 数据库卷和独立 10-GiB 日志归档卷。这是支持资源下限，不保证吞吐或并发。工具与镜像版本由仓库 manifest 定义。运行 `bash scripts/preflight-local.sh --cluster` 检查现有 kind 安装。

现有 kind 数据库 PVC 低于 5 GiB 时需要迁移。kind 专用 `bash scripts/upgrade-local-db-volume.sh` 停止应用写入进程，将数据库离线复制到新 PVC，并保留旧 PVC 用于回滚。重要安装在存储维护前先备份。恢复检查完成前不得删除保留的 PVC。

Helm 启用 Fluent Bit 节点采集器和一个 `orbitjob-log-archive` StatefulSet。归档服务在绑定来源前验证 Pod/Job/JobRun UID 和账本固定的 attempt。它将 gzip 段、有界 manifest 和生命周期事件写入独立 PVC；PostgreSQL 保存紧凑的租户范围身份目录与清理请求。归档服务使用受信任的 Runtime 数据库身份。Admin API 使用独立 Admin 身份和内部读取器 mTLS 证书。

通过 `charts/orbitjob/values.yaml` 配置归档限制；`internal/logarchive` 实现以下默认限制：

| 预算 | 行为 |
|---|---|
| 一次逻辑执行 | 重试、Pod、重启和输出流共用 32 MiB 未压缩 stdout/stderr |
| 超出执行上限后的窗口 | 以有界段保留前 8 MiB 和最近 24 MiB；省略区间可见 |
| Workflow | 所有子执行合计 128 MiB；各子执行还受自身上限约束 |
| 租户 | 2 GiB 保留的未压缩输出 |
| 安装归档 | 10-GiB PVC 内计入预算的文件系统分配最多 7 GiB，另保留空闲余量 |
| 生命周期与 manifest | 1,024 个事件、64 KiB 事件文本；256-KiB manifest；超出的事实以可见缺口标记 |
| 重放元数据 | 所有已接受的确定性来源指纹保留在有界 manifest 中；耗尽时拒绝新输出并持久记录缺失标记 |
| 目录 | 128-MiB 逻辑身份预算和 256-MiB 物理表、索引、TOAST 准入保护 |

卷准入采用配置限制、实际 PVC 请求和文件系统余量中的较小值。不会因为 kind hostPath 文件系统较大而推定容量无限，也不会自动扩卷。磁盘保护覆盖受管理的归档写入；其他进程和存储故障仍可能耗尽共享后端设备。应监控归档、采集器指标与数据库大小。

<a id="collector-state-and-node-capacity"></a>
### 采集器状态与节点容量

默认采集器将 tail 数据库和文件系统 chunk 保存在各节点的 `/var/lib/orbitjob-collectors/<namespace>/<release>`。命名空间和 Helm release 隔离不同安装。同一节点上替换采集器 Pod 会保留此目录；替换或丢失节点则不会保留。数据库与归档分别提交。重放记录通过已持久化来源指纹去重，不依赖采集器生成的请求 ID。

`logs.collectorBuffer` 限制 Fluent Bit 输出队列，初始值 128 MiB。它不限制宿主目录、SQLite 数据库或其他节点文件的物理容量。使用生产采集器配置前，在每个执行节点配置并监控目录配额。例如为默认队列配置 256-MiB 配额后设置：

```yaml
logs:
  collectorPersistence:
    mode: hostPath
    profile: production
    hostPathBase: /var/lib/orbitjob-collectors
    quotaVerified: true
    quotaBytes: 268435456
```

`quotaVerified` 是管理员声明。Helm 不创建节点配额，也不独立测量配额。生产渲染拒绝临时目录、缺少声明，或声明配额小于配置队列加 64 MiB 元数据余量的情况。开发配置在 Helm notes 和 Pod 注解中报告未验证配额。显式设置 `logs.collectorPersistence.mode=ephemeral` 会为一次性开发使用带大小限制的 `emptyDir`；Pod 替换时丢失 offset 与未导出 chunk。常规升级不得删除保留的节点目录。

每个来源文件有独立采集器 tag 和 chunk 路由，将永久无效的来源批次与无关文件分离。队列溢出仍可能丢弃最旧排队 chunk。超大物理行会被跳过，使后续行可继续处理；应检查采集器警告和丢弃记录指标。节点丢失、队列淘汰、采集前来源轮转、本地数据库或文件系统故障均可能丢失输出。这些丢失不改变任务业务结果。无法确定缺失范围时，来源覆盖完整性仍未验证。

<a id="ingestion-and-replay"></a>
### 摄入与重放

内部端点在追加输出前检查完整 JSON-lines 批次及其结构限制：最多 8 MiB、20,000 条记录和 256 个来源文件。追加任何输出分组前先完成来源验证。JSON 格式错误或结构超限返回 400 或 413，且不提交输出前缀。验证可以登记来源元数据；这与接受容器输出不同。

前面分组提交后仍可能发生持久化失败。因此 503 要求以原始来源记录重放原批次。逐记录指纹同时对完整批次重放和重新分组的重放去重。成功响应报告 `received` 和 `rejected`；成功不表示每条记录均被保留。永久无效记录，以及容量、过期或隔离归档导致的拒绝分别计数。已知归档身份在存储允许时记录持久缺失标记；无法归属的来源失败只在计数器中显示，不编造归档身份。

固定的 Fluent Bit 4.2.3 HTTP output 不重试普通 4xx 响应，包括 400 和 413；它重试 408、429、5xx 和传输失败。因此必须预验证：永久批次拒绝不能留下已接受输出前缀。临时故障期间，文件系统重试队列保持有界。状态规则定义在[固定版本输出实现](https://github.com/fluent/fluent-bit/blob/v4.2.3/plugins/out_http/http.c#L283-L316)中。

<a id="retention-and-missing-evidence"></a>
## 保留与缺失证据

终态归档从完成与采集封存两者较晚时间起保护 **15 天**。迟到且被接受的输出再延长保护 15 天，不替换已封存输出。达到上限的已封存归档拒绝迟到输出并记录缺口。自动清理不会淘汰其他执行的受保护日志。运行任务超出输出上限时，只能轮转自己的窗口。

保护到期后回收归档，有界 `expired` 摘要再保留 15 天。JobRun 删除后继续维护目录。按数量清理执行历史时保护受保留归档。常规终态 Job 清理等待归档封存，并保留账本摘要；单独删除 Pod 不删除持久归档。

归档保留、执行账本保留和 Function 调用历史是独立生命周期。归档保护期不是 Function 历史 TTL。Function 日志路由还要求父定义可用，以解析其当前资源组；父定义删除后，仅保留归档文件不能维持原 Function URL 可访问。

预算耗尽时任务继续，采集变为 partial 或 unavailable。面板和导出显示已知省略范围，或明确表示范围未知。独立预留控制块使主 manifest 预算耗尽后仍能记录已知缺失。采集属于观察证据；节点丢失、采集器缓冲丢失和从未采集的历史输出无法重建。封存状态不能证明来源覆盖没有缺口。

**Delete retained logs** 请求带审计删除，需要独立 `instance:DeleteLogs` 或 `function:DeleteLogs` 授权。它可在保护期内删除终态归档，但不删除执行摘要。清理异步执行；回执表示已请求，不表示已删除。

<a id="api-and-permissions"></a>
## API 与权限

读取和下载需要 `instance:ReadLogs`；删除需要 `instance:DeleteLogs`。各权限均独立于普通执行读取权限：

```text
GET    /api/v1/instances/{run_id}/logs/archive
GET    /api/v1/instances/{run_id}/logs/archive/download
DELETE /api/v1/instances/{run_id}/logs/archive
```

Instance 归档路由要求凭据没有资源组限制。绑定资源组的凭据使用 Function 归档路由，并按 Function 已持久化资源组授权。

Function 历史使用 `/api/v1/functions/{id}/runs/{history_uuid}/logs/archive` 和相同 `/download` 后缀，按实际父资源组授权，并检查 `function:ReadLogs` / `function:DeleteLogs`。每次操作均应用显式 Deny 和租户隔离。浏览器调用方不能提供 Pod 定位参数。

读取查询支持 `after`、`limit`（最多 1,000）、正数 `attempt` 和 `search`（最多 256 字符）。跟随重连使用最后游标。搜索只扫描保留证据。响应包含采集状态、缺口、输出字节数、保留字节数、限制字节数和保护到期时间。下载包含省略标记和显式结束标记；中断导出不能视为完整。密钥保留在请求头和页面内存中，不进入 URL 或 localStorage。

旧 `/attempts/{attempt}/logs` 端点仍提供来自保留 Kubernetes 资源的有界快照，可用于旧执行，但没有持久保留保证。其可用性与归档不同。

读取携带调用者取消上下文和包含排队在内的五秒工作预算。每个归档进程最多并发四次读取，每租户最多两次；各归档自行协调段快照与追加、删除。解压与搜索不持有安装元数据锁。读取取消或预算耗尽时返回 unavailable，不返回完整空页。已在操作系统内阻塞的本地文件系统操作不能被 Go context 强制中断。

段缺失或损坏会返回显式部分页面，标明省略序列范围；其余已验证段仍可读取。下载包含后续页面发现的缺口和采集状态变化。Manifest 格式无效、超大、缺失或校验和无效时，隔离按哈希命名的归档目录，保留文件和计费分配，并拒绝在该证据上重新注册。健康归档仍可用。符号链接、不安全目录布局或 manifest 身份与目录不一致，仍会阻止服务打开不受信任归档根目录。
新写入 manifest 包含内容校验和；无此校验和的旧 manifest 保留结构和 gzip 完整性检查，但不追溯保证内容校验和。校验和检测损坏，不认证有卷访问权限的管理员所作更改。

检查 `orbitjob_archive_corruption_total`，并保留受影响目录用于恢复。不得通过删除文件或伪造空 manifest 恢复健康状态。替换证据前，先在隔离副本验证协调备份。隔离的 manifest 目录排除在自动无引用文件清理之外，继续占用归档容量。

在安装了 `go.mod` 声明 Go 版本的源码 checkout 中，可检查停止的归档或隔离卷副本，且不改写证据：

```bash
go run ./internal/logarchive/inspectcmd --directory /path/to/archive-copy --timeout 1m
```

命令输出包含归档哈希和完整性发现的 JSON，不含容器输出。底层命令未发现问题时退出 0，发现归档问题时退出 1，根目录不安全或检查未完成时退出 2；`go run` 还会报告命令非零退出状态。命令拒绝活动写入锁，不创建缺失锁文件。检查最多覆盖 10,000 个归档目录，并受指定时间预算限制。备份副本本身必须一致；检查中断不能证明剩余归档完整。

<a id="operations-and-verification"></a>
## 运维与验证

检查安装命名空间中的 `orbitjob-log-archive` 和 `orbitjob-log-collector`、就绪探针和 `/metrics` 端点。归档计数器使用有界标签，不包含输出文本和凭据。采集器节点日志访问属于受信任安装能力；工作负载和浏览器凭据不获得节点、Secret 或 exec 访问权限。输出按纯文本处理，仍属敏感内容。

触及数据库物理元数据保护上限时，现有归档仍可更新状态并接受清理。先回收符合条件的行，再由数据库所有者在维护窗口回收关系膨胀（先 `VACUUM FULL
execution_log_archives`，再 `ANALYZE execution_log_archives`），之后才能预期恢复新注册。该保护拒绝超出分配的增长。

将 PostgreSQL 数据库、归档卷和安装配置作为一个可恢复安装协调备份。依赖备份前，在隔离环境中同时验证恢复的身份目录与归档文件。仅恢复数据库不恢复归档输出；仅恢复归档不恢复读取所需身份和权限。

应用回滚必须保留归档文件、增量数据库 schema 和保留限制。禁用新采集不授权删除受保护归档或无界缓冲。历史快照输出没有追溯完整性保证，不可用输出无法重建。

文件系统容量、保留、PostgreSQL 授权和重启恢复结果见[验证记录](/orbitjob/docs/verification/)。

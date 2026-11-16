---
title: "已知问题"
---

本页记录实现缺口与运行约束。[项目状态](/orbitjob/docs/status/)说明已实现能力；[验证记录](/orbitjob/docs/verification/)记录已测试行为和资格验证限制。

<a id="current-implementation-gaps"></a>
## 当前实现缺口

<a id="fault-qualification-requires-complete-installed-recovery-evidence"></a>
### 故障资格验证需要完整的已安装恢复证据

负载验证程序为 Operator、Admin API、PostgreSQL 和 Scheduler 故障分别准备恢复 canary。验证交叉核对租户范围账本事实、Kubernetes 归属、连续 fixture journal 和 Scheduler 预算评估。副本就绪或人工提供恢复声明均不能证明恢复。缺少准备、来源观察或 journal 连续性时，结果保持 `INCONCLUSIVE`。Fixture 在内存中保存 journal；重启会使当前运行的连续性失效，即使之后仍能读取效果记录。

这些检查需要针对实际候选版本执行已安装故障场景，才能支持资格验证。它们不证明任意网络分区或 Lease 丢失恢复。见[负载测试](/orbitjob/docs/load-testing/profiles/)和限定范围的[验证记录](/orbitjob/docs/verification/)。

<a id="clean-machine-onboarding-remains-unverified"></a>
### 全新机器首次使用仍未验证

快速开始尚无经过测量的全新机器完成时间保证。安装耗时取决于下载、镜像构建与宿主机资源。安装检查见[验证记录](/orbitjob/docs/verification/)，首次使用范围见[产品需求](https://github.com/s3loy/orbitjob/blob/main/docs/prd.md)。

<a id="execution-evidence-can-remain-irrecoverable"></a>
### 执行证据可能无法恢复

已绑定 Job 缺失时，执行变为非终态 ExecutionUnknown。空 Pod 清单、节点丢失和强制删除不能证明物理进程终止。Unknown 和 CancelUnknown 保留 Forbid 名额，不参与常规历史清理。目前没有强制释放 API；长期不确定状态可能需要人工隔离，以及单独审阅的恢复流程。

新工作负载 Pod 保留容器退出证据，直到 Operator 将其提交。因此 Operator 或数据库故障可能导致已退出 Pod 及其 Job 继续存在。移除安装前先恢复兼容 Operator 和数据库，并遵循[排空检查](/orbitjob/docs/usage/helm-and-kubernetes/#74-upgrade-and-uninstall)。历史工作负载如果没有该保护，升级不会补全其缺失证据。

终态 Pod 缺少可信容器退出状态时，即使 Kubernetes 报告 Job 失败，执行仍保持 unknown。这包括清理时被标记为 Failed 的未调度 Pod。其证据保留持续生效，直到经单独审阅的恢复确认可以安全移除哪些内容。

<a id="schedule-adherence-has-no-dedicated-sli-derivation"></a>
### 调度准时性没有专用 SLI 推导

账本记录 `scheduled_for`，但受支持的 SLI 类型与 Run 结果条件不推导专用调度准时性指标。现有状态和时长 SLI 无法完整衡量调度准时性。术语由 `internal/core/domain/sli/create_input.go` 和 `criteria.go` 定义；`scheduled_for` 本身不是准时性评估。

<a id="sli-retirement-preserves-unresolved-history"></a>
### SLI 清理保留尚未明确的历史

只有原 Run 已删除、所有计划成员都有匹配事实，且计划与最新事实都超过最短 30 天重放保护期后，Operator 才按有界分页清理重放明细。可查询的 `unavailable` 事实保留最长受支持 SLO 窗口，目前为 8760 小时。缺少事件时间、投影未完成或 Run 仍被保留，均阻止自动清理。聚合快照与观察状态继续保留。

来源锁和事件 Run 锁串行化计划创建、事实记录与清理；已清理计划不能授权再次增加计数。维护覆盖非活动来源，以及仍在 Operator 命名空间映射中的挂起租户。从映射移除租户也会使其退出自动维护，应保留数据以供明确审阅。见[结果恢复](/orbitjob/docs/database/#audit-permissions-and-outcome-recovery)。

<a id="operational-and-compatibility-constraints"></a>
## 运维与兼容性约束

- **账户部署需要已安装验收。** 个人账户候选版本包含迁移 0027、会话 origin 配置和带外设置、恢复工具。PostgreSQL 与浏览器 fixture 检查不能证明已升级安装。保留原 API 密钥和迁移输入渠道，停止旧写入进程，并在协调切换后验证个人会话、已有自动化和保留历史。
- **当前开发集群仍需维护窗口。** 原生 kind NetworkPolicy 执行和有界采集器文件系统已在一次性集群中测试。在当前安装自身的流量、挂载、容量和重启检查通过前，其已验证标志必须保持未设置。宿主后端文件系统空间必须满足 40-GiB 前置要求。安排变更前遵循[本地维护](/orbitjob/docs/local-maintenance/)。

- **Settling 观察已投影的 JobRun 账本。** 托管运行器使用 `loadtest settle`，设置事务内租户上下文，并使用本次运行记录的修订版本 ID。观察缺失或查询错误保持 unknown。计数为零不能证明尚未投影的 CR 已完成，不能停止活动 cron 定义，也不能证明故障恢复；恢复结论仍以独立验证器为准。
- **历史投影切换。** 迁移 0012 保留旧聚合并确认已有终态行。它无法区分历史事件此前已计数还是漏投影。自动恢复覆盖之后的终态转换；更早的缺失投影需要离线审阅。迁移前停止旧 Operator，迁移后启动新版本；新旧写入进程重叠可能破坏去重。
- **迁移校验和渠道不同。** 现有 Helm ConfigMap `|-` 投影移除末尾 LF 字节；迁移程序对实际输入计算哈希。因此原始目录 SQL 与 Helm 挂载 SQL 不能在已有账本中互换。保留原输入渠道和源码、投影后的两份清单，不得改写已记录校验和。见[数据库设置](/orbitjob/docs/database/#audit-permissions-and-outcome-recovery)。
- **凭据轮换需要协调。** 仅更改安装 Secret 不会更改 PostgreSQL 角色密码或运行中 Pod 的环境变量。遵循[数据库设置](/orbitjob/docs/database/)中的 owner-init 与升级流程，并验证新连接和重启后的工作负载。通过 trust 规则进行的回环或端口转发连接不能证明所提供密码有效。
- **集成测试需要一次性数据库。** 测试可能重建 schema、配置角色和修改数据。专用 `TEST_DATABASE_DSN` 绝不能指向安装状态。支持的命令与串行执行要求见 [CONTRIBUTING.md](https://github.com/s3loy/orbitjob/blob/main/CONTRIBUTING.md) 和 Makefile；没有强制的机器专属测试端口或容器名称。
- **命名空间接入必须显式配置。** 工作负载命名空间必须出现在 `operator.namespaceTenants` 中并获得匹配 RBAC。仅新增租户行不会配置命名空间，也不会扩大 Operator 权限。
- **外部工作可能重复。** Job backoff 与平台重试具有至少一次效果。成功触发响应确认 Run 资源，不表示工作负载已完成，也不表示查询已能看到对应账本行。

<a id="live-evidence-still-required"></a>
## 仍需实际环境证据

受控恢复场景不能证明任意网络分区恢复、生产韧性或通用 Job/Pod 垃圾回收时序。升级、回滚与备份恢复必须针对实际源码、数据库历史和部署配置验证。已完成检查与待执行场景见[验证记录](/orbitjob/docs/verification/)。

负载配置时长与托管选项由 [test/load/config/](https://github.com/s3loy/orbitjob/blob/main/test/load/config/) 和[负载工作流](https://github.com/s3loy/orbitjob/blob/main/.github/workflows/loadtest.yml)定义。读取 `result.json` 中的产品结论；仅验证程序成功完成不构成资格验证。

<a id="fault-qualification-has-no-independent-business-recovery-probe"></a>

<a id="the-combined-console-and-backend-still-require-unified-acceptance"></a>

<a id="deduplication-facts-have-no-bounded-archival-horizon"></a>

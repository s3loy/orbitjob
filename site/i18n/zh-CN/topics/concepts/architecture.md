# 架构

OrbitJob 使用 Kubernetes Job 执行容器工作负载，使用 PostgreSQL 记录持久化
执行事实，并通过 Admin API 管理定义和访问权限。Operator 控制执行。
Scheduler 根据已记录的观测结果评估 SLO。这些职责在故障和恢复期间仍保持独立。

## 组件

| 组件 | 职责 |
|---|---|
| Admin API | 认证调用方、执行访问策略、写入管理状态及其事务审计、发布和取消运行资源，并读取执行历史。它不能写入执行台账。 |
| Operator | 投影不可变修订版本、准入运行、创建和观察 Kubernetes Job、调度作业和 Check、同步 Function、推进 Workflow、恢复派生结果，并清理符合条件的历史记录。 |
| Scheduler | 根据 SLI 快照计算 SLO 预算和消耗告警。它不创建工作负载 Job。 |
| 定义准入 | 在 Kubernetes 持久化前，根据检查后保存身份校验最终 Job/Workflow 定义，并为受信任的原生写入方提供显式例外。 |
| 日志归档 | 在专用 PVC 上存储有界执行证据，并维护按租户隔离的 PostgreSQL 身份目录。它接收采集器输出，并通过 mTLS 向内部读取方提供服务。 |
| Fluent Bit 采集器 | 读取已映射工作负载命名空间的节点 CRI 输出，并将经过来源校验的记录转发到归档。 |
| Console | 在 Admin API 的 `/console/` 同源地址下提供浏览器界面，用于编写定义、执行、监控和访问管理。 |
| CLI | 调用 HTTP API，并使用 kubectl 进行安装诊断；它不访问数据库。 |

安装命令生成数据库凭据（`configure`）、应用有序且带校验和的迁移（`migrate`）、
交付引导密钥（`bootstrap`），并签发个人账户设置和恢复能力令牌
（`orbitjob-identity`）。这些命令需要安装管理权限，与普通工作负载客户端相互独立。
请参阅[数据库设置](database-setup.md)、[账户](accounts.md)和
[本地部署](local-deployment.md)。

## 定义与执行

ScheduledJob 和 WorkflowJob 是 Kubernetes 自定义资源。Check 和 Function
通过 Admin API 存储。Operator 将这些配置投影为不可变执行修订版本。
JobRun 表示一次逻辑执行请求；WorkflowRun 协调其步骤 Run。每个平台 Attempt
绑定一个 Kubernetes Job。Kubernetes 退避机制可以在同一 Attempt 中创建多个 Pod。

手动作业触发和 Function 调用发布 JobRun 资源；Workflow 触发发布 WorkflowRun
资源。响应引用资源及触发标识，台账记录随后异步生成。计划触发项和工作流步骤在发布前
记录持久化身份。发生中断后，协调过程使用已存储的身份。

平台只有 Kubernetes Job 这一种执行后端，不提供进程内 worker 回退。
`timeoutSeconds` 转换为每个 Attempt 的 Kubernetes Job 截止时间，不限制所有
Attempt 合计的实际耗时。[执行模型](concepts/execution-model.md)从工作负载
用户的角度说明阶段、重试预算、并发和取消行为。

## 管理与租户边界

浏览器会话标识个人用户；API key 标识自动化调用方。工作区成员资格及有效的
操作和资源授权决定工作负载请求是否获准。安装实例管理员身份允许进行全局账户
和工作区元数据管理，不隐含工作负载访问权限。

API 根据认证结果及已授权目标解析可信的租户、分组和操作者身份。客户端 JSON
不能替换这些身份。显式 Deny 优先；缺少授权时拒绝访问。如果受支持的资源和操作
语义无法证明包含关系，权限边界交集与委派包含检查均拒绝放行。

支持分组的资源和证据会解析其实际父分组。无法执行分组范围控制的路由会拒绝绑定
分组的凭据。平台调用方选择一个已授权目标租户，同时用于授权和数据访问；选择器
本身不授予跨租户访问权限。请参阅[管理访问权限](tasks/manage-access.md)和
[安全](../SECURITY.md)。

## 持久化状态与数据库角色

PostgreSQL 存储管理状态、不可变修订版本、执行历史、投影确认、审计事件和日志身份目录。

- `job_definition_revisions` 存储可执行快照。
- `job_run_control_plane` 和 `job_run_attempts_control_plane` 存储 Run 和
  Attempt 事实；`workflow_run_control_plane` 存储父运行及步骤决策。
- Check、Function 及其派生历史提供管理和查询模型。
- SLI 快照、SLO 预算和告警记录监控证据。
- 审计变更与对应领域变更在同一事务中提交。
- `outcome_recorded_at` 确认所需终态投影；`sli_snapshot_events` 在写入快照时，
  对租户、运行和 SLI 的增量去重。去重事实的保留期长于普通运行清理周期。

应用角色不拥有租户表。仓储在执行限定范围查询前设置事务局部 `app.tenant_id`，
RLS 对这些角色生效。表所有者为 NOLOGIN；迁移身份可以切换为该角色。
Admin 具有管理写入权限和只读执行历史权限。Operator、Scheduler、准入组件和归档
使用 Runtime 登录身份及各自所需的运行时授权。范围受限的 SECURITY DEFINER
操作用于必须在普通租户事务之前执行的身份解析。

日志归档使用可信的 Runtime 数据库身份。Admin 读取其目录，并提交经过审计的清理
请求；容器输出、清单和生命周期事件存储在归档 PVC 上。数据库与文件系统不共享事务。
[执行日志](execution-logs.md)说明重放、容量、保留和协调备份行为。

## 创建与不可变身份

向外部发出 Job 创建请求前，Operator 先提交 UID 为空的 `CreatingAttempt`
意图。后续事务锁定 Run/Attempt、检查当前状态，并执行有界 Job Ensure。
不确定的 Kubernetes 响应不能证明 Job 未被创建。恢复过程解析原始地址，校验完整
身份，并绑定已确认的 UID。

后续观测必须匹配该不可变身份，并维持 resourceVersion 和阶段处理的单调性。
仅凭名称不足以确认身份。当前名称保留完整触发摘要；历史短名称只有在完整身份
检查通过后，才作为有界查找候选。修改和保留操作使用 UID 和 resourceVersion
条件，防止影响替换后的资源。

源准入以及修订版本和生命周期写入通过 READ COMMITTED 事务中的 PostgreSQL 锁串行化。
重放在准入新工作前先解析原始触发项。被 Forbid 阻止的手动 JobRun 请求保持待处理，
不会新增台账行。计划跳过和错过调度处理，以及被拒绝的 Workflow 触发回执，分别
具有独立的准入契约。结果未知的执行继续占用并发名额。

## 执行恢复与并发准入

已绑定的 Job 缺失时，运行进入非终态 `ExecutionUnknown`。OrbitJob 保留原始
身份和观测结果；资源缺失不构成创建另一个 Attempt、确认终态或释放 Forbid 名额
的依据。结果未知的 Workflow 步骤会阻止新工作，已经运行的同级步骤仍可继续观察。

取消操作使用 UID/resourceVersion 检查更新意图。Operator 缩短原始 Job 的
截止时间，先要求控制器确认停止，再接受完整的所属 Pod 清单。删除时间戳不能
证明正在进行的 Pod 创建已经结束。新工作负载 Pod 保留由 Operator 管理的终止
证据 finalizer，使退出观测能够在控制器清理后继续保留。

Operator 先提交完整的所属容器证据，再使用 Pod UID/resourceVersion 前置条件
释放其 finalizer。成功、失败、重试和截止时间处理均在推进台账前保持这一
持久化与释放顺序。取消过程仅在停止证明持久化并释放证据 finalizer 后删除原始
Job，随后在事务中检查证明，再记录 `Canceled`。已停止或终态的 Workflow 父运行会阻止新的子运行
预留和创建。

取消确认窗口使用已持久化的请求时间戳，重启不会延长该窗口。超时保留不确定状态，
完整留存证据随后可以支持恢复。明确的证据缺口会撤销证明并隔离 Attempt；后续
范围更窄的清单不能消除该缺口。未保留证据的旧工作负载继续采用保守恢复语义。
节点丢失或外部移除证据不能证明工作负载已经终止。

Client-go Lease 领导者机制协调单实例的调度、Check、Function、Workflow 和保留
循环。失去领导者资格后，当前任期的工作会被取消并排空，然后才重新竞选。
Lease 会话不提供跨 PostgreSQL 和 Kubernetes 的通用隔离保证。事务准入另行
提供源级并发边界。幂等终态投影重放不要求领导者资格。

## 终态恢复与 SLO

终态执行事实先提交，随后写入派生的 Check、Function 和 SLI 记录。所有必需的
幂等投影完成后才予以确认。失败投影保持待处理状态，并受到防清理保护。按租户
进行的键集扫描允许后续正常行继续推进，不受先前失败行阻塞。

迁移 0012 保留历史聚合，并确认切换前的终态行。它不会重建无法证明的历史计数。
执行该迁移前必须停止旧 Operator；请参阅
[数据库切换流程](database-setup.md#writer-cutover)。

HTTP Check 执行按摘要固定的探测容器。确认的结果生成 Check 结果和五分钟 SLI
分桶。Scheduler 根据这些分桶评估 SLO 窗口和消耗告警。判定条件使用已产生的
状态和耗时事实；调度遵循情况没有专门的 SLI 派生逻辑。

## 浏览器与外部接口

Admin 镜像构建并提供静态 Console。浏览器请求使用同源 HttpOnly 会话 cookie
及内存中的 CSRF 令牌。API key 继续作为独立的自动化凭据。公开文档站点不接收
安装实例凭据。仅进行 Go API 开发时可以不构建前端包，但这不代表安装后的 Console
可用。

在线定义路由接受映射命名空间中的类型化 Job/Workflow spec，并独立要求 Apply
权限、预演准入和 UID/resourceVersion 比较。默认 chart 启用验证 Webhook；调用失败时拒绝请求。
该 Webhook 依据已审核摘要和当前操作检查最终准入 spec。显式原生写入方仍需提供
受支持的类型化 spec，但免于检查后保存回执要求。这些接口不提供任意 Kubernetes
写入。日志读取通过保留的历史解析不可变归属，并要求独立的 ReadLogs 权限。
Admin API 不提供通用 Kubernetes 代理、Secret、exec 或 attach 接口。

HTTP 契约为 [OpenAPI](../api/openapi.yaml)，通过 `/openapi.json` 提供。
结构化日志、Prometheus 指标和 `X-Trace-ID` 支持观测和请求关联。目前未实现
分布式追踪 span 或 OpenTelemetry 导出器。[验证记录](verification.md)记录限定
范围的测试和安装证据；[已知问题](known-issues.md)记录剩余限制。

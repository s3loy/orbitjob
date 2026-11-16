---
title: "架构设计"
---

OrbitJob 将工作负载执行、持久执行事实与管理操作分离。本文说明已实现组件及其契约。[验证记录](/orbitjob/docs/verification/)记录已测试行为与限制。

<a id="components"></a>
## 组件

OrbitJob 通过 Kubernetes Job 执行租户工作负载，并将执行事实记录到 PostgreSQL。Kubernetes 资源请求工作并承载期望状态；Run 账本记录固定修订版本、attempt、身份和结果。已移除的进程内 dispatcher/worker 流程不作为执行后备路径。

| 入口 | 职责 |
|---|---|
| `cmd/admin-api` | 经过认证的管理 API；读取定义与历史，为 trigger、invoke 和 cancellation 创建或修改 Run CR。写入管理表，但不能写入执行账本。 |
| `cmd/operator` | 投影定义，创建计划和 Check Run，同步 Function，推进 Workflow，确保容器 Job，观察 attempt，恢复投影并清理历史。 |
| `cmd/scheduler` | 将租户 SLI 快照评估为 SLO 预算和消耗率告警，不创建工作负载 Job。 |
| `cmd/configure` | 生成安装数据库状态和角色凭据。 |
| `cmd/migrate` | 以迁移身份应用有序且带校验和的 SQL 迁移。 |
| `cmd/bootstrap` | 确保初始租户和密钥存在，并将配置的密钥写入本地文件或 Kubernetes Secret。 |
| `cmd/orbitjob` | Check、Job、Run、状态与诊断的 HTTP/kubectl 客户端，不直接访问数据库。 |
| `cmd/openapi-gen` | 从路由元数据和 DTO 生成或检查 OpenAPI 文档。 |
| `cmd/healthcheck` | 健康端点辅助工具。 |
| `cmd/log-archive` | 通过单写入进程在独立 PVC 上保存有界执行证据；PostgreSQL 保存按租户隔离的紧凑目录；节点采集器摄入和内部读取通过 mTLS 访问。 |

归档使用受信任的 Runtime 数据库身份；Admin API 拥有目录读取和带审计的清理请求权限。容器输出、生命周期事件和 manifest 存放在日志 PVC 上。[执行日志](/orbitjob/docs/execution-logs/)定义采集、保留、配额和运维边界。

<a id="browser-console"></a>
## 浏览器 Console

从源码构建的 Admin 镜像在 `/console/` 提供静态 Console。Docker 前端阶段构建 `web/`；`internal/admin/console` 通过现有 Admin API Service 提供输出。仅 Go 源码开发不要求 Node 或前端资源。React Router 管理浏览器路由，TanStack Query 管理读取与有界观察，生成类型跟随 `api/openapi.yaml`。

安装配置通过 HttpOnly 个人会话 cookie 发出相对同源请求，并将会话 CSRF 绑定保存在内存。API 密钥仍用于自动化，不用于管理个人账户。安装 bundle 排除模拟客户端。公共站点仅提供文档；其构建不发布 Console 或 Demo bundle，也不接受安装凭据。连接范围操作状态跨导航保留执行意图、回执和取消观察，在连接变化时丢弃。Foreground 实验与 AutoDL 不在此范围内。

类型化定义路由仅在已配置租户命名空间内生效，并比较 UID/resourceVersion。它们发布 Kubernetes 期望状态；Operator 负责账本写入。独立日志路由解析不可变执行归属，并返回有界保留文本。两类接口均不提供通用 Kubernetes 代理。平台预设文档使用迁移 0007 的窄授权读取器；工作区发现使用迁移 0011 的授权元数据读取器。见[使用指南](/orbitjob/docs/usage/web-console/)、[凭据边界](/orbitjob/docs/security/#browser-console)与[前端开发](/orbitjob/docs/deployment/frontend-development/)。

<a id="code-boundaries"></a>
## 代码边界

`internal/admin/http` 将请求适配到 Admin 应用用例与仓储。`internal/operator` 将 Kubernetes 事件适配到 Core 应用服务与账本。`internal/core/domain` 包含业务模型；`internal/core/app` 包含用例与依赖接口；`internal/core/store/postgres` 实现持久化。共享验证和资源类型位于 `internal/domain`；基础设施位于 `internal/platform`。

Admin 和 Operator 可以导入 Core。生产 Core 代码不得导入 Admin 或 Operator。外部包集成测试可以同时覆盖该边界两侧。

<a id="tenant-and-authorization-boundaries"></a>
## 租户与授权边界

Bearer 认证在每个请求中一次性解析密钥、租户、资源组和有效策略文档。受保护路由声明 action 和目标资源。显式 Deny 优先；没有授权时拒绝访问。边界交集和委派包含性保留受支持的精确 action 与完整资源段通配语义，证明限制耗尽时拒绝访问。Bootstrap 租户本身不提供跨租户访问权限。

可信 tenant/group/path 字段在客户端绑定后由服务端赋值，或排除在绑定之外。支持资源组的读模型保留父资源作用域；不支持资源组限制的接口拒绝带作用域调用方。SLI/SLO 引用在写入事务和租户外键中检查。见 [API 密钥与授权](/orbitjob/docs/security/#api-keys-and-authorization)。

<a id="persistent-state"></a>
## 持久化状态

完整[迁移目录](https://github.com/s3loy/orbitjob/blob/main/db/migrations/)定义 schema、角色、函数、RLS 和授权。[Chart 迁移副本](https://github.com/s3loy/orbitjob/blob/main/charts/orbitjob/migrations/)同步并受检查，不是第二套 schema 定义。

- `job_definition_revisions` 保存不可变可执行快照。ScheduledJob 和 WorkflowJob 投影、Check 和 Function 同步均使用修订版本。
- `job_run_control_plane` 和 `job_run_attempts_control_plane` 保存 Run 与 attempt。`workflow_run_control_plane` 保存父 Run 和任务决策。
- `checks`、`check_runs`、`functions`、`function_runs`、SLI、SLO、快照、预算和告警提供管理状态与派生查询模型。
- `audit_events` 及其分区对应用角色只允许追加。管理或账本写入与其审计事件一起提交。
- `outcome_recorded_at` 跟踪已完成终态投影；`sli_snapshot_events` 在快照更新的同一事务内，对每个 tenant/run/SLI 增量去重。这些事实在 Run 清理后仍保留。

仓储设置事务内 `app.tenant_id`；RLS 过滤应用角色，这些角色不拥有表。表所有者为 NOLOGIN，迁移程序可以切换为此角色，运行进程使用独立登录身份。Admin 对执行历史只读，同时保留管理表写权限。经审阅的 SECURITY DEFINER 函数覆盖必须在普通作用域事务可用前解析租户的窄范围操作。

<a id="creation-observation-and-cancellation"></a>
## 创建、观察与取消

1. 每个入口解析固定修订版本和稳定 occurrence 身份。API 手动触发和 Function 调用发布 Run CR，供 Operator 准入。计划执行和 Workflow 步骤先记录持久 occurrence 或步骤，再发布 CR。操作中断后，幂等发布与恢复收敛到已保存身份。
2. 发起外部创建请求前，仓储提交 UID 为空的 `CreatingAttempt` 意图。第二个事务锁定 Run/attempt，检查当前状态并执行有界 Job Ensure。PostgreSQL 与 Kubernetes 没有共同事务；持久意图使不明确结果可见。
3. 确认的 UID 绑定 attempt。后续观察必须匹配此不可变身份，并单调推进 resourceVersion/phase。恢复使用已保存的实际 Job 地址，包括兼容历史名称。
4. Workflow 步骤先锁父资源，再锁子资源和 attempt。已停止或终态父资源拒绝新步骤、预留和 Ensure；过期 Running 更新不能覆盖取消。
5. 取消通过 UID/resourceVersion 测试修改 Run 意图。Operator 缩短原 Job 截止时间，等待控制器停止确认，提交已验证的所属 Pod 终止证据，解除其 Pod 证据 finalizer，再请求前台删除 Job。创建不明确时保持 `CancelUnknown`；没有已知 UID 不能证明从未执行。终态结果保持最终性。

新 Run 名称保留完整 occurrence 摘要。历史短名称仅为有界查询候选，完整身份检查通过后才接受。历史保留使用 UID/resourceVersion 删除前置条件，并覆盖非活动来源和 Workflow 父子资源。见[迁移兼容性](/orbitjob/docs/database/#migration-lineage-and-compatibility)。

外部执行具有至少一次语义。`restartPolicy: Never` 防止 Pod 内重启；Job 已配置 backoff 和平台 attempt 仍可能再次启动工作负载代码。工作负载必须容忍重复效果。渲染 Pod 禁用 ServiceAccount token 挂载。

<a id="terminal-recovery-and-slos"></a>
## 终态恢复与 SLO

终态阶段先于派生 Check、Function 和 SLI 写入提交。所有必需幂等投影完成后才确认。失败投影保持待处理并受历史保留保护；按租户 keyset 扫描防止整页失败记录阻塞后续健康行。由于重复投递安全，此恢复循环不要求领导权。

迁移 0012 保留已有统计并确认旧终态行；切换后新进入终态的 Run 参与自动恢复。迁移前必须停止旧 Operator。恢复不重建无法证明的历史计数。[数据库指南](/orbitjob/docs/database/#audit-permissions-and-outcome-recovery)定义切换、回滚和去重保留要求。

HTTP Check 以固定镜像摘要的容器探针运行。其实际 Run 结果产生 Check 结果与五分钟 SLI 桶。Scheduler 根据这些桶评估 SLO 窗口和消耗率告警。受支持条件只描述已产生的状态与时长事实；调度准时性没有专门 SLI 推导。

<a id="interfaces-and-deployment"></a>
## 接口与部署

HTTP 契约为 [OpenAPI](/orbitjob/openapi.json)，通过 `/openapi.json` 提供。`make openapi-check` 验证生成内容是否最新；契约测试额外验证 JSON 字段名、必填字段和带版本请求体。生成的 `APIKey` 安全方案声明 Bearer 请求头；中间件与策略评估执行认证和授权。

可观测性使用结构化日志、Prometheus 指标，以及原样返回或生成的 `X-Trace-ID` 关联请求。目前未实现分布式追踪 span 或 OpenTelemetry exporter。

四类 CRD 为 `ScheduledJob`、`JobRun`、`WorkflowJob` 和 `WorkflowRun`，定义在 [api/kubernetes/](https://github.com/s3loy/orbitjob/blob/main/api/kubernetes/)，从 [chart CRD](https://github.com/s3loy/orbitjob/blob/main/charts/orbitjob/crds/) 安装。Operator 监听这些资源和 Kubernetes Job。其计划、Check、Function 同步、Workflow 和历史保留循环在 Kubernetes Lease 领导权下运行。Scheduler 具有独立、可选且由构建标签控制的 etcd 路径。

Helm 按顺序围绕部署启动执行 owner 初始化、迁移和 bootstrap hook。数据库 Secret 名称和键配置来自 chart values。租户工作负载命名空间必须出现在 `operator.namespaceTenants` 中，并在这些命名空间获得 RBAC。Admin 可以创建、读取和修改 Run CR；Operator 管理 Job 生命周期、状态和历史删除。见[数据库设置](/orbitjob/docs/database/)、[本地部署](/orbitjob/docs/deployment/prerequisites/)与[使用指南](/orbitjob/docs/usage/before-you-begin/)。

<a id="accepted-correctness-work-in-progress"></a>
<a id="execution-recovery-and-concurrent-admission"></a>
## 执行恢复与并发准入

Operator 将已绑定但缺失的 Job 保留为非终态 `ExecutionUnknown`。其原 Job/JobRun 身份和持久观察是恢复依据。资源缺失不能证明终止，也不允许另一个 attempt。Unknown 步骤阻止新的 Workflow 工作，已运行的同级步骤仍可观察。显式取消需要保留终止证据才能建立 `Canceled`。
新工作负载 Pod 模板带有 Operator 管理的证据 finalizer。取消通过 UID/resourceVersion 检查缩短原 Job 截止时间，等待控制器停止条件后再收集完整 Pod 清单。删除时间戳不能证明在途 Pod 创建已结束。保留 Pod 在控制器清理期间保存退出证据。
Operator 先提交完整所属容器证据，再以 Pod UID/resourceVersion 前置条件解除自己的 finalizer。成功、失败、重试和已配置截止时间均采用相同持久化与解除顺序，再推进账本。恢复覆盖历史 attempt 待完成解除；历史清理不能先移除其归属记录。取消只在证明持久化且解除后删除原 Job，并在原 Job 删除和最终事务证明检查后确认 `Canceled`。

取消中的 Run 不能只凭 Job 或 attempt 阶段获取停止证明。五分钟确认窗口使用持久化取消时间戳，因此重启不会延长窗口。单纯超时仍不明确，可以通过完整保留清单恢复。明确证据缺口按 parent/run/attempt 锁顺序原子撤销证明并隔离 attempt；后续范围更窄的清单不能解除该隔离。没有证据 finalizer 的旧工作负载保留保守恢复语义。

Client-go 领导会话确定权限，不比较远端墙钟 Lease 过期时间。失去领导任期后，必须先取消并排空该任期工作，同一进程才能再次竞选。此机制协调领导权，不提供通用写入隔离。

来源准入、修订版本和生命周期写入在 READ COMMITTED 事务中使用同一 PostgreSQL 锁串行化。重放优先返回原 occurrence。已接受的手动 Run CR 因 Forbid 阻塞时保持待处理，不新增账本行；已有计划 skip/misfire 规则和 Workflow 计划拒绝回执仍是独立契约。Unknown 执行保留其名额。
Operator 与 PostgreSQL 仓储实现这些契约。领导权协调单例循环，不提供跨 PostgreSQL 与 Kubernetes 的通用 fencing token。已覆盖故障和并发场景见[验证记录](/orbitjob/docs/verification/)。

<a id="sources-of-truth"></a>

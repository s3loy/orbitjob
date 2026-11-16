---
title: "Web 控制台"
---

按上述说明获取访问权限后，在安装的 `/console/` 地址使用 Console。Admin 镜像内置 Console；独立的 [Vite 服务器](/orbitjob/docs/deployment/frontend-development/)用于前端开发。打开安装的 `/` 地址会重定向到 `/console/`。Console 通过与页面相同的协议、主机和端口调用 `/api/v1/` 和 `/openapi.json`，无需配置独立的 API 来源字段。安装管理员通过域名或 IP 暴露服务时必须保留这些路径；参阅[安装地址与外部访问](/orbitjob/docs/usage/helm-and-kubernetes/#installation-address-and-external-access)。

<a id="connect-to-a-workspace"></a>

### 连接工作区

在安装的 Connection 页面输入用户名和密码，然后选择 **Sign in**。在 **Your account** 下选择活动工作区。新用户接受工作区管理员的邀请；安装人员通过一次性 setup 链接创建首个管理员。相关流程、密码重置和运维恢复见[账户](/orbitjob/docs/accounts/)。

**Keep me signed in for 7 days** 创建绝对有效期为七天的持久会话；取消勾选后创建浏览器会话 cookie，服务端有效期为八小时。重新加载会恢复有效会话，轮询不会延长有效期。在共享设备上选择 **Sign out**。401 会清除受保护的本地状态并要求重新登录；403 仅限制被拒绝的操作，其他已获准操作仍可用。

一个账户可以具有多个工作区的成员关系。工作区名称和 slug 用于显示，稳定 ID、活动成员关系和有效策略授权决定权限。安装管理员可以管理全局账户和工作区元数据，访问工作负载则需要显式成员关系。Connection 的高级详情展示准确的 action/resource 权限检查。

浏览器在 `sessionStorage` 中保存不含秘密的已选工作区 ID。草稿、查询缓存和操作回执保存在页面内存中，退出登录、收到 401 或身份／工作区变更时清除。同一账户重新连接时，使用相同的用户 actor ID 核对持久化请求。

创建工作区不会配置 Kubernetes 资源。管理员需通过 Helm 配置 namespace 映射与 RBAC，工作负载才能运行。namespace 选项来自该映射，仍需集群校验。参阅 [namespace 拓扑](/orbitjob/docs/usage/helm-and-kubernetes/#71-namespace-topology)和[认证约定](/orbitjob/docs/usage/authentication-and-request-conventions/)。

<a id="create-and-update-workloads"></a>

### 创建与更新工作负载

通过表单创建 Job 或 Workflow，依次选择 Review and save、Check against cluster、Save definition 和 Confirm save。仅手动触发的定义使用空 schedule。保存手动 Job 不会执行它；应用未暂停的 schedule 可能启用后续自动执行。命令和参数项按字面值处理，不按 shell 文本解释。

高级 YAML 可导入受支持的 ScheduledJob 或 WorkflowJob，并进入相同审阅流程；不支持的 metadata、spec 字段及 kind 会被拒绝。复制和下载只导出编辑器文本，不应用资源。AI YAML 助手提供工作负载描述及可移植 skill ZIP，包含示例、API/CRD 参考和源码哈希。生成的 YAML 必须经审阅和集群校验后再应用；包内不包含凭据或写入权限。

镜像复用读取获准的 Job 与 Function。公共 Docker Hub 搜索仅在显式操作后打开，不会自动发送私有镜像引用或工作负载名称。镜像引用不证明集群可以拉取镜像；私有仓库访问由安装管理员配置。

在线读取定义需要 `job:Get` 或 `workflow:Get`，校验和写入需要 `job:Apply` 或 `workflow:Apply`。资源组作用域凭据会被拒绝。仅接受安装映射 namespace 中的类型化 spec，不提供定义删除或任意 Kubernetes 资源访问。

编辑前先载入在线 spec。集群 dry-run 校验 admission，不保存资源；Apply 确认已审阅 manifest 的 digest、请求 UUID 与预期 UID/resourceVersion。收到 409 后需重新读取并对比变更，再执行写入。替换资源的 UID 不能沿用为原编辑基线。响应不明确时保留请求身份进行只读核对，Console 不自动重放写入。Kubernetes 接受与 Operator 投影是不同状态，详见[类型化定义操作](/orbitjob/docs/usage/jobs-runs-functions-and-workflows/#104-create-or-update-a-typed-kubernetes-definition)。

Function 支持创建、编辑、暂停、恢复及无参数调用。创建使用幂等键，更新比较 `expected_version`；仅当活动执行修订已就绪时，Console 才调用已审阅版本。暂停阻止新调用，已有执行继续运行。不支持删除 Function 或任意调用 payload，详见 [Function 管理](/orbitjob/docs/usage/jobs-runs-functions-and-workflows/#105-manage-and-invoke-a-function)。

Workflow schedule 使用五段 cron 与 IANA 时区，并提供服务端预览。新建表单选择 `Forbid`；已有定义省略策略时仍为 `Allow`。暂停停止自动启动，不阻止手动触发或已有执行；错过的启动会跳过。计划触发与 Forbid 冲突时生成终态 Rejected 跳过回执；已接受的手动请求等待于 Pending/ConcurrencyBlocked，不创建步骤。Unknown 子执行阻止新的依赖工作，并保留父级并发占用。详见 [Workflow 操作](/orbitjob/docs/usage/jobs-runs-functions-and-workflows/#106-trigger-observe-and-cancel-a-workflow)。

<a id="inspect-execution-and-logs"></a>

### 查看执行与日志

Jobs 和 Runs 按能力提供来源、namespace、阶段及创建时间筛选。执行计数覆盖完整筛选账本，不受当前页影响。分页表格支持每页 10、20、50 或 100 行；Function 历史使用独立的历史限制。搜索、筛选与分页状态保存在 URL 中，在相关详情之间导航时保留。历史失败是已记录结果，不自动代表尚未解决的事件。

执行详情显示已记录的 attempt 和时间戳。Workflow 历史使用固定修订与明确的 task-to-step 身份。Function 历史包含按 UUID 寻址的终态结果，其 UUID 与数字执行 ID 不同。缺少的元数据保持不可用，不作推断。

一次触发确认创建一个执行意图。重试不明确的响应时保留其幂等键，显式新触发才创建另一个意图。回执在同一连接内跨页面保留，仅链接唯一匹配的执行。观测以两分钟窗口为界，后台标签页暂停并遵守限流；父级历史核对最多读取 1,000 条记录，支持手动刷新。

取消是独立请求。接受请求不代表已终止，也不直接改变观测到的执行阶段。`ExecutionUnknown` 与 `CancelUnknown` 在后端取得足够的自有资源证据前保持未决，详见[执行阶段与取消](/orbitjob/docs/usage/jobs-runs-functions-and-workflows/#103-runs-and-phases)。

Execution logs 从有容量上限的归档提供 attempt/stage 筛选、搜索、暂停、跟随、复制与下载。读取和删除权限独立于执行详情权限。采集缺口、保留与清理见[执行日志](/orbitjob/docs/execution-logs/)。旧版 [attempt 快照](/orbitjob/docs/usage/jobs-runs-functions-and-workflows/#107-read-retained-attempt-logs)依赖仍保留的 Kubernetes 资源，Pod/Job 清理后可能消失。日志访问被拒绝不影响另行获准的执行详情。

<a id="configure-reliability-monitoring"></a>

### 配置可靠性监控

Reliability 侧栏提供 Checks、SLOs 和只读 Alerts。打开 SLOs 后，选择 SLIs 标签管理指标，Objectives 标签管理服务目标。先创建 HTTP Check，再选择获准的 Job、Check 或 Function 作为指标来源，最后创建该指标的目标。Console 以整小时创建滚动窗口，已有日历窗口仍可读取。Check、SLI、SLO 的搜索在服务端分页前完成；Alert 文本搜索只覆盖已加载页。输入或清空该搜索会保留当前页，不重新请求列表；计数区分匹配数与已加载记录数。更改服务端筛选条件会回到第一页。

关闭 Check、SLI、SLO 创建窗口后，同一连接内跨导航保留输入和已选来源；待处理或未知结果仍关联该草稿。成功创建只清除对应草稿，Discard draft 显式重置草稿。丢弃未知结果前需核对资源是否已创建，并再次确认。断开连接会清除这些草稿。

预算按事件计数，剩余值可能为负。预算缺失或允许量为零不证明健康。Alert 保留触发时的 burn，不使用当前目标预算替代。Check 暂停、恢复、删除及 SLO 暂停、恢复、删除需匹配已审阅版本；冲突后刷新并重新审阅，创建结果不明确时先核对再重试。

| 页面 | 读取权限 | 写入权限 |
| --- | --- | --- |
| Checks | `check:List`, `check:Get` | `check:Create`, `check:Pause`, `check:Resume`, `check:Delete` |
| Results | `checkrun:List`, `checkrun:Get` | 无 |
| SLIs | `sli:List`, `sli:Get` | `sli:Create`, `sli:Delete` |
| SLOs | `slo:List`, `slo:Get`, `slo:GetBudget`, `slo:ListBudgets` | `slo:Create`, `slo:Pause`, `slo:Resume`, `slo:Delete` |
| Alerts | `sloalert:List`, `sloalert:Get` | 无 |

资源组 key 保留父 Check/SLO 的作用域。详情读取授权实际父资源；集合权限必须覆盖整个查询域，并遵守 Deny 与权限边界规则。内嵌 SLO 预算与独立预算读取各有权限要求。参阅[监控授权](/orbitjob/docs/security/#console-monitoring-evidence-scope)和 [Check/SLI/SLO API 用法](/orbitjob/docs/usage/checks-slis-and-slos/)。

<a id="manage-access"></a>

### 管理访问权限

Administration 管理账户、工作区成员关系、工作区、策略、资源组与命名 API key。**Accounts and members** 提供邀请、成员角色和全局账户控制；参阅[账户管理](/orbitjob/docs/accounts/#invite-and-manage-members)。必须显式选择管理目标；普通工作负载请求要求在已选工作区中具有活动成员关系。特权访问变更要求在此前 15 分钟内确认过密码，完成确认后重试目标操作。命名选择器辅助选择，ID 和服务端授权决定实际访问。读取与创建权限相互独立。刷新时保留已加载记录并禁用记录操作，直到收到响应，以保持创建表单位置不变。401 或 403 响应后会隐藏不可用记录。

策略可从任务模板或显式 JSON 文档开始。格式化不会授予权限，创建前需审阅 action、resource 与 Deny 规则。仍绑定 key 的策略删除时返回 409；撤销 key 不会解除其策略绑定。Console 不提供策略重新绑定或解绑操作。

新建 key 的明文只显示一次，应在离开会话前存入获准的密钥存储，数据库无法恢复明文。创建结果不明确时，先通过 key 列表和显式撤销核对，再创建另一个 key。丢失 key 后应创建替代凭据并撤销旧凭据；重命名仅修改显示名称。参阅[租户与 key 操作](/orbitjob/docs/usage/tenants-and-api-keys/)。

工作负载权限与可靠性、管理权限相互独立：

| 页面 | 读取权限 | 写入权限 | 资源组 key |
| --- | --- | --- | --- |
| Jobs 与 Runs | `job:List`, `job:Get`, `instance:List`, `instance:Get` | `job:Apply`, `job:Trigger`, `instance:Cancel` | 拒绝 |
| Workflow 定义 | `workflow:List`, `workflow:Get` | `workflow:Apply` | 拒绝 |
| Workflow 执行 | `workflow:List`, `workflow:Get` | 触发与取消均使用 `workflow:Trigger` | 拒绝 |
| Function 与结果 | `function:List`, `function:Get` | `function:Create`, `function:Update`, `function:Invoke` | 仅所属资源组的定义与结果 |

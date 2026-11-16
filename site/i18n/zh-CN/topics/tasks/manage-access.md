# 管理工作区访问权限

Console 使用个人账户，脚本或服务使用命名 API key。两者都需要工作区中的
显式权限。安装管理与工作负载授权具有独立范围。

## 选择所需能力

| 任务 | 所需操作 |
|---|---|
| 查找和查看作业 | `job:List`、`job:Get` |
| 创建或更新类型化作业定义 | `job:Apply` |
| 触发作业 | `job:Trigger` |
| 列出和查看运行 | `instance:List`、`instance:Get` |
| 取消运行 | `instance:Cancel` |
| 读取或下载作业运行日志 | `instance:ReadLogs` |
| 删除作业运行日志 | `instance:DeleteLogs` |
| 创建或更新 Function | 分别为 `function:Create`、`function:Update` |
| 调用 Function | `function:Invoke` |
| 读取或删除 Function 日志 | 分别为 `function:ReadLogs`、`function:DeleteLogs` |
| 创建或更新 Workflow 定义 | `workflow:Apply` |
| 触发或取消 Workflow 运行 | `workflow:Trigger` |
| 暂停执行准入 | 对应来源种类的 `ManageExecution` 操作 |

Check、SLI、SLO、预算和告警的集合读取与详情读取具有独立操作。父定义授权不
隐含所有派生读取。即使另一个策略允许某项操作，显式 Deny 和权限边界仍然生效。

Job、Workflow 和实例接口无法执行分组范围控制时，要求凭据不受资源组限制。
Check、Function 和监控接口解析其已持久化的父分组。请求引用或猜测的资源 ID
不授予任何权限。

## 邀请 Console 用户

工作区管理员打开目标工作区，再打开 **Administration → Accounts**。
选择 Viewer、Operator 或 Administrator，再选择 **Create invitation**。
可以将邀请绑定到用户名。应私下交付一次性链接；链接在 24 小时后过期。

默认 Viewer 使用 `ReadOnlyAccess`。Operator 使用 `JobOperator`，包括作业
触发、运行取消和执行日志读取。Administrator 使用 `TenantAdminAccess`，
包括访问管理，但该预设不包含 Job/Workflow `Apply` 或执行日志读取操作。
这些任务需要额外策略授权。初始管理员和工作区创建者在各自工作区获得显式
`ConsoleOwnerAccess` 策略。

选择角色前，检查实际策略和边界。委派不能超过管理员的有效授权或边界。禁用
成员资格会阻止后续工作区请求。禁用账户和重置密码属于全局账户操作，仅安装
实例管理员可以执行。请参阅[账户管理](../accounts.md#invite-and-manage-members)。

## 签发自动化密钥

使用 **Administration** 创建命名密钥，配置所需最小策略，并在受支持时限定资源组范围。通过浏览器修改访问权限时，要求在此前 15 分钟内完成过密码确认。将明文结果
复制到预期的秘密存储；明文只返回一次。没有授权的密钥可以通过认证，但每个
受保护操作都会被拒绝。

为每个调用方使用专用密钥，使其身份和撤销相互独立。密钥元数据不能恢复秘密。
撤销泄露或不再使用的密钥，并通过已授权管理员签发替代密钥。普通工作负载不得
使用安装引导密钥。

创建响应丢失后，先核对保留的创建状态和密钥元数据，再进行另一次创建。不要
自动签发另一个凭据。Console 在页面内存中保留可用的一次性结果，直到结果被
丢弃或连接被清除；明文丢失后无法恢复。

## 排查访问失败

401 表示认证被拒绝。403 表示操作被拒绝，不能据此认定会话已过期。检查当前
工作区、成员资格状态、精确操作、资源范围、显式 Deny 和权限边界。安装实例
管理员身份本身不授予工作区工作负载访问权限。应识别缺少的预期授权，不能
以扩大密钥范围代替排查。

与 HTTP 授权同时适用的数据库、Kubernetes、会话和日志边界，请参阅
[安全模型](../../SECURITY.md)。

文档字段、资源模式、Deny 优先级和权限边界请参阅
[访问策略参考](../reference/access-policies.md)。

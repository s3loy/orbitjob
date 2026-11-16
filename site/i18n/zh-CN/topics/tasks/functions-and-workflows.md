# 使用 Function 与 Workflow

Function 通过调用 API 提供无参数容器执行。Workflow 协调现有作业组成的图。
两者与普通作业使用相同的 Run、Attempt 和 Kubernetes Job 执行模型。

## 管理并调用 Function

创建 Function 要求 `function:Create`；编辑要求 `function:Update`。
读取、调用和日志操作相互独立。限定分组范围的凭据在自身分组内创建，并访问
已持久化的 Function 父资源。显式 Deny 仍然生效。

打开 **Functions → Create a function**。保存前检查容器镜像、argv、资源、
超时、重试设置以及活动或暂停状态。创建 API 要求
`X-OrbitJob-Idempotency-Key`。在保留的相同操作者及请求身份下，相同规范化
载荷返回原始创建快照，即使 Function 后来已被编辑。同一身份下的输入变化会冲突。

更新替换完整的可变定义，并要求 `expected_version`。再次更新前，读取并审核
并发版本；省略的可选设置解析为创建默认值。已保存定义和可执行活动修订版本分别
具有独立的就绪状态。调用前，应等待活动修订版本匹配预期定义版本。

选择 **Invoke** 请求执行。Console 固定已审核版本。API 调用使用
`POST /api/v1/functions/{id}/invoke`、可选的稳定
`X-OrbitJob-Idempotency-Key`，以及在调用方要求特定定义版本时提供的
`expected_version`。Function 调用不接受任意输入请求体。`wait_seconds`
支持 0 至 60 秒的有界同步等待；等待到期不会取消执行，也不能确定结果。
保留 JobRun 回执并继续观察。

Function 历史包含终态调用投影。仍在运行的调用不一定出现在其中，投影可能在
执行终态提交后出现。Function 历史使用 UUID 运行身份，与实例数字 ID 不同。
通过精确 Function 历史 URL 及 `function:ReadLogs` 读取保留输出。暂停
Function 会阻止新调用，现有执行继续运行。管理 API 不提供 Function 删除操作。

## 创建并运行 Workflow

Workflow 读取要求 `workflow:Get` 和 `workflow:List`。编写还要求
`workflow:Apply`、已授权命名空间，以及不受资源组限制的凭据。
触发和取消使用 `workflow:Trigger`。

打开 **Workflows → Create a workflow**。引用同一命名空间中的 ScheduledJob。
为每个任务设置唯一名称，定义依赖关系并检查图。环或 `dependsOn` 中未知的任务名
会阻止投影。非空 `jobRef` 即使引用不可用的 ScheduledJob，也可以投影成功；
活动 Workflow 修订版本不能证明其引用作业已就绪并可执行。
任务条件可以读取已声明上游的终态阶段、Attempt 数量或终态 Attempt 耗时。
条件不通过时跳过任务，不创建子 Run。

选择 Allow 或 Forbid 并发策略。可选调度使用五字段 cron 和 IANA 时区，默认
UTC。Workflow 调度预览使用相同解析器检查，不创建工作。夏令时缺口会被跳过，
重复的本地时间表示不同的时刻。挂起 Workflow 会暂停自动启动；手动请求仍可用。
使用独立的执行暂停控制冻结新准入。

执行策略版本 1 在父运行准入时解析引用的 ScheduledJob，并固定所有任务修订版本；
引用作业缺失时拒绝准入。旧版 0 在任务开始时解析作业并选择修订版本；
作业缺失时，任务可能被跳过，原因是 `missing_job_reference`。
每个任务使用其引用作业自身的资源、超时和重试预算。Workflow 实际耗时
超时由 Operator 强制实施，因此 Operator 故障可能延迟超时处理。它不替代每个
Attempt 的 Kubernetes 截止时间。

审核类型化定义、对照集群检查并保存。等待目标修订版本就绪，再选择 **Trigger**。
保留 WorkflowRun 回执，并检查父运行和各步骤结果。FailFast 下，步骤终态失败
会停止新工作，已经运行的步骤仍可观察。Continue 允许依赖和条件规则决定符合
条件的工作。执行证据未知的步骤会阻止新的 Workflow 工作；不能假定其已失败、
已取消或可以安全替换。

取消记录停止意图，并将其传播到非终态步骤。父运行等待充分的子运行终止证据。
请参阅[取消运行](cancel-a-run.md)和[执行模型](../concepts/execution-model.md)。

## 恢复不确定的修改

保留请求键、目标版本和已接受资源引用。Function 创建响应丢失时，使用精确的
原始创建请求核对。Function 更新响应丢失时，读取已存储版本并审核，再进行下次
写入。Workflow 类型化保存保留持久化操作身份，并要求 UID/resourceVersion
检查。复用已接受请求会核对该操作；更改内容会产生冲突。

触发或调用响应成功确认发布，不确认执行。历史读取被拒绝不表示原始请求被拒绝。
触发结果未知时，保留原始请求身份，观察或核对后再启动替代工作。

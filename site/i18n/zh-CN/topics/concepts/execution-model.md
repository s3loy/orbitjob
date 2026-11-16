# 执行模型

OrbitJob 通过 Kubernetes Job 执行固定的容器定义。Run 记录一次逻辑执行请求。
其 Attempt 分别记录平台执行尝试，每个 Attempt 绑定一个 Kubernetes Job。
Kubernetes Job 可以根据自身的退避策略创建替代 Pod。

## 定义、修订版本与请求

ScheduledJob 定义容器作业，并可配置 cron 调度。WorkflowJob 定义依赖图，
其中的任务引用同一命名空间中的 ScheduledJob。Function 和 Check 通过
Admin API 存储定义。Operator 将可执行配置投影为不可变修订版本。编辑定义会改变
后续执行选择，不会重写已准入运行所固定的配置。

API 为手动作业请求或 Function 调用发布 JobRun，为工作流请求发布 WorkflowRun。
Operator 准入资源、记录台账并创建工作负载 Job。初始 HTTP 回执包含资源命名空间、
名称和触发标识。它不是台账运行 ID，也不能证明 Pod 已经启动。计划触发项和工作流
步骤在发布前持久化其触发或步骤身份，使中断的发布能够依据原始身份修复。

稳定的幂等键使客户端可以将重复触发与同一触发身份进行核对。保留原始目标修订版本、
键和回执。更改目标修订版本或生成新键可能会请求不同的工作。幂等性依赖于身份留存，
不能保证外部工作负载效果恰好发生一次。

## Run 与 Attempt 生命周期

| 阶段 | 含义 |
|---|---|
| `Pending` | 已准入运行正在等待派发。 |
| `CreatingAttempt` | 持久化创建意图已存在；Kubernetes 创建结果可能仍未确定。 |
| `Running` | 正在观察当前 Attempt。 |
| `RetryWaiting` | 失败的 Attempt 正在等待平台重试或重试耗尽处理。 |
| `CancelRequested` | 已记录停止意图；尚未确认终止。 |
| `ExecutionUnknown` | 执行证据不足以确定执行仍在运行还是已经完成。 |
| `CancelUnknown` | 取消过程尚未建立完整停止证明。 |
| `Succeeded` | 已确认成功完成。 |
| `Failed` | 已完成适用的重试处理，并确认失败。 |
| `Canceled` | 已确认请求的取消。 |

只有 `Succeeded`、`Failed` 和 `Canceled` 是 JobRun 终态。Workflow 父运行
还支持终态 `Rejected`，表示步骤开始前准入已拒绝请求；被跳过的工作流任务没有
子 Run。Job 缺失、观察窗口到期或 API 超时均不能确认终态结果。结果未知的运行
保留其身份和并发名额。OrbitJob 不会仅因资源消失而自动替换执行。平台没有公开的
强制裁定 API。

请求创建 Kubernetes Job 前，Operator 持久化 `CreatingAttempt`。不确定的
创建响应依据该 Attempt 的原始地址和不可变身份进行协调。Operator 校验 JobRun、
Job 和 Pod 的归属；仅匹配资源名称不足以确认。延迟观测到达时，终态结果仍保持最终性。

## 重试与截止时间

`retryPolicy.maxAttempts` 限制平台 Attempt 数，包括首次尝试。省略或为零时
解析为一个 Attempt；负值无效。`jobTemplate.backoffLimit` 设置每个 Attempt
内的 Kubernetes Job 退避次数。Pod 使用 `restartPolicy: Never`，因此失败容器
不会在同一 Pod 内静默重启。新增 Pod 或另一个 Attempt 仍可能重复产生工作负载
的外部效果。启用任一重试机制前，必须确保重复效果是安全的。

执行策略版本 1 在 `backoffLimit > 0` 或 `maxAttempts > 1` 时要求
`retrySafe: true`。它将 CPU、内存和临时存储的请求及限制解析到修订版本中。
工作负载 Pod 使用非 root UID、RuntimeDefault seccomp、禁止权限提升并移除
Linux capabilities，同时禁用 Kubernetes 服务账户令牌挂载。旧版 0 修订版本保留
范围更窄的历史配置契约；编辑定义不会静默升级正在运行的修订版本。

ScheduledJob 的 `timeoutSeconds` 转换为每个 Attempt 的 Kubernetes Job 上的
`activeDeadlineSeconds`。它限制单个 Attempt，并由 Kubernetes 执行，即使
Operator 不可用也生效。多个 Attempt 的总耗时可能超过此值。零值不设置 Job
截止时间。Workflow 自身的超时由 Operator 检查，不具备 Attempt 的 Kubernetes
截止时间在 Operator 故障期间仍可执行的同等保证。

## 并发、暂停与取消

ScheduledJob 的计划准入支持 Allow、Forbid 和 Replace。Forbid 下的手动请求
等待先前活动或结果不确定的执行。计划跳过和错过调度处理具有独立契约。
Replace 在准入替代计划工作前请求停止；它不能将不确定状态视为已确认停止。
Workflow 支持 Allow 和 Forbid。

挂起 ScheduledJob 会阻止计划和手动启动。挂起 WorkflowJob 会暂停自动启动，
手动请求仍然可用。执行控制区分调度暂停、执行暂停和身份删除。执行暂停还会阻止
新的根运行、工作流步骤和尚未开始的派发；可能已经启动的工作仍可观察和取消。
暂停不会取消正在运行的工作。身份删除是不可逆的墓碑标记，与日志清理相互独立。

取消操作发出停止请求，随后等待所属工作负载停止。新工作负载 Pod 通过 Operator
管理的 finalizer 保留终止证据。Operator 要求控制器确认停止，并获得完整的
所属 Pod 终止证据，才会释放证据保留并删除原始 Job。删除时间戳、空 Pod 清单、
节点丢失或强制移除均不能证明工作负载代码已经停止。确认超时保留不确定状态，
后续证据充分时仍可恢复。

## 历史、输出与派生结果

运行台账记录身份、Attempt 和结果。执行归档独立于 Pod 保留，存储有界生命周期
事件和容器 stdout/stderr。归档缺口和采集状态限定了可用输出；日志丢失不会改变
工作负载的业务结果。

终态执行事实先于派生的 Check、Function 和 SLI 记录。待处理投影以幂等方式重试，
并受到常规清理保护。因此，终态运行可能先于派生历史或指标出现。执行历史、
Function 历史和日志保护分别具有独立的保留生命周期。运行边界请参阅
[执行日志](../execution-logs.md)和[架构](../architecture.md)。

# 工作负载资源参考

OrbitJob 使用 `workloads.orbitjob.io/v1alpha1` 资源。应用用户通常通过 Console
或类型化定义 API 编写 ScheduledJob 和 WorkflowJob 定义。直接 Kubernetes
写入方需要显式命名空间 RBAC 和准入授权。符合 schema 的资源仍需成功协调，
才能具有可执行修订版本。

## 定义创建与旧资源

默认类型化定义 API 创建新 ScheduledJob 或 WorkflowJob 时，要求
`executionPolicyVersion: 1`。下文中省略版本或使用 `0` 的说明描述底层 CRD
和已有旧配置；它们不是通过默认类型化 API 创建新定义的有效请求。显式启用的
旧版适配器是独立的安装选择。

## ScheduledJob

| `spec` 中的字段 | 含义 |
| --- | --- |
| `schedule` | 为空或省略表示仅手动。配置的调度使用受支持的五字段 cron 语法。 |
| `executionPolicyVersion` | `1` 接受显式重试和资源策略；省略或为 `0` 时保留旧策略。 |
| `retrySafe` | 版本 1 中，`jobTemplate.backoffLimit > 0` 或 `retryPolicy.maxAttempts > 1` 时必填。声明它不会使外部效果具有幂等性。 |
| `suspend` | 停止自动调度并拒绝手动触发；不取消已经运行的执行。 |
| `concurrencyPolicy` | `Allow`、`Forbid` 或 `Replace`。结果未知的执行可能继续占用 Forbid 名额。 |
| `misfirePolicy` | `Skip`、`FireOnce` 或 `CatchUpBounded`；控制错过的调度触发项。 |
| `timeoutSeconds` | 复制到运行，并作为 `activeDeadlineSeconds` 应用于每个 Attempt 的 Kubernetes Job。它不是多 Attempt Run 的总预算。 |
| `jobTemplate.image` | 必填容器镜像；镜像仓库访问是安装前提。 |
| `jobTemplate.command`、`jobTemplate.args` | 字面参数数组。只有显式选择 shell 作为命令时才使用 shell。 |
| `jobTemplate.backoffLimit` | 一个平台 Attempt 内的 Kubernetes Job 重试预算。 |
| `jobTemplate.resources` | 执行策略支持的 CPU、内存和临时存储请求及限制。 |
| `retryPolicy.maxAttempts` | 平台 Attempt 总数，包括首次。有效省略值为一个 Attempt。 |
| `history.successfulRuns`、`history.failedRuns` | 按结果保留的终态运行。零值选择平台默认值，不表示无限历史。 |

版本 1 规范化全部三个资源维度。当前标准配置使用以下值：

| 资源 | 请求 | 限制 | 接受上限 |
| --- | --- | --- | --- |
| CPU | `100m` | `1` | `16` |
| 内存 | `128Mi` | `256Mi` | `64Gi` |
| 临时存储 | `100Mi` | `1Gi` | `100Gi` |

数量必须为正数，CPU 精度最多为毫核，字节数量必须是整数个字节。请求不能超过
限制。这些是接受边界，不是实测节点容量。已接受修订版本保留其规范化值。
配置原生工具时，参阅[执行策略实现](../../internal/domain/executionpolicy/policy.go)
和[随包分发的 CRD](../../charts/orbitjob/crds/workloads.orbitjob.io_scheduledjobs.yaml)。

类型化模板不接受任意 Kubernetes Pod spec。仅配置受支持字段；任意环境变量、
卷挂载和 Pod 安全设置不是此 API 的导入字段。完整编写流程请参阅
[管理作业](../tasks/manage-jobs.md)。

## WorkflowJob

| `spec` 中的字段 | 含义 |
| --- | --- |
| `executionPolicyVersion` | 版本 `1` 在接受执行前固定引用任务修订版本；省略时保留旧的逐任务选择方式。 |
| `schedule`、`timeZone` | 可选 cron 调度和 IANA 时区。空调度表示仅手动；提供的时区仍必须有效。 |
| `suspend` | 停止自动调度，同时保留手动启动。 |
| `concurrencyPolicy` | `Allow` 或 `Forbid`；WorkflowJob 不支持 `Replace`。 |
| `timeoutSeconds` | 由 Operator 遍历器执行的 Workflow 实际耗时截止时间。Operator 故障会延迟超时处理。 |
| `failPolicy` | `FailFast`（默认值）在步骤进入失败终态后停止创建新步骤；`Continue` 继续按依赖和条件评估其余任务。具体行为见下文。 |
| `tasks` | 非空命名任务列表，引用同命名空间 ScheduledJob。依赖必须能够无环解析。 |
| `history` | 按结果设置的终态工作流历史限制。零值选择默认值。 |

`FailFast` 允许已创建的步骤继续运行。尚无步骤的任务在依赖已确定结果后，以 `fail_fast` 原因标记为跳过；仍等待运行中依赖的任务暂不作判定。任务已可启动但其引用的 ScheduledJob 不可用时，该任务被跳过，`FailFast` 停止创建新步骤。`Continue` 允许任务在依赖结束后运行，包括依赖失败的情况，但必须满足已声明的条件。某个依赖任务被跳过时，依赖它的任务也会被跳过；未声明条件时，依赖失败本身不会使下一任务跳过。

未请求取消 Workflow 时，两种策略都会在所有任务均已确定结果后汇总：如果某个步骤失败，或某个任务因 ScheduledJob 引用不可用而被跳过，Workflow 结果为失败。仅因条件不满足而跳过不会使其失败。执行或取消证据未知时，Workflow 保持非终态。Workflow 取消和截止时间处理独立于 `failPolicy`，会停止新工作并请求取消执行中的步骤。

条件通过阶段比较（`eq`、`ne`、`in`、`not_in`）或数值 `attempts`/`duration_ms`
比较（`lt`、`le`、`gt`、`ge`）读取已声明依赖，不读取任意任务输出，也不构成
通用数据流系统。整数值必须保持精确。请参阅
[WorkflowJob CRD](../../charts/orbitjob/crds/workloads.orbitjob.io_workflowjobs.yaml)
和[Function 与 Workflow 任务](../tasks/functions-and-workflows.md)。

## JobRun 与 WorkflowRun

运行资源表示执行意图和观测。普通用户应通过已授权 API 触发或取消，不应构造
任意运行资源。运行标识来源和触发项，固定执行内容，并记录可信写入方提供的操作者。

`Succeeded`、`Failed` 和 `Canceled` 是 JobRun 终态结果。WorkflowRun 还具有
终态 `Rejected`，表示执行在准入期间被拒绝。`ExecutionUnknown` 和
`CancelUnknown` 保留未解决的证据状态。仅凭 Job 缺失、空 Pod 列表或取消请求
已被接受，均不能确定终止。请参阅[执行模型](../concepts/execution-model.md)。

## 定义接受与就绪

类型化 API 写入使用预演摘要、稳定请求 UUID，以及预期不可变 Kubernetes 身份
和资源版本。准入响应成功表示定义已被接受。触发请求版本前，检查已观察
generation、活动修订版本和就绪状态。写入结果含糊时，使用相同请求身份和当前
状态核对，避免提交不同写入。请参阅[管理作业](../tasks/manage-jobs.md)。

# 入门

OrbitJob 通过 Kubernetes Job 执行容器工作负载。使用已安装的 Console
请求执行并查看结果，或使用 Admin API 和 CLI 实现自动化。安装实例提供执行环境；
工作区为定义、运行和访问策略提供租户边界。

## 选择起点

| 任务 | 起点 |
|---|---|
| 执行团队提供的作业 | [运行第一个作业](tutorials/first-job.md#run-an-existing-job) |
| 创建并运行自己的容器作业 | [创建第一个作业](tutorials/first-job.md#create-a-job-as-an-author) |
| 接入脚本或服务 | [调用 API](tasks/call-api.md) |
| 为团队安装 OrbitJob | [安装 OrbitJob](setup/installation.md) |
| 了解运行、重试和不确定结果 | [执行模型](concepts/execution-model.md) |

## 获取访问权限

向安装实例或工作区管理员索取：

- 可信的 Console 地址，例如 `https://orbitjob.example.com/console/`。
- 工作区邀请，或具有有效成员资格的现有账户。
- 可以运行的作业名称；如果需要编写作业，则索取执行命名空间及创建定义的权限。
- 读取运行的权限，以及需要时读取其执行日志的权限。

使用个人用户名和密码登录。在 **Account** 中选择有效工作区，再选择
**Open workspace**。API key 不能用于登录 Console。API 客户端使用管理员
提供的独立命名密钥，并将其放在授权请求头中。有关邀请、密码、会话和管理员设置，
请参阅[账户](accounts.md)。

仅凭角色名称无法确定全部能力。默认 Operator 预设可以触发现有作业并读取日志。
Viewer 无法触发作业；默认 Administrator 预设不包含 Job/Workflow `Apply`
或执行日志读取操作。作业作者需要显式授予 `job:Apply`，并具有就绪检查和读取权限。
安装实例管理员身份不隐含工作区工作负载访问权限。
[管理访问权限](tasks/manage-access.md)列出了各项任务所需的能力。

## 跟踪一次执行

打开 **Jobs**，选择作业，再选择 **Trigger**。提交前检查作业和命名空间。
保留回执，并通过回执进入运行。回执确认运行请求；Operator 随后异步记录并启动运行。
采用 Forbid 并发策略时，如果先前执行仍处于活动状态或结果不确定，准入可能延迟。

查看运行阶段和 Attempt 记录。`Succeeded`、`Failed` 和 `Canceled` 是已确认的
终态结果。`ExecutionUnknown` 和 `CancelUnknown` 需要继续观察，不能据此认定
代码已经停止。如果具备独立的日志权限，打开 **Execution logs**。缺少输出不能
证明业务执行成功或失败。

后续可参阅[触发与观察](tasks/trigger-and-observe.md)、
[取消运行](tasks/cancel-a-run.md)或[读取日志](tasks/read-logs.md)。

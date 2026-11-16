# 触发并观察运行

触发操作请求执行一次作业的活动修订版本。在请求对应到运行并确认结果前，保留其回执。

## 开始前

需要对作业具有 `job:Get`、`job:List` 和 `job:Trigger`。观察需要独立的
`instance:List` 和 `instance:Get` 授权。作业和实例路由要求凭据不受资源组
限制。日志还需要 `instance:ReadLogs`。如果必须查看执行结果，应在触发前获取
所需访问权限。

作业必须具有已投影的活动修订版本，且不能处于挂起状态。确认其命名空间和可执行
配置，尤其是重试安全性和超时。请参阅[执行模型](../concepts/execution-model.md)。

## 使用 Console

打开 **Jobs**，选择作业，再选择 **Trigger**。检查目标并提交。Console 将操作
与稳定请求键关联，并在当前连接内跨页面导航保留已接受回执。通过回执进入运行，
或打开作业的 **Runs** 标签页。

Forbid 可能使已接受的手动 JobRun 在台账行存在前保持等待。该期间的空历史结果
不表示请求被拒绝。观察还取决于读取授权、所选工作区以及列表过滤器或时间窗口。
将运行视为缺失前，先调整这些条件。

Console 对自动观察设置界限。如果观察暂停，恢复检查或刷新运行。浏览器检查暂停
不会暂停服务端执行。页面内存中的回执在退出登录、收到 401 或身份或工作区
改变时清除。将不确定请求交给管理员处理时，记录命名空间、名称和触发标识。

## 使用 CLI

按照 [CLI 配置](../cli.md#configuration)设置安装实例 API 地址和专用密钥。
一个预期运行保留一个请求身份：

```sh
JOB_ID=42
REQUEST_KEY='invoice-check-2026-11-16-001'
orbitjob jobs get "$JOB_ID"
orbitjob jobs trigger "$JOB_ID" --idempotency-key "$REQUEST_KEY" --json
```

`JOB_ID` 为 `jobs list` 输出的活动修订版本数字 ID；将示例替换为目标作业当前 ID。
触发结果包含 `namespace`、`name`、`occurrence_key`、`trigger`、`phase` 和
`created`，不包含台账运行 ID。Operator 观察前，phase 为空是有效状态。要精确
关联请求，按[调用 API](call-api.md#observe-the-request)所述，以回执中的
`occurrence_key` 调用实例列表 API。CLI 的 `runs list` 支持阶段过滤器，
但不支持触发标识过滤器。

获取实例数字 ID 后：

```sh
RUN_ID=73
orbitjob runs get "$RUN_ID" --json
```

将 `73` 替换为实际记录 ID。检查全部 Attempt 以及 Run 的阶段。
`ExecutionUnknown` 和 `CancelUnknown` 仍是非终态。缺少日志或 Kubernetes
对象不能证明成功、失败或取消。

## 恢复不确定的触发

响应丢失时，保留原始作业修订版本 ID 和请求键。在身份仍被保留期间，使用同一键
再次发送相同触发，以核对同一次触发。新键或不同修订版本可能创建不同工作。不要让
HTTP 库自动重复不带键的触发请求。如果原始目标不可用或其身份已被清理，应先
请管理员核对保留的执行证据，再请求替代工作。

401 表示认证被拒绝；应重新登录或获取有效密钥。403 表示访问被拒绝，不能据此
认定会话已过期。429 需要延迟且有界的重试，并复用原始请求身份。传输故障或
5xx 可能使操作结果不确定。发出新请求前，保留回执或请求身份，并观察或核对。

核对现有触发时，使用相同的认证主体和工作区。个人会话请求与 API key 请求
记录不同操作者；跨这些身份复用键不会转移原始请求的所有权。

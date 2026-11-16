# 调用 Admin API

Admin API 提供经过认证的管理操作和执行历史。自动化使用命名 API key。
安装后的 Console 使用独立的同源个人账户会话；API key 不能用于登录 Console。

## 开始前

获取安装实例的可信 API 基础 URL，以及具有所需操作和资源授权的专用密钥。
除显式配置的回环开发环境外，均使用 HTTPS。示例使用支持 `--fail-with-body`
的 `curl`，并使用 `jq` 查看 JSON。通过常规秘密交付流程提供密钥，不要将其
字面值写入共享 shell 历史：

```sh
set -o pipefail
export ORBITJOB_API_URL='https://orbitjob.example.com'
read -r -s ORBITJOB_API_KEY
export ORBITJOB_API_KEY
```

在 Bash 或 Zsh 中执行这些命令，并为 `read` 输入提供的密钥。响应通过管道
传给 `jq` 或 `tee` 时，`pipefail` 保留 curl 失败的退出状态；继续前检查每条
命令均已成功。密钥保留在授权请求头中，不要加入 URL。打开
`$ORBITJOB_API_URL/openapi.json` 查看安装实例的机器可读契约；源码契约为
[OpenAPI](../../api/openapi.yaml)。

使用无损 JSON 解析保留数字 ID，并遵循每个集合的分页契约。精确 int64 值、
标识符类型、时间窗口和追踪诊断请参阅
[HTTP 客户端约定](../reference/http-conventions.md)。

## 读取可用作业

```sh
curl --silent --show-error --fail-with-body \
  -H "Authorization: Bearer $ORBITJOB_API_KEY" \
  "$ORBITJOB_API_URL/api/v1/jobs?limit=20" | jq .
```

响应的 `items` 是已授权工作区中活动的投影作业修订版本。记录目标条目的数字
`id`、`source_uid`、命名空间和配置。该读取要求 `job:List`；详情要求
`job:Get`。作业和实例路由要求凭据不受资源组限制。

租户凭据始终绑定自身租户。平台凭据必须通过 `tenant_id` 选择已授权目标，并在
每个相关请求上使用同一目标。个人会话请求同样选择当前工作区，并要求有效
成员资格。租户选择器本身从不授予访问权限。

## 触发一次运行

为每次预期执行使用新的幂等键，并在该执行的重试中保留它。重放使用相同的认证
主体和工作区；从个人会话切换到 API key 会改变记录的操作者，并可能产生冲突。
将 `42` 替换为选定的活动作业修订版本 ID：

```sh
JOB_ID=42
REQUEST_KEY='invoice-check-2026-11-16-001'
curl --silent --show-error --fail-with-body \
  -X POST \
  -H "Authorization: Bearer $ORBITJOB_API_KEY" \
  -H "X-OrbitJob-Idempotency-Key: $REQUEST_KEY" \
  "$ORBITJOB_API_URL/api/v1/jobs/$JOB_ID/trigger" | tee trigger-receipt.json
```

触发不接受请求体，并要求 `job:Trigger`。新发布返回 201；核对到现有资源返回
200。JSON 字段为 `namespace`、`name`、`occurrence_key`、`trigger`、`phase`
和 `created`。初始 phase 可以为空。该回执标识请求的 JobRun，不包含台账运行
ID，也不确认 Pod 已启动。

该路由的幂等请求头为可选项，但省略后每次触发都会引入新的随机值，无法安全自动重放。
请求头会去除首尾空白，处理后最多为 128 字节。其触发身份包括租户、源 UID 和
选定修订版本 ID。更改修订版本或键可能请求另一次运行。保留期限制身份可用性，不提供
永久去重保证。回执标识符或错误敏感时，应保护回执。

## 观察请求

查询与回执中的触发标识精确匹配的执行。这需要独立的 `instance:List` 权限：

```sh
OCCURRENCE_KEY=$(jq -er '.occurrence_key | select(type == "string" and length > 0)' trigger-receipt.json)
curl --silent --show-error --fail-with-body --get \
  -H "Authorization: Bearer $ORBITJOB_API_KEY" \
  --data-urlencode "occurrence_key=$OCCURRENCE_KEY" \
  --data-urlencode 'limit=20' \
  "$ORBITJOB_API_URL/api/v1/instances" | jq .
```

新接受的请求在 Operator 准入前可能尚不可见，包括 Forbid 将其置于先前执行之后
等待的情况。按有界间隔观察，等待到期后保留回执。超时不表示请求被拒绝。条目
出现后，使用 `instance:Get` 读取 `/api/v1/instances/{id}`。检查 Run 阶段和
Attempt 记录。只有 `Succeeded`、`Failed` 和 `Canceled` 才能确定 Run 终态结果。

使用 `POST /api/v1/instances/{id}/cancel` 和 `instance:Cancel` 请求停止，
随后继续观察。读取或下载归档需要独立的 `instance:ReadLogs` 操作。请参阅
[取消](cancel-a-run.md)和[日志](read-logs.md)。

## 处理失败

| 结果 | 客户端操作 |
|---|---|
| 400 或 413 | 重试前修正请求或请求体大小。Admin 请求体限制为 1 MiB。 |
| 401 | 认证被拒绝。获取有效凭据；不要将其解释为工作负载结果。 |
| 403 | 检查精确操作、目标、成员资格、Deny 和边界。重复提交无法解决授权问题。 |
| 404 | 核实目标和范围；当前视图中资源不存在，不能证明工作负载代码从未运行。 |
| 409 | 重新加载当前状态，检查契约中的版本或身份冲突。 |
| 429 | 使用带抖动的退避和有界重试预算。保留原始修改身份。 |
| 5xx、传输错误或响应丢失 | 保留原始请求。请求新工作前，先观察或核对其结果。 |

认证和权限存储故障可能返回 503，此时当前凭据仍可能有效，应予保留。响应提供
`Retry-After` 时应遵循，但不要假定每个限流响应都包含该请求头。作业触发和
实例取消默认采用每进程、每租户每秒 5 个请求的预算；普通写入为 10，读取为 100，
各自突发大小与其预算相同。安装设置可以更改这些值。它们不保证工作负载执行吞吐量。

定义保存、Function 创建和带版本更新各有独立身份及并发要求。只有路由定义了
核对行为时，才复用完整保留请求。通用 HTTP 重试策略无法安全重放每种修改。
请参阅[管理作业](manage-jobs.md)和
[Function 与 Workflow](functions-and-workflows.md)。

# HTTP 客户端约定

向安装实例的 Admin API 源地址发送请求。其 `/openapi.json` 描述已部署契约。
[调用任务](../tasks/call-api.md)提供从请求到结果的示例；各操作参考声明参数、
独立授权、请求体和响应。

## 标识符与 JSON 整数

将数字 ID 保留为精确十进制值。JavaScript `Number` 对超过 `9007199254740991`
的整数会丢失精度。将 ID 用于路径前，先使用无损解析器解码响应；将已经舍入的
数字转换为字符串无法恢复原值。Console 传输将不安全整数保留为十进制文本。
Python JSON 整数也保留该精度。

声明为整数的请求字段要求 JSON 整数 token，不能使用带引号的字符串。客户端
可以在内部保留精确十进制表示，并直接序列化为声明的 JSON 类型，无需经过浮点
数转换。比较值、资源版本和修订版本 ID 必须保持精确。

| 标识符 | 获取与使用方式 |
| --- | --- |
| 租户、策略、API key 和分组 ID | 管理响应中的不透明字符串；显示名称或 slug 不是 ID。 |
| Job 或 Workflow 定义 `id` | 已授权列表中的不可变修订版本数字 ID。编辑可能产生新的活动 ID。 |
| `source_uid` | 跨修订版本保持稳定的工作负载来源身份。 |
| 实例 `id` | 执行台账数字 ID，用于实例详情、Attempt、取消和实例日志。 |
| Function `id` | Function 数字身份，与其活动修订版本不同。 |
| Function 历史 `run_id` | 历史 UUID，用于 Function 调用详情和 Function 日志。 |
| Workflow 历史 `id` | 工作流台账数字 ID，与定义 ID 和子 Run ID 不同。 |
| `occurrence_key` | 回执关联身份；不是台账 ID 或 Function 历史 UUID。 |

对应台账行可见前，触发可能已经被接受。保留来源和触发身份，并继续有界观察；
请参阅[触发并观察](../tasks/trigger-and-observe.md)。

## 过滤器与时间窗口

仅使用操作声明的参数。Job 和 Workflow 支持字面名称及命名空间搜索；Function
支持名称及描述搜索。SQL 通配字符不构成客户端模式语言。

实例和 Workflow 历史按台账创建时间筛选，包含 `created_from` 边界，不包含 `created_before` 边界。Function 历史使用调用触发时间。边界使用带时区的 RFC3339，
最多九位小数。相同或逆序的边界窗口会被拒绝。历史按时间戳降序排序，时间相同
时按 ID 降序排序。

查询值使用 URL 编码。例如：

```bash
curl --silent --show-error --fail-with-body --get \
  -H "Authorization: Bearer $ORBITJOB_API_KEY" \
  --data-urlencode 'phase=Succeeded' \
  --data-urlencode 'created_from=2026-11-01T00:00:00Z' \
  --data-urlencode 'created_before=2026-11-16T00:00:00Z' \
  "$ORBITJOB_API_URL/api/v1/instances/summary"
```

实例摘要使用不带分页边界的已授权列表过滤条件，并要求 `instance:List`。
它返回 `total` 和阶段计数，保留实例路由要求凭据不受分组限制的约束。

## 分页

普通偏移分页集合使用声明的 `limit` 和 `offset` 参数。显式发送正数 limit 和
非负 offset；普通页面最多 100 项。多数列表响应包含 `items`，不包含 `total`
或 `has_more`。按收到的项目数推进 offset，遇到不足一页时停止；完整一页可能
需要最后再请求一次空页。

工作区目录默认返回 20 项，并为可见匹配包含 `has_more`，不披露全局总数。
日志归档使用自身游标和 `has_more` 协议；应跟随返回游标，不应用偏移规则。
并发写入可能使偏移页移动。使用稳定 ID 去重，并通过有界时间窗口进行可重复的
历史调查。

## 认证、错误与诊断

自动化使用 `Authorization: Bearer <issued-key>`。Console 使用个人会话 Cookie
及精确 Origin/CSRF 检查。显式 Authorization 请求头优先于 Cookie。账户管理
不能使用 API key；请遵循[账户与会话](../accounts.md)。

标准 Admin 错误封装包含 `error.code`、`error.message` 和可选的 `error.field`。
使用 HTTP 状态和机器可读代码，不要将自然语言消息解析为稳定契约。认证、授权、
校验、冲突、限流和服务不可用需要分别处理；请参阅
[调用 API](../tasks/call-api.md#handle-failures)。

服务转发 `X-Trace-ID`，缺失时生成，并在响应中回显。向运维人员求助时保留该
请求头、状态和错误代码。凭据、能力令牌和敏感响应体不得进入共享请求日志。操作
支持核对时，重试必须保留其原始身份、主体和范围。

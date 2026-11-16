---
title: "认证与请求约定"
---

<a id="8-authentication-and-request-conventions"></a>

<a id="81-choose-an-installation-and-credential"></a>

<a id="choose-an-installation-and-credential"></a>

### 选择安装与凭据

将 API 请求发送到目标安装的 Admin API。该安装的 `GET /openapi.json` 描述实际安装；仓库 [OpenAPI 文档](/orbitjob/openapi.json)描述当前源码。较旧修订的部署可能具有不同契约。

以下示例使用 Bash、curl；从文件组装 JSON 时还需 Python 3。先获取安装管理员签发的专用租户 API key，并通过本地密钥流程加载到 `ORBITJOB_API_KEY`。对于本地 Kind 安装，`source <(make kind-env)` 加载用于初始配置的管理员凭据；日常使用应签发权限更窄的租户 key。将 `ORBITJOB_API` 设为安装管理员提供的 API 来源，包含协议和必要的端口，不加 `/console/` 后缀。本文本地端口转发的来源为 `http://localhost:18080`。通过域名或 IP 访问安装时，使用实际暴露来源，例如 `https://orbitjob.example.com` 或 `http://192.0.2.10:30080`。

```bash
ORBITJOB_API='https://<installation-host>'
AUTH="Authorization: Bearer ${ORBITJOB_API_KEY:?load an issued API key first}"
curl -sS --fail-with-body -H "$AUTH" "$ORBITJOB_API/api/v1/identity"
```

不要将凭据写入共享命令、截图、源文件或请求日志。`AUTH` 是便于 shell 使用的变量，不是服务端环境变量。`/healthz` 返回 `200` 仅确认公开健康端点；`/api/v1/identity` 才验证 key。

公开端点无需凭据：

```text
GET /healthz
GET /metrics
GET /openapi.json
```

Chart 可选的 Ingress 转发 `/openapi.json`，但排除 `/healthz` 和 `/metrics`。运维端点应通过内部 Service 或独立受控的管理路径访问。

以下自动化示例使用已签发的 API key 认证：

```http
Authorization: Bearer otj_...
```

OpenAPI security scheme 名为 `APIKey`，传输格式是上述 Bearer header。服务端仅保存 bcrypt 哈希。未知、过期或已撤销的 key 返回 `401`。

已安装的 Console 通过具有 HttpOnly、SameSite=Strict 属性的 cookie 使用个人账户会话。浏览器提交凭据和能力令牌时必须提供配置的 Origin；cookie 认证写入还要求当前会话的 `X-CSRF-Token`。显式 `Authorization` 优先，绝不回退使用会话 cookie。个人账户管理使用 `/auth/*` 路由，不能通过 API key 执行。Setup、邀请、会话过期和管理员恢复见[账户与工作区访问](/orbitjob/docs/accounts/)。

不提供实验凭据、前台实验 API 或 OAuth。下文身份能力检查结果描述策略决策，不是凭据。

<a id="82-tenant-group-and-permission-scope"></a>

<a id="tenant-group-and-permission-scope"></a>

### 租户、资源组与权限范围

租户 key 不能通过查询或请求体中的 `tenant_id` 切换其认证租户。平台 key 访问租户数据时必须显式选择 `tenant_id`；授权检查和数据访问使用相同目标。租户对象管理路由继续授权路径中显式指定的租户。

平台 key 必须在每个租户数据请求的查询字符串中加入 `tenant_id`，包括详情、触发、取消和定义请求。值必须是获准目录或创建操作返回的准确目标租户 ID。不要将其放入未声明 `tenant_id` 字段的请求体，也不要使用不存在的 `X-OrbitJob-Tenant-Id` header。以下租户 key 示例省略该查询，因为凭据已经固定租户。

每个操作都需要对 `orbitjob:<tenant>:<group-or-*>:<resource-type>/<id-or-*>` 具有相应 action。列表、读取、写入和日志访问相互独立。已认证但无策略的 key 不授予任何受保护操作。显式 Deny 和权限边界仍然生效。

| 流程 | 所需 action |
|---|---|
| 读取 Job 定义和执行历史 | `job:List`, `job:Get`, `instance:List`, `instance:Get` |
| 发布和取消 Job 执行 | `job:Trigger`, `instance:Cancel` |
| 读取／校验并应用在线工作负载 spec | `job:Get` / `job:Apply`，或 `workflow:Get` / `workflow:Apply` |
| 管理／调用 Function | `function:Create`, `function:Update` / `function:Invoke`；读取需要 `function:List`, `function:Get` |
| 读取／触发及取消 Workflow | `workflow:List`, `workflow:Get` / `workflow:Trigger` |
| 读取保留的任务日志 | `instance:ReadLogs` 或 `function:ReadLogs` |

Jobs、Instances、Workflows 和类型化定义路由要求未绑定资源组的凭据。Function 支持凭据所属的资源组。集合授权不会扩大资源组限制。监控和访问管理分别保留自己的 action 与作用域检查；参阅各参考操作。

`GET /api/v1/identity` 返回不含秘密的 key 类型、key ID、租户、资源组和权限边界标识。平台 key 未选择目标时，仅获得元数据，没有能力检查结果。最多可提供 64 对 `action` 和 `resource` 查询值，以检查所选租户内的准确策略决策。该操作不枚举资源，也不披露策略文档：

```bash
curl -sS -G "$ORBITJOB_API/api/v1/identity" -H "$AUTH" \
  --data-urlencode "tenant_id=$TENANT_ID" \
  --data-urlencode 'action=job:Get' \
  --data-urlencode "resource=orbitjob:$TENANT_ID:*:job/42"
```

身份响应可以包含工作区标签和配置的工作负载 namespace。标签和 namespace 映射属于元数据，不证明集群就绪或具有 Apply 权限。`/api/v1/workspaces` 搜索获准的工作区标签，不会切换租户 key 或授予工作负载访问权。

<a id="83-ids-filters-and-pagination"></a>

<a id="ids-filters-and-pagination"></a>

### ID、筛选与分页

客户端必须将 int64 ID 保留为准确的十进制文本。JavaScript `Number` 在超过 `9007199254740991` 后丢失整数精度；构造路径前应使用无损 JSON 解析器处理响应。将已经舍入的数字转换成字符串不能恢复其 ID。Python JSON 整数和带引号的 shell 变量可以保留这些值。API 中声明为整数的请求字段仍需 JSON 整数 token，不能用带引号的字符串。

| 标识 | 获取位置／用途 |
|---|---|
| Tenant、key、policy 和 group ID | 管理响应中的不透明字符串，不是显示名或 slug |
| Job / Workflow 定义 `id` | 列表中的数字不可变修订 ID；编辑可能产生新的活动 ID |
| `source_uid` | 稳定的工作负载来源身份，用于查找同一来源的修订或执行 |
| Instance `id` | 数字账本执行 ID，用于 Instance 详情、attempt、日志和取消 |
| Function `id` | 数字 Function 身份，独立于其活动修订 ID |
| Function 历史 `run_id` | Function 历史中的 UUID，用于历史详情和日志 |
| Workflow 历史 `id` | 数字 Workflow 账本执行 ID，独立于定义 ID 和步骤执行 ID |
| 触发回执 `occurrence_key` | 回执关联值，不是账本 ID 或 Function 历史 UUID |

Job 和 Workflow 支持对名称／namespace 做字面 `search`；Function 对名称／描述搜索。Instance 与 Workflow 历史以账本创建时间应用 `created_from`（包含）和 `created_before`（不包含）。Function 历史使用调用触发时间，并接受 `offset`。所有边界要求带时区的 RFC3339，最多九位小数；空窗口或反向窗口会被拒绝。历史按时间戳降序排列，同一时间戳按 ID 降序排列。

```bash
curl -sS -G "$ORBITJOB_API/api/v1/instances/summary" -H "$AUTH" \
  --data-urlencode 'phase=Succeeded' \
  --data-urlencode 'created_from=2026-10-01T00:00:00Z' \
  --data-urlencode 'created_before=2026-11-01T00:00:00Z'
```

汇总对与 Instances 列表相同的获准筛选返回 `total` 和 `phases`，不受分页边界限制。需要 `instance:List`，并保留列表对未绑定资源组 key 的限制。

可选追踪 header：

```http
X-Trace-ID: request-20260713-001
```

服务会回显该值；缺少时自行生成。为运维诊断保留此响应 header，以及 HTTP 状态和错误 code。

列表 API 在参考文档声明的位置使用 `limit`/`offset`。显式传入正数 limit 和非负 offset；普通分页列表的 `limit` 上限为 100。工作区目录默认 20，并返回 `has_more`。多数其他集合仅返回 `{"items":[...]}`，不要自行假设具有 `total` 或 `has_more`。这类集合按实际收到的页长度推进，遇到短页停止；完整页可能还需一次空请求。并发写入可能改变 offset 分页，因此应按稳定 ID 去重，并使用时间边界进行可重复的历史调查。Instance `/summary` 是独立计数操作，不是列表分页字段。

---
title: "租户与 API key"
---

<a id="9-tenants-and-api-keys"></a>

租户包含显示名 `name`、便于阅读的工作区引用 `slug`，以及用于 API 寻址的不可变 ID。Console 优先显示名称和 slug，完整 ID 保留在详情中。名称或 slug 不改变租户作用域。创建租户不会同时配置执行环境；需按 [namespace 拓扑](/orbitjob/docs/usage/helm-and-kubernetes/#71-namespace-topology) 配置 namespace 映射与权限。

创建租户：

```bash
curl -sS -X POST "$ORBITJOB_API/api/v1/tenants" \
  -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"slug":"payments","name":"Payments","status":"active"}'
```

查询：

```bash
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/tenants?limit=50&offset=0"
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/tenants/<tenant_id>"
```

按名称或 slug 查找工作区，无需记住 ID：

```bash
curl -sS --get -H "$AUTH" "$ORBITJOB_API/api/v1/workspaces" \
  --data-urlencode 'search=Payments' --data 'limit=20' --data 'offset=0'
```

目录在 `items` 中返回 `id`、`name`、`slug`、`status`，并用 `has_more` 表示调用者可见结果是否还有下一页。搜索会去除两端空白，再对名称或 slug 做字面子串匹配，最长 128 个字符。结果按名称/ID 稳定排序；授权和搜索先于分页。默认 limit 为 20，上限为 100，不暴露全局租户总数。读取自己工作区的标签不需要租户管理权限；其他工作区需要对 `orbitjob:<target>:*:tenant/<target>` 具备有效的 `tenant:Get`。显式 Deny、权限边界和资源组作用域仍然生效。平台凭据没有隐含的所属工作区。目录不接受 scope override，也不授予所列工作区的工作负载或管理访问权限。

在已安装的 Console 中，Connection 选择已登录账户的一项活动工作区成员关系，并在打开工作负载前验证身份。仅有安装管理员身份不会创建工作区成员关系，也不授予工作负载访问权。在 Administration 中按名称选择获准工作区，会改变访问管理目标，不会切换工作负载成员关系。后续每个操作仍独立检查相应授权。自动化使用平台 key 时显式选择目标租户；租户 key 始终限制在自身租户内。

创建有名称的 API key 时，先选好其工作需要的策略。将 `<policy_id>` 替换为获授权策略列表返回的 ID；Console 提供名称选择器，无需手输 ID。

Administration 选中的管理工作区同时用于 policy/group 目录、新建 policy/group 和 API key。切换时会清除上一目标的授权选择；未完成的 key 草稿按工作区分别保留。该管理选择不会改变凭据用于普通工作负载查询的工作区。

安装管理员接入其他工作区时使用下列显式管理接口。每项都需要对准确目标集合具备已有的 `policy:List`、`policy:Create`、`group:List` 或 `group:Create` 授权。服务端先检查调用者有效授权，再进入目标作用域；自定义策略授权不得超出调用者的有效权限，显式 Deny 和边界也计入限制。普通 `/policies` 与 `/resource_groups` 查询保持原作用域。

```bash
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/tenants/<tenant_id>/policies"
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/tenants/<tenant_id>/resource_groups"
curl -sS -X POST "$ORBITJOB_API/api/v1/tenants/<tenant_id>/policies" \
  -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"name":"read-job-history","document":{"version":"2026-01-01","statement":[{"effect":"Allow","action":["job:List","job:Get","instance:List","instance:Get"],"resource":["orbitjob:self:*:job/*","orbitjob:self:*:instance/*"]}]}}'
curl -sS -X POST "$ORBITJOB_API/api/v1/tenants/<tenant_id>/resource_groups" \
  -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"slug":"payments-services","name":"Payments services"}'
```

```bash
curl -sS -X POST "$ORBITJOB_API/api/v1/tenants/<tenant_id>/api_keys" \
  -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"name":"payments-export-worker","policies":["<policy_id>"]}'
```

可选 name 只是显示元数据。未传 `name` 的现有请求保持兼容；空 `{}` 仍会创建不带 policy 授权的凭据，因此可以认证，但不能使用受保护能力。可选 boundary/group 字段见 [OpenAPI schema](/orbitjob/openapi.json)。创建响应的 `key` 明文只显示一次，请立即保存。列表接口返回名称、key 前缀等元数据，不返回明文。

```bash
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/tenants/<tenant_id>/api_keys"
curl -sS -X POST -H "$AUTH" "$ORBITJOB_API/api/v1/api_keys/<key_id>/revoke"
```

为 active key 改名，不轮换 Secret，也不改变授权：

```bash
curl -sS -X PATCH "$ORBITJOB_API/api/v1/api_keys/<key_id>" \
  -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"name":"payments-export-production"}'
```

响应为 `{"id":"<key_id>","name":"payments-export-production"}`。此操作需要对 key 实际所属租户和作用域具备 `apikey:Update`。将 name 设为空字符串会清除名称，不会签发替代明文、更新 policy 绑定或重新激活已撤销 key。

丢失 key 明文时，应申请带所需策略的新 key，将使用者或服务切换过去，再撤销旧凭据。可通过名称、前缀、创建时间和 ID 找到旧记录；标签不能恢复 Secret，也不能证明持有 Secret。如果旧 key 可能泄露，应及时撤销，并与安装负责人协调替换。

管理接口执行明确的 action/resource policy 检查。没有授权的 key 可以认证，却不能使用受保护能力。撤销操作针对目标 key 的实际所属租户与 ID 检查 `apikey:Revoke`。租户凭据不能撤销其他租户的 key；目标不属于自己与目标不存在返回相同拒绝。平台凭据仍需要对准确目标具备匹配授权。不要把 bootstrap key 提供给普通业务调用者；应使用具备所需授权的专用租户 key，并在不可信网络前配置网关 ACL。

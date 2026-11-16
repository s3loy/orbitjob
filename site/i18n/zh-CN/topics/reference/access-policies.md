# 访问策略参考

策略将操作授权与资源模式组合，不会创建工作区成员资格、配置 Kubernetes
命名空间或建立数据库连接。个人成员和自动化密钥的完整流程请参阅
[管理访问权限](../tasks/manage-access.md)。

## 文档与语句

策略文档接受 `version` 字符串，并要求非空 `statement` 数组。每个语句具有
`effect`（`Allow` 或 `Deny`）、非空 `action` 数组和非空 `resource` 数组。
字段名称是精确的小写名称；未知字段、重复键和不受支持的文档结构均被拒绝。
没有任意条件语言字段。

以下只读模式展示文档结构。创建策略前，将租户替换为实际目标：

```json
{
  "version": "1",
  "statement": [
    {
      "effect": "Allow",
      "action": ["job:List", "job:Get", "instance:List", "instance:Get"],
      "resource": [
        "orbitjob:YOUR_TENANT:*:job/*",
        "orbitjob:YOUR_TENANT:*:instance/*"
      ]
    }
  ]
}
```

此示例不授予执行或日志访问。每个操作使用其声明的 action；`job:Trigger`、
`instance:Cancel`、`instance:ReadLogs` 和 `instance:DeleteLogs` 是独立能力。
租户编写的策略使用受支持的精确操作。平台预设可以包含通配操作；租户不能通过
复制此类文档获得平台权限。

## 资源模式

资源格式为 `orbitjob:<tenant>:<group>:<type>/<id>`，每个路径段都必须存在。
`*` 匹配整个路径段，不是子字符串模式。旧式空分组段也匹配所有分组。
操作支持时，新策略应使用显式分组模式。

服务端根据认证身份和持久化资源归属解析目标范围。调用方不能通过提供任意目标
ARN 绕过路由解析器。不支持分组限制的路由会拒绝绑定分组的凭据，包括普通
Job/Instance/Workflow 和类型化定义操作。Function 具有自身持久化分组范围。

## 评估与委派

没有适用的 Allow 即拒绝。适用的 Deny 优先于 Allow。权限边界限制有效授权，
本身不授予操作。策略、成员资格和密钥委派必须保持在调用方有效权限和权限边界
之内。含糊或无法证明的交集会被拒绝。

平台凭据在租户数据路由上必须选择已授权目标租户。租户凭据始终绑定自身租户。
工作区发现和安装实例管理员身份不隐含工作负载能力。检查每个操作的 action
和实际父资源；使用 `/api/v1/identity` 查看认证身份和受支持的权限检查。
允许的权限判定不能证明资源存在。

委派策略或 API key 管理前，应阅读[安全边界](../../SECURITY.md)。操作被拒绝
后，重新检查显式 Deny、资源组、权限边界和所选工作区；更改角色标签无法绕过
这些限制。

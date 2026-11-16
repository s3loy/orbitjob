# Console 账户与工作区访问

Console 用户使用个人用户名和密码登录。脚本和服务使用独立的命名 API key，
放在 `Authorization: Bearer <key>` 中。API key 包括安装引导密钥，均不能用于
登录 Console。个人账户具有独立于 API key ID 的稳定用户 ID。

普通用户从[登录](#sign-in-and-choose-a-workspace)开始。安装运维人员创建首个
管理员并执行带外恢复。工作区管理员邀请成员并分配工作负载权限。当前源码未实现
OIDC、通行密钥、邮件交付或多因素认证。

这些流程要求迁移 `0027_personal_accounts.up.sql`，以及匹配的 Admin 镜像和
chart。更新检出目录不会更新正在运行的安装实例。在现有安装实例启用账户前，
遵循[账户迁移流程](database-setup.md#schema-privileges-and-account-changes)。

## 创建首个管理员

安装运维人员需要具备在已安装 Admin 容器中执行身份命令的 Kubernetes 权限。
该容器的 `ADMIN_DSN` 必须使用 `orbitjob_admin` 数据库角色。先完成数据库
引导和全部迁移；Default 工作区必须存在且处于活动状态。按照
[HTTPS 与本地开发](#https-and-local-development)配置精确 Console 源地址（origin）。

确认目标集群和上下文，设置安装命名空间，并使用其实际公开源地址：

```sh
INSTALLATION_NAMESPACE='orbitjob-system'
kubectl -n "$INSTALLATION_NAMESPACE" exec deploy/orbitjob-admin-api -c admin-api -- \
  /orbitjob-identity -action setup -origin https://orbitjob.example.com
```

对于显式配置的本地开发，源地址可以为 `http://localhost:18080`。源地址仅包含
协议、主机和可选端口，不包含路径或末尾斜杠。该命令也可以在 Kubernetes 外使用相同构建
及私下提供的 `ADMIN_DSN` 运行。省略 `-origin` 时，读取 `CONSOLE_PUBLIC_ORIGIN`。

私有 JSON 结果包含 `id`、`token`、`expires_at`，提供源地址时还包含 `url`。
链接格式为 `https://orbitjob.example.com/console/connection#setup=<token>`。
默认有效期为 15 分钟；`-expires-in` 接受不超过一小时的正时长。签发设置或恢复
能力令牌会使先前未使用的安装能力令牌失效。数据库存储令牌哈希。

通过已批准的私有通道交付结果。在可信浏览器中打开链接，输入用户名、可选显示
名称和密码，再选择 **Create administrator account**。

用户名包含 3–64 个 ASCII 字母、数字、点、连字符或下划线，以字母或数字开头，
并规范化为小写。密码至少 15 个字符，最多 1,024 个 UTF-8 字节。本地常见密码
阻止列表中的密码和短重复模式会被拒绝。使用唯一密码。

设置在同一事务中消费能力令牌、创建安装实例管理员，并授予 Default 中的显式所有者
成员资格。随后 Console 登录该账户。即使之后移除账户行，安装实例仍保持已认领；
设置不会自动重新开放。没有安装能力令牌或邀请时，不提供开放注册。

Console 从地址栏移除链接片段，并将令牌保存在页面内存中。重新加载或选择
**Discard link** 会清除该副本。未使用的能力令牌在到期或被替换前仍然有效。

## 登录并选择工作区

打开安装实例可信的 `/console/` 地址。输入用户名和密码，选择 **Sign in**。
尚无账户或有效成员资格时，向工作区管理员获取邀请。

**Keep me signed in for 7 days** 默认选中，创建持久化 cookie 和七天绝对有效期
的服务端会话。取消勾选会创建浏览器会话 cookie，服务端绝对有效期为八小时。
请求和轮询均不延长有效期。到期后重新登录。浏览器会话恢复可能保留会话 cookie；
共享设备上应显式退出登录。

在 **Account** 中选择活动工作区，再选择 **Open workspace**。每个工作负载
请求都要求当前有效成员资格和有效策略授权。安装实例管理员身份允许管理全局
账户和工作区元数据，不自动授予工作负载访问权限。创建工作区会给予创建者显式
所有者成员资格，但执行仍需要安装命名空间映射和 Kubernetes RBAC。运行工作前，
请安装运维人员完成配置。

浏览器在 `sessionStorage` 中存储不含秘密的已选工作区 ID 和未确认的源控制意图。
保留的源控制意图按已认证主体、工作区和源隔离，使同一账户可以在重新连接后审核。
退出登录、收到 401 或身份或工作区改变时，受保护的内存查询和操作状态会清除。
403 拒绝操作，不能据此认定会话已过期。

Job、Workflow 和 Function 编写草稿仅保存在当前连接的页面内存中。重新加载、
结束或更改连接会清除草稿；当前不提供服务端私有草稿持久化和恢复功能。
执行这些操作前，应将未完成内容保存在 Console 之外。已接受的定义保存和
Function 创建操作保留操作者及请求身份，以供重新连接后核对；其回执不能恢复
尚未保存的编写草稿。

## 邀请和管理成员

工作区管理员打开目标工作区，进入 **Administration → Accounts**。选择角色，
可选绑定邀请用户名，再选择 **Create invitation**。默认预设存在以下差异：

| 邀请角色 | 默认策略 | 工作负载权限含义 |
|---|---|---|
| Viewer | `ReadOnlyAccess` | 预设中的读取操作；不授予触发或日志读取。 |
| Operator | `JobOperator` | 现有工作负载读取、作业和工作流触发、运行取消、Function 调用及日志读取；不包含 Job/Workflow Apply。 |
| Administrator | `TenantAdminAccess` | 工作区访问管理及已配置管理操作；不授予 Job/Workflow Apply 或执行日志读取。 |

初始管理员和每个工作区创建者在各自工作区获得显式 `ConsoleOwnerAccess`
策略，包括编写和日志操作。自定义策略、显式 Deny 和权限边界可以进一步改变
有效访问权限。向成员说明角色可以执行某项任务前，应检查实际授权。委派不能
超过管理员的有效授权或权限边界。[管理访问权限](tasks/manage-access.md)
列出了常见任务对应的操作。

复制一次性 **Invitation link** 并私下交付。链接 24 小时后过期，仅适用于记录的
工作区。新用户通过链接创建账户。现有用户选择 **I already have an account**，
登录并使用该账户的有效会话接受邀请。绑定用户名的邀请要求用户名匹配。邀请
不会替换现有密码。已过期、已消费和不匹配的链接会被拒绝。

成员表可以更改角色或禁用成员资格。禁用成员资格会阻止对该工作区的后续请求。
在 **Installation accounts** 中，安装实例管理员可以更改全局账户状态和安装
实例管理员角色。禁用账户会撤销其会话。最后一个活动安装实例管理员不能被禁用
或降级。

访问变更要求在此前 15 分钟内完成过密码确认。出现提示时，完成
**Confirm your password**，再重试预期操作。相同要求也保护通过浏览器会话执行
的 API key、策略和资源组写入。每个请求重新检查当前账户、成员资格和策略
状态，因此撤销和角色变更会影响后续请求。

## 更改或重置密码

在 **Account → Change password** 中提供当前密码和新密码，选择
**Change password and sign out**。成功后撤销该账户的全部会话，包括当前
浏览器会话。随后重新登录。

安装实例管理员近期完成密码确认后，可以为账户选择 **Create password reset link**。
该操作更改全局身份；仅有工作区管理员身份不能签发。链接仅显示一次、仅可使用
一次，一小时后过期。签发新重置链接会使该账户先前未消费的重置失效。成功重置
会撤销该账户的全部会话。系统不提供邮件重置服务。

在 **Account** 中展开 **Signed-in sessions**，查看到期时间并撤销会话。
撤销当前会话会使本浏览器退出登录。离开共享设备前退出登录，即使已取消记住
登录选项也应如此。

## 恢复安装管理权限

没有安装实例管理员可以登录时，具有安装实例 Admin 数据库访问权限的运维人员
可以签发带外恢复能力令牌：

```sh
INSTALLATION_NAMESPACE='orbitjob-system'
kubectl -n "$INSTALLATION_NAMESPACE" exec deploy/orbitjob-admin-api -c admin-api -- \
  /orbitjob-identity -action recovery -username administrator \
  -origin https://orbitjob.example.com
```

将 `administrator` 和源地址替换为目标账户及可信安装实例源地址。
`-username` 为可选项；提供时限制可消费该能力令牌的用户名。设置表单和有效期控制与初始设置相同。

恢复要求安装实例已经认领，且 Default 工作区处于活动状态。它可以重新激活现有
账户、重置密码并恢复安装实例管理员身份，也可以创建替代账户。它授予 Default
中的显式所有者成员资格，并撤销被恢复账户的现有会话。应将命令输出作为安装
凭据处理。旧 API key 不能证明个人账户所有权，也不能重新开放设置。

## HTTPS 与本地开发

生产使用 `__Host-orbitjob_session`，带有 `HttpOnly`、`Secure`、
`SameSite=Strict`、`Path=/`，且不设置 `Domain`。PostgreSQL 存储会话秘密值
的哈希。独立 CSRF 令牌保存在页面内存中。通过 cookie 认证的写入要求 `Origin`
与允许的源地址精确匹配，并携带 `X-CSRF-Token`；登录和能力令牌提交也检查 Origin。
显式无效的 Bearer 请求头会失败，不回退到会话 cookie。

将 `CONSOLE_PUBLIC_ORIGIN` 或 Helm `adminapi.console.publicOrigin` 设置为
精确公开 HTTPS 源地址。省略时，Admin 要求 `https://` 加请求 Host。反向代理
必须保留公开 Host。转发请求头不选择可信源地址，也不放宽 cookie 安全。地址改变时
检查此设置，无需重新构建前端。

`CONSOLE_ALLOW_INSECURE_LOCALHOST` 默认为 `false`。显式 HTTP 开发使用
`true` 或 Helm `adminapi.console.allowInsecureLocalhost: true`，仅允许
`localhost`、`127.0.0.1` 和 `::1` 等回环主机。它使用不带 `Secure` 的
`orbitjob_session_local`。节点 IP 或公开 HTTP 地址不能通过此例外登录。
使用前端开发代理时，保持浏览器 Host 和 Origin 一致，并将 `publicOrigin`
留空或设为前端源地址。请参阅[前端开发](local-deployment.md#change-check-and-redeploy)。

HttpOnly cookie 仍对其持有者授权，同源脚本执行可以通过它执行操作。通过 HTTPS
提供 Console，保留其 CSP，并保护管理员设备。密码哈希使用 Argon2id，内存为
64 MiB、迭代三次、并行度为一。API key 继续使用独立的 bcrypt 校验和自动化策略。

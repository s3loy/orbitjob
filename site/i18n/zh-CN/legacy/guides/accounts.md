---
title: "账户与工作区访问"
---

已安装的 Console 使用个人用户名和密码登录。HTTP 客户端和脚本继续使用命名 API key，并通过 `Authorization: Bearer <key>` 认证。API key（包括 bootstrap key）不能登录 Console。账户具有独立于 API key ID 的稳定用户 ID。当前源码未实现 OIDC、通行密钥、邮件投递或多因素认证。

本说明适用于包含迁移 `0027_personal_accounts.up.sql` 及其配套 Admin 镜像和 chart 的源码构建。更新源码文件不会升级运行中的安装。所需发布边界见[数据库兼容性](/orbitjob/docs/database/#personal-account-migration)。

<a id="create-the-first-administrator"></a>

## 创建首个管理员

安装人员需要 Kubernetes 访问权限，以便在已安装的 Admin 容器中执行命令；该容器的 `ADMIN_DSN` 使用 `orbitjob_admin` 数据库角色。必须先完成数据库 bootstrap 和全部迁移，且 Default 工作区必须存在并处于活动状态。按 [HTTPS 与本地开发](/orbitjob/docs/accounts/#https-and-local-development)配置浏览器来源。

确认 Kubernetes context，将示例 HTTPS 来源替换为安装的实际来源，然后运行：

```bash
INSTALLATION_NAMESPACE='orbitjob-system'
kubectl -n "$INSTALLATION_NAMESPACE" exec deploy/orbitjob-admin-api -c admin-api -- \
  /orbitjob-identity -action setup -origin https://orbitjob.example.com
```

对于已显式配置的本地开发安装，改用 `-origin http://localhost:18080`。来源仅包含协议、主机和可选端口，不含路径或末尾斜杠。也可以在 Kubernetes 之外使用相同构建和私下提供的 `ADMIN_DSN` 运行该命令。命令接受 `-origin`，未指定时使用 `CONSOLE_PUBLIC_ORIGIN`。

私有 JSON 结果包含 `id`、`token`、`expires_at`；提供来源时还包含 `url`。URL 格式为 `https://orbitjob.example.com/console/connection#setup=<token>`。链接默认在 15 分钟后过期；`-expires-in` 可指定不超过一小时的正时长。签发新的 setup 或 recovery 链接会使此前尚未使用的安装链接失效。数据库仅保存 token 哈希。

在可信浏览器中打开 URL，输入用户名、可选显示名和密码，然后选择 **Create administrator account**。用户名包含 3 至 64 个 ASCII 字母、数字、点、连字符或下划线，必须以字母或数字开头，并统一转换为小写。密码至少包含 15 个字符，且不超过 1,024 个 UTF-8 字节。系统拒绝常见密码和短重复模式。使用密码管理器生成的唯一密码。

服务端在同一事务中消费链接、创建安装管理员，并在 Default 中显式授予具有 owner 策略的管理员成员关系，随后 Console 登录该账户。即使之后删除账户行，安装标记仍保持已认领状态，setup 不会自动重新开放。注册必须持有安装能力令牌或邀请。

将命令输出视为凭据，仅通过获准的私密渠道传递。Console 会从地址栏移除 fragment，并将 token 保留在页面内存中。重新加载页面或选择 **Discard link** 会清除该副本；尚未使用的链接在过期或被替换前仍可使用。

<a id="sign-in-and-choose-a-workspace"></a>

## 登录并选择工作区

打开安装的 `/console/` 地址，输入用户名和密码，然后选择 **Sign in**。Console 的账户请求和工作负载请求使用相同来源。尚无账户或成员关系时，向工作区管理员申请邀请。

**Keep me signed in for 7 days** 默认选中，创建持久 cookie，服务端绝对有效期为七天。取消勾选后创建浏览器会话 cookie，服务端绝对有效期为八小时。浏览器恢复会话时可能保留会话 cookie；在共享设备上应显式退出登录。轮询和普通请求均不会延长有效期。过期后需重新登录。

在 **Your account** 下选择活动工作区，然后选择 **Open selected workspace**。工作负载访问需要活动成员关系及其有效策略授权。安装管理员身份允许管理全局账户和工作区元数据，不会自动授予工作区工作负载访问权。创建工作区会向创建者显式授予 owner 成员关系。新工作区还需配置执行 namespace 映射和 Kubernetes RBAC，任务才能运行；参阅 [namespace 拓扑](/orbitjob/docs/usage/helm-and-kubernetes/#71-namespace-topology)。

浏览器仅在 `sessionStorage` 中保存不含秘密的已选工作区 ID。退出登录、收到 401 或身份／工作区变更时，会清除受保护的查询状态和操作回执。同一用户重新连接时保留稳定 actor ID，用于核对服务端持久化的草稿和请求。403 表示操作被拒绝，不能据此判定会话已过期。

<a id="invite-and-manage-members"></a>

## 邀请与管理成员

工作区管理员打开 **Administration → Accounts and members**，选择目标工作区，选择 **Viewer**、**Operator** 或 **Administrator**，然后选择 **Create invitation**。可以通过用户名将邀请绑定到一个账户。审阅所分配的角色和策略范围；委派不得超出管理员的有效授权或权限边界。

复制一次性 **Invitation link** 并私下交付。邀请在 24 小时后过期，仅授予其所记录工作区的成员关系。创建新账户的接收者输入用户名和密码；已有账户的接收者选择 **I already have an account**，以受邀用户名登录后接受邀请。已登录时，应先核对显示的账户。邀请不会替换已有账户的密码。过期、已消费或不匹配的链接会被拒绝。

成员表支持更改角色或禁用成员关系。禁用后，对该工作区的后续请求会被阻止。安装管理员在 **Installation accounts** 下管理全局账户状态和安装管理员角色。禁用账户会撤销其会话。最后一个活动安装管理员不能被禁用或降级。

访问权限变更要求在此前 15 分钟内确认过密码。出现 **Confirm your password** 时，完成确认后重试目标操作。通过浏览器会话写入 API key、策略和资源组时也适用此要求。每次请求均检查当前账户、成员关系和策略；角色变更和撤销会影响后续请求。

<a id="change-or-reset-a-password"></a>

## 更改或重置密码

在 **Your account → Change password** 下提供当前密码和新密码，然后选择 **Change password and sign out**。更改成功后会撤销该账户的所有会话，包括当前会话。使用新密码重新登录。

安装管理员在近期确认密码后，可以在 **Accounts and members** 中为账户选择 **Create password reset link**。该操作控制全局身份，因此仅有工作区管理员身份不能签发重置链接。链接在一小时后过期、仅可使用一次，且只显示一次。再次签发会使该账户此前尚未使用的重置链接失效。接收者设置新密码后，该账户的所有会话均被撤销，必须重新登录。系统没有邮件密码重置服务。

在 **Your account** 下使用 **Signed-in sessions** 查看过期时间和撤销会话。撤销当前会话会使当前浏览器退出登录。离开共享设备时应退出登录，即使未勾选记住登录选项。

<a id="recover-installation-administration"></a>

## 恢复安装管理权限

如果没有安装管理员能够登录，拥有该安装 Admin 数据库访问权限的运维人员可以签发 recovery 链接：

```bash
INSTALLATION_NAMESPACE='orbitjob-system'
kubectl -n "$INSTALLATION_NAMESPACE" exec deploy/orbitjob-admin-api -c admin-api -- \
  /orbitjob-identity -action recovery -username administrator \
  -origin https://orbitjob.example.com
```

将 `administrator` 替换为要恢复的账户。`-username` 可选；绑定用户名后，仅该用户名可以消费能力令牌。Recovery 使用相同的 setup 表单和有效期控制，可重新启用已有账户、重置其密码和安装管理员角色，或创建替代账户。该操作在 Default 中显式授予 owner 成员关系，并撤销被恢复账户的已有会话。安装必须已被认领，且 Default 工作区必须处于活动状态。这是运维人员级别的恢复流程；旧 API key 不能证明账户所有权，也不能重新开放 setup。

<a id="https-and-local-development"></a>

## HTTPS 与本地开发

生产会话使用 `__Host-orbitjob_session`，属性为 `HttpOnly`、`Secure`、`SameSite=Strict`、`Path=/`，且无 `Domain`。PostgreSQL 仅保存会话秘密的哈希。JavaScript 在页面内存中获得独立于会话凭据的 CSRF token。使用 cookie 认证的写入必须同时提供精确匹配的允许 `Origin` 和 `X-CSRF-Token`。登录和能力令牌提交也强制检查允许的来源。显式无效的 Bearer header 会直接失败，不会回退使用会话 cookie。

在 Admin 上将 `CONSOLE_PUBLIC_ORIGIN`，或 Helm 的 `adminapi.console.publicOrigin`，设为精确的公开 HTTPS 来源。省略时，服务端使用 `https://` 加请求 Host 推导来源。反向代理必须保留公开 Host。转发 header 不能指定可信来源，也不能关闭 cookie 安全要求。更改公开地址时需检查此设置，无需重新构建前端。

`CONSOLE_ALLOW_INSECURE_LOCALHOST` 默认为 `false`。HTTP 开发时将其设为 `true`，或设置 Helm `adminapi.console.allowInsecureLocalhost: true`。仅 `localhost`、`127.0.0.1`、`::1` 等回环主机符合条件。此模式使用不带 `Secure` 的 `orbitjob_session_local`，不允许通过节点 IP 或公开地址进行 HTTP 登录。kind 验证 values 显式启用此本地模式。使用 Vite 时，保持代理后的浏览器 Host 与 Origin 一致，并将 `publicOrigin` 留空或设为 Vite 来源。

持有 `HttpOnly` cookie 仍然具有会话权限。应通过 HTTPS 提供 Console、维护其 Content Security Policy、保护管理员设备，并限制共享浏览器上的会话有效期。即使使用 `HttpOnly`，在 Console 来源内运行的脚本仍可通过该会话执行操作。密码使用 Argon2id，内存为 64 MiB，迭代三次，并行度为一。API key 继续使用独立的 bcrypt 验证和自动化策略。

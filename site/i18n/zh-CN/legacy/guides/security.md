---
title: "安全与授权"
---

本策略描述当前源码。运行已发布版本时，应使用该版本 tag 下的安全策略。安装管理员管理个人账户、工作区成员和访问策略。Console 用户通过用户名和密码登录；脚本使用单独命名的 API 密钥。

<a id="reporting-vulnerabilities"></a>
## 报告漏洞

请勿在公开 Issue、Discussion 或 PR 中披露漏洞。发送邮件至 <justs3loy@gmail.com>，主题为 `[OrbitJob Security]`。

报告包含受影响 commit/版本、所需权限、最小复现、租户、数据库角色与命名空间边界、实际影响和建议修复。仅在获授权环境中测试。不要发送真实 API 密钥、密码、Secret 内容或生产数据。

维护者确认可复现报告后协调修复与披露。本策略不承诺固定响应时限 SLA 或长期支持线。发布记录见 [CHANGELOG.md](https://github.com/s3loy/orbitjob/blob/main/CHANGELOG.md)；打包的 chart/app 版本定义在 [Chart.yaml](https://github.com/s3loy/orbitjob/blob/main/charts/orbitjob/Chart.yaml)。安全更新通过对应发布版本标识。修复公开后对报告者署名，要求匿名者除外。

<a id="current-security-model"></a>
## 当前安全模型

安全边界包括个人账户会话或自动化 Bearer 认证、显式工作区成员和策略授权、可信租户与资源组处理、PostgreSQL 最小权限角色与 RLS，以及 Kubernetes 命名空间 RBAC、准入和 Secret 控制。各项应对不同失败；数据库 RLS 不授权 HTTP action，也不验证 Kubernetes 归属。[验证记录](/orbitjob/docs/verification/)记录已测试边界和剩余资格验证工作。

<a id="api-keys-and-authorization"></a>
### API 密钥与授权

API 密钥使用 `otj_` 前缀。PostgreSQL 保存 bcrypt 摘要，不保存可恢复明文。普通密钥创建只返回一次明文；列表操作返回元数据。密钥已提交但 Secret 写入失败时，bootstrap 可重试交付配置的同一明文：先验证已有 bcrypt 摘要，并拒绝冲突文件或 Secret。它不从数据库恢复明文，也不静默轮换已有凭据。

Bootstrap Secret 交付授予命名空间范围 create，以及 `resourceNames` 限定的 `bootstrap-api-key` get，用于匹配数据重试。它没有 Secret list、update 或 patch 授权。旧 `bootstrap.secretName` chart 值已弃用且被忽略；应移除覆盖值，不应将其作为写入目标配置。应用始终使用固定交付名称。

`orbitjob_auth_api_key` 在租户尚未知时解析候选摘要。它使用固定 search path 和受限执行授权的 SECURITY DEFINER；应用进行 bcrypt 比较。未知、已撤销、已过期和非活动租户凭据均返回相同认证失败响应。

Admin API 路由执行显式 action/resource 策略授权。没有授权的密钥可通过认证，但不能访问受保护能力。显式 Deny 优先；委派和边界交集不得扩大调用方授权。租户策略使用受支持的精确 action 和资源段语义；通配能力由平台预设管理。[架构](/orbitjob/docs/architecture/#tenant-and-authorization-boundaries)说明策略评估与可信请求身份。

请求体不能替换服务端管理的 tenant/group/path 身份。支持资源组的 Check、SLI、SLO 及其派生读取保留资源组作用域；不能执行资源组限制的接口拒绝资源组密钥。跨租户管理需要覆盖实际目标的策略授权；bootstrap 租户身份不构成隐含授权。撤销操作按目标密钥所有者与资源组授权，并审计已认证操作者。

普通业务客户端不得使用 bootstrap 凭据。应使用具有所需授权的专用密钥，并在向不受信任网络暴露管理端点前增加 Ingress 或网关访问控制。

<a id="browser-console"></a>
### 浏览器 Console

Console 用户获取工作区邀请并使用个人账户。安装者通过 `/orbitjob-identity`，使用安装的 Admin DSN 签发首位管理员单次设置链接。首次设置标记不会自动重开设置。管理权限丢失时，需要操作者签发恢复能力令牌。旧 API 密钥不标识账户所有者。见[账户与恢复](/orbitjob/docs/accounts/)。

密码使用 Argon2id，内存 64 MiB、迭代三次、并行度一。服务端要求至少 15 个字符、最多 1,024 个 UTF-8 字节，并拒绝本地常见密码阻止列表和短重复模式。不保存可恢复密码。当前源码没有邮件投递或多因素认证。已有账户接受邀请需要该账户有效会话，且不会更改密码。只有安装管理员可签发全局密码重置链接。密码修改和重置均撤销该账户全部会话。

`/console/` 下的 GET/HEAD 导航、资源，以及根路径到 `/console/` 的重定向公开。受保护 API 路由保留认证、策略授权、租户速率限制、请求体限制和 trace/metrics 中间件。缺失资源和未知 API 路径不回退到 SPA。Console 响应使用同源 Content Security Policy、`nosniff`、禁止框架嵌入和 no-referrer；为组件库允许内联样式。字体、脚本与样式均本地打包。安装版 Console 没有 Demo 入口或模拟客户端。仅在受信任安装的 Console origin 输入密码。

生产会话使用 `__Host-orbitjob_session`，具有 `HttpOnly`、`Secure`、`SameSite=Strict`、`Path=/`，且没有 `Domain`。PostgreSQL 只保存会话秘密的哈希。记住登录选项使用七天绝对有效期的持久会话；取消此选项则使用浏览器会话 cookie，服务端有效期为八小时。请求和轮询不延长过期时间。浏览器恢复可能保留会话 cookie，因此共用设备上的用户必须显式退出。独立 CSRF token 保留在页面内存。每个 cookie 认证写请求均要求精确 Origin 验证和 `X-CSRF-Token`；登录和能力令牌提交也检查 Origin。显式无效 Bearer 凭据绝不回退到 cookie 认证。

`CONSOLE_PUBLIC_ORIGIN` 可固定精确公共 HTTPS origin。否则预期 origin 为 `https://` 加请求 Host。反向代理必须保留公共 Host；转发请求头不能放宽 origin 或 cookie 安全。`CONSOLE_ALLOW_INSECURE_LOCALHOST=false` 为默认设置。显式本地模式仅允许 HTTP 回环，使用 `orbitjob_session_local`；非回环 HTTP 地址不能登录。Helm 和前端开发配置见 [origin 设置](/orbitjob/docs/accounts/#https-and-local-development)。

浏览器只在 `sessionStorage` 保存非秘密工作区选择。受保护查询缓存、草稿和回执保存在页面内存，退出、401、身份或工作区变化时清除。一次性能力令牌片段从地址栏移除，仅保留在页面内存。会话 cookie 必须按凭据管理。同源脚本可以通过 `HttpOnly` 会话执行操作；HTTPS、CSP 和设备保护仍为必要措施。

每个请求重新检查用户和工作区成员活动状态，以及有效策略授权。安装管理员身份控制全局账户与工作区元数据，不提供隐含工作负载访问权限。工作区创建者获得显式 owner 成员资格。停用账户撤销会话；停用成员或更改角色影响后续工作区请求。特权账户变更，以及浏览器 API 密钥、策略和资源组写入要求过去 15 分钟内确认过密码。稳定用户 actor ID 独立于密钥 ID。安装安全事件使用独立只追加审计流；工作区变更保留事务内租户审计记录。

一次性 API 密钥创建结果保留在客户端范围临时存储中，不进入查询或 mutation 缓存。离开页面不会静默丢失结果；显式丢弃或断开连接才移除。断开的客户端丢弃迟到创建响应。创建结果未知时必须核对，不自动重试。身份查询只返回元数据和精确策略决策，不返回凭据、策略文档，也不探测资源是否存在。平台请求对授权与数据访问使用同一个显式选择的租户；普通租户凭据不能选择其他租户。

对 Bearer 客户端，已认证 `/workspaces` 目录只提供名称、slug、状态和不可变 ID。租户密钥可读取自身身份标签；其他租户行要求精确有效 `tenant:Get` 授权。资源组凭据不能发现其他工作区。平台凭据没有隐含自身行。策略包计算精确有限或余有限可见集合，包含递归边界和显式 Deny。迁移 0011 的 Admin-only 元数据读取器在分页前应用可信范围及字面名称、slug 搜索；`has_more` 描述可见结果，不返回全局总数。读取器固定 search_path，不扩大表 RLS，也不授予工作负载访问。
目录请求不能选择业务租户。Administration 选择只更改已有显式管理目标；普通租户业务查询保留凭据绑定作用域。个人会话单独列出活动成员资格；安装元数据发现不会创建成员资格。

Manifest 导出在现有 CSP 下运行本地预编译验证器，不授予 Kubernetes 写入或日志访问。在线定义读写为独立类型化路由，仅限安装映射的租户命名空间和无资源组限制凭据。Dry-run 和持久化均由独立 Apply 授权保护。只写 spec 和服务端请求注解；UID/resourceVersion 比较防止静默替换并发编辑。未知写入保留身份并通过读取核对，不自动重试。

日志读取要求独立父资源授权和已记录、不可变的 JobRun/Job/Pod 归属。Admin 除 Run 发布权限外，仅有命名空间范围的定义 get/create/patch、Job get、Pod get/list 和 pods/log get，没有通用 Kubernetes 代理，也没有 Secret、exec 或 attach 访问。
独立归档路由提供有界游标读取、同源跟随、搜索、导出和带审计 DeleteLogs 请求。受信任 Fluent Bit 采集器读取映射命名空间的节点 CRI 路径，使用 collector mTLS；Admin 使用 reader mTLS。单写入归档进程使用 Runtime 数据库凭据，不新增窄权限数据库登录身份。归档输出和事件保存在独立有界 PVC，仍属于敏感内容。见[执行日志](/orbitjob/docs/execution-logs/)。
快照具有字节、行数和时间限制，Console 不缓存。没有 JobRun controller reference 的旧 Job 不可用；Kubernetes 垃圾回收可能随所属对象移除诊断日志。

平台策略文档通过迁移 0007 的 Admin-only SECURITY DEFINER 读取器，为已认证密钥 ID 加载。它只返回有效且 tenant 为 NULL 的平台凭据绑定的预设与边界文档；PUBLIC/runtime/operator 不能执行。配置边界无效时拒绝访问。策略边界按每次实际请求评估，在不同授权文档间保留显式 Deny。所选租户不扩大预设 self 语义，也不改变普通租户密钥 RLS。

Function 管理有独立 Create/Update 授权。资源组范围读写解析实际持久化资源组；显式 Deny 仍优先。创建回执绑定 tenant、actor、key 和 payload。更新原子比较已审阅版本；Console 调用固定该版本及其活动修订版本。定义写入与审计记录一起提交。新增 action 只更新指定 TenantAdminAccess 预设；已有显式租户策略不新增写权限。

Workflow 自动启动使用持久 occurrence 账本、精确 CR 身份，以及与手动启动共用的原子重叠检查。WorkflowRun 必须绑定原 UID 并被观察为 live 后，才能开始步骤。发布恢复拒绝其他归属或替代资源。历史保留在完整 tenant/source/revision/occurrence 身份检查后，使用 UID/resourceVersion 前置条件仅删除终态资源，再清理记录。Operator 的 `workflowruns` delete 只授予安装映射租户命名空间，不新增集群级写权限。被拒绝的计划启动保留贯穿触发分钟的回执；账本清理后，不能从过期 Schedule CR 重放。

<a id="console-monitoring-evidence-scope"></a>
### Console 监控证据范围

Check 结果、SLO 预算与历史、SLO 告警通过父 Check 或 SLO 应用已认证凭据的资源组。详情和单 SLO 预算、历史读取按实际父资源组和 ID 授权，无资源组限制凭据也保留资源组专属 Deny。其他组父资源返回 404，包括预算历史。CheckRun/Alert 集合权限必须覆盖整个租户或凭据资源组查询域。相关逐 ID/资源组 Deny、边界限制或证明预算耗尽时，在读取条目和计数前返回 403；过滤和分页不能绕过证明。不会静默移除被拒条目或计数。资源组 ARN 授权不能扩大为租户级证据读取。每次读取仍需独立授权。
对资源组密钥，继续省略 SLO 内嵌 `current_budget`；无资源组限制调用方需要对实际 SLO 资源组和 ID 的 `slo:GetBudget`，包括显式 Deny。Check/SLI/SLO 定义保留资源组过滤。SLO 创建在写事务内验证 SLI 引用：相同租户、有效行，以及调用方有作用域时的匹配资源组。缺失或不可访问引用返回 not-found。

<a id="postgresql-roles-and-rls"></a>
### PostgreSQL 角色与 RLS

| 角色 | 用途 |
|---|---|
| `orbitjob_table_owner` | NOLOGIN，拥有 schema 对象和安全函数 |
| `orbitjob_migrator` | 允许切换为表所有者的迁移身份 |
| `orbitjob_admin` | Admin API/bootstrap 管理写入、经审阅的账户安全函数，以及执行历史只读访问 |
| `orbitjob_runtime` | Scheduler/Operator 登录身份，具有所需运行写权限 |
| `orbitjob_operator` | NOLOGIN 执行角色授权，不是单独部署的登录凭据 |

运行进程不得使用安装所有者、表所有者或超级用户 DSN。Operator 先解析 `OPERATOR_DSN`，再回退到 Runtime 配置；Secret 名称、键和连接示例定义在 [chart values](https://github.com/s3loy/orbitjob/blob/main/charts/orbitjob/values.yaml) 和 [deploy/env/](https://github.com/s3loy/orbitjob/blob/main/deploy/env/) 中。见[数据库设置](/orbitjob/docs/database/)。

完整[迁移目录](https://github.com/s3loy/orbitjob/blob/main/db/migrations/)包含 RLS、授权和函数，包括迁移 0012 增加的 `sli_snapshot_events`。应用角色不拥有租户表，因此无需 FORCE RLS，ENABLE RLS 即对其生效；NOLOGIN 所有者仍为维护边界。仓储在查询的同一事务内设置事务局部 `app.tenant_id`。连接池中使用会话级租户设置不安全。

审计父表和直接分区按授权允许应用追加与读取，不允许 UPDATE、DELETE 或 TRUNCATE。领域更改与审计事实一起提交。SLI/SLO 引用通过约束和事务内来源、资源组检查执行租户归属。幂等派生成功后才确认投影；清理保留去重事实必须维持去重保证。

经审阅的窄范围 SECURITY DEFINER 入口在作用域事务可用前，处理认证、活动租户枚举、bootstrap 和撤销所有者发现。它们固定 search path、撤销 PUBLIC 执行，仅授权特定角色。平台预设和授权工作区元数据读取器具有相同窄范围契约。新增跨租户路径需要同等审阅和真实 PostgreSQL 测试；能通过作用域事务回答查询时优先使用该事务。基线撤销 `PUBLIC` 在 `public` schema 上的 `CREATE`；新安全函数也必须显式撤销公共执行权限。

<a id="kubernetes-workloads"></a>
### Kubernetes 工作负载

Operator 从配置的命名空间映射解析租户，不采用工作负载资源自报租户。Admin 的每命名空间 Role 允许对 JobRun 和 WorkflowRun 执行 create/get/patch。类型化编写增加命名空间内定义 get/create/patch；保留日志读取增加 Job get、Pod get/list 和 pods/log get。这些授权不提供 Job/Pod 生命周期写入、status 写入、Secret、exec 或 attach 访问。Operator RBAC 管理配置命名空间内的执行与历史保留操作。

渲染工作负载 Pod 设置 `automountServiceAccountToken: false` 和 `restartPolicy: Never`。Token 挂载必须保持禁用；访问 Kubernetes API 需要经审阅的模板扩展和独立窄权限身份。不得为工作负载扩大 Operator 或默认命名空间 ServiceAccount 权限。

镜像在命名空间准入、Pod Security 和网络控制范围内执行任意容器代码。需要不可变内容时，在 registry 或准入层固定镜像，并通过 NetworkPolicy 或出站代理限制工作负载、探针出站。Check 探针对其配置 URL 发出出站请求；已移除的进程内 HTTP/webhook handler 防护不适用于它们。

Run/attempt 接管要求完整身份与不可变 Job UID。取消 patch 和历史删除使用 UID/resourceVersion 前置条件。已提交 Workflow 停止阻止新子执行创建；不明确创建保持为 `CancelUnknown`，直到归属和终止可确认。即使配置 Pod restartPolicy，Job backoff 或新平台 attempt 仍可能重复外部效果；工作负载必须容忍重复执行。

新工作负载 Pod 使用 Operator 管理的 finalizer 保留终止证据。Operator 只在映射工作负载命名空间内拥有 Pod `patch` 权限，并在持久证明后以 UID/resourceVersion 检查仅移除自身 finalizer。Admin 与采集器凭据不获得 Pod 修改权限。取消要求控制器停止确认后才能接受完整 Pod 清单；Job 删除时间戳不充分。证明持久化和证据解除先于原 Job 删除。此协议在控制器和数据库故障期间保留观察，不证明节点丢失或外部移除保留机制后的终止。

执行恢复将缺失的已绑定 Job 保留为 `ExecutionUnknown`，占用并发名额并禁止自动替换。终止证据需要完整命名空间 Pod 观察和不可变所有者身份；空清单、强制删除和节点丢失不证明工作负载代码停止。Client-go Lease 会话协调单例工作，事务来源准入执行 Forbid。保证与限制见[执行恢复与并发准入](/orbitjob/docs/architecture/#execution-recovery-and-concurrent-admission)。Admin 保留执行账本只读权限，没有强制解决 API。

<a id="secrets-logs-and-test-environments"></a>
### Secret、日志与测试环境

不得提交 `.runtime/` 安装状态、`.kind/` 生成凭据、API 密钥、密码或 DSN、Secret 明文，或包含真实标识和错误的 smoke 输出。数据库安装文件使用受限权限；bootstrap 使用本地秘密文件或 `bootstrap-api-key` Secret。对已记录 release 命名空间，私下读取方式如下：

```bash
kubectl -n orbitjob-system get secret bootstrap-api-key \
  -o jsonpath='{.data.api-key}' | base64 --decode
```

不得将输出放入 CI 日志或公开报告。日志不得包含 Authorization 请求头、API 密钥、DSN、完整请求体或 Secret 数据。角色密码、Secret 值和工作负载环境必须协调轮换；修改其中一项不会更新其他项。

集成测试需要专用一次性 PostgreSQL 数据库，可能重建 schema 和配置角色；不得将 `TEST_DATABASE_DSN` 指向已安装环境。测试结果及适用环境记录在[验证记录](/orbitjob/docs/verification/)中。

<a id="known-contract-and-compatibility-limits"></a>
## 已知契约与兼容限制

- OpenAPI 通过 `APIKey` 声明自动化 Bearer 认证，通过 `ConsoleSession` 声明 Console cookie 认证。中间件执行对应认证、CSRF 与策略要求。Check/SLI/SLO DELETE 请求需要 [OpenAPI](/orbitjob/openapi.json) 声明的带版本 JSON 请求体。
- 迁移 0027 增加个人账户，不从已有 API 密钥分配账户归属。部署匹配 Admin 镜像与 chart，再完成显式账户设置；见[迁移指南](/orbitjob/docs/database/#personal-account-migration)。
- 迁移 0012 保留历史聚合，不重放旧终态行。迁移前停止旧 Operator，之后启动新版本。见[迁移兼容性](/orbitjob/docs/database/#migration-lineage-and-compatibility)。
- 新 Run 资源名称保留完整 occurrence 摘要；匹配历史名称在完整身份检查后仍可读。见[升级流程](/orbitjob/docs/deployment/upgrading-an-existing-installation/)。
- 当前验证缺口，包括独立故障恢复和有界去重归档策略，仍记录在[已知问题](/orbitjob/docs/known-issues/)中。

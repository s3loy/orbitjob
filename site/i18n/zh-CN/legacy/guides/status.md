---
title: "项目状态"
---

实现已于 2026-11-15 审阅。本页说明仓库当前能力。发布可用性记录在[更新日志](https://github.com/s3loy/orbitjob/blob/main/CHANGELOG.md)中；测试结果与适用范围记录在[验证记录](/orbitjob/docs/verification/)中。

<a id="current-capabilities"></a>
## 当前能力

| 领域 | 已实现行为 |
|---|---|
| 执行 | ScheduledJob cron 与手动执行、不可变定义修订版本、重试、截止时间、取消，以及 Run 和 attempt 历史 |
| Workflow | WorkflowJob DAG、自动与手动启动、依赖条件、失败策略和取消传播 |
| Function | 版本化定义管理、调用、活动修订版本同步和调用历史 |
| 可靠性 | 容器化 HTTP Check、受支持的状态与时长 SLI 条件、SLO 窗口、错误预算和消耗率告警 |
| 管理 | 个人账户、工作区成员、可撤销 Console 会话、API 密钥、显式策略授权、资源组和经授权的工作区发现 |
| 个人账户 | 仅通过邀请加入的账户、密码认证、可撤销浏览器会话、显式工作区成员资格和安装管理员恢复 |
| Console | 同源管理界面，支持引导式 Job/Workflow 编写、Function 管理、执行历史、保留日志和 Administration |
| 执行日志 | 有界 stdout/stderr 与生命周期证据，独立于 Job、Pod 和 Run 资源清理保留；支持授权读取与带审计的删除 |
| 接口 | Admin HTTP API、生成的 OpenAPI、Kubernetes CRD 和 CLI |
| 交付 | 支持可配置 Service 暴露方式和可选同源 Ingress 的 Helm 安装、源码构建和开发集群工具 |
| 维护 | 按租户有界清理 SLI 重放明细、基于账本的执行隔离清单，以及带独立证据检查的故障恢复 canary |

[架构](/orbitjob/docs/architecture/)定义组件职责和信任边界。[使用指南](/orbitjob/docs/usage/before-you-begin/)说明受支持操作；[执行日志](/orbitjob/docs/execution-logs/)定义保留与容量行为。

[账户指南](/orbitjob/docs/accounts/)定义首位管理员设置、邀请、密码和会话处理。全局密码重置要求安装管理员权限；工作区管理员管理本工作区成员和授权。安装管理员身份不隐含工作区工作负载访问权限。

<a id="execution-and-isolation-guarantees"></a>
## 执行与隔离保证

Kubernetes 运行容器；PostgreSQL 记录执行事实。Admin API 写入管理状态并发布 Kubernetes 意图；Operator 负责更新执行账本。Runtime 数据库身份属于受信任身份；应用策略、数据库 RLS 和命名空间 RBAC 分别执行各自访问边界。

持久创建意图和不可变 Job 身份保护外部操作结果不明确时的恢复。缺失执行证据保持为非终态 `ExecutionUnknown`，既不允许替换 attempt，也不释放 `Forbid` 准入名额。取消只在有充分的所属终止证据时完成。终态投影幂等重试；未确认的结果继续受保护，不被历史清理删除。

新工作负载 Pod 保留容器退出证据，直到证据持久化。Operator 必须取得控制器停止确认后才能接受完整清单；仅解除自己的证据 finalizer，再删除原始 Job。恢复和历史保留会保护尚未完成的证据义务，包括历史 attempt。删除延迟可在取得可信证明后恢复；明确的证据缺口继续保持隔离。

Operator 单例工作使用 client-go Lease 会话。来源准入使用事务范围 PostgreSQL 锁，并与修订版本和生命周期更新共用该锁。领导权协调工作，不提供跨 PostgreSQL 和 Kubernetes 的通用写入隔离。外部工作负载效果具有至少一次语义；重复效果不安全时，工作负载必须提供幂等性。

<a id="compatibility-and-operations"></a>
## 兼容性与运维

配置参阅 chart 和环境示例，数据库 schema 参阅迁移文件，HTTP 契约参阅 OpenAPI。[部署指南](/orbitjob/docs/deployment/prerequisites/)和[数据库指南](/orbitjob/docs/database/)定义安装、凭据和迁移流程。

个人 Console 账户要求迁移 0027 和匹配 Admin 镜像。操作者签发单次设置或恢复链接；普通用户通过工作区邀请加入，并使用自己的账户登录。记住登录状态的会话在七天后过期。已有 API 密钥保留自动化语义，不会变为个人账户。见[账户设置与访问](/orbitjob/docs/accounts/)。

已安装 Console 使用安装的 IP 或域名及端口。可选 Ingress 将 Console、API 和 schema 路由到同一 origin，并可引用现有 TLS Secret。Chart 不安装 Ingress 控制器，也不签发证书。外部访问见[安装访问](/orbitjob/docs/usage/helm-and-kubernetes/#72-first-install)，已测试 origin 与升级场景见[验证记录](/orbitjob/docs/verification/)。

恢复和资源身份迁移切换前，必须停止旧执行写入进程。保留已记录迁移校验和和安装原有迁移输入渠道。数据库与日志归档需要持久存储；除非明确要删除数据，否则资源清理必须保留其数据。

个人账户候选版本增加迁移 0027。其已安装切换和现有开发集群网络、存储维护遵循[维护流程](/orbitjob/docs/local-maintenance/)；仓库中的实现不能证明该窗口已经完成。

<a id="remaining-limitations"></a>
## 剩余限制

无法证明终止时，Unknown 执行可能无限期占用并发名额。SLI 清理保留未完成计划和可查询的缺失证据，覆盖仍分配给 Operator 的租户。缺少时间戳可能阻止自动清理。调度准时性 SLI 推导、通用 Workflow 数据流与跨集群调度不在已实现范围内。

已记录安装配置和受控恢复测试不构成生产吞吐或高可用资格验证。运维约束见[已知问题](/orbitjob/docs/known-issues/)，测试行为、环境与待补证据见[验证记录](/orbitjob/docs/verification/)。

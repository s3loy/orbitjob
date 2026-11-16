# OrbitJob

[English](README.md)

OrbitJob 在 Kubernetes 上运行定时和按需容器工作负载，支持 cron 调度、具有依赖关系的工作流，以及版本化的容器 Function。使用者通过同源 Console、HTTP API 或 CLI 管理工作负载。

Operator 负责调度和执行，将定义投影为不可变的修订版本，并将获准执行的 `JobRun` 资源绑定到各自的定义修订版本。每次平台 Attempt 对应一个 Kubernetes Job；该 Job 可以按自身的 backoff 策略创建替代 Pod。PostgreSQL 保存定义修订版本和执行台账，记录触发信息、操作者、尝试与观察到的结果，同时保存访问策略和审计记录。

独立且有容量限制的日志归档在工作负载清理之外保留容器输出，并报告采集缺口。HTTP Check 和 SLI 快照为 SLO 错误预算与消耗率告警提供观测数据；Scheduler 根据已记录的状态评估这些预算。

## 开始使用

### 使用要求

使用已有安装时，应取得可信的 Console 地址、工作区邀请，以及运行目标作业和读取其执行历史的权限。读取输出需要独立的日志访问权限。个人账户用于登录 Console；API key 用于脚本和 HTTP 客户端认证。邀请和管理员设置见[账户设置与访问](docs/accounts.md)。

安装 OrbitJob 前，应准备 Kubernetes、PostgreSQL、持久化数据库与日志存储，以及所需的组件镜像。[安装前提](docs/setup/prerequisites.md)说明版本、容量、命名空间映射和权限要求。搭建源码开发环境时，使用[本地开发指南](docs/local-deployment.md)。

### 运行首个作业

在已配置且由管理员提供了目标作业的安装中：

1. 登录 `/console/`。在 **Account** 中选择工作区，再选择 **Open workspace**。
2. 打开 **Jobs**，选择目标作业并核对其命名空间和执行设置。选择 **Trigger**，确认请求。
3. 保留回执，并跟随回执打开执行记录。检查最终阶段及各次 Attempt；拥有权限时打开 **Execution logs**。

回执表示请求已被确认。执行历史异步生成，缺少输出不能确定执行结果。缺少终止证据时，Run 可能保持未知状态。[首个作业教程](docs/tutorials/first-job.md)包含权限要求、观测步骤和清理操作，也指导具备编写权限的使用者创建一个打印消息的小型手动作业。保存手动定义不会执行作业。

### 安装或从源码构建

在已有集群中部署时，使用[安装指南](docs/setup/installation.md)。从同一源码提交构建当前组件，并使用匹配的 Chart 和数据库迁移。指南包含凭据、存储、命名空间映射、访问方式和安装检查。

本地源码开发使用 [kind 工作流](docs/local-deployment.md)，构建开发镜像并加载到隔离集群中。它的前提和生命周期与升级已有安装不同。修改正常运行的系统前，先阅读[升级流程](docs/operations/upgrading.md)。

## 后续阅读

- 按[入门指南](docs/getting-started.md)准备工作区访问，并了解从请求到结果的流程。
- 在[教程与任务索引](USAGE.md)中查找工作负载编写、执行、取消、日志和访问管理操作。
- 阅读[执行模型](docs/concepts/execution-model.md)和[技术架构](docs/architecture.md)，了解修订版本、重试、不确定状态和组件职责。工作负载的外部副作用可能重复；不能安全承受重复时，应使这些副作用具备幂等性。
- 使用单独签发的 API key [调用 API](docs/tasks/call-api.md)。安装在 `/openapi.json` 提供契约；应使用与目标安装匹配的契约。仓库副本见 [OpenAPI](api/openapi.yaml)。
- 查询[工作负载资源](docs/reference/workload-resources.md)、[配置](docs/reference/configuration.md)和 [CLI 命令](docs/cli.md)。
- 运维安装前，核对[数据库角色与迁移](docs/database-setup.md)、[执行日志保留](docs/execution-logs.md)、[安全](SECURITY.md)和[已知限制](docs/known-issues.md)。

## 参与贡献

[CONTRIBUTING](CONTRIBUTING.md)说明开发前提、测试、分支约定和迁移规则。文档贡献遵循[文档维护流程](docs/project-site.md)。

## 项目状态

当前源码处于面向 v0.3.0 的开发阶段。Chart 版本或本地构建成功不能证明镜像仓库中的制品可用。发布历史见[变更日志](CHANGELOG.md)，已检查的环境与行为见[验证记录](docs/verification.md)。这些检查不构成生产吞吐能力或高可用性的验收结论。

## 许可证

[BSD 3-Clause](LICENSE)

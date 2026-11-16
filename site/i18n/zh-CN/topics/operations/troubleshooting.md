# 安装故障排查

从受影响操作、其已接受身份和负责下一步骤的组件开始排查。重试前保留原始错误、请求和运行身份、时间戳以及相关事件。凭据和原始执行输出应保存在私有证据存储中。

## 采集安装快照

使用预期上下文和实际发行版命名空间：

```bash
export RELEASE_NAMESPACE=orbitjob-system
export RELEASE_NAME=orbitjob
kubectl config current-context
helm status "$RELEASE_NAME" -n "$RELEASE_NAMESPACE"
kubectl get pods,jobs,pvc -n "$RELEASE_NAMESPACE"
kubectl get events -n "$RELEASE_NAMESPACE" --sort-by=.metadata.creationTimestamp
```

使用 `kubectl describe pod` 和 `kubectl logs` 检查具体故障 Pod。对于重启过的容器，`kubectl logs --previous` 可能保留较早失败。调查执行时，应匹配命名空间、来源 UID、修订版本、调度项，以及原始 Job/Pod UID。仅按资源名称可能定位到无关替代资源。

不要将删除数据库、清除迁移校验和、移除终结器或重新触发不确定工作作为诊断步骤。写入响应不明确时，必须先查找身份和回执，再进行另一项变更。

## 钩子 Pod 报告 CreateContainerConfigError

读取 Pod 事件，确认缺失 Secret 或键。Owner-init、migrate 和 bootstrap 在普通应用使用前就会使用安装 Secret。默认情况下，它是发行版命名空间中的 `orbitjob-database`，包含[数据库配置](../database-setup.md#create-the-installation-secret)所列八个键。

新建本地 kind 安装时，`make kind-db` 准备该 Secret 和 PostgreSQL 服务器。外部 PostgreSQL 应在重试 Helm 前完成数据库配置。数据库命名空间中的 Secret 不能满足发行版命名空间内的 Pod。已初始化安装应恢复已有凭据，不要生成新的引导 API key。

## 迁移钩子失败

在钩子 TTL 删除失败 Job 或 Pod 前，检查它们及其日志：

```bash
kubectl get jobs -n "$RELEASE_NAMESPACE"
kubectl logs -n "$RELEASE_NAMESPACE" job/YOUR_FAILED_MIGRATION_JOB
```

使用维护权限检查全部迁移行。每个成功迁移独立提交；后续失败可能使模式处于部分推进状态。重试前检查数据库可达性、角色设置、对象所有权和选定 SQL 输入通道。

对于 `checksum mismatch`，比较原始输入和已安装 ConfigMap。Helm 的 `|-` 移除末尾换行，因此原始目录 SQL 不是可互换的迁移通道。对于旧版版本 7 开发迁移链，应停止并使用[兼容性流程](../database-setup.md#existing-development-lineages)。不要编辑迁移台账或 SQL 来隐藏错误。

Owner-init 角色偏差错误会指出需要修复的角色或属性。数据库管理员必须恢复所需角色属性和管理授权。应用连接不能替代该权限。

## 数据库认证或 TLS 失败

检查选定数据库、URI 编码、网络端点、引导账户和证书信任。在配置机器上可达的端点，在 Pod 内可能不可用。仅供主机使用的端口转发或 `sslrootcert` 路径不会自动供集群组件使用。

外部设置需要私有 DSN 文件，并在首次设置时检查每个生成角色的连通性。复用状态会刷新本地环境，但不检查服务器。该重复路径的成功消息不能证明 DSN 仍然有效。应检查实际 Secret 键映射和部署环境引用，不要将凭据打印到报告。

轮换 Secret 时，应协调 PostgreSQL 密码、本地状态、全部 DSN 和密码键，以及使用方重启。单独更新 Secret 不会重启 Pod，重启 Pod 也不会轮换数据库凭据。

## Operator 退出或定义没有活动修订版本

读取启动和协调日志：

```bash
kubectl logs -n "$RELEASE_NAMESPACE" deployment/orbitjob-operator --tail=100
```

`OPERATOR_NAMESPACE_TENANTS` 为空或格式错误会阻止启动。当前 Chart 在启用 Operator 时会拒绝缺失映射；如果在线值不同，应检查旧清单或手动 Deployment 编辑。映射必须指定预期的已有租户，以及包含来源 CR 的命名空间。

对于尚未投影的定义，应检查 CR 条件、准入错误、修订版本状态和原始来源 UID。检查 Webhook 就绪状态、证书有效期和原生写入方策略。普通客户端应使用 checked-definition API。采用失败关闭策略的 Webhook 未就绪，不构成禁用准入并创建未检查来源的理由。

## Pod 无法拉取或启动镜像

读取 Pod 事件，区分拉取凭据、平台清单缺失、镜像引用缺失、镜像启动错误和准入拒绝。使用 `global.imagePullPolicy: Never` 时，每个节点必须已经包含渲染后的精确名称和标签。对于普通贡献者集群：

```bash
make kind-load TAG=dev
```

使用安装 values 选定的标签。本地 Docker 登录不会配置节点镜像拉取凭据。重新构建 `:dev` 后，重启每个受影响的镜像使用方；未改变的 Pod 模板可能继续使用旧代码。

## API 拒绝认证或授权

HTTP 401 表示认证不可用或无效；检查该客户端使用的凭据。本地交付的引导集成 API key 位于 Secret `bootstrap-api-key`、键 `api-key`；`source <(make kind-env)` 为脚本加载它。该 API key 不是 Console 密码。

Console 登录使用个人账户和配置的 HTTPS origin，或显式启用的 HTTP 回环模式。检查浏览器精确 origin 是否匹配 `adminapi.console.publicOrigin`，包括协议和端口。转发头不受信任，不能用于选择该 origin。失去安装管理权限时应遵循[账户恢复](../accounts.md#recover-installation-administration)。

HTTP 403 需要检查精确操作、租户和分组边界、显式 Deny 和权限边界。ReadLogs、DeleteLogs、执行控制和取消是独立授权。资源详情在授权范围外可能返回 404；安装管理不会授予工作负载访问权限。不要仅为让一个诊断请求通过而扩大权限。

## 触发已接受但运行未完成

触发被接受只确立提交身份，不代表终态执行。检查返回的运行或调度项、活动修订版本、JobRun、Attempt 轨迹，以及 Kubernetes Job 和 Pod 状态。检查 Operator 领导权、命名空间 RBAC、ResourceQuota、Pod Security、镜像拉取和执行网络目标。

Admin Service 的 `/healthz` 不检查数据库。Scheduler 和 Operator 就绪检查包含数据库 ping，但都不能证明已接受 Job 完成。控制平面没有明显活动时，物理工作负载仍可能在运行。

对于 `ExecutionUnknown` 或 `CancelUnknown`，检查原始 Attempt 身份、未知原因、首次未知时间和已保存停止证据。恢复对原始对象的可信观测。Pod 缺失、时间经过或取消请求被接受，都不能证明终止。在支持的恢复路径解决证据问题前，应继续占用并发。参见[执行恢复](../architecture.md)和[执行日志](../execution-logs.md)。

## 取消或卸载被阻塞

保持 PostgreSQL 和兼容 Operator 可用，直到原始 Job 确认停止且所需 Pod 终止证据已提交。检查 `orbitjob.io/termination-evidence` 终结器和 Attempt 的待释放状态。手动移除终结器会丢弃这一恢复边界。

停止新提交，并遵循[排空流程](upgrading.md#stop-submissions-and-drain)。保留证据义务尚未完成时，不要删除命名空间或 PVC。卸载命令返回不代表已获得可恢复备份或物理停止证据。

## 执行日志缺失、部分可用或达到容量限制

先检查获授权运行、Attempt 和归档身份，再检查采集器队列和偏移量状态、mTLS 连通性、归档就绪状态，以及目录报告状态。`partial`、`capacity`、`expired` 和 `deleted` 描述不同证据条件。空输出不能替代缺失的采集。

分别检查归档分配量、实际 PVC 容量、元数据存储和每个采集器的物理配额。Helm 配额声明不会创建文件系统配额。不要删除排队数据块，也不要将旧 `tail.db` 与新数据块混合。需要恢复时，应恢复一致的已停止采集器快照。清理权限和容量恢复参见[执行日志](../execution-logs.md)。

## 监控缺失或不完整

遵循[监控检查](monitoring.md#check-targets-rules-and-dashboard)。检查监控器命名空间、标签和端口选择器，发现的 Prometheus 目标、规则健康，以及精确的运行台账仪表盘 UID。本地监控辅助工具不会部署 PostgreSQL exporter 或归档和采集器监控器。

解读执行隔离计数前，检查采集成功状态和时间戳。提供的 CR StateSet 不包含 `ExecutionUnknown`，不能证明不存在执行不确定性。`kubectl top` 需要独立 Metrics API。

## 负载测试结论不明确

读取该次运行的 `result.json` 和 `report.md`。确认原始配置、生成语料、来源身份、已接受回执、租户 API key、命名空间、测试夹具启动身份和观测日志（journal）序列。工作被拒绝或跳过、运行未完成，以及缺失独立故障证据，都会阻止形成完整结论。重启测试夹具会丢失内存证据，无法修复该次运行的连续性。遵循[负载测试](../loadtest-guide.md)，并在开始新运行 ID 前保留原始制品。

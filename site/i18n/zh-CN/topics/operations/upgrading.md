# 升级 OrbitJob

在维护窗口内升级需要保留的安装。在候选版本通过所需检查前，应保留已有数据库、归档、采集器状态、资源身份和凭据。镜像更新、模式迁移和从备份恢复具有不同的兼容性要求。

对于历史上已检查的 kind 安装及其存储工作，除本流程外还应参照[本地安装维护](../local-maintenance.md)。该记录不能证明其他集群的当前状态。

## 开始前

安装管理员需要 Kubernetes 写入权限、数据库维护权限，以及暂停所有受影响工作负载的授权。准备以下内容：

- 当前部署的 Helm values、渲染清单、镜像摘要、组件副本数、CRD、工作负载 CR，以及命名空间与租户映射。
- 来自同一个已审查版本的候选 Chart、CRD、有序迁移和全部七个组件镜像。确认每个节点均可访问仓库。
- 协调的[备份与恢复计划](backup-and-recovery.md)，包含 PostgreSQL、归档、采集器、Secret 和迁移输入通道。
- 明确的维护窗口、测试工作负载和恢复决策点。

修改需要保留的安装前，先在隔离环境验证候选版本。阅读[数据库兼容性](../database-setup.md#migration-execution-and-compatibility)和适用的[已知限制](../known-issues.md)。Chart 渲染和以往源码测试都不能证明已安装环境的升级路径可用。

设置并检查实际安装上下文：

```bash
export RELEASE_NAMESPACE=orbitjob-system
export RELEASE_NAME=orbitjob
kubectl config current-context
helm status "$RELEASE_NAME" -n "$RELEASE_NAMESPACE"
```

## 准备候选版本

在停机前构建或获取所有组件镜像。使用唯一标签并记录摘要；重新构建可变标签可能不会改变已有 Pod。对照安装当前值审查候选 values。保留租户映射、存储类、容量预算、TLS origin、Secret 和允许的网络目标。不要用全新示例文件替换已有 values。

在不更改集群的情况下检查候选 Chart 和迁移打包：

```bash
make helm-migrations-check
helm lint ./charts/orbitjob -f /absolute/path/to/candidate-values.yaml
```

渲染到私有目录供审查。渲染结果可能包含 TLS 私钥。检查镜像身份、命名空间 RBAC、PVC 行为、准入设置和网络规则。Chart 的 pre-upgrade 钩子在更新 Deployment 前运行迁移，不会停止旧进程。Helm 回滚成功也不能撤销已提交迁移或外部工作负载副作用。

审查维护写入如何通过候选版本的准入策略。`admission.nativeWriters` 默认为 `[]`；此前使用 `kubectl` 的主体在升级后可能无法恢复工作负载 CR。资源受支持时，使用获授权的已检查定义 API；否则，应在停机前审查并配置实际原生写入方身份。不要为了使恢复通过而将 `kubernetes-admin` 广泛加入原生写入方允许范围。兼容旧版 v0 执行配置，不代表已检查 v1 定义 API 能够替代所有历史资源。

## 停止提交并排空

停止并阻止手动触发、Function 调用、Webhook 和其他提交客户端，包括原生工作负载 CR 提交者。根据已安装版本选择流程。v0.2.1 没有 `execution-controls` API、`ManageExecution` 权限或来源控制暂停列。

### 提供执行控制的安装

仅当已安装版本提供 `GET /api/v1/<kind>/<id>/execution-controls` 时使用本流程。对每个受影响的 Job、Workflow、Function 和 Check 执行以下步骤：

1. 使用所需的资源 Get 权限读取其 `GET /api/v1/<kind>/<id>/execution-controls` 响应。在私有维护记录中保存租户、来源身份、版本和两个暂停值。
2. 使用当前 `version`、`schedule_paused: true` 和原始 `execution_paused` 值 PATCH 控制设置。写入需要独立的 `ManageExecution` 权限。保留已有执行暂停。
3. 等待已接受工作完成，或通过支持的 API 取消。清单应包含已接受但尚未发布的运行、工作流步骤和保留 Pod。`ExecutionUnknown` 和 `CancelUnknown` 仍属于未解决状态。
4. 排空后，使用已确认的当前版本设置 `execution_paused: true`。确认最终边界期间没有提交被接受。

收到 409 后，需要重新读取并与最后一次已确认写入比较。不要覆盖其他管理员在此期间的变更。写入超时表示结果未知；之后观察到相等值不能证明该写入由本次维护产生。将每次成功 PATCH 的响应保存为维护写入收据。如果已有执行暂停阻止 Workflow 排空，应停止受影响流程，并取得该来源所有者的决定。

对于 Check 循环尚不执行来源调度暂停的旧镜像，还应使用 `POST /api/v1/checks/<id>/pause` 和从定义读取获得的 `{"version":N}` 暂停每个活动 Check 定义。此操作需要 `check:Get` 和 `check:Pause`。保留原始状态、租户、ID、创建时间、定义版本和已确认暂停响应。Check 定义版本必须与来源控制版本分开记录。已读取的批次仍可能接受迟到调度项，较早的已暂停 Check 循环也可能不会修复已接受但未发布的运行。连续几个间隔未观察到活动，不能证明此类工作已排空。

### 从 v0.2.1 升级

排空已接受工作期间，保持旧 Operator 可用。停止所有旧写入方前，完成以下步骤：

1. 保存每个 ScheduledJob 和 WorkflowJob 的原始 `spec`、UID、`resourceVersion`、generation 和 `spec.suspend` 值。核对 UID，以保存的 Kubernetes `resourceVersion` 为写入前提，仅设置 `spec.suspend: true`。保留原本已挂起的 CR。记录已确认的对象，并核对其 `status.observedGeneration` 和实际投影定义，之后才能认定挂起已生效。
2. 保存 Function 身份和状态。v0.2.1 为 Function 提供读取和调用接口，没有暂停或恢复写入 API。继续阻止调用方；不要直接在 PostgreSQL 中修改 Function 状态。
3. 使用 `GET /api/v1/checks/<id>` 读取每个 Check，仅对活动定义使用 `POST /api/v1/checks/<id>/pause` 和 `{"version":N}`，其中 `N >= 1` 为当前定义版本。此操作需要 `check:Get` 和 `check:Pause`。保存租户、ID、`created_at`、原始状态和版本，以及成功响应中的新版本。原本已暂停的 Check 继续保持暂停。收到 409 或写入超时后需要审查；超时后状态相等不能证明存在已确认的维护写入。
4. 将所有已接受收据与运行台账、JobRun CR 和物理 Kubernetes Job、Pod 关联核对。分页读取 `GET /api/v1/instances`，再读取每个运行的 `GET /api/v1/instances/<run_id>` 和 `GET /api/v1/instances/<run_id>/attempts`。这些读取需要 `instance:List` 和 `instance:Get`；`POST /api/v1/instances/<run_id>/cancel` 需要 `instance:Cancel`。Function 专属运行历史只包含终态运行，因此历史为空不能证明已排空。使用 v0.2.1 支持的筛选条件；不要向其实例 API 发送较新版本的 `ExecutionUnknown` 阶段。
5. 单独分页检查 Workflow 运行，使用 `GET /api/v1/workflows/<id>/runs` 和 `GET /api/v1/workflows/<id>/runs/<run_id>`，并核对每个运行的步骤。这些读取需要 `workflow:List` 和 `workflow:Get`。v0.2.1 的取消端点 `POST /api/v1/workflows/<id>/runs/<run_id>/cancel` 需要 `workflow:Trigger`。保持旧 Operator 可用，直到现有 Workflow 运行达到已确认终态或取消完成：其 walker 推进已有运行时不会检查任务 Job 的 `spec.suspend`。

如果无法单独隔离经 Admin API 提交的客户端，在完成上述 API 操作后，保存 Admin API 规格和副本数，再停止其入口。原生 CR 提交者必须另行阻止；无法做到时，应停止升级。保持 Operator 运行以完成排空。Admin API 停止后，通过只读数据库、CR 和物理工作负载检查重新核对并持续维护完整的运行与 Workflow 清单；后续观测不得依赖已停止的 HTTP 接口。

旧 ScheduledJob 调度器在挂起后会先于 `repairOpenRuns` 返回；旧 Check 循环暂停后也可能不会修复已接受但尚未发布的工作。如果仍存在任何已接受但尚未发布或其他状态未知的工作，应停止升级，保留收据、台账、CR 和物理证据。一段时间无活动和挂起本身都不能证明已排空。仅在完整清单已排空后，才可停止所有旧写入方，并按下一节进行最终复查。

v0.2.1 的台账和绑定 Attempt 已进入终态后，JobRun CR 仍可能暂时保留活动阶段。保持旧 Operator 可用，结合台账和物理工作负载检查其正常状态协调结果。不要手工将 CR 标记为终态以满足排空检查。

v0.2.1 暴露 Workflow 的 `schedule` 和 `suspend` 字段，但没有 Workflow cron 调度器。候选版本能够调度这些 Workflow。候选版本验证完成前，应保持其 CR 挂起；启用此前未生效的调度需要明确审查，且属于行为变更。

### 保留的执行证据

检查每个执行命名空间中保留证据的 Pod：

```bash
kubectl get pods -n YOUR_EXECUTION_NAMESPACE \
  -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.metadata.finalizers}{"\n"}{end}'
```

任何尚未完成的 `orbitjob.io/termination-evidence` 义务都需要兼容 Operator 和 PostgreSQL 持续可用，直到证据提交且 Operator 释放其自身终结器。不要强制删除保留 Pod、清除终结器，或仅凭 API 对象缺失释放 Forbid 并发槽。

## 停止旧写入方并备份

停止组件前保存实际组件规格和副本数。停止 Admin API、Scheduler 和 Operator，并验证其旧 Pod 已终止。按已保存默认配置，停止命令如下：

```bash
kubectl scale -n "$RELEASE_NAMESPACE" \
  deployment/orbitjob-admin-api deployment/orbitjob-scheduler deployment/orbitjob-operator \
  --replicas=0
```

对于自定义安装，不要将这些示例副本数当作恢复记录。写入方停止后，重新检查运行台账和物理工作负载；任何迟到或未解决的运行都意味着排空失败。

对于有采集器和归档的安装，在归档可用时让采集器完成发送。检查偏移量、排队数据块和归档状态；就绪状态不能证明队列为空。使用已记录的 DaemonSet 调度暂停措施停止采集器，然后停止归档。将每个节点的采集器目录作为整体保留。v0.2.1 没有这两个组件：记录其不存在，备份安装中实际存在的存储，无需执行不存在服务的发送步骤。审查候选版本新增归档与采集器的存储和容量。协调备份期间应保持 PostgreSQL 可用。遵循[备份与恢复](backup-and-recovery.md)，并在模式变更前验证备份。

## 应用 CRD 并升级

旧发布方停止后，单独应用已经审查的候选 CRD：

```bash
kubectl apply -f charts/orbitjob/crds/
```

普通升级期间，Helm 不会更新 Chart 的 `crds/` 目录中的 CRD。执行此步骤前应审查 CRD 变更；其兼容性属于维护边界的一部分。

使用相同发行版命名空间、Secret 和迁移输入通道运行候选升级：

```bash
helm upgrade "$RELEASE_NAME" ./charts/orbitjob \
  --namespace "$RELEASE_NAMESPACE" \
  -f /absolute/path/to/candidate-values.yaml \
  --wait --wait-for-jobs --timeout 10m
```

Chart 应用新清单时会恢复配置的副本数和采集器调度。启动期间应保持来源控制暂停、旧版 CR 挂起、Check 定义暂停，并继续阻止提交客户端。如果需要逐组件手动启动，应在使用 Helm 前准备并审查清单转换，使相关工作负载保持停止；Chart 没有通用副本保持设置。

任何钩子失败后都应保持提交暂停。重试前检查完整迁移台账。此前迁移可能已经提交。保留原始 Helm ConfigMap 字节表示。绝不能通过编辑台账或历史 SQL 绕过校验和失败。

### 建立候选版本执行控制记录

对于保留的 Job 和 Workflow，通过候选版本获授权的定义接口重新查明各自当前的活动修订 ID；Function 和 Check 使用保留的定义 ID。使用相应 ID 读取 `GET /api/v1/<kind>/<id>/execution-controls`。按实际返回结果保存租户、`source_mode`、`source_uid`、`version`、两个暂停值、`actor` 和 `updated_at`，包括审计字段缺失的情况。迁移 `0022` 不回填控制记录：没有行时，读取结果为版本 `0`，两个暂停值均为 `false`。这些值不能证明旧版 CR 或 Check 暂停由本次维护产生。

如果本次维护新增候选版本控制暂停，使用 `ManageExecution` 权限，按实际当前版本和两个预期暂停值执行 PATCH。将已确认响应与旧版维护记录分开保存。候选版本检查期间，继续保持旧版 CR、Check 定义和客户端的停止状态。候选版本控制的恢复只能依据本次维护自身已确认的写入，并遵循下述规则。

使用 `executionPolicyVersion: 1` 的 Workflow 要求每个引用 Job 的活动修订具有完整解析的执行策略版本 1 设置和不可变镜像摘要。保留的执行策略版本 0 Job 不会自动满足该要求。通过 [Job 编写流程](../tasks/manage-jobs.md)审查并发布符合要求的 Job，再从这类 Workflow 引用它们。

引用 Job 尚未就绪时，API 仍可能接受已投影 Workflow 的触发请求。其 WorkflowRun 可能以 `workflow_reference_not_ready` 保持 `Pending`，此时尚无运行台账或步骤。保留已接受的 WorkflowRun 收据，检查其条件和引用的 Job 版本；触发已接受本身不能证明父运行已获准入。

## 验证并恢复提交

恢复正常提交前，必须满足以下所有条件：

1. 预期组件镜像正在运行，滚动发布检查成功，数据库迁移行与候选输入匹配，凭据保持一致。
2. 历史定义、运行与 Attempt 身份、账户登录和保留输出仍可通过获授权接口读取。
3. 采集器存储与归档预算匹配真实容量；采集和读取能够在预期重启后继续工作。检查内部证书有效期。
4. 执行隔离清单完整且为最新状态，排空未留下未解决义务。应结合观测时间戳、采集成功状态和计数判断。不得汇总相加各副本快照。
5. 一个有意启用的测试来源能够验证、保存、投影、触发，并以正确身份和日志达到预期终态结果。检查幂等重放、取消，以及允许和拒绝的网络目标。
6. [监控](monitoring.md)目标和规则健康，且所有缺失监控器或证据来源均已查明。

只有来源 UID、控制版本和两个暂停值仍与最后一次已确认维护写入一致，才能恢复来源控制暂停状态。使用验证后的 CAS 版本执行恢复。身份或版本变化、并发编辑或响应不明确时，需要审查。原本已暂停的来源应恢复为原始暂停状态。不要将旧版 CR 或 Check 暂停记录当作候选版本控制写入收据。

通过已审查的已检查定义 API 或原生写入方，单独恢复旧版 CR。将维护记录、当前 UID、`resourceVersion` 和相关 `spec` 与已确认挂起记录核对；计入已审查的候选版本投影变更，发现并发或无法解释的编辑时停止恢复。以当前已验证的 Kubernetes `resourceVersion` 为前提，仅恢复原始 `spec.suspend` 值。保留原本的挂起状态。对于 v0.2.1 未执行的 Workflow 调度，在解除挂起前完成明确的调度审查。

Check 定义应单独恢复，且仅当本次维护将其从活动改为暂停时才可恢复。确认租户、ID、创建时间、状态，以及完全一致的已确认定义版本，然后使用该版本和 `check:Resume` 调用 `POST /api/v1/checks/<id>/resume`。游标时间戳或旧审计操作者不能证明暂停由本次维护产生。

在安装维护记录中记录实际候选版本、环境、检查和剩余限制。升级证据适用于记录的版本和配置。生产吞吐量与高可用性需要单独验证。

## 从失败升级恢复

保持提交暂停，并保留失败钩子、应用日志和数据。如果兼容修正版本能够保留已记录模式和执行事实，应优先使用它。只有旧二进制仍遵循新的数据库、资源地址和执行约定时，恢复旧 Deployment 规格才安全。

需要恢复数据时，停止所有写入方，并恢复协调一致的备份，包含数据库和备份边界上存在的归档或采集器状态。条件允许时，先在隔离环境中验证。不要将旧偏移量数据库与较新采集器数据块组合，不要移除不确定性事实，也不要独立于审计历史回滚账户。参见[备份与恢复](backup-and-recovery.md)。

## 卸载需要保留的安装

卸载前完成相同的提交暂停、排空和证据终结器检查。在兼容组件和数据库访问仍可用时备份安装。仅在这些检查完成后执行：

```bash
helm uninstall "$RELEASE_NAME" --namespace "$RELEASE_NAMESPACE"
```

检查剩余 PVC、Secret、CRD、工作负载 CR 和采集器目录。Helm 卸载不是完整数据擦除流程，也不能证明所有物理工作负载已停止。仅在安装所有者明确作出保留决策后，才可删除保留数据。不要通过删除共享命名空间清理 OrbitJob 资源。

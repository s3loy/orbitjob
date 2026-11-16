---
title: "Helm 与 Kubernetes"
---

<a id="7-helm-and-kubernetes"></a>

Chart 位于 [charts/orbitjob/](https://github.com/s3loy/orbitjob/blob/main/charts/orbitjob/)，版本由 [Chart.yaml](https://github.com/s3loy/orbitjob/blob/main/charts/orbitjob/Chart.yaml) 定义。本流程面向安装管理员，要求 Go 1.27.3+（运行配置工具）、Python 3 和 OpenSSL（准备 Secret）、已有 PostgreSQL 实例、用于 chart 资源的集群管理员权限、持久存储，以及从同一源码修订构建的完整组件镜像。使用已发布软件时，应使用对应版本的 chart 和说明。旧 `v0.2.1` 镜像不包含此分支的 Console 和日志归档，不应将当前 chart 的默认 appVersion 作为镜像选择依据。

<a id="71-namespace-topology"></a>

<a id="namespace-topology"></a>

### namespace 拓扑

安装后的 namespace 布局：

| namespace | 内容 |
|---|---|
| `orbitjob-system` | Admin API、scheduler 和 operator Deployment；日志归档 StatefulSet/PVC、日志采集器及安装 hook Job |
| `operator.namespaceTenants` 中配置的 namespace | ScheduledJob/JobRun 和 WorkflowJob/WorkflowRun CR，以及生成的容器 Job |
| `monitoring` | kube-prometheus-stack：Prometheus、Grafana、告警（可选，通过 `make monitoring-up` 安装） |

一次安装使用一个 `orbitjob-database` Secret，所有副本共享。

<a id="72-first-install"></a>

<a id="first-install"></a>

### 首次安装

**步骤 1：准备数据库 Secret。** 从 bootstrap DSN 生成配置：

```bash
install -m 0600 /dev/null /tmp/orbitjob-bootstrap-dsn
printf '%s\n' 'postgres://<owner>:<password>@<host>:5432/orbitjob?sslmode=require' \
  > /tmp/orbitjob-bootstrap-dsn

go run ./cmd/configure setup \
  --mode external \
  --database-dsn-file /tmp/orbitjob-bootstrap-dsn \
  --state .runtime/database.json \
  --runtime-env .runtime/database.env
```

该命令创建本地安装状态，不会自行安装 Kubernetes Secret。安装 Helm 前，按[数据库配置](/orbitjob/docs/database/)中的 Secret 流程应用该状态。默认数据键在 `charts/orbitjob/values.yaml` 中定义：

```text
bootstrap-owner-dsn  migrator-dsn  admin-dsn  runtime-dsn
migrator-password    admin-password  runtime-password  bootstrap-api-key
```

完整约束见 [`docs/database-setup.md`](/orbitjob/docs/database/)。

**步骤 2：选择已审阅镜像并安装。** 在获准环境中构建并发布所选修订的镜像，然后选择 registry 和 tag。Chart 的默认值及 tag 回退规则见 values 与 Chart.yaml。选择包含全部七个 OrbitJob 组件的可用 tag：admin-api、scheduler、operator、bootstrap、migrate、log-archive 和 admission。`make docker-build` 从当前源码构建这些目标；Admin 镜像还会构建并内置 Console。日志采集器使用 chart 中独立固定的 Fluent Bit 镜像。

例如，使用集群可访问的 registry 和 tag 构建组件镜像：

```bash
make docker-build REGISTRY=registry.example.com TAG='<reviewed-tag>'
```

按 registry 的镜像交付流程发布这些镜像，并在安装前配置必要的镜像拉取权限。保存以下 values：

```yaml
global:
  imageRegistry: registry.example.com
  imageTag: "<reviewed-tag>"

images:
  admin: {repository: orbitjob-admin-api}
  scheduler: {repository: orbitjob-scheduler}
  bootstrap: {repository: orbitjob-bootstrap}
  migrate: {repository: orbitjob-migrate}

operator:
  image: {repository: orbitjob-operator}
  # First installation: map this namespace to the reserved Default tenant.
  namespaceTenants: "orbitjob-tasks=00000000000000000000000001"

taskNamespace:
  create: true
  name: orbitjob-tasks

logs:
  image: {repository: orbitjob-log-archive}

admission:
  image: {repository: orbitjob-admission}
```

将配置保存为 `values.installation.yaml`，并将 registry 和镜像 tag 替换为此次安装准备的已审阅镜像。示例创建 `orbitjob-tasks` 并将其映射到预留 Default 租户，因此首次安装无需从尚未启动的 API 获取租户 ID。安装后添加工作区和 namespace 映射时，使用实际创建租户所返回的 ID。以逗号分隔的映射应放在 values 文件中，不应通过普通 `--set` 传递。

运行 Helm 前，先按下一节选择安装地址。

<a id="installation-address-and-external-access"></a>

#### 安装地址与外部访问

已安装的 Console 与 HTTP API 共享同一来源。必须通过相同的公开协议、主机和端口提供 `/console/`、`/api/v1/` 和 `/openapi.json`，保留原始请求路径，包括嵌套 Console 导航 URL。服务端将 `/` 重定向到 `/console/`，并在 `/console/` 下提供 Console 导航。不支持在 `/orbitjob/` 等额外前缀下安装。更改公开域名或 IP 不需要重新构建前端。将 `adminapi.console.publicOrigin` 设为精确的 HTTPS 来源，或留空并通过代理保留公开 Host；否则服务端使用 `https://` 加该 Host 作为预期来源，不信任转发 header 来决定来源。Cookie 认证写入强制执行精确的 Origin 和 CSRF 检查。参阅 [Console 来源配置](/orbitjob/docs/accounts/#https-and-local-development)。

默认 `adminapi.service.type` 为 `ClusterIP`，Service 端口为 `8080`，Ingress 关闭。内部访问、本地端口转发或能够访问 Service 的已有代理可使用此默认配置。外部访问选择以下一种配置，保存为 `values.access.yaml`，与 `values.installation.yaml` 一同使用。

**通过 Ingress 提供 HTTPS 域名访问。** 管理员必须提供已安装的 Ingress controller 及其 class、指向该 controller 的 DNS，以及 `orbitjob-system` 中已有的 `kubernetes.io/tls` Secret，其证书必须覆盖目标主机名。Chart 不安装 Ingress controller 或证书管理器、不创建 DNS 记录，也不签发公开证书。安装 release 前，先在数据库配置阶段准备的 release namespace 中安装 TLS Secret。例如，使用管理员提供的证书和私钥文件：

```bash
kubectl -n orbitjob-system create secret tls orbitjob-public-tls \
  --cert=/path/to/fullchain.pem --key=/path/to/private-key.pem
```

通过 controller 支持的 annotation 或 gateway 配置设置 HTTPS 重定向和访问控制策略。以下示例选择已有的 `nginx` IngressClass，应替换为实际安装的 class：

```yaml
adminapi:
  ingress:
    enabled: true
    className: nginx
    host: orbitjob.example.com
    tls:
      enabled: true
      secretName: orbitjob-public-tls
```

安装来源为 `https://orbitjob.example.com`。`ingress.host` 接受 DNS 主机名，不接受 URL、端口或 IP 地址。公开 TLS 要求非空主机名和已有 Secret 名称。可选 `adminapi.ingress.annotations` 将 controller 专用设置传递到 Ingress。Ingress 仅转发 `/`（精确匹配）、`/console` 和 `/api/v1`（前缀匹配），以及 `/openapi.json`（精确匹配），不转发 `/healthz` 或 `/metrics`。

**通过 Service 提供 IP 访问。** 集群节点地址可达时，选择 NodePort，并在节点防火墙或网络安全规则中允许所选端口：

```yaml
adminapi:
  service:
    type: NodePort
    port: 8080
    nodePort: 30080
```

使用浏览器可达的节点地址，例如 `http://192.0.2.10:30080/console/`；将示例 IP 替换为实际地址。`nodePort` 必须位于集群配置的范围内。省略时由 Kubernetes 分配端口，然后通过 `kubectl -n orbitjob-system get svc orbitjob-admin-api` 查看。已集成负载均衡器的集群可改设 `adminapi.service.type: LoadBalancer`，省略 `nodePort`，使用分配的外部地址及 `adminapi.service.port`。供应商专用 Service 设置可通过 `adminapi.service.annotations` 传递。

NodePort 和 LoadBalancer 暴露整个 Admin HTTP 服务，包括 `/healthz` 与 `/metrics`。应用在该端点提供 HTTP；选择任一 Service 类型都不会配置 TLS 或 gateway 访问控制。不支持通过非回环 IP 地址使用 HTTP 登录 Console。应在 Service 前配置受控的 HTTPS gateway 或反向代理，并限制直接访问。HTTP 也会使自动化 Bearer key 在传输中暴露；遵循[安全策略](/orbitjob/docs/security/)。

**通过已有 Ingress controller 提供 IP 访问。** 如果 controller 接受无主机名规则，可省略主机名并保持公开 TLS 关闭：

```yaml
adminapi:
  ingress:
    enabled: true
    className: nginx
    host: ""
    tls:
      enabled: false
```

这只配置 HTTP 路由，可以提供公开 Console 静态资源，但账户登录要求 HTTPS 端点及对浏览器访问地址有效的证书。该规则接受任意 HTTP Host，管理员必须考虑该 controller 处理的其他应用。使用账户或传输集成 key 前，先配置 HTTPS 和访问控制。

**已有反向代理。** 代理可以访问 `orbitjob-admin-api.orbitjob-system.svc:8080` 时，保持 Service 为内部服务。对于集群外代理，提供受限的 NodePort、LoadBalancer 或其他管理员维护的 Service 访问路径。将精确的 `/` 与 `/openapi.json` 请求，以及 `/console` 与 `/api/v1` 前缀转发到该 Service，保留路径和查询字符串。嵌套 Console URL 应路由到 Admin 服务，不能交给独立静态文件回退。在代理处终止 HTTPS 并执行外部访问策略，从公开路由中排除 `/healthz` 和 `/metrics`，Console 与 API 均使用代理的公开来源。

使用所选暴露配置安装：

```bash
helm upgrade --install orbitjob charts/orbitjob \
  --namespace orbitjob-system \
  --create-namespace \
  -f values.installation.yaml \
  -f values.access.yaml \
  --wait --wait-for-jobs
```

内部访问或本地端口转发可省略 `-f values.access.yaml`，保留默认 ClusterIP 配置。通过 HTTP 回环地址登录 Console 还需设置 `adminapi.console.allowInsecureLocalhost: true`。生产环境保持关闭；该选项不允许通过节点 IP 建立 HTTP 会话。

**步骤 3：检查安装并建立访问。** 检查 Pod 和 Helm 状态：

```bash
kubectl get pods -n orbitjob-system
helm status orbitjob -n orbitjob-system
```

查看所选 Service，以及启用时的 Ingress，获取分配地址：

```bash
kubectl -n orbitjob-system get svc orbitjob-admin-api
kubectl -n orbitjob-system get ingress orbitjob-admin-api
```

Ingress 关闭时省略对应命令。在所选安装来源打开 `/` 或 `/console/`。内部访问或初始管理可改用端口转发；如果 `adminapi.service.port` 不使用默认值，应相应更改右侧端口：

```bash
kubectl -n orbitjob-system port-forward svc/orbitjob-admin-api 18080:8080
```

保持转发终端运行。在 `http://localhost:18080/console/` 进行本地 HTTP 登录时，按上文显式启用回环会话；否则使用配置的 HTTPS 来源。归档使用受 mTLS 保护的独立内部 Service，不应通过公开 Console/API 路由暴露。

按[首个管理员设置](/orbitjob/docs/accounts/#create-the-first-administrator)为所选 Console 来源签发一次性链接，创建账户并邀请工作区成员。使用成员账户完成[首个任务](/orbitjob/docs/getting-started/)，检查工作负载执行。Pod Ready 和成功登录本身不能证明任务已成功运行。

<a id="73-day-to-day-operations"></a>

<a id="day-to-day-operations"></a>

### 日常运维

所有命令均假设使用 `orbitjob-system` namespace。设置默认 namespace 可减少重复输入：

```bash
kubectl config set-context --current --namespace=orbitjob-system
```

**观测：**

| 命令 | 用途 |
|---|---|
| `kubectl get pods -A` | 集群全局概览，定位异常 Pod |
| `kubectl get pods,svc,deploy -o wide` | 控制平面资源、IP 和节点 |
| `kubectl get jobs -n '<tenant-namespace>'` | `operator.namespaceTenants` 配置的 namespace 中的容器 Job 执行 |
| `kubectl get endpoints` | 确认 Service 具有 Pod endpoint；空 endpoint 是常见故障原因 |
| `helm list -A` | 已安装 release 和修订 |

**诊断：**

| 命令 | 用途 |
|---|---|
| `kubectl describe pod '<pod>'` | 事件、探针配置、镜像、环境变量 |
| `kubectl logs '<pod>' --tail=50` | 最近日志 |
| `kubectl logs -l app.kubernetes.io/name=orbitjob-operator --tail=50` | 按标签读取日志，无需写死 Pod 名称 |
| `kubectl logs '<pod>' --previous` | 上次崩溃前的日志，用于诊断重启循环 |
| `kubectl get events --sort-by=.lastTimestamp` | 按时间排序的集群事件 |

OrbitJob 运行镜像基于 `scratch` 构建，不包含 shell。`kubectl exec -it '<pod>' -- sh` 会返回 `executable file not found`。需要进入检查时，使用临时调试容器：

```bash
kubectl debug -it '<pod>' --image=busybox --target='<container>'
```

多数情况可使用 `kubectl logs` 和 `kubectl get endpoints` 诊断，很少需要 exec。

**API 端口转发**，每个转发占用一个终端：

```bash
kubectl port-forward svc/orbitjob-admin-api 18080:8080
```

监控 Service 名称取决于监控 release；使用[本地部署](/orbitjob/docs/deployment/prerequisites/)中的命令，或查看 `kubectl -n monitoring get svc`。

**重启与扩缩容：**

```bash
kubectl rollout restart deploy/orbitjob-scheduler  # rolling restart
kubectl rollout status deploy/orbitjob-scheduler   # watch the rollout
kubectl delete pod '<pod>'                          # delete a pod to force recreation
```

Operator 副本通过 Kubernetes Lease 共享领导权。Scheduler 多副本需要单独配置可选的选主路径；仅增加副本数不能保证 SLO 求值单实例运行。

<a id="74-upgrade-and-uninstall"></a>

<a id="upgrade-and-uninstall"></a>

### 升级与卸载

更改 chart 或 values 后，先审阅迁移与兼容性要求，再升级。对于迁移 0012，必须先停止旧 operator，执行迁移后再启动新版本。滚动重叠可能对缺少匹配幂等事实的投影计数。历史终态行保留聚合，并在切换时确认；自动恢复覆盖之后的终态转换。参阅[迁移兼容性](/orbitjob/docs/database/#migration-lineage-and-compatibility)。增量执行观测迁移 0013 也要求兼容的写入方和 CRD schema。遵循[升级指南](/orbitjob/docs/deployment/upgrading-an-existing-installation/)，不要重写早期 Console 迁移校验和，也不要将 Helm rollback 视为数据库降级。

仅在迁移流程允许所选发布方式时执行升级：

```bash
# If images changed, load them into the kind nodes first
kind load docker-image --name orbitjob-dev '<image>:<tag>'

# Upgrade the release
helm upgrade orbitjob charts/orbitjob -n orbitjob-system -f values.installation.yaml --wait
```

编辑 `db/migrations/*.up.sql` 后，同步 chart 副本：

```bash
make helm-migrations-sync
make helm-migrations-check
```

卸载前，先通过各来源的控制项停止新的计划和手动提交。等待已有执行完成，或请求取消。在终止证据提交且全部 OrbitJob 证据 finalizer 释放前，保持兼容的 Operator、PostgreSQL 和 namespace RBAC 可用。检查 `operator.namespaceTenants` 中的每个 namespace：

```bash
kubectl -n '<tenant-namespace>' get pods \
  -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.metadata.finalizers}{"\n"}{end}'
```

只要仍有 `orbitjob.io/termination-evidence` finalizer，卸载或降级前就必须恢复。修复兼容 Operator 时，应保留数据库和已保留的 Pod；手动移除 finalizer 会丢弃此保护。旧 Operator 不能完成这些处理义务。

`orbitjob_execution_recovery_total` 的 `hold_*` 结果统计恢复事件，重复观测可能使其增加。使用上面的 Pod 清单确认仍待保留证据的对象；这些计数器不是当前保留的 Pod 数量。

Operator 还从账本读取当前 `ExecutionUnknown` 和 `CancelUnknown` 处理义务，包括 CR 缺失的执行。检查 `orbitjob_execution_isolated_runs`、`orbitjob_execution_isolation_oldest_timestamp_seconds` 和 `orbitjob_execution_isolation_observed_timestamp_seconds`。每个副本观测相同的已配置租户范围；同一安装内应使用 `max`，不要累加副本。观测失败时保留上次快照，并将 `orbitjob_execution_isolation_collection_success` 设为零。过期或不完整的清单不能证明所有工作已停止。使用 `orbitjob runs get ID` 读取获准的 attempt 记录，检查原始 Job UID、首次未知时间和原因。恢复该证据后，才能恢复受影响来源。

本地存储或网络变更，应在停止组件前遵循协同[维护流程](/orbitjob/docs/local-maintenance/)。

排空完成后：

```bash
helm uninstall orbitjob -n orbitjob-system
```

Chart 不删除外部 PostgreSQL 数据，也不清理通过 `operator.namespaceTenants` 配置的租户 namespace 中遗留的 Job。

<a id="75-common-troubleshooting"></a>

<a id="common-troubleshooting"></a>

### 常见故障诊断

**Pod Running 但未 Ready。** 按以下顺序检查：

1. `kubectl describe pod`：检查探针失败原因（`Readiness probe failed: ...`）。
2. `kubectl logs`：检查应用错误。
3. `kubectl get endpoints`：确认依赖 Service 有后端。

探针失败通常表示数据库或下游 Service 等依赖未就绪，不代表进程崩溃。诊断崩溃时使用 `--previous` 日志。

**PostgreSQL 连接失败。** Pod 显示 `0/1` Running、未 Ready，且 `kubectl logs` 出现 `connection refused` 时，检查：

```bash
kubectl get endpoints orbitjob-postgres -n orbitjob
```

`<none>` 表示 Service 后没有 Pod，可能是 Deployment 副本数为零，或 PostgreSQL 未部署到该 namespace。

**健康检查边界。** Scheduler/operator 的 `/readyz` 检查数据库连接；Admin API 的 `/healthz` 仅检查存活。就绪不能证明调度、Job 完成或业务恢复。判定执行路径健康前，应检查组件日志和实际 run/attempt 进度。

**容器 Job 未运行：**

```bash
kubectl -n '<tenant-namespace>' get jobs,pods
kubectl -n '<tenant-namespace>' describe job '<name>'
kubectl auth can-i create jobs \
  --as system:serviceaccount:orbitjob-system:orbitjob-operator \
  -n '<tenant-namespace>'
```

使用 `operator.namespaceTenants` 中映射到 JobRun 租户的 namespace 键。检查镜像、命令、operator RBAC、配额和 Pod Security 限制，再对比对应 JobRun CR 的 status conditions。生成的工作负载 Pod 设置 `automountServiceAccountToken: false`，因此不接收 Kubernetes API token。

**ImagePullBackOff。** 可能由镜像缺失、tag 错误、registry 凭据或网络故障引起。本地 kind 构建应确认通过 `kind load docker-image --name '<cluster>' '<image>:<tag>'` 载入了准确 tag。重新创建 Pod 前，先检查 Pod 事件。

<a id="76-kind-verification"></a>

<a id="kind-verification"></a>

### kind 验证

需要 Docker、kind、kubectl、Helm 和 OpenSSL：

```bash
make kind-verify
```

脚本创建或复用 `orbitjob-verify` 集群，构建并载入镜像，安装 PostgreSQL 和 chart，并验证重复升级、迁移失败回滚和数据保留。成功时删除临时集群；失败时保留用于调查。

日常开发使用 `orbitjob-dev` 集群：

```bash
make kind-up         # create the cluster
make kind-status     # nodes and pods
make kind-down       # delete the cluster after typing delete-kind to confirm
```

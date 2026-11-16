# 监控安装

同时监控控制平面健康、数据库可用性、执行不确定性、资源压力和日志存储。Pod 就绪或抓取成功不能证明已提交工作负载正确完成。

仓库为本地 kind 环境提供可选的 kube-prometheus-stack 配置。它是独立于 OrbitJob 的 Helm 发行版。生产安装可以使用已有监控栈，并配置等效的抓取和告警。

## 开始前

取得安装监控 CRD 和集群读取组件的权限，准备持久 StorageClass、足够集群容量，以及目标上下文的 kubectl 访问。辅助工具在 `monitoring` 中安装 `kube-prometheus-stack`，使用 10-GiB Prometheus PVC、36 小时保留期和 8-GiB 保留大小。Makefile 固定 Chart 版本为 91.4.1。

提供的监控器面向控制平面命名空间 `orbitjob-system` 和 PostgreSQL 命名空间 `orbitjob`。应用前，应在持续维护的副本中调整其他命名空间、端口或工作负载标签。辅助工具 values 禁用 kind 中不可用的 controller-manager、scheduler、etcd 和 proxy 抓取端点，还配置 Grafana `admin` / `admin` 和匿名 Viewer 访问。此配置仅限受信任本地环境；暴露共享监控服务前应设置安装的正常访问策略。

## 安装本地监控栈

从仓库根目录检查目标上下文并安装：

```bash
kubectl config current-context
make monitoring-up
kubectl rollout status -n monitoring deployment/kube-prometheus-stack-grafana --timeout=5m
kubectl get pods,pvc -n monitoring
```

辅助工具安装 kube-prometheus-stack、OrbitJob 监控器、告警规则和运行台账仪表盘。它不会部署 PostgreSQL exporter。对于仓库本地 PostgreSQL 清单，应单独部署 exporter：

```bash
kubectl apply -f deploy/monitoring/postgres-exporter.yaml
kubectl rollout status -n orbitjob deployment/orbitjob-postgres-exporter --timeout=3m
```

该 exporter 读取本地 `orbitjob-postgres` Secret 和端点。外部 PostgreSQL 需要范围适当的监控凭据，以及外部服务自己的 exporter 或监控流程；不要原样应用本地凭据清单。

在不同终端启动端口转发：

```bash
kubectl port-forward -n monitoring svc/kube-prometheus-stack-prometheus 9090:9090
```

```bash
kubectl port-forward -n monitoring svc/kube-prometheus-stack-grafana 3000:80
```

在 `http://localhost:9090` 打开 Prometheus，在 `http://localhost:3000` 打开 Grafana。除非安装访问控制允许更大暴露范围，否则应保持绑定回环地址。

## 检查目标、规则与仪表盘

端口转发运行时执行：

```bash
make observability-status
```

成功要求监控发行版存在、Prometheus 可访问且就绪、已声明的 OrbitJob 抓取池有健康目标、OrbitJob 告警规则正在求值，以及指定仪表盘 ConfigMap 存在。仪表盘必须有 sidecar 标签 `grafana_dashboard=1` 和 JSON UID `orbitjob-run-ledger`。

Prometheus PVC 缺失时，脚本会警告。可选 Grafana 转发不可用时也会警告；声称监控设置完整前应检查。Grafana 返回仪表盘缺失、HTTP 错误、无效 JSON 或不同 UID 时，检查失败。其他无关仪表盘不能满足该检查。

如果 Prometheus 已配置抓取池但没有目标，应比较命名空间选择器、Pod 标签和端口选择。如果没有 OrbitJob 抓取池或规则，应检查监控器、规则选择器和 CRD 发现。仅有健康 Prometheus 进程不能证明应用监控已建立。

## 组件端点

运维端点属于安装内部接口。默认公开 Ingress 转发 Console/API 路径，不包含健康和指标路径。NodePort 或 LoadBalancer 会暴露整个 Admin 监听器。

| 组件 | 监听端口 | 健康与就绪 | 指标 |
|---|---|---|---|
| Admin API | 8080 | `/healthz`；没有依赖数据库的 `/readyz` | `/metrics` |
| Scheduler | 6060 | `/healthz`；`/readyz` 检查数据库 | `/metrics` |
| Operator | 默认 8082 | `/healthz`、`/readyz` | `/metrics` |
| 日志归档 | 8083，独立于 mTLS 归档端口 8443 | `/healthz`、`/readyz` | `/metrics` |
| 准入 | 8084，独立于 Webhook 端口 8443 | `/healthz`、`/readyz` | 提供的监控器未定义应用抓取 |
| Fluent Bit 采集器 | 2020 | 使用 `/api/v1/metrics` 进行就绪检查 | Fluent Bit HTTP 指标接口 |

提供的 PodMonitor 覆盖 Admin API、Scheduler 和 Operator，没有配置归档或采集器抓取。依赖归档和采集器指标或告警前，应增加适合安装环境的监控器。`kubectl top` 还需要可工作的 Kubernetes Metrics API；应用 Prometheus 抓取不提供该 API。

## 读取执行与存储信号

结合底层健康与执行事实判断：

| 信号 | 含义与下一步检查 |
|---|---|
| `orbitjob_operator_reconcile_errors_total` | 检查受影响资源的协调日志和原始 CR 身份 |
| `orbitjob_operator_job_creation_failures_total` | 检查准入、RBAC、配额和 Job 创建或接管响应 |
| Operator 单例 Lease | 续租缺失或过期会阻止单例调度与保留工作；检查 Operator 和 API 连通性 |
| `orbitjob_execution_isolated_runs` | 隔离执行阶段的台账快照；检查获授权运行的 Attempt 和证据 |
| `orbitjob_execution_isolation_collection_success` | 零表示清单未能完整采集 |
| `orbitjob_execution_isolation_observed_timestamp_seconds` | 解读隔离计数前检查新鲜度 |
| `orbitjob_archive_storage_bytes` / `orbitjob_archive_budget_bytes` | 已分配归档字节与配置分配上限；需先抓取归档指标 |
| `orbitjob_http_requests_total` 和 `orbitjob_ratelimit_hits_total` | 请求错误和准入压力；比较路由、状态和租户范围 |

隔离 gauge 是快照。对于同一安装和阶段，应在副本间取 `max`，不要将重复清单相加。过期或失败的采集会保留之前的计数，不能证明未解决义务为零。快照覆盖 Operator 配置的租户映射，包括映射中已暂停的租户。

提供的 kube-state-metrics `jobrun_phase` 是 StateSet：仅值为 `1` 的序列标识当前列出的阶段。仅有带阶段标签的序列存在，不能说明运行处于该阶段。当前阶段列表不包含 `ExecutionUnknown`；应使用专用隔离清单和获授权运行证据判断不确定性。CR 指标和数据库台账证据也可能相互滞后，因此判定两者矛盾前应检查身份和时间戳。

归档分配指标不衡量采集器目录配额或数据库元数据容量。三者应分别检查。ReadLogs 和 DeleteLogs 是独立能力；监控角色不应仅为检查存储而获得清理权限。

## 响应告警

提供的规则涵盖抓取失败、Admin API 可用性、Operator 协调和创建错误、领导权过期、Pending 运行、取消延迟、隔离积压与过期、exporter 失败、节点压力和 API 错误率。应根据工作负载审查阈值，不要将示例规则视为生产服务目标。

对于隔离或取消告警，应保留原始 Job 和 Pod 证据。不要通过清除终结器或重试不确定 Attempt 来消除告警。对于存储压力，遵循[执行日志](../execution-logs.md)和[故障排查](troubleshooting.md)，同时保留偏移量、数据块和归档身份。数据库审计和历史清理需要各自获得授权的维护流程。

## 移除本地监控

仅停止为本任务启动的端口转发进程。对于专用本地监控安装：

```bash
make monitoring-down
```

此命令删除独立发行版和 `monitoring` 命名空间。其他应用共享该命名空间时不得使用此辅助工具。删除前审查 PVC/PV 保留行为并保存所需指标。它不会删除单独部署的 PostgreSQL exporter；不再需要时应单独删除自有清单。移除监控不会卸载 OrbitJob。

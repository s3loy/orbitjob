# 安装 OrbitJob

完成[前提条件](prerequisites.md)后，在 Kubernetes 集群安装当前源码。本流程用于新安装。如果已有任何 OrbitJob 数据库、工作负载 CR 或发行版，应使用[升级 OrbitJob](../operations/upgrading.md)流程。贡献者的 kind 环境参见[本地开发](../local-deployment.md)。

## 开始前

获取集群管理权限、所有节点均能拉取的镜像仓库、已准备的 PostgreSQL 数据库和持久归档存储。选择源码修订版本，并为该修订版本指定唯一镜像标签。将 Chart、CRD、迁移和全部七个组件镜像作为同一制品集合保留。

命令使用以下示例名称：

```bash
export RELEASE_NAMESPACE=orbitjob-system
export RELEASE_NAME=orbitjob
export EXECUTION_NAMESPACE=orbitjob-tasks
export ORBITJOB_REGISTRY=registry.example.com/your-team
export ORBITJOB_IMAGE_TAG=source-REPLACE_WITH_REVISION
kubectl config current-context
```

继续前应替换仓库和镜像标签。使用集群已有的镜像拉取机制配置仓库凭据。Chart 没有 `imagePullSecrets` 值；不能假定节点能够使用本地 Docker 登录凭据。在选定修订版本的仓库根目录运行其余命令。

## 构建与发布镜像

使用同一标签构建七个组件镜像：

```bash
make docker-build REGISTRY="$ORBITJOB_REGISTRY" TAG="$ORBITJOB_IMAGE_TAG"
for component in admin-api scheduler operator migrate bootstrap log-archive admission; do
  docker push "$ORBITJOB_REGISTRY/orbitjob-$component:$ORBITJOB_IMAGE_TAG"
done
```

镜像架构应与集群节点一致。默认构建命令针对选定的 Docker 平台构建，不会发布多平台清单。将每个已推送摘要与选定源码修订版本一并记录。仅更改标签而没有新镜像内容不构成更新，重新构建可变标签也不会重启已有 Pod。

使用已发布制品安装时，应使用该发行版自己的 Chart、镜像和说明，不要替换成当前源码 Chart。应明确验证制品可用性和 Console 支持情况。

## 准备命名空间与数据库凭据

在预期上下文中创建发行版和执行命名空间：

```bash
kubectl create namespace "$RELEASE_NAMESPACE" --dry-run=client -o yaml | kubectl apply -f -
kubectl create namespace "$EXECUTION_NAMESPACE" --dry-run=client -o yaml | kubectl apply -f -
```

按[数据库配置](../database-setup.md#external-postgresql)创建安装生成状态，并在 `$RELEASE_NAMESPACE` 中创建 `orbitjob-database` Secret。应在 Helm 前完成：pre-install 钩子需要该 Secret。DSN 必须能从 Pod 解析；钩子和长期运行组件都必须能够获得所需 TLS 文件。妥善保密状态和引导凭据。

此初始映射将执行命名空间归属于引导创建的 Default 工作区，其 ID 为 `00000000000000000000000001`。增加租户映射时，需要已有工作区身份和命名空间，以及 Chart 对应的命名空间内授权。不要将命名空间映射到虚构的租户 ID。

## 配置安装

在仓库外或被忽略的配置目录中创建私有 `installation-values.yaml`。替换仓库、镜像标签、命名空间、公开 origin、Ingress 类和 TLS Secret：

```yaml
global:
  imageRegistry: registry.example.com/your-team
  imageTag: source-REPLACE_WITH_REVISION
  imagePullPolicy: IfNotPresent
operator:
  namespaceTenants: orbitjob-tasks=00000000000000000000000001
taskNamespace:
  create: false
  name: orbitjob-tasks
adminapi:
  console:
    publicOrigin: https://jobs.example.com
  ingress:
    enabled: true
    className: nginx
    host: jobs.example.com
    tls:
      enabled: true
      secretName: orbitjob-public-tls
```

`taskNamespace.create: false` 使用前面准备的执行命名空间；Helm 不应尝试将已有命名空间导入为 Chart 所有的对象。使用此 Ingress 前应准备 `orbitjob-public-tls` 和 DNS。若使用单独管理的 HTTPS 网关，保持 Ingress 禁用，并将 Console 和 API 流量路由到 ClusterIP Service。公开 origin 不包含路径；转发头不会决定其值。

根据实际集群审查执行网络和采集器存储。声明生产配置时，添加已经验证的设置：

```yaml
executionNetwork:
  profile: production
  enforcementVerified: true
  allowedEgress:
    - namespace: orbitjob-tasks
      cidrs: [192.0.2.10/32]
      ports:
        - {protocol: TCP, port: 443}
logs:
  collectorPersistence:
    mode: hostPath
    profile: production
    hostPathBase: /var/lib/orbitjob-collectors
    quotaVerified: true
    quotaBytes: 268435456
```

目标地址仅为示例，必须替换为应用所需目标。只有完成真实流量和文件系统检查后，才能设置验证标志。这些值是管理员声明，不会准备相应设施。如果默认 StorageClass 无法满足归档要求，应设置 `logs.storageClass`。

对于仅回环访问且不使用 Ingress 的开发环境，应设置 `adminapi.console.allowInsecureLocalhost: true`，并使用 localhost 端口转发。该例外不允许公开 origin 上的 HTTP 会话。

## 渲染与安装

将 `VALUES_FILE` 设置为已审查文件。渲染清单包含生成的 TLS 私钥，必须保密：

```bash
export VALUES_FILE=/absolute/path/to/installation-values.yaml
umask 077
helm lint ./charts/orbitjob -f "$VALUES_FILE"
helm template "$RELEASE_NAME" ./charts/orbitjob \
  --namespace "$RELEASE_NAMESPACE" -f "$VALUES_FILE" > /tmp/orbitjob-rendered.yaml
```

检查镜像引用、命名空间映射、Secret、PVC 要求、网络目标和公开路由。渲染错误或前提条件缺失时应停止。Helm 渲染不能证明数据库连通、网络策略已执行或仓库可访问。

完成审查后安装：

```bash
helm install "$RELEASE_NAME" ./charts/orbitjob \
  --namespace "$RELEASE_NAMESPACE" -f "$VALUES_FILE" \
  --wait --wait-for-jobs --timeout 10m
```

钩子顺序依次为所有者和角色初始化、有序模式迁移，以及工作负载安装后的应用引导。成功的钩子 Job 可能被删除。钩子失败时，此前迁移提交的数据库变更可能仍保留；卸载发行版不会撤销这些变更。

## 验证安装

检查所有长期运行组件：

```bash
for component in admin-api scheduler operator admission; do
  kubectl rollout status -n "$RELEASE_NAMESPACE" "deployment/orbitjob-$component" --timeout=5m
done
kubectl rollout status -n "$RELEASE_NAMESPACE" statefulset/orbitjob-log-archive --timeout=5m
kubectl rollout status -n "$RELEASE_NAMESPACE" daemonset/orbitjob-log-collector --timeout=5m
kubectl get pods,jobs,pvc -n "$RELEASE_NAMESPACE"
helm status "$RELEASE_NAME" -n "$RELEASE_NAMESPACE"
```

使用数据库维护账户检查 `SELECT version, name, checksum FROM schema_migrations ORDER BY version;`。验证选定的完整迁移链，不能只检查最高版本。为后续升级保留 Helm 挂载 SQL 输入通道。

在配置的公开 origin 打开 `https://jobs.example.com/console/`。使用运维人员签发的一次性能力凭据完成[首位管理员设置](../accounts.md#create-the-first-administrator)。引导 API key 供 API 客户端使用，不是 Console 密码。安装管理与工作区工作负载访问具有独立权限。

接纳正常工作前，创建一个受控工作负载，观察其活动修订版本，触发执行，并确认终态运行、Attempt 身份和保留日志。使用权限受到适当限制的身份，确认被禁止的操作确实遭到拒绝。仅有就绪状态不能证明数据库、授权、执行或日志采集行为。配置[监控](../operations/monitoring.md)，并验证[可恢复备份](../operations/backup-and-recovery.md)。

## 安装失败时

保留数据库、安装状态、Secret、归档和失败钩子证据。重试前先查看[故障排查](../operations/troubleshooting.md)。补齐前提条件，并使用相同凭据和制品集合重试。不要在引导 API key 已提交后重新生成密钥、重写迁移校验和，或移除证据终结器来使安装成功。

完成审查后删除私有渲染清单。删除失败发行版不构成删除数据库或归档数据的授权。对于可丢弃集群，仅在保留所需证据后才删除整个隔离环境；对于需要保留的安装，应在卸载前遵循备份、排空和恢复流程。

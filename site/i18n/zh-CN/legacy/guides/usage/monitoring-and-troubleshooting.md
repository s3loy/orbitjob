---
title: "监控与故障排查"
---

<a id="14-monitoring-and-troubleshooting"></a>

对于已配置监控 release 的安装，先选择目标 Kubernetes context，再从仓库根目录运行命令。按 API 配置说明设置[安装 API 来源](/orbitjob/docs/usage/authentication-and-request-conventions/#81-choose-an-installation-and-credential)，指标查询也使用相同地址：

```bash
make observability-status
curl -fsS "$ORBITJOB_API/metrics"
```

从实际部署的 [Helm values](https://github.com/s3loy/orbitjob/blob/main/charts/orbitjob/values.yaml) 读取 `adminapi.rateLimits`，不要假设独立进程设置同样适用于已安装 API。

运行 `make monitoring-up` 后，Grafana 会预置 Prometheus 数据源和 OrbitJob dashboard。

<a id="migration-failures"></a>

### 迁移失败

```bash
kubectl -n orbitjob-system get pods | grep -E 'migrate|owner-init'
kubectl -n orbitjob-system logs '<migrate-or-owner-init-pod>'
```

- `unsupported pre-release schema history; recreate the database`：迁移器拒绝不兼容的开发历史。先确认目标并保留所需数据，再使用新的可丢弃数据库或单独审阅的迁移方案。不得为绕过检查而删除已安装数据库或修改 `schema_migrations`。
- 迁移记录名为 `baseline` 时出现 `migration 0001 checksum mismatch`：发布的基线文件被修改。恢复发布时的文件，不要通过重建数据库绕过迁移历史变更。
- 后续迁移的校验和不匹配：同样应恢复原文件，或新增连续编号的迁移修正 schema。

<a id="bootstrap-key-retrieval-fails"></a>

### 无法取得 bootstrap key

```bash
kubectl -n orbitjob-system get pods        # bootstrap Job status
kubectl -n orbitjob-system logs '<bootstrap-pod>'
```

如果 bootstrap Job 失败，先查看日志；成功后 key 位于 `bootstrap-api-key` Secret。不要将 key 写入日志或提交到 Git。

<a id="run-stuck-pending"></a>

### 执行停留在 Pending

```bash
kubectl -n orbitjob-system logs deployment/orbitjob-operator --tail=100
kubectl -n orbitjob-system get scheduledjob,jobrun -A
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/instances/<run_id>"
```

检查 operator 调度循环是否创建 JobRun CR、operator 日志是否有协调错误、`operator.namespaceTenants` 是否覆盖该租户所属 namespace，以及 Kubernetes Job 是否受到配额或 admission 限制。

<a id="container-job-not-running"></a>

### 容器 Job 未运行

```bash
kubectl -n '<tenant-namespace>' get jobs,pods
kubectl -n '<tenant-namespace>' describe job '<name>'
kubectl auth can-i create jobs \
  --as system:serviceaccount:orbitjob-system:orbitjob-operator \
  -n '<tenant-namespace>'
```

使用 `operator.namespaceTenants` 中映射到该租户的 namespace 键。检查镜像与命令、operator RBAC、配额和 Pod Security 限制。生成的工作负载 Pod 不挂载 ServiceAccount token。Workflow 历史保留还要求对同一 namespace 中的 `workflowruns` 具有 `delete` 权限，chart 已向 operator 授予该权限。只有 controller 已确认停止、自有 Pod 终止证据通过验证且原始 Job 已移除时，取消结果才报告 `Canceled`。Operator 需要每个映射 namespace 内的 Pod `patch` 权限，以释放其证据 finalizer。数据库或 Operator 故障可能使已退出 Pod 及其 Job 保留至恢复完成。创建请求结果不明确时保持 `CancelUnknown`，直到协调可以确认实际工作负载状态。

寻址执行时使用触发 API 返回的 namespace 与 name。新执行资源名保留完整 occurrence digest；历史短名资源通过完整身份校验后仍可访问，已有资源不重命名。参阅[迁移兼容性](/orbitjob/docs/database/#migration-lineage-and-compatibility)。

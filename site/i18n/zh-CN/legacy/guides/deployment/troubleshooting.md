---
title: "故障排查"
---

<a id="the-release-is-failed-and-a-hook-pod-is-in-createcontainerconfigerror"></a>
### Release 为 `failed`，hook Pod 为 `CreateContainerConfigError`

检查 Pod 事件中报告缺失的 Secret 或键。本地安装通过 `make kind-db` 创建 `orbitjob-database`；重试 Helm 前，确认它位于 release 命名空间内。此错误也可能指出其他缺失的配置引用。

<a id="the-operator-is-in-crashloopbackoff"></a>
### Operator 处于 `CrashLoopBackOff`

```bash
kubectl logs -n orbitjob-system deployment/orbitjob-operator --tail=5
```

出现 `OPERATOR_NAMESPACE_TENANTS is required` 表示传入 Pod 的映射为空。`helm install` 会拒绝缺少该值的渲染，正常通过它安装不应出现此情况；检查手动修改的 Deployment 或旧 release。

<a id="pods-cannot-pull-their-image"></a>
### Pod 无法拉取镜像

`global.imagePullPolicy=Never` 要求节点事先已有每个镜像。对 `global.imageTag` 设置的 tag 运行 `make kind-load TAG=<tag>`。

<a id="the-api-returns-401"></a>
### API 返回 401

密钥来自 `bootstrap-api-key` Secret，不是 `orbitjob-database`。`source <(make kind-env)` 读取 bootstrap 密钥。

<a id="resetting"></a>
### 重置

以下操作具有破坏性。kind 集群将 PostgreSQL 和日志归档卷存放在节点存储中。删除命名空间、PVC 或集群前，先保留一致的数据库备份、安装凭据与配置，以及日志归档，再在独立环境中验证恢复。验证成功前保留原数据。以下命令仅适用于一次性环境，或已完成上述恢复流程的环境：

```bash
kubectl delete namespace orbitjob-system    # control plane
kubectl delete namespace orbitjob           # PostgreSQL and its data
```

不要为移除租户工作负载而删除共用的 `default` 命名空间。完整重置时，使用 `kind delete cluster --name orbitjob-dev` 删除隔离开发集群，再从第一步开始。

<a id="installed-m3-monitoring-verification"></a>
### 已安装 M3 监控验证

`web/installed-tests/monitoring.spec.ts` 通过已安装 Console 创建专用 Check、SLI 和滚动 SLO fixture。它观察真实成功或失败的 Check Job、已记录结果、评估预算与历史，以及消耗率告警。资源组用例检查本组证据、真实外组父资源详情 404 和过滤后的列表，并检查版本冲突与暂停、恢复。独立缺失授权的拒绝行为由后端和受控浏览器检查覆盖。已执行套件结果记录在[验证记录](/orbitjob/docs/verification/)。此流程不通过路由模拟或数据库种子替换执行链路。应预留最多十分钟，真实 SLO Scheduler 每五分钟评估一次。

使用[已安装 Console 验证](/orbitjob/docs/deployment/installed-console-verification/)准备的私有状态。其个人账户和匹配 API 配置包含所需监控授权；资源组专属证据使用直接 Bearer 探针。租户必须在 Operator 配置中映射到 fixture 命名空间。准备阶段向 kind 节点加载 `internal/core/app/projection/check.go` 指定的固定摘要探针镜像。不得用自行编造的本地别名替换摘要。

完成准备后，可运行此 spec 进行诊断：

```bash
python3 scripts/console-installed.py run \
  --kubeconfig "$KUBECONFIG" \
  --state-dir "$PWD/.runtime/console-kind/fixtures" --port 0 \
  --spec monitoring.spec.ts
```

套件将成功和失败定义保留为暂停状态，并保留 SLI/SLO 与结果证据以供检查。完成后只删除记录的 fixture ID，不得删除无关租户资源。浏览器录制保持禁用。

<a id="installed-m4-verification"></a>
### 已安装 M4 验证

[已安装验证程序](/orbitjob/docs/deployment/installed-console-verification/)在同一专用 kind 安装中准备受保护的 Function 定义并应用 Workflow fixture。[Function 辅助程序](https://github.com/s3loy/orbitjob/blob/main/deploy/kind/console-fixture/README.md)等待真实 Operator 的活动修订版本，并在私有清单中记录其拥有的精确 ID；它不插入修订版本或 Function 结果。Workflow 覆盖依赖、失败步骤重试、条件和跳过任务、挂起定义，以及长时间运行的取消分支。

使用 `credentials.json` 中生成的账户与 API 配置。浏览器权限配置使用各自个人账户登录；直接 API 探针保留专用的只读、资源组、外租户和显式 Deny 密钥。不要构建旧式仅含密钥的浏览器凭据文件。运行两个诊断 spec：

```bash
python3 scripts/console-installed.py run \
  --kubeconfig "$KUBECONFIG" \
  --state-dir "$PWD/.runtime/console-kind/fixtures" --port 0 \
  --spec m4.spec.ts --spec m4-retrospective.spec.ts
```

验证真实五分钟停止确认边界时，还需使用同一私有状态，以 `--spec m4-unknown.spec.ts` 运行验证程序。该用例保留唯一命名的测试 Job finalizer，等待 Workflow 进入 `CancelUnknown`，验证两分钟浏览器观察暂停和手动刷新，然后在仅移除此 finalizer 前检查已持久化、经控制器确认的停止证明，以及不存在证据缺口。原 Job 消失后，步骤与 Workflow 必须恢复到 `Canceled`，且不产生新 attempt。明确的证据缺口仍保持 unknown；仅移除资源不能确认取消。

套件验证真实 Function 成功、失败、重试、取消和幂等重放，Workflow 依赖、条件、重试与取消，以及实际授权、资源组、租户和步骤读取边界。回顾测试检查无效 UUID、稳定的从新到旧分页，以及独立定义 Get 被拒绝时的 List-only 历史。脱敏证据写入私有验证结果目录。检查后，使用验证程序的 `cleanup` 命令停用所拥有的定义，并撤销 fixture 账户和密钥。专用集群删除前，保留历史数据库证据。Trace、视频和自动截图保持禁用，以防凭据和会话数据进入录制内容。

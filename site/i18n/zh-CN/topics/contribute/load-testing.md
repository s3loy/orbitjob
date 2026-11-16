# 负载测试

针对隔离 Kubernetes 安装运行受控 ScheduledJob 语料。负载工具生成声明、准备租户和命名空间、驱动手动触发、注入已配置故障，并验证独立执行证据。应读取该次运行的精确结论；工作流完成或负载档位名为 `standard`，都不能证明通过适用性验证。

负载测试是运维人员运行的证据收集，本身不阻止合并。测试会修改安装状态，并可能停止控制平面或数据库组件。使用专用安装，每次只运行一个负载档位，并在清理前保留制品。

## 负载档位与配置

| `test/load/config/` 下的配置 | `--profile` | 虚拟调度时间 | 请求适用性验证 | 时间范围内的故障 |
|---|---|---|---|---|
| `smoke.yaml` | `smoke` | 30 分钟 | 否 | 无 |
| `smoke-faults.yaml` | `smoke` | 30 分钟 | 否 | 25m 时 Operator，27m 时 PostgreSQL |
| `standard.yaml` | `standard` | 4 小时 | 是，至少 10,000 次符合条件的运行 | 180m 时 Scheduler，190m 时 Operator，210m 时 Admin，220m 时 PostgreSQL |
| `standard-dynamic.yaml` | `standard` | 节奏延长前为 4 小时 | 是，至少 10,000 次符合条件的运行 | 标准计划 |
| `long.yaml` | `long` | 8 小时 | 否 | 370m 时 Scheduler，390m 时 Operator，400m 时 Admin，410m 时 PostgreSQL |

标志必须与配置内部的 `profile` 一致；文件名不是负载档位标识符。standard 要求干净的源码和配置树，以及至少 10 CPU / 8 GiB Docker 容量。smoke 要求 6 CPU / 8 GiB；long 使用 10 CPU / 8 GiB。这些检查读取配置的 Docker 容量，不测量整个运行期间实际可用资源。

所有提供的配置都在四个生成租户中声明 1,200 个定义：280 个数据处理、220 个数据库、240 个 HTTP/Webhook、140 个运维、220 个失败和 100 个外部探测定义。产品触发类型包括 840 个 manual 和 360 个 cron；来源标签在 manual 范围内包含 120 个 Webhook 案例。安装支持 Function 或 Workflow，并不意味着 ScheduledJob 工作负载验证了它们的执行。

生成器将真实租户 ULID 持久保存在 `tenant-ids.json`；prepare、run 和 verify 必须复用该映射。新运行 ID 获得独立租户和声明身份摘要。`default` 这类 slug 不是租户 ID。显式租户 ID 配置仍受工具验证和所有权检查约束。

静态 standard 使用十分钟 cron 间隔。smoke 和 long 使用每小时 cron。动态 standard 根据已检查资源计算速率和活动上限，再依据 Prometheus 调整触发节奏。当前 `run` 标志 `--prometheus-url` 默认为 `http://localhost:9090`；使用其他端点时应显式设置该标志。Prometheus 失败会冻结节奏并发出警告，不会使运行失败。不要仅根据配置文件推断存在实时反馈。

## 开始前

按[本地开发](local-deployment.md)在专用集群安装当前源码。负载工具假定使用仓库本地 PostgreSQL Deployment `orbitjob-postgres`、数据库和用户 `orbitjob` / `postgres`，并能在该 Pod 内访问 `psql`。其准备、重置和故障路径不是通用外部数据库测试工具。

获取集群管理权限和安装引导 API key。工具创建租户和命名空间、扩展 Operator 映射、应用 CR、创建租户 API key，并可能重启组件或使其故障。它直接更新 Operator 映射，不会建立具有对应 Admin、准入、归档映射和策略的完整生产租户安装。下文额外测试夹具授权提供手动触发 CR 接口。不要将此流程用于生产租户接入或网络隔离适用性验证。

安装 Docker、kind、kubectl、Helm、Go、curl、Git、OpenSSL 和 Bash。整个运行期间保持主机唤醒。macOS 上，引擎使用绑定进程的 `caffeinate -i -s` 断言，standard 预检要求交流电源。其他平台需要自行安排禁止挂起。挂起会拉长调度并使延迟测量失效。动态节奏和故障恢复可能使实际耗时超过配置的虚拟调度时间。

从选定仓库根目录运行所有命令。为完整流程设置一个配置和运行身份：

```bash
export CONFIG=test/load/config/smoke.yaml
export PROFILE=smoke
export RUN_ID="smoke-$(date -u +%Y%m%dT%H%M%SZ)"
export ORBITJOB_NAMESPACE=orbitjob-system
export ORBITJOB_DB_NAMESPACE=orbitjob
export ORBITJOB_API_URL=http://localhost:18080
kubectl config current-context
```

适用性验证应配套使用 `standard` / `test/load/config/standard.yaml`。执行开始后绝不能复用运行 ID。运行制品包含明文 API key；`test/load/runs/` 被 Git 忽略，生成的含敏感数据文件使用私有权限。不要将制品放入公开日志。

## 1. 验证与生成

```bash
go run ./scripts/loadtest preflight --config "$CONFIG" \
  --images test/load/config/images.lock.yaml --profile "$PROFILE" --check-only
go run ./scripts/loadtest generate --config "$CONFIG" \
  --images test/load/config/images.lock.yaml --profile "$PROFILE" --run-id "$RUN_ID"
```

预检检查配置、含六个条目且按摘要固定镜像版本的锁文件、必需工具、Docker 容量、档位规则、要求时的源码干净状态，以及静态最小实例可行性。当前实现即使未提供 `--check-only` 也不会拉取镜像或修改集群；应独立确保节点能够获取锁定镜像。`--images-only` 仅验证镜像锁，不能证明环境就绪。

生成会写入 `generated/definitions.json`、类别摘要、租户 ID 和独立故障恢复金丝雀。准备前应审查生成的工作负载。金丝雀不计入提供的工作负载、覆盖率和最小实例数。配置、镜像或语料错误会在此阶段、集群变更前停止。

## 2. 准备共享测试夹具与访问

对于默认命名空间，Make 目标部署测试夹具基础设施：

```bash
make loadtest-prepare
kubectl rollout status -n orbitjob-load deployment/load-fixture --timeout=5m
kubectl rollout status -n orbitjob-load deployment/load-postgres --timeout=5m
```

该目标创建共享负载和任务命名空间、运维 RBAC、HTTP/TLS 测试夹具和独立测试夹具 PostgreSQL，不创建该次运行的租户。对于自定义控制平面命名空间，应使用 `scripts/render-local-manifest.sh load-operations "$ORBITJOB_NAMESPACE"` 生成运维 RBAC，不要原样应用默认绑定。Go prepare 命令也会渲染该覆盖配置并应用基础设施。

两次运行之间如果测试夹具代码或生成证书发生变化，应重启 `deployment/load-fixture` 并等待，再开始下一次运行。应用 ConfigMap 或 Secret 不会重启已有测试夹具 Pod。绝不能在运行中重启测试夹具。计数器、去重集合和仅追加的观测日志（journal）均保存在进程内存中。单副本测试夹具的启动身份和观测日志序列是连续性证据。

在不同终端启动以下转发：

```bash
kubectl port-forward -n "$ORBITJOB_NAMESPACE" svc/orbitjob-admin-api 18080:8080
```

```bash
kubectl port-forward -n orbitjob-load svc/load-fixture 18081:8080
```

```bash
kubectl port-forward -n "$ORBITJOB_DB_NAMESPACE" deployment/orbitjob-postgres 15432:5432
```

确认每个转发成功绑定端口；如果已有其他进程监听，应停止。检查 `http://localhost:18080/healthz` 和 `http://localhost:18081/ledger` 返回 HTTP 200。这些检查是前提条件，不是执行证据。

加载凭据，不将值放入 argv：

```bash
export ORBITJOB_API_KEY="$(kubectl get secret bootstrap-api-key \
  -n "$ORBITJOB_NAMESPACE" -o jsonpath='{.data.api-key}' | base64 -d)"
ORBITJOB_DSN="$(kubectl get secret orbitjob-database \
  -n "$ORBITJOB_NAMESPACE" -o jsonpath='{.data.admin-dsn}' | base64 -d)"
export ORBITJOB_DSN="${ORBITJOB_DSN/orbitjob-postgres.${ORBITJOB_DB_NAMESPACE}.svc:5432/127.0.0.1:15432}"
```

上述端点替换方式适用于本地安装的 DSN。如果经过自定义，应私下检查实际端点。admin 登录账户使用事务局部租户上下文提供修订版本范围内的证据读取；引导所有者不能作为可互换的读取身份。

对于动态 standard，安装并检查[监控](operations/monitoring.md)，然后保持其 Prometheus 在 9090 上的转发。静态档位不使用 Prometheus 反馈。

## 3. 准备运行

```bash
go run ./scripts/loadtest prepare --config "$CONFIG" --profile "$PROFILE" \
  --run-id "$RUN_ID" --api-url "$ORBITJOB_API_URL"
```

准备执行以下变更：

1. 在安装数据库中，使用持久化 ID 初始化自有租户行。
2. 为每个租户创建一个带标签的 `orbitjob-tasks-<tenant-id>` 命名空间，并配置适用于生成语料的 LimitRange 和配额。
3. 扩展 `OPERATOR_NAMESPACE_TENANTS`，保留已有映射，并在该映射表改变时重启 Operator。
4. 应用 ScheduledJob 声明，等待每个实际活动修订版本，并保存案例到修订版本和命名空间的记录。
5. 使用 `TenantAdminAccess` 预设创建或复用租户 API key，并解析独立恢复金丝雀身份。

尚未开始执行的准备可以重试。未改变的声明保留修订版本，有效的已记录 API key 继续复用。执行一旦开始，准备将拒绝该运行 ID。如果 reset 删除了尚未开始运行的集群状态，准备必须在使用前刷新修订版本和 API key 记录。

生成的 Operator Role 包含 Pod `patch` 权限，以便 Operator 在持久化终止证据后释放受保护的终结器。缺少该授权时，终态投影和清理可能持续阻塞。

### 授予测试夹具触发发布权限

这些命名空间在 Chart 安装后创建。仅向 Admin API 授予这些工具自有命名空间中的测试夹具 CR 发布接口权限：

```bash
for namespace in $(kubectl get namespace \
    -l orbitjob.io/managed-by=orbitjob-loadtest \
    -o jsonpath='{.items[*].metadata.name}'); do
  kubectl apply -n "$namespace" -f - <<MANIFEST
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: orbitjob-admin-api
rules:
  - apiGroups: ["workloads.orbitjob.io"]
    resources: ["jobruns", "workflowruns"]
    verbs: ["create", "get", "patch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: orbitjob-admin-api
roleRef: {apiGroup: rbac.authorization.k8s.io, kind: Role, name: orbitjob-admin-api}
subjects:
  - kind: ServiceAccount
    name: orbitjob-admin-api
    namespace: ${ORBITJOB_NAMESPACE}
MANIFEST
done
```

缺少这些授权时，API 接收触发后，手动发布仍可能失败。此循环是受所有权标签限制的测试夹具基础设施；正常安装命名空间变更应放入已审查 Helm values。

## 4. 执行

```bash
go run ./scripts/loadtest run --config "$CONFIG" --profile "$PROFILE" \
  --run-id "$RUN_ID" --api-url "$ORBITJOB_API_URL" \
  --fixture-url http://localhost:18081
```

故障恢复使用导出的 `ORBITJOB_DSN`。动态调优还使用 `--prometheus-url`；`--tune-deployments` 显式修改 Admin API 的触发速率环境并重启组件。该标志要求动态配置。选择修改 Deployment 前，应先审查 `--dry-run-tuning` 输出。

引擎持久保存执行配置哈希、源码提交和脏状态、计划故障、最终计数器，以及每个已接受手动触发的一份身份回执。HTTP 200/201 必须包含完整运行身份；HTTP 409 属于拒绝。幂等重放返回相同的已接受身份。

预期取消的场景由生成器通过 API 取消，不由工作负载正文自行取消。测试夹具工作负载运行时间足够长，以便请求到达。取消被接受后，仍需要 Operator 提供物理停止和终态台账证据。

故障使用与节奏相同的虚拟阶段时钟。注入器在写入最终统计前执行有界恢复。PostgreSQL 故障在中断前后观测进行中的自有金丝雀，并在之后探测新工作。Scheduler 恢复需要真实 SLI/SLO 求值证据；Admin 和 Operator 恢复需要独立金丝雀精确的来源、修订版本、台账、Kubernetes 对象和观测日志中的效果记录。仅 Deployment 就绪不能证明业务恢复。Lease 丢失和任意网络分区不属于这四种故障类型。

中断运行会保留截断证据。清理或开始另一运行前，应检查实际组件状态和故障恢复记录。不要复用运行 ID 来覆盖被中断执行。

## 5. 收敛检查与验证

调度结束时，最后接受的 Job 可能仍在执行。对当前运行的已投影台账采样：

```bash
go run ./scripts/loadtest settle --run-id "$RUN_ID"
```

`settle` 执行一次有界采样并打印非终态计数，不是等待命令。失败或格式错误的采样属于未知，绝不能视为零。租户没有观测到匹配行属于证据缺口。采样计数为零不能证明所有 CR 已投影，也不能证明未来 cron 调度项不会启动；应及时收集验证，同时保留该次运行来源。

使用原始配置运行验证器：

```bash
go run ./scripts/loadtest verify --run-id "$RUN_ID" --config "$CONFIG" \
  --api-url "$ORBITJOB_API_URL" --fixture-url http://localhost:18081
```

验证器在收集证据前核对生成定义、修订版本、租户 API key、命名空间、执行统计和已接受回执。它读取租户范围数据库行、JobRun 身份、API 隔离探针、测试夹具观测日志和特定故障来源。每个已接受手动回执必须恰好对应一个手动台账行；cron 活动不能补足该缺口。验证器检查重复成功 Attempt、重复幂等副作用、终态回退、预期终态和完整 JobRun 身份。

测试夹具观测日志独立于去重集合。启动身份和完整序列区间必须保持完整；重启会丢失证据，不能证明没有重复副作用。故障检查点将注入、恢复、就绪、提交和观测绑定到同一运行、故障和金丝雀。独立数据库时钟采样界定计时不确定性。来源不受支持或缺失时，不能产生 PASS。

## 6. 读取与保留结果

```bash
go run ./scripts/loadtest report --run-id "$RUN_ID"
```

命令在 `result.json`、`run-record.json` 和 `run-stats.json` 旁写入 `report.md` 和 `checksums.txt`。它拒绝缺失、损坏或身份不一致的制品。非适用性验证报告以 `NON-STANDARD RUN - NOT A RELEASE QUALIFICATION` 开头。

| 结论 | 含义 |
|---|---|
| `PASS` | 此次已记录运行及其范围所需检查通过；smoke 和 long 仍不属于适用性验证运行 |
| `FAIL` | 已检查正确性或恢复条件失败，或适用性验证运行未达到符合条件运行的最低数量 |
| `INCONCLUSIVE` | 证据缺失或不完整、工作负载被拒绝或跳过、截断、进行中的运行、覆盖缺口或不受支持的恢复观测，阻止得出请求的结论 |

适用性验证还要求每个已创建定义均有观测到的运行，并至少有 10,000 次符合条件的终态运行及其所需 CR 证据。smoke 可以有部分语料定义未触发而不违反覆盖规则；但 Attempt、状态转换、租户探针或观测日志证据为空时，仍无法形成完整结论。

由于 cron 声明独立运行，运行总数可能超过提供的手动触发数。预期失败场景应以 `Failed` 结束；仅该计数不代表产品失败。应将实际终态结果与每个场景声明的预期比较。不要使用旧报告或较新检出来重新标记历史执行源码或不完整证据。

语料包括 JSON 哈希、SQL 成功和无效 SQL、DNS、令牌缺失、HTTP 分页和下载、Webhook、重试、超时、TLS 握手，以及显式失败和取消。渲染的工作负载 Pod 禁用投影 ServiceAccount 令牌。HTTPS 测试夹具使用生成的自签名证书，客户端使用 `--insecure`；这些案例测试握手，不验证证书链。报告结果时应保留这些限制。

## 便捷命令

`scripts/loadtest-ci.sh smoke` 或 `standard` 自动执行本地测试夹具、转发、重置、生成、准备、触发授权、运行、收敛检查、验证和报告流程。开始前，它会重置所有已明确确认所有权的既往负载租户，因此仅可在专用安装中、保留所需证据后使用。它仅接受这两个负载档位，不接受任意配置变体。其 settle 循环以 15 秒间隔最多采样 60 次，等待到期后仍会运行独立验证器。它不会将未知的收敛检查状态转换为成功。

`make loadtest-smoke` 和 `make loadtest-long` 仅运行预检。`make loadtest-full` 运行 standard 预检和生成，随后要求手动审查；不会执行四小时适用性验证。

## 重置与清理

保留报告和所需私有制品后，使用以下命令删除自有在线负载状态：

```bash
go run ./scripts/loadtest reset --confirm
```

Reset 发现带标签的负载命名空间，删除并等待其中工作负载停止，然后以事务方式清除自有数据库状态。租户所有权同时要求 `load-` slug 和元数据标记 `orbitjob.io/managed-by=orbitjob-loadtest`；仅名称不构成删除授权。未标记历史测试夹具需要手动审查。任何命名空间删除或 SQL 失败都会停止重置；不要通过强制移除证据终结器来完成重置。

Reset 不删除共享测试夹具、监控或本地制品。它保留已删除命名空间的 Operator 映射条目；这些条目不再解析该处的在线工作。新运行添加自身持久化租户。应将这种测试夹具行为与持续维护的生产 values 分开。

仅在不再需要时删除某次运行的本地制品：

```bash
go run ./scripts/loadtest clean --run-id "$RUN_ID" --confirm
```

`clean` 仅删除该运行目录。仅停止为该次运行启动的转发。审查所有权并保留所需执行证据后，单独删除共享测试夹具清单或隔离集群。

---
title: "运行负载测试"
---

```bash
export RUN_ID="smoke-$(date -u +%Y%m%dT%H%M%SZ)"
RELEASE_NAMESPACE="${ORBITJOB_NAMESPACE:-orbitjob-system}"
DB_NAMESPACE="${ORBITJOB_DB_NAMESPACE:-orbitjob}"
export ORBITJOB_API_KEY=$(kubectl get secret bootstrap-api-key \
  -n "$RELEASE_NAMESPACE" -o jsonpath='{.data.api-key}' | base64 -d)
CONFIG=test/load/config/smoke.yaml
PROFILE=smoke
```

`bootstrap-api-key` Secret 保存明文密钥。`orbitjob-database` 保存数据库凭据，是另一个 Secret。

<a id="1-preflight"></a>
### 1. 预检

```bash
go run ./scripts/loadtest preflight --config "$CONFIG" \
  --images test/load/config/images.lock.yaml --profile "$PROFILE"
```

```
preflight: profile=smoke Docker capacity >= 6 CPU / 8 GiB
preflight: configuration and image lock valid
```

预检验证配置与镜像锁结构、所需工具、Docker 容量，以及各配置要求的工作区、供电和静态卷约束。当前实现不会拉取镜像或检查镜像可用性，即使不带 `--check-only` 也不会。预检不能证明集群就绪或任何工作负载已执行。`--images-only` 仅检查锁文件，不使用 Docker。

Makefile 快捷目标 `loadtest-smoke` 和 `loadtest-long` 只调用预检。`loadtest-full` 调用预检和生成，然后输出人工审阅提示；`loadtest-run` 输出操作说明。这些目标均不执行完整配置。

<a id="2-generate"></a>
### 2. 生成

```bash
go run ./scripts/loadtest generate --config "$CONFIG" \
  --images test/load/config/images.lock.yaml --profile "$PROFILE" \
  --run-id "$RUN_ID"
```

写入 `test/load/runs/$RUN_ID/generated/`；Smoke 配置生成 1200 个 ScheduledJob 声明及其类别分布摘要。

重复生成复用已记录租户映射，并要求完整定义集合完全一致，包括镜像和工作负载参数。镜像或场景输入改变生成定义时，即使尚未执行，也必须使用新 Run ID，以免准备阶段的修订版本 ID 在未提示的情况下仍指向旧工作负载。
首次执行独占创建 `run-started.json`；之后生成、prepare 和第二次执行均拒绝覆盖证据。再次尝试必须使用新 Run ID。触发幂等键也包含 Run ID，使同一运行内的重试收敛，且不复用其他运行的工作。租户记录还绑定生成阶段完整解析后的输入配置。即使 profile、seed 和租户数未变，prepare、run 与 verify 也会拒绝定义集合、cron 间隔、速率或其他配置字段变化。更改工作负载需要新 Run ID 并重新生成；显式租户 ID 不能被旧映射静默替换。

声明包含完整 ScheduledJob spec。Cron 定义使用配置中的间隔；仅手动定义使用 `0 0 30 2 *`，即 2 月 30 日。它能被标准 cron 表达式解析，但不匹配任何时刻，既满足 CRD 非空 schedule 规则，也不会被 Scheduler 触发。每个声明将历史限制提高到定义集合大小，防止保留器在运行期间清除证据。Operator 只以 image、command 和 args 渲染 Kubernetes Job，不提供 env 块或 service account，因此每个工作负载主体将 case id、fixture 地址和数据库 DSN 保存为字面参数。

<a id="3-prepare"></a>
### 3. 准备

```bash
go run ./scripts/loadtest prepare --config "$CONFIG" --profile "$PROFILE" \
  --run-id "$RUN_ID" --api-url http://localhost:18080
```

```
prepare: declared 1200 definitions across 4 tenants
```

Prepare 配置运行所依赖的全部资源。`ORBITJOB_NAMESPACE` 选择控制平面命名空间，`ORBITJOB_DB_NAMESPACE` 选择安装的 PostgreSQL 命名空间。Prepare 和 CI 运行器为所选控制平面渲染运维 Role 与 RoleBinding；任务 ServiceAccount 仍位于 `orbitjob-tasks`。

1. 将本次运行持久保存的生成 ULID 对应租户行写入安装数据库（通过 `kubectl exec psql` 执行 `INSERT ... ON CONFLICT DO NOTHING`；API 会自行生成 ID，而命名空间表需要这些精确 ID）。
2. 为每个租户创建 `orbitjob-tasks-<tenant-ULID>` 命名空间，带 `orbitjob.io/managed-by=orbitjob-loadtest` 标签，并应用相同的 LimitRange 和本次运行专用 ResourceQuota。其内容来自 `scripts/loadtest/prepare.go` 的 `tenantNamespaceManifest`；渲染的 Kubernetes Job 在这里执行。
3. 向 Operator 命名空间表 `OPERATOR_NAMESPACE_TENANTS` 中为每个负载租户增加 `namespace=tenant` 条目；仅在表发生变化时重启 Operator。现有映射保留。
4. 声明完整集合：每个定义对应一个 ScheduledJob 自定义资源，按租户合并为多文档 manifest，保存在运行目录的 `cr-manifests/` 中作为证据。
5. 等待每个声明具有活动修订版本，从各 CR 的 `status.activeRevision` 读取，并将 case 与修订版本映射记录到 `created-definitions.json`。触发路由使用修订版本地址，运行引擎据此映射触发。
6. 为每个租户创建绑定 `TenantAdminAccess` 策略的 API 密钥，写入 `tenant-keys.json`（明文，因此 `test/load/runs/` 被 gitignore 排除），并写入验证器所需的 `tenant-namespaces.json`。

每次调用均通过 API 密钥确定租户，没有租户请求头。负载流程将定义声明为自定义资源，并等待 Operator 物化修订版本。独立的类型化定义 API 不替代该 fixture 生成路径。

执行前可重试 prepare。未变的 ScheduledJob 保留修订版本，仍有效的已记录租户密钥会复用。执行开始后，prepare 拒绝该 Run ID。执行 `reset` 后，保留已启动运行的历史产物，并为新 Run ID 重新生成和准备。尚未启动的运行可复用租户映射，但 prepare 必须刷新 CR 修订版本和密钥记录，因为 reset 已移除其数据库与命名空间状态。

<a id="35-grant-the-admin-api-its-trigger-surface"></a>
### 3.5 授予 Admin API 触发权限

`prepare` 在 chart 安装后创建租户命名空间；chart 按 `operator.namespaceTenants` 中的每个调度命名空间分别授予 Admin API CR 权限。没有 Role 的命名空间会拒绝每次手动触发的 JobRun 发布，运行返回 500，且不会执行。向工具创建的命名空间应用 chart 的 Role 和绑定。这些命名空间带有 `orbitjob.io/managed-by=orbitjob-loadtest` 标签，`reset` 也按此标签清理：

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
    resources: ["jobruns"]
    verbs: ["create", "get", "patch"]
  - apiGroups: ["workloads.orbitjob.io"]
    resources: ["workflowruns"]
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
    namespace: ${RELEASE_NAMESPACE}
MANIFEST
done
```

`scripts/loadtest-ci.sh` 在 prepare 与 run 之间执行此步骤。扩展真实安装仍应通过 Helm 升级 `operator.namespaceTenants`；该循环只配置工具自行创建并再次删除的 fixture 命名空间。

<a id="4-run"></a>
### 4. 运行

```bash
go run ./scripts/loadtest run --config "$CONFIG" --profile "$PROFILE" \
  --run-id "$RUN_ID" --api-url http://localhost:18080
```

故障恢复还需要验证阶段使用的受保护 `ORBITJOB_DSN` 环境变量。不得在命令参数中放入凭据。生成阶段写入独立的 `generated/recovery.json` canary；准备阶段将解析后身份写入 `recovery-prepared.json`。这些资源不计入施加的工作负载、覆盖率或最小实例数。本地 fixture 端点与默认值不同时，通过 `--fixture-url` 覆盖。

Smoke 配置采用三十分钟虚拟计划。限速、容量等待和故障恢复可能延长墙钟耗时。进度输出如下：

```
loadtest progress: triggered=290 accepted=289 rejected=0 skipped=0 pace=1.00
```

最终计数示例（数值取决于本次运行）：

```
run: triggered=500 accepted=500 rejected=0 skipped=0
```

`pace` 是反馈控制器相对于配置目标速率的倍率；`1.00` 表示按计划运行而未限速。

执行开始时，工具记录解析后配置的 SHA-256，以及检出的 commit 与 dirty 标记。`run-stats.json` 同时保存最终计数和每个已接受手动触发的回执（租户、修订版本和 occurrence key）。HTTP 200/201 要求完整的手动 Run 身份；HTTP 409 表示拒绝。幂等重放为 API 返回 HTTP 200 且身份相同。

预期 `canceled` 的场景由生成器自行取消：触发后，引擎根据响应 occurrence key 解析 Run 账本 ID，再调用 `POST /api/v1/instances/{run_id}/cancel`。API 修改 Run 的 JobRun 上的 `spec.cancelRequested`；Operator 删除 Kubernetes Job；Run 以 `Canceled` 结束。工作负载主体会休眠足够长时间，使取消请求在 Job 仍运行时到达。

<a id="5-verify"></a>
### 5. 验证

验证器在本机运行，因此需要本机可达 DSN。Secret 保存集群内主机名，必须改写为端口转发地址：

```bash
kubectl port-forward -n "$DB_NAMESPACE" deploy/orbitjob-postgres 15432:5432 &

ORBITJOB_DSN=$(kubectl get secret orbitjob-database -n "$RELEASE_NAMESPACE" \
  -o jsonpath='{.data.admin-dsn}' | base64 -d)
export ORBITJOB_DSN="${ORBITJOB_DSN/orbitjob-postgres.${DB_NAMESPACE}.svc:5432/127.0.0.1:15432}"

go run ./scripts/loadtest verify --run-id "$RUN_ID" --config "$CONFIG" \
  --api-url http://localhost:18080 \
  --fixture-url http://localhost:18081
```

**传入原始 `--config`。** 默认值为 `standard.yaml`。验证拒绝不同 Run ID 或配置指纹，包括同一 profile 中速率、时长、seed、故障计划或最小数量变化。源码身份来自执行时统计，不来自随后运行 verify 的 checkout。收集证据前，生成定义集合、已创建修订版本、租户密钥和命名空间映射必须一致；缺少主体或重复的 revision/case 记录不能静默缩小测试范围。

证据来自数据库、Kubernetes、fixture journal 和 API。账本中的预期 Run 是 `job_run_control_plane` 行，attempt 轨迹在 `job_run_attempts_control_plane` 中，均限定为本次运行创建的修订版本。
集群中，每个终态 Run 的 JobRun 自定义资源必须存在于租户命名空间，命名为 `<bounded-definition-prefix>-<complete occurrence digest>`，并具有预期 source UID、revision、occurrence 和 trigger。匹配的历史名称只在完整身份验证后接受。API 使用跨租户探针，让每个租户密钥尝试读取其他租户定义。

账本受行级安全保护，因此每个租户查询都在事务中先将 `app.tenant_id` 设置为该租户 ULID；设置只在事务内有效，读取结束后回滚。查询也按本次运行修订版本 ID 过滤，因此早期运行残留不会参与检查。

验证器运行以下检查及已配置的故障检查：

| 检查 | 检测内容 |
|---|---|
| `duplicate-succeeded-attempt` | 同一 Run 有两个成功 attempt，即同一 occurrence 执行两次 |
| `tenant-leak` | 某租户通过 API 读取其他租户定义 |
| `duplicate-idempotent-side-effect` | 同一幂等键实际执行工作两次 |
| `terminal-regression` | Run 离开终态（`job_run.status_changed` 审计事件） |
| `expected-terminal-state` | Run 以不同于场景声明的阶段结束 |
| `jobrun-cr-present` | 终态账本 Run 的 JobRun 缺失，或不具备预期命名空间和完整身份 |
| `manual-trigger-accounting` | 施加事件、接受回执和观察到的手动账本行必须按租户、修订版本与 occurrence 对账 |

被拒绝或跳过的触发使施加的工作负载不完整，结果为 `INCONCLUSIVE`；不能根据少量剩余健康行推定失败预算。每份已接受手动回执必须精确对应一行账本。Cron 行不能替代缺失的手动工作。历史统计缺少执行配置、源码记录或接受回执时，不能支持新的 `PASS`；保留旧报告作为带日期证据，并使用新 Run ID。

Attempt、阶段转换观察、租户探针或 fixture 副作用 journal 为空都属于证据缺口，smoke 运行也不例外。它们产生 `INCONCLUSIVE`，对应检查不输出 `PASS`。转换扫描先读取本次运行修订版本范围的审计事实，再检查终态回退。Fixture 独立于去重集合暴露只追加副作用 journal，因此去重保护失效时仍可产生重复观察。只有带本次运行生成 case ID 的条目计数；fixture 重启会丢失内存证据，不能证明正确性。

动态节奏下，故障使用引擎相同的虚拟阶段时钟。运行持久保存故障计划，在写最终统计前取消并等待注入器退出（包括有界恢复），并检查每项计划故障都有一次成功注入和恢复记录。注入失败或记录缺失为 `INCONCLUSIVE`；就绪恢复超时为 `FAIL`，取消的观察保持 `INCONCLUSIVE`。

恢复检查要求组件专属业务结果：

- Operator 与 Admin API 故障需要独立 canary 的接受身份、精确 source/revision、账本 Run、所属 Kubernetes 对象和 journal 效果。Admin 重放必须返回同一已接受身份。
- PostgreSQL 故障还要求原始在途 canary 的所属 Job 在观察到的故障期间完成，之后终态持久化，并在恢复后产生新 canary。
- Scheduler 故障需要真实 canary SLI 事件，以及计算结果符合预期的新评估 SLO 预算。仅 Job 成功不充分。

Fixture 分配的版本化检查点将故障、恢复、就绪、提交和观察绑定到同一运行、故障与 canary nonce。独立数据库时钟采样约束跨来源时间戳；时序不明确时保持 `INCONCLUSIVE`。验证重新读取来源事实，不接受统计中的 `business_recovered` 或自由文本声明。缺失或不受支持的证据不能产生 `PASS`。Lease 丢失和任意网络分区行为不属于这四种故障类型。

恢复截止时间使用墙钟时间，计划故障偏移使用引擎虚拟阶段时钟。正常负载结束允许有界探针排空；用户取消会停止观察，恢复保留独立的有界清理截止时间。声称通过资格验证前，仍需候选版本的已安装故障测试和完整 standard 运行。

`run` 返回时，最后一批 Job 可能仍在执行；验证会将此不完整证据报告为 `INCONCLUSIVE`。按前述方式导出 `ORBITJOB_DSN` 后，可采样当前运行已投影账本：

```bash
go run ./scripts/loadtest settle --run-id "$RUN_ID"
```

命令只进行一次采样，成功时只打印非终态数量，不等待。它从 `test/load/runs/$RUN_ID`（或 `--run-root`）读取 `created-definitions.json`，按租户分组修订版本，在设置事务内 `app.tenant_id` 的事务中读取每组，随后回滚。定义无效或为空、租户没有匹配账本观察、数据库或事务错误时，以非零码退出且不打印数量。连接详情不进入 argv，不记录原始驱动错误。每次采样有界限制数据库超时。

`scripts/loadtest-ci.sh` 最多轮询此命令 60 次；非零或未知采样之间等待 15 秒。命令失败或输出格式错误均为 `unknown`，不能当作零。次数耗尽后记录警告，仍运行独立验证器，以保留最终缺失证据结论与报告。

Settling 本身不是资格验证证据。它采样 ScheduledJob 集合已投影的 JobRun 账本，不观察未投影 CR 或 Workflow 完成。生成器结束后 cron 定义仍然活动，因此应及时观察本次运行账本并收集验证结果。零采样不保证之后不会出现新 occurrence。空来源继续保持 inconclusive。

<a id="6-report"></a>
### 6. 报告

```bash
go run ./scripts/loadtest report --run-id "$RUN_ID"
```

向运行目录写入 `report.md` 和 `checksums.txt`。不满足资格验证的运行在首行说明：

```
NON-STANDARD RUN - NOT A RELEASE QUALIFICATION
```

报告要求可读取的 `run-record.json`、`run-stats.json` 和 `result.json`，且 run、qualification、profile、seed、start 和 source 身份一致。产物缺失或损坏会停止报告生成。已保存 PASS 还要求执行统计已完成且对账一致，所有必需检查存在并为 PASS；不能同时存在 `completed=false` 或缺少执行身份。Docker CPU/内存值标记为配置要求，不代表观察到的实际分配。托管任务摘要使用设置前记录的本次尝试精确运行目录；不会在早期失败后选择其他运行的最新报告。

---
title: "Job、Run、Function 与 Workflow"
---

<a id="10-jobs-runs-functions-and-workflows"></a>

<a id="101-job-definitions-the-scheduledjob-cr"></a>

<a id="job-definitions-the-scheduledjob-cr"></a>

### 10.1 Job 定义（ScheduledJob CR）

Job 定义是 `ScheduledJob` 自定义资源，由 Operator 投影到 `job_definition_revisions`。下列接口读取不可变修订版本；创建与更新使用 [Web Console](/orbitjob/docs/usage/web-console/) 中描述的独立类型化 `/api/v1/definitions/jobs/{namespace}/{name}` 校验/应用契约。这些写入修改 Kubernetes 资源，不直接写入由 Operator 管理的执行账本。

```bash
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/jobs?limit=50"
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/jobs/<job_id>"
```

也可以通过 GitOps 在映射到租户的 namespace 中声明定义。先把下面内容保存为 `reconcile-billing.yaml`，再应用：

```yaml
apiVersion: workloads.orbitjob.io/v1alpha1
kind: ScheduledJob
metadata:
  name: reconcile-billing
  namespace: your-tenant-namespace
spec:
  schedule: "17 3 * * *"
  concurrencyPolicy: Forbid
  misfirePolicy: Skip
  timeoutSeconds: 300
  retryPolicy:
    maxAttempts: 3
  history:
    successfulRuns: 10
    failedRuns: 10
  jobTemplate:
    image: registry.example.com/jobs/reconcile
    command: ["/app/reconcile"]
    args: ["--tenant", "payments"]
```

```bash
kubectl -n '<tenant-namespace>' apply -f reconcile-billing.yaml
```

| 字段 | 取值 / 含义 |
|---|---|
| `schedule` | 标准五字段 cron，每个到期时间点产生一次执行 |
| `concurrencyPolicy` | `Allow`、`Forbid`、`Replace` |
| `misfirePolicy` | `Skip`、`FireOnce`、`CatchUpBounded` |
| `timeoutSeconds` | 每次执行的 deadline，写入 Kubernetes Job 的 `activeDeadlineSeconds` |
| `retryPolicy.maxAttempts` | 总尝试预算，最小为 1；未设置表示 1，不代表无限重试 |
| `history` | 按结果保留终态执行（`successfulRuns`/`failedRuns`）；0 表示默认值，不代表无限保留 |
| `suspend` | `true` 暂停触发 |
| `jobTemplate` | `image`、`command`、`args`、`backoffLimit` |

来源准入以原子方式将活动来源的 Forbid 策略应用于 ScheduledJob cron、手动请求和 Workflow 步骤请求。已有 occurrence 会在准入求值前返回原有身份，其执行 payload 仍固定。已接受但被 Forbid 阻塞的手动请求等待于 Pending/ConcurrencyBlocked，不产生新账本行。Cron 的跳过／错过触发规则保持生效；Replace 保留原有 cron 取消行为。Check 和 Function 合成来源保持 Allow。参阅[架构](/orbitjob/docs/architecture/#execution-recovery-and-concurrent-admission)和[验证记录](/orbitjob/docs/verification/)。

<a id="102-manual-trigger"></a>

<a id="manual-trigger"></a>

### 10.2 手动触发

将 `JOB_ID` 设为 Jobs API 返回且已审阅的当前修订版本。无需请求体。每次计划的新执行使用新的幂等 key；恢复同一个请求时保留原值：

```bash
JOB_ID='<active revision id>'
curl -sS --fail-with-body -H "$AUTH" \
  "$ORBITJOB_API/api/v1/jobs/$JOB_ID" -o job-reviewed.json
SOURCE_UID="$(python3 -c 'import json; print(json.load(open("job-reviewed.json"))["source_uid"])')"
IDEMPOTENCY_KEY="$(python3 -c 'import uuid; print(uuid.uuid4())')"
curl -sS --fail-with-body -X POST "$ORBITJOB_API/api/v1/jobs/$JOB_ID/trigger" \
  -H "$AUTH" -H "X-OrbitJob-Idempotency-Key: $IDEMPOTENCY_KEY" \
  -o trigger-receipt.json
```

首次发布返回 `201`；重放返回 `200` 与原资源引用。回执包含 `namespace`、`name`、`occurrence_key`、`trigger`、`phase` 和 `created`，不包含账本执行 ID。Operator 尚未观察到资源时，phase 可能为空。HTTP 成功说明发布成功，不代表执行成功。actor 来自已认证 key，不来自调用者提供的 header。

发布后，从账本读取准确的 occurrence：

```bash
OCCURRENCE_KEY="$(python3 -c 'import json; print(json.load(open("trigger-receipt.json"))["occurrence_key"])')"
curl -sS --fail-with-body -G "$ORBITJOB_API/api/v1/instances" -H "$AUTH" \
  --data-urlencode "source_uid=$SOURCE_UID" \
  --data-urlencode "occurrence_key=$OCCURRENCE_KEY" --data 'limit=100'
```

空页可能表示投影仍未完成。使用有上限的观察窗口和退避；窗口结束后仍应保留回执。记录出现后，用 Instance `id` 读取详情、attempt 和取消。Trigger 权限不包含这些读取权限。观察返回 `403` 不会撤销已发布执行，也不是重新触发的理由。幂等 header 去除两端空白后最长 128 个字符；省略时每次调用都会产生新的执行身份。幂等身份与固定的定义修订版本关联，因此新的当前修订版本属于不同请求目标。

<a id="103-runs-and-phases"></a>

<a id="runs-and-phases"></a>

### 10.3 执行与阶段

每次执行（自动或手动）都是 `JobRun` CR；Operator 将其渲染为 Kubernetes Job 并写入账本。阶段变化如下：

```text
Pending -> CreatingAttempt -> Running -> Succeeded
                              |-> RetryWaiting -> CreatingAttempt (next attempt)
                              |-> ExecutionUnknown -> Running / RetryWaiting / Succeeded
                              `-> Failed
Pending/CreatingAttempt/Running/RetryWaiting/ExecutionUnknown -> CancelRequested -> Canceled
                                                        `-> CancelUnknown
```

`ExecutionUnknown` 是已接受的非终态恢复状态，用于已绑定 Job 缺失或同名资源已变为其他 UID 的情况。Operator 必须先恢复任何可信的终态 attempt 事实、保留原始身份，并在观测不确定期间避免创建另一个 attempt。该执行保留其 Forbid 并发占用，不产生终态结果或 SLI 样本。Attempt 暴露已知的 `owner_run_uid`、`execution_unknown_since` 和 `execution_unknown_reason`。权限、限流和传输失败属于观测错误，不能证明 Job 缺失。恢复规则见[执行恢复](/orbitjob/docs/architecture/#execution-recovery-and-concurrent-admission)，已执行场景见[验证记录](/orbitjob/docs/verification/)。

`CancelUnknown` 表示无法确认取消结果。工作负载 Pod 使用 `restartPolicy: Never`；终态失败 Job 结束一次平台 attempt。配置的 Job backoff 可以在同一次 attempt 内创建另一个 Pod，平台重试可以创建另一次 attempt。两者均不保证外部执行恰好一次。

```bash
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/instances?phase=Failed&limit=50"
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/instances/<run_id>"
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/instances/<run_id>/attempts"
```

取消不需要请求体：

```bash
curl -sS -X POST -H "$AUTH" "$ORBITJOB_API/api/v1/instances/<run_id>/cancel"
```

取消会 patch 自有 JobRun CR 的 `spec.cancelRequested`。如果逻辑执行在等待重试期间被取消，失败的执行 attempt 仍保持 Failed。新工作负载 Pod 从创建时即具有 `orbitjob.io/termination-evidence` finalizer。对于活动且已绑定的 attempt，Operator 通过 UID 和 resourceVersion 检查缩短原始 Job 的执行截止时间，再等待 controller 确认停止；已有的 controller 完成条件也可以提供此确认。随后，Operator 校验完整的自有 Pod 清单，将可信的容器退出证据提交到 PostgreSQL，并通过 Pod UID 和 resourceVersion 检查，仅移除自己的 finalizer。只有证据释放后，才使用原始 UID 请求前台删除 Job；只有 Job 已消失且已提交证明仍然有效，才确认 `Canceled`。停止后的 Kubernetes Job 可能报告 `DeadlineExceeded`，而逻辑执行变为 `Canceled`。

正常终止在五分钟确认窗口内保持 `CancelRequested`。协调重试和 Operator 重启不会重置该窗口。Job 删除不能确认进行中的 Pod 创建已完成。仅有资源移除、空 Pod 清单或终态阶段，不能证明工作负载代码已停止。窗口过期仍未确认时变为 `CancelUnknown`。如果新执行的完整清单仍受证据 finalizer 或已有已提交证明保护，之后仍可完成确认。身份冲突、节点丢失和明确证据缺口继续保持隔离，后续空清单或缩小的清单不能解除这些状态。已有且不带 finalizer 的工作负载不会获得事后重建的保证。观测不可用时继续重试，不虚构停止事实。取消和恢复检查见[验证记录](/orbitjob/docs/verification/)。已提交的 Workflow 停止会阻止迟到步骤发布和新 attempt。

账本行存在但 CR 缺失时，API 返回 `409 Conflict`；从未存在的执行返回 `404`。已接受的取消响应表示取消意图，不能证明终止。终态执行保持最终状态。

<a id="104-create-or-update-a-typed-kubernetes-definition"></a>

<a id="create-or-update-a-typed-kubernetes-definition"></a>

### 10.4 创建或更新类型化 Kubernetes 定义

在线定义路径使用 Kubernetes 名称寻址；Jobs/Workflows 列表使用投影后的数字修订版本 ID。应使用映射到本租户的 namespace 和合法 Kubernetes 资源名。更新时先 `GET` 同一路径，保留 `uid`、`resource_version` 与完整 `spec_json` 基线。

下面创建只允许手动执行的 ScheduledJob。请将镜像替换为集群可拉取的镜像。body 只包含类型化 `spec`，不是完整 Kubernetes manifest。Python 在将对象编码为所需的 `spec_json` 字符串时保留整数精度：

```bash
NAMESPACE='<mapped workload namespace>'
DEFINITION_NAME='nightly-export'
DEFINITION_URL="$ORBITJOB_API/api/v1/definitions/jobs/$NAMESPACE/$DEFINITION_NAME"
cat > workload-spec.json <<'JSON'
{"schedule":"","jobTemplate":{"image":"registry.example.com/team/task:reviewed"},"timeoutSeconds":300,"retryPolicy":{"maxAttempts":1},"history":{"successfulRuns":3,"failedRuns":3}}
JSON
python3 - <<'PY'
import json, uuid
spec = json.load(open('workload-spec.json'))
request = dict(request_id=str(uuid.uuid4()), expected_uid='',
               expected_resource_version='', spec_json=json.dumps(spec), dry_run=True)
with open('definition-dry-run.json', 'w') as f:
    json.dump(request, f)
PY
curl -sS --fail-with-body -X PUT "$DEFINITION_URL" -H "$AUTH" \
  -H 'Content-Type: application/json' --data-binary @definition-dry-run.json \
  -o definition-validated.json
```

dry-run 执行集群 admission 校验，不保存资源。准备实际写入前，先审阅返回的规范化 `spec_json` 和 `manifest_digest`：

```bash
python3 - <<'PY'
import json
request = json.load(open('definition-dry-run.json'))
validated = json.load(open('definition-validated.json'))
assert validated['state'] == 'validated'
request.update(spec_json=validated['spec_json'],
               manifest_digest=validated['manifest_digest'], dry_run=False)
with open('definition-apply.json', 'w') as f:
    json.dump(request, f)
PY
curl -sS --fail-with-body -X PUT "$DEFINITION_URL" -H "$AUTH" \
  -H 'Content-Type: application/json' --data-binary @definition-apply.json
```

更新时提供初始 GET 得到的两个预期值，编辑完整 spec 后重新校验。版本过期或 UID 已被替换会返回 `409`。采用另一个 resourceVersion 前，先读取并比较在线 spec；不要替换预期 UID 来覆盖另一个资源。`state:accepted` 只说明 Kubernetes 接受了写入。触发前应确认投影定义对应准确返回的 UID/generation。定义 metadata 不属于可写 schema，也没有定义删除 API。

响应丢失时，保留原 request ID 和 digest，再 GET 定义。匹配的 `last_request_id` 与 `last_manifest_digest` 可确认该笔写入身份；不匹配时，在核对完成前继续保留未知结果。不要自动发送带新身份的写入。应用未暂停且 schedule 非空的定义可能启用自动执行，这与手动 Trigger 授权独立。

<a id="105-manage-and-invoke-a-function"></a>

<a id="manage-and-invoke-a-function"></a>

### 10.5 管理与调用 Function

Function 使用定义中保存的命令和参数执行容器。调用不接受请求体。`POST /functions` 需要 `function:Create` 和必填幂等 header。创建身份由租户、已认证 actor 和幂等 key 确定；相同的规范化请求会返回原始快照，即使之后已有编辑。使用同一身份提交不同定义会产生冲突。请将示例镜像替换为可访问的工作负载镜像：

```bash
FUNCTION_CREATE_KEY="$(python3 -c 'import uuid; print(uuid.uuid4())')"
curl -sS --fail-with-body -X POST "$ORBITJOB_API/api/v1/functions" \
  -H "$AUTH" -H 'Content-Type: application/json' \
  -H "X-OrbitJob-Idempotency-Key: $FUNCTION_CREATE_KEY" \
  -d '{"name":"report-export","image":"registry.example.com/team/task:reviewed","status":"active","timeout_seconds":60,"retry_limit":0,"history_success":3,"history_failed":3}' \
  -o function-created.json
FUNCTION_ID="$(python3 -c 'import json; print(json.load(open("function-created.json"))["id"])')"
curl -sS --fail-with-body -H "$AUTH" \
  "$ORBITJOB_API/api/v1/functions/$FUNCTION_ID" -o function-reviewed.json
```

返回的 `version` 是已保存定义版本。`ready:true` 表示它处于 active 状态，且 `active_revision_version` 与之相同；投影是异步的。应在有上限的 deadline 内持续读取，直到准确保存的版本就绪。出现新版本时需要重新审阅。调用实际审阅过的版本：

```bash
FUNCTION_VERSION="$(python3 -c 'import json; d=json.load(open("function-reviewed.json")); assert d["ready"]; print(d["version"])')"
FUNCTION_INVOKE_KEY="$(python3 -c 'import uuid; print(uuid.uuid4())')"
curl -sS --fail-with-body -X POST \
  "$ORBITJOB_API/api/v1/functions/$FUNCTION_ID/invoke?expected_version=$FUNCTION_VERSION&wait_seconds=30" \
  -H "$AUTH" -H "X-OrbitJob-Idempotency-Key: $FUNCTION_INVOKE_KEY"
curl -sS --fail-with-body -H "$AUTH" \
  "$ORBITJOB_API/api/v1/functions/$FUNCTION_ID/runs?limit=50&offset=0"
```

Invoke 不接受 body。`wait_seconds` 范围为 0 至 60；等待超时会返回最后观察到的 phase 和回执，不会虚构终态结果。Function 已暂停、审阅版本已变化或对应修订版本仍在投影时返回 `409`。`201` 与 `200` 区分新发布和重放，不区分执行成功与失败。Function 执行在 Kubernetes Pod 中运行；重放同一调用身份不会创建另一次执行。回执不包含 Function HTTP 响应正文或日志文本。

历史只记录终态结果（`success`、`failed`、`canceled`）。使用非 null 的 `occurrence_key` 关联调用回执，再用 UUID `run_id` 调用 `GET /functions/{id}/runs/{run_id}`。occurrence 映射为 null 时表示未知，不要根据 UUID 推导。列表行的 `attempts:null`；详情会返回实际保留的 attempt 和 `last_attempt`。Null 表示不可用，`[]` 确认没有保留的 attempt。不要猜测使用 attempt 1 读取日志。

更新使用 `PUT /functions/{id}`，必须提交**完整** Function 定义和最新 `expected_version`；省略的选项会采用替换默认值。从 GET 中只复制可写字段到更新文件，排除 ID、就绪信息和时间戳。暂停或恢复时，在完整 body 中将 `status` 改为 `paused` 或 `active`，再审阅新保存版本及其就绪状态。不提供独立 Function pause、delete 或资源组范围 cancel 接口。未绑定资源组且具备独立 Instance 权限的调用者，可通过 Instance API 观察并取消准确关联的底层 occurrence。

```bash
python3 - <<'PY'
import json
reviewed = json.load(open('function-reviewed.json'))
fields = ('name', 'description', 'status', 'image', 'command', 'args',
          'timeout_seconds', 'retry_limit', 'history_success', 'history_failed', 'labels')
request = {field: reviewed[field] for field in fields}
request.update(expected_version=reviewed['version'], status='paused')
with open('function-update.json', 'w') as f:
    json.dump(request, f)
PY
curl -sS --fail-with-body -X PUT "$ORBITJOB_API/api/v1/functions/$FUNCTION_ID" \
  -H "$AUTH" -H 'Content-Type: application/json' --data-binary @function-update.json
```

<a id="106-trigger-observe-and-cancel-a-workflow"></a>

<a id="trigger-observe-and-cancel-a-workflow"></a>

### 10.6 触发、观察与取消 Workflow

通过 `/definitions/workflows/{namespace}/{name}` 创建或更新 WorkflowJob，使用与 Job 相同的 dry-run、规范化 digest 和 UID/version 流程。其 spec 包含 `tasks`，其中 `jobRef.name` 引用相同 namespace 的 ScheduledJob，不嵌入任意镜像或 Pod spec。触发前先读取当前 Workflow 修订版本及其 DAG。

```bash
curl -sS --fail-with-body -H "$AUTH" "$ORBITJOB_API/api/v1/workflows?limit=50"
WORKFLOW_ID='<reviewed active workflow revision id>'
curl -sS --fail-with-body -H "$AUTH" "$ORBITJOB_API/api/v1/workflows/$WORKFLOW_ID"
WORKFLOW_TRIGGER_KEY="$(python3 -c 'import uuid; print(uuid.uuid4())')"
curl -sS --fail-with-body -X POST "$ORBITJOB_API/api/v1/workflows/$WORKFLOW_ID/runs" \
  -H "$AUTH" -H "X-OrbitJob-Idempotency-Key: $WORKFLOW_TRIGGER_KEY" \
  -o workflow-receipt.json
curl -sS --fail-with-body -H "$AUTH" \
  "$ORBITJOB_API/api/v1/workflows/$WORKFLOW_ID/runs?limit=50&offset=0"
```

Trigger 不需要 body，返回 WorkflowRun 回执，不返回数字账本 ID。对 Workflow 历史逐页按 `occurrence_key` 匹配；列表没有 occurrence filter。找到后，将 `WORKFLOW_RUN_ID` 设为该行数字 `id`：

```bash
WORKFLOW_RUN_ID='<matching workflow history id>'
curl -sS --fail-with-body -H "$AUTH" \
  "$ORBITJOB_API/api/v1/workflows/$WORKFLOW_ID/runs/$WORKFLOW_RUN_ID"
curl -sS --fail-with-body -X POST -H "$AUTH" \
  "$ORBITJOB_API/api/v1/workflows/$WORKFLOW_ID/runs/$WORKFLOW_RUN_ID/cancel"
```

详情暴露固定的 DAG 和步骤账本 ID。用步骤 ID 读取 Instance attempts 和独立授权的日志。若 `pinned_revision_available:false` 且 `tasks:null`，表示历史图元数据不可用；应使用已记录的 step/task decisions，不要拿当前 DAG 替代。保留的历史父修订版本 ID 仍可读取；触发使用当前修订版本。取消需要 `workflow:Trigger`；Operator 将取消意图传递给未完成步骤。响应 `200` 不代表所有 Pod 已停止。

自动调度可校验五字段 cron 和 IANA 时区：

```bash
curl -sS --fail-with-body -X POST "$ORBITJOB_API/api/v1/workflows/schedule-preview" \
  -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"schedule":"0 9 * * 1-5","time_zone":"Asia/Shanghai","count":5}'
```

Preview 需要未绑定资源组的 `workflow:Apply`；`count` 为 1 至 20，`upcoming` 包含未来的 UTC 时间点。保存的 spec 使用驼峰字段 `timeZone`，preview 请求体使用 `time_zone`。Workflow 并发支持 `Allow` 或 `Forbid`，错过的启动会跳过；`suspend:true` 暂停自动启动，手动触发仍可用。计划触发与 Forbid 冲突时保留终态 Rejected 跳过回执，原因为 `concurrency_forbid`。根据手动准入契约，已接受的手动 WorkflowRun 等待于 Pending/ConcurrencyBlocked，不产生账本行或步骤，直到可以进入。Unknown 子执行阻止新步骤，并保留父级并发占用；已运行的同级步骤仍可观测。已执行场景见[验证记录](/orbitjob/docs/verification/)。

<a id="107-read-retained-attempt-logs"></a>

<a id="read-retained-attempt-logs"></a>

### 10.7 读取保留的 attempt 日志

需要持久历史和可重新连接的实时观测时，使用 `GET /api/v1/instances/{run_id}/logs/archive?after=0&limit=500`，或对应的 Function 历史 `/logs/archive` 路由。将返回的 `next` 游标作为 `after`；`attempt` 和 `search` 用于筛选保留的证据。`/download` 后缀导出保留的输出和缺口标记。独立的 ReadLogs 授权仍然适用。对归档基础路由执行受审计的 `DELETE` 需要 DeleteLogs，并返回异步请求回执。参阅[归档指南](/orbitjob/docs/execution-logs/)。以下快照路由继续兼容旧客户端和历史执行。

使用 Instance attempts 或 Function 调用详情实际返回的正数 attempt number，不是 attempt 行 ID：

```bash
RUN_ID='<instance ledger id>'
ATTEMPT='<retained attempt number>'
curl -sS --fail-with-body -G \
  "$ORBITJOB_API/api/v1/instances/$RUN_ID/attempts/$ATTEMPT/logs" -H "$AUTH" \
  --data 'tail_lines=100' --data 'limit_bytes=16384'
FUNCTION_RUN_UUID='<function history run_id>'
curl -sS --fail-with-body -G \
  "$ORBITJOB_API/api/v1/functions/$FUNCTION_ID/runs/$FUNCTION_RUN_UUID/attempts/$ATTEMPT/logs" \
  -H "$AUTH" --data 'tail_lines=100' --data 'limit_bytes=16384'
```

`tail_lines` 范围为 1 至 500，`limit_bytes` 为 1 至 65536。解释 `text` 前先查看 `state`、`reason` 和 `truncated`；HTTP `200` 也可能表示尚在启动、证据缺失或不可用。服务端验证不可变归属链，自行解析 namespace/Pod/container locator；调用者传入的 locator 会被拒绝。日志是有大小限制、请求 deadline 为五秒的快照，不是流或持久归档。删除来源 JobRun/Pod 后日志可能丢失。如果 Pod 已消失但通过验证的 Job 仍保留，`reason` 包含 Job 已记录的失败原因和消息，例如 `DeadlineExceeded`。这是平台诊断证据；由于未恢复容器输出，`state` 仍为 `absent`，`text` 保持为空。

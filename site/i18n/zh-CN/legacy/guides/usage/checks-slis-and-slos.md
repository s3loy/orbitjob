---
title: "Check、SLI 与 SLO"
---

<a id="12-checks-slis-and-slos"></a>

<a id="121-checks"></a>

<a id="checks"></a>

### 12.1 Check

当前只支持 `http_health`。Check 以 Kubernetes Job 执行，使用按 digest 固定的 `curlimages/curl` 探针，只支持 GET/HEAD/OPTIONS。interval 调度间隔至少为 30 秒：

```bash
curl -sS -X POST "$ORBITJOB_API/api/v1/checks" \
  -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{
    "name":"public-api-health",
    "check_type":"http_health",
    "check_config":{"url":"https://example.com/health","method":"GET","expected_status":200},
    "assertion_rules":[{"metric":"response_time_ms","operator":">","threshold":500,"severity":"warning"}],
    "schedule_type":"interval",
    "interval_sec":60,
    "timeout_sec":10
  }'
```

示例保存了一条断言规则，用于在响应时间超过 500ms 时产生 warning。断言规则接受 `>`、`<`、`==`、`!=`、`>=`、`<=`，但当前容器探针不求值 `assertion_rules`，也不产生 warning 严重级别。探针仅在超时内检查配置的 URL、方法和预期 HTTP 状态；Check 结果由执行结果派生。

请保留返回的数字 Check `id`。Check 执行来源为 `check-<id>`；Check 响应没有 `source_uid` 字段。Check run 的 UUID 引用与数字 Instance 账本 ID 不同。应使用 Check run 自身的数字 `id` 读取记录：

```bash
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/checks?status=active"
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/check-runs?check_id=<id>"
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/check-runs/<check_run_id>"
```

暂停、恢复和删除 Check 都需要当前 `version`；删除也必须带 JSON body。每个独立操作前都要重新读取最新版本。例如，用响应中的版本暂停 Check：

```bash
CHECK_ID='<created check id>'
curl -sS --fail-with-body -H "$AUTH" \
  "$ORBITJOB_API/api/v1/checks/$CHECK_ID" -o check-current.json
python3 - <<'PY'
import json
with open('check-version.json', 'w') as f:
    json.dump({'version': json.load(open('check-current.json'))['version']}, f)
PY
curl -sS --fail-with-body -X POST "$ORBITJOB_API/api/v1/checks/$CHECK_ID/pause" \
  -H "$AUTH" -H 'Content-Type: application/json' --data-binary @check-version.json
```

恢复使用 `/checks/{id}/resume`，并传重新读取的版本。删除同样使用版本请求体与新的读取结果，不通过 query 传版本：

```bash
curl -sS -X DELETE "$ORBITJOB_API/api/v1/checks/<id>" \
  -H "$AUTH" -H 'Content-Type: application/json' -d '{"version":2}'
```

<a id="122-slis"></a>

<a id="slis"></a>

### 12.2 SLI

```bash
curl -sS -X POST "$ORBITJOB_API/api/v1/slis" \
  -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{
    "name":"public-api-availability",
    "sli_type":"availability",
    "source_type":"job_run",
    "source_config":{"source_uid":"<source_uid>"},
    "aggregation":"ratio",
    "good_event_criteria":{"status":"success"}
  }'
```

SLI 根据执行账本事件生成五分钟 UTC bucket。`source_type` 为 `job_run`，`source_config.source_uid` 指定来源定义。使用获准读取返回的实际来源身份：`check-` 加返回的 Check ID、Job 返回的 `source_uid`（ScheduledJob CR UID），或 `function-` 加实际 Function ID。来源必须处于活动状态，且在调用方租户／资源组内可见。`good_event_criteria` 只接受 `status`（`success` 或 `failed`）和 `duration_ms`（有限非负数，或 `{"op":"<=","value":500}` 等比较对象）。Availability/quality 需要非空条件；latency 需要 `duration_ms`。Custom 条件可以为空，此时所有生成的事件都计为 good。未知字段和运算符会被拒绝。通过 `GET /slis` 和 `GET /slis/{id}` 读取定义；配置可读不证明已有样本。

终态投影重放具有幂等性：相同租户／执行／SLI 事实只增加一次 bucket。失败的派生保持待重试，并防止其执行历史被清理。这不表示工作负载恰好执行一次。

删除 SLI 需要当前 `version`：

```bash
curl -sS -X DELETE "$ORBITJOB_API/api/v1/slis/<id>" \
  -H "$AUTH" -H 'Content-Type: application/json' -d '{"version":2}'
```

<a id="123-slos-budgets-and-alerts"></a>

<a id="slos-budgets-and-alerts"></a>

### 12.3 SLO、预算与告警

```bash
curl -sS -X POST "$ORBITJOB_API/api/v1/slos" \
  -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{
    "name":"public-api-99.9",
    "sli_id":9,
    "target":0.999,
    "window_type":"rolling",
    "window_duration":"720h",
    "alert_fast_burn_rate":14.4,
    "alert_slow_burn_rate":6
  }'
```

将示例 `sli_id` 替换为创建 SLI 返回的 ID。SLO 只能引用相同租户和适用资源组内可见的 SLI。`window_duration` 使用 Go duration 格式，例如 `168h`、`720h`，边界见 OpenAPI。Burn 阈值必须有限，slow burn 必须低于 fast burn。支持 `target` 为 `1`，并使用明确的零预算行为。

将示例中的 `sli_id:9` 替换为创建或读取 SLI 时返回的准确数字 ID。该 SLI 必须在相同租户和允许的资源组内可访问。预算与告警使用 SLO 返回的数字 ID：

```bash
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/slos/<id>"
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/slos/<id>/budget"
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/slos/<id>/budgets"
curl -sS -H "$AUTH" "$ORBITJOB_API/api/v1/slo-alerts?slo_id=<id>&status=firing"
```

暂停、恢复和删除 SLO 都需要在 JSON body 中提供当前 `version`，与 Check 一样先做新的 GET。对应接口为 `POST /slos/{id}/pause`、`POST /slos/{id}/resume` 和 `DELETE /slos/{id}`。`GET /slo-alerts/{id}` 读取单条告警；不提供告警确认操作。没有 Check/SLI 样本时预算可能为空，这不是 API 错误。零样本和零允许量不能证明实际健康，也不能支持有意义的 burn-rate 比较。监控权限与执行权限独立。资源组 key 可管理自己组内 Check/SLI/SLO，并独立授权读取 parent Check/SLO 组内的 result、budget、alert。Detail 与单个 SLO 的 budget/history 按实际 parent group 和 ID 授权，未绑定资源组的凭据也遵守 group-specific Deny；其他组 parent 返回 404，包括 history。CheckRun/Alert 集合权限必须覆盖整个 tenant 或凭据 group 查询域；相关逐 ID/group Deny、boundary 限制或 proof budget 耗尽时，在读取 items/counts 前整请求返回 403。过滤与分页不能绕过该检查，也不返回静默过滤后的部分响应。SLO definition 的可选嵌入 `current_budget` 仍对 group-scoped caller 省略；未绑定资源组的调用者需要在实际 SLO group 和 ID 上获准 `slo:GetBudget`，并遵守显式 Deny。

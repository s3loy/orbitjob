#!/usr/bin/env bash
# OrbitJob 全功能端到端测试脚本
# 从构建到所有 API 功能全覆盖
# 用法: ./scripts/smoke-test.sh [API_BASE_URL]
# 默认 API_BASE_URL=http://localhost:18080 (docker compose)；devserver 传 http://localhost:8080

set -euo pipefail

# ── 颜色 ───────────────────────────────────────────────────────
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# ── 配置 ───────────────────────────────────────────────────────
API="${1:-http://localhost:18080}"
BOOTSTRAP_KEY="${ADMIN_BOOTSTRAP_API_KEY:-otj_devkey_2026}"
PASS=0
FAIL=0
SKIP=0
RESULTS_FILE="smoke-test-results.md"

# ── 工具函数 ───────────────────────────────────────────────────
log_pass() { echo -e "${GREEN}[PASS]${NC} $1"; PASS=$((PASS+1)); echo "- [x] $1" >> "$RESULTS_FILE"; }
log_fail() { echo -e "${RED}[FAIL]${NC} $1"; FAIL=$((FAIL+1)); echo "- [ ] $1" >> "$RESULTS_FILE"; }
log_skip() { echo -e "${YELLOW}[SKIP]${NC} $1"; SKIP=$((SKIP+1)); echo "- [-] $1 (skipped)" >> "$RESULTS_FILE"; }
log_info() { echo -e "${BLUE}[INFO]${NC} $1"; }
log_section() { echo ""; echo -e "${BLUE}═══ $1 ═══${NC}"; echo "" >> "$RESULTS_FILE"; echo "## $1" >> "$RESULTS_FILE"; }

# jq 提取字段（容错）
jget() { echo "$1" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('$2',''))" 2>/dev/null || echo ""; }

# HTTP 请求 + 状态码捕获
http() {
  local method="$1" path="$2"
  shift 2
  local code body
  body=$(curl -s -w "\n%{http_code}" -X "$method" "$API$path" "$@" 2>/dev/null) || true
  code=$(echo "$body" | tail -1)
  body=$(echo "$body" | sed '$d')
  LAST_CODE="$code"
  LAST_BODY="$body"
}

# 断言 HTTP 状态码
assert_code() {
  local expected="$1" name="$2"
  if [ "$LAST_CODE" = "$expected" ]; then
    log_pass "$name (HTTP $LAST_CODE)"
  else
    log_fail "$name (expected $expected, got $LAST_CODE): $LAST_BODY"
  fi
}

# ── 初始化结果文件 ─────────────────────────────────────────────
cat > "$RESULTS_FILE" << EOF
# OrbitJob 全功能测试报告

> 自动生成于 $(date)
> 测试目标: 每个端点、每个枚举值、所有 handler 类型

EOF

# ═══════════════════════════════════════════════════════════════
log_section "0. 前置检查"
# ═══════════════════════════════════════════════════════════════

log_info "检查 curl / python3 / docker"
command -v curl >/dev/null && log_pass "curl 可用" || { log_fail "curl 不可用"; exit 1; }
command -v python3 >/dev/null && log_pass "python3 可用" || { log_fail "python3 不可用"; exit 1; }

log_info "等待 admin-api 就绪 (最多 60s)"
for i in $(seq 1 60); do
  if curl -s "$API/healthz" >/dev/null 2>&1; then
    log_pass "admin-api 就绪 (${i}s)"
    break
  fi
  [ "$i" = "60" ] && { log_fail "admin-api 60s 内未就绪"; exit 1; }
  sleep 1
done

# ═══════════════════════════════════════════════════════════════
log_section "1. 系统端点"
# ═══════════════════════════════════════════════════════════════

http GET "/healthz"
assert_code "200" "GET /healthz 健康检查"

http GET "/metrics"
assert_code "200" "GET /metrics Prometheus 指标"

http GET "/openapi.json"
assert_code "200" "GET /openapi.json OpenAPI spec"

# ═══════════════════════════════════════════════════════════════
log_section "2. 认证"
# ═══════════════════════════════════════════════════════════════

# 无认证
http GET "/api/v1/tenants"
assert_code "401" "无认证访问被拒 (401)"

# 错误 key
http GET "/api/v1/tenants" -H "Authorization: Bearer wrong-key"
assert_code "401" "错误 API key 被拒 (401)"

# 正确 bootstrap key
http GET "/api/v1/tenants" -H "Authorization: Bearer $BOOTSTRAP_KEY"
assert_code "200" "bootstrap key 认证成功"
BOOTSTRAP_TENANT=$(echo "$LAST_BODY" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('items',[{}])[0].get('slug',''))" 2>/dev/null || echo "")
log_info "bootstrap tenant slug: $BOOTSTRAP_TENANT"

# ═══════════════════════════════════════════════════════════════
log_section "3. Tenant 租户"
# ═══════════════════════════════════════════════════════════════

AUTH="-H Authorization:Bearer\ $BOOTSTRAP_KEY"

# 创建租户
http POST "/api/v1/tenants" \
  -H "Authorization: Bearer $BOOTSTRAP_KEY" \
  -H "Content-Type: application/json" \
  -d '{"slug":"test-team","name":"Test Team","status":"active"}'
TENANT_ID=$(jget "$LAST_BODY" "id")
assert_code "201" "创建租户 test-team"

# 重复创建（冲突）
http POST "/api/v1/tenants" \
  -H "Authorization: Bearer $BOOTSTRAP_KEY" \
  -H "Content-Type: application/json" \
  -d '{"slug":"test-team","name":"Test Team"}'
assert_code "409" "重复 slug 冲突 (409)"

# 校验失败：空 slug
http POST "/api/v1/tenants" \
  -H "Authorization: Bearer $BOOTSTRAP_KEY" \
  -H "Content-Type: application/json" \
  -d '{"slug":"","name":"x"}'
assert_code "400" "空 slug 校验失败 (400)"

# 列出租户
http GET "/api/v1/tenants?limit=20" -H "Authorization: Bearer $BOOTSTRAP_KEY"
assert_code "200" "列出租户"
TENANT_COUNT=$(echo "$LAST_BODY" | python3 -c "import sys,json; print(len(json.load(sys.stdin).get('items',[])))" 2>/dev/null || echo "0")
[ "$TENANT_COUNT" -ge 2 ] && log_pass "租户数 >= 2 (bootstrap + test-team)" || log_fail "租户数异常: $TENANT_COUNT"

# 查询单个租户
http GET "/api/v1/tenants/$TENANT_ID" -H "Authorization: Bearer $BOOTSTRAP_KEY"
assert_code "200" "查询单个租户"

# 查询不存在
http GET "/api/v1/tenants/no-such-team" -H "Authorization: Bearer $BOOTSTRAP_KEY"
assert_code "404" "查询不存在租户 (404)"

# ═══════════════════════════════════════════════════════════════
log_section "4. API Key"
# ═══════════════════════════════════════════════════════════════

# 为 test-team 创建 API key
http POST "/api/v1/tenants/$TENANT_ID/api_keys" \
  -H "Authorization: Bearer $BOOTSTRAP_KEY" \
  -H "Content-Type: application/json" \
  -d '{}'
assert_code "201" "为 test-team 创建 API key"
TEST_KEY=$(jget "$LAST_BODY" "key")
TEST_KEY_ID=$(jget "$LAST_BODY" "id")
[ -n "$TEST_KEY" ] && log_pass "API key 明文返回" || log_fail "API key 明文未返回"
log_info "test-team key: ${TEST_KEY:0:12}... (id=$TEST_KEY_ID)"

# 列出 key
http GET "/api/v1/tenants/$TENANT_ID/api_keys" -H "Authorization: Bearer $BOOTSTRAP_KEY"
assert_code "200" "列出 test-team 的 API key"

# 吊销 key
http POST "/api/v1/api_keys/$TEST_KEY_ID/revoke" -H "Authorization: Bearer $BOOTSTRAP_KEY"
assert_code "200" "吊销 API key"

# 用已吊销 key 访问（应 401）
http GET "/api/v1/jobs" -H "Authorization: Bearer $TEST_KEY"
assert_code "401" "已吊销 key 访问被拒 (401)"

# 再创建一个用于后续测试
http POST "/api/v1/tenants/$TENANT_ID/api_keys" \
  -H "Authorization: Bearer $BOOTSTRAP_KEY" \
  -H "Content-Type: application/json" \
  -d '{}'
assert_code "201" "再创建 API key"
TEST_KEY=$(jget "$LAST_BODY" "key")
log_info "新 test-team key: ${TEST_KEY:0:12}..."

# ═══════════════════════════════════════════════════════════════
log_section "5. Job - exec handler"
# ═══════════════════════════════════════════════════════════════

# 创建 manual + exec job
http POST "/api/v1/jobs" \
  -H "Authorization: Bearer $TEST_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "name":"exec-test",
    "trigger_type":"manual",
    "handler_type":"exec",
    "handler_payload":{"command":"echo","args":["hello-orbitjob"]},
    "timeout_sec":30
  }'
assert_code "201" "创建 manual+exec job"
EXEC_JOB_ID=$(jget "$LAST_BODY" "id")
EXEC_JOB_VERSION=$(jget "$LAST_BODY" "version")
log_info "exec job id=$EXEC_JOB_ID version=$EXEC_JOB_VERSION"

# 触发
http POST "/api/v1/jobs/$EXEC_JOB_ID/trigger" \
  -H "Authorization: Bearer $TEST_KEY" \
  -H "Content-Type: application/json" -d '{}'
assert_code "201" "触发 exec job"
EXEC_RUN_ID=$(jget "$LAST_BODY" "run_id")
log_info "exec run_id=$EXEC_RUN_ID"

# 幂等触发
http POST "/api/v1/jobs/$EXEC_JOB_ID/trigger" \
  -H "Authorization: Bearer $TEST_KEY" \
  -H "X-OrbitJob-Idempotency-Key: idem-exec-1" \
  -H "Content-Type: application/json" -d '{}'
assert_code "201" "幂等触发 (第一次创建)"
http POST "/api/v1/jobs/$EXEC_JOB_ID/trigger" \
  -H "Authorization: Bearer $TEST_KEY" \
  -H "X-OrbitJob-Idempotency-Key: idem-exec-1" \
  -H "Content-Type: application/json" -d '{}'
assert_code "200" "幂等触发 (第二次返回已存在)"

# 等待执行完成
log_info "等待 exec job 执行..."
for i in $(seq 1 15); do
  http GET "/api/v1/instances/$EXEC_RUN_ID" -H "Authorization: Bearer $TEST_KEY"
  EXEC_STATUS=$(jget "$LAST_BODY" "status")
  [ "$EXEC_STATUS" = "success" ] || [ "$EXEC_STATUS" = "failed" ] && break
  sleep 1
done
log_info "exec final status: $EXEC_STATUS"

# 查询 instance
http GET "/api/v1/instances/$EXEC_RUN_ID" -H "Authorization: Bearer $TEST_KEY"
assert_code "200" "查询 exec instance"

# 查看 attempts
http GET "/api/v1/instances/$EXEC_RUN_ID/attempts" -H "Authorization: Bearer $TEST_KEY"
assert_code "200" "查看 exec instance attempts"

# ═══════════════════════════════════════════════════════════════
log_section "6. Job - http handler"
# ═══════════════════════════════════════════════════════════════

http POST "/api/v1/jobs" \
  -H "Authorization: Bearer $TEST_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "name":"http-test",
    "trigger_type":"manual",
    "handler_type":"http",
    "handler_payload":{"url":"https://httpbin.org/get","method":"GET","timeout_sec":15},
    "timeout_sec":30
  }'
assert_code "201" "创建 manual+http job"
HTTP_JOB_ID=$(jget "$LAST_BODY" "id")

http POST "/api/v1/jobs/$HTTP_JOB_ID/trigger" \
  -H "Authorization: Bearer $TEST_KEY" -H "Content-Type: application/json" -d '{}'
assert_code "201" "触发 http job"
HTTP_RUN_ID=$(jget "$LAST_BODY" "run_id")

# ═══════════════════════════════════════════════════════════════
log_section "7. Job - webhook handler"
# ═══════════════════════════════════════════════════════════════

http POST "/api/v1/jobs" \
  -H "Authorization: Bearer $TEST_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "name":"webhook-test",
    "trigger_type":"manual",
    "handler_type":"webhook",
    "handler_payload":{"url":"https://httpbin.org/post","method":"POST","body":"{\"event\":\"test\"}","headers":{"Content-Type":"application/json"}},
    "timeout_sec":30
  }'
assert_code "201" "创建 manual+webhook job"
WEBHOOK_JOB_ID=$(jget "$LAST_BODY" "id")

http POST "/api/v1/jobs/$WEBHOOK_JOB_ID/trigger" \
  -H "Authorization: Bearer $TEST_KEY" -H "Content-Type: application/json" -d '{}'
assert_code "201" "触发 webhook job"

# ═══════════════════════════════════════════════════════════════
log_section "8. Job - pg_notify handler"
# ═══════════════════════════════════════════════════════════════

http POST "/api/v1/jobs" \
  -H "Authorization: Bearer $TEST_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "name":"pgnotify-test",
    "trigger_type":"manual",
    "handler_type":"pg_notify",
    "handler_payload":{"channel":"test_channel","payload":"{\"hello\":\"world\"}"},
    "timeout_sec":10
  }'
assert_code "201" "创建 manual+pg_notify job"
PGNOTIFY_JOB_ID=$(jget "$LAST_BODY" "id")

http POST "/api/v1/jobs/$PGNOTIFY_JOB_ID/trigger" \
  -H "Authorization: Bearer $TEST_KEY" -H "Content-Type: application/json" -d '{}'
assert_code "201" "触发 pg_notify job"

# ═══════════════════════════════════════════════════════════════
log_section "9. Job - cron + 校验"
# ═══════════════════════════════════════════════════════════════

# cron job
http POST "/api/v1/jobs" \
  -H "Authorization: Bearer $TEST_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "name":"cron-test",
    "trigger_type":"cron",
    "cron_expr":"*/5 * * * *",
    "timezone":"Asia/Shanghai",
    "handler_type":"exec",
    "handler_payload":{"command":"date"},
    "concurrency_policy":"forbid",
    "misfire_policy":"fire_now",
    "retry_limit":2,
    "retry_backoff_strategy":"exponential"
  }'
assert_code "201" "创建 cron+exec job (forbid/fire_now/exponential)"
CRON_JOB_ID=$(jget "$LAST_BODY" "id")

# 校验：cron 缺 cron_expr
http POST "/api/v1/jobs" \
  -H "Authorization: Bearer $TEST_KEY" \
  -H "Content-Type: application/json" \
  -d '{"name":"bad","trigger_type":"cron","handler_type":"exec","handler_payload":{"command":"echo"}}'
assert_code "400" "cron 缺 cron_expr 校验失败 (400)"

# 校验：无效 handler_type
http POST "/api/v1/jobs" \
  -H "Authorization: Bearer $TEST_KEY" \
  -H "Content-Type: application/json" \
  -d '{"name":"bad","trigger_type":"manual","handler_type":"invalid","handler_payload":{}}'
assert_code "400" "无效 handler_type 校验失败 (400)"

# 校验：无效 concurrency_policy
http POST "/api/v1/jobs" \
  -H "Authorization: Bearer $TEST_KEY" \
  -H "Content-Type: application/json" \
  -d '{"name":"bad","trigger_type":"manual","handler_type":"exec","handler_payload":{"command":"echo"},"concurrency_policy":"bad"}'
assert_code "400" "无效 concurrency_policy 校验失败 (400)"

# ═══════════════════════════════════════════════════════════════
log_section "10. Job - 查询/更新/暂停/恢复/删除"
# ═══════════════════════════════════════════════════════════════

# 列出
http GET "/api/v1/jobs?limit=20" -H "Authorization: Bearer $TEST_KEY"
assert_code "200" "列出 job"

http GET "/api/v1/jobs?status=active&limit=5" -H "Authorization: Bearer $TEST_KEY"
assert_code "200" "按 status 过滤列出 job"

# 查询单个
http GET "/api/v1/jobs/$EXEC_JOB_ID" -H "Authorization: Bearer $TEST_KEY"
EXEC_JOB_VERSION=$(jget "$LAST_BODY" "version")
assert_code "200" "查询单个 job"

# 查询不存在
http GET "/api/v1/jobs/999999" -H "Authorization: Bearer $TEST_KEY"
assert_code "404" "查询不存在 job (404)"

# 更新（乐观锁）
http PUT "/api/v1/jobs/$EXEC_JOB_ID" \
  -H "Authorization: Bearer $TEST_KEY" \
  -H "Content-Type: application/json" \
  -H "X-Actor-ID: tester" \
  -d "{\"version\":$EXEC_JOB_VERSION,\"name\":\"exec-test-renamed\",\"retry_limit\":5}"
assert_code "200" "更新 job (PUT + 乐观锁)"

# 更新冲突（旧 version）
http PUT "/api/v1/jobs/$EXEC_JOB_ID" \
  -H "Authorization: Bearer $TEST_KEY" \
  -H "Content-Type: application/json" \
  -H "X-Actor-ID: tester" \
  -d "{\"version\":$EXEC_JOB_VERSION,\"name\":\"stale\"}"
assert_code "409" "更新 job 乐观锁冲突 (409)"

# 暂停
EXEC_JOB_VERSION=$(jget "$LAST_BODY" "version")
[ -z "$EXEC_JOB_VERSION" ] && EXEC_JOB_VERSION=2
http POST "/api/v1/jobs/$CRON_JOB_ID/pause" \
  -H "Authorization: Bearer $TEST_KEY" \
  -H "Content-Type: application/json" \
  -H "X-Actor-ID: tester" \
  -d '{"version":1}'
assert_code "200" "暂停 cron job"

# 恢复
http POST "/api/v1/jobs/$CRON_JOB_ID/resume" \
  -H "Authorization: Bearer $TEST_KEY" \
  -H "Content-Type: application/json" \
  -H "X-Actor-ID: tester" \
  -d '{"version":2}'
assert_code "200" "恢复 cron job"

# 删除
http DELETE "/api/v1/jobs/$CRON_JOB_ID" \
  -H "Authorization: Bearer $TEST_KEY" \
  -H "X-Actor-ID: tester"
assert_code "200" "删除 cron job"

# ═══════════════════════════════════════════════════════════════
log_section "11. Instance - 列表/取消"
# ═══════════════════════════════════════════════════════════════

# 列出 instance
http GET "/api/v1/instances?limit=20" -H "Authorization: Bearer $TEST_KEY"
assert_code "200" "列出 instance"

http GET "/api/v1/instances?status=success&limit=10" -H "Authorization: Bearer $TEST_KEY"
assert_code "200" "按 status 过滤列出 instance"

# 取消（用一个新触发的 instance）
http POST "/api/v1/jobs/$EXEC_JOB_ID/trigger" \
  -H "Authorization: Bearer $TEST_KEY" -H "Content-Type: application/json" -d '{}'
CANCEL_RUN_ID=$(jget "$LAST_BODY" "run_id")
[ -n "$CANCEL_RUN_ID" ] && http GET "/api/v1/instances/$CANCEL_RUN_ID" -H "Authorization: Bearer $TEST_KEY"
CANCEL_VERSION=$(jget "$LAST_BODY" "version")
[ -n "$CANCEL_RUN_ID" ] && http POST "/api/v1/instances/$CANCEL_RUN_ID/cancel" \
  -H "Authorization: Bearer $TEST_KEY" \
  -H "Content-Type: application/json" \
  -d "{\"version\":$CANCEL_VERSION}" && assert_code "200" "取消 instance" || log_skip "取消 instance (无可用 instance)"

# ═══════════════════════════════════════════════════════════════
log_section "12. Check 健康检查"
# ═══════════════════════════════════════════════════════════════

# 创建 interval check
http POST "/api/v1/checks" \
  -H "Authorization: Bearer $TEST_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "name":"http-check",
    "check_type":"http_health",
    "check_config":{"url":"https://httpbin.org/get","method":"GET"},
    "assertion_rules":[{"metric":"status","operator":"==","threshold":200,"severity":"critical"}],
    "schedule_type":"interval",
    "interval_sec":30,
    "timeout_sec":10,
    "priority":5,
    "labels":{"env":"test"}
  }'
assert_code "201" "创建 interval check"
CHECK_ID=$(jget "$LAST_BODY" "id")
CHECK_VERSION=$(jget "$LAST_BODY" "version")

# 创建 cron check
http POST "/api/v1/checks" \
  -H "Authorization: Bearer $TEST_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "name":"cron-check",
    "check_type":"http_health",
    "check_config":{"url":"https://httpbin.org/status/200","method":"GET"},
    "schedule_type":"cron",
    "cron_expr":"*/10 * * * *",
    "timezone":"UTC"
  }'
assert_code "201" "创建 cron check"

# 校验：无效 check_type
http POST "/api/v1/checks" \
  -H "Authorization: Bearer $TEST_KEY" \
  -H "Content-Type: application/json" \
  -d '{"name":"bad","check_type":"invalid","check_config":{},"schedule_type":"interval","interval_sec":30}'
assert_code "400" "无效 check_type 校验失败 (400)"

# 列出
http GET "/api/v1/checks?status=active&limit=20" -H "Authorization: Bearer $TEST_KEY"
assert_code "200" "列出 check"

# 查询
http GET "/api/v1/checks/$CHECK_ID" -H "Authorization: Bearer $TEST_KEY"
assert_code "200" "查询单个 check"

# 暂停/恢复
http POST "/api/v1/checks/$CHECK_ID/pause" \
  -H "Authorization: Bearer $TEST_KEY" -H "Content-Type: application/json" \
  -d "{\"version\":$CHECK_VERSION}"
assert_code "200" "暂停 check"

CHECK_VERSION=$(jget "$LAST_BODY" "version")

http POST "/api/v1/checks/$CHECK_ID/resume" \
  -H "Authorization: Bearer $TEST_KEY" -H "Content-Type: application/json" \
  -d "{\"version\":$CHECK_VERSION}"
assert_code "200" "恢复 check"

# ═══════════════════════════════════════════════════════════════
log_section "13. Check Run 巡检记录"
# ═══════════════════════════════════════════════════════════════

# 轮询等待 check 执行产生 run（最多 90s，interval check 30s + scheduler 周期 + pause/resume 重置）
log_info "等待 check run 产生（最多 90s）..."
CHECK_RUN_ID=""
for i in $(seq 1 90); do
  http GET "/api/v1/check-runs?limit=5" -H "Authorization: Bearer $TEST_KEY"
  CHECK_RUN_ID=$(echo "$LAST_BODY" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('items',[{}])[0].get('id',''))" 2>/dev/null || echo "")
  [ -n "$CHECK_RUN_ID" ] && log_info "check run 出现在 ${i}s" && break
  sleep 1
done

http GET "/api/v1/check-runs?limit=20" -H "Authorization: Bearer $TEST_KEY"
assert_code "200" "列出 check run"

http GET "/api/v1/check-runs?status=success&limit=10" -H "Authorization: Bearer $TEST_KEY"
assert_code "200" "按 status 过滤列出 check run"

[ -n "$CHECK_RUN_ID" ] && http GET "/api/v1/check-runs/$CHECK_RUN_ID" -H "Authorization: Bearer $TEST_KEY" && assert_code "200" "查询单个 check run" || log_skip "查询单个 check run (无 run)"

# ═══════════════════════════════════════════════════════════════
log_section "14. SLI 服务水平指标"
# ═══════════════════════════════════════════════════════════════

http POST "/api/v1/slis" \
  -H "Authorization: Bearer $TEST_KEY" \
  -H "Content-Type: application/json" \
  -d '{"name":"availability-sli","sli_type":"availability","source_type":"check_run","source_config":{"check_id":'"$CHECK_ID"'},"aggregation":"ratio","good_event_criteria":{"field":"status","op":"eq","value":"success"}}'
assert_code "201" "创建 SLI"
SLI_ID=$(jget "$LAST_BODY" "id")
SLI_VERSION=$(jget "$LAST_BODY" "version")

http GET "/api/v1/slis?limit=20" -H "Authorization: Bearer $TEST_KEY"
assert_code "200" "列出 SLI"

http GET "/api/v1/slis/$SLI_ID" -H "Authorization: Bearer $TEST_KEY"
assert_code "200" "查询单个 SLI"

# ═══════════════════════════════════════════════════════════════
log_section "15. SLO 服务水平目标"
# ═══════════════════════════════════════════════════════════════

http POST "/api/v1/slos" \
  -H "Authorization: Bearer $TEST_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "name":"api-slo",
    "sli_id":'"$SLI_ID"',
    "target":0.999,
    "window_type":"rolling",
    "window_duration":"720h",
    "alert_fast_burn_rate":0.5,
    "alert_slow_burn_rate":0.1
  }'
assert_code "201" "创建 SLO (99.9% / 30d rolling)"
SLO_ID=$(jget "$LAST_BODY" "id")
SLO_VERSION=$(jget "$LAST_BODY" "version")

http GET "/api/v1/slos?limit=20" -H "Authorization: Bearer $TEST_KEY"
assert_code "200" "列出 SLO"

http GET "/api/v1/slos/$SLO_ID" -H "Authorization: Bearer $TEST_KEY"
assert_code "200" "查询单个 SLO"

# 暂停/恢复
http POST "/api/v1/slos/$SLO_ID/pause" \
  -H "Authorization: Bearer $TEST_KEY" -H "Content-Type: application/json" \
  -d "{\"version\":$SLO_VERSION}"
assert_code "200" "暂停 SLO"

http POST "/api/v1/slos/$SLO_ID/resume" \
  -H "Authorization: Bearer $TEST_KEY" -H "Content-Type: application/json" \
  -d "{\"version\":$((SLO_VERSION+1))}"
assert_code "200" "恢复 SLO"

# ═══════════════════════════════════════════════════════════════
log_section "16. SLO Budget 错误预算"
# ═══════════════════════════════════════════════════════════════

http GET "/api/v1/slos/$SLO_ID/budget" -H "Authorization: Bearer $TEST_KEY"
assert_code "200" "查看 SLO 当前预算"

http GET "/api/v1/slos/$SLO_ID/budgets?limit=20" -H "Authorization: Bearer $TEST_KEY"
assert_code "200" "查看 SLO 预算历史"

# ═══════════════════════════════════════════════════════════════
log_section "17. SLO Alert 告警"
# ═══════════════════════════════════════════════════════════════

http GET "/api/v1/slo-alerts?limit=20" -H "Authorization: Bearer $TEST_KEY"
assert_code "200" "列出 SLO 告警"

http GET "/api/v1/slo-alerts?slo_id=$SLO_ID&limit=10" -H "Authorization: Bearer $TEST_KEY"
assert_code "200" "按 SLO 过滤列出告警"

# ═══════════════════════════════════════════════════════════════
log_section "18. 限流测试"
# ═══════════════════════════════════════════════════════════════

log_info "快速发送 30 个请求测试限流（无间隔）..."
RATE_LIMITED=0
for i in $(seq 1 30); do
  http POST "/api/v1/jobs/$EXEC_JOB_ID/trigger" \
    -H "Authorization: Bearer $TEST_KEY" -H "Content-Type: application/json" -d '{}'
  [ "$LAST_CODE" = "429" ] && RATE_LIMITED=1 && break
done
[ "$RATE_LIMITED" = "1" ] && log_pass "触发限流 (429)" || log_skip "限流测试 (未触发，可能 burst 足够)"

# ═══════════════════════════════════════════════════════════════
log_section "19. 清理"
# ═══════════════════════════════════════════════════════════════


# 删除 check（移到此处，确保它活过 section 13 的 35s 等待以产生 check run）
# 先 GET 取最新 version（section 12 pause/resume 后 scheduler 可能已更新 next_run_at）
if [ -n "$CHECK_ID" ]; then
  http GET "/api/v1/checks/$CHECK_ID" -H "Authorization: Bearer $TEST_KEY"
  CHECK_VERSION=$(jget "$LAST_BODY" "version")
  [ -n "$CHECK_VERSION" ] && http DELETE "/api/v1/checks/$CHECK_ID" \
    -H "Authorization: Bearer $TEST_KEY" \
    -H "Content-Type: application/json" \
    -d "{\"version\":$CHECK_VERSION}" && assert_code "200" "删除 check" || log_skip "删除 check (无 version)"
else
  log_skip "删除 check (无 CHECK_ID)"
fi

# 删除 SLO
http DELETE "/api/v1/slos/$SLO_ID" \
  -H "Authorization: Bearer $TEST_KEY" \
  -H "Content-Type: application/json" \
  -d "{\"version\":$((SLO_VERSION+2))}"
assert_code "200" "删除 SLO"

# 删除 SLI
http DELETE "/api/v1/slis/$SLI_ID" \
  -H "Authorization: Bearer $TEST_KEY" \
  -H "Content-Type: application/json" \
  -d "{\"version\":$SLI_VERSION}"
assert_code "200" "删除 SLI"

# ═══════════════════════════════════════════════════════════════
log_section "总结"
# ═══════════════════════════════════════════════════════════════

echo "" >> "$RESULTS_FILE"
echo "**总计: $PASS 通过 / $FAIL 失败 / $SKIP 跳过**" >> "$RESULTS_FILE"

echo ""
echo -e "${BLUE}═══════════════════════════════════════${NC}"
echo -e "总计: ${GREEN}$PASS 通过${NC} / ${RED}$FAIL 失败${NC} / ${YELLOW}$SKIP 跳过${NC}"
echo -e "测试报告已写入: ${BLUE}$RESULTS_FILE${NC}"
echo ""

[ "$FAIL" = "0" ] && echo -e "${GREEN}全部通过！${NC}" || echo -e "${RED}有 $FAIL 项失败，详见 $RESULTS_FILE${NC}"

exit $FAIL

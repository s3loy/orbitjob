#!/usr/bin/env bash
# OrbitJob Job Dataset Importer
# 从 job-dataset.json 批量创建 job 到目标租户
#
# 用法:
#   ./scripts/import-jobs.sh [API_BASE_URL] [DATASET_FILE]
#
# 默认:
#   API_BASE_URL  = http://localhost:18080
#   DATASET_FILE  = ./scripts/job-dataset.json
#
# 环境变量:
#   ADMIN_BOOTSTRAP_API_KEY  — bootstrap key（默认 otj_devkey_2026）
#   ORBITJOB_TENANT_SLUG     — 目标租户 slug（默认 devops-team）
#   ORBITJOB_TENANT_NAME     — 目标租户名称（默认 "DevOps Team (Load Test)"）
#
# 幂等：租户已存在则复用；所有 job 创建失败不中断，汇总报结果。

set -euo pipefail

API="${1:-http://localhost:18080}"
DATASET="${2:-$(dirname "$0")/job-dataset.json}"
BOOTSTRAP_KEY="${ADMIN_BOOTSTRAP_API_KEY:-otj_devkey_2026}"
TENANT_SLUG="${ORBITJOB_TENANT_SLUG:-devops-team}"
TENANT_NAME="${ORBITJOB_TENANT_NAME:-DevOps Team (Load Test)}"

GREEN='\033[0;32m'; RED='\033[0;31m'; YELLOW='\033[1;33m'; NC='\033[0m'

log_info()  { echo -e "[INFO] $1"; }
log_pass()  { echo -e "${GREEN}[OK]${NC} $1"; }
log_fail()  { echo -e "${RED}[FAIL]${NC} $1"; }
log_warn()  { echo -e "${YELLOW}[WARN]${NC} $1"; }

# ── 检查依赖 ──
for cmd in curl python3; do
  command -v "$cmd" >/dev/null || { echo "Missing: $cmd" >&2; exit 1; }
done

if [ ! -f "$DATASET" ]; then
  echo "Dataset file not found: $DATASET" >&2
  exit 1
fi

JOB_COUNT=$(python3 -c "import json; print(len(json.load(open('$DATASET'))))")
log_info "Dataset: $DATASET ($JOB_COUNT jobs)"
log_info "API: $API"
log_info "Tenant: $TENANT_SLUG"

# ── 等待 API 就绪 ──
log_info "Waiting for API..."
for i in $(seq 1 30); do
  curl -sf "$API/healthz" >/dev/null 2>&1 && break
  [ "$i" = "30" ] && { log_fail "API not ready after 30s"; exit 1; }
  sleep 1
done
log_pass "API ready"

# ── 创建/获取租户 ──
RESP=$(curl -s -w "\n%{http_code}" -X POST "$API/api/v1/tenants" \
  -H "Authorization: Bearer $BOOTSTRAP_KEY" \
  -H "Content-Type: application/json" \
  -d "{\"slug\":\"$TENANT_SLUG\",\"name\":\"$TENANT_NAME\",\"status\":\"active\"}")

HTTP_CODE=$(echo "$RESP" | tail -1)
BODY=$(echo "$RESP" | sed '$d')

if [ "$HTTP_CODE" = "201" ] || [ "$HTTP_CODE" = "200" ]; then
  TENANT_ID=$(echo "$BODY" | python3 -c "import sys,json; print(json.load(sys.stdin).get('id',''))" 2>/dev/null || echo "")
fi

# 409 → tenant exists, fetch by slug
if [ -z "${TENANT_ID:-}" ]; then
  TENANT_ID=$(curl -s "$API/api/v1/tenants?slug=$TENANT_SLUG" \
    -H "Authorization: Bearer $BOOTSTRAP_KEY" | \
    python3 -c "import sys,json; print(json.load(sys.stdin)['items'][0]['id'])" 2>/dev/null || echo "")
fi

if [ -z "${TENANT_ID:-}" ]; then
  log_fail "Cannot create or find tenant '$TENANT_SLUG'"
  exit 1
fi
log_info "Tenant ID: $TENANT_ID"

# ── 创建 API key ──
KEY_RESP=$(curl -s -X POST "$API/api/v1/tenants/$TENANT_ID/api_keys" \
  -H "Authorization: Bearer $BOOTSTRAP_KEY" \
  -H "Content-Type: application/json" -d '{}')
TENANT_KEY=$(echo "$KEY_RESP" | python3 -c "import sys,json; print(json.load(sys.stdin).get('key',''))" 2>/dev/null || echo "")

if [ -z "$TENANT_KEY" ]; then
  log_warn "Cannot create API key, trying to list existing..."
  KEY_LIST=$(curl -s "$API/api/v1/tenants/$TENANT_ID/api_keys" -H "Authorization: Bearer $BOOTSTRAP_KEY")
  # Keys returned via list don't include the secret — use bootstrap key as fallback
  TENANT_KEY="$BOOTSTRAP_KEY"
  log_info "Falling back to bootstrap key for import"
else
  log_info "Key: ${TENANT_KEY:0:12}..."
fi

# ── 批量导入 ──
log_info "Importing $JOB_COUNT jobs..."

python3 -c "
import json, subprocess, sys

api = '$API'
key = '$TENANT_KEY'
ok = 0
fail = 0
skipped = 0

with open('$DATASET') as f:
    jobs = json.load(f)

for j in jobs:
    body = json.dumps(j)
    r = subprocess.run(
        ['curl', '-s', '-w', '%{http_code}', '-X', 'POST', f'{api}/api/v1/jobs',
         '-H', f'Authorization: Bearer {key}',
         '-H', 'Content-Type: application/json', '-d', body],
        capture_output=True, text=True)
    out = r.stdout.strip()
    code = out[-3:] if len(out) >= 3 else '???'
    name = j['name']

    if code == '201':
        print(f'\033[32m[OK]\033[0m {name}')
        ok += 1
    elif code == '409':
        print(f'\033[33m[SKIP]\033[0m {name} (already exists)')
        skipped += 1
    else:
        msg = out[:-3] if len(out) > 3 else out
        print(f'\033[31m[FAIL]\033[0m {name} ({code}): {msg[:100]}')
        fail += 1

print()
print(f'Imported: {ok} OK / {skipped} skipped / {fail} FAIL')

# Summary breakdown
print()
print('Breakdown:')
handler_types = {}
trigger_types = {}
cron_exprs = []
for j in jobs:
    ht = j['handler_type']
    tt = j['trigger_type']
    handler_types[ht] = handler_types.get(ht, 0) + 1
    trigger_types[tt] = trigger_types.get(tt, 0) + 1
    if 'cron_expr' in j:
        cron_exprs.append(j['cron_expr'])

print('  By handler:', ', '.join(f'{k}={v}' for k, v in sorted(handler_types.items())))
print('  By trigger:', ', '.join(f'{k}={v}' for k, v in sorted(trigger_types.items())))
print(f'  Cron expressions: {len(cron_exprs)} ({len(set(cron_exprs))} unique)')
print(f'  Manual jobs: {trigger_types.get(\"manual\", 0)}')

sys.exit(0 if fail == 0 else 1)
"

exit $?

#!/usr/bin/env bash
# OrbitJob 真实负载种子数据 — 50 个 DevOps 场景 job
# 用法: ./scripts/seed-jobs.sh [API_BASE_URL]
set -euo pipefail
API="${1:-http://localhost:18080}"
BOOTSTRAP_KEY="${ADMIN_BOOTSTRAP_API_KEY:-otj_devkey_2026}"

GREEN='\033[0;32m'; RED='\033[0;31m'; NC='\033[0m'

# ── 创建租户 + key ──
TENANT_RESP=$(curl -s -w "\n%{http_code}" -X POST "$API/api/v1/tenants" \
  -H "Authorization: Bearer $BOOTSTRAP_KEY" \
  -H "Content-Type: application/json" \
  -d '{"slug":"devops-team","name":"DevOps Team (Load Test)","status":"active"}')
TENANT_ID=$(echo "$TENANT_RESP" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d['id'])" 2>/dev/null || echo "")

if [ -z "$TENANT_ID" ]; then
  TENANT_ID=$(curl -s "$API/api/v1/tenants?slug=devops-team" -H "Authorization: Bearer $BOOTSTRAP_KEY" | python3 -c "import sys,json; print(json.load(sys.stdin)['items'][0]['id'])" 2>/dev/null)
fi

KEY_RESP=$(curl -s -X POST "$API/api/v1/tenants/$TENANT_ID/api_keys" \
  -H "Authorization: Bearer $BOOTSTRAP_KEY" -H "Content-Type: application/json" -d '{}')
TEST_KEY=$(echo "$KEY_RESP" | python3 -c "import sys,json; print(json.load(sys.stdin)['key'])")
echo -e "[INFO] Tenant: $TENANT_ID  Key: ${TEST_KEY:0:12}..."

# ── 用 python3 构建 job payload 并 curl 创建 ──
python3 -c "
import json, subprocess, sys

api = '$API'
key = '$TEST_KEY'
ok = 0
fail = 0

def create(name, trigger, handler, payload, timeout=60, retry=0, cron='', concurrency='forbid', misfire='fire_now'):
    global ok, fail
    body = {
        'name': name, 'trigger_type': trigger, 'handler_type': handler,
        'handler_payload': payload, 'timeout_sec': timeout, 'retry_limit': retry,
    }
    if trigger == 'cron':
        body['cron_expr'] = cron
        body['concurrency_policy'] = concurrency
        body['misfire_policy'] = misfire

    j = json.dumps(body)
    r = subprocess.run(['curl', '-s', '-w', '%{http_code}', '-X', 'POST', f'{api}/api/v1/jobs',
        '-H', f'Authorization: Bearer {key}',
        '-H', 'Content-Type: application/json', '-d', j],
        capture_output=True, text=True)
    out = r.stdout.strip()
    code = out[-3:] if len(out) >= 3 else '???'
    if code == '201':
        print(f'\033[32m[OK]\033[0m {name}')
        ok += 1
    else:
        msg = out[:-3] if len(out) > 3 else out
        print(f'\033[31m[FAIL]\033[0m {name} ({code}): {msg[:80]}')
        fail += 1

# ═══════════════════ 50 个真实 DevOps Job ═══════════════════

# ── 1. 数据管道 (10 exec jobs) ──
for name, cron, timeout, retry in [
    ('etl-daily-user-sync', '0 3 * * *', 120, 3),
    ('etl-hourly-metrics-rollup', '0 * * * *', 60, 2),
    ('etl-warehouse-export', '0 6 * * 1-5', 300, 1),
    ('data-retention-cleanup', '0 2 * * 0', 600, 0),
    ('data-partition-create', '0 0 1 * *', 180, 2),
    ('log-archive-compress', '0 4 * * 0', 900, 0),
    ('cache-warmup-daily', '30 7 * * 1-5', 120, 1),
    ('search-index-rebuild', '0 1 * * 0', 1800, 1),
    ('audit-log-export', '0 8 * * 1-5', 300, 2),
    ('replica-lag-check', '*/10 * * * *', 30, 0),
]:
    create(name, 'cron', 'exec',
           {'command': 'echo', 'args': [f'executing {name} at $(date -Iseconds)']},
           timeout=timeout, retry=retry, cron=cron)

# ── 2. 健康检查 (8 http jobs) ──
for name, cron, url in [
    ('health-api-gateway', '*/5 * * * *', 'https://httpbin.org/get'),
    ('health-db-primary', '*/2 * * * *', 'https://httpbin.org/status/200'),
    ('health-redis-cluster', '*/5 * * * *', 'https://httpbin.org/delay/1'),
    ('health-kafka-brokers', '*/5 * * * *', 'https://httpbin.org/get'),
    ('health-cdn-endpoints', '*/10 * * * *', 'https://httpbin.org/get'),
    ('health-s3-bucket-access', '0 */2 * * *', 'https://httpbin.org/status/200'),
    ('health-smtp-relay', '*/15 * * * *', 'https://httpbin.org/get'),
    ('health-elasticsearch', '*/5 * * * *', 'https://httpbin.org/get'),
]:
    create(name, 'cron', 'http',
           {'url': url, 'method': 'GET', 'timeout_sec': 15},
           timeout=30, retry=0, cron=cron)

# ── 3. 监控告警 (6 exec+http mix) ──
create('monitor-error-budget-burn', 'cron', 'exec',
       {'command': 'curl', 'args': ['-sf', 'https://httpbin.org/get']},
       timeout=30, cron='*/5 * * * *', retry=0)
create('monitor-ssl-cert-expiry', 'cron', 'exec',
       {'command': 'openssl', 'args': ['s_client', '-connect', 'httpbin.org:443', '-servername', 'httpbin.org', '</dev/null']},
       timeout=60, cron='0 9 * * 1', retry=0)
create('monitor-disk-usage-alert', 'cron', 'exec',
       {'command': 'df', 'args': ['-h']},
       timeout=30, cron='0 */4 * * *', retry=0)
create('monitor-quota-exceeded', 'cron', 'exec',
       {'command': 'find', 'args': ['/tmp', '-type', 'f', '-size', '+100M', '-ls']},
       timeout=20, cron='*/10 * * * *', retry=0)
create('monitor-slo-window-check', 'cron', 'exec',
       {'command': 'curl', 'args': ['-sf', 'https://httpbin.org/json']},
       timeout=45, cron='0 */6 * * *', retry=0)
create('monitor-cost-anomaly-daily', 'cron', 'http',
       {'url': 'https://httpbin.org/post', 'method': 'POST', 'body': '{\"check\":\"cost-anomaly\"}'},
       timeout=120, retry=1, cron='0 10 * * *')

# ── 4. 备份 (5 exec jobs) ──
for name, cron, timeout, retry, cmd in [
    ('backup-db-full-daily', '0 1 * * *', 3600, 1, 'pg_dump'),
    ('backup-db-incremental', '0 */4 * * *', 600, 1, 'pg_dump'),
    ('backup-config-snapshot', '0 2 * * 0', 300, 0, 'tar'),
    ('disaster-recovery-drill', '0 10 1 */3 *', 1800, 0, 'bash'),
    ('backup-verify-restore', '0 5 * * 0', 1200, 1, 'pg_restore'),
]:
    create(name, 'cron', 'exec',
           {'command': cmd, 'args': ['--version']},
           timeout=timeout, retry=retry, cron=cron)

# ── 5. 报表 (6 webhook jobs) ──
for name, cron in [
    ('report-daily-business-kpi', '0 8 * * 1-5'),
    ('report-weekly-sre-summary', '0 9 * * 1'),
    ('report-monthly-billing', '0 7 1 * *'),
    ('report-user-growth-analysis', '0 6 * * 1'),
    ('report-sla-compliance', '0 10 1 * *'),
    ('report-cost-attribution', '0 5 2 * *'),
]:
    create(name, 'cron', 'webhook',
           {'url': 'https://httpbin.org/post', 'method': 'POST',
            'body': json.dumps({'report': name, 'generated_by': 'orbitjob'}),
            'headers': {'Content-Type': 'application/json'}},
           timeout=300, retry=2, cron=cron)

# ── 6. 通知 (8 webhook jobs) ──
for name, cron in [
    ('notify-deploy-success', '0 * * * *'),
    ('notify-incident-escalation', '*/5 * * * *'),
    ('notify-sprint-reminder', '0 9 * * 1'),
    ('notify-oncall-handoff', '0 8,20 * * *'),
    ('notify-release-notes', '0 17 * * 5'),
    ('notify-uptime-daily', '0 9 * * *'),
    ('notify-cert-expiry-alert', '0 9 * * 1'),
    ('notify-budget-threshold', '*/10 * * * *'),
]:
    create(name, 'cron', 'webhook',
           {'url': 'https://httpbin.org/post', 'method': 'POST',
            'body': json.dumps({'event': name, 'timestamp': '2026-01-01T00:00:00Z'}),
            'headers': {'Content-Type': 'application/json'}, 'secret': 'whsec_seed'},
           timeout=30, cron=cron, retry=0)

# ── 7. 数据库维护 (4 exec jobs) ──
create('db-vacuum-analyze', 'cron', 'exec',
       {'command': 'psql', 'args': ['-c', 'VACUUM ANALYZE']},
       timeout=1800, cron='0 3 * * 0', retry=0, misfire='skip')
create('db-reindex-weekly', 'cron', 'exec',
       {'command': 'psql', 'args': ['-c', 'REINDEX DATABASE current']},
       timeout=3600, cron='0 4 * * 0', retry=0, misfire='skip')
create('db-connection-pool-reset', 'cron', 'exec',
       {'command': 'kill', 'args': ['-HUP', '1']},
       timeout=60, retry=1, cron='0 */6 * * *')
create('db-slow-query-log-rotate', 'cron', 'exec',
       {'command': 'mv', 'args': ['/var/log/pg/slow.log', '/var/log/pg/slow.log.1']},
       timeout=120, cron='0 1 * * *', retry=0)

# ── 8. CI/CD (3 http jobs) ──
for name, cron, method in [
    ('cicd-nightly-build', '0 3 * * *', 'POST'),
    ('cicd-security-scan', '0 4 * * 0', 'POST'),
    ('cicd-dependency-update', '0 2 * * 1', 'POST'),
]:
    create(name, 'cron', 'http',
           {'url': 'https://httpbin.org/post', 'method': method,
            'body': json.dumps({'pipeline': name, 'ref': 'main'}),
            'headers': {'Content-Type': 'application/json'}},
           timeout=900, retry=2, cron=cron)

# ── 手动触发 job (2 个，给手动测试用) ──
create('manual-emergency-rollback', 'manual', 'exec',
       {'command': 'echo', 'args': ['rollback initiated']}, timeout=60)
create('manual-data-fix', 'manual', 'http',
       {'url': 'https://httpbin.org/post', 'method': 'POST',
        'body': '{\"action\":\"fix-orphan-records\"}',
        'headers': {'Content-Type': 'application/json'}}, timeout=300)

print(f'\\nDone: {ok} OK / {fail} FAIL')
" 2>&1
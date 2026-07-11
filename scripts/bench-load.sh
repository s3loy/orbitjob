#!/usr/bin/env bash
# OrbitJob 调度性能负载测试
# 量化：创建吞吐、触发延迟、分发延迟、规模退化曲线
#
# 用法:
#   ./scripts/bench-load.sh [API] [SCALE]
#
#   SCALE: small(100) | medium(500) | large(2000) | xlarge(5000)
#   默认: small
#
# 环境变量:
#   ADMIN_BOOTSTRAP_API_KEY — bootstrap key
#   ITERATIONS              — 重复次数（取中位数），默认 1

set -euo pipefail
API="${1:-http://localhost:18080}"
SCALE="${2:-small}"
BOOTSTRAP_KEY="${ADMIN_BOOTSTRAP_API_KEY:-otj_devkey_2026}"
ITERATIONS="${ITERATIONS:-1}"

case "$SCALE" in
  small)  JOB_COUNT=100  BATCH=10  ;;
  medium) JOB_COUNT=500  BATCH=25  ;;
  large)  JOB_COUNT=2000 BATCH=50  ;;
  xlarge) JOB_COUNT=5000 BATCH=100 ;;
  *) echo "Unknown scale: $SCALE (use small|medium|large|xlarge)" >&2; exit 1 ;;
esac

GREEN='\033[0;32m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'; NC='\033[0m'
PASS=0

bench_header()  { echo -e "\n${CYAN}═══ $1 ═══${NC}"; }
bench_metric()  { printf "  ${GREEN}%-35s${NC} %s\n" "$1" "$2"; }

# ── 工具：计时器 ──
timer_start() { date +%s%3N; }
timer_end() { echo $(( $(date +%s%3N) - $1 )); }

# ── 创建测试租户 ──
SLUG="bench-$(date +%H%M%S)"

bench_header "0. 环境准备"

T=$(timer_start)
RESP=$(curl -s -w "\n%{http_code}" -X POST "$API/api/v1/tenants" \
  -H "Authorization: Bearer $BOOTSTRAP_KEY" \
  -H "Content-Type: application/json" \
  -d "{\"slug\":\"$SLUG\",\"name\":\"Benchmark $SCALE\",\"status\":\"active\"}")
TENANT_ID=$(echo "$RESP" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('id',''))" 2>/dev/null || echo "")
[ -z "$TENANT_ID" ] && TENANT_ID=$(curl -s "$API/api/v1/tenants?slug=$SLUG" -H "Authorization: Bearer $BOOTSTRAP_KEY" | python3 -c "import sys,json; print(json.load(sys.stdin)['items'][0]['id'])")
bench_metric "tenant ready" "$(timer_end $T)ms"

KEY=$(curl -s -X POST "$API/api/v1/tenants/$TENANT_ID/api_keys" \
  -H "Authorization: Bearer $BOOTSTRAP_KEY" -H "Content-Type: application/json" -d '{}' | \
  python3 -c "import sys,json; print(json.load(sys.stdin)['key'])")
bench_metric "API key" "${KEY:0:12}..."

# ── 1. 批量创建吞吐量 ──
bench_header "1. 批量创建 $JOB_COUNT jobs (batch=$BATCH)"

create_latencies=()
TOTAL_START=$(timer_start)

for ((i=0; i<JOB_COUNT; i+=BATCH)); do
  # 构建 batch 个 job 的 JSON 数组
  PAYLOAD=$(python3 -c "
import json
jobs = []
for n in range($i, min($i+$BATCH, $JOB_COUNT)):
    jobs.append({
        'name': f'bench-{n:05d}',
        'trigger_type': 'manual',
        'handler_type': 'exec',
        'handler_payload': {'command': 'echo', 'args': [f'job-{n}']},
        'timeout_sec': 30,
        'retry_limit': 0,
    })
print(json.dumps(jobs))
")

  for job_json in $(echo "$PAYLOAD" | python3 -c "import sys,json; [print(json.dumps(j)) for j in json.load(sys.stdin)]"); do
    T0=$(timer_start)
    CODE=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$API/api/v1/jobs" \
      -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" -d "$job_json" 2>/dev/null)
    LAT=$(timer_end $T0)
    create_latencies+=($LAT)
  done

  # progress
  DONE=$((i+BATCH > JOB_COUNT ? JOB_COUNT : i+BATCH))
  printf "\r  Created %d/%d ..." $DONE $JOB_COUNT
done

TOTAL_MS=$(timer_end $TOTAL_START)
TOTAL_SEC=$(echo "scale=2; $TOTAL_MS / 1000" | bc)
TPS=$(echo "scale=1; $JOB_COUNT / $TOTAL_SEC" | bc)

echo ""
bench_metric "total time"           "${TOTAL_SEC}s"
bench_metric "throughput"           "${TPS} jobs/sec"
bench_metric "avg create latency"   "$(python3 -c "import sys; l=[$((IFS=,; echo "${create_latencies[*]}"))]; print(f'{sum(l)/len(l):.1f}ms')" 2>/dev/null || echo "N/A")"
bench_metric "P50 create latency"   "$(python3 -c "import sys; l=sorted([$((IFS=,; echo "${create_latencies[*]}"))]); print(f'{l[len(l)//2]}ms')" 2>/dev/null || echo "N/A")"
bench_metric "P95 create latency"   "$(python3 -c "import sys; l=sorted([$((IFS=,; echo "${create_latencies[*]}"))]); print(f'{l[int(len(l)*0.95)]}ms')" 2>/dev/null || echo "N/A")"

# ── 2. 触发延迟 ──
bench_header "2. 触发延迟采样 (sample=100)"

TRIGGER_LATENCIES=()
SAMPLE=$((JOB_COUNT < 100 ? JOB_COUNT : 100))
JOB_IDS=$(curl -s "$API/api/v1/jobs?limit=$SAMPLE" -H "Authorization: Bearer $KEY" | \
  python3 -c "import sys,json; print(','.join(str(i['id']) for i in json.load(sys.stdin)['items']))")

for jid in $(echo $JOB_IDS | tr ',' ' '); do
  T0=$(timer_start)
  RUN_ID=$(curl -s -X POST "$API/api/v1/jobs/$jid/trigger" \
    -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" -d '{}' | \
    python3 -c "import sys,json; print(json.load(sys.stdin).get('run_id',''))" 2>/dev/null || echo "")
  LAT=$(timer_end $T0)
  [ -n "$RUN_ID" ] && TRIGGER_LATENCIES+=($LAT)
done

[ ${#TRIGGER_LATENCIES[@]} -gt 0 ] && {
  python3 -c "
l = sorted([$((IFS=,; echo "${TRIGGER_LATENCIES[*]}"))])
n = len(l)
if n > 0:
    print(f'  samples: {n}')
    print(f'  mean:    {sum(l)/n:.1f}ms')
    print(f'  P50:     {l[n//2]}ms')
    print(f'  P95:     {l[int(n*0.95)]}ms')
    print(f'  P99:     {l[int(n*0.99)]}ms')
    print(f'  min:     {l[0]}ms')
    print(f'  max:     {l[-1]}ms')
" || bench_metric "trigger latency" "N/A"
}

# ── 3. 分发延迟（trigger → dispatch → worker claim） ──
bench_header "3. trigger→dispatch 端到端延迟"

# 等 worker/dispatcher 处理一轮
sleep 5

# 检查有多少 instances 已经从 pending 变为其他状态
STATS=$(curl -s "$API/api/v1/instances?limit=1" -H "Authorization: Bearer $KEY" | \
  python3 -c "import sys,json; print(json.load(sys.stdin).get('items',[]))" 2>/dev/null)
PENDING=$(curl -s "$API/api/v1/instances?status=pending&limit=1" -H "Authorization: Bearer $KEY" | \
  python3 -c "import sys,json; print(json.load(sys.stdin).get('total',0))" 2>/dev/null || echo "0")
RUNNING=$(curl -s "$API/api/v1/instances?status=running&limit=1" -H "Authorization: Bearer $KEY" | \
  python3 -c "import sys,json; print(json.load(sys.stdin).get('total',0))" 2>/dev/null || echo "0")
SUCCESS=$(curl -s "$API/api/v1/instances?status=success&limit=1" -H "Authorization: Bearer $KEY" | \
  python3 -c "import sys,json; print(json.load(sys.stdin).get('total',0))" 2>/dev/null || echo "0")
FAILED=$(curl -s "$API/api/v1/instances?status=failed&limit=1" -H "Authorization: Bearer $KEY" | \
  python3 -c "import sys,json; print(json.load(sys.stdin).get('total',0))" 2>/dev/null || echo "0")

TOTAL_INST=$((PENDING + RUNNING + SUCCESS + FAILED))
bench_metric "total instances"     "$TOTAL_INST"
bench_metric "pending"             "$PENDING"
bench_metric "running"             "$RUNNING"
bench_metric "success"             "$SUCCESS"
bench_metric "failed"              "$FAILED"
[ "$TOTAL_INST" -gt 0 ] && {
  DISPATCH_RATE=$(echo "scale=1; ($RUNNING + $SUCCESS + $FAILED) * 100 / $TOTAL_INST" | bc)
  bench_metric "dispatch rate"     "${DISPATCH_RATE}%"
}

# ── 4. 并发租户隔离 ──
bench_header "4. 多租户并发"

# 创建第二个租户 + 快速触发
SLUG2="bench2-$(date +%H%M%S)"
T2=$(curl -s -X POST "$API/api/v1/tenants" \
  -H "Authorization: Bearer $BOOTSTRAP_KEY" -H "Content-Type: application/json" \
  -d "{\"slug\":\"$SLUG2\",\"name\":\"Bench2\",\"status\":\"active\"}" | \
  python3 -c "import sys,json; print(json.load(sys.stdin).get('id',''))" 2>/dev/null)
K2=$(curl -s -X POST "$API/api/v1/tenants/$T2/api_keys" \
  -H "Authorization: Bearer $BOOTSTRAP_KEY" -H "Content-Type: application/json" -d '{}' | \
  python3 -c "import sys,json; print(json.load(sys.stdin)['key'])")

# 两个租户同时各创建 20 个 job 并触发
CONCURRENT_START=$(timer_start)
for i in $(seq 1 20); do
  curl -s -o /dev/null -X POST "$API/api/v1/jobs" \
    -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" \
    -d "{\"name\":\"concurrent-a-$i\",\"trigger_type\":\"manual\",\"handler_type\":\"exec\",\"handler_payload\":{\"command\":\"echo\"},\"timeout_sec\":30}" &
  curl -s -o /dev/null -X POST "$API/api/v1/jobs" \
    -H "Authorization: Bearer $K2" -H "Content-Type: application/json" \
    -d "{\"name\":\"concurrent-b-$i\",\"trigger_type\":\"manual\",\"handler_type\":\"exec\",\"handler_payload\":{\"command\":\"echo\"},\"timeout_sec\":30}" &
done
wait
CONCURRENT_MS=$(timer_end $CONCURRENT_START)
bench_metric "2 tenants × 20 jobs" "${CONCURRENT_MS}ms ($(echo "scale=1; 40 / ($CONCURRENT_MS / 1000)" | bc) jobs/sec)"

# ── 5. 持久化查询性能 ──
bench_header "5. 数据库查询性能"

T0=$(timer_start)
curl -s -o /dev/null "$API/api/v1/jobs?limit=100" -H "Authorization: Bearer $KEY"
JOB_LIST_MS=$(timer_end $T0)

T0=$(timer_start)
curl -s -o /dev/null "$API/api/v1/instances?limit=100" -H "Authorization: Bearer $KEY"
INST_LIST_MS=$(timer_end $T0)

T0=$(timer_start)
curl -s -o /dev/null "$API/api/v1/jobs?limit=100&status=active" -H "Authorization: Bearer $KEY"
FILTER_MS=$(timer_end $T0)

bench_metric "list 100 jobs"       "${JOB_LIST_MS}ms"
bench_metric "list 100 instances"  "${INST_LIST_MS}ms"
bench_metric "filter by status"    "${FILTER_MS}ms"

# ── 总结 ──
bench_header "Summary: $SCALE ($JOB_COUNT jobs)"

echo -e "  scale        ${GREEN}$SCALE${NC}"
echo -e "  jobs created ${GREEN}$JOB_COUNT${NC}"
echo -e "  throughput   ${GREEN}${TPS} jobs/sec${NC}"
echo -e "  instances    ${GREEN}$TOTAL_INST${NC} (${DISPATCH_RATE:-0}% dispatched)"
echo -e "  trigger P50  ${GREEN}$(python3 -c "l=sorted([$((IFS=,; echo "${TRIGGER_LATENCIES[*]:-0}"))]); print(f'{l[len(l)//2]}ms')" 2>/dev/null || echo "N/A")${NC}"
echo ""

# cleanup
curl -s -o /dev/null -X DELETE "$API/api/v1/tenants/$T2" -H "Authorization: Bearer $BOOTSTRAP_KEY" -H "Content-Type: application/json" -d '{"version":1}' 2>/dev/null || true

#!/usr/bin/env bash
set -euo pipefail

compose=(docker compose)
deadline=$((SECONDS + ${ORBITJOB_START_TIMEOUT_SEC:-180}))

while (( SECONDS < deadline )); do
  migrate=$(${compose[@]} ps -a --format json migrate 2>/dev/null | python3 -c 'import json,sys; s=sys.stdin.read().strip(); print((json.loads(s) if s else {}).get("State",""))' 2>/dev/null || true)
  bootstrap=$(${compose[@]} ps -a --format json bootstrap 2>/dev/null | python3 -c 'import json,sys; s=sys.stdin.read().strip(); print((json.loads(s) if s else {}).get("State",""))' 2>/dev/null || true)
  if [[ "$migrate" == "exited" && "$bootstrap" == "exited" ]]; then
    if ${compose[@]} ps --status running --format '{{.Service}} {{.Health}}' | grep -Eq '^admin-api healthy$' && \
       ${compose[@]} ps --status running --format '{{.Service}} {{.Health}}' | grep -Eq '^scheduler healthy$' && \
       ${compose[@]} ps --status running --format '{{.Service}} {{.Health}}' | grep -Eq '^dispatcher healthy$' && \
       ${compose[@]} ps --status running --format '{{.Service}} {{.Health}}' | grep -Eq '^worker healthy$' && \
       curl -fsS http://127.0.0.1:8080/healthz >/dev/null 2>&1 && \
       curl -fsS http://127.0.0.1:9090/-/ready >/dev/null 2>&1 && \
       curl -fsS http://127.0.0.1:3000/api/health >/dev/null 2>&1; then
      exit 0
    fi
  fi
  sleep 2
done

echo "OrbitJob did not become ready within the timeout." >&2
${compose[@]} ps -a >&2
exit 1

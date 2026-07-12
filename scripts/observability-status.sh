#!/usr/bin/env bash
set -euo pipefail

curl -fsS http://localhost:9090/-/ready >/dev/null
curl -fsS http://localhost:3000/api/health >/dev/null

python3 - <<'PY'
import json, urllib.request
url = 'http://localhost:9090/api/v1/targets'
with urllib.request.urlopen(url, timeout=5) as r:
    data = json.load(r)
targets = [t for t in data['data']['activeTargets'] if t['labels'].get('job') in {'admin-api','scheduler','dispatcher','worker'}]
bad = [t for t in targets if t.get('health') != 'up']
if len(targets) != 4 or bad:
    raise SystemExit(f'Prometheus targets unhealthy: found={len(targets)} bad={len(bad)}')
print('Prometheus: ready; 4 OrbitJob targets up')
PY

user="${GRAFANA_USER:-admin}"
password="${GRAFANA_PASSWORD:?GRAFANA_PASSWORD is required}"
curl -fsS -u "$user:$password" http://localhost:3000/api/datasources/uid/prometheus >/dev/null
curl -fsS -u "$user:$password" http://localhost:3000/api/search?query=OrbitJob | python3 -c 'import json,sys; d=json.load(sys.stdin); assert d, "OrbitJob dashboard not found"'
echo "Grafana: healthy; Prometheus datasource and OrbitJob dashboard loaded"

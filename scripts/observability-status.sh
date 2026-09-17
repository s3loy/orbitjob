#!/usr/bin/env bash
# Diagnose the OrbitJob observability stack against the kube-prometheus-stack
# release and a running Prometheus.
#
# Failing loudly is the point: when the stack is missing or Prometheus is not
# reachable there is nothing to diagnose, so the script says so and exits
# non-zero rather than reporting a healthy stack that is not there.
#
# Prerequisites, in order:
#   1. make monitoring-up        (installs the stack; this script cannot)
#   2. kubectl port-forward -n monitoring svc/kube-prometheus-stack-prometheus 9090:9090
#   3. optionally kubectl port-forward -n monitoring svc/kube-prometheus-stack-grafana 3000:80
#      (the dashboard check degrades to a WARN without it)
set -euo pipefail

fail() { echo "[FAIL] $1" >&2; exit 1; }
warn() { echo "[WARN] $1" >&2; }

RELEASE=kube-prometheus-stack
NAMESPACE=monitoring
DASHBOARD_UID=orbitjob-run-ledger

# 1. The release itself: everything below assumes it exists.
if helm status "$RELEASE" -n "$NAMESPACE" >/dev/null 2>&1; then
  echo "[OK] Helm release: $RELEASE present in namespace $NAMESPACE"
else
  fail "Helm release: $RELEASE not found in namespace $NAMESPACE — run make monitoring-up first"
fi

# 2. The long-lived TSDB promise: Prometheus persists on a PVC, not an
# emptyDir. A missing claim means the stack runs but loses history on restart.
if kubectl -n "$NAMESPACE" get pvc 2>/dev/null | grep -q "kube-prometheus-stack-prometheus-db"; then
  echo "[OK] Prometheus TSDB persists on a PVC in $NAMESPACE"
else
  warn "Prometheus TSDB: no PVC found — history will not survive a pod restart (R-b violated?)"
fi

# 3. Prometheus reachable through the documented port-forward.
if curl -fsS --max-time 5 http://localhost:9090/-/ready >/dev/null 2>&1; then
  echo "[OK] Prometheus: ready at http://localhost:9090"
else
  fail "Prometheus: not reachable at http://localhost:9090 — start the port-forward: kubectl port-forward -n $NAMESPACE svc/$RELEASE-prometheus 9090:9090"
fi

# 4. Scrape targets. The set of jobs to check is read from the running
# Prometheus config instead of being hardcoded. A hardcoded list is how this
# script came to expect the dispatcher and worker jobs long after both
# processes were deleted: the list and the config drifted, and the check
# failed for a reason that had nothing to do with the install. Deriving it
# means a job added to the config is checked automatically and a job removed
# from the config cannot be demanded.
python3 - <<'PY'
import json, re, urllib.request

def get(path):
    with urllib.request.urlopen('http://localhost:9090' + path, timeout=5) as r:
        return json.load(r)

config = get('/api/v1/status/config')['data']['yaml']
expected = sorted({
    name for name in re.findall(r"job_name:\s*['\"]?([A-Za-z0-9_/-]+)", config)
    if 'orbitjob-' in name
})
if not expected:
    raise SystemExit('[FAIL] no orbitjob scrape jobs declared in the running Prometheus config — the PodMonitors in deploy/monitoring/orbitjob-observability.yaml are not being picked up')

# Group by scrapePool, which is the config's job_name: the targets' own job
# label is overridden by the monitors' jobLabel and would not match.
by_pool = {}
for target in get('/api/v1/targets')['data']['activeTargets']:
    by_pool.setdefault(target.get('scrapePool'), []).append(target)

missing = [job for job in expected if not by_pool.get(job)]
down = sorted({job for job in expected
        if any(t.get('health') != 'up' for t in by_pool.get(job, []))})
if missing or down:
    hint = ''
    if missing:
        hint += (' A job in the config with no target usually means its pods do not'
                 ' match the monitor selector, or (operator) the chart does not'
                 ' declare the scraped container port yet.')
    raise SystemExit(f'[FAIL] Prometheus targets unhealthy: missing={missing} down={down}.{hint}')
print(f'[OK] Prometheus: {len(expected)} OrbitJob scrape jobs up: ' + ', '.join(expected))
PY

# 5. The alert rules are loaded and evaluating.
python3 - <<'PY'
import json, urllib.request

with urllib.request.urlopen('http://localhost:9090/api/v1/rules', timeout=5) as r:
    groups = json.load(r)['data']['groups']
rules = [rule for g in groups if 'orbitjob' in g['name'] for rule in g['rules']]
bad = [(rule['name'], rule.get('lastError', '')) for rule in rules if rule.get('health') != 'ok']
if not rules:
    raise SystemExit('[FAIL] no orbitjob alert rules loaded — the PrometheusRule in deploy/monitoring/orbitjob-observability.yaml is not being picked up')
if bad:
    raise SystemExit(f'[FAIL] orbitjob alert rules failing to evaluate: {bad}')
print(f'[OK] Prometheus: {len(rules)} OrbitJob alert rules loaded and evaluating')
PY

# 6. The dashboard. Its ConfigMap must exist with the sidecar label (that is
# what gets it loaded); the live Grafana API check is best-effort because it
# needs a second port-forward. Anonymous viewer access is part of the dev
# posture, so no credentials are involved.
if kubectl -n "$NAMESPACE" get configmap -l grafana_dashboard=1 2>/dev/null | grep -q .; then
  echo "[OK] Grafana: dashboard ConfigMap(s) labeled grafana_dashboard=1 present in $NAMESPACE"
else
  fail "Grafana: no dashboard ConfigMap labeled grafana_dashboard=1 in $NAMESPACE — apply deploy/monitoring/grafana-dashboards.yaml"
fi

if curl -fsS --max-time 5 "http://localhost:3000/api/dashboards/uid/$DASHBOARD_UID" >/dev/null 2>&1; then
  echo "[OK] Grafana: dashboard $DASHBOARD_UID loaded (anonymous viewer, http://localhost:3000)"
else
  warn "Grafana: not reachable at http://localhost:3000 — cannot confirm the dashboard is loaded in Grafana itself. Port-forward with: kubectl port-forward -n $NAMESPACE svc/$RELEASE-grafana 3000:80"
fi

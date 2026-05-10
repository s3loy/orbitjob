#!/bin/bash
# OrbitJob full release script
# Runs migration then upgrades all components in dependency order.
# Components are upgraded starting from the deepest layer (worker) up to the entry point (admin-api).

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
COMPONENTS=(worker dispatcher scheduler admin-api)

echo "============================================"
echo "OrbitJob Release — $(date '+%Y-%m-%d %H:%M:%S')"
echo "============================================"

# 1. Run database migrations
echo ""
echo "--- Step 1/2: Database migration ---"
"${SCRIPT_DIR}/migrate.sh"
echo "--- Migration complete ---"

# 2. Upgrade each component in order
echo ""
echo "--- Step 2/2: Component upgrades ---"
for component in "${COMPONENTS[@]}"; do
    echo ""
    echo ">>> Upgrading ${component}..."
    "${SCRIPT_DIR}/upgrade.sh" "$component"
    echo "<<< ${component} upgraded successfully"
done

# 3. Final health check on admin-api
echo ""
echo "--- Final health check: admin-api ---"
HEALTH_TIMEOUT=15
ELAPSED=0
HEALTH_INTERVAL=2

while [ $ELAPSED -lt $HEALTH_TIMEOUT ]; do
    if curl -sf "http://127.0.0.1:8080/healthz" > /dev/null 2>&1; then
        echo "[$(date '+%Y-%m-%d %H:%M:%S')] Admin API healthy"
        echo ""
        echo "============================================"
        echo "Release complete — $(date '+%Y-%m-%d %H:%M:%S')"
        echo "============================================"
        exit 0
    fi
    sleep "$HEALTH_INTERVAL"
    ELAPSED=$((ELAPSED + HEALTH_INTERVAL))
done

echo "[$(date '+%Y-%m-%d %H:%M:%S')] ERROR: Admin API health check failed after ${HEALTH_TIMEOUT}s"
exit 1

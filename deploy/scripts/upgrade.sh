#!/bin/bash
# OrbitJob single-component upgrade script
# Usage: upgrade.sh <component>
#   component: admin-api | scheduler | dispatcher | worker

set -euo pipefail

COMPONENT="${1:-}"
if [ -z "$COMPONENT" ]; then
    echo "Usage: $0 <component>"
    echo "  component: admin-api | scheduler | dispatcher | worker"
    exit 1
fi

BIN_DIR="/opt/orbitjob/bin"
CURRENT_LINK="/opt/orbitjob/current/${COMPONENT}"
NEW_BIN="${BIN_DIR}/${COMPONENT}.new"
PREV_BIN="${BIN_DIR}/${COMPONENT}.prev"
HEALTH_PORTS=(
    [admin-api]=8080
    [scheduler]=6060
    [dispatcher]=6061
    [worker]=6062
)
HEALTH_PORT="${HEALTH_PORTS[$COMPONENT]:-}"
HEALTH_TIMEOUT=10
HEALTH_INTERVAL=2

echo "[$(date '+%Y-%m-%d %H:%M:%S')] Building ${COMPONENT}..."

# Build new binary
cd /opt/orbitjob/repo
CGO_ENABLED=0 go build -o "$NEW_BIN" "./cmd/${COMPONENT}"

echo "[$(date '+%Y-%m-%d %H:%M:%S')] Build complete: ${NEW_BIN}"

# Backup existing binary
if [ -f "$CURRENT_LINK" ]; then
    cp -f "$CURRENT_LINK" "$PREV_BIN"
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] Backed up current binary to ${PREV_BIN}"
fi

# Atomic replacement
mv "$NEW_BIN" "$CURRENT_LINK"
echo "[$(date '+%Y-%m-%d %H:%M:%S')] Replaced binary at ${CURRENT_LINK}"

# Restart service
systemctl restart "orbitjob-${COMPONENT}"
echo "[$(date '+%Y-%m-%d %H:%M:%S')] Restarted orbitjob-${COMPONENT}"

# Health check
if [ -n "$HEALTH_PORT" ]; then
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] Waiting for health check on port ${HEALTH_PORT}..."
    ELAPSED=0
    while [ $ELAPSED -lt $HEALTH_TIMEOUT ]; do
        if curl -sf "http://127.0.0.1:${HEALTH_PORT}/healthz" > /dev/null 2>&1; then
            echo "[$(date '+%Y-%m-%d %H:%M:%S')] Health check passed"
            exit 0
        fi
        sleep "$HEALTH_INTERVAL"
        ELAPSED=$((ELAPSED + HEALTH_INTERVAL))
    done

    # Health check failed — rollback
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] Health check FAILED after ${HEALTH_TIMEOUT}s — rolling back"
    if [ -f "$PREV_BIN" ]; then
        mv "$PREV_BIN" "$CURRENT_LINK"
        systemctl restart "orbitjob-${COMPONENT}"
        echo "[$(date '+%Y-%m-%d %H:%M:%S')] Rollback complete"
    else
        echo "[$(date '+%Y-%m-%d %H:%M:%S')] ERROR: No previous binary to roll back to"
    fi
    exit 1
fi

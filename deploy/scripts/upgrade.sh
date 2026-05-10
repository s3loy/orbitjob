#!/bin/bash
# OrbitJob single-component upgrade script
# Usage: upgrade.sh [--build] <component>
#   component: admin-api | scheduler | dispatcher | worker
#   --build: build binary locally (default: expect pre-built at $BIN_DIR/<component>.new)

set -euo pipefail

# --build flag: build binary locally (default: expect pre-built)
BUILD=false
if [ "${1:-}" = "--build" ]; then
    BUILD=true
    shift
fi

COMPONENT="${1:-}"
if [ -z "$COMPONENT" ]; then
    echo "Usage: $0 [--build] <component>"
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

if $BUILD; then
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] Building ${COMPONENT}..."
    cd /opt/orbitjob/repo
    CGO_ENABLED=0 go build -o "$NEW_BIN" "./cmd/${COMPONENT}"
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] Build complete: ${NEW_BIN}"
else
    if [ ! -f "$NEW_BIN" ]; then
        echo "ERROR: Pre-built binary not found: ${NEW_BIN}"
        echo "  Run with --build flag to build on this machine, or"
        echo "  place the pre-built binary at ${NEW_BIN}"
        exit 1
    fi
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] Using pre-built binary: ${NEW_BIN}"
fi

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

    # Health check failed -- rollback
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] Health check FAILED after ${HEALTH_TIMEOUT}s -- rolling back"
    if [ -f "$PREV_BIN" ]; then
        mv "$PREV_BIN" "$CURRENT_LINK"
        systemctl restart "orbitjob-${COMPONENT}"
        echo "[$(date '+%Y-%m-%d %H:%M:%S')] Rollback: restarted with previous binary"

        # Verify rollback health
        echo "[$(date '+%Y-%m-%d %H:%M:%S')] Rollback: waiting for health check on port ${HEALTH_PORT}..."
        ROLLBACK_ELAPSED=0
        ROLLBACK_TIMEOUT=$((HEALTH_TIMEOUT * 2))
        ROLLBACK_HEALTHY=false
        while [ $ROLLBACK_ELAPSED -lt $ROLLBACK_TIMEOUT ]; do
            if curl -sf "http://127.0.0.1:${HEALTH_PORT}/healthz" > /dev/null 2>&1; then
                echo "[$(date '+%Y-%m-%d %H:%M:%S')] Rollback health check passed"
                ROLLBACK_HEALTHY=true
                break
            fi
            sleep "$HEALTH_INTERVAL"
            ROLLBACK_ELAPSED=$((ROLLBACK_ELAPSED + HEALTH_INTERVAL))
        done

        if $ROLLBACK_HEALTHY; then
            echo "[$(date '+%Y-%m-%d %H:%M:%S')] Rollback complete -- previous binary is healthy"
        else
            echo "[$(date '+%Y-%m-%d %H:%M:%S')] CRITICAL: Rollback health check FAILED after ${ROLLBACK_TIMEOUT}s"
        fi
    else
        echo "[$(date '+%Y-%m-%d %H:%M:%S')] ERROR: No previous binary to roll back to"
    fi
    exit 1
fi

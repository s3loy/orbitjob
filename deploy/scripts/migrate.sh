#!/bin/bash
# OrbitJob database migration script
# Requires golang-migrate CLI and DATABASE_URL environment variable.

set -euo pipefail

if [ -z "${DATABASE_URL:-}" ]; then
    echo "ERROR: DATABASE_URL environment variable is required"
    exit 1
fi

MIGRATIONS_DIR="/opt/orbitjob/current/db/migrations"

if [ ! -d "$MIGRATIONS_DIR" ]; then
    echo "ERROR: Migrations directory not found: ${MIGRATIONS_DIR}"
    exit 1
fi

echo "[$(date '+%Y-%m-%d %H:%M:%S')] Current migration version:"
migrate -path "$MIGRATIONS_DIR" -database "$DATABASE_URL" version 2>&1 || true

echo ""
echo "[$(date '+%Y-%m-%d %H:%M:%S')] Dry-run: applying one migration..."
migrate -path "$MIGRATIONS_DIR" -database "$DATABASE_URL" up 1

echo ""
echo "[$(date '+%Y-%m-%d %H:%M:%S')] Dry-run: rolling back one migration..."
migrate -path "$MIGRATIONS_DIR" -database "$DATABASE_URL" down 1

echo ""
echo "[$(date '+%Y-%m-%d %H:%M:%S')] Applying all pending migrations..."
migrate -path "$MIGRATIONS_DIR" -database "$DATABASE_URL" up

echo ""
echo "[$(date '+%Y-%m-%d %H:%M:%S')] New migration version:"
migrate -path "$MIGRATIONS_DIR" -database "$DATABASE_URL" version

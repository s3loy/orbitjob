#!/bin/bash
# OrbitJob database backup script
# Dumps PostgreSQL database to /opt/orbitjob/var/backups/ with rotation.

set -euo pipefail

BACKUP_DIR="/opt/orbitjob/var/backups"
RETENTION_DAYS="${RETENTION_DAYS:-30}"
TIMESTAMP="$(date +%Y%m%d_%H%M%S)"
BACKUP_FILE="${BACKUP_DIR}/orbitjob_${TIMESTAMP}.dump"

mkdir -p "$BACKUP_DIR"

echo "[$(date '+%Y-%m-%d %H:%M:%S')] Starting backup to ${BACKUP_FILE}"

pg_dump \
  -U orbitjob \
  -d orbitjob \
  --format=custom \
  --compress=9 \
  --file="$BACKUP_FILE"

echo "[$(date '+%Y-%m-%d %H:%M:%S')] Backup complete: $(du -h "$BACKUP_FILE" | cut -f1)"

# Rotate old backups
if [ -d "$BACKUP_DIR" ]; then
    DELETED=$(find "$BACKUP_DIR" -name "orbitjob_*.dump" -type f -mtime +"$RETENTION_DAYS" -print -delete | wc -l)
    if [ "$DELETED" -gt 0 ]; then
        echo "[$(date '+%Y-%m-%d %H:%M:%S')] Rotated ${DELETED} backup(s) older than ${RETENTION_DAYS} days"
    fi
fi

#!/usr/bin/env bash
# Daily database backup (run from the project folder on the server).
#   ./scripts/backup.sh                       -> backups/leadloop-YYYY-MM-DD_HHMM.sql.gz, keeps the last 7 days
# Another compose file:  COMPOSE_FILE=docker-compose.prod.yml ./scripts/backup.sh
# Cron (every day 02:30):  30 2 * * * cd /opt/lead-loop && ./scripts/backup.sh >> backups/backup.log 2>&1
# A backup you have never restored is not a backup: run ./scripts/restore-test.sh after the first one.
set -euo pipefail
cd "$(dirname "$0")/.."
COMPOSE=(docker compose -f "${COMPOSE_FILE:-docker-compose.prod.yml}")
KEEP_DAYS="${KEEP_DAYS:-7}"
mkdir -p backups
out="backups/leadloop-$(date +%F_%H%M).sql.gz"
"${COMPOSE[@]}" exec -T db sh -c 'pg_dump -U "$POSTGRES_USER" --no-owner "$POSTGRES_DB"' | gzip > "$out.tmp"
# an empty or truncated dump must never replace a good one
if [ "$(gzip -dc "$out.tmp" | wc -c)" -lt 1000 ]; then
  rm -f "$out.tmp"; echo "backup FAILED: dump is empty or too small" >&2; exit 1
fi
mv "$out.tmp" "$out"
find backups -name 'leadloop-*.sql.gz' -mtime +"$KEEP_DAYS" -delete
echo "backup ok: $out ($(du -h "$out" | cut -f1))"

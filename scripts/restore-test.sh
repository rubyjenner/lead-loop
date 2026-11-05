#!/usr/bin/env bash
# Proves the newest backup can really be restored. It loads the dump into a TEMPORARY database
# (restore_check), compares row counts with the live database, then drops the temporary one.
# The live database is never touched.
set -euo pipefail
cd "$(dirname "$0")/.."
COMPOSE=(docker compose -f "${COMPOSE_FILE:-docker-compose.prod.yml}")
file="${1:-$(ls -1t backups/leadloop-*.sql.gz 2>/dev/null | head -1)}"
[ -n "$file" ] && [ -f "$file" ] || { echo "no backup file found in backups/" >&2; exit 1; }
echo "restoring $file into a temporary database..."
"${COMPOSE[@]}" exec -T db sh -c 'dropdb -U "$POSTGRES_USER" --if-exists restore_check && createdb -U "$POSTGRES_USER" restore_check'
trap '"${COMPOSE[@]}" exec -T db sh -c '"'"'dropdb -U "$POSTGRES_USER" --if-exists restore_check'"'"' >/dev/null 2>&1 || true' EXIT
gzip -dc "$file" | "${COMPOSE[@]}" exec -T db sh -c 'psql -q -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" restore_check' >/dev/null
count() { "${COMPOSE[@]}" exec -T db sh -c "psql -tA -U \"\$POSTGRES_USER\" $1 -c \"SELECT count(*) FROM $2\"" | tr -d '[:space:]'; }
status=0
for t in users campaigns contents leads scoring_rules; do
  live=$(count '"$POSTGRES_DB"' "$t"); copy=$(count restore_check "$t")
  if [ "$live" = "$copy" ]; then echo "  $t: $live = $copy  OK"; else echo "  $t: live $live, restored $copy  (differs: the live data may have changed since the backup)"; fi
  [ -n "$copy" ] || status=1
done
[ $status -eq 0 ] && echo "restore test passed" || { echo "restore test FAILED" >&2; exit 1; }

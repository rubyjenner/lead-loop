# Windows version of restore-test.sh: restores the newest backup into a TEMPORARY database (restore_check),
# compares row counts with the live database, then drops the temporary one. The live database is not touched.
param([string]$ComposeFile = 'docker-compose.yml', [string]$File = '')
$ErrorActionPreference = 'Stop'
if (-not $File) { $f = Get-ChildItem backups -Filter 'leadloop-*.sql.gz' | Sort-Object LastWriteTime -Descending | Select-Object -First 1; if ($f) { $File = $f.FullName } }
if (-not $File -or -not (Test-Path $File)) { throw 'no backup file found in backups\' }
function Dc([string]$cmd) { docker compose -f $ComposeFile exec -T db sh -c $cmd; if ($LASTEXITCODE -ne 0) { throw "failed: $cmd" } }
function Count([string]$db, [string]$table) {
  # no double quotes inside the argument: Windows PowerShell 5.1 strips them when it calls docker
  $out = ((docker compose -f $ComposeFile exec -T db sh -c "psql -tA -U `$POSTGRES_USER $db -c 'SELECT count(*) FROM $table'") -join '').Trim()
  if ($out -notmatch '^\d+$') { throw "could not count rows in $db.$table (got: '$out')" }
  $out
}
Write-Host "restoring $File into a temporary database..."
docker compose -f $ComposeFile cp $File db:/tmp/restore.sql.gz
if ($LASTEXITCODE -ne 0) { throw 'copy into container failed' }
try {
  Dc 'dropdb -U "$POSTGRES_USER" --if-exists restore_check && createdb -U "$POSTGRES_USER" restore_check'
  Dc 'gunzip -c /tmp/restore.sql.gz | psql -q -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" restore_check > /dev/null'
  $fail = $false
  foreach ($t in 'users', 'campaigns', 'contents', 'leads', 'scoring_rules') {
    $live = Count '$POSTGRES_DB' $t; $copy = Count 'restore_check' $t
    if ($live -eq $copy) { Write-Host "  $t : $live = $copy  OK" -ForegroundColor Green }
    else { Write-Host "  $t : live $live, restored $copy (differs: live data may have changed since the backup)" -ForegroundColor Yellow }
    if (-not $copy) { $fail = $true }
  }
  if ($fail) { throw 'restore test FAILED' }
  Write-Host 'restore test passed' -ForegroundColor Green
} finally {
  docker compose -f $ComposeFile exec -T db sh -c 'dropdb -U "$POSTGRES_USER" --if-exists restore_check; rm -f /tmp/restore.sql.gz' 2>&1 | Out-Null
}
# Windows version of backup.sh, for trying the backup on a development machine.
#   .\scripts\backup.ps1                      (uses docker-compose.yml)
#   .\scripts\backup.ps1 -ComposeFile docker-compose.prod.yml
# The dump is made inside the container and copied out as a file, so no PowerShell encoding can corrupt it.
param([string]$ComposeFile = 'docker-compose.yml', [int]$KeepDays = 7)
$ErrorActionPreference = 'Stop'
New-Item -ItemType Directory -Force -Path backups | Out-Null
$name = 'leadloop-' + (Get-Date -Format 'yyyy-MM-dd_HHmm') + '.sql.gz'
docker compose -f $ComposeFile exec -T db sh -c 'pg_dump -U "$POSTGRES_USER" --no-owner "$POSTGRES_DB" | gzip > /tmp/backup.sql.gz'
if ($LASTEXITCODE -ne 0) { throw 'pg_dump failed' }
docker compose -f $ComposeFile cp db:/tmp/backup.sql.gz "backups/$name"
if ($LASTEXITCODE -ne 0) { throw 'copy from container failed' }
docker compose -f $ComposeFile exec -T db rm -f /tmp/backup.sql.gz
$size = (Get-Item "backups/$name").Length
if ($size -lt 500) { Remove-Item "backups/$name"; throw "backup FAILED: file is only $size bytes" }
Get-ChildItem backups -Filter 'leadloop-*.sql.gz' | Where-Object { $_.LastWriteTime -lt (Get-Date).AddDays(-$KeepDays) } | Remove-Item
Write-Host "backup ok: backups/$name ($([math]::Round($size / 1KB)) KB)" -ForegroundColor Green

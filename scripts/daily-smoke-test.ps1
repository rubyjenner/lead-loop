# LeadLoop: smoke test for the daily job and follow-ups (mock data only; use a dev database).
# Needs: docker compose up -d with the daily job code built in. It back-dates two test leads through
# `docker compose exec db psql`, so run it from the project folder. It also runs the daily job for ALL
# leads in the database, which may create follow-up drafts for other eligible leads: dev data only.
# This file is pure ASCII so Windows PowerShell 5.1 reads it correctly.
param([string]$BaseUrl = 'http://localhost:8080', [string]$Email = 'admin@example.com',
      [string]$DbUser = 'leadloop', [string]$DbName = 'leadloop')
$ErrorActionPreference = 'Stop'
$script:pass = 0; $script:fail = 0; $script:Token = $null

function Check([string]$name, [bool]$ok) {
  if ($ok) { Write-Host "PASS  $name" -ForegroundColor Green; $script:pass++ }
  else { Write-Host "FAIL  $name" -ForegroundColor Red; $script:fail++ }
}

function Call([string]$Method, [string]$Path, $Json = $null, [switch]$NoAuth) {
  $h = @{}
  if (-not $NoAuth -and $script:Token) { $h['Authorization'] = "Bearer $($script:Token)" }
  $req = @{ Uri = "$BaseUrl$Path"; Method = $Method; Headers = $h; UseBasicParsing = $true }
  if ($null -ne $Json) {
    $req['Body'] = [System.Text.Encoding]::UTF8.GetBytes($Json)
    $req['ContentType'] = 'application/json; charset=utf-8'
  }
  $status = 0; $content = $null
  try { $r = Invoke-WebRequest @req; $status = [int]$r.StatusCode; $content = $r.Content }
  catch {
    $resp = $_.Exception.Response
    if ($null -eq $resp) { throw }
    $status = [int]$resp.StatusCode
    $content = $_.ErrorDetails.Message
    if (-not $content) {
      try { $sr = New-Object IO.StreamReader($resp.GetResponseStream(), [Text.Encoding]::UTF8); $content = $sr.ReadToEnd() } catch {}
    }
  }
  $obj = $null
  if ($content) { try { $obj = $content | ConvertFrom-Json } catch {} }
  [pscustomobject]@{ Status = $status; Body = $obj }
}

function Sql([string]$q) {
  $out = docker compose exec -T db psql -U $DbUser $DbName -c $q 2>&1
  if ($LASTEXITCODE -ne 0) { throw "psql failed: $out" }
}

function NewLead([string]$name, [bool]$marketing) {
  $ext = 'daily-smoke-' + [guid]::NewGuid().ToString('N').Substring(0, 10)
  $body = @{ display_name = $name; channel = 'mock'; channel_user_id = $ext; consent_general = $true;
             consent_marketing = $marketing; consent_version = 'mock-v1' } | ConvertTo-Json
  $r = Call 'POST' '/leads' $body
  if ($r.Status -ne 201 -and $r.Status -ne 200) { throw "create lead failed: HTTP $($r.Status)" }
  [int64]$r.Body.id
}

Write-Host "`n== Login ==" -ForegroundColor Cyan
if ($env:SMOKE_ADMIN_PASSWORD) {
  $plain = $env:SMOKE_ADMIN_PASSWORD
} else {
  $sec = Read-Host 'Admin password' -AsSecureString
  $plain = [Runtime.InteropServices.Marshal]::PtrToStringAuto([Runtime.InteropServices.Marshal]::SecureStringToBSTR($sec))
}
$login = Call 'POST' '/auth/login' (@{ email = $Email; password = $plain } | ConvertTo-Json) -NoAuth
Check 'admin login' ($login.Status -eq 200 -and $login.Body.token)
if ($login.Status -ne 200) {
  Write-Host "Cannot continue: login returned HTTP $($login.Status) (401 = wrong password, 429 = too many failed logins -> docker compose restart api)" -ForegroundColor Yellow
  exit 1
}
$script:Token = $login.Body.token

Write-Host "`n== Access ==" -ForegroundColor Cyan
Check 'run without token -> 401' ((Call 'POST' '/daily-jobs/run' -NoAuth).Status -eq 401)
Check 'followups without token -> 401' ((Call 'GET' '/followups' -NoAuth).Status -eq 401)

Write-Host "`n== Prepare two quiet test leads ==" -ForegroundColor Cyan
$a = NewLead 'Daily smoke A (marketing yes)' $true
$b = NewLead 'Daily smoke B (marketing no)' $false
Check "lead A created (id $a)" ($a -gt 0)
Check "lead B created (id $b)" ($b -gt 0)
Sql "UPDATE leads SET created_at = now() - interval '8 days' WHERE id IN ($a, $b);"
Sql "UPDATE lead_events SET created_at = now() - interval '8 days' WHERE lead_id IN ($a, $b);"

Write-Host "`n== First run ==" -ForegroundColor Cyan
$r1 = Call 'POST' '/daily-jobs/run'
Check 'run -> 200' ($r1.Status -eq 200)
Check 'silence events created (3 days and 7 days)' ($r1.Body.no_reply_3d -ge 2 -and $r1.Body.inactive_7d -ge 2)
Check 'at least one follow-up draft created' ($r1.Body.followups_created -ge 1)
Check 'no errors' ($r1.Body.errors -eq 0)

$pending = (Call 'GET' '/followups?status=pending_approval').Body
$fa = @($pending | Where-Object { $_.lead_id -eq $a })
$fb = @($pending | Where-Object { $_.lead_id -eq $b })
Check 'lead A (marketing consent) has one pending draft' ($fa.Count -eq 1)
Check 'draft is follow-up number 1' ($fa.Count -eq 1 -and $fa[0].seq -eq 1)
Check 'lead B (no marketing consent) has NO draft' ($fb.Count -eq 0)
Check 'draft text offers an opt-out' ($fa.Count -eq 1 -and $fa[0].body.Contains([string][char]0x0e2b + [char]0x0e22 + [char]0x0e38 + [char]0x0e14))

$la = (Call 'GET' "/leads/$a").Body
Check 'lead A score log shows the silence rules' (@($la.score_breakdown).Count -ge 1 -or $la.score -eq 0)

Write-Host "`n== Second run does not repeat ==" -ForegroundColor Cyan
$r2 = Call 'POST' '/daily-jobs/run'
Check 'no new silence events' ($r2.Body.no_reply_3d -eq 0 -and $r2.Body.inactive_7d -eq 0)
Check 'no new drafts' ($r2.Body.followups_created -eq 0)
$last = (Call 'GET' '/daily-jobs/last').Body
Check 'last run is reported' ($null -ne $last.run -and $null -ne $last.run.summary)

Write-Host "`n== Consent withdrawn while a draft waits ==" -ForegroundColor Cyan
$w = Call 'POST' "/leads/$a/consent" '{"kind":"marketing","action":"withdrawn","text_version":"mock-v1"}'
Check 'marketing consent withdrawn' ($w.Status -eq 200)
$ap = Call 'POST' "/followups/$($fa[0].id)/approve"
Check 'approving the draft -> 409' ($ap.Status -eq 409)
$rej = @((Call 'GET' '/followups?status=rejected').Body | Where-Object { $_.id -eq $fa[0].id })
Check 'the draft was rejected by the system' ($rej.Count -eq 1)

Write-Host "`n== Approve and reject normally ==" -ForegroundColor Cyan
$c = NewLead 'Daily smoke C (marketing yes)' $true
Sql "UPDATE leads SET created_at = now() - interval '8 days' WHERE id = $c;"
$null = Call 'POST' '/daily-jobs/run'
$fc = @((Call 'GET' '/followups?status=pending_approval').Body | Where-Object { $_.lead_id -eq $c })
Check 'lead C has a pending draft' ($fc.Count -eq 1)
if ($fc.Count -eq 1) {
  $ok = Call 'POST' "/followups/$($fc[0].id)/approve"
  Check 'approve -> sent (mock channel)' ($ok.Status -eq 200 -and $ok.Body.status -eq 'sent')
  Check 'approve again -> 409' ((Call 'POST' "/followups/$($fc[0].id)/approve").Status -eq 409)
  Check 'reject a sent draft -> 409' ((Call 'POST' "/followups/$($fc[0].id)/reject").Status -eq 409)
}

Write-Host "`n== Cleanup ==" -ForegroundColor Cyan
foreach ($id in @($a, $b, $c)) { $null = Call 'DELETE' "/leads/$id" }
Check 'test leads anonymized' ((Call 'GET' "/leads/$a").Status -in 200, 404)

Write-Host "`nPassed: $script:pass   Failed: $script:fail" -ForegroundColor $(if ($script:fail -eq 0) { 'Green' } else { 'Red' })
if ($script:fail -gt 0) { exit 1 }

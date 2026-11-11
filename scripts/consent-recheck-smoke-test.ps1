# LeadLoop: smoke test for PDPA consent vs follow-up drafts (mock data only; use a dev database).
# Checks that a follow-up draft never outlives the customer's consent: withdrawing consent, switching marketing
# off or deleting the data rejects every draft that was not sent yet (waiting for approval OR approved but unsent),
# the inbox counter follows, approving afterwards returns 409, and nothing is sent.
# Needs: docker compose up -d. It back-dates test leads through `docker compose exec db psql`, so run it from
# the project folder. It runs the daily job for ALL leads in the database: dev data only.
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

function NewLead([string]$name, [bool]$marketing, [string]$channel = 'mock') {
  $ext = 'consent-smoke-' + [guid]::NewGuid().ToString('N').Substring(0, 10)
  $body = @{ display_name = $name; channel = $channel; channel_user_id = $ext; consent_general = $true;
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
  Write-Host "Cannot continue: login returned HTTP $($login.Status)" -ForegroundColor Yellow
  exit 1
}
$script:Token = $login.Body.token

# one helper: make an aged lead, run the daily job, return the id of its pending draft (0 if none)
function DraftFor([int64]$leadId) {
  Sql "UPDATE leads SET created_at = now() - interval '10 days' WHERE id = $leadId;"
  Sql "UPDATE lead_events SET created_at = now() - interval '10 days' WHERE lead_id = $leadId;"
  $null = Call 'POST' '/daily-jobs/run'
  $f = @((Call 'GET' '/followups').Body | Where-Object { $_.lead_id -eq $leadId })
  if ($f.Count -ge 1) { return $f[0] }
  return $null
}
function DraftNow([int64]$leadId) { @((Call 'GET' '/followups').Body | Where-Object { $_.lead_id -eq $leadId })[0] }
function Pending { (Call 'GET' '/inbox').Body.followups_to_approve }

Write-Host "`n== A: withdraw all consent while a draft waits ==" -ForegroundColor Cyan
$a = NewLead 'Consent smoke A' $true 'web'
$da = DraftFor $a
Check 'A: a pending draft exists' ($null -ne $da -and $da.status -eq 'pending_approval')
$before = Pending
$w = Call 'POST' "/leads/$a/consent" '{"kind":"general","action":"withdrawn","text_version":"v1"}'
Check 'A: consent withdrawn (200)' ($w.Status -eq 200)
$n = DraftNow $a
Check 'A: the draft is rejected at once by the system' ($n.status -eq 'rejected' -and $n.decided_by -eq 'system:consent_withdrawn')
Check 'A: the inbox counter went down by one' ((Pending) -eq ($before - 1))
Check 'A: approving afterwards -> 409' ((Call 'POST' "/followups/$($da.id)/approve").Status -eq 409)
Check 'A: nothing was sent' ($null -eq (DraftNow $a).sent_at)

Write-Host "`n== B: only marketing is switched off ==" -ForegroundColor Cyan
$b = NewLead 'Consent smoke B' $true 'web'
$db = DraftFor $b
Check 'B: a pending draft exists' ($null -ne $db -and $db.status -eq 'pending_approval')
$null = Call 'POST' "/leads/$b/consent" '{"kind":"marketing","action":"withdrawn","text_version":"v1"}'
Check 'B: the draft is rejected at once' ((DraftNow $b).status -eq 'rejected')
Check 'B: approving afterwards -> 409' ((Call 'POST' "/followups/$($db.id)/approve").Status -eq 409)

Write-Host "`n== C: approved but not yet sent (real channel), then consent is withdrawn ==" -ForegroundColor Cyan
$c = NewLead 'Consent smoke C' $true 'web'
$dc = DraftFor $c
$ap = Call 'POST' "/followups/$($dc.id)/approve"
Check 'C: approve while consent is valid (status approved, not sent)' ($ap.Status -eq 200 -and $ap.Body.status -eq 'approved')
$null = Call 'POST' "/leads/$c/consent" '{"kind":"general","action":"withdrawn","text_version":"v1"}'
$nc = DraftNow $c
Check 'C: the approved-but-unsent draft is cancelled' ($nc.status -eq 'rejected' -and $null -eq $nc.sent_at)

Write-Host "`n== D: consent given again does not revive an old draft ==" -ForegroundColor Cyan
$null = Call 'POST' "/leads/$a/consent" '{"kind":"general","action":"granted","text_version":"v1"}'
Check 'D: the old draft stays rejected' ((DraftNow $a).status -eq 'rejected')

Write-Host "`n== E: the lead's data is deleted ==" -ForegroundColor Cyan
$e = NewLead 'Consent smoke E' $true 'web'
$de = DraftFor $e
$null = Call 'DELETE' "/leads/$e"
Check 'E: approving a draft of a deleted lead -> 409' ((Call 'POST' "/followups/$($de.id)/approve").Status -eq 409)
Check 'E: no draft of a deleted lead is listed' (@((Call 'GET' '/followups').Body | Where-Object { $_.lead_id -eq $e }).Count -eq 0)

Write-Host "`n== F: control, a valid draft still works ==" -ForegroundColor Cyan
$f = NewLead 'Consent smoke F' $true 'web'
$df = DraftFor $f
Check 'F: a valid draft can be approved' ((Call 'POST' "/followups/$($df.id)/approve").Status -eq 200)

foreach ($id in @($a, $b, $c, $f)) { $null = Call 'DELETE' "/leads/$id" }
Write-Host "`nResult: $($script:pass) passed, $($script:fail) failed"
if ($script:fail -gt 0) { exit 1 }

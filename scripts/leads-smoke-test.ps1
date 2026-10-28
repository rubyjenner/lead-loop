<#
 Lead/scoring smoke test. Uses fictitious data only and a development database.
 Run after `docker compose up --build -d` and applying db/leads.sql to an existing DB.
 It creates and anonymizes a mock lead and creates/deletes a temporary supplementary rule.
#>
param(
    [string]$Base = 'http://localhost:8080',
    [string]$AdminEmail = 'admin@example.com'
)
$ErrorActionPreference = 'Stop'
$pass = 0; $fail = 0
function Check($name, $ok) {
    if ($ok) { Write-Host "PASS  $name" -ForegroundColor Green; $script:pass++ }
    else { Write-Host "FAIL  $name" -ForegroundColor Red; $script:fail++ }
}
function Call($method, $path, $token, $body) {
    $p = @{ Method = $method; Uri = "$Base$path"; UseBasicParsing = $true }
    if ($token) { $p.Headers = @{ Authorization = "Bearer $token" } }
    if ($null -ne $body) {
        $p.ContentType = 'application/json; charset=utf-8'
        $p.Body = [System.Text.Encoding]::UTF8.GetBytes(($body | ConvertTo-Json -Depth 10 -Compress))
    }
    try {
        $r = Invoke-WebRequest @p
        $status = [int]$r.StatusCode; $text = $r.Content
    } catch {
        if (-not $_.Exception.Response) { throw }
        $status = [int]$_.Exception.Response.StatusCode
        $text = if ($_.ErrorDetails -and $_.ErrorDetails.Message) { $_.ErrorDetails.Message } else { '' }
    }
    $json = $null; try { $json = $text | ConvertFrom-Json } catch { }
    [pscustomobject]@{ Status = $status; Json = $json }
}

if ($env:SMOKE_ADMIN_PASSWORD) { $pw = $env:SMOKE_ADMIN_PASSWORD }
else {
    $sec = Read-Host "Password for $AdminEmail" -AsSecureString
    $ptr = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($sec)
    try { $pw = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($ptr) }
    finally { [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($ptr) }
}
$login = Call 'POST' '/auth/login' $null @{ email = $AdminEmail; password = $pw }
Check 'admin login' ($login.Status -eq 200 -and $login.Json.token)
if ($login.Status -ne 200) { exit 1 }
$token = $login.Json.token

Write-Host "`n== Lead API (mock data only) =="
$noConsent = Call 'POST' '/leads' $token @{ display_name = 'Mock no-consent'; channel = 'mock'; consent_general = $false; consent_version = 'mock-v1' }
Check 'reject lead without general consent (400)' ($noConsent.Status -eq 400)
$lead = Call 'POST' '/leads' $token @{
    display_name = 'ลูกค้าทดสอบ smoke'; channel = 'mock'; channel_user_id = 'mock-smoke-001'; budget = 12900
    consent_general = $true; consent_marketing = $false; consent_version = 'mock-v1'
}
Check 'create mock lead with consent (201)' ($lead.Status -eq 201 -and $lead.Json.id)
if (-not $lead.Json.id) { Write-Host 'Lead create failed; cannot continue.' -ForegroundColor Red; exit 1 }
$id = [int]$lead.Json.id
$event = Call 'POST' "/leads/$id/events" $token @{ type = 'asked_price'; data = @{} }
Check 'record event and calculate score' ($event.Status -eq 200 -and $event.Json.score -ge 10)
$get = Call 'GET' "/leads/$id" $token $null
Check 'lead detail contains score breakdown' ($get.Status -eq 200 -and $get.Json.score -ge 10 -and $get.Json.score_breakdown.Count -ge 1)

Write-Host "`n== Scoring rule CRUD =="
$rule = Call 'POST' '/scoring-rules' $token @{
    name = 'Smoke mock rule'; description = 'temporary test rule'; category = 'intent'
    condition = @{ event = 'smoke_custom_event' }; points = 4; mode = 'once'; active = $true; sort_order = 9900
}
Check 'create supplementary rule' ($rule.Status -eq 201 -and $rule.Json.id)
if ($rule.Json.id) {
    $rid = [int]$rule.Json.id
    $patch = Call 'PATCH' "/scoring-rules/$rid" $token @{ points = 7 }
    Check 'update rule points' ($patch.Status -eq 200)
    $del = Call 'DELETE' "/scoring-rules/$rid" $token $null
    Check 'delete unused supplementary rule' ($del.Status -eq 200 -and -not $del.Json.soft_deleted)
}
$dry = Call 'POST' '/scoring-rules/test' $token @{ events = @(@{ type = 'asked_price'; data = @{} }); lead = @{ channel = 'mock' } }
Check 'dry-run scoring rules' ($dry.Status -eq 200 -and $dry.Json.score -ge 10)

Write-Host "`n== Consent and anonymization =="
$withdraw = Call 'POST' "/leads/$id/consent" $token @{ kind = 'general'; action = 'withdrawn'; text_version = 'mock-v1' }
Check 'withdraw general consent' ($withdraw.Status -eq 200)
$msg = Call 'POST' "/leads/$id/message" $token @{ text = 'ข้อความจำลองที่ไม่มี keyword' }
Check 'reject message processing after withdrawal (409)' ($msg.Status -eq 409)
$anon = Call 'DELETE' "/leads/$id" $token $null
Check 'anonymize mock lead' ($anon.Status -eq 200 -and $anon.Json.ok)
$gone = Call 'GET' "/leads/$id" $token $null
Check 'anonymized lead is not readable (404)' ($gone.Status -eq 404)

Write-Host "`n$pass passed, $fail failed"
if ($fail -gt 0) { exit 1 }

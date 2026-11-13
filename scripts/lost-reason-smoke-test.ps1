<#
 LeadLoop smoke test for the "why a lead was lost" reason (POST /leads/:id/status with a reason code).
 Needs a running API and at least one lead in status qualified or handed_off (the public-chat smoke test makes one).
 Usage:  $env:SMOKE_ADMIN_PASSWORD = '...'; .\scripts\lost-reason-smoke-test.ps1
 Changes ONE test lead to lost. Dev database only. Pure ASCII file.
#>
param([string]$Base = 'http://localhost:8080', [string]$AdminEmail = 'admin@example.com')
$ErrorActionPreference = 'Stop'
$script:pass = 0; $script:fail = 0
function Check($Name, $Cond) {
    if ($Cond) { Write-Host "PASS  $Name" -ForegroundColor Green; $script:pass++ }
    else { Write-Host "FAIL  $Name" -ForegroundColor Red; $script:fail++ }
}
function Call($Method, $Path, $Token, $Body) {
    $p = @{ Method = $Method; Uri = "$Base$Path"; UseBasicParsing = $true }
    if ($Token) { $p.Headers = @{ Authorization = "Bearer $Token" } }
    if ($null -ne $Body) {
        $p.ContentType = 'application/json; charset=utf-8'
        $p.Body = [System.Text.Encoding]::UTF8.GetBytes(($Body | ConvertTo-Json -Compress))
    }
    $text = ''
    try { $r = Invoke-WebRequest @p; $status = [int]$r.StatusCode; $text = $r.Content }
    catch {
        if (-not $_.Exception.Response) { throw }
        $status = [int]$_.Exception.Response.StatusCode
        if ($_.ErrorDetails -and $_.ErrorDetails.Message) { $text = $_.ErrorDetails.Message }
        else { try { $sr = New-Object System.IO.StreamReader($_.Exception.Response.GetResponseStream()); $text = $sr.ReadToEnd() } catch { } }
    }
    $json = $null
    if ($text) { try { $json = $text | ConvertFrom-Json } catch { } }
    [pscustomobject]@{ Status = $status; Json = $json }
}

$pw = $env:SMOKE_ADMIN_PASSWORD
if (-not $pw) {
    $sec = Read-Host 'Admin password' -AsSecureString
    $pw = [Runtime.InteropServices.Marshal]::PtrToStringAuto([Runtime.InteropServices.Marshal]::SecureStringToBSTR($sec))
}
$login = Call 'POST' '/auth/login' $null @{ email = $AdminEmail; password = $pw }
Check 'admin login' ($login.Status -eq 200 -and $login.Json.token)
if ($login.Status -ne 200) { exit 1 }
$admin = $login.Json.token

$all = Call 'GET' '/leads' $admin $null
$open = @($all.Json | Where-Object { $_.status -eq 'qualified' -or $_.status -eq 'handed_off' } | Select-Object -First 1)
if ($open.Count -eq 0) { Write-Host 'SKIP  no qualified/handed_off lead; run public-chat-smoke-test.ps1 first' -ForegroundColor Yellow; exit 0 }
$id = $open[0].id

Write-Host '== Reason validation =='
$r = Call 'POST' "/leads/$id/status" $admin @{ status = 'lost'; reason = 'not-a-code' }
Check 'unknown reason code -> 400' ($r.Status -eq 400)
$r = Call 'POST' "/leads/$id/status" $admin @{ status = 'closed'; reason = 'price' }
Check 'reason with a status other than lost -> 400' ($r.Status -eq 400)
$d = Call 'GET' "/leads/$id" $admin $null
Check 'the refused requests changed nothing' ($d.Json.status -eq $open[0].status)

Write-Host '== Lost with a reason =='
$r = Call 'POST' "/leads/$id/status" $admin @{ status = 'lost'; reason = 'price' }
Check 'lost with reason price -> 200' ($r.Status -eq 200)
$au = Call 'GET' '/audit-log?limit=20' $admin $null
Check 'audit log records lead_status_changed with the reason' (($au.Json | ConvertTo-Json -Depth 6) -match 'lost:price')

Write-Host ''
Write-Host "$script:pass passed, $script:fail failed"
if ($script:fail -gt 0) { exit 1 }

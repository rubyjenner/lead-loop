<#
 LeadLoop smoke test for the 12 Nov UI round: edit the campaign plan by hand (who may, validation, audit,
 marked as human) and edit your own profile (name and job title).
 Needs a running API (docker compose up -d). Windows PowerShell 5.1 and PowerShell 7 both work.
 Usage:  $env:SMOKE_ADMIN_PASSWORD = '...'; .\scripts\round2-smoke-test.ps1
 It creates users named round2-*@example.com (disabled at the end) and one campaign. Dev database only.
 This file is pure ASCII so Windows PowerShell 5.1 reads it correctly.
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
if ($login.Status -ne 200) { Write-Host "Cannot continue (HTTP $($login.Status))" -ForegroundColor Yellow; exit 1 }
$admin = $login.Json.token

$sfx = [guid]::NewGuid().ToString('N').Substring(0, 8)
$tempPw = 'Temp-' + [guid]::NewGuid().ToString('N').Substring(0, 12)
$newPw = 'New-' + [guid]::NewGuid().ToString('N').Substring(0, 12)
function NewUser($Role, $Name, $Title) {
    $email = "round2-$Role-$sfx-$([guid]::NewGuid().ToString('N').Substring(0,4))@example.com"
    $c = Call 'POST' '/users' $admin @{ email = $email; name = $Name; job_title = $Title; password = $tempPw; role = $Role }
    if ($c.Status -ne 201) { throw "create user failed: HTTP $($c.Status)" }
    $l = Call 'POST' '/auth/login' $null @{ email = $email; password = $tempPw }
    $ch = Call 'POST' '/auth/password' $l.Json.token @{ current_password = $tempPw; new_password = $newPw }
    [pscustomobject]@{ Id = $c.Json.id; Email = $email; Token = $ch.Json.token }
}

Write-Host "`n== Edit your own profile (name and job title) ==" -ForegroundColor Cyan
$a = NewUser 'marketing' "Round Two Creator $sfx" 'Marketer'
$b = NewUser 'marketing' "Round Two Other $sfx" 'Marketer'
$s = NewUser 'sales' "Round Two Sales $sfx" 'Sales'
Check 'no token -> 401' ((Call 'PUT' '/auth/me' $null @{ name = 'x' }).Status -eq 401)
Check 'empty body -> 400' ((Call 'PUT' '/auth/me' $a.Token @{}).Status -eq 400)
Check 'blank name -> 400' ((Call 'PUT' '/auth/me' $a.Token @{ name = '   ' }).Status -eq 400)
Check 'name over 100 characters -> 400' ((Call 'PUT' '/auth/me' $a.Token @{ name = ('n' * 101) }).Status -eq 400)
Check 'job title over 100 characters -> 400' ((Call 'PUT' '/auth/me' $a.Token @{ job_title = ('t' * 101) }).Status -eq 400)
$up = Call 'PUT' '/auth/me' $a.Token @{ name = "Renamed $sfx"; job_title = 'Head of Growth' }
Check 'update own name and title (200)' ($up.Status -eq 200)
$me = (Call 'GET' '/auth/me' $a.Token $null).Json
Check '/auth/me shows the new name and title' ($me.name -eq "Renamed $sfx" -and $me.job_title -eq 'Head of Growth')
$up2 = Call 'PUT' '/auth/me' $a.Token @{ job_title = 'Director' }
$me = (Call 'GET' '/auth/me' $a.Token $null).Json
Check 'changing only the title keeps the name' ($up2.Status -eq 200 -and $me.name -eq "Renamed $sfx" -and $me.job_title -eq 'Director')
Check 'role cannot be changed through /auth/me' ((Call 'PUT' '/auth/me' $a.Token @{ name = "Renamed $sfx"; role = 'admin' }).Status -eq 200 -and (Call 'GET' '/auth/me' $a.Token $null).Json.role -eq 'marketing')
Check 'the other user is not affected' ((Call 'GET' '/auth/me' $b.Token $null).Json.name -eq "Round Two Other $sfx")

Write-Host "`n== Edit the campaign plan by hand ==" -ForegroundColor Cyan
$camp = Call 'POST' '/campaigns' $a.Token @{ name = "round2-$sfx"; goal = 'test'; budget = 100 }
Check 'A creates a campaign (201)' ($camp.Status -eq 201)
$cid = $camp.Json.id
Check 'A saves a plan (200)' ((Call 'PUT' "/campaigns/$cid/plan" $a.Token @{ plan = "Week 1: post`nWeek 2: follow up" }).Status -eq 200)
$cd = (Call 'GET' "/campaigns/$cid" $a.Token $null).Json
Check 'the plan is stored and marked as written by a human' ($cd.plan -like 'Week 1*' -and $cd.plan_generated_by -eq 'human')
Check 'a draft campaign becomes planned' ($cd.status -eq 'planned')
$r2 = Call 'PUT' "/campaigns/$cid/plan" $a.Token @{ plan = '  trimmed plan  ' }
Check 'the plan is trimmed' ($r2.Status -eq 200 -and (Call 'GET' "/campaigns/$cid" $a.Token $null).Json.plan -eq 'trimmed plan')
Check 'blank plan -> 400' ((Call 'PUT' "/campaigns/$cid/plan" $a.Token @{ plan = '   ' }).Status -eq 400)
Check 'plan over 8000 characters -> 400' ((Call 'PUT' "/campaigns/$cid/plan" $a.Token @{ plan = ('p' * 8001) }).Status -eq 400)
Check 'the bad requests changed nothing' ((Call 'GET' "/campaigns/$cid" $a.Token $null).Json.plan -eq 'trimmed plan')
Check 'B (other marketing) cannot edit the plan (403)' ((Call 'PUT' "/campaigns/$cid/plan" $b.Token @{ plan = 'hacked' }).Status -eq 403)
Check 'sales cannot edit the plan (403)' ((Call 'PUT' "/campaigns/$cid/plan" $s.Token @{ plan = 'x' }).Status -eq 403)
Check 'no token -> 401' ((Call 'PUT' "/campaigns/$cid/plan" $null @{ plan = 'x' }).Status -eq 401)
Check 'unknown campaign -> 404' ((Call 'PUT' '/campaigns/999999999/plan' $a.Token @{ plan = 'x' }).Status -eq 404)
Check 'the plan is still the creator text after the refused attempts' ((Call 'GET' "/campaigns/$cid" $b.Token $null).Json.plan -eq 'trimmed plan')
Check 'admin edits the plan of someone else (200)' ((Call 'PUT' "/campaigns/$cid/plan" $admin @{ plan = 'plan edited by admin' }).Status -eq 200)

Write-Host "`n== Audit log ==" -ForegroundColor Cyan
$actions = @((Call 'GET' '/audit-log?limit=200' $admin $null).Json | ForEach-Object { $_.action })
foreach ($x in 'campaign_plan_edited', 'admin_edit_other_campaign', 'profile_updated') { Check "audit log has $x" ($actions -contains $x) }

foreach ($u in $a, $b, $s) { [void](Call 'PATCH' "/users/$($u.Id)" $admin @{ active = $false }) }
Write-Host "`n$($script:pass) passed, $($script:fail) failed"
if ($script:fail -gt 0) { exit 1 }

<#
 LeadLoop smoke test: who may change a campaign (creator / approver / admin / sales), rejection reason,
 edit-after-approval, full name and job title on creator and approver, newest lead first.
 Needs a running API (docker compose up -d). Windows PowerShell 5.1 and PowerShell 7 both work.
 Usage:  $env:SMOKE_ADMIN_PASSWORD = '...'; .\scripts\roles-smoke-test.ps1
 It creates users named roles-*@example.com (disabled at the end) and one campaign. Dev database only.
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
    $email = "roles-$Role-$sfx-$([guid]::NewGuid().ToString('N').Substring(0,4))@example.com"
    $c = Call 'POST' '/users' $admin @{ email = $email; name = $Name; job_title = $Title; password = $tempPw; role = $Role }
    if ($c.Status -ne 201) { throw "create user failed: HTTP $($c.Status)" }
    $l = Call 'POST' '/auth/login' $null @{ email = $email; password = $tempPw }
    $ch = Call 'POST' '/auth/password' $l.Json.token @{ current_password = $tempPw; new_password = $newPw }
    [pscustomobject]@{ Id = $c.Json.id; Email = $email; Token = $ch.Json.token }
}

Write-Host "`n== Users with full name and job title ==" -ForegroundColor Cyan
$a = NewUser 'marketing' "Somchai Creator $sfx" 'Marketing Manager'
$b = NewUser 'marketing' "Somying Approver $sfx" 'Senior Marketer'
$s = NewUser 'sales' "Sales Person $sfx" 'Sales'
$me = Call 'GET' '/auth/me' $a.Token $null
Check '/auth/me returns name and job_title' ($me.Json.name -like 'Somchai*' -and $me.Json.job_title -eq 'Marketing Manager')

Write-Host "`n== Creator edits, others cannot ==" -ForegroundColor Cyan
$camp = Call 'POST' '/campaigns' $a.Token @{ name = "roles-$sfx"; goal = 'test'; budget = 100 }
Check 'A creates a campaign (201)' ($camp.Status -eq 201)
$cid = $camp.Json.id
$cd = (Call 'GET' "/campaigns/$cid" $b.Token $null).Json
Check 'campaign shows creator full name and job title' ($cd.created_by_name -like 'Somchai*' -and $cd.created_by_title -eq 'Marketing Manager')
$ct = Call 'POST' "/campaigns/$cid/contents" $a.Token @{ channel = 'line'; body = 'roles smoke text' }
Check 'A (creator) adds a content draft' ($ct.Status -eq 201)
$tid = $ct.Json.id
Check 'B (other marketing) cannot add content (403)' ((Call 'POST' "/campaigns/$cid/contents" $b.Token @{ channel = 'line'; body = 'x' }).Status -eq 403)
Check 'B cannot ask the AI to re-plan (403)' ((Call 'POST' "/campaigns/$cid/plan" $b.Token $null).Status -eq 403)
Check 'B cannot edit the draft (403)' ((Call 'PUT' "/contents/$tid" $b.Token @{ body = 'hacked' }).Status -eq 403)
Check 'B cannot submit it (403)' ((Call 'POST' "/contents/$tid/submit" $b.Token $null).Status -eq 403)
Check 'sales cannot edit the draft (403)' ((Call 'PUT' "/contents/$tid" $s.Token @{ body = 'x' }).Status -eq 403)
Check 'A edits own draft (200)' ((Call 'PUT' "/contents/$tid" $a.Token @{ body = 'roles smoke text v2' }).Status -eq 200)
Check 'A submits for approval' ((Call 'POST' "/contents/$tid/submit" $a.Token $null).Status -eq 200)

Write-Host "`n== Approver: approve or reject only, reason required ==" -ForegroundColor Cyan
Check 'A cannot reject own content (403)' ((Call 'POST' "/contents/$tid/reject" $a.Token @{ reason = 'self' }).Status -eq 403)
Check 'sales cannot reject (403)' ((Call 'POST' "/contents/$tid/reject" $s.Token @{ reason = 'x' }).Status -eq 403)
Check 'B rejecting without a reason -> 400' ((Call 'POST' "/contents/$tid/reject" $b.Token @{ reason = '   ' }).Status -eq 400)
Check 'B rejects with a reason (200)' ((Call 'POST' "/contents/$tid/reject" $b.Token @{ reason = 'price is wrong' }).Status -eq 200)
$row = @((Call 'GET' "/campaigns/$cid/contents" $a.Token $null).Json | Where-Object { $_.id -eq $tid })[0]
Check 'creator sees status rejected and the reason' ($row.status -eq 'rejected' -and $row.reject_reason -eq 'price is wrong')
Check 'rejected by shows full name and job title' ($row.rejected_by_name -like 'Somying*' -and $row.rejected_by_title -eq 'Senior Marketer')
Check 'A edits and resubmits' ((Call 'PUT' "/contents/$tid" $a.Token @{ body = 'roles smoke text v3' }).Status -eq 200 -and (Call 'POST' "/contents/$tid/submit" $a.Token $null).Status -eq 200)
Check 'A cannot approve own content (403)' ((Call 'POST' "/contents/$tid/approve" $a.Token $null).Status -eq 403)
Check 'B approves (200)' ((Call 'POST' "/contents/$tid/approve" $b.Token $null).Status -eq 200)
$row = @((Call 'GET' "/campaigns/$cid/contents" $a.Token $null).Json | Where-Object { $_.id -eq $tid })[0]
Check 'approved by shows full name and job title' ($row.status -eq 'approved' -and $row.approved_by_name -like 'Somying*' -and $row.approved_by_title -eq 'Senior Marketer')

Write-Host "`n== Editing after approval goes back to pending ==" -ForegroundColor Cyan
$ed = Call 'PUT' "/contents/$tid" $a.Token @{ body = 'roles smoke text v4 after approval' }
Check 'creator edits approved content (200, status pending_approval)' ($ed.Status -eq 200 -and $ed.Json.status -eq 'pending_approval')
$row = @((Call 'GET' "/campaigns/$cid/contents" $a.Token $null).Json | Where-Object { $_.id -eq $tid })[0]
Check 'the approval is cleared' ($row.status -eq 'pending_approval' -and -not $row.approved_by)
$same = Call 'PUT' "/contents/$tid" $a.Token @{ body = 'roles smoke text v4 after approval' }
Check 'saving the same text changes nothing' ($same.Status -eq 200 -and $same.Json.changed -eq $false)

Write-Host "`n== Admin edits any campaign, with an audit entry ==" -ForegroundColor Cyan
Check 'admin edits A campaign content (200)' ((Call 'PUT' "/contents/$tid" $admin @{ body = 'edited by admin' }).Status -eq 200)
$audit = Call 'GET' '/audit-log?limit=200' $admin $null
$actions = @($audit.Json | ForEach-Object { $_.action })
foreach ($x in 'admin_edit_other_campaign', 'content_edited', 'content_rejected') { Check "audit log has $x" ($actions -contains $x) }

Write-Host "`n== Leads: newest first ==" -ForegroundColor Cyan
$l1 = Call 'POST' '/leads' $admin @{ display_name = "roles lead 1 $sfx"; channel = 'web'; channel_user_id = "roles-a-$sfx"; consent_general = $true; consent_version = 'v1' }
Start-Sleep -Milliseconds 1100
$l2 = Call 'POST' '/leads' $admin @{ display_name = "roles lead 2 $sfx"; channel = 'web'; channel_user_id = "roles-b-$sfx"; consent_general = $true; consent_version = 'v1' }
$list = @((Call 'GET' '/leads' $admin $null).Json)
$i1 = [array]::IndexOf(@($list | ForEach-Object { $_.id }), $l1.Json.id)
$i2 = [array]::IndexOf(@($list | ForEach-Object { $_.id }), $l2.Json.id)
Check 'the lead created last is listed before the earlier one' ($i2 -ge 0 -and $i1 -ge 0 -and $i2 -lt $i1)
foreach ($id in @($l1.Json.id, $l2.Json.id)) { [void](Call 'DELETE' "/leads/$id" $admin $null) }

foreach ($u in $a, $b, $s) { [void](Call 'PATCH' "/users/$($u.Id)" $admin @{ active = $false }) }
Write-Host "`n$($script:pass) passed, $($script:fail) failed"
if ($script:fail -gt 0) { exit 1 }

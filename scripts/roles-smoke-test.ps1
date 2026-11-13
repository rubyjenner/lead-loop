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
Check 'campaign shows the creator role' ($cd.created_by_role -eq 'marketing')
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

Write-Host "`n== Approvals: submit time, newest submitted first, role ==" -ForegroundColor Cyan
$ct2 = Call 'POST' "/campaigns/$cid/contents" $a.Token @{ channel = 'line'; body = 'roles smoke second text' }
$tid2 = $ct2.Json.id
Check 'a second draft has no submit time yet' ($ct2.Status -eq 201 -and -not $ct2.Json.submitted_at)
Start-Sleep -Milliseconds 1100
Check 'A submits the second draft' ((Call 'POST' "/contents/$tid2/submit" $a.Token $null).Status -eq 200)
$pend = @((Call 'GET' '/contents?status=pending_approval' $b.Token $null).Json)
$ids = @($pend | ForEach-Object { $_.id })
$p2 = [array]::IndexOf($ids, $tid2); $p1 = [array]::IndexOf($ids, $tid)
Check 'both are pending and the later submission is listed first' ($p2 -ge 0 -and $p1 -ge 0 -and $p2 -lt $p1)
$r2 = $pend[$p2]
Check 'the row has submitted_at and the creator role' ($r2.submitted_at -and $r2.created_by_role -eq 'marketing')

Write-Host "`n== Product change: what it touches ==" -ForegroundColor Cyan
$prod = Call 'POST' '/products' $a.Token @{ name = "roles product $sfx"; description = 'first text'; price = 100 }
Check 'A creates a product (201)' ($prod.Status -eq 201)
$pid2 = $prod.Json.id
$camp2 = Call 'POST' '/campaigns' $a.Token @{ name = "roles-p-$sfx"; goal = 'test'; budget = 100; product_id = $pid2 }
$cid2 = $camp2.Json.id
$k1 = (Call 'POST' "/campaigns/$cid2/contents" $a.Token @{ channel = 'line'; body = 'approved text' }).Json.id
$k2 = (Call 'POST' "/campaigns/$cid2/contents" $a.Token @{ channel = 'line'; body = 'pending text' }).Json.id
[void](Call 'POST' "/contents/$k1/submit" $a.Token $null)
Check 'B approves the first content' ((Call 'POST' "/contents/$k1/approve" $b.Token $null).Status -eq 200)
[void](Call 'POST' "/contents/$k2/submit" $a.Token $null)
$same = Call 'PUT' "/products/$pid2" $a.Token @{ name = "roles product $sfx"; description = 'first text'; price = 100 }
Check 'saving the same product data reports changed=false' ($same.Status -eq 200 -and $same.Json.changed -eq $false)
$chg = Call 'PUT' "/products/$pid2" $a.Token @{ name = "roles product $sfx"; description = 'first text'; price = 120 }
Check 'a price change reports changed=true' ($chg.Status -eq 200 -and $chg.Json.changed -eq $true)
Check 'impact counts the campaign, the approved and the pending content' ($chg.Json.impact.campaigns -eq 1 -and $chg.Json.impact.approved -eq 1 -and $chg.Json.impact.pending -eq 1)
$f1 = Call 'PUT' "/product-facts/$pid2" $a.Token @{ variants = 'S M L'; product_type = ''; after_sales = ''; payment_methods = ''; installment_info = '' }
Check 'changing the facts reports changed=true with the same impact' ($f1.Status -eq 200 -and $f1.Json.changed -eq $true -and $f1.Json.impact.approved -eq 1)
$f2 = Call 'PUT' "/product-facts/$pid2" $a.Token @{ variants = 'S M L'; product_type = ''; after_sales = ''; payment_methods = ''; installment_info = '' }
Check 'saving the same facts reports changed=false' ($f2.Status -eq 200 -and $f2.Json.changed -eq $false)
$cdp = (Call 'GET' "/campaigns/$cid2" $b.Token $null).Json
Check 'the campaign carries product_changed_at after the approval' ($cdp.product_changed_at)
Check 'unknown product -> 404' ((Call 'PUT' '/products/999999999' $admin @{ name = 'x'; description = ''; price = 1 }).Status -eq 404)

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

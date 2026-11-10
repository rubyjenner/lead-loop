<#
 Staff conversation smoke test (needs PUBLIC_CHAT_ENABLED=1 and an admin login). FICTITIOUS DATA ONLY.
 Checks the flow after hand-off on the web chat: the bot goes quiet, customer messages reach the lead page
 (GET /leads/:id/thread), sales answer (POST /leads/:id/reply), the customer page receives the answer
 (GET /public/chat/replies), and stored texts disappear when consent is withdrawn.

   .\scripts\staff-thread-smoke-test.ps1                    # newest campaign
   .\scripts\staff-thread-smoke-test.ps1 -CampaignId 1
 Prod via Caddy: add  -Base http://localhost/api
 Admin password: $env:SMOKE_ADMIN_PASSWORD, or the script asks.
#>
param(
    [string]$Base = 'http://localhost:8080',
    [string]$AdminEmail = 'admin@example.com',
    [int]$CampaignId = 0
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
        $status = [int]$r.StatusCode
        $text = [System.Text.Encoding]::UTF8.GetString($r.RawContentStream.ToArray())
    } catch {
        if (-not $_.Exception.Response) { throw }
        $status = [int]$_.Exception.Response.StatusCode
        $text = if ($_.ErrorDetails -and $_.ErrorDetails.Message) { $_.ErrorDetails.Message } else { '' }
    }
    $json = $null; try { $json = $text | ConvertFrom-Json } catch { }
    [pscustomobject]@{ Status = $status; Json = $json; Text = $text }
}
function NewSession { 'smoke' + [guid]::NewGuid().ToString('N').Substring(0, 20) }

if ($env:SMOKE_ADMIN_PASSWORD) { $pw = $env:SMOKE_ADMIN_PASSWORD }
else {
    $sec = Read-Host "Password for $AdminEmail" -AsSecureString
    $pw = [System.Net.NetworkCredential]::new('', $sec).Password
}
$login = Call 'POST' '/auth/login' $null @{ email = $AdminEmail; password = $pw }
Check 'admin login' ($login.Status -eq 200 -and $login.Json.token)
if ($login.Status -ne 200) { exit 1 }
$token = $login.Json.token

$info = Call 'GET' '/public/chat/info' $null $null
if ($info.Json.enabled -ne $true) { Write-Host 'Public chat is OFF (PUBLIC_CHAT_ENABLED=1 is needed for this test).' -ForegroundColor Red; exit 1 }
if ($CampaignId -le 0) {
    $camps = Call 'GET' '/campaigns' $token $null
    $list = @($camps.Json)
    if ($list.Count -eq 0) { Write-Host 'No campaign found. Run scripts\demo-seed.ps1 first.' -ForegroundColor Red; exit 1 }
    $CampaignId = [int](($list | Sort-Object { [int]$_.id } -Descending | Select-Object -First 1).id)
    Write-Host "Using campaign id $CampaignId"
}

Write-Host "`n== Bot phase: nothing is stored =="
$sid = NewSession
$name = "Smoke Thread $(Get-Date -Format 'HHmmss')"
$cons = Call 'POST' '/public/chat/consent' $null @{ session_id = $sid; display_name = $name; campaign_id = $CampaignId }
Check 'consent accepted' ($cons.Status -eq 200 -and $cons.Json.reply)
$m1 = Call 'POST' '/public/chat/messages' $null @{ session_id = $sid; text = 'ราคาเท่าไหร่ครับ' }
Check 'price question -> bot answers' ($m1.Status -eq 200 -and $m1.Json.reply -and $m1.Json.staff_mode -ne $true)
$leads = Call 'GET' '/leads?channel=web' $token $null
$mine = @($leads.Json) | Where-Object { $_.display_name -eq $name } | Select-Object -First 1
Check 'lead exists' ($null -ne $mine)
if (-not $mine) { Write-Host "`nResult: $pass passed, $fail failed"; exit 1 }
$leadId = [int]$mine.id
$t0 = Call 'GET' "/leads/$leadId/thread" $token $null
Check 'before hand-off no message text is stored' ($t0.Status -eq 200 -and @($t0.Json).Count -eq 0)

Write-Host "`n== Hand-off: the bot goes quiet =="
$m2 = Call 'POST' '/public/chat/messages' $null @{ session_id = $sid; text = 'ขอคุยกับเจ้าหน้าที่ครับ ช่วยโทรกลับด้วย' }
Check 'asking for a person -> bot still gives the hand-off notice' ($m2.Status -eq 200 -and $m2.Json.reply)
$m3 = Call 'POST' '/public/chat/messages' $null @{ session_id = $sid; text = 'ผ่อนได้ไหมครับ' }
Check 'next customer message -> NO bot answer (staff_mode)' ($m3.Status -eq 200 -and -not $m3.Json.reply -and $m3.Json.staff_mode -eq $true)
Check 'public reply hides score/level/lead_id' ($m3.Text -notmatch '"(score|level|lead_id)"')

Write-Host "`n== Sales sees the customer's message =="
$inbox = Call 'GET' '/inbox' $token $null
Check 'inbox counts a lead with unread customer messages' ([int]$inbox.Json.messages_unread -ge 1)
$lst = Call 'GET' '/leads?channel=web' $token $null
$row = @($lst.Json) | Where-Object { [int]$_.id -eq $leadId } | Select-Object -First 1
Check 'lead list shows unread count' ([int]$row.unread_messages -ge 1)
$t1 = Call 'GET' "/leads/$leadId/thread" $token $null
$in1 = @($t1.Json) | Where-Object { $_.dir -eq 'in' }
Check 'thread holds the customer message' (@($in1).Count -eq 1 -and @($in1)[0].body -match 'ผ่อนได้')
$lst2 = Call 'GET' '/leads?channel=web' $token $null
$row2 = @($lst2.Json) | Where-Object { [int]$_.id -eq $leadId } | Select-Object -First 1
Check 'opening the thread marks it as read' ([int]$row2.unread_messages -eq 0)

Write-Host "`n== Sales answers, the customer receives it =="
$e1 = Call 'POST' "/leads/$leadId/reply" $token @{ body = '   ' }
Check 'empty answer -> 400' ($e1.Status -eq 400)
$e2 = Call 'POST' "/leads/$leadId/reply" $token @{ body = ('ก' * 501) }
Check 'answer over 500 chars -> 400' ($e2.Status -eq 400)
$s1 = Call 'POST' "/leads/$leadId/reply" $token @{ body = 'สวัสดีค่ะ เจ้าหน้าที่ยินดีให้บริการค่ะ ผ่อนได้ 3 เดือนค่ะ' }
Check 'sales answer saved (201)' ($s1.Status -eq 201)
$p1 = Call 'GET' "/public/chat/replies?session_id=$sid&after=0" $null $null
Check 'customer page receives the answer' ($p1.Status -eq 200 -and @($p1.Json.replies).Count -eq 1 -and @($p1.Json.replies)[0].body -match 'ผ่อนได้')
Check 'customer never sees the staff e-mail' ($p1.Text -notmatch '@')
$lastId = [int]@($p1.Json.replies)[0].id
$p2 = Call 'GET' "/public/chat/replies?session_id=$sid&after=$lastId" $null $null
Check 'polling with after= returns nothing new' ($p2.Status -eq 200 -and @($p2.Json.replies).Count -eq 0)
$p3 = Call 'GET' "/public/chat/replies?session_id=$(NewSession)&after=0" $null $null
Check 'unknown session gets nothing' ($p3.Status -eq 200 -and @($p3.Json.replies).Count -eq 0)
$p4 = Call 'GET' '/public/chat/replies?session_id=short' $null $null
Check 'bad session id -> 400' ($p4.Status -eq 400)

Write-Host "`n== The conversation continues both ways =="
$m4 = Call 'POST' '/public/chat/messages' $null @{ session_id = $sid; text = 'ขอบคุณครับ ขอเป็นสีดำนะครับ' }
Check 'customer writes again -> still no bot answer' ($m4.Status -eq 200 -and -not $m4.Json.reply)
$t2 = Call 'GET' "/leads/$leadId/thread" $token $null
Check 'thread now has 2 customer messages and 1 answer' (@(@($t2.Json) | Where-Object { $_.dir -eq 'in' }).Count -eq 2 -and @(@($t2.Json) | Where-Object { $_.dir -eq 'out' }).Count -eq 1)

Write-Host "`n== Closing gives the conversation back to the bot =="
$cl = Call 'POST' "/leads/$leadId/status" $token @{ status = 'closed' }
Check 'close the lead' ($cl.Status -eq 200)
$m5 = Call 'POST' '/public/chat/messages' $null @{ session_id = $sid; text = 'ราคาเท่าไหร่ครับ' }
Check 'after closing -> bot answers again' ($m5.Status -eq 200 -and $m5.Json.reply -and $m5.Json.staff_mode -ne $true)

Write-Host "`n== Withdraw deletes the stored texts =="
$wd = Call 'POST' '/public/chat/withdraw' $null @{ session_id = $sid }
Check 'withdraw ok' ($wd.Status -eq 200)
$p5 = Call 'GET' "/public/chat/replies?session_id=$sid&after=0" $null $null
Check 'after withdraw the customer page gets nothing' ($p5.Status -eq 200 -and @($p5.Json.replies).Count -eq 0)
$s3 = Call 'POST' "/leads/$leadId/reply" $token @{ body = 'ทดสอบ' }
Check 'staff cannot write to a withdrawn lead (409)' ($s3.Status -eq 409)
$t3 = Call 'GET' "/leads/$leadId/thread" $token $null
Check 'stored texts were deleted with the consent' ($t3.Status -eq 200 -and @($t3.Json).Count -eq 0)

Write-Host "`n== Cleanup =="
$del = Call 'DELETE' "/leads/$leadId" $token $null
Check 'anonymize the smoke lead' ($del.Status -eq 200)

Write-Host "`nResult: $pass passed, $fail failed"
if ($fail -gt 0) { exit 1 }

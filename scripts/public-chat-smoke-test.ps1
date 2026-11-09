<#
 Public chat + storefront smoke test (no login needed for the public part).
 FICTITIOUS DATA ONLY. Each run uses a new random session id and a display name starting with "Smoke Web".

 Modes:
   .\scripts\public-chat-smoke-test.ps1 -ExpectDisabled            # PUBLIC_CHAT_ENABLED not set: everything must be closed
   .\scripts\public-chat-smoke-test.ps1 -CampaignId 1              # chat on: full flow
   .\scripts\public-chat-smoke-test.ps1 -CampaignId 1 -RateLimit   # LAST: trips the consent limit (8/min per IP)
 Prod via Caddy: add  -Base http://localhost/api
 Admin password (to check the lead in the back office): $env:SMOKE_ADMIN_PASSWORD, or the script asks.
 Without -CampaignId the script uses the newest campaign it finds (needs admin login).
#>
param(
    [string]$Base = 'http://localhost:8080',
    [string]$AdminEmail = 'admin@example.com',
    [int]$CampaignId = 0,
    [switch]$ExpectDisabled,
    [switch]$RateLimit
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
function NewSession { 'smoke' + [guid]::NewGuid().ToString('N').Substring(0, 20) }   # 25 chars, matches [A-Za-z0-9_-]{16,64}

# ---------------------------------------------------------------- disabled mode
if ($ExpectDisabled) {
    Write-Host "== Public chat must be CLOSED (PUBLIC_CHAT_ENABLED not set) =="
    $sid = NewSession
    $info = Call 'GET' '/public/chat/info' $null $null
    Check 'info says enabled:false (200)' ($info.Status -eq 200 -and $info.Json.enabled -eq $false)
    $m = Call 'POST' '/public/chat/messages' $null @{ session_id = $sid; text = 'ทดสอบ' }
    Check 'messages -> 404' ($m.Status -eq 404)
    $c = Call 'POST' '/public/chat/consent' $null @{ session_id = $sid; display_name = 'Smoke Web closed' }
    Check 'consent -> 404' ($c.Status -eq 404)
    $w = Call 'POST' '/public/chat/withdraw' $null @{ session_id = $sid }
    Check 'withdraw -> 404' ($w.Status -eq 404)
    $s = Call 'GET' '/public/shop?campaign_id=1' $null $null
    Check 'storefront -> 404' ($s.Status -eq 404)
    Write-Host "`nResult: $pass passed, $fail failed"
    if ($fail -gt 0) { exit 1 } else { exit 0 }
}

# ---------------------------------------------------------------- admin login (to find the campaign and verify the lead)
if ($env:SMOKE_ADMIN_PASSWORD) { $pw = $env:SMOKE_ADMIN_PASSWORD }
else {
    $sec = Read-Host "Password for $AdminEmail (back-office check)" -AsSecureString
    $pw = [System.Net.NetworkCredential]::new('', $sec).Password
}
$login = Call 'POST' '/auth/login' $null @{ email = $AdminEmail; password = $pw }
Check 'admin login' ($login.Status -eq 200 -and $login.Json.token)
if ($login.Status -ne 200) { exit 1 }
$token = $login.Json.token

if ($CampaignId -le 0) {
    $camps = Call 'GET' '/campaigns' $token $null
    $list = @($camps.Json)
    if ($list.Count -eq 0) { Write-Host 'No campaign found. Run scripts\demo-seed.ps1 first.' -ForegroundColor Red; exit 1 }
    $CampaignId = [int](($list | Sort-Object { [int]$_.id } -Descending | Select-Object -First 1).id)
    Write-Host "Using campaign id $CampaignId"
}

# ---------------------------------------------------------------- storefront
Write-Host "`n== Storefront =="
$info = Call 'GET' "/public/chat/info?campaign_id=$CampaignId" $null $null
Check 'info: enabled + campaign name' ($info.Status -eq 200 -and $info.Json.enabled -eq $true -and $info.Json.campaign_name)
Check 'info: product price is public' ($null -ne $info.Json.product_price)
$shop = Call 'GET' "/public/shop?campaign_id=$CampaignId" $null $null
Check 'shop: product + posts array' ($shop.Status -eq 200 -and $shop.Json.product_name -and $null -ne $shop.Json.posts)
Check 'shop: no private fields (score, lead, budget)' ($shop.Text -notmatch '"(score|level|lead_id|budget)"')
$shop400 = Call 'GET' '/public/shop' $null $null
Check 'shop without campaign_id -> 400' ($shop400.Status -eq 400)
$shop404 = Call 'GET' '/public/shop?campaign_id=999999999' $null $null
Check 'shop unknown campaign -> 404' ($shop404.Status -eq 404)

# ---------------------------------------------------------------- validation
Write-Host "`n== Input validation =="
$bad1 = Call 'POST' '/public/chat/messages' $null @{ session_id = 'short'; text = 'hi' }
Check 'bad session id -> 400' ($bad1.Status -eq 400)
$bad2 = Call 'POST' '/public/chat/messages' $null @{ session_id = (NewSession); text = '   ' }
Check 'empty text -> 400' ($bad2.Status -eq 400)
$bad3 = Call 'POST' '/public/chat/messages' $null @{ session_id = (NewSession); text = ('ก' * 501) }
Check 'text over 500 chars -> 400' ($bad3.Status -eq 400)
$bad4 = Call 'POST' '/public/chat/consent' $null @{ session_id = (NewSession); campaign_id = 999999999 }
Check 'consent with unknown campaign -> 404' ($bad4.Status -eq 404)

# ---------------------------------------------------------------- consent first
Write-Host "`n== Consent before anything is stored =="
$sid = NewSession
$name = "Smoke Web $(Get-Date -Format 'HHmmss')"
$before = Call 'GET' '/leads?channel=web' $token $null
$countBefore = @($before.Json).Count
$m1 = Call 'POST' '/public/chat/messages' $null @{ session_id = $sid; text = 'ราคาเท่าไหร่ครับ' }
Check 'message before consent -> needs_consent' ($m1.Status -eq 200 -and $m1.Json.needs_consent -eq $true -and $m1.Json.reply)
Check 'public reply hides score/level/lead_id' ($m1.Text -notmatch '"(score|level|lead_id)"')
$mid = Call 'GET' '/leads?channel=web' $token $null
Check 'no lead created before consent' (@($mid.Json).Count -eq $countBefore)

# ---------------------------------------------------------------- consent, chat, scoring
Write-Host "`n== Consent, chat, lead creation =="
$cons = Call 'POST' '/public/chat/consent' $null @{ session_id = $sid; display_name = $name; campaign_id = $CampaignId; consent_marketing = $false }
Check 'consent accepted (200)' ($cons.Status -eq 200 -and $cons.Json.reply)
$m2 = Call 'POST' '/public/chat/messages' $null @{ session_id = $sid; text = 'ราคาเท่าไหร่ครับ' }
Check 'ask price -> reply, no consent prompt' ($m2.Status -eq 200 -and $m2.Json.needs_consent -eq $false -and $m2.Json.reply)
Check 'ask price -> reply mentions a number' ($m2.Json.reply -match '\d')
$after = Call 'GET' '/leads?channel=web' $token $null
$mine = @($after.Json) | Where-Object { $_.display_name -eq $name } | Select-Object -First 1
Check 'lead exists in back office (channel web)' ($null -ne $mine)
if (-not $mine) { Write-Host 'Lead not found; cannot continue.' -ForegroundColor Red; Write-Host "`nResult: $pass passed, $fail failed"; exit 1 }
$leadId = [int]$mine.id
$d1 = Call 'GET' "/leads/$leadId" $token $null
Check 'lead linked to the campaign' ($mine.campaign_id -eq $CampaignId)
Check 'lead has a score from the price question' ($d1.Json.score -ge 1 -and $d1.Json.score_breakdown.Count -ge 1)
$score1 = [int]$d1.Json.score

# ---------------------------------------------------------------- hand-off to sales
Write-Host "`n== Hand-off to sales =="
$m3 = Call 'POST' '/public/chat/messages' $null @{ session_id = $sid; text = 'สนใจครับ ขอคุยกับเจ้าหน้าที่ ช่วยโทรกลับด้วย' }
Check 'ask for a human -> reply' ($m3.Status -eq 200 -and $m3.Json.reply)
$d2 = Call 'GET' "/leads/$leadId" $token $null
Check 'score increased' ([int]$d2.Json.score -gt $score1)
Check 'lead level is Hot' ("$($d2.Json.level)" -match '(?i)hot')
Write-Host ("      score {0} -> {1}, level {2}, status {3}" -f $score1, $d2.Json.score, $d2.Json.level, $d2.Json.status)

# ---------------------------------------------------------------- withdraw
Write-Host "`n== Withdraw consent (PDPA) =="
$wd = Call 'POST' '/public/chat/withdraw' $null @{ session_id = $sid }
Check 'withdraw ok' ($wd.Status -eq 200 -and $wd.Json.ok -eq $true)
$m4 = Call 'POST' '/public/chat/messages' $null @{ session_id = $sid; text = 'ยังอยู่ไหม' }
Check 'after withdraw -> asked for consent again' ($m4.Status -eq 200 -and $m4.Json.needs_consent -eq $true)
$d3 = Call 'GET' "/leads/$leadId" $token $null
Check 'back office shows consent_general = false' ($d3.Json.consent_general -eq $false)
$wd2 = Call 'POST' '/public/chat/withdraw' $null @{ session_id = (NewSession) }
Check 'withdraw for unknown session is harmless (200)' ($wd2.Status -eq 200)

# ---------------------------------------------------------------- cleanup of the test lead
Write-Host "`n== Cleanup =="
$del = Call 'DELETE' "/leads/$leadId" $token $null
Check 'anonymize the smoke lead' ($del.Status -eq 200)

# ---------------------------------------------------------------- rate limit (optional, last)
if ($RateLimit) {
    Write-Host "`n== Rate limit (your IP will be limited for about 1 minute) =="
    $hit429 = $false
    for ($i = 1; $i -le 12; $i++) {
        $r = Call 'POST' '/public/chat/consent' $null @{ session_id = (NewSession); display_name = "Smoke Web rl$i"; campaign_id = $CampaignId }
        if ($r.Status -eq 429) { $hit429 = $true; Write-Host "      429 at request $i"; break }
    }
    Check 'consent endpoint returns 429 after too many requests' $hit429
    Write-Host 'Note: lead rows created by this step are mock "Smoke Web rl*" leads; anonymize them from the back office if you want.'
}

Write-Host "`nResult: $pass passed, $fail failed"
if ($fail -gt 0) { exit 1 }

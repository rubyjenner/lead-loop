<#
 LINE webhook smoke test: runs WITHOUT LINE and without a tunnel. It signs fake webhook events with your
 channel secret and posts them to your local API. FICTITIOUS DATA ONLY (fake LINE user ids).
 Replies cannot be delivered (fake reply tokens), so the API log will show "LINE API ... returned 400/401"
 lines. That is expected here. What is checked: signature, consent gate, lead creation, scoring, hand-off, withdraw.

 Needs LINE_CHANNEL_SECRET and LINE_CHANNEL_ACCESS_TOKEN set in .env (any non-empty test token is fine) and
 `docker compose up -d --force-recreate api`.
   $env:SMOKE_LINE_SECRET = '<the same LINE_CHANNEL_SECRET>'
   $env:SMOKE_ADMIN_PASSWORD = '...'
   .\scripts\line-webhook-smoke-test.ps1            # prod via Caddy: -Base http://localhost/api
 With the LINE variables unset, run:  .\scripts\line-webhook-smoke-test.ps1 -ExpectDisabled
#>
param(
    [string]$Base = 'http://localhost:8080',
    [string]$AdminEmail = 'admin@example.com',
    [switch]$ExpectDisabled
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
    try { $r = Invoke-WebRequest @p; $status = [int]$r.StatusCode; $text = [System.Text.Encoding]::UTF8.GetString($r.RawContentStream.ToArray()) }
    catch {
        if (-not $_.Exception.Response) { throw }
        $status = [int]$_.Exception.Response.StatusCode
        $text = if ($_.ErrorDetails -and $_.ErrorDetails.Message) { $_.ErrorDetails.Message } else { '' }
    }
    $json = $null; try { $json = $text | ConvertFrom-Json } catch { }
    [pscustomobject]@{ Status = $status; Json = $json; Text = $text }
}
function Webhook($json, $secret, $badSig) {
    $bytes = [System.Text.Encoding]::UTF8.GetBytes($json)
    $sig = 'AAAA'
    if (-not $badSig) {
        $h = New-Object System.Security.Cryptography.HMACSHA256 (, [System.Text.Encoding]::UTF8.GetBytes($secret))
        $sig = [Convert]::ToBase64String($h.ComputeHash($bytes))
    }
    try {
        $r = Invoke-WebRequest -Method Post -Uri "$Base/public/line/webhook" -Body $bytes -ContentType 'application/json' `
            -Headers @{ 'X-Line-Signature' = $sig } -UseBasicParsing
        [int]$r.StatusCode
    } catch {
        if (-not $_.Exception.Response) { throw }
        [int]$_.Exception.Response.StatusCode
    }
}
function Ev($type, $uid, $extra) {
    $e = @{ type = $type; replyToken = ('0' * 32); timestamp = 1; source = @{ type = 'user'; userId = $uid } }
    foreach ($k in $extra.Keys) { $e[$k] = $extra[$k] }
    (@{ destination = 'Usmoke'; events = @($e) } | ConvertTo-Json -Depth 10 -Compress)
}
function Msg($uid, $text) { Ev 'message' $uid @{ message = @{ type = 'text'; id = '1'; text = $text } } }
function Post($uid, $data) { Ev 'postback' $uid @{ postback = @{ data = $data } } }

if ($ExpectDisabled) {
    Write-Host "== LINE webhook must be CLOSED (LINE variables not set) =="
    $s = Webhook '{"events":[]}' 'whatever' $false
    Check 'webhook -> 404 when LINE is not configured' ($s -eq 404)
    Write-Host "`nResult: $pass passed, $fail failed"; if ($fail) { exit 1 } else { exit 0 }
}

$secret = $env:SMOKE_LINE_SECRET
if (-not $secret) { Write-Host 'Set $env:SMOKE_LINE_SECRET to the same value as LINE_CHANNEL_SECRET.' -ForegroundColor Red; exit 1 }
if ($env:SMOKE_ADMIN_PASSWORD) { $pw = $env:SMOKE_ADMIN_PASSWORD }
else { $sec = Read-Host "Password for $AdminEmail" -AsSecureString; $pw = [System.Net.NetworkCredential]::new('', $sec).Password }
$login = Call 'POST' '/auth/login' $null @{ email = $AdminEmail; password = $pw }
Check 'admin login' ($login.Status -eq 200 -and $login.Json.token)
if ($login.Status -ne 200) { exit 1 }
$tok = $login.Json.token
function LineLeads { @((Call 'GET' '/leads?channel=line' $tok $null).Json) }
$uid = 'U' + [guid]::NewGuid().ToString('N')

Write-Host "`n== Signature =="
Check 'bad signature -> 401' ((Webhook (Msg $uid 'hi') $secret $true) -eq 401)
Check 'valid signature, no events (LINE Verify button) -> 200' ((Webhook '{"destination":"U","events":[]}' $secret $false) -eq 200)

Write-Host "`n== Consent gate =="
$before = @(LineLeads)
Check 'message before consent -> 200' ((Webhook (Msg $uid 'ราคาเท่าไหร่ครับ') $secret $false) -eq 200)
Check 'no lead stored before consent' (@(LineLeads).Count -eq $before.Count)
$grp = Ev 'message' $uid @{ message = @{ type = 'text'; id = '2'; text = 'hi' } }
$grp = $grp -replace '"type":"user"', '"type":"group"'
Check 'group chat ignored -> 200, no lead' ((Webhook $grp $secret $false) -eq 200 -and @(LineLeads).Count -eq $before.Count)
Check 'decline -> 200, still no lead' ((Webhook (Post $uid 'action=decline') $secret $false) -eq 200 -and @(LineLeads).Count -eq $before.Count)

Write-Host "`n== Consent, lead, scoring, hand-off =="
Check 'consent button -> 200' ((Webhook (Post $uid 'action=consent') $secret $false) -eq 200)
$after = @(LineLeads)
$ids = @($before | ForEach-Object { $_.id })
$mine = $after | Where-Object { $ids -notcontains $_.id } | Select-Object -First 1
Check 'lead created, channel line' ($null -ne $mine -and $mine.channel -eq 'line')
if (-not $mine) { Write-Host 'Lead not found; stopping.' -ForegroundColor Red; Write-Host "`nResult: $pass passed, $fail failed"; exit 1 }
$lid = [int]$mine.id
Check 'consent twice does not duplicate the lead' ((Webhook (Post $uid 'action=consent') $secret $false) -eq 200 -and @(LineLeads).Count -eq $before.Count + 1)
[void](Webhook (Msg $uid 'ราคาเท่าไหร่ครับ') $secret $false)
$d1 = Call 'GET' "/leads/$lid" $tok $null
Check 'asking the price gives points' ($d1.Json.score -ge 10)
[void](Webhook (Msg $uid 'สนใจครับ ขอคุยกับเจ้าหน้าที่ ช่วยโทรกลับด้วย') $secret $false)
$d2 = Call 'GET' "/leads/$lid" $tok $null
Check 'asking for a person -> Hot, handed off' ("$($d2.Json.level)" -match '(?i)hot' -and $d2.Json.score -gt $d1.Json.score)
Write-Host ("      score {0} -> {1}, level {2}, status {3}" -f $d1.Json.score, $d2.Json.score, $d2.Json.level, $d2.Json.status)

Write-Host "`n== Withdraw =="
[void](Webhook (Msg $uid 'ถอนความยินยอม') $secret $false)
$d3 = Call 'GET' "/leads/$lid" $tok $null
Check 'withdraw by typing the words' ($d3.Json.consent_general -eq $false)
$score3 = $d3.Json.score
[void](Webhook (Msg $uid 'ราคาเท่าไหร่ครับ') $secret $false)
$d4 = Call 'GET' "/leads/$lid" $tok $null
Check 'after withdrawal messages are not processed' ($d4.Json.score -eq $score3)

Write-Host "`n== Cleanup =="
Check 'anonymize the test lead' ((Call 'DELETE' "/leads/$lid" $tok $null).Status -eq 200)
Write-Host "`nResult: $pass passed, $fail failed"
if ($fail -gt 0) { exit 1 }

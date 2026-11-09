# Public customer chat + storefront: automatic checks against the REAL API (needs a database, Docker running).
# Test data only. Every lead it creates is anonymized at the end.
#
#   Development stack (docker-compose.yml, API on :8080), chat turned on with PUBLIC_CHAT_ENABLED=1 in .env:
#       .\scripts\public-chat-smoke-test.ps1
#   Production-style trial (docker-compose.prod.yml, everything behind Caddy on port 80):
#       .\scripts\public-chat-smoke-test.ps1 -Base http://localhost/api
#   Check the OFF state (PUBLIC_CHAT_ENABLED empty, API restarted):
#       .\scripts\public-chat-smoke-test.ps1 -ExpectDisabled
#   Also try the rate limit (uses up this machine's limit for about a minute):
#       .\scripts\public-chat-smoke-test.ps1 -RateLimit
#
# The consent limit is 8 per minute per IP and one run uses 7, so wait a minute before running it again.
param(
  [string]$Base = 'http://localhost:8080',
  [string]$AdminEmail = 'admin@example.com',
  [switch]$ExpectDisabled,
  [switch]$RateLimit
)
$ErrorActionPreference = 'Stop'
$script:pass = 0; $script:fail = 0

function Check([string]$Name, [bool]$Ok, [string]$Detail = '') {
  if ($Ok) { Write-Host "PASS  $Name" -ForegroundColor Green; $script:pass++ }
  else { Write-Host "FAIL  $Name   $Detail" -ForegroundColor Red; $script:fail++ }
}
function Section([string]$t) { Write-Host ''; Write-Host "== $t ==" -ForegroundColor Cyan }

# One HTTP call. Never throws on 4xx/5xx; returns Status, Json and Text.
function Call([string]$Method, [string]$Path, $Body = $null, [string]$Token = '') {
  $req = @{ Uri = ($Base.TrimEnd('/') + $Path); Method = $Method; UseBasicParsing = $true; ErrorAction = 'Stop' }
  if ($Token) { $req['Headers'] = @{ Authorization = "Bearer $Token" } }
  if ($null -ne $Body) {
    $req['ContentType'] = 'application/json; charset=utf-8'
    $req['Body'] = [System.Text.Encoding]::UTF8.GetBytes(($Body | ConvertTo-Json -Depth 6 -Compress))
  }
  $status = 0; $text = ''
  try { $r = Invoke-WebRequest @req; $status = [int]$r.StatusCode; $text = $r.Content }
  catch {
    $resp = $_.Exception.Response
    if ($null -eq $resp) { throw }
    $status = [int]$resp.StatusCode
    if ($resp.PSObject.Methods.Name -contains 'GetResponseStream') {
      $sr = New-Object System.IO.StreamReader($resp.GetResponseStream(), [System.Text.Encoding]::UTF8); $text = $sr.ReadToEnd(); $sr.Close()
    } else { $text = [string]$_.ErrorDetails.Message }
  }
  $json = $null
  if ($text) { try { $json = $text | ConvertFrom-Json } catch { } }
  [pscustomobject]@{ Status = $status; Json = $json; Text = $text }
}
function NewSid { -join ((1..32) | ForEach-Object { '{0:x}' -f (Get-Random -Maximum 16) }) }
function Keys($o) { if ($null -eq $o) { @() } else { @($o.PSObject.Properties.Name) } }

# ---------- the chat must be OFF by default ----------
if ($ExpectDisabled) {
  Section 'Public chat is switched off'
  $i = Call GET '/public/chat/info'
  Check 'info says enabled = false' ($i.Status -eq 200 -and $i.Json.enabled -eq $false) "status $($i.Status) $($i.Text)"
  $m = Call POST '/public/chat/messages' @{ session_id = (NewSid); text = 'hello' }
  Check 'messages -> 404' ($m.Status -eq 404) "status $($m.Status)"
  $c = Call POST '/public/chat/consent' @{ session_id = (NewSid) }
  Check 'consent -> 404' ($c.Status -eq 404) "status $($c.Status)"
  $s = Call GET '/public/shop?campaign_id=1'
  Check 'shop -> 404' ($s.Status -eq 404) "status $($s.Status)"
  Write-Host ''; Write-Host "$script:pass passed, $script:fail failed"
  if ($script:fail -gt 0) { exit 1 } else { exit 0 }
}

# ---------- admin login (only used to look at the leads the public chat made) ----------
$sec = Read-Host "password of $AdminEmail" -AsSecureString
$pw = [Runtime.InteropServices.Marshal]::PtrToStringAuto([Runtime.InteropServices.Marshal]::SecureStringToBSTR($sec))
$login = Call POST '/auth/login' @{ email = $AdminEmail; password = $pw }
if ($login.Status -ne 200 -or -not $login.Json.token) { Write-Host "admin login failed (status $($login.Status))" -ForegroundColor Red; exit 1 }
$tok = $login.Json.token
Check 'admin login' $true

# a campaign that has a product with a price, so the price answer can be checked
$camps = @((Call GET '/campaigns' $null $tok).Json)
$prods = @((Call GET '/products' $null $tok).Json)
$camp = $null; $prod = $null
foreach ($c in $camps) {
  if ($null -ne $c.product_id) {
    $p = $prods | Where-Object { $_.id -eq $c.product_id -and $_.price -gt 0 } | Select-Object -First 1
    if ($p) { $camp = $c; $prod = $p; break }
  }
}
if (-not $camp) { Write-Host 'no campaign with a priced product found: create one first (demo-seed.ps1 does)' -ForegroundColor Red; exit 1 }
$cid = [int]$camp.id; $price = [double]$prod.price
Write-Host "using campaign $cid ($($camp.name)), product '$($prod.name)' price $price"

$info = Call GET '/public/chat/info'
if (-not ($info.Status -eq 200 -and $info.Json.enabled -eq $true)) {
  Write-Host 'the public chat is OFF. Set PUBLIC_CHAT_ENABLED=1 in .env, then: docker compose up -d' -ForegroundColor Yellow
  Write-Host '(to test the OFF state use -ExpectDisabled)'; exit 2
}

$tag = 'pubtest-' + (Get-Random -Maximum 999999)
$created = New-Object System.Collections.ArrayList
function LeadsNamed([string]$n) { @((Call GET '/leads' $null $tok).Json | Where-Object { $_.display_name -eq $n }) }
function LeadCount { @((Call GET '/leads' $null $tok).Json).Count }

# ---------- 1. what a stranger can see ----------
Section 'Public surface'
$anon = Call GET '/leads'
Check 'private API still needs a login (GET /leads -> 401)' ($anon.Status -eq 401) "status $($anon.Status)"
$ic = Call GET "/public/chat/info?campaign_id=$cid"
Check 'info shows campaign and product price' ($ic.Status -eq 200 -and $ic.Json.campaign_name -and [double]$ic.Json.product_price -eq $price) "status $($ic.Status) $($ic.Text)"
$extra = @(Keys $ic.Json | Where-Object { @('enabled', 'campaign_name', 'product_name', 'product_price') -notcontains $_ })
Check 'info shows nothing else' ($extra.Count -eq 0) ('extra fields: ' + ($extra -join ', '))
$s0 = Call GET '/public/shop'
Check 'shop without campaign_id -> 400' ($s0.Status -eq 400) "status $($s0.Status)"
$s1 = Call GET '/public/shop?campaign_id=999999999'
Check 'shop with unknown campaign -> 404' ($s1.Status -eq 404) "status $($s1.Status)"
$shop = Call GET "/public/shop?campaign_id=$cid"
Check 'shop with campaign -> 200' ($shop.Status -eq 200) "status $($shop.Status)"
$allowed = @('enabled', 'campaign_name', 'product_name', 'description', 'product_price', 'image_url', 'posts')
$extra = @(Keys $shop.Json | Where-Object { $allowed -notcontains $_ })
Check 'shop shows no internal fields (budget, goal, ids...)' ($extra.Count -eq 0) ('extra fields: ' + ($extra -join ', '))
$contents = @((Call GET "/campaigns/$cid/contents" $null $tok).Json)
$okBodies = @($contents | Where-Object { @('approved', 'published') -contains $_.status } | ForEach-Object { $_.body })
$bad = @(@($shop.Json.posts) | Where-Object { $okBodies -notcontains $_ })
Check 'shop shows only approved posts' ($bad.Count -eq 0) ("$($bad.Count) post(s) that are not approved")

# ---------- 2. bad input ----------
Section 'Bad input is refused'
$r = Call POST '/public/chat/messages' @{ session_id = 'short'; text = 'hi' }
Check 'session id too short -> 400' ($r.Status -eq 400) "status $($r.Status)"
$r = Call POST '/public/chat/messages' @{ session_id = (NewSid); text = '   ' }
Check 'empty text -> 400' ($r.Status -eq 400) "status $($r.Status)"
$r = Call POST '/public/chat/messages' @{ session_id = (NewSid); text = ('x' * 501) }
Check 'text over 500 characters -> 400' ($r.Status -eq 400) "status $($r.Status)"
$r = Call POST '/public/chat/consent' @{ session_id = 'short' }
Check 'consent with bad session id -> 400' ($r.Status -eq 400) "status $($r.Status)"
$r = Call POST '/public/chat/consent' @{ session_id = (NewSid); display_name = $tag; campaign_id = 999999999 }
Check 'consent for unknown campaign -> 404' ($r.Status -eq 404) "status $($r.Status)"

# ---------- 3. nothing is stored before consent ----------
Section 'Consent gate'
$sidA = NewSid
$before = LeadCount
$m = Call POST '/public/chat/messages' @{ session_id = $sidA; text = 'ราคาเท่าไหร่คะ' }
Check 'first message gets the consent notice' ($m.Status -eq 200 -and $m.Json.needs_consent -eq $true -and "$($m.Json.reply)" -match 'ยินยอม') "status $($m.Status) $($m.Text)"
$extra = @(Keys $m.Json | Where-Object { @('reply', 'needs_consent') -notcontains $_ })
Check 'answer has only reply and needs_consent (no score, level, lead id)' ($extra.Count -eq 0) ('extra fields: ' + ($extra -join ', '))
if ($before -ge 200) { Write-Host 'SKIP  nothing stored before consent (more than 200 leads, the list cannot show the difference)' -ForegroundColor Yellow }
else { Check 'no lead was stored before consent' ((LeadCount) -eq $before) "before $before, after $(LeadCount)" }

# ---------- 4. consent creates the lead ----------
Section 'Consent and lead'
$nameA = "$tag-A"
$c1 = Call POST '/public/chat/consent' @{ session_id = $sidA; display_name = $nameA; campaign_id = $cid; consent_marketing = $false }
Check 'consent accepted' ($c1.Status -eq 200 -and "$($c1.Json.reply)") "status $($c1.Status) $($c1.Text)"
$la = @(LeadsNamed $nameA)
Check 'lead exists, channel web, no marketing consent, score 0' ($la.Count -eq 1 -and $la[0].channel -eq 'web' -and $la[0].consent_marketing -eq $false -and $la[0].score -eq 0) "found $($la.Count)"
if ($la.Count -ge 1) { [void]$created.Add($la[0].id) }
$leadA = if ($la.Count -ge 1) { $la[0].id } else { 0 }
$null = Call POST '/public/chat/consent' @{ session_id = $sidA; display_name = $nameA; campaign_id = $cid }
Check 'consent again does not create a second lead' (@(LeadsNamed $nameA).Count -eq 1)

$p = Call POST '/public/chat/messages' @{ session_id = $sidA; text = 'ราคาเท่าไหร่คะ' }
$digits = ("$($p.Json.reply)" -replace ',', '')
Check 'price answer comes from the product (no consent prompt)' ($p.Status -eq 200 -and $p.Json.needs_consent -eq $false -and $digits.Contains([string][int]$price)) "reply: $($p.Json.reply)"
$d = Call GET "/leads/$leadA" $null $tok
Check 'lead got points for asking the price' ($d.Json.score -ge 10 -and @($d.Json.score_breakdown).Count -ge 1) "score $($d.Json.score)"

$p = Call POST '/public/chat/messages' @{ session_id = $sidA; text = 'ผ่อนได้ไหมคะ' }
Check 'installments: passed to the team, no invented numbers' ("$($p.Json.reply)" -match 'ทีมงาน' -and "$($p.Json.reply)" -notmatch '\d') "reply: $($p.Json.reply)"

$p = Call POST '/public/chat/messages' @{ session_id = $sidA; text = 'ขอคุยกับเจ้าหน้าที่ ช่วยโทรกลับด้วย' }
Check 'asking for a person: hand-off reply' ($p.Status -eq 200 -and "$($p.Json.reply)" -match 'ทีมงาน') "reply: $($p.Json.reply)"
$d = Call GET "/leads/$leadA" $null $tok
Check 'lead is now hot and waiting for sales' ($d.Json.level -eq 'hot' -and @('qualified', 'handed_off') -contains $d.Json.status) "level $($d.Json.level), status $($d.Json.status)"

# ---------- 5. names are cleaned ----------
Section 'Display name'
$sidB = NewSid
$dirty = 'a' + [char]7 + 'b' + $tag + ('x' * 70)
$null = Call POST '/public/chat/consent' @{ session_id = $sidB; display_name = $dirty; campaign_id = $cid }
$lb = @((Call GET '/leads' $null $tok).Json | Where-Object { $_.display_name -like "ab$tag*" })
Check 'control characters removed and name cut to 60 characters' ($lb.Count -eq 1 -and $lb[0].display_name.Length -eq 60 -and $lb[0].display_name -notmatch '[\x00-\x1f]') "found $($lb.Count)"
foreach ($x in $lb) { [void]$created.Add($x.id) }

# ---------- 6. withdrawing consent ----------
Section 'Withdraw consent'
$w = Call POST '/public/chat/withdraw' @{ session_id = $sidA }
Check 'withdraw accepted' ($w.Status -eq 200 -and $w.Json.ok -eq $true) "status $($w.Status)"
$d = Call GET "/leads/$leadA" $null $tok
Check 'lead shows consent withdrawn' ($d.Json.consent_general -eq $false -and $d.Json.consent_marketing -eq $false)
$m = Call POST '/public/chat/messages' @{ session_id = $sidA; text = 'สวัสดี' }
Check 'after withdrawal the chat asks for consent again' ($m.Json.needs_consent -eq $true)
$w2 = Call POST '/public/chat/withdraw' @{ session_id = (NewSid) }
Check 'withdraw for an unknown session reveals nothing' ($w2.Status -eq 200 -and $w2.Json.ok -eq $true)
$null = Call POST '/public/chat/consent' @{ session_id = $sidA; display_name = $nameA; campaign_id = $cid }
$again = @(LeadsNamed $nameA)
$again = @(LeadsNamed $nameA)
$d2 = Call GET "/leads/$leadA" $null $tok
Check 'consenting again renews the SAME lead (no duplicate)' ($again.Count -eq 1 -and $d2.Json.consent_general -eq $true) "found $($again.Count)"

# ---------- 7. rate limit (optional) ----------
if ($RateLimit) {
  Section 'Rate limit'
  $sidR = NewSid; $hits = 0
  1..45 | ForEach-Object { $x = Call POST '/public/chat/messages' @{ session_id = $sidR; text = 'hi' }; if ($x.Status -eq 429) { $hits++ } }
  Check 'too many messages get 429' ($hits -ge 1) 'no 429 seen'
  Write-Host 'this machine is rate limited for about a minute now' -ForegroundColor Yellow
}

# ---------- cleanup: remove the personal data of the test leads ----------
Section 'Cleanup'
$ok = 0
foreach ($id in $created) { $x = Call DELETE "/leads/$id" $null $tok; if ($x.Status -eq 200) { $ok++ } }
Check "anonymized the test leads ($($created.Count))" ($ok -eq $created.Count) "$ok of $($created.Count)"

Write-Host ''
Write-Host "$script:pass passed, $script:fail failed"
if ($script:fail -gt 0) { exit 1 }

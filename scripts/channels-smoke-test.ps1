# LeadLoop: smoke test for the mock chat channel (mock data only; use a dev database).
# Needs: docker compose up -d, the channels code built in, and db/channels.sql applied.
# This file is pure ASCII (Thai text is sent as \uXXXX escapes) so Windows PowerShell 5.1 reads it correctly.
param([string]$BaseUrl = 'http://localhost:8080', [string]$Email = 'admin@example.com')
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

function CountOf($body) { if ($null -eq $body) { 0 } else { @($body).Count } }

function Msg([string]$ext, [string]$text) {
  Call 'POST' '/channels/mock/messages' ('{"external_id":"' + $ext + '","text":"' + $text + '"}')
}

# Thai test phrases as JSON escapes
$T_PRICE    = '\u0e23\u0e32\u0e04\u0e32\u0e40\u0e17\u0e48\u0e32\u0e44\u0e2b\u0e23\u0e48'     # asks the price
$T_CALLBACK = '\u0e0a\u0e48\u0e27\u0e22\u0e15\u0e34\u0e14\u0e15\u0e48\u0e2d\u0e01\u0e25\u0e31\u0e1a\u0e14\u0e49\u0e27\u0e22'  # asks for a callback
$T_STOP     = '\u0e2b\u0e22\u0e38\u0e14'      # unsubscribe keyword

Write-Host "`n== Login ==" -ForegroundColor Cyan
if ($env:SMOKE_ADMIN_PASSWORD) {
  $plain = $env:SMOKE_ADMIN_PASSWORD   # same variable as smoke-test.ps1
} else {
  $sec = Read-Host 'Admin password' -AsSecureString
  $plain = [Runtime.InteropServices.Marshal]::PtrToStringAuto([Runtime.InteropServices.Marshal]::SecureStringToBSTR($sec))
}
$login = Call 'POST' '/auth/login' (@{ email = $Email; password = $plain } | ConvertTo-Json) -NoAuth
Check 'admin login' ($login.Status -eq 200 -and $login.Body.token)
if ($login.Status -ne 200) {
  Write-Host "Cannot continue: login returned HTTP $($login.Status) (401 = wrong password, 429 = too many failed logins -> docker compose restart api)" -ForegroundColor Red
  exit 1
}
$script:Token = $login.Body.token
$plain = $null

Write-Host "`n== Setup: product 12900 THB + campaign ==" -ForegroundColor Cyan
$tag = [guid]::NewGuid().ToString('N').Substring(0, 8)
$prod = Call 'POST' '/products' ('{"name":"Smoke Phone ' + $tag + '","description":"Used, battery 90%","price":12900,"brand_info":"smoke"}')
$camp = Call 'POST' '/campaigns' ('{"name":"smoke-channel-' + $tag + '","product_id":' + $prod.Body.id + ',"budget":1000}')
Check 'product and campaign created' ($prod.Status -eq 201 -and $camp.Status -eq 201)
$cid = $camp.Body.id

$ext = 'smoke-' + [guid]::NewGuid().ToString('N').Substring(0, 12)
$before = CountOf (Call 'GET' '/leads?channel=mock').Body

Write-Host "`n== Consent first: nothing is stored before the customer accepts ==" -ForegroundColor Cyan
$m = Msg $ext $T_PRICE
Check 'unknown customer gets the consent prompt (200, needs_consent)' ($m.Status -eq 200 -and $m.Body.needs_consent -eq $true -and $m.Body.reply)
$after = CountOf (Call 'GET' '/leads?channel=mock').Body
Check 'no lead was stored before consent' ($after -eq $before)

Write-Host "`n== Customer accepts ==" -ForegroundColor Cyan
$c1 = Call 'POST' '/channels/mock/consent' ('{"external_id":"' + $ext + '","campaign_id":' + $cid + ',"consent_marketing":true}')
Check 'consent creates the lead (created=true)' ($c1.Status -eq 200 -and $c1.Body.created -eq $true -and $c1.Body.lead_id)
$lid = $c1.Body.lead_id
$c2 = Call 'POST' '/channels/mock/consent' ('{"external_id":"' + $ext + '","campaign_id":' + $cid + ',"consent_marketing":true}')
Check 'accepting twice does not create a second lead' ($c2.Status -eq 200 -and $c2.Body.created -eq $false -and $c2.Body.lead_id -eq $lid)
$bad = Call 'POST' '/channels/mock/consent' ('{"external_id":"' + $ext + '","campaign_id":999999999}')
Check 'unknown campaign -> 404' ($bad.Status -eq 404)

Write-Host "`n== Messages: events, score and replies ==" -ForegroundColor Cyan
$m = Msg $ext $T_PRICE
Check 'price question detected as asked_price' ($m.Status -eq 200 -and ($m.Body.detected -contains 'asked_price'))
Check 'reply states the product price from the database' ($m.Body.reply -like '*12,900*')
Check 'score is 10 (cold)' ($m.Body.score -eq 10 -and $m.Body.level -eq 'cold')

$m = Msg $ext 'hello'
Check 'message without keywords: nothing detected, score unchanged' ($m.Status -eq 200 -and @($m.Body.detected).Count -eq 0 -and $m.Body.score -eq 10 -and $m.Body.reply)

$m = Msg $ext $T_CALLBACK
Check 'callback request: immediate handoff, hot' ($m.Status -eq 200 -and $m.Body.immediate_handoff -eq $true -and $m.Body.level -eq 'hot')
Check 'score is 40 (price 10 + callback 30)' ($m.Body.score -eq 40)
$lead = Call 'GET' "/leads/$lid"
Check 'lead is qualified' ($lead.Status -eq 200 -and $lead.Body.status -eq 'qualified')

Write-Host "`n== Unsubscribe keyword ==" -ForegroundColor Cyan
$m = Msg $ext $T_STOP
Check 'unsubscribe detected' ($m.Status -eq 200 -and ($m.Body.detected -contains 'unsubscribed'))
$lead = Call 'GET' "/leads/$lid"
Check 'marketing consent withdrawn' ($lead.Body.consent_marketing -eq $false)

Write-Host "`n== Withdraw general consent: processing stops ==" -ForegroundColor Cyan
$w = Call 'POST' "/leads/$lid/consent" '{"kind":"general","action":"withdrawn","text_version":"v1"}'
Check 'withdraw general consent' ($w.Status -eq 200)
$m = Msg $ext $T_PRICE
Check 'after withdrawal the customer is asked for consent again' ($m.Status -eq 200 -and $m.Body.needs_consent -eq $true)
$lead = Call 'GET' "/leads/$lid"
Check 'score did not change after withdrawal' ($lead.Body.score -eq 40)

Write-Host "`n== Input checks and login ==" -ForegroundColor Cyan
Check 'empty text -> 400' ((Msg $ext '').Status -eq 400)
Check 'missing external_id -> 400' ((Call 'POST' '/channels/mock/messages' '{"text":"x"}').Status -eq 400)
Check 'no token -> 401' ((Call 'POST' '/channels/mock/messages' '{"external_id":"a","text":"x"}' -NoAuth).Status -eq 401)

Write-Host "`n== Anonymize, then the same person starts fresh ==" -ForegroundColor Cyan
Check 'anonymize lead' ((Call 'DELETE' "/leads/$lid").Status -eq 200)
$m = Msg $ext $T_PRICE
Check 'after anonymize: consent prompt again' ($m.Status -eq 200 -and $m.Body.needs_consent -eq $true)
$c3 = Call 'POST' '/channels/mock/consent' ('{"external_id":"' + $ext + '","campaign_id":' + $cid + '}')
Check 'new consent makes a NEW lead' ($c3.Status -eq 200 -and $c3.Body.created -eq $true -and $c3.Body.lead_id -ne $lid)
Call 'DELETE' "/leads/$($c3.Body.lead_id)" | Out-Null

Write-Host "`n$($script:pass) passed, $($script:fail) failed" -ForegroundColor $(if ($script:fail -eq 0) { 'Green' } else { 'Red' })
Write-Host 'Mock leads were anonymized. The smoke-channel product and campaign stay in the dev database.'
if ($script:fail -gt 0) { exit 1 }
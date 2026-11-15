# LeadLoop: smoke test for payment requests (mock data only; use a dev database).
# Needs in .env: PAYMENTS_ENABLED=1 and PROMPTPAY_ID=<a 10-digit phone, e.g. 0812345678>, then
#   docker compose up -d --force-recreate api
# Add -Mock when PAYMENTS_MOCK=1 is also set, to test the "customer paid" path with the fake gateway.
# Checks: a request can only be made for a qualified lead, the amount comes from the product price (or a manual
# amount), one open request per lead, the creator cannot confirm their own request, cancelling, and that
# withdrawing consent cancels an unpaid request. Uses ONE admin account, so the confirm-by-another-person
# success path is covered by -Mock only.
# This file is pure ASCII so Windows PowerShell 5.1 reads it correctly.
param([string]$BaseUrl = 'http://localhost:8080', [string]$Email = 'admin@example.com', [switch]$Mock)
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

Write-Host "`n== Login ==" -ForegroundColor Cyan
if ($env:SMOKE_ADMIN_PASSWORD) {
  $plain = $env:SMOKE_ADMIN_PASSWORD
} else {
  $sec = Read-Host 'Admin password' -AsSecureString
  $plain = [Runtime.InteropServices.Marshal]::PtrToStringAuto([Runtime.InteropServices.Marshal]::SecureStringToBSTR($sec))
}
$login = Call 'POST' '/auth/login' (@{ email = $Email; password = $plain } | ConvertTo-Json) -NoAuth
Check 'admin login' ($login.Status -eq 200 -and $login.Body.token)
if ($login.Status -ne 200) { Write-Host "Cannot continue: HTTP $($login.Status)" -ForegroundColor Yellow; exit 1 }
$script:Token = $login.Body.token

# a product with a price, a campaign for it, and a helper that makes a qualified lead
$prod = Call 'POST' '/products' '{"name":"Pay smoke phone","description":"test","price":12900,"brand_info":"test"}'
$camp = Call 'POST' '/campaigns' (@{ name = 'Pay smoke campaign'; product_id = $prod.Body.id; budget = 100 } | ConvertTo-Json)
function NewQualifiedLead([string]$name) {
  $ext = 'pay-smoke-' + [guid]::NewGuid().ToString('N').Substring(0, 10)
  $body = @{ campaign_id = $camp.Body.id; display_name = $name; channel = 'web'; channel_user_id = $ext;
             consent_general = $true; consent_marketing = $false; consent_version = 'mock-v1' } | ConvertTo-Json
  $r = Call 'POST' '/leads' $body
  if ($r.Status -ne 201 -and $r.Status -ne 200) { throw "create lead failed: HTTP $($r.Status)" }
  [int64]$id = $r.Body.id
  $null = Call 'POST' "/leads/$id/events" '{"type":"requested_human"}'
  $id
}
function PayStatus([int64]$leadId) { @((Call 'GET' "/payments?lead_id=$leadId").Body)[0].status }

Write-Host "`n== A: normal flow ==" -ForegroundColor Cyan
$a = NewQualifiedLead 'Pay smoke A'
Check 'A: lead is qualified' ((Call 'GET' "/leads/$a").Body.status -eq 'qualified')
$p = Call 'POST' "/leads/$a/payments"
if ($p.Status -eq 409 -and "$($p.Body.error)" -like '*not enabled*') {
  Write-Host 'Payments are off. Set PAYMENTS_ENABLED=1 and PROMPTPAY_ID in .env, then docker compose up -d --force-recreate api' -ForegroundColor Yellow
  $null = Call 'DELETE' "/leads/$a"
  exit 1
}
Check 'A: request created (201)' ($p.Status -eq 201)
Check 'A: amount is the product price' ([double]$p.Body.amount -eq 12900 -and $p.Body.amount_source -eq 'product_price')
Check 'A: QR payload looks like PromptPay' ("$($p.Body.qr_payload)".StartsWith('000201') -and "$($p.Body.qr_payload)".Contains('A000000677010111') -and "$($p.Body.qr_payload)".Contains('12900.00'))
$tok = $p.Body.token
$pub = Call 'GET' "/public/pay/$tok" -NoAuth
Check 'A: public page works without login' ($pub.Status -eq 200 -and $pub.Body.status -eq 'pending')
Check 'A: public page has no lead data' (-not ($pub.Body.PSObject.Properties.Name -contains 'lead_id') -and -not ($pub.Body.PSObject.Properties.Name -contains 'display_name'))
Check 'A: unknown token -> 404' ((Call 'GET' '/public/pay/0000000000000000000000000000000000000000deadbeef' -NoAuth).Status -eq 404)
Check 'A: a second open request -> 409' ((Call 'POST' "/leads/$a/payments").Status -eq 409)
Check 'A: creator cannot confirm own request -> 403' ((Call 'POST' "/payments/$($p.Body.id)/confirm").Status -eq 403)
Check 'A: cancel works' ((Call 'POST' "/payments/$($p.Body.id)/cancel").Status -eq 200)
Check 'A: cancelled page shows no QR' ((Call 'GET' "/public/pay/$tok" -NoAuth).Body.status -eq 'cancelled')
Check 'A: confirming a cancelled request -> 409 or 403' ((Call 'POST' "/payments/$($p.Body.id)/confirm").Status -in 403, 409)
$p2 = Call 'POST' "/leads/$a/payments" '{"amount":11900.50,"expires_in_hours":2}'
Check 'A: after cancel a new request with a manual amount works' ($p2.Status -eq 201 -and $p2.Body.amount_source -eq 'manual' -and [double]$p2.Body.amount -eq 11900.5)
Check 'A: bad amount (3 decimals) -> 400' ((Call 'POST' "/leads/$a/payments" '{"amount":10.123}').Status -eq 400)

Write-Host "`n== B: consent withdrawn cancels an unpaid request ==" -ForegroundColor Cyan
$b = NewQualifiedLead 'Pay smoke B'
$pb = Call 'POST' "/leads/$b/payments"
Check 'B: request created' ($pb.Status -eq 201)
$null = Call 'POST' "/leads/$b/consent" '{"kind":"general","action":"withdrawn","text_version":"v1"}'
Check 'B: the request was cancelled by the database trigger' ((PayStatus $b) -eq 'cancelled')
Check 'B: public link no longer open' ((Call 'GET' "/public/pay/$($pb.Body.token)" -NoAuth).Body.status -eq 'cancelled')
Check 'B: no new request for a lead without consent -> 409' ((Call 'POST' "/leads/$b/payments").Status -eq 409)

Write-Host "`n== C: a lead that is not qualified ==" -ForegroundColor Cyan
$ext = 'pay-smoke-' + [guid]::NewGuid().ToString('N').Substring(0, 10)
$nl = Call 'POST' '/leads' (@{ campaign_id = $camp.Body.id; display_name = 'Pay smoke C'; channel = 'web'; channel_user_id = $ext;
        consent_general = $true; consent_marketing = $false; consent_version = 'mock-v1' } | ConvertTo-Json)
$c = [int64]$nl.Body.id
Check 'C: new lead (not qualified) -> 409' ((Call 'POST' "/leads/$c/payments").Status -eq 409)

if ($Mock) {
  Write-Host "`n== D: fake gateway marks it paid (PAYMENTS_MOCK=1) ==" -ForegroundColor Cyan
  $d = NewQualifiedLead 'Pay smoke D'
  $pd = Call 'POST' "/leads/$d/payments"
  $mp = Call 'POST' "/public/pay/$($pd.Body.token)/mock-pay" -NoAuth
  Check 'D: mock pay -> 200' ($mp.Status -eq 200)
  Check 'D: payment is paid' ((PayStatus $d) -eq 'paid')
  Check 'D: the lead is closed' ((Call 'GET' "/leads/$d").Body.status -eq 'closed')
  Check 'D: paying twice -> 409' ((Call 'POST' "/public/pay/$($pd.Body.token)/mock-pay" -NoAuth).Status -eq 409)
  Check 'D: a paid request cannot be cancelled -> 409' ((Call 'POST' "/payments/$($pd.Body.id)/cancel").Status -eq 409)
  $null = Call 'DELETE' "/leads/$d"
  Check 'D: anonymizing the lead keeps the paid record' ((@((Call 'GET' '/payments?status=paid').Body | Where-Object { $_.id -eq $pd.Body.id }).Count) -eq 1)
}

foreach ($id in @($a, $b, $c)) { $null = Call 'DELETE' "/leads/$id" }
Write-Host "`nResult: $($script:pass) passed, $($script:fail) failed"
if ($script:fail -gt 0) { exit 1 }

<#
 Demo data for LeadLoop: 1 product, 1 campaign, 3 mock leads (Cold / Warm / Hot).
 FICTITIOUS DATA ONLY. Run against the development database after `docker compose up -d`.
 Password: set $env:SMOKE_ADMIN_PASSWORD first, or the script asks (hidden input).
 Safe to run more than once: each run adds a new set (names end with the time), nothing is deleted.
#>
param(
    [string]$Base = 'http://localhost:8080',
    [string]$AdminEmail = 'admin@example.com'
)
$ErrorActionPreference = 'Stop'

function Call($method, $path, $body) {
    $p = @{ Method = $method; Uri = "$Base$path"; UseBasicParsing = $true }
    if ($script:token) { $p.Headers = @{ Authorization = "Bearer $script:token" } }
    if ($null -ne $body) {
        $p.ContentType = 'application/json; charset=utf-8'
        $p.Body = [System.Text.Encoding]::UTF8.GetBytes(($body | ConvertTo-Json -Depth 10 -Compress))
    }
    try { $r = Invoke-WebRequest @p } catch {
        $msg = if ($_.ErrorDetails -and $_.ErrorDetails.Message) { $_.ErrorDetails.Message } else { $_.Exception.Message }
        throw "$method $path failed: $msg"
    }
    $text = [System.Text.Encoding]::UTF8.GetString($r.RawContentStream.ToArray())
    if ($text) { return ($text | ConvertFrom-Json) }
}

$pw = $env:SMOKE_ADMIN_PASSWORD
if (-not $pw) {
    $sec = Read-Host "Password for $AdminEmail" -AsSecureString
    $pw = [System.Net.NetworkCredential]::new('', $sec).Password
}
$script:token = (Call 'POST' '/auth/login' @{ email = $AdminEmail; password = $pw }).token
if (-not $script:token) { throw 'login failed' }
$stamp = Get-Date -Format 'HHmm'

Write-Host "`n== Product and campaign (mock) =="
$prod = Call 'POST' '/products' @{
    name = "iPhone 12 (ตัวอย่าง $stamp)"; description = 'มือสอง สภาพ 90% (ข้อมูลจำลอง)'
    price = 12900; brand_info = 'ร้านมือถือตัวอย่าง'
}
$camp = Call 'POST' '/campaigns' @{
    name = "แคมเปญเดโม $stamp"; goal = '50 ลีด'; target_audience = 'นักศึกษา'
    budget = 5000; product_id = $prod.id
}
Write-Host "product id $($prod.id), campaign id $($camp.id)"

function NewLead($name, $budget, $messages) {
    $body = @{
        campaign_id = $camp.id; display_name = $name; channel = 'mock'
        channel_user_id = "demo-$stamp-$([guid]::NewGuid().ToString('N').Substring(0,6))"
        consent_general = $true; consent_marketing = $true; consent_version = 'mock-v1'
    }
    if ($null -ne $budget) { $body.budget = $budget }
    $lead = Call 'POST' '/leads' $body
    foreach ($m in $messages) { [void](Call 'POST' "/leads/$($lead.id)/message" @{ text = $m }) }
    return $lead.id
}

Write-Host "`n== Three mock leads =="
$cold = NewLead "ลูกค้าเดโม Cold $stamp" $null @('ราคาเท่าไหร่ครับ')
$warm = NewLead "ลูกค้าเดโม Warm $stamp" 12900 @('ขอรายละเอียดหน่อย', 'ผ่อนได้ไหมครับ')
$hot  = NewLead "ลูกค้าเดโม Hot $stamp"  12900 @('สนใจครับ ขอคุยกับเจ้าหน้าที่ ช่วยโทรกลับด้วย')

foreach ($id in @($cold, $warm, $hot)) {
    $d = Call 'GET' "/leads/$id" $null
    Write-Host ("lead {0}: {1} | score {2} | level {3} | status {4}" -f $id, $d.display_name, $d.score, $d.level, $d.status)
    foreach ($b in $d.score_breakdown) { Write-Host ("    {0,4}  {1}" -f $b.points, $b.rule) }
}
Write-Host "`nDone. Open Postman or run: Invoke-RestMethod $Base/leads (with the Authorization header)."

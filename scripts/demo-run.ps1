<#
 LeadLoop demo runner: runs every demo step in order and stops after each one,
 so you can take one screenshot per step for the slides.
 FICTITIOUS DATA ONLY. Use with the development database.

 Before running:   docker compose restart api
                   .\scripts\demo-seed.ps1      (creates the mock campaign and 3 mock leads)
 Run:              .\scripts\demo-run.ps1
 Options:          -NoPause (no stops)   -CampaignId 18 (default: newest campaign)

 Password: set $env:SMOKE_ADMIN_PASSWORD first, or the script asks (hidden input).
 Steps 4, 7 and 8 create or change mock data (see DEMO_SCRIPT.md). Run demo-seed.ps1 again before the live demo.
#>
param(
    [string]$Base = 'http://localhost:8080',
    [string]$AdminEmail = 'admin@example.com',
    [int]$CampaignId = 0,
    [switch]$NoPause
)
$ErrorActionPreference = 'Stop'
try { [Console]::OutputEncoding = [System.Text.Encoding]::UTF8 } catch {}

function Api($method, $path, $body, [bool]$auth = $true) {
    $p = @{ Method = $method; Uri = "$Base$path"; UseBasicParsing = $true }
    if ($auth) { $p.Headers = @{ Authorization = "Bearer $script:token" } }
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
        $text = ''
        if ($_.ErrorDetails -and $_.ErrorDetails.Message) { $text = $_.ErrorDetails.Message }
    }
    $json = $null
    if ($text) { try { $json = $text | ConvertFrom-Json } catch {} }
    return [pscustomobject]@{ Status = $status; Json = $json; Text = $text }
}

function Step($n, $title) {
    Clear-Host
    Write-Host ("=== Step {0}: {1} ===" -f $n, $title) -ForegroundColor Yellow
    Write-Host ''
}
function Cmd($text) { Write-Host "PS> $text" -ForegroundColor Cyan }
function Result($r) {
    $color = 'Green'
    if ($r.Status -ge 400) { $color = 'Yellow' }
    Write-Host ("HTTP {0}" -f $r.Status) -ForegroundColor $color
}
function Wait-Shot {
    if ($NoPause) { return }
    Write-Host ''
    [void](Read-Host 'Screenshot now (Win+Shift+S), then press Enter for the next step')
}

# ---------- login (not shown in the slides: clear screen afterwards) ----------
$pw = $env:SMOKE_ADMIN_PASSWORD
$setEnv = $false
if (-not $pw) {
    $sec = Read-Host "Password for $AdminEmail" -AsSecureString
    $pw = [System.Net.NetworkCredential]::new('', $sec).Password
    $env:SMOKE_ADMIN_PASSWORD = $pw   # only for step 9 (leads-smoke-test); removed at the end
    $setEnv = $true
}
$login = Api 'POST' '/auth/login' @{ email = $AdminEmail; password = $pw } $false
if ($login.Status -ne 200 -or -not $login.Json.token) { throw "login failed (HTTP $($login.Status)). If 429: docker compose restart api" }
$script:token = $login.Json.token
$pw = $null

if ($CampaignId -le 0) {
    $camps = @((Api 'GET' '/campaigns' $null).Json)
    if ($camps.Count -eq 0) { throw 'no campaign found: run .\scripts\demo-seed.ps1 first' }
    $CampaignId = ($camps | Measure-Object -Property id -Maximum).Maximum
}
$camp = $CampaignId

# ---------- Step 1 ----------
Step 1 'System is running (API, PostgreSQL, Redis)'
Cmd 'docker compose ps'
try { & docker compose ps } catch { Write-Host '(docker compose not available in this window)' }
Write-Host ''
Cmd "Invoke-RestMethod $Base/health"
$r = Api 'GET' '/health' $null $false
Result $r
Write-Host $r.Text
Wait-Shot

# ---------- Step 2 ----------
Step 2 'No login, no access'
Cmd "Invoke-RestMethod $Base/campaigns        # without a token"
$r = Api 'GET' '/campaigns' $null $false
Result $r
Write-Host $r.Text
Write-Host ''
Cmd "Invoke-RestMethod $Base/campaigns -Headers `$h   # with a token"
$r = Api 'GET' '/campaigns' $null
Result $r
Write-Host ("campaigns returned: {0}" -f @($r.Json).Count)
Wait-Shot

# ---------- Step 3 ----------
Step 3 'AI writes the plan and ad drafts (with fallback)'
Cmd "POST /campaigns/$camp/plan"
$r = Api 'POST' "/campaigns/$camp/plan" $null
Result $r
Write-Host ("generated_by: {0}" -f $r.Json.generated_by)
$t = [string]$r.Json.plan
if ($t.Length -gt 500) { $t = $t.Substring(0, 500) + ' ...' }
Write-Host $t
Write-Host ''
Cmd "POST /campaigns/$camp/generate-contents   {channel: line, count: 2}"
$g = Api 'POST' "/campaigns/$camp/generate-contents" @{ channel = 'line'; count = 2 }
Result $g
Write-Host ("generated_by: {0}" -f $g.Json.generated_by)
foreach ($c in @($g.Json.contents)) { Write-Host '-----'; Write-Host $c.body }
Wait-Shot

# ---------- Step 4 ----------
Step 4 'A person must approve, and not the one who created it'
Cmd "POST /campaigns/$camp/contents   (create a draft)"
$c = Api 'POST' "/campaigns/$camp/contents" @{ channel = 'line'; body = 'ข้อความทดสอบเดโม' }
Result $c
$cid = $c.Json.id
Cmd "POST /contents/$cid/submit"
$s = Api 'POST' "/contents/$cid/submit" $null
Result $s
Cmd "POST /contents/$cid/approve   (the creator approves their own draft)"
$a = Api 'POST' "/contents/$cid/approve" $null
Result $a
Write-Host $a.Text
Write-Host ''
Write-Host 'A different person can approve (covered by the automated tests).'
Wait-Shot

# ---------- Step 5 ----------
Step 5 'No consent, no data stored (PDPA)'
Cmd 'POST /leads   (no consent_general)'
$r = Api 'POST' '/leads' @{ display_name = 'ลูกค้าไม่ยินยอม'; channel = 'mock' }
Result $r
Write-Host $r.Text
Wait-Shot

# ---------- Step 6 ----------
Step 6 'Lead scoring with reasons'
Cmd 'GET /leads   (highest score first)'
$leads = Api 'GET' '/leads' $null
Result $leads
@($leads.Json) | Select-Object id, display_name, score, level, status | Select-Object -First 8 | Format-Table -AutoSize
$top = @($leads.Json)[0]
Cmd ("GET /leads/{0}   (why this score)" -f $top.id)
$d = Api 'GET' "/leads/$($top.id)" $null
Result $d
Write-Host ("{0} | score {1} | {2} | {3}" -f $d.Json.display_name, $d.Json.score, $d.Json.level, $d.Json.status)
@($d.Json.score_breakdown) | Format-Table -AutoSize
Wait-Shot

# ---------- Step 7 ----------
Step 7 'Edit scoring rules: add, test, delete (the mentor request)'
Cmd 'GET /scoring-rules'
$rules = Api 'GET' '/scoring-rules' $null
Result $rules
@($rules.Json) | Select-Object id, name, points, mode, active | Select-Object -First 8 | Format-Table -AutoSize
Write-Host ("total rules: {0}" -f @($rules.Json).Count)
Wait-Shot
Step 7 'Edit scoring rules: add a rule, a lead earns points, delete the rule'
$coldLeads = @(@($leads.Json) | Where-Object { $_.level -eq 'cold' })
if ($coldLeads.Count -gt 0) { $lead = $coldLeads[0] } else { $lead = @($leads.Json)[-1] }
Write-Host ("lead {0} ({1}) score before: {2}" -f $lead.id, $lead.display_name, $lead.score)
Write-Host ''
Cmd 'POST /scoring-rules   {name, condition: {event: asked_color}, points: 5}'
$nr = Api 'POST' '/scoring-rules' @{ name = 'ถามสี (ทดลอง)'; category = 'intent'; condition = @{ event = 'asked_color' }; points = 5; mode = 'once'; active = $true }
Result $nr
$ruleId = $nr.Json.id
if (-not $ruleId) {
    $all = @((Api 'GET' '/scoring-rules' $null).Json)
    $found = $all | Where-Object { $_.name -like 'ถามสี*' } | Select-Object -Last 1
    if ($found) { $ruleId = $found.id }
}
Write-Host ("new rule id: {0}" -f $ruleId)
Cmd ("POST /leads/{0}/events   {{type: asked_color}}" -f $lead.id)
$ev = Api 'POST' "/leads/$($lead.id)/events" @{ type = 'asked_color' }
Result $ev
$d2 = Api 'GET' "/leads/$($lead.id)" $null
Write-Host ("score after: {0}" -f $d2.Json.score)
@($d2.Json.score_breakdown) | Format-Table -AutoSize
Cmd ("DELETE /scoring-rules/{0}" -f $ruleId)
$del = Api 'DELETE' "/scoring-rules/$ruleId" $null
Result $del
Wait-Shot

# ---------- Step 8 ----------
Step 8 'Customer withdraws consent and asks for deletion'
Cmd 'POST /leads   (mock lead with consent)'
$t1 = Api 'POST' '/leads' @{ display_name = 'ลูกค้าทดสอบถอน'; channel = 'mock'; consent_general = $true; consent_marketing = $false; consent_version = 'mock-v1' }
Result $t1
$tid = $t1.Json.id
Cmd ("POST /leads/{0}/consent   {{kind: general, action: withdrawn}}" -f $tid)
$w = Api 'POST' "/leads/$tid/consent" @{ kind = 'general'; action = 'withdrawn'; text_version = 'mock-v1' }
Result $w
Cmd ("POST /leads/{0}/message   (after withdrawal)" -f $tid)
$m = Api 'POST' "/leads/$tid/message" @{ text = 'ขอรายละเอียดหน่อย' }
Result $m
Write-Host $m.Text
Cmd ("DELETE /leads/{0}   (anonymize)" -f $tid)
$x = Api 'DELETE' "/leads/$tid" $null
Result $x
Wait-Shot

# ---------- Step 9 ----------
Step 9 'Automated tests'
Cmd '.\scripts\leads-smoke-test.ps1'
$leadsTest = Join-Path $PSScriptRoot 'leads-smoke-test.ps1'
if (Test-Path $leadsTest) { & $leadsTest -Base $Base -AdminEmail $AdminEmail } else { Write-Host 'leads-smoke-test.ps1 not found' }
if ($setEnv) { Remove-Item Env:SMOKE_ADMIN_PASSWORD -ErrorAction SilentlyContinue }
Write-Host ''
Write-Host 'Done. Run .\scripts\demo-seed.ps1 again before the live demo.' -ForegroundColor Green

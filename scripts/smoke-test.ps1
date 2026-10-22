<#
 LeadLoop smoke test: checks login, roles, password rules, user disable, audit log and the AI flow
 against a running API (docker compose up -d). Windows PowerShell 5.1 and PowerShell 7 both work.

 Usage:
   .\scripts\smoke-test.ps1                      # asks for the admin password
   .\scripts\smoke-test.ps1 -RateLimit           # also checks the login limit (blocks your IP for 15 min;
                                                 # run `docker compose restart api` afterwards)
 The script creates a few users named smoke-*@example.com and disables them at the end.
 Each run makes 2 failed logins on purpose; after 2 runs within 15 minutes run `docker compose restart api`.
 Use a development database only.
#>
param(
    [string]$Base = 'http://localhost:8080',
    [string]$AdminEmail = 'admin@example.com',
    [switch]$RateLimit
)

$ErrorActionPreference = 'Stop'
$script:pass = 0
$script:fail = 0

function Check($Name, $Cond) {
    if ($Cond) { Write-Host "PASS  $Name" -ForegroundColor Green; $script:pass++ }
    else { Write-Host "FAIL  $Name" -ForegroundColor Red; $script:fail++ }
}

# Returns Status (int) and Json (parsed body, or $null)
function Call($Method, $Path, $Token, $Body) {
    $p = @{ Method = $Method; Uri = "$Base$Path"; UseBasicParsing = $true }
    if ($Token) { $p.Headers = @{ Authorization = "Bearer $Token" } }
    if ($null -ne $Body) {
        $p.ContentType = 'application/json; charset=utf-8'
        $p.Body = [System.Text.Encoding]::UTF8.GetBytes(($Body | ConvertTo-Json -Compress))
    }
    $text = ''
    try {
        $r = Invoke-WebRequest @p
        $status = [int]$r.StatusCode
        $text = $r.Content
    } catch {
        if (-not $_.Exception.Response) { throw }
        $status = [int]$_.Exception.Response.StatusCode
        if ($_.ErrorDetails -and $_.ErrorDetails.Message) {
            $text = $_.ErrorDetails.Message
        } else {
            try {
                $sr = New-Object System.IO.StreamReader($_.Exception.Response.GetResponseStream())
                $text = $sr.ReadToEnd()
            } catch { }
        }
    }
    $json = $null
    try { $json = $text | ConvertFrom-Json } catch { }
    [pscustomobject]@{ Status = $status; Json = $json }
}

if ($env:SMOKE_ADMIN_PASSWORD) {
    $adminPw = $env:SMOKE_ADMIN_PASSWORD
} else {
    $sec = Read-Host "Password for $AdminEmail" -AsSecureString
    $adminPw = [Runtime.InteropServices.Marshal]::PtrToStringAuto([Runtime.InteropServices.Marshal]::SecureStringToBSTR($sec))
}
$sfx = -join ((97..122) | Get-Random -Count 6 | ForEach-Object { [char]$_ })
$tempPw = 'Temp-pass-1234'
$newPw = 'New-pass-5678'

Write-Host "`n== Public and unauthenticated =="
Check 'GET /health is public' ((Call 'GET' '/health' $null $null).Status -eq 200)
Check 'no token -> 401' ((Call 'GET' '/campaigns' $null $null).Status -eq 401)
Check 'bad token -> 401' ((Call 'GET' '/campaigns' 'not-a-token' $null).Status -eq 401)

Write-Host "`n== Login =="
$bad = Call 'POST' '/auth/login' $null @{ email = $AdminEmail; password = 'definitely-wrong-password' }
Check 'wrong password -> 401' ($bad.Status -eq 401)
$login = Call 'POST' '/auth/login' $null @{ email = $AdminEmail; password = $adminPw }
Check 'admin login -> 200 with token' ($login.Status -eq 200 -and $login.Json.token)
if ($login.Status -ne 200) { Write-Host 'Cannot continue without admin login.' -ForegroundColor Red; exit 1 }
$admin = $login.Json.token
$me = Call 'GET' '/auth/me' $admin $null
Check '/auth/me is admin' ($me.Json.role -eq 'admin')
$adminId = $me.Json.id

# creates a user, checks the first-login rule, changes the password, returns @{ Id; Email; Token }
function NewUser($Role) {
    $email = "smoke-$Role-$sfx@example.com"
    $c = Call 'POST' '/users' $admin @{ email = $email; password = $tempPw; role = $Role }
    Check "admin creates $Role user" ($c.Status -eq 201 -and $c.Json.must_change_password -eq $true)
    $l = Call 'POST' '/auth/login' $null @{ email = $email; password = $tempPw }
    $t = $l.Json.token
    $blocked = Call 'GET' '/campaigns' $t $null
    Check "$Role must change password first (403)" ($blocked.Status -eq 403 -and $blocked.Json.code -eq 'password_change_required')
    $same = Call 'POST' '/auth/password' $t @{ current_password = $tempPw; new_password = $tempPw }
    Check "$Role cannot reuse the temporary password (400)" ($same.Status -eq 400)
    $wrong = Call 'POST' '/auth/password' $t @{ current_password = 'wrong-current-pw'; new_password = $newPw }
    Check "$Role wrong current password rejected (400)" ($wrong.Status -eq 400)
    $ch = Call 'POST' '/auth/password' $t @{ current_password = $tempPw; new_password = $newPw }
    Check "$Role changes password (200 + new token)" ($ch.Status -eq 200 -and $ch.Json.token)
    Check "$Role old token stops working (401)" ((Call 'GET' '/campaigns' $t $null).Status -eq 401)
    [pscustomobject]@{ Id = $c.Json.id; Email = $email; Token = $ch.Json.token }
}

Write-Host "`n== Users, first login, password change =="
$mk = NewUser 'marketing'
$sales = NewUser 'sales'
Check 'weak password (<8) rejected on create' ((Call 'POST' '/users' $admin @{ email = "smoke-weak-$sfx@example.com"; password = 'short'; role = 'sales' }).Status -eq 400)
Check 'duplicate email -> 409' ((Call 'POST' '/users' $admin @{ email = $mk.Email; password = $tempPw; role = 'sales' }).Status -eq 409)

Write-Host "`n== Role table =="
Check 'sales can read campaigns' ((Call 'GET' '/campaigns' $sales.Token $null).Status -eq 200)
Check 'sales cannot create campaign (403)' ((Call 'POST' '/campaigns' $sales.Token @{ name = 'x' }).Status -eq 403)
Check 'sales cannot list users (403)' ((Call 'GET' '/users' $sales.Token $null).Status -eq 403)
Check 'sales cannot read audit log (403)' ((Call 'GET' '/audit-log' $sales.Token $null).Status -eq 403)
Check 'marketing cannot list users (403)' ((Call 'GET' '/users' $mk.Token $null).Status -eq 403)
Check 'marketing cannot change roles (403)' ((Call 'PATCH' "/users/$($mk.Id)" $mk.Token @{ role = 'admin' }).Status -eq 403)
Check 'admin can list users' ((Call 'GET' '/users' $admin $null).Status -eq 200)

Write-Host "`n== Campaign and content flow (marketing) =="
$camp = Call 'POST' '/campaigns' $mk.Token @{ name = "smoke-$sfx"; goal = '10 leads'; budget = 1000 }
Check 'marketing creates campaign (201)' ($camp.Status -eq 201)
$cid = $camp.Json.id
$cont = Call 'POST' "/campaigns/$cid/contents" $mk.Token @{ channel = 'line'; body = 'smoke test content' }
Check 'marketing creates content (draft, generated_by human)' ($cont.Status -eq 201 -and $cont.Json.status -eq 'draft' -and $cont.Json.generated_by -eq 'human')
$ctid = $cont.Json.id
Check 'submit content' ((Call 'POST' "/contents/$ctid/submit" $mk.Token $null).Status -eq 200)
Check 'sales cannot approve (403)' ((Call 'POST' "/contents/$ctid/approve" $sales.Token $null).Status -eq 403)
Check 'creator cannot approve own content (403)' ((Call 'POST' "/contents/$ctid/approve" $mk.Token $null).Status -eq 403)
Check 'a different person (admin) approves' ((Call 'POST' "/contents/$ctid/approve" $admin $null).Status -eq 200)
$list = Call 'GET' "/campaigns/$cid/contents" $mk.Token $null
$approved = @($list.Json) | Where-Object { $_.id -eq $ctid }
Check 'approved_by comes from the logged-in approver' ($approved.approved_by -eq $AdminEmail -and $approved.created_by -eq $mk.Email)
Check 'approving twice -> 409' ((Call 'POST' "/contents/$ctid/approve" $admin $null).Status -eq 409)

Write-Host "`n== AI (falls back to template when no key or provider is down) =="
$plan = Call 'POST' "/campaigns/$cid/plan" $mk.Token $null
Check 'plan returns text and generated_by' ($plan.Status -eq 200 -and $plan.Json.plan -and $plan.Json.generated_by)
$gen = Call 'POST' "/campaigns/$cid/generate-contents" $mk.Token @{ channel = 'line'; count = 2 }
Check 'generate-contents returns 2 drafts' ($gen.Status -eq 201 -and @($gen.Json.contents).Count -eq 2)
Check 'generated drafts are marked with their source' (@('claude', 'gemini', 'template') -contains $gen.Json.generated_by)
Check 'sales cannot call AI (403)' ((Call 'POST' "/campaigns/$cid/plan" $sales.Token $null).Status -eq 403)
Write-Host "      AI source used: $($gen.Json.generated_by)"

Write-Host "`n== Disable, re-enable, reset password =="
Check 'admin disables marketing user' ((Call 'PATCH' "/users/$($mk.Id)" $admin @{ active = $false }).Status -eq 200)
Check 'disabled user token rejected at once (401)' ((Call 'GET' '/campaigns' $mk.Token $null).Status -eq 401)
Check 'disabled user cannot log in (401)' ((Call 'POST' '/auth/login' $null @{ email = $mk.Email; password = $newPw }).Status -eq 401)
Check 'admin re-enables user' ((Call 'PATCH' "/users/$($mk.Id)" $admin @{ active = $true }).Status -eq 200)
Check 'old token stays invalid after re-enable (401)' ((Call 'GET' '/campaigns' $mk.Token $null).Status -eq 401)
$again = Call 'POST' '/auth/login' $null @{ email = $mk.Email; password = $newPw }
Check 'user can log in again after re-enable' ($again.Status -eq 200)
Check 'role change takes effect at once' ((Call 'PATCH' "/users/$($sales.Id)" $admin @{ role = 'marketing' }).Status -eq 200 -and (Call 'POST' '/campaigns' $sales.Token @{ name = "smoke2-$sfx" }).Status -eq 201)
Check 'invalid role rejected (400)' ((Call 'PATCH' "/users/$($sales.Id)" $admin @{ role = 'boss' }).Status -eq 400)
Check 'unknown user -> 404' ((Call 'PATCH' '/users/99999999' $admin @{ active = $false }).Status -eq 404)
Check 'admin resets a password' ((Call 'POST' "/users/$($sales.Id)/reset-password" $admin @{ new_password = $tempPw }).Status -eq 200)
Check 'reset invalidates the old token (401)' ((Call 'GET' '/campaigns' $sales.Token $null).Status -eq 401)
$rl = Call 'POST' '/auth/login' $null @{ email = $sales.Email; password = $tempPw }
Check 'after reset the user must change the password again' ($rl.Status -eq 200 -and $rl.Json.must_change_password -eq $true)
# Only safe to try when this admin really is the only active admin (otherwise the call would succeed)
$admins = @((Call 'GET' '/users' $admin $null).Json | Where-Object { $_.role -eq 'admin' -and $_.active })
if ($admins.Count -eq 1) {
    Check 'last active admin cannot be disabled (409)' ((Call 'PATCH' "/users/$adminId" $admin @{ active = $false }).Status -eq 409)
    Check 'last active admin cannot be demoted (409)' ((Call 'PATCH' "/users/$adminId" $admin @{ role = 'sales' }).Status -eq 409)
} else {
    Write-Host '      SKIP  last-admin checks (more than one active admin exists)' -ForegroundColor Yellow
}

Write-Host "`n== Audit log =="
$audit = Call 'GET' '/audit-log?limit=200' $admin $null
$actions = @($audit.Json | ForEach-Object { $_.action })
Check 'audit log readable by admin' ($audit.Status -eq 200)
foreach ($a in 'login_success', 'login_failed', 'user_created', 'user_updated', 'password_changed', 'password_reset', 'content_approved', 'content_approve_denied') {
    Check "audit log has $a" ($actions -contains $a)
}
Check 'audit log never stores passwords' (-not (($audit.Json | ConvertTo-Json -Depth 5) -match [regex]::Escape($newPw)))

# clean up: disable the smoke users
foreach ($u in $mk, $sales) { [void](Call 'PATCH' "/users/$($u.Id)" $admin @{ active = $false }) }

if ($RateLimit) {
    Write-Host "`n== Login rate limit (blocks this IP for 15 minutes) =="
    $codes = 1..7 | ForEach-Object { (Call 'POST' '/auth/login' $null @{ email = "nobody-$sfx@example.com"; password = 'wrong-password-x' }).Status }
    Write-Host "      status codes: $($codes -join ' ')"
    Check 'login is rate limited (429 appears)' ($codes -contains 429)
    Write-Host 'Run: docker compose restart api   (resets the counters)' -ForegroundColor Yellow
}

Write-Host "`n$($script:pass) passed, $($script:fail) failed"
if ($script:fail -gt 0) { exit 1 }

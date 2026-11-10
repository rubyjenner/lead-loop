<#
 Product edit smoke test (PUT /products/:id).
 FICTITIOUS DATA ONLY. Creates its own test product ("Smoke Edit ...") and removes nothing else.
 The original values of the product are never touched: the script edits only the product it creates.

   $env:SMOKE_ADMIN_PASSWORD = 'Admin123!'
   .\scripts\product-edit-smoke-test.ps1
 Prod via Caddy: add  -Base http://localhost/api
#>
param(
    [string]$Base = 'http://localhost:8080',
    [string]$AdminEmail = 'admin@example.com'
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
function FindProduct($token, $id) {
    @((Call 'GET' '/products' $token $null).Json) | Where-Object { $_.id -eq $id } | Select-Object -First 1
}

# ---------------------------------------------------------------- login
if ($env:SMOKE_ADMIN_PASSWORD) { $pw = $env:SMOKE_ADMIN_PASSWORD }
else {
    $sec = Read-Host "Password for $AdminEmail" -AsSecureString
    $pw = [Runtime.InteropServices.Marshal]::PtrToStringAuto([Runtime.InteropServices.Marshal]::SecureStringToBSTR($sec))
}
$login = Call 'POST' '/auth/login' $null @{ email = $AdminEmail; password = $pw }
Check 'admin login' ($login.Status -eq 200 -and $login.Json.token)
if ($login.Status -ne 200) { exit 1 }
$token = $login.Json.token

# ---------------------------------------------------------------- create a throw-away product
Write-Host "`n== Setup: a new test product =="
$stamp = Get-Date -Format 'HHmmss'
$create = Call 'POST' '/products' $token @{ name = "Smoke Edit $stamp"; description = 'ของทดสอบ'; price = 100; brand_info = 'แบรนด์ทดสอบ' }
Check 'create test product (201)' ($create.Status -eq 201 -and $create.Json.id)
if ($create.Status -ne 201) { exit 1 }
$id = $create.Json.id

# ---------------------------------------------------------------- edit
Write-Host "`n== Edit name, description, price =="
$put = Call 'PUT' "/products/$id" $token @{ name = "Smoke Edit $stamp v2"; description = 'รายละเอียดใหม่'; price = 250.5; brand_info = 'แบรนด์ทดสอบ' }
Check 'edit -> 200 ok' ($put.Status -eq 200 -and $put.Json.ok -eq $true)
$p = FindProduct $token $id
Check 'name changed' ($p.name -eq "Smoke Edit $stamp v2")
Check 'description changed' ($p.description -eq 'รายละเอียดใหม่')
Check 'price changed (250.5)' ([double]$p.price -eq 250.5)
Check 'brand_info kept' ($p.brand_info -eq 'แบรนด์ทดสอบ')

Write-Host "`n== Name is trimmed; price can be cleared =="
[void](Call 'PUT' "/products/$id" $token @{ name = "  Smoke Edit $stamp v3  "; description = ''; price = $null; brand_info = '' })
$p = FindProduct $token $id
Check 'name trimmed' ($p.name -eq "Smoke Edit $stamp v3")
Check 'price cleared -> null' ($null -eq $p.price)
Check 'description may be empty' ($p.description -eq '')

Write-Host "`n== Product facts are not touched by an edit =="
[void](Call 'PUT' "/product-facts/$id" $token @{ variants = 'ไซส์ M'; product_type = ''; after_sales = ''; payment_methods = 'โอน'; installment_info = '' })
[void](Call 'PUT' "/products/$id" $token @{ name = "Smoke Edit $stamp v4"; description = 'x'; price = 10; brand_info = '' })
$f = @((Call 'GET' '/product-facts' $token $null).Json) | Where-Object { $_.id -eq $id } | Select-Object -First 1
Check 'facts still there after product edit' ($f.variants -eq 'ไซส์ M' -and $f.payment_methods -eq 'โอน')

# ---------------------------------------------------------------- validation
Write-Host "`n== Input validation =="
Check 'empty name -> 400' ((Call 'PUT' "/products/$id" $token @{ name = '   '; description = 'x'; price = 10 }).Status -eq 400)
Check 'negative price -> 400' ((Call 'PUT' "/products/$id" $token @{ name = 'x'; description = 'x'; price = -1 }).Status -eq 400)
Check 'bad id -> 400' ((Call 'PUT' '/products/abc' $token @{ name = 'x'; price = 1 }).Status -eq 400)
Check 'unknown product -> 404' ((Call 'PUT' '/products/999999999' $token @{ name = 'x'; price = 1 }).Status -eq 404)
$p = FindProduct $token $id
Check 'failed edits changed nothing' ($p.name -eq "Smoke Edit $stamp v4" -and [double]$p.price -eq 10)

# ---------------------------------------------------------------- auth
Write-Host "`n== Access =="
Check 'no token -> 401' ((Call 'PUT' "/products/$id" $null @{ name = 'x'; price = 1 }).Status -eq 401)

# ---------------------------------------------------------------- audit log
Write-Host "`n== Audit log =="
$audit = Call 'GET' '/audit-log?limit=50' $token $null
if ($audit.Status -eq 200) {
    $hit = @($audit.Json) | Where-Object { $_.action -eq 'product_update' -and $_.target -eq "product:$id" } | Select-Object -First 1
    Check 'product_update is in the audit log' ($null -ne $hit)
} else {
    Write-Host '      (GET /audit-log not available here: check the audit log page by hand)' -ForegroundColor Yellow
}

Write-Host "`nResult: $pass passed, $fail failed"
Write-Host "Test product left behind: id $id (name starts with 'Smoke Edit'). Delete it by hand if you do not want it in the list." -ForegroundColor Yellow
if ($fail -gt 0) { exit 1 }

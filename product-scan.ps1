$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $Root

function Fail([string]$Message) {
    Write-Error "[ProdSplat] $Message"
    exit 1
}

if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
    Fail "Docker Desktop is not installed or docker.exe is not in PATH."
}
try { docker compose version | Out-Null } catch { Fail "Docker Compose is unavailable." }
try { docker info | Out-Null } catch { Fail "Docker Desktop is not running." }

if (-not (Test-Path ".env")) {
    Copy-Item ".env.example" ".env"
    $bytes = New-Object byte[] 32
    [System.Security.Cryptography.RandomNumberGenerator]::Fill($bytes)
    $token = [Convert]::ToHexString($bytes).ToLowerInvariant()
    $content = Get-Content ".env" -Raw
    $content = $content.Replace("replace-with-random-token", $token)
    Set-Content ".env" $content -NoNewline
    Write-Host "[ProdSplat] Created .env with a random internal token."
}

if ((Get-Content ".env" -Raw).Contains("replace-with-random-token")) {
    Fail "Replace INTERNAL_TOKEN in .env."
}

New-Item -ItemType Directory -Force -Path "workspace\jobs" | Out-Null
New-Item -ItemType Directory -Force -Path "workspace\tasks" | Out-Null
New-Item -ItemType Directory -Force -Path "workspace\runtime" | Out-Null

Write-Host "[ProdSplat] Building and starting services..."
docker compose up -d --build
if ($LASTEXITCODE -ne 0) { Fail "docker compose up failed." }

$port = "8080"
$match = Select-String -Path ".env" -Pattern '^APP_PORT=(.+)$' | Select-Object -Last 1
if ($match) { $port = $match.Matches[0].Groups[1].Value.Trim() }
$readyUrl = "http://127.0.0.1:$port/ready"
$appUrl = "http://127.0.0.1:$port/app/"

Write-Host "[ProdSplat] Waiting for app + GPU worker readiness..."
$ready = $false
for ($i=0; $i -lt 180; $i++) {
    try {
        Invoke-WebRequest -Uri $readyUrl -UseBasicParsing -TimeoutSec 2 | Out-Null
        $ready = $true
        break
    } catch { Start-Sleep -Seconds 1 }
}

if (-not $ready) {
    docker compose ps
    docker compose logs --tail=250
    Fail "Timed out waiting for readiness."
}

Write-Host "[ProdSplat] Ready: $appUrl"
Start-Process $appUrl
Write-Host "[ProdSplat] Ctrl+C stops log following; containers remain running."
docker compose logs -f

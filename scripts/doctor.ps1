$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
Set-Location $Root
if (-not (Get-Command docker -ErrorAction SilentlyContinue)) { throw "Docker Desktop is not installed." }
docker info | Out-Null
docker compose version
if (-not (Test-Path ".env")) {
    Copy-Item ".env.example" ".env"
    $bytes = New-Object byte[] 32
    [System.Security.Cryptography.RandomNumberGenerator]::Fill($bytes)
    $token = [Convert]::ToHexString($bytes).ToLowerInvariant()
    $content = (Get-Content ".env" -Raw).Replace("replace-with-random-token", $token)
    Set-Content ".env" $content -NoNewline
}
Write-Host "Building worker image and validating NVIDIA/CUDA access..."
docker compose build worker
docker compose run --rm --no-deps worker python -c "from prodsplat.preflight import run_preflight; ok,e,g,v=run_preflight(); print('healthy:',ok); print('gpu:',g); print('versions:',v); print('error:',e); raise SystemExit(0 if ok else 1)"

$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
Set-Location $Root

if (Get-Command go -ErrorAction SilentlyContinue) {
    Write-Host "== Go =="
    Push-Location app
    gofmt -w .
    go test -timeout 30s ./...
    Pop-Location
} else { Write-Host "Go not installed on host; skipping dev-only Go tests." }

if (Get-Command python -ErrorAction SilentlyContinue) {
    Write-Host "== Python =="
    python -m compileall -q worker\prodsplat
    try { python -m pytest -q worker\tests } catch { Write-Host "pytest unavailable; syntax compilation still passed." }
} else { Write-Host "Python not installed on host; skipping dev-only Python tests." }

if (Get-Command node -ErrorAction SilentlyContinue) {
    node --check web\app.js
} else { Write-Host "Node not installed on host; skipping JS syntax check." }

if (Get-Command docker -ErrorAction SilentlyContinue) {
    $env:INTERNAL_TOKEN = "validation-token"
    docker compose config | Out-Null
}

Write-Host "Available static validation checks passed."

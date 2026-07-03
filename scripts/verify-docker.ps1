# scripts/verify-docker.ps1
# Verifica end-to-end el ciclo Docker del server SAI.
#
# Uso: .\scripts\verify-docker.ps1
param(
  [string]$AdminEmail    = "admin@sai.local",
  [string]$AdminPassword = "Test#2026",
  [int]$HealthTimeout    = 30
)

Set-Location (Join-Path $PSScriptRoot "..")

Write-Host "=== SAI Docker verify ===" -ForegroundColor Cyan
Write-Host "Admin:    $AdminEmail"
Write-Host "Password: $AdminPassword"
Write-Host ""

# 1. Build
Write-Host "[1/6] docker compose build server"
docker compose build server 2>&1 | Select-Object -Last 5

# 2. Up
Write-Host "[2/6] docker compose up -d"
docker compose up -d postgres server

# 3. Esperar /health
Write-Host "[3/6] Esperando /api/v1/health (timeout ${HealthTimeout}s)…"
$healthy = $false
for ($i = 1; $i -le $HealthTimeout; $i++) {
  $r = docker compose exec -T server /app/sai-server --healthcheck 2>&1
  if ($LASTEXITCODE -eq 0) {
    Write-Host "      healthcheck OK (t=${i}s)" -ForegroundColor Green
    $healthy = $true
    break
  }
  Start-Sleep -Seconds 1
}
if (-not $healthy) {
  Write-Host "      healthcheck FAILED tras ${HealthTimeout}s" -ForegroundColor Red
  Write-Host ""
  Write-Host "      Logs del server:"
  docker compose logs --tail=80 server
  exit 1
}

# 4. Bootstrap admin
Write-Host "[4/6] docker compose exec server --bootstrap"
docker compose exec -T server /app/sai-server --bootstrap `
    --admin-email $AdminEmail `
    --admin-password $AdminPassword 2>&1 | Select-Object -Last 5

# 5. Compilar smoketest
Write-Host "[5/6] go build ./cmd/smoketest"
go build -o bin/smoketest.exe ./cmd/smoketest

# 6. Smoke test
Write-Host "[6/6] cmd/smoketest contra el server"
$env:SAI_URL = "http://localhost:8080"
$env:SAI_ADMIN_EMAIL = $AdminEmail
$env:SAI_ADMIN_PASSWORD = $AdminPassword
& ".\bin\smoketest.exe"
$smokeExit = $LASTEXITCODE

if ($smokeExit -eq 0) {
  Write-Host ""
  Write-Host "=== verify OK ===" -ForegroundColor Green
} else {
  Write-Host ""
  Write-Host "=== verify FAILED (smoketest exit=$smokeExit) ===" -ForegroundColor Red
  Write-Host "Logs del server:"
  docker compose logs --tail=80 server
}
exit $smokeExit
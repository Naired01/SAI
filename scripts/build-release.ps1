# scripts/build-release.ps1
# Compila todos los binarios localmente y deja los assets en ./dist/
#
# Uso:  .\scripts\build-release.ps1 -Version "0.1.0"
param(
  [string]$Version = "dev"
)

$Commit = (git rev-parse --short HEAD 2>$null)
if (-not $Commit) { $Commit = "unknown" }
$BuildTime = (Get-Date -AsUTC -Format "yyyy-MM-ddTHH:mm:ssZ")

$Ldflags = "-s -w -X github.com/Naired01/SAI/internal/version.Version=$Version " +
           "-X github.com/Naired01/SAI/internal/version.Commit=$Commit " +
           "-X github.com/Naired01/SAI/internal/version.BuildTime=$BuildTime"

New-Item -ItemType Directory -Force -Path dist | Out-Null
New-Item -ItemType Directory -Force -Path bin | Out-Null

function Build([string]$Target, [string]$Out) {
  Write-Host "→ Building $Out"
  $env:CGO_ENABLED = "0"
  $env:GOOS = ($Target.Split('-')[0])
  $env:GOARCH = ($Target.Split('-')[1])
  go build -trimpath -ldflags="$Ldflags" -o $Out ./cmd/$($args[0])
}

# Sai-server
Write-Host "→ Building sai-server (linux/amd64)"
$env:GOOS="linux"; $env:GOARCH="amd64"; $env:CGO_ENABLED="0"
go build -trimpath -ldflags="$Ldflags" -o bin/sai-server-linux-amd64.exe ./cmd/server

# Agent cross-compile
foreach ($t in @("windows-amd64 .exe","linux-amd64 ","linux-arm64 ","darwin-amd64 ","darwin-arm64 ")) {
  $parts = $t -split " "
  $goos = $parts[0].Split('-')[0]
  $goarch = $parts[0].Split('-')[1]
  $sfx = $parts[1]
  Write-Host "→ Building sai-agent ($goos/$goarch)"
  $env:GOOS = $goos
  $env:GOARCH = $goarch
  $env:CGO_ENABLED = "0"
  go build -trimpath -ldflags="$Ldflags" -o "dist/sai-agent-${goos}-${goarch}${sfx}.exe" ./cmd/agent
}

# Agent-installer (host platform)
$env:GOOS = $null; $env:GOARCH = $null; $env:CGO_ENABLED = "0"
go build -trimpath -ldflags="$Ldflags" -o bin/sai-agent-installer.exe ./cmd/agent-installer

Write-Host ""
Write-Host "Build complete. Artifacts:"
Get-ChildItem bin | Select-Object Name, Length | Format-Table
Get-ChildItem dist | Select-Object Name, Length | Format-Table
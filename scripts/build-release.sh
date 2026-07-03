#!/usr/bin/env bash
# scripts/build-release.sh
# Compila todos los binarios localmente y deja los assets en ./dist/
#
# Uso:  ./scripts/build-release.sh [VERSION]
set -euo pipefail

VERSION="${1:-dev}"
COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

LDFLAGS="-s -w -X github.com/Naired01/SAI/internal/version.Version=${VERSION} \
         -X github.com/Naired01/SAI/internal/version.Commit=${COMMIT} \
         -X github.com/Naired01/SAI/internal/version.BuildTime=${BUILD_TIME}"

mkdir -p dist
mkdir -p bin

echo "→ Building sai-server (linux/amd64)"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="${LDFLAGS}" -o bin/sai-server-linux-amd64 ./cmd/server

echo "→ Building sai-server (linux/arm64)"
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="${LDFLAGS}" -o bin/sai-server-linux-arm64 ./cmd/server

for target in "windows amd64 .exe" "linux amd64" "linux arm64" "darwin amd64" "darwin arm64"; do
  set -- $target
  os="$1"; arch="$2"; sfx="$3"
  echo "→ Building sai-agent (${os}/${arch})"
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
    go build -trimpath -ldflags="${LDFLAGS}" \
    -o "dist/sai-agent-${os}-${arch}${sfx}" ./cmd/agent
done

echo "→ Building sai-agent-installer (local platform)"
go build -trimpath -ldflags="${LDFLAGS}" -o bin/sai-agent-installer ./cmd/agent-installer

echo
echo "Build complete. Artifacts:"
ls -lh bin/
ls -lh dist/
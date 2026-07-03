# SAI

[![Version](https://img.shields.io/badge/version-0.1.0-blue.svg)](https://github.com/Naired01/SAI/releases)
[![Go](https://img.shields.io/badge/go-1.22+-00ADD8.svg)](https://go.dev)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)

**SAI** — Sistema de Administración de Equipos para TI.

Agente liviano que corre como servicio nativo en el equipo + servidor central con panel
web para visibilidad, gestión de procesos/servicios, comandos remotos, inventario
de hardware/software, trabajos masivos y auditoría.

- **Conexión WSS reversa** (el agente inicia; atraviesa NAT/firewalls sin configuración del usuario final).
- **Enrolamiento por token** generado desde el panel admin; bundle pre-configurado descargable.
- **Panel web** en React + Vite + i18next (Español por defecto, Inglés).
- **Auditoría inmutable** con filtros; hash-chain preparado para activarse en Fase 10.
- **Auto-hospedado**: corre en tu infraestructura (Docker o binario nativo).
- **Distribución BETA** vía GitHub Releases + imagen `ghcr.io/naired01/sai`.

> 📄 El plan detallado (contexto, decisiones, roadmap y checklist por fase) está en [PLAN.md](PLAN.md).
> Es un documento vivo: consúltalo antes de empezar cualquier trabajo nuevo.

---

## Quickstart (desarrollo)

Requisitos: Go 1.22+, Node 20+, Docker (recomendado) o PostgreSQL 16 local.

```bash
# 1. Levantar Postgres
docker compose up -d postgres

# 2. Variables de entorno
cp deploy/.env.example deploy/.env
# editar deploy/.env con valores seguros

# 3. Backend (modo dev)
go run ./cmd/server --bootstrap --admin-email admin@sai.local --admin-password 'CambiaEsto#2026'

# 4. Panel (modo dev, en otra terminal)
cd web && npm install && npm run dev

# 5. Abrir
#   Backend:    http://localhost:8080
#   Panel Vite: http://localhost:5173
```

## Quickstart (Docker)

```bash
cp deploy/.env.example deploy/.env
# editar deploy/.env
docker compose up -d --build
docker compose exec server /app/sai-server --bootstrap \
    --admin-email admin@sai.local \
    --admin-password 'CambiaEsto#2026'

# Panel: http://localhost:8080
```

## Estructura

```
cmd/         server, agent, agent-installer (binarios)
internal/    api, auth, agents, tokens, groups, templates,
             jobs, audit, ws, bundles, dashboard, db, config, i18n
pkg/         saiclient (SDK Go, Fase 8)
web/         panel admin (React + Vite + i18next)
deploy/      Dockerfile, docker-compose, migraciones, .env.example
dist/        binarios base del agente (los baja CI)
scripts/     build-release.sh/.ps1
```

## Releases

Las releases BETA se publican vía GitHub Actions al pushear tags `v*+beta*`:

```bash
# Última BETA
VERSION=$(curl -s https://api.github.com/repos/Naired01/SAI/releases/latest | jq -r .tag_name)

# Server
curl -L -O "https://github.com/Naired01/SAI/releases/download/${VERSION}/sai-server-linux-amd64"
chmod +x sai-server-linux-amd64

# Agente (Windows)
curl -L -O "https://github.com/Naired01/SAI/releases/download/${VERSION}/sai-agent-windows-amd64.exe"

# Container
docker run -d --name sai -p 8080:8080 \
    -e SAI_PUBLIC_URL=https://sai.tudominio.com \
    -e SAI_DB_URL=postgres://... \
    -e SAI_JWT_SECRET=$(openssl rand -hex 32) \
    ghcr.io/naired01/sai:${VERSION}
```

## Licencia

MIT — ver [LICENSE](LICENSE).
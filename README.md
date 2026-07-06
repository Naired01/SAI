# SAI

[![Version](https://img.shields.io/badge/version-0.1.0-blue.svg)](https://github.com/Naired01/SAI/releases)
[![Go](https://img.shields.io/badge/go-1.25+-00ADD8.svg)](https://go.dev)
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
- **Distribución de releases** vía GitHub Releases + imagen `ghcr.io/naired01/sai` (tags `v*`).

> 📄 El plan detallado (contexto, decisiones, roadmap y checklist por fase) está en [PLAN.md](PLAN.md).
> Es un documento vivo: consúltalo antes de empezar cualquier trabajo nuevo.

---

## Quickstart (desarrollo)

Requisitos: Go 1.25+, Node 20+, Docker (recomendado) o PostgreSQL 16 local.

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

> **`--bootstrap` es idempotente**:
> - DB vacía → crea el admin.
> - Email ya existe → resetea el password (útil para recuperación).
> - Email distinto al existente → error. Usa `--force-reset` para reemplazar (¡borra el admin previo!).

### Bootstrap en primer arranque (env vars) vs `--bootstrap` (CLI)

Hay **dos mecanismos independientes** y no hacen lo mismo:

| Mecanismo | Cuándo se ejecuta | Qué pasa tras ejecutarse | Cuándo usarlo |
|---|---|---|---|
| `SAI_BOOTSTRAP_EMAIL` + `SAI_BOOTSTRAP_PASSWORD` en `.env` | Cada arranque del server, como un paso normal de startup (STEP 6) | Sólo si la tabla `users` está **vacía** crea el admin. Si ya hay usuarios, **no hace nada** (no resetea contraseñas). El server **continúa arrancando** siempre. | Provisionar el primer admin al levantar un stack nuevo. |
| `docker compose exec server /app/sai-server --bootstrap --admin-email X --admin-password Y` | Cuando el operador lo corre manualmente dentro del container | Crea o resetea el admin (reglas idempotentes de la tabla de arriba) **y sale del proceso**. No arranca el servidor. | Rotar/recuperar contraseñas, o reemplazar admin existente con `--force-reset`. |

> **Importante**: dejar `SAI_BOOTSTRAP_*` en el `.env` después del primer
> arranque es seguro e inerte. No se reaplica, no resetea credenciales, y no
> causa reinicios del container (este era el bug de "loop infinito" que se
> daba cuando ambos caminos compartían la rama que hacía `exit` tras bootstrap).

## Limitaciones conocidas (Fase 1)

Estas son funcionalidades cuyo modelo y UI existen pero que aún no están
operativas hasta fases posteriores. **No las reportes como bug** — el plan
las cerrará en versiones futuras.

| Limitación | Estado | Cuando se cierra | Detalle |
|---|---|---|---|
| Ejecución real de comandos | Los jobs se crean y aparecen en la UI (`/jobs`) pero los items quedan en `pending` | **Fase 3** | Llegará con los mensajes WS `command` / `command_result` y el dispatcher real sobre el mismo hub. El panel ya muestra el aviso `jobs.phase_notice`. |
| Inventario HW/SW | Las tabs `Hardware` y `Software` en `AgentDetail` muestran "Disponible en una fase posterior" | **Fase 2** | Mensajes WS `inventory_request` / `inventory_snapshot` + storage + UI. |
| Validación de JWT por-agente | El server emite `session_jwt` en el welcome, pero **no lo valida aún**: cada reconexión re-usa el enrollment token. El agent no persiste el JWT. | **Fase 3** | Firma con el secreto único de `agent_credentials` para revocación granular. |
| Hash-chain de auditoría | La tabla `audit_events` ya tiene `prev_hash` y `hash` (migración 0002), pero `audit.Record` no las popula. | **Fase 10** | Hardening. |

Para el detalle completo de la deuda técnica ver
[`PLAN.md` §13-bis "Deuda técnica conocida — Fase 1"](PLAN.md).

## Verificación end-to-end

**Opción automática (Docker):** corre build → up → healthcheck → bootstrap → smoke test en un solo comando:

```bash
./scripts/verify-docker.sh        # bash
.\scripts\verify-docker.ps1       # PowerShell
```

**Opción manual:** una vez el server esté corriendo (Docker o local), podés correr el smoke test que valida 14 endpoints incluyendo auth, CSRF, CRUD y logout:

```bash
# Compilar
go build -o bin/smoketest ./cmd/smoketest

# Ejecutar (asume server en :8080 con admin@sai.local / Test#2026)
SAI_URL=http://localhost:8080 \
SAI_ADMIN_EMAIL=admin@sai.local \
SAI_ADMIN_PASSWORD='CambiaEsto#2026' \
./bin/smoketest
```

Salida esperada:

```
[1-5]   health / version / auth/me sin sesión / login / auth/me autenticado -> OK
[6]     dashboard/summary  -> KPIs: online=0 offline=0 tokens=1 jobs=0
[7-13]  tokens, agents, groups, templates (6 builtin), audit, logout -> OK
[14]    auth/me post-logout -> 401 OK
Resultado: 14/14 tests passed
```

## Diagnóstico del server (startup + panics)

El server loggea cada paso del arranque con `step=N/name` para identificar exactamente dónde falla:

```
INFO msg="sai-server startup" step=0/init version=... pid=...
INFO msg="startup step" step=1/config msg="loading configuration"
INFO msg="startup ok" step=1/config env=development bind=:8080 ...
INFO msg="startup step" step=2/db_open msg="connecting to postgres"
INFO msg="startup ok" step=2/db_open msg="postgres pool ready"
... 13/logging/listen ...
INFO msg="startup complete" step=13/listen msg="READY: sai-server is listening" addr=:8080
```

Cualquier panic durante el arranque se vuelca con stack trace a stderr **y** a `crash.log`:

- `/var/log/sai/crash.log` (Docker)
- `./sai-server.crash.log` (local)

Subí ambos archivos cuando reportes un bug.

## Obtener e instalar agentes

Los **agentes** son binarios livianos (~6 MB) que corren como servicio nativo en cada equipo administrado. Se distribuyen como ZIP preconfigurados con `server_url` y `enrollment_token` ya embebidos.

### Flujo end-to-end

```
[Panel] Admin → Tokens → [+] Crear token → recibe 6 URLs (Windows/Linux/macOS × amd64/arm64)
   │
   ▼ el admin elige la URL de la plataforma correcta
[Equipo destino] Descarga ZIP → descomprime → ejecuta install.ps1 / install.sh
   │
   ▼ (al iniciar)
[Agente] WSS hello → server canjea token → crea agent_id → registra
   │
   ▼
[Panel] Aparece en Agentes con estado "online"
```

### Paso a paso

1. **Login en el panel** con tu admin (`http://<server>:8080`).
2. **Menú Tokens** (icono de llave en la sidebar) → **+ Nuevo token**.
3. Completá:
   - `Label`: nombre descriptivo (ej. "Laptop Juan Pérez")
   - `Max uses`: 1 para un solo equipo, N si vas a usar el mismo link en varios
   - `TTL hours`: ventana de tiempo para que el agente se conecte (ej. 24)
4. Click **Crear token** → aparece un diálogo con:
   - **Plain token** (con botón copiar) — **copialo ya**, no se vuelve a mostrar.
   - **Grid de 6 URLs de descarga**, una por cada combinación OS × arch soportada (Windows/Linux/macOS × amd64/arm64). La plataforma detectada del navegador se resalta con badge "Detectado" pero podés elegir cualquiera.
5. Click en el botón de la plataforma del equipo destino → el browser descarga `sai-agent-{os}-{arch}.zip`. Si necesitás instalar en otro OS, copiá el link correspondiente y mandáselo al usuario.
6. **En el equipo destino**, ejecutá:

**Windows** (PowerShell como Administrador):
```powershell
Expand-Archive -Path sai-agent-windows-amd64.zip
cd sai-agent-windows-amd64
.\install.ps1
# Verificar: Get-Service sai-agent
# Logs:     Get-Content C:\Logs\SAI\*.log -Tail
```

**Linux** (root):
```bash
unzip sai-agent-linux-amd64.zip
cd sai-agent-linux-amd64
sudo ./install.sh
# Verificar: systemctl status sai-agent
# Logs:     sudo journalctl -u sai-agent -f
```

**macOS** (root):
```bash
unzip sai-agent-darwin-amd64.zip
cd sai-agent-darwin-amd64
sudo ./install.sh
# Verificar: sudo launchctl list | grep com.sai.agent
```

7. **El agente se conecta solo** al server, se autentica con el token, y aparece en **Agentes** del panel.

### Vía CLI (sin panel)

Para air-gapped o automatización masiva:

```bash
# 1. Crear token vía API
SAI_TOKEN=$(curl -sb cookies.txt -c cookies.txt \
  -H "Content-Type: application/json" -H "X-CSRF-Token: $CSRF" \
  -d '{"label":"batch-1","max_uses":50,"ttl_hours":48}' \
  http://server:8080/api/v1/tokens | jq -r .plain)

# 2. Generar bundle sin tocar el server
go run ./cmd/agent-installer \
  --server wss://server.example.com/api/v1/agent/ws \
  --token "$SAI_TOKEN" \
  --os windows --arch amd64 \
  --out bundle-laptop.zip
```

### De dónde sale el binario

- **Docker**: la imagen `ghcr.io/naired01/sai:vX.Y.Z` ya incluye los 6 binarios del agente en `/app/dist/` (los compila en el stage `go` del Dockerfile).
- **Local dev**: `./scripts/build-release.sh` cross-compila los 6 targets a `dist/`.
- **GitHub Releases**: cada tag publica los 6 binarios como assets del release.
- **Tag Docker `latest`**: cada release BETA/stable también taggea `ghcr.io/naired01/sai:latest`.

### Troubleshooting del download

| Error | Causa | Solución |
|---|---|---|
| `binary_not_available` (404) | `dist/` no tiene el binario para esa plataforma | Reconstruí la imagen Docker (los binarios van embebidos) o corré `./scripts/build-release.sh` |
| `invalid_params` (400) | Falta `?os=` o `?arch=` en la URL | Ambos son obligatorios. Usá los links que genera el panel |
| `invalid_token` (403) | Token expirado / agotado / revocado | Creá uno nuevo en el panel |
| `missing token` (400) | Falta el query param `token=` | El link del panel ya lo incluye; si lo armás a mano, agregá `?token=XXX` |

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

Las releases se publican vía GitHub Actions al pushear tags que comienzan con `v` (ej: `v0.1.0`):

```bash
# Última release
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
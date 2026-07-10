# SAI — Plan de Implementación v1.1

> Sistema de administración remota para equipos de TI, auto-hospedado.
> Documento vivo: incluye contexto para nuevos agentes + checklist de progreso por fase.
>
> **Stack**: Go 1.25+ (chi + gorilla/websocket + pgx) + PostgreSQL 16 + React/Vite/i18next (panel) + Docker + GitHub Actions.
> **Estado**: 🟢 Fases 0–3 cerradas · DT-1..DT-5 cerradas · próximo: Fase 4 (scheduled jobs + retry).
> **Repo**: `github.com/Naired01/SAI` · **Imagen**: `ghcr.io/naired01/sai` · **i18n**: Español (default) + Inglés.

---

## 0. Contexto del proyecto

### Problema
El equipo de TI necesita visibilidad y control sobre las máquinas que administra (escritorios, servidores, laptops remotos). Hoy no existe un sistema unificado; se hace con herramientas dispersas (RDP manual, scripts sueltos, consultas verbales al usuario).

### Solución
**SAI** (Sistema de Administración de Equipos):
- **Agente** liviano que corre como servicio nativo (Windows: `sai-agent` service, Linux: systemd, macOS: launchd).
- **Servidor central** que recibe conexiones WSS reversas, expone API REST + WebSocket, y sirve el panel.
- **Enrolamiento por token** que se canjea por `agent_id` + credencial de sesión.
- **Bundle pre-configurado** generado server-side: el admin descarga un ZIP con el binario del agente, `config.json` con el server URL + token, y script de instalación.
- **Grupos jerárquicos** para organizar la flota (con subgrupos y grupo virtual "Sin catalogar").
- **Plantillas de comando reutilizables** ejecutables masivamente desde Dashboard o por agente individual.
- **Auditoría inmutable** de todos los movimientos con filtros y export CSV.

### Capacidades objetivo

| Capacidad | Disponible desde |
|---|---|
| Enrolamiento + heartbeat + visibilidad online/offline | **Fase 1** |
| Grupos jerárquicos (con subgrupos y "Sin catalogar") | **Fase 1** |
| Plantillas de comando (CRUD, ejecución real) | **Fase 1 (CRUD) / Fase 3 (ejecución)** |
| Trabajos masivos (modelo, UI, cancel) | **Fase 1 (modelo+UI) / Fase 3 (dispatch real)** |
| Auditoría (tabla + UI + filtros + export) | **Fase 1** |
| Dashboard (KPIs + problemas + quick actions) | **Fase 1** |
| Inventario HW/SW | Fase 2 |
| Comandos remotos reales (ejecutados por el agente) | Fase 3 |
| Gestión procesos/servicios | Fase 3 |
| Scheduled jobs + retry | Fase 4 |
| Transferencia de archivos | Fase 5 |
| Terminal interactiva | Fase 6 |
| Políticas GPO-like | Fase 7 |
| API tokens + OpenAPI + SDK | Fase 8 |
| Anti-tamper + auto-update firmado | Fase 9 |
| Hardening (CSP, audit hash-chain) | Fase 10 |

### Decisiones de diseño

| # | Decisión | Elegido | Por qué |
|---|---|---|---|
| 1 | Relación con ZentinelMesh | **Proyecto nuevo e independiente** | SAI es marca propia; sin reuso de paquetes |
| 2 | Stack backend + agente | **Go 1.25+** | Ecosistema maduro, cross-compile trivial win/linux/mac |
| 3 | DB | **PostgreSQL 16** con `pgx` directo + migraciones embebidas (`embed.FS`) | Tipos ricos (UUID, JSONB, INET), particiones |
| 4 | Auth agentes | **JWT por-agente** firmado por server, secret único en `agent_credentials` | Replay-resistant, revocable, sin certificados |
| 5 | Conexión agente↔server | **WSS reverso** (agente inicia) | NAT/firewall transparente |
| 6 | Generación del bundle | **Server ensambla ZIP** (binario base + `config.json` + install script) por request | Sin compilación en runtime; binarios base vienen de GH Releases |
| 7 | Panel admin | **React 18 + Vite 5 + TypeScript + i18next + react-router + TanStack Query** servido por backend en `/` | Un proceso; sin CORS |
| 8 | Idioma | **Español por defecto + Inglés** desde el inicio | `Accept-Language` en backend; i18next en panel |
| 9 | Bootstrap admin | Flag `--bootstrap` + env vars | Sin usuario inicial en DB |
| 10 | Distribución de releases | **GitHub Actions** publica binarios + imagen `ghcr.io/naired01/sai` en tags `v*` | Coherente con el monorepo del usuario |
| 11 | TLS | **Reverse proxy** (NPM) en prod, HTTP plano en dev | Mismo patrón que ZentinelMesh |
| 12 | Visibilidad agente | `visible`/`invisible` configurable por agente desde panel | Hardening real en Fase 9 |

---

## 1. Arquitectura de alto nivel

```
            ┌─────────────────────────────────────────┐
            │     Equipo del usuario final            │
            │                                         │
            │   ┌─────────────────────────────┐       │
            │   │  sai-agent (servicio)       │       │
            │   │  • WSS client (reverse)     │       │
            │   │  • heartbeat 30s            │       │
            │   │  • ejecuta comandos (Fase 3)│      │
            │   │  • config: %ProgramData%\   │       │
            │   │    SAI\config.json          │       │
            │   └─────────────┬───────────────┘       │
            └─────────────────┼───────────────────────┘
                              │ WSS (reverse)
                              ▼
┌──────────────────────────────────────────────────────────────────────┐
│                  SAI Server (Go, monolito)                           │
│                                                                      │
│  ┌──────────────┐    ┌───────────────┐    ┌───────────────────────┐  │
│  │  HTTP API    │    │  WSS Hub      │    │  Bundle Builder      │  │
│  │  (chi)       │    │  (gorilla/ws) │    │  (zip bin+config+    │  │
│  │  /api/v1/*   │    │  /api/v1/     │    │   install script)    │  │
│  │  /           │    │   agent/ws    │    │                       │  │
│  │  (panel SPA) │    │               │    │  /api/v1/agents/      │  │
│  └──────┬───────┘    └───────┬───────┘    │   download            │  │
│         │                    │            └───────────┬───────────┘  │
│         └────────────────────┼────────────────────────┘              │
│                              │                                       │
│                  ┌───────────▼────────────┐                          │
│                  │ internal/services       │                          │
│                  │ auth agents tokens      │                          │
│                  │ groups templates jobs   │                          │
│                  │ audit dashboard ws      │                          │
│                  │ bundles i18n            │                          │
│                  └───────────┬────────────┘                          │
│                              │                                       │
└──────────────────────────────┼───────────────────────────────────────┘
                               │ pgx
                               ▼
                       ┌──────────────┐
                       │  PostgreSQL  │
                       │      16      │
                       └──────────────┘

      Admin (navegador) ──HTTPS──▶ Nginx Proxy Manager ──▶ SAI Server
```

---

## 2. Estructura del repositorio

```
SAI/
├── README.md
├── PLAN.md                       ← este documento
├── LICENSE
├── .gitignore
├── .editorconfig
├── go.mod
├── go.sum
├── docker-compose.yml
├── Dockerfile
├── deploy/
│   ├── .env.example
│   └── migrations/               ← SQL versionado (numerado)
├── cmd/
│   ├── server/                   ← servidor (HTTP + WSS + UI)
│   ├── agent/                    ← agente (servicio nativo)
│   └── agent-installer/          ← CLI offline para generar bundle
├── internal/
│   ├── api/                      ← router chi + middleware
│   ├── auth/                     ← argon2id, JWT admin, sesiones, CSRF
│   ├── agents/                   ← registro, catálogo, eventos
│   ├── tokens/                   ← enrollment tokens
│   ├── groups/                   ← grupos jerárquicos
│   ├── templates/                ← plantillas de comando + seed builtin
│   ├── jobs/                     ← trabajos (modelo + UI; dispatcher real en Fase 3)
│   ├── audit/                    ← Record() + handlers
│   ├── dashboard/                ← summary endpoint
│   ├── ws/                       ← hub WSS de agentes
│   ├── bundles/                  ← ensamblador ZIP
│   ├── i18n/                     ← backend messages (es/en)
│   ├── config/                   ← carga de env vars
│   ├── db/                       ← pool pgx + migraciones embebidas
│   └── version/                  ← Version, Commit, BuildTime
├── pkg/
│   └── saiclient/                ← SDK Go (Fase 8)
├── web/                          ← panel admin (React + Vite + i18next)
├── dist/                         ← binarios base del agente (CI los baja)
├── scripts/
│   ├── build-release.sh
│   └── build-release.ps1
└── .github/
    └── workflows/
        ├── ci.yml                ← lint + test + build
        └── release.yml           ← binarios + imagen en tag v* (+ workflow_dispatch)
```

---

## 3. Esquema de base de datos

### Migración 0001_init.sql (Fase 1, base)

```sql
CREATE EXTENSION IF NOT EXISTS pgcrypto;     -- gen_random_uuid()
CREATE EXTENSION IF NOT EXISTS citext;

CREATE TABLE users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email         CITEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    totp_secret   TEXT,
    role          TEXT NOT NULL CHECK (role IN ('admin','operator','viewer')),
    is_active     BOOLEAN NOT NULL DEFAULT TRUE,
    last_login_at TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE sessions (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    csrf_token  TEXT NOT NULL,
    user_agent  TEXT,
    ip          INET,
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_sessions_user ON sessions(user_id);
CREATE INDEX idx_sessions_expires ON sessions(expires_at);

CREATE TABLE enrollment_tokens (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    token_hash   TEXT UNIQUE NOT NULL,
    label        TEXT NOT NULL,
    created_by   UUID NOT NULL REFERENCES users(id),
    max_uses     INT NOT NULL DEFAULT 1,
    uses         INT NOT NULL DEFAULT 0,
    expires_at   TIMESTAMPTZ,
    revoked_at   TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_tokens_hash ON enrollment_tokens(token_hash);

CREATE TABLE agents (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    hostname      TEXT NOT NULL,
    os            TEXT NOT NULL,
    os_version    TEXT,
    arch          TEXT,
    agent_version TEXT,
    enrollment_id UUID REFERENCES enrollment_tokens(id),
    labels        JSONB NOT NULL DEFAULT '{}'::jsonb,
    visibility    TEXT NOT NULL DEFAULT 'visible' CHECK (visibility IN ('visible','invisible')),
    last_seen_at  TIMESTAMPTZ,
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_agents_last_seen ON agents(last_seen_at);

CREATE TABLE agent_credentials (
    agent_id   UUID PRIMARY KEY REFERENCES agents(id) ON DELETE CASCADE,
    jwt_secret TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    rotated_at TIMESTAMPTZ
);

CREATE TABLE agent_events (
    id         BIGSERIAL PRIMARY KEY,
    agent_id   UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    type       TEXT NOT NULL,
    payload    JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_events_agent_time ON agent_events(agent_id, created_at DESC);
```

### Migración 0002_groups_templates_jobs_audit.sql (Fase 1, ampliado)

```sql
-- Grupos jerárquicos
CREATE TABLE agent_groups (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    parent_id   UUID REFERENCES agent_groups(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    description TEXT,
    color       TEXT,
    icon        TEXT,
    sort_order  INT NOT NULL DEFAULT 0,
    created_by  UUID REFERENCES users(id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (parent_id, name)
);
CREATE INDEX idx_agent_groups_parent ON agent_groups(parent_id);

CREATE TABLE agent_group_members (
    agent_id   UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    group_id   UUID NOT NULL REFERENCES agent_groups(id) ON DELETE CASCADE,
    added_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    added_by   UUID REFERENCES users(id),
    PRIMARY KEY (agent_id, group_id)
);
CREATE INDEX idx_group_members_group ON agent_group_members(group_id);

-- Plantillas de comando
CREATE TABLE command_templates (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name              TEXT NOT NULL UNIQUE,
    description       TEXT,
    category          TEXT NOT NULL DEFAULT 'general',
    command           TEXT NOT NULL,
    args              JSONB NOT NULL DEFAULT '[]'::jsonb,
    working_dir       TEXT,
    timeout_seconds   INT  NOT NULL DEFAULT 60 CHECK (timeout_seconds BETWEEN 1 AND 86400),
    requires_elevation BOOLEAN NOT NULL DEFAULT FALSE,
    requires_confirm  BOOLEAN NOT NULL DEFAULT TRUE,
    is_builtin        BOOLEAN NOT NULL DEFAULT FALSE,
    show_in_dashboard BOOLEAN NOT NULL DEFAULT FALSE,
    icon              TEXT,
    created_by        UUID REFERENCES users(id),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_templates_category ON command_templates(category);

-- Trabajos (ejecución masiva o dirigida)
CREATE TABLE jobs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            TEXT NOT NULL,
    description     TEXT,
    source          TEXT NOT NULL CHECK (source IN ('template','inline')),
    template_id     UUID REFERENCES command_templates(id),
    inline_command  TEXT,
    inline_args     JSONB NOT NULL DEFAULT '[]'::jsonb,
    inline_timeout  INT,
    target_type     TEXT NOT NULL CHECK (target_type IN ('agent','group','all')),
    target_id       UUID,
    status          TEXT NOT NULL DEFAULT 'pending'
                    CHECK (status IN ('pending','dispatching','running','completed','failed','partial','cancelled')),
    total_items     INT NOT NULL DEFAULT 0,
    pending_items   INT NOT NULL DEFAULT 0,
    success_items   INT NOT NULL DEFAULT 0,
    failed_items    INT NOT NULL DEFAULT 0,
    scheduled_at    TIMESTAMPTZ,
    started_at      TIMESTAMPTZ,
    completed_at    TIMESTAMPTZ,
    created_by      UUID NOT NULL REFERENCES users(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_jobs_status ON jobs(status);
CREATE INDEX idx_jobs_created ON jobs(created_at DESC);

CREATE TABLE job_items (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id       UUID NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    agent_id     UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    status       TEXT NOT NULL DEFAULT 'pending'
                 CHECK (status IN ('pending','dispatched','running','completed','failed','timeout','cancelled','offline')),
    exit_code    INT,
    stdout       TEXT,
    stderr       TEXT,
    error_msg    TEXT,
    started_at   TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (job_id, agent_id)
);
CREATE INDEX idx_job_items_job ON job_items(job_id);
CREATE INDEX idx_job_items_agent ON job_items(agent_id);
CREATE INDEX idx_job_items_status ON job_items(status);

-- Auditoría (hash-chain se activa en Fase 10)
CREATE TABLE audit_events (
    id           BIGSERIAL PRIMARY KEY,
    occurred_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    actor_type   TEXT NOT NULL CHECK (actor_type IN ('user','agent','system','token')),
    actor_id     UUID,
    actor_label  TEXT NOT NULL,
    action       TEXT NOT NULL,
    target_type  TEXT,
    target_id    UUID,
    target_label TEXT,
    ip           INET,
    user_agent   TEXT,
    metadata     JSONB NOT NULL DEFAULT '{}'::jsonb,
    prev_hash    BYTEA,
    hash         BYTEA
);
CREATE INDEX idx_audit_time    ON audit_events(occurred_at DESC);
CREATE INDEX idx_audit_actor   ON audit_events(actor_type, actor_id);
CREATE INDEX idx_audit_target  ON audit_events(target_type, target_id);
CREATE INDEX idx_audit_action  ON audit_events(action);
```

> "Sin catalogar" NO es fila: se calcula en UI con `LEFT JOIN agent_group_members ... WHERE m.group_id IS NULL`.

---

## 4. Endpoints REST — Fase 1

Prefijo `/api/v1`. Permisos: `admin` = todo · `operator` = lectura + ejecutar · `viewer` = solo lectura.

| Método | Path | Permiso | Propósito |
|---|---|---|---|
| `POST` | `/auth/login` | público | Login email/password |
| `POST` | `/auth/logout` | admin | Cierra sesión |
| `GET` | `/auth/me` | admin | Usuario actual |
| `GET` | `/auth/csrf` | admin | Token CSRF |
| `GET` | `/tokens` | admin | Lista enrollment tokens |
| `POST` | `/tokens` | admin | Crea token (devuelve plaintext UNA vez) |
| `POST` | `/tokens/{id}/revoke` | admin | Revoca |
| `GET` | `/agents` | admin/operator | Lista (filtros: `status`, `group_id`, `ungrouped`, `q`) |
| `GET` | `/agents/{id}` | admin/operator | Detalle |
| `PATCH` | `/agents/{id}` | admin | Update (visibility, labels, group_ids) |
| `DELETE` | `/agents/{id}` | admin | Baja |
| `GET` | `/agents/{id}/events` | admin/operator | Eventos del agente |
| `GET` | `/agents/download` | público (token) | Sirve ZIP pre-configurado |
| `GET` | `/groups` | admin/operator | Árbol de grupos |
| `POST` | `/groups` | admin | Crea grupo |
| `GET` | `/groups/{id}` | admin/operator | Detalle |
| `PATCH` | `/groups/{id}` | admin | Edita |
| `DELETE` | `/groups/{id}` | admin | Elimina |
| `POST` | `/groups/{id}/members` | admin | Agrega agentes |
| `DELETE` | `/groups/{id}/members/{agentId}` | admin | Quita |
| `POST` | `/groups/bulk-move` | admin | Mueve N agentes |
| `GET` | `/templates` | admin/operator | Lista |
| `POST` | `/templates` | admin | Crea |
| `GET` | `/templates/{id}` | admin/operator | Detalle |
| `PATCH` | `/templates/{id}` | admin | Edita (no builtin) |
| `DELETE` | `/templates/{id}` | admin | Elimina (no builtin) |
| `POST` | `/templates/{id}/run` | admin/operator | Ejecuta → crea job |
| `GET` | `/jobs` | admin/operator | Lista jobs |
| `GET` | `/jobs/{id}` | admin/operator | Detalle job |
| `POST` | `/jobs/{id}/cancel` | admin/operator | Cancela |
| `GET` | `/jobs/{id}/items` | admin/operator | Items |
| `GET` | `/jobs/{id}/items/{itemId}` | admin/operator | Item con stdout/stderr |
| `GET` | `/jobs/{id}/export.csv` | admin/operator | CSV |
| `GET` | `/audit/events` | admin/operator | Lista con filtros |
| `GET` | `/audit/events/{id}` | admin/operator | Detalle |
| `GET` | `/audit/actions` | admin/operator | Valores distintos de action |
| `GET` | `/audit/export.csv` | admin/operator | CSV del filtro |
| `GET` | `/dashboard/summary` | admin/operator | KPIs + listas |
| `GET` | `/health` | público | Liveness |
| `GET` | `/version` | público | Versión |
| `GET` | `/` | público | SPA React |

---

## 5. Protocolo WebSocket (agente ↔ server)

Endpoint: `wss://<host>/api/v1/agent/ws`.

### Handshake de enrolamiento

```
agent → server:
  { "type":"hello", "token":"<plain>", "agent_version":"0.1.0",
    "os":"windows", "arch":"amd64", "hostname":"LAPTOP-01" }

server → agent:
  { "type":"welcome", "agent_id":"uuid", "session_jwt":"eyJ…",
    "server_time":"..." }
```

Tras `welcome`, todos los frames WS llevan `Authorization: Bearer <session_jwt>` en el subprotocol.

### Mensajes (Fase 1 + roadmap)

| type | dir | payload | fase |
|---|---|---|---|
| `hello` | agent→server | enroll | 1 |
| `welcome` | server→agent | creds | 1 |
| `heartbeat` | agent→server | `{ts}` | 1 |
| `heartbeat_ack` | server→agent | `{ts}` | 1 |
| `ping`/`pong` | ambos | `{nonce}` | 1 |
| `set_visibility` | server→agent | `{visibility}` | 1 (persiste; hardening real Fase 9) |
| `command` | server→agent | `{job_item_id, command, args, timeout}` | 3 |
| `command_result` | agent→server | `{job_item_id, exit_code, stdout, stderr}` | 3 |
| `inventory_request` | server→agent | `{id}` | 2 |
| `inventory_snapshot` | agent→server | `{id, hardware, software}` | 2 |

Reconexión: backoff exponencial con jitter (1s → 5min tope). Re-envía `hello` solo si JWT expiró.

---

## 6. Generación del bundle del agente

1. **CI** compila binarios base (`sai-agent-{windows,linux,darwin}-{amd64,arm64}`) y los publica como artefacto de GH Releases.
2. **Server, al arrancar**, valida/lee los binarios de `dist/`.
3. **`POST /tokens`** crea token; UI muestra botón "Descargar agente".
4. **`GET /agents/download?token=...&os=...&arch=...`** (público, valida token):
   - Verifica token (hash, max_uses, expiración, no revocado).
   - Lee binario base de `dist/`.
   - Genera `config.json` con `server_url` + `enrollment_token` + `labels`.
   - Genera `install.ps1` (Windows) o `install.sh` (Linux/macOS).
   - Empaqueta ZIP, marca `uses++`.
   - Devuelve con `Content-Disposition: attachment`.
5. **CLI `cmd/agent-installer`** permite el mismo bundle offline (air-gapped).

---

## 7. Variables de entorno

| Variable | Descripción | Default |
|---|---|---|
| `SAI_ENV` | `development` \| `production` | `development` |
| `SAI_BIND` | Dirección de escucha | `:8080` |
| `SAI_TLS_MODE` | `reverse_proxy` \| `direct` | `reverse_proxy` |
| `SAI_PUBLIC_URL` | URL pública (WSS + bundle) | — |
| `SAI_DB_URL` | DSN Postgres | requerido |
| `SAI_JWT_SECRET` | Secreto JWT admin (≥ 32 bytes) | dev default |
| `SAI_AGENT_JWT_SECRET` | Secreto para JWT agente | dev default |
| `SAI_BUNDLE_DIR` | Ruta binarios base | `./dist` |
| `SAI_DEFAULT_LANG` | Idioma por defecto | `es` |
| `SAI_SESSION_TTL` | TTL cookie admin | `8h` |
| `SAI_AGENT_TOKEN_TTL` | TTL enrollment token | `24h` |
| `SAI_LOG_LEVEL` | `debug`,`info`,`warn`,`error` | `info` |
| `SAI_WEB_DIST` | Path al bundle web (en embed o FS) | `./web/dist` |
| `SAI_AGENT_DOWNLOAD_URL_TTL` | TTL de la URL firmada de descarga | `15m` |

---

## 8. Panel admin (Fase 1)

Stack: React 18 + Vite 5 + TypeScript + react-router 6 + TanStack Query 5 + TailwindCSS + lucide-react + i18next.

Páginas:

| Ruta | Página | Descripción |
|---|---|---|
| `/login` | Login | email/password |
| `/` | Dashboard | KPIs + agentes con problemas + acciones rápidas (templates con `show_in_dashboard=true`) + trabajos recientes |
| `/agents` | Agents | Sidebar árbol de grupos (con "Sin catalogar" virtual) + tabla filtrable + bulk move |
| `/agents/:id` | AgentDetail | Tabs: Información, Hardware (F2), Software (F2), Comandos (F3), Terminal (F6), Eventos, Auditoría |
| `/groups` | Groups | CRUD + árbol |
| `/commands` | Templates | CRUD de plantillas (botón ▶ ejecuta) |
| `/jobs` | Jobs | Listado de trabajos |
| `/jobs/:id` | JobDetail | Detalle + items + cancel |
| `/audit` | Audit | Lista con filtros + drawer de detalle + export CSV |

---

## 9. i18n

- **Backend**: `internal/i18n/locales/{es,en}.json`. Helper `i18n.T(r, "key")` lee `Accept-Language`.
- **Frontend**: i18next con detección por `navigator.language`; toggle en header. Namespaces: `common`, `auth`, `dashboard`, `groups`, `agents`, `templates`, `jobs`, `audit`.

---

## 10. Docker

Multi-stage: `node:22-alpine` (panel) → `golang:1.25-alpine` (server + agente) → `gcr.io/distroless/static-debian12`. Volumen opcional para `dist/` y datos.

`docker-compose.yml` con servicios `postgres` + `server`, red interna, healthchecks.

---

## 11. GitHub Actions

### `ci.yml` (push/PR)
- `golangci-lint run`
- `go test ./...`
- En `web/`: `npm ci && npm run lint && npm run build`
- Build de verificación del server.

### `release.yml` (tag `v*` + `workflow_dispatch`)
1. Build cross-platform del agente (6 targets).
2. Build del server (linux/amd64 + linux/arm64).
3. Build del panel web.
4. Build & push imagen Docker multi-arch → `ghcr.io/naired01/sai:<tag>` + `latest`.
5. GitHub Release con binarios adjuntos y notas auto-generadas.
6. Smoke test: levanta Postgres en servicio, golpea `/api/v1/health` y `/api/v1/version`.

---

## 12. Seguridad (Fase 1)

- Argon2id para passwords de admin (auto-test al hashear).
- JWT HS256 admin: cookie `HttpOnly`, `SameSite=Lax`, `Secure` en prod.
- CSRF token en header `X-CSRF-Token` para mutaciones.
- Enrollment tokens: solo SHA-256 en DB; plaintext se muestra una vez.
- JWT por-agente con secreto único en `agent_credentials`.
- Rate limit por IP en `/auth/login` (5/min).
- Headers: `X-Content-Type-Options: nosniff`, `Referrer-Policy: same-origin`, `X-Frame-Options: DENY`.
- Inputs validados con `go-playground/validator`.

---

## 13. Roadmap y checklist

| Fase | Estado | Entregable |
|---|---|---|
| **0** | ✅ | Andamiaje repo, go.mod, estructura, docker-compose skeleton |
| **1** | ✅ | Auth, tokens, agents, **grupos**, **templates**, **jobs (modelo)**, **audit (tabla+UI)**, **dashboard**, ws hub, bundle, panel básico, i18n, GH Actions (release.yml) |
| **2** | ✅ | **Inventario HW** (Host + CPU + RAM + Discos + Redes): mensajes WS `inventory_request`/`inventory_snapshot`, paquete `internal/inventory`, 3 endpoints REST, ticker de purga, tabs Hardware en `AgentDetail` |
| **2.1** | ✅ | **Inventario SW** (paquetes instalados + servicios + updates disponibles): collectors per-OS con build tags (dpkg/rpm/pacman/apt/yum en Linux; pkgutil+brew+launchctl+softwareupdate en macOS; winget/PS+SCM en Windows), bump SchemaVer 1→2, UI tab Software con sub-tabs y búsqueda |
| 3 | ✅ | Comandos reales (ejecutados por el agente) + JWT persistente por-agente |
| 4 | ⏳ | Scheduled jobs + retry + dependencias |
| 5 | ⏳ | Transferencia de archivos |
| 6 | ⏳ | Terminal interactiva |
| 7 | ⏳ | Políticas GPO-like |
| 8 | ⏳ | API tokens + OpenAPI + SDK (`pkg/saiclient`) |
| 9 | ⏳ | Anti-tamper + auto-update firmado |
| 10 | ⏳ | Hardening (CSP, audit hash-chain, rate limit distribuido) |

> **Nota v1.2**: la checklist de Fase 1 quedó **desactualizada** (todos los items estaban
> marcados `[ ]` aunque la implementación existía). Se reescribió con marcas `[x]` reales y
> fechas aproximadas obtenidas del historial de git. Próximas iteraciones deben mantener
> este checklist sincronizado con cada release.

### Checklist Fase 1 — cerrado (jul-2026)

Bloque backend (`internal/`, `cmd/`):
- [x] 0.1 Repo + README + LICENSE + .gitignore — `f0fabcd` (commit inicial)
- [x] 0.2 go.mod + estructura completa — `go.mod` + `cmd/{server,agent,agent-installer,smoketest}` + `internal/{api,auth,agents,tokens,groups,templates,jobs,audit,ws,bundles,dashboard,i18n,db,config,version,httpx}`. `pkg/saiclient/` queda pendiente (Fase 8).
- [x] 0.3 internal/config + internal/db + migraciones embebidas (`//go:embed sql/*.sql`)
- [x] 0.4 Migración 0001_init.sql (users / sessions / tokens / agents / agent_credentials / agent_events)
- [x] 0.5 Migración 0002_groups_templates_jobs_audit.sql
- [x] 1.1 internal/auth (argon2id OWASP-2024 params, JWT admin HS256, sesiones persistidas, CSRF)
- [x] 1.2 internal/tokens (CRUD + hashToken + Redeem atómico con `FOR UPDATE`)
- [x] 1.3 internal/agents (Create + List + UpdateAgent + Touch + RecordEvent + GetSecret + IssueDevJWT)
- [x] 1.4 internal/groups (CRUD + Tree + AddMembers + RemoveMember + BulkMove + assertNotDescendant/ErrCycle)
- [x] 1.5 internal/templates (CRUD + SeedBuiltins con 6 plantillas + read-only enforcement sobre `is_builtin`)
- [x] 1.6 internal/jobs (modelo completo: List/Get/Create/Cancel/Items/CSV; dispatcher real queda en Fase 3)
- [x] 1.7 internal/audit (Record + handlers list/get/actions/CSV; hash-chain inactivo, pendiente Fase 10)
- [x] 1.8 internal/dashboard (Build con KPIs + problem_agents + quick_actions + recent_jobs)
- [x] 1.9 internal/ws (Hub + Handler + hello/welcome/heartbeat + audit hooks)
- [x] 1.10 internal/bundles (ZIP bin+config+install por OS, con grid 3×2 os/arch)
- [x] 1.11 internal/i18n (es + en + middleware Accept-Language)
- [x] 1.12 internal/api (chi router + middleware chain + todos los endpoints de §4)
- [x] 1.13 cmd/server (bootstrap idempotente + force-reset + version + healthcheck + 13 pasos de startup logging + panic→crash.log)
- [x] 1.14 cmd/agent (cliente WSS con backoff+jitter, hello/welcome/heartbeat)
- [x] 1.15 cmd/agent-installer (CLI offline de bundle)

Panel (`web/`):
- [x] 1.16 10 páginas: Login, Dashboard, Agents, AgentDetail, Groups, Templates, Jobs, JobDetail, Audit, Tokens + dark mode + i18n es/en. Cubre y excede el scope de §8.

Infraestructura:
- [x] 1.17 Dockerfile multi-stage (node:22-alpine → golang:1.25-alpine → distroless static) + docker-compose (postgres+server con healthchecks) + `.env.example`
- [x] 1.18 `.github/workflows/ci.yml` (lint + test + build server + tsc + build web + artifacts) + `release.yml` (cross-build 6 targets agente + 2 targets server + installer + imagen multi-arch + GH Release + smoke job)
- [x] 1.19 README quickstart final (dev + Docker + bootstrap env vs CLI explicados + instalación del agente por OS + troubleshooting)

### Checklist Fase 2 — Inventario HW (cerrado jul-2026) — `b6178d0`

Backend:
- [x] 2.1 Migración `0003_inventory.sql`: tablas `agent_inventory` (UPSERT latest), `inventory_snapshots` (append-only), `inventory_events` (log de flujo)
- [x] 2.2 Paquete `internal/inventory` con tipos versionados (`SchemaVer=1`) + `Collector` (gopsutil, timeout 8s) + storage (`UpsertLatest`, `Latest`, `History`, `StaleOrMissing`, `PurgeHistory`)
- [x] 2.3 Servidor: bienvenida → `maybeRequestInventory` (server-push si stale), handler `MsgInventorySnap` con validación id-echo + persist + audit (`inventory.requested|received|failed`)
- [x] 2.4 Agente: handler `inventory_request` → recolección → respuesta `inventory_snapshot` (mismo `id`)
- [x] 2.5 Endpoints REST: `POST /agents/{id}/inventory/refresh` (rate-limit 30s por agente), `GET /agents/{id}/inventory`, `GET /agents/{id}/inventory/history?limit=&before=`
- [x] 2.6 `LastInventoryAt` añadido al modelo `Agent` (visible en `last_seen`)
- [x] 2.7 Tests puros en `internal/inventory/{collect,protocol}_test.go`

Panel:
- [x] 2.8 Tab **Hardware** en `AgentDetail` con `InventoryHardware.tsx` (Host + CPU + RAM + Discos + Redes), format helpers (`formatBytes`, `formatUptime`, `formatRelativeFromNow`)
- [x] 2.9 Botón "Solicitar inventario" con toast y auto-refresh a 3s
- [x] 2.10 i18n: 30+ claves nuevas (es + en)

Smoketest:
- [x] 2.11 Tests #15–17 añaden shape de endpoints (`/inventory/refresh`, `/inventory`, `/inventory/history`)

### Checklist Fase 2.1 — Inventario SW (cerrado jul-2026) — `ef5fb40`

Backend:
- [x] 2.1.1 SchemaVer 1→2 (server acepta ambas versiones para back-compat)
- [x] 2.1.2 Tipos estrictos: `Software{Packages, Services, Updates}` con `Source` por package-manager
- [x] 2.1.3 Per-OS collectors con build tags:
  - [x] Linux: dpkg → rpm → pacman (paquetes), systemd (servicios), apt → yum (updates)
  - [x] macOS: pkgutil → brew (paquetes), launchctl (servicios), softwareupdate (updates)
  - [x] Windows: winget → PowerShell `Get-Package` (paquetes), `sc query` (servicios)
- [x] 2.1.4 `Sheller` interface para testabilidad; parsers puros y testeados cross-OS
- [x] 2.1.5 Agente: `collectSoftwareWithTimeout(6s)` best-effort; sólo marca `Error` si encuentra algo parcial
- [x] 2.1.6 Sin elevación

Panel:
- [x] 2.1.7 Tab **Software** en `AgentDetail` con `InventorySoftware.tsx`: 3 KPIs (packages/services/updates), sub-tabs, búsqueda y source filter

Tests:
- [x] 2.1.8 `internal/inventory/software_test.go` + `{linux,darwin,windows}_test.go` (parsers + cross-OS)

### Deuda técnica conocida — Fase 1 (a cerrar antes o durante Fase 2/3)

| # | Tema | Detalle | Resolución |
|---|---|---|---|
| DT-1 | Reconnect crea agente nuevo | `internal/ws/ws.go:202` siempre llama a `agents.Create` (comentario: "siempre crea"). Cada reconexión = nueva fila + nuevo `agent_credentials`. | ✅ **Cerrado (v1.5, `4333a3d`)**: `findOrCreateAgent` en `internal/ws/ws.go` hace lookup por `(enrollment_id, hostname)` antes de crear. Si encuentra, reusa fila + secret y emite `audit.ActionAgentReconnect`. Helper testeable `findOrCreateAgentWith` + `agentsRepoFromPool` adapter; `internal/ws/ws_test.go` cubre reuse / first-enroll / error-de-lookup / error-de-create con fakes. |
| DT-2 | `ProblemThreshold` declarado pero no usado | `internal/dashboard/dashboard.go:34` define `ProblemThreshold = 5*time.Minute` sin uso. KPI usa `INTERVAL '2 minutes'` hardcoded. | ✅ **Cerrado (v1.5, `4333a3d`)**: constante `agents.OnlineThreshold = 2*time.Minute` es la única fuente de verdad. `dashboard.Build` la consume vía nuevo helper puro `ComputeCutoffs(now)`; queries SQL usan el cutoff derivado (no más hardcoded `INTERVAL`). Tests en `internal/dashboard/dashboard_test.go` blindan el invariante (cutoff = `now - OnlineThreshold`, delta = `ProblemLookback - OnlineThreshold`). |
| DT-3 | JWT del agente es código muerto | Server emite `session_jwt` en welcome pero nunca lo valida. Agent pone `agent_id` en `Authorization: Bearer` (valor inválido). | ✅ **Cerrado (v0.3.0)**: el JWT se persiste en `<install_dir>/session.jwt` (0600) y se envía en `Authorization: Bearer` en cada reconexión. Server lo firma y valida con `agent_credentials.jwt_secret`. Enrollment token sólo se usa en primer enrolamiento. Cierra el bug "agente fuera al agotar token". |
| DT-4 | Sin tests unitarios Go | `**/*_test.go` no existe. `ci.yml` corre `go test -race -shuffle=on ./...` que pasa trivialmente. Solo hay `cmd/smoketest` (integration). | ✅ **Cerrado (v1.5, `4333a3d`)**: tests puros en `internal/auth`, `internal/tokens`, `internal/agents` (Round 1) más `internal/inventory/{collect,protocol,software*}` (Fase 2) y `internal/{ws,dashboard}` (v1.5, Round 2). Pendiente opcional: tests con sqlmock/testcontainers para `groups` ciclo y `tokens.Redeem` (cubierto en producción por `cmd/smoketest`). |
| DT-5 | Jobs reales no se ejecutan | `internal/jobs/jobs.go:3` documenta que el dispatcher real llega en Fase 3. Items quedan en `pending` permanentemente. | ✅ **Cerrado (v0.3.0)**: `internal/jobs/dispatcher.go` con tick cada 2s. SELECT items `pending` con `FOR UPDATE SKIP LOCKED`. Envía `MsgCommand` via Hub. `MsgCommandResult` actualiza item. Items `offline` para agentes no conectados. Sin cancel granular (sólo timeout). |

### Plan detallado Fase 3 — JWT persistente + Dispatcher real (cerrado jul-2026)

**Commits (en `origin/main`):**

| # | SHA | Mensaje |
|---|---|---|
| C1 | `20d6e90` | feat(agent): persist session JWT to session.jwt (0600, atomic write) |
| C2 | `e05bc0b` | feat(ws,agents): validate agent JWT signed with per-agent secret |
| C3 | `84520d4` | feat(jobs,ws,agent): real command dispatcher (DT-5) |
| C4 | `92a7df3` | feat(web): stdout/stderr drawer + Commands tab history |
| C5 | `a60af2c` | feat(smoketest): end-to-end dispatch test (25-27) |
| C6 | (este commit) | docs: prepare v0.3.0 release notes |

**Scope**: DT-3 + DT-5. NO incluye cancel granular, scheduled jobs, ni GPO.

#### Decisiones de diseño

| Tema | Decisión | Razón |
|---|---|---|
| Persistencia JWT en agente | Archivo `<install_dir>/session.jwt`, 0600 | Estándar (kubectl, vault agent); separación del `config.json` |
| Truncado stdout/stderr | 64 KB por stream, 128 KB total, sufijo `[truncated at 64 KB]` | Cubre 99% de comandos; evita DoS por `cat /var/log/syslog` |
| Concurrencia por agente | 1 activo, cola FIFO | Simple; segundo job espera al primero |
| Cancel granular | No en Fase 3 (sólo timeout) | Tracking de procesos se difiere a Fase 4 |
| Migration nueva | **No** — schema actual alcanza | `jobs`, `job_items`, `agent_credentials` ya tienen todos los campos |
| Backward compat | Server acepta AMBOS flujos: `enrollment_token` (v0.2.1) y `Authorization: Bearer JWT` (Fase 3) | Bundles viejos siguen funcionando |

#### Plan en 6 commits atómicos

| # | Mensaje | Archivos principales |
|---|---|---|
| C1 | `feat(agent): persist session JWT to session.jwt` | `cmd/agent/{main,jwt_session,jwt_session_test}.go` |
| C2 | `feat(ws,agents): validate agent JWT signed with per-agent secret` | `internal/ws/ws.go`, `internal/agents/agents.go`, `internal/auth/auth.go` |
| C3 | `feat(jobs,ws): dispatcher real con command/command_result` | `internal/jobs/{dispatcher,dispatcher_test}.go`, `internal/ws/ws.go`, `cmd/agent/main.go` |
| C4 | `feat(web): stdout/stderr viewer + Commands tab` | `web/src/pages/{JobDetail,AgentDetail}.tsx`, `web/src/locales/*.json` |
| C5 | `feat(audit,smoketest): JobExecute/Complete/Fail + e2e test` | `internal/audit/audit.go`, `cmd/smoketest/main.go` |
| C6 | `docs: prepare v0.3.0 release notes` | `PLAN.md`, `README.md` |

#### Tests nuevos (~15)

| Archivo | Tests | Cubre |
|---|---|---|
| `cmd/agent/jwt_session_test.go` | 3 | save/load/clear, permisos 0600 (Unix) |
| `internal/agents/agents_test.go` | +2 | IssueAgentJWT roundtrip + claims |
| `internal/ws/ws_test.go` | +2 | JWT accepted + JWT invalid fallback |
| `internal/jobs/dispatcher_test.go` | 4 | tick, offline, result handling, status recalc |
| `internal/jobs/jobs_test.go` | 1 | truncate helper |
| `cmd/smoketest/main.go` | +3 | dispatch e2e (skip si no hay agente) |

#### Criterios de "done"

- [x] Los 6 commits mergeados a `main`.
- [x] `go test ./...` pasa (todos los paquetes con tests OK).
- [x] Smoketest 30/30 OK end-to-end con agente real (no fue skip).
- [x] Agente bajo servicio Windows persiste JWT, sobrevive restart sin re-enrolar.
- [x] Comando end-to-end via API: crear job → ver item `completed` → ver stdout en UI.
- [x] Concurrencia: 1 activo por agente, cola FIFO (mutex activeJobMu).
- [x] Offline: job para agente desconectado → item `offline` (sin reintento automático).
- [x] Release `v0.3.0` taggeado y pusheado.

---

## 14. Referencias cruzadas

- `deploy/.env.example` — variables completas.
- `internal/api/router.go` — mapa de endpoints.
- `internal/ws/protocol.go` — tipos de mensajes JSON.
- `internal/bundles/templates/install_windows.ps1.tmpl` — script de instalación.
- `internal/db/migrations.go` — orden y contenido de migraciones.
- `.github/workflows/release.yml` — qué se publica y cuándo.

---

## 15. Changelog del plan

- **v0.3.0 / v1.7** (jul-2026, `20d6e90`..`a60af2c`): **Fase 3 — JWT persistente + Dispatcher real (DT-3 + DT-5 cerrados)**. (a) `cmd/agent/jwt_session.go` con saveJWT/loadJWT/clearJWT atómicos, permisos 0600, escritura vía tmp + rename. El agente persiste `session.jwt` recibido en el welcome y lo envía en `Authorization: Bearer` en cada reconexión. (b) `internal/auth/auth.go` gana `AgentClaims` + `IssueAgentJWT(secret, agentID, ttl)` + `ParseAgentJWT(secret, raw)` (rechaza `kind != "agent"`). `internal/agents/agents.go` añade `IssueAgentJWT(ctx, pool, agentID, ttl)` que firma con `agent_credentials.jwt_secret` (no con el secret general del server) y `RotateSecret` para revocación granular. (c) `internal/ws/ws.go` reordena el handshake: si el agente trae Bearer JWT, `authenticateByJWT` extrae `agent_id` sin validar firma, busca el agente, lee su secret de DB, re-valida la firma y emite un nuevo welcome. Si falla → `sendError(code="reauth_required")` para que el agente borre `session.jwt` y re-enrole. (d) `internal/jobs/dispatcher.go`: `Dispatcher` con tick cada 2s. `SELECT pending items FOR UPDATE SKIP LOCKED LIMIT 50` (max por tick). Por cada item: marca `dispatched`, decide entre `failed("no command")` / `offline` / `hub.SendTo` (rollback a `pending` si buffer lleno). Recalcula status agregado del job (pending/success/failed/active → `completed`/`partial`/`failed`). `HandleCommandResult` actualiza item con exit_code, stdout, stderr (truncado a 64 KB con sufijo), error_msg; emite audit (`ActionJobDispatch`/`ItemComplete`/`ItemFailed`/`ItemTimeout`/`ItemOffline`). (e) `cmd/agent/main.go` añade `handleCommand`: serialización FIFO con `activeJobMu`, `exec.CommandContext` con timeout, `limitedBuffer` con cap 2×64 KB y truncado al cap, distingue timeout de otros errores. (f) `web/src/pages/JobDetail.tsx`: drawer de output con copy-to-clipboard, columna "Output" con botón "Ver output", `StatusBadge kind="item"`. `web/src/pages/AgentDetail.tsx`: tab "Comandos" muestra los últimos 20 job_items del agente con link al job detail. Backend nuevo `GET /api/v1/agents/{id}/jobs`. (g) `cmd/smoketest` añade tests [25-27] que validan el ciclo completo end-to-end con agente real. Sin migration nueva: schema actual alcanza. Limitaciones acordadas: 1 comando activo por agente, sin cancel granular, 64 KB truncado por stream. **Bug crítico cerrado**: el agente ya no queda fuera cuando se agota el enrollment_token — el JWT persistente sobrevive N reconexiones sin gastar uses.
- **v0.2.1 / v1.6** (jul-2026): **5 bugs cerrados durante Perfil B + pruebas locales**. (a) `internal/ws/ws.go`: el handler pasaba `r.Context()` a la goroutine `serveAgent`; tras el upgrade WS ese contexto se cancelaba y rompía `tokens.Redeem` con `context canceled`. Reemplazado por `context.WithCancel(context.Background())` con cancelación atada al cierre de la goroutine. (b) `cmd/agent`: el loop `default:` con `ReadMessage` no bloqueante seguía llamando `ReadMessage` miles de veces tras un error del server y disparaba el panic `repeated read on failed websocket connection` de gorilla. Refactor a una reader goroutine dedicada que entrega mensajes por canales `msgCh` / `readErrCh`. (c) `cmd/agent`: soporte nativo de **Windows Service** vía `golang.org/x/sys/windows/svc`. Build tag `//go:build windows` para `service_windows.go` con la struct `saiService` que implementa `svc.Service` (StartPending → Running → StopPending y handlers de Stop/Shutdown/Pause/Continue); stub `service_other.go` para el resto. Detección por `svc.IsWindowsService()`. (d) `cmd/agent`: nuevo flag `--log-file <path>` para redirigir `slog` a un archivo (necesario cuando corre bajo SCM donde no hay stderr visible). `internal/bundles/bundles.go`: `install.ps1` actualizado para invocar el binario **directo** (sin `cmd.exe /c` wrapper que mata el servicio al instante) y pasarle `--log-file "C:\Logs\SAI\agent.log"`. (e) `internal/api/auth_handlers.go`: `clientIP` parseaba IPv6 con búsqueda manual de `:` y devolvía `[::1]` con brackets — Postgres rechazaba como INET y rompía el login con 500. Reemplazado por `net.SplitHostPort` + tests en `internal/api/auth_handlers_test.go` cubriendo 9 casos (IPv4, IPv6, XFF primer hop, XRI, prioridad, sin puerto). (f) `cmd/smoketest`: tests 13-15 movidos ANTES del logout; test 15 ahora usa `withCSRF` (POST /inventory/refresh requiere CSRF). 27/27 tests OK.
- **v0.2.0 / v1.5** (jul-2026, `4333a3d` + `0467b48`): **Cierre de deuda técnica DT-1, DT-2, DT-4**. (a) `ws.findOrCreateAgent` ahora hace `FindByEnrollmentAndHost(enrID, hostname)` antes de `Create`: reconexiones del mismo `(token, host)` reusan fila + secret, ya no duplican agentes; se emite la nueva acción de auditoría `ActionAgentReconnect`. Refactor menor para hacerlo testeable: `findOrCreateAgentWith(ctx, repoPool, …)` con interfaces `agentFinder`/`agentCreator` y adapter `agentsRepoFromPool`. (b) `agents.OnlineThreshold` queda como única fuente de verdad de la ventana online; `dashboard.ComputeCutoffs(now)` centraliza los timestamps que las queries SQL usan para KPI/problem-agents. Se eliminó el comentario histórico sobre el viejo `ProblemThreshold`. (c) Round 2 de tests unitarios: `internal/ws/ws_test.go` (reuse / first-enroll / error de lookup / error de create con fakes) y `internal/dashboard/dashboard_test.go` (invariantes de los cutoffs, gap entre ventanas, default de `ProblemLookback = 30 días`). Total ahora: tests puros en `auth`, `tokens`, `agents`, `ws`, `dashboard`, `inventory/*`. DT-3 y DT-5 siguen abiertos (Fase 3).
- **v1.4** (jul-2026): **Fase 2.1 — Inventario SW**. SchemaVer 1→2 (server acepta ambas versiones para back-compat). Software `{Packages, Services, Updates}` con tipos estrictos y `Source` por package-manager. Per-OS collectors con build tags: Linux (dpkg → rpm → pacman / systemd / apt → yum), macOS (pkgutil → brew / launchctl / softwareupdate), Windows (winget → PS Get-Package / sc query). Sheller interface para testabilidad; parsers son puros y testeados cross-OS. UI: tab Software con 3 KPIs (packages/services/updates), sub-tabs, search y source filter. Agent: `collectSoftwareWithTimeout(6s)` best-effort; sólo marca `Error` si encuentra algo parcial. Sin elevación.
- **v1.3** (jul-2026): **Fase 2 — Inventario HW**. Migración `0003_inventory.sql` con `agent_inventory` (UPSERT latest) + `inventory_snapshots` (append-only historial) + `inventory_events` (log de flujo). Nuevo paquete `internal/inventory` con tipos versionados (SchemaVer=1), collector gopsutil (`Collect(ctx, agentVersion)` con timeout 8s), storage atómico (`UpsertLatest`), `Latest`, `History`, `StaleOrMissing`, `PurgeHistory`. Servidor: bienvenida → `maybeRequestInventory` (server-push si stale), handler `MsgInventorySnap` con validación id-echo + persist + audit. Agente: handler `inventory_request` que recolecta y responde con `inventory_snapshot` (mismo `id`). Endpoints: `POST /agents/{id}/inventory/refresh` (rate-limit 30s por agente), `GET /agents/{id}/inventory` (latest), `GET /agents/{id}/inventory/history?limit=&before=`. Constantes de auditoría: `inventory.requested|received|failed`. `LastInventoryAt` añadido al modelo `Agent` (visible en `last_seen`). Panel: tab Hardware real con `InventoryHardware.tsx`, format helpers (`formatBytes`, `formatUptime`, `formatRelativeFromNow`), botón "Solicitar inventario" con toast y auto-refresh a 3s. i18n: 30+ claves nuevas. Smoketest: tests #15-17 añaden shape de endpoints. Deuda técnica cerrada: tests puros para Fase 1 (`auth`, `tokens`, `agents`). Pendiente Fase 2.1: software (paquetes).
- **v1.2** (jul-2026): sincronización del checklist §13 con el estado real del repo (todos los items de Fase 1 cerrados en código aunque la doc los marcaba `[ ]`). Se añade §13-bis *“Deuda técnica conocida — Fase 1”* con cinco items a cerrar antes/durante Fase 2-3 (DT-1 reconexión del agente, DT-2 threshold del dashboard, DT-3 JWT path, DT-4 tests unitarios, DT-5 jobs reales). Acciones ejecutadas en este lote: docs sincronizadas, agregación de tests puros `auth` + `tokens`, fix de `ProblemThreshold` → `OnlineThreshold`, mirror de “jobs penden” en README y mirror del grid 3×2 ya documentado en release notes.
- **v1.1**: grupos jerárquicos, plantillas de comando, jobs, auditoría y dashboard suben a Fase 1; páginas de Agentes con tabs; seed de plantillas builtin.
  - **v1.1.1**: el endpoint `POST /api/v1/tokens` ahora devuelve `download_urls` (array de 6 URLs por plataforma) en lugar de un único `download_url`. El handler `GET /api/v1/agents/download` rechaza con `400 invalid_params` si faltan `?os=` o `?arch=` (ya no hace fallback a `runtime.GOOS` del server, que hacía que Docker Linux siempre sirviera binarios Linux sin importar el OS destino). El panel muestra grid 3×2 con auto-detección del cliente y badge "Detectado".
- **v1**: versión inicial con auth, tokens, agentes, WS hub, bundle y panel básico.
# SAI — Plan de Implementación v1.1

> Sistema de administración remota para equipos de TI, auto-hospedado.
> Documento vivo: incluye contexto para nuevos agentes + checklist de progreso por fase.
>
> **Stack**: Go 1.25+ (chi + gorilla/websocket + pgx) + PostgreSQL 16 + React/Vite/i18next (panel) + Docker + GitHub Actions.
> **Estado**: 🟡 v1.1 aprobado — Fase 0/1 en construcción.
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
| **1** | 🔄 | Auth, tokens, agents, **grupos**, **templates**, **jobs (modelo)**, **audit (tabla+UI)**, **dashboard**, ws hub, bundle, panel básico, i18n, GH Actions (release.yml) |
| 2 | ⏳ | Inventario HW/SW |
| 3 | ⏳ | Comandos reales (ejecutados por el agente) |
| 4 | ⏳ | Scheduled jobs + retry + dependencias |
| 5 | ⏳ | Transferencia archivos |
| 6 | ⏳ | Terminal interactiva |
| 7 | ⏳ | Políticas GPO-like |
| 8 | ⏳ | API tokens + OpenAPI + SDK |
| 9 | ⏳ | Anti-tamper + auto-update firmado |
| 10 | ⏳ | Hardening (CSP, audit hash-chain, rate limit distribuido) |

### Checklist Fase 1 (en curso)

- [x] 0.1 Repo + README + LICENSE + .gitignore
- [ ] 0.2 go.mod + estructura completa
- [ ] 0.3 internal/config + internal/db + migraciones embebidas
- [ ] 0.4 Migración 0001_init.sql
- [ ] 0.5 Migración 0002_groups_templates_jobs_audit.sql
- [ ] 1.1 internal/auth (argon2id, JWT admin, sesiones, CSRF)
- [ ] 1.2 internal/tokens (CRUD + canje)
- [ ] 1.3 internal/agents (registro + catálogo + eventos)
- [ ] 1.4 internal/groups (CRUD + árbol + bulk-move + cycle check)
- [ ] 1.5 internal/templates (CRUD + seed builtin)
- [ ] 1.6 internal/jobs (modelo + UI; dispatcher Fase 3)
- [ ] 1.7 internal/audit (Record + handlers)
- [ ] 1.8 internal/dashboard (summary)
- [ ] 1.9 internal/ws (hub + handshake + heartbeat)
- [ ] 1.10 internal/bundles (ZIP)
- [ ] 1.11 internal/i18n (es/en)
- [ ] 1.12 internal/api (router + middleware)
- [ ] 1.13 cmd/server (bootstrap + wiring)
- [ ] 1.14 cmd/agent (mínimo)
- [ ] 1.15 cmd/agent-installer
- [ ] 1.16 Panel React (Login, Dashboard, Agents, Groups, Templates, Jobs, Audit)
- [ ] 1.17 Dockerfile + docker-compose + .env.example
- [ ] 1.18 GH Actions ci.yml + release.yml
- [ ] 1.19 README quickstart final

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

- **v1.1** (este): grupos jerárquicos, plantillas de comando, jobs, auditoría y dashboard suben a Fase 1; páginas de Agentes con tabs; seed de plantillas builtin.
- **v1**: versión inicial con auth, tokens, agentes, WS hub, bundle y panel básico.
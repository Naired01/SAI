-- 0001_init.sql
-- Migración inicial: usuarios, sesiones, tokens de enrolamiento, agentes,
-- credenciales de agente y eventos del agente.

CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS citext;

-- -----------------------------------------------------------------------------
-- Usuarios administradores del panel
-- -----------------------------------------------------------------------------
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

-- -----------------------------------------------------------------------------
-- Sesiones del panel (cookie)
-- -----------------------------------------------------------------------------
CREATE TABLE sessions (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    csrf_token  TEXT NOT NULL,
    user_agent  TEXT,
    ip          INET,
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_sessions_user    ON sessions(user_id);
CREATE INDEX idx_sessions_expires ON sessions(expires_at);

-- -----------------------------------------------------------------------------
-- Tokens de enrolamiento (canjeados por el agente al hacer hello)
-- -----------------------------------------------------------------------------
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

-- -----------------------------------------------------------------------------
-- Agentes (uno por equipo enrolado)
-- -----------------------------------------------------------------------------
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
CREATE INDEX idx_agents_os        ON agents(os);

-- -----------------------------------------------------------------------------
-- Credencial JWT única por agente (rotable)
-- -----------------------------------------------------------------------------
CREATE TABLE agent_credentials (
    agent_id   UUID PRIMARY KEY REFERENCES agents(id) ON DELETE CASCADE,
    jwt_secret TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    rotated_at TIMESTAMPTZ
);

-- -----------------------------------------------------------------------------
-- Eventos del agente (connect, disconnect, heartbeat, errors, ...)
-- -----------------------------------------------------------------------------
CREATE TABLE agent_events (
    id         BIGSERIAL PRIMARY KEY,
    agent_id   UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    type       TEXT NOT NULL,
    payload    JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_events_agent_time ON agent_events(agent_id, created_at DESC);
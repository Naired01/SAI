-- 0002_groups_templates_jobs_audit.sql
-- Grupos jerárquicos, plantillas de comando, trabajos y auditoría.

-- -----------------------------------------------------------------------------
-- Grupos jerárquicos de agentes
-- -----------------------------------------------------------------------------
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

-- -----------------------------------------------------------------------------
-- Plantillas de comando (reutilizables)
-- -----------------------------------------------------------------------------
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

-- -----------------------------------------------------------------------------
-- Trabajos (ejecución masiva o dirigida)
-- -----------------------------------------------------------------------------
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
CREATE INDEX idx_jobs_status  ON jobs(status);
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
CREATE INDEX idx_job_items_job    ON job_items(job_id);
CREATE INDEX idx_job_items_agent  ON job_items(agent_id);
CREATE INDEX idx_job_items_status ON job_items(status);

-- -----------------------------------------------------------------------------
-- Auditoría (hash-chain se activa en Fase 10)
-- -----------------------------------------------------------------------------
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
CREATE INDEX idx_audit_time   ON audit_events(occurred_at DESC);
CREATE INDEX idx_audit_actor  ON audit_events(actor_type, actor_id);
CREATE INDEX idx_audit_target ON audit_events(target_type, target_id);
CREATE INDEX idx_audit_action ON audit_events(action);
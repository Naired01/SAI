-- =============================================================================
-- Migration 0003: Inventario HW/SW (Fase 2)
-- =============================================================================
-- Tres tablas:
--   * agent_inventory   — UPSERT 1-fila-por-agente. Lectura rápida del latest.
--   * inventory_snapshots — Append-only BIGSERIAL. Historial paginado.
--   * inventory_events    — Log del flujo (requested/received/failed/stale).
-- =============================================================================

CREATE TABLE agent_inventory (
    agent_id      UUID PRIMARY KEY REFERENCES agents(id) ON DELETE CASCADE,
    received_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    source        TEXT NOT NULL DEFAULT 'agent'
                  CHECK (source IN ('agent', 'manual', 'seed')),
    hardware      JSONB NOT NULL,
    software      JSONB NOT NULL DEFAULT '{}'::jsonb,
    agent_version TEXT,
    schema_ver    INT  NOT NULL DEFAULT 1
                  CHECK (schema_ver >= 1)
);

CREATE INDEX idx_agent_inventory_received ON agent_inventory(received_at DESC);

-- -----------------------------------------------------------------------------

CREATE TABLE inventory_snapshots (
    id            BIGSERIAL PRIMARY KEY,
    agent_id      UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    received_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    source        TEXT NOT NULL DEFAULT 'agent'
                  CHECK (source IN ('agent', 'manual', 'seed')),
    hardware      JSONB NOT NULL,
    software      JSONB NOT NULL DEFAULT '{}'::jsonb,
    agent_version TEXT,
    schema_ver    INT  NOT NULL DEFAULT 1
                  CHECK (schema_ver >= 1)
);

CREATE INDEX idx_inv_snapshots_agent_time
    ON inventory_snapshots(agent_id, received_at DESC);

-- -----------------------------------------------------------------------------

CREATE TABLE inventory_events (
    id             BIGSERIAL PRIMARY KEY,
    agent_id       UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    event_type     TEXT NOT NULL
                    CHECK (event_type IN ('requested', 'received', 'failed', 'stale')),
    request_id     UUID,
    error_msg      TEXT,
    agent_version  TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_inv_events_agent_time
    ON inventory_events(agent_id, created_at DESC);

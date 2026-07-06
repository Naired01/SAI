package inventory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// -----------------------------------------------------------------------------
// Storage — operaciones contra PostgreSQL.
// -----------------------------------------------------------------------------

// SnapshotRecord es el resultado de Latest() / History(). Hardware y Software
// se devuelven como json.RawMessage para que el handler HTTP los reenvíe sin
// re-marshal; la UI los parsea según su propio modelo.
type SnapshotRecord struct {
	AgentID      string          `json:"agent_id"`
	ReceivedAt   time.Time       `json:"received_at"`
	Source       string          `json:"source"`
	Hardware     json.RawMessage `json:"hardware"`
	Software     json.RawMessage `json:"software"`
	AgentVersion string          `json:"agent_version"`
	SchemaVer    int             `json:"schema_ver"`
}

// ErrNotFound se devuelve cuando no existe snapshot para el agente pedido.
var ErrNotFound = errors.New("inventory: not found")

// UpsertLatest persiste el snapshot en agent_inventory (UPSERT) y crea una
// entrada append-only en inventory_snapshots. La transacción garantiza que
// ambas escrituras quedan consistentes.
func UpsertLatest(ctx context.Context, pool *pgxpool.Pool, agentID string, snap Snapshot) error {
	if agentID == "" {
		return errors.New("inventory: empty agent_id")
	}
	hw, err := json.Marshal(snap.Hardware)
	if err != nil {
		return fmt.Errorf("marshal hardware: %w", err)
	}
	sw, err := json.Marshal(snap.Software)
	if err != nil {
		return fmt.Errorf("marshal software: %w", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// 1) UPSERT latest.
	if _, err := tx.Exec(ctx, `
		INSERT INTO agent_inventory
			(agent_id, received_at, source, hardware, software, agent_version, schema_ver)
		VALUES ($1, $2, 'agent', $3::jsonb, $4::jsonb, $5, $6)
		ON CONFLICT (agent_id) DO UPDATE SET
			received_at   = EXCLUDED.received_at,
			source        = EXCLUDED.source,
			hardware      = EXCLUDED.hardware,
			software      = EXCLUDED.software,
			agent_version = EXCLUDED.agent_version,
			schema_ver    = EXCLUDED.schema_ver
	`, agentID, snap.CollectedAt, string(hw), string(sw), snap.AgentVersion, snap.SchemaVer); err != nil {
		return fmt.Errorf("upsert agent_inventory: %w", err)
	}

	// 2) Append historial.
	if _, err := tx.Exec(ctx, `
		INSERT INTO inventory_snapshots
			(agent_id, received_at, source, hardware, software, agent_version, schema_ver)
		VALUES ($1, $2, 'agent', $3::jsonb, $4::jsonb, $5, $6)
	`, agentID, snap.CollectedAt, string(hw), string(sw), snap.AgentVersion, snap.SchemaVer); err != nil {
		return fmt.Errorf("insert history: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}

	// 3) Log evento 'received'.
	_ = RecordEvent(ctx, pool, agentID, "received", uuid.Nil, snap.AgentVersion, "")
	return nil
}

// Latest devuelve el snapshot más reciente del agente. ErrNotFound si no hay.
func Latest(ctx context.Context, pool *pgxpool.Pool, agentID string) (*SnapshotRecord, error) {
	row := pool.QueryRow(ctx, `
		SELECT agent_id, received_at, source, hardware, software,
		       COALESCE(agent_version,''), schema_ver
		FROM agent_inventory WHERE agent_id = $1
	`, agentID)
	return scanSnapshot(row)
}

// HistoryOptions filtros para History.
type HistoryOptions struct {
	Limit  int
	Before time.Time // 0 = sin tope inferior (toma los más recientes)
}

// History devuelve los últimos N snapshots del agente ordenados desc.
func History(ctx context.Context, pool *pgxpool.Pool, agentID string, opts HistoryOptions) ([]*SnapshotRecord, error) {
	if opts.Limit <= 0 || opts.Limit > 500 {
		opts.Limit = 50
	}
	args := []any{agentID}
	where := "agent_id = $1"
	if !opts.Before.IsZero() {
		args = append(args, opts.Before)
		where += fmt.Sprintf(" AND received_at < $%d", len(args))
	}
	args = append(args, opts.Limit)
	q := fmt.Sprintf(`
		SELECT agent_id, received_at, source, hardware, software,
		       COALESCE(agent_version,''), schema_ver
		FROM inventory_snapshots
		WHERE %s
		ORDER BY received_at DESC
		LIMIT $%d
	`, where, len(args))

	rows, err := pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*SnapshotRecord
	for rows.Next() {
		r, err := scanSnapshot(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// StaleOrMissing devuelve true si el agente no tiene snapshot o si el último
// tiene más de `ttl` de antigüedad.
func StaleOrMissing(ctx context.Context, pool *pgxpool.Pool, agentID string, ttl time.Duration) (bool, error) {
	var receivedAt *time.Time
	err := pool.QueryRow(ctx, `
		SELECT received_at FROM agent_inventory WHERE agent_id = $1
	`, agentID).Scan(&receivedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return true, nil
		}
		return false, err
	}
	if receivedAt == nil {
		return true, nil
	}
	return time.Since(*receivedAt) > ttl, nil
}

// RecordEvent apila un evento del flujo inventory (requested/received/failed/stale).
// Si reqID es uuid.Nil se persiste NULL.
func RecordEvent(ctx context.Context, pool *pgxpool.Pool, agentID, eventType string, reqID uuid.UUID, agentVersion, errMsg string) error {
	var reqArg any = nil
	if reqID != uuid.Nil {
		reqArg = reqID
	}
	var errArg any = nil
	if errMsg != "" {
		errArg = errMsg
	}
	var verArg any = nil
	if agentVersion != "" {
		verArg = agentVersion
	}
	_, err := pool.Exec(ctx, `
		INSERT INTO inventory_events (agent_id, event_type, request_id, error_msg, agent_version)
		VALUES ($1, $2, $3, $4, $5)
	`, agentID, eventType, reqArg, errArg, verArg)
	return err
}

// PurgeHistory mantiene como máximo `keep` snapshots por agente. Elimina el
// resto (los más viejos). Devuelve el número total de filas borradas.
func PurgeHistory(ctx context.Context, pool *pgxpool.Pool, keep int) (int64, error) {
	tag, err := pool.Exec(ctx, `
		DELETE FROM inventory_snapshots s
		USING (
			SELECT id FROM (
				SELECT id, ROW_NUMBER() OVER (
					PARTITION BY agent_id ORDER BY received_at DESC
				) AS rn FROM inventory_snapshots
			) ranked WHERE rn > $1
		) to_delete
		WHERE s.id = to_delete.id
	`, keep)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// -----------------------------------------------------------------------------
// Internals
// -----------------------------------------------------------------------------

type rowScanner interface {
	Scan(dest ...any) error
}

func scanSnapshot(r rowScanner) (*SnapshotRecord, error) {
	var (
		rec          SnapshotRecord
		hardwareRaw  []byte
		softwareRaw  []byte
	)
	if err := r.Scan(
		&rec.AgentID, &rec.ReceivedAt, &rec.Source,
		&hardwareRaw, &softwareRaw,
		&rec.AgentVersion, &rec.SchemaVer,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	rec.Hardware = hardwareRaw
	rec.Software = softwareRaw
	return &rec, nil
}

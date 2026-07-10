// Package jobs - dispatcher real (Fase 3 / DT-5).
//
// El dispatcher recorre los job_items en estado 'pending', los dispatcha
// al agente conectado via WS, y maneja el ciclo de vida completo:
//   pending -> dispatched -> running -> completed|failed|timeout|offline
//
// Se ejecuta como goroutine en cmd/server (cmd_server.go) con un tick cada
// 2s. Usa SELECT ... FOR UPDATE SKIP LOCKED para que múltiples instancias
// del server (futuro HA) puedan correr el dispatcher en paralelo sin
// dispatchar el mismo item dos veces.
package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/Naired01/SAI/internal/audit"
	"github.com/Naired01/SAI/internal/ws"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Dispatcher ejecuta el ciclo dispatch->result para job_items pendientes.
type Dispatcher struct {
	pool   *pgxpool.Pool
	hub    *ws.Hub
	logger *slog.Logger

	mu     sync.Mutex
	cancel context.CancelFunc
}

// NewDispatcher construye un dispatcher. Llamar Start() para arrancarlo
// en una goroutine.
func NewDispatcher(pool *pgxpool.Pool, hub *ws.Hub, logger *slog.Logger) *Dispatcher {
	return &Dispatcher{pool: pool, hub: hub, logger: logger}
}

// Start lanza el loop. Devuelve inmediatamente. Para detenerlo,
// llamar Stop() o cancelar el ctx que se paso a Start.
func (d *Dispatcher) Start(parent context.Context) {
	ctx, cancel := context.WithCancel(parent)
	d.mu.Lock()
	d.cancel = cancel
	d.mu.Unlock()
	go d.loop(ctx)
}

// Stop cancela el loop del dispatcher. Idempotente.
func (d *Dispatcher) Stop() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.cancel != nil {
		d.cancel()
	}
}

func (d *Dispatcher) loop(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	// Tick inicial inmediato para no esperar 2s tras arrancar.
	if err := d.tick(ctx); err != nil && !errors.Is(err, context.Canceled) {
		d.logger.Warn("dispatcher tick error", "err", err)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := d.tick(ctx); err != nil && !errors.Is(err, context.Canceled) {
				d.logger.Warn("dispatcher tick error", "err", err)
			}
		}
	}
}

// MaxItemsPerTick limita cuantos items procesa cada tick para evitar
// contention si hay miles de jobs encolados.
const MaxItemsPerTick = 50

// MaxOutputBytes es el limite duro de stdout/stderr por item (64 KB).
// Coincide con el limite del agente; truncamos tambien en el server
// para defensa en profundidad.
const MaxOutputBytes = 64 * 1024

// TruncateSuffix se agrega cuando stdout/stderr exceden el limite.
const TruncateSuffix = "\n[truncated at 64 KB]"

func truncate(s string) string {
	if len(s) <= MaxOutputBytes {
		return s
	}
	return s[:MaxOutputBytes] + TruncateSuffix
}

// pendingItem representa un job_item listo para ser enviado al agente.
// El campo isFromTemplate es true si el command+args vienen de la
// command_templates table (vía template_id) en lugar del job inline.
type pendingItem struct {
	id             string
	jobID          string
	agentID        string
	cmd            string
	args           []string
	timeoutSec     int
	isFromTemplate bool
}

// tick procesa hasta MaxItemsPerTick items pendientes. Devuelve el primer
// error o nil.
func (d *Dispatcher) tick(ctx context.Context) error {
	// 1) Recoger items pending y lockearlos.
	rows, err := d.pool.Query(ctx, `
		SELECT i.id, i.job_id, i.agent_id,
		       COALESCE(j.inline_command, '') AS cmd,
		       COALESCE(j.inline_args, '[]'::jsonb) AS args,
		       COALESCE(j.inline_timeout, 60) AS timeout,
		       COALESCE(t.command, '') AS template_cmd,
		       COALESCE(t.args, '[]'::jsonb) AS template_args,
		       COALESCE(t.timeout_seconds, 60) AS template_timeout
		FROM job_items i
		JOIN jobs j ON j.id = i.job_id
		LEFT JOIN command_templates t ON t.id = j.template_id
		WHERE i.status = 'pending'
		  AND j.status IN ('pending','dispatching','running')
		ORDER BY i.created_at
		LIMIT $1
		FOR UPDATE OF i SKIP LOCKED
	`, MaxItemsPerTick)
	if err != nil {
		return fmt.Errorf("query pending items: %w", err)
	}
	var items []pendingItem
	for rows.Next() {
		var it pendingItem
		var cmd, tCmd string
		var args, tArgs []byte
		if err := rows.Scan(&it.id, &it.jobID, &it.agentID,
			&cmd, &args, &it.timeoutSec,
			&tCmd, &tArgs, &it.timeoutSec); err != nil {
			rows.Close()
			return fmt.Errorf("scan: %w", err)
		}
		// Template wins sobre inline si template_id no es null.
		if tCmd != "" {
			it.cmd = tCmd
			it.isFromTemplate = true
		} else {
			it.cmd = cmd
		}
		if it.isFromTemplate {
			if err := json.Unmarshal(tArgs, &it.args); err != nil {
				it.args = nil
			}
		} else {
			if err := json.Unmarshal(args, &it.args); err != nil {
				it.args = nil
			}
		}
		if it.timeoutSec <= 0 {
			it.timeoutSec = 60
		}
		items = append(items, it)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	// 2) Para cada item, transicionar a dispatched y enviar el comando.
	for _, it := range items {
		if err := d.dispatch(ctx, it); err != nil {
			d.logger.Warn("dispatch item failed", "item_id", it.id, "err", err)
		}
	}

	// 3) Recalcular status agregado de jobs.
	if err := d.recalcJobStatuses(ctx); err != nil {
		return fmt.Errorf("recalc jobs: %w", err)
	}

	return nil
}

func (d *Dispatcher) dispatch(ctx context.Context, it pendingItem) error {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Marcar dispatched.
	if _, err := tx.Exec(ctx, `
		UPDATE job_items
		SET status = 'dispatched'
		WHERE id = $1 AND status = 'pending'
	`, it.id); err != nil {
		return fmt.Errorf("update item dispatched: %w", err)
	}

	// Resolver command + args + timeout finales.
	if it.cmd == "" {
		// Sin comando: marcar failed.
		if _, err := tx.Exec(ctx, `
			UPDATE job_items
			SET status = 'failed', error_msg = $2, completed_at = now()
			WHERE id = $1
		`, it.id, "no command specified"); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		audit.Record(ctx, d.pool, audit.Event{
			Actor:  audit.Actor{Type: "system", Label: "dispatcher"},
			Action: audit.ActionJobItemFailed,
			Target: &audit.Target{Type: "job_item", ID: &it.id, Label: it.id},
			Metadata: map[string]any{"reason": "no_command"},
		})
		return nil
	}

	// ¿Agente conectado?
	if !d.hub.IsConnected(it.agentID) {
		if _, err := tx.Exec(ctx, `
			UPDATE job_items
			SET status = 'offline', completed_at = now()
			WHERE id = $1
		`, it.id); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		audit.Record(ctx, d.pool, audit.Event{
			Actor:  audit.Actor{Type: "system", Label: "dispatcher"},
			Action: audit.ActionJobItemOffline,
			Target: &audit.Target{Type: "job_item", ID: &it.id, Label: it.id},
			Metadata: map[string]any{"agent_id": it.agentID},
		})
		return nil
	}

	// Enviar por WS. El handle del agente responde con command_result.
	msg := struct {
		Type         string   `json:"type"`
		JobItemID    string   `json:"job_item_id"`
		Command      string   `json:"command"`
		Args         []string `json:"args"`
		TimeoutSec   int      `json:"timeout_sec"`
	}{
		Type:       "command",
		JobItemID:  it.id,
		Command:    it.cmd,
		Args:       it.args,
		TimeoutSec: it.timeoutSec,
	}
	if !d.hub.SendTo(it.agentID, msg) {
		// Hub lleno, marcar como offline (caso raro, lo reintenta el proximo tick).
		if _, err := tx.Exec(ctx, `
			UPDATE job_items
			SET status = 'pending'
			WHERE id = $1
		`, it.id); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		return errors.New("hub.SendTo returned false (agent buffer full)")
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}

	audit.Record(ctx, d.pool, audit.Event{
		Actor:  audit.Actor{Type: "system", Label: "dispatcher"},
		Action: audit.ActionJobDispatch,
		Target: &audit.Target{Type: "job_item", ID: &it.id, Label: it.id},
		Metadata: map[string]any{
			"job_id":   it.jobID,
			"agent_id": it.agentID,
			"command":  it.cmd,
			"timeout":  it.timeoutSec,
		},
	})
	d.logger.Info("dispatched command", "item_id", it.id, "agent_id", it.agentID, "command", it.cmd)
	return nil
}

// HandleCommandResult lo llama el readerLoop del WS cuando llega un
// command_result. Actualiza el job_item y recalcula el status del job.
// Devuelve error sólo si falla la DB; un payload malformado se ignora
// silenciosamente.
func (d *Dispatcher) HandleCommandResult(ctx context.Context, agentID string, raw []byte) error {
	var msg struct {
		JobItemID string `json:"job_item_id"`
		ExitCode  int    `json:"exit_code"`
		Stdout    string `json:"stdout"`
		Stderr    string `json:"stderr"`
		Error     string `json:"error,omitempty"`
	}
	if err := json.Unmarshal(raw, &msg); err != nil {
		d.logger.Debug("command_result bad json", "agent", agentID, "err", err)
		return nil
	}
	if msg.JobItemID == "" {
		d.logger.Debug("command_result missing job_item_id", "agent", agentID)
		return nil
	}

	stdout := truncate(msg.Stdout)
	stderr := truncate(msg.Stderr)

	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Sólo aceptar resultados de items que estaban dispatched/running
	// para este agente (defensa contra mensajes cruzados).
	var found bool
	if err := tx.QueryRow(ctx, `
		UPDATE job_items
		SET status = $2,
		    exit_code = $3,
		    stdout = $4,
		    stderr = $5,
		    error_msg = NULLIF($6, ''),
		    completed_at = now(),
		    started_at = COALESCE(started_at, now())
		WHERE id = $1
		  AND agent_id = $7
		  AND status IN ('dispatched','running')
		RETURNING true
	`, msg.JobItemID, classifyResult(msg.ExitCode, msg.Error), msg.ExitCode, stdout, stderr, msg.Error, agentID).Scan(&found); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			d.logger.Debug("command_result for unknown/already-finalized item",
				"item_id", msg.JobItemID, "agent_id", agentID)
			return nil
		}
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}

	// Audit + recálculo de job.
	action := audit.ActionJobItemComplete
	if msg.Error == "timeout" {
		action = audit.ActionJobItemTimeout
	} else if msg.ExitCode != 0 {
		action = audit.ActionJobItemFailed
	}
	audit.Record(ctx, d.pool, audit.Event{
		Actor:  audit.Actor{Type: "agent", ID: &agentID, Label: agentID},
		Action: action,
		Target: &audit.Target{Type: "job_item", ID: &msg.JobItemID, Label: msg.JobItemID},
		Metadata: map[string]any{
			"exit_code": msg.ExitCode,
			"stdout_bytes": len(stdout),
			"stderr_bytes": len(stderr),
		},
	})

	if err := d.recalcJobStatuses(ctx); err != nil {
		return err
	}
	return nil
}

// classifyResult mapea exit_code + error a status del item.
// Convención del agente: error="timeout" => timeout; cualquier otro error
// no-vacío => failed; exit_code != 0 => failed; exit_code == 0 => completed.
func classifyResult(exitCode int, errMsg string) string {
	if errMsg == "timeout" {
		return ItemTimeout
	}
	if errMsg != "" || exitCode != 0 {
		return ItemFailed
	}
	return ItemCompleted
}

// recalcJobStatuses actualiza el status agregado de cada job y emite
// los status finales (completed/partial/failed). Llamarlo después de
// dispatch y después de cada HandleCommandResult.
func (d *Dispatcher) recalcJobStatuses(ctx context.Context) error {
	rows, err := d.pool.Query(ctx, `
		SELECT j.id,
		       SUM(CASE WHEN i.status = 'pending' THEN 1 ELSE 0 END) AS pending,
		       SUM(CASE WHEN i.status = 'completed' THEN 1 ELSE 0 END) AS success,
		       SUM(CASE WHEN i.status IN ('failed','timeout','offline','cancelled') THEN 1 ELSE 0 END) AS failed,
		       SUM(CASE WHEN i.status IN ('dispatched','running') THEN 1 ELSE 0 END) AS active
		FROM jobs j
		JOIN job_items i ON i.job_id = j.id
		WHERE j.status IN ('pending','dispatching','running')
		GROUP BY j.id
	`)
	if err != nil {
		return err
	}
	type jobStat struct {
		id     string
		pend   int
		ok     int
		fail   int
		active int
	}
	var stats []jobStat
	for rows.Next() {
		var s jobStat
		if err := rows.Scan(&s.id, &s.pend, &s.ok, &s.fail, &s.active); err != nil {
			rows.Close()
			return err
		}
		stats = append(stats, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, s := range stats {
		var newStatus string
		var completedAt *time.Time
		if s.active == 0 && s.pend == 0 {
			if s.fail == 0 {
				newStatus = StatusCompleted
			} else if s.ok == 0 {
				newStatus = StatusFailed
			} else {
				newStatus = StatusPartial
			}
			now := time.Now()
			completedAt = &now
		} else {
			newStatus = StatusRunning
		}
		if _, err := d.pool.Exec(ctx, `
			UPDATE jobs
			SET status = $2,
			    pending_items = $3,
			    success_items = $4,
			    failed_items = $5,
			    started_at = COALESCE(started_at, CASE WHEN $2 = 'running' THEN now() ELSE NULL END),
			    completed_at = $6
			WHERE id = $1
		`, s.id, newStatus, s.pend, s.ok, s.fail, completedAt); err != nil {
			return err
		}
	}
	return nil
}
// Package jobs administra trabajos de ejecución masiva o dirigida.
//
// En Fase 1 el dispatcher real no existe todavía (los agentes aún no
// entienden `command` por WS); los `job_items` quedan en estado `pending`
// y se despacharán cuando llegue Fase 3.
package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Estados posibles.
const (
	StatusPending     = "pending"
	StatusDispatching = "dispatching"
	StatusRunning     = "running"
	StatusCompleted   = "completed"
	StatusFailed      = "failed"
	StatusPartial     = "partial"
	StatusCancelled   = "cancelled"
)

// Estados de items.
const (
	ItemPending    = "pending"
	ItemDispatched = "dispatched"
	ItemRunning    = "running"
	ItemCompleted  = "completed"
	ItemFailed     = "failed"
	ItemTimeout    = "timeout"
	ItemCancelled  = "cancelled"
	ItemOffline    = "offline"
)

// Source values.
const (
	SourceTemplate = "template"
	SourceInline   = "inline"
)

// TargetType values.
const (
	TargetAgent = "agent"
	TargetGroup = "group"
	TargetAll   = "all"
)

// Job representa un trabajo.
type Job struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Description   string     `json:"description,omitempty"`
	Source        string     `json:"source"`
	TemplateID    *string    `json:"template_id,omitempty"`
	InlineCommand *string    `json:"inline_command,omitempty"`
	InlineArgs    []string   `json:"inline_args"`
	InlineTimeout *int       `json:"inline_timeout,omitempty"`
	TargetType    string     `json:"target_type"`
	TargetID      *string    `json:"target_id,omitempty"`
	Status        string     `json:"status"`
	TotalItems    int        `json:"total_items"`
	PendingItems  int        `json:"pending_items"`
	SuccessItems  int        `json:"success_items"`
	FailedItems   int        `json:"failed_items"`
	ScheduledAt   *time.Time `json:"scheduled_at,omitempty"`
	StartedAt     *time.Time `json:"started_at,omitempty"`
	CompletedAt   *time.Time `json:"completed_at,omitempty"`
	CreatedBy     string     `json:"created_by"`
	CreatedAt     time.Time  `json:"created_at"`
}

// Item es un item individual (un agente dentro del trabajo).
type Item struct {
	ID          string     `json:"id"`
	JobID       string     `json:"job_id"`
	AgentID     string     `json:"agent_id"`
	AgentHost   string     `json:"agent_hostname,omitempty"`
	AgentOS     string     `json:"agent_os,omitempty"`
	Status      string     `json:"status"`
	ExitCode    *int       `json:"exit_code,omitempty"`
	Stdout      string     `json:"stdout,omitempty"`
	Stderr      string     `json:"stderr,omitempty"`
	ErrorMsg    string     `json:"error_msg,omitempty"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// CreateInput body para crear un trabajo.
type CreateInput struct {
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Source        string   `json:"source"` // "template" | "inline"
	TemplateID    *string  `json:"template_id"`
	InlineCommand *string  `json:"inline_command"`
	InlineArgs    []string `json:"inline_args"`
	InlineTimeout *int     `json:"inline_timeout"`
	TargetType    string   `json:"target_type"` // "agent" | "group" | "all"
	TargetID      *string  `json:"target_id"`
	CreatedBy     string   `json:"-"`
}

// Create crea un trabajo y los job_items correspondientes.
func Create(ctx context.Context, pool *pgxpool.Pool, in CreateInput) (*Job, error) {
	if in.Source != SourceTemplate && in.Source != SourceInline {
		return nil, fmt.Errorf("invalid source: %s", in.Source)
	}
	if in.Source == SourceTemplate && (in.TemplateID == nil || *in.TemplateID == "") {
		return nil, errors.New("template_id required when source=template")
	}
	if in.Source == SourceInline && (in.InlineCommand == nil || strings.TrimSpace(*in.InlineCommand) == "") {
		return nil, errors.New("inline_command required when source=inline")
	}
	if in.TargetType != TargetAgent && in.TargetType != TargetGroup && in.TargetType != TargetAll {
		return nil, fmt.Errorf("invalid target_type: %s", in.TargetType)
	}
	if (in.TargetType == TargetAgent || in.TargetType == TargetGroup) && (in.TargetID == nil || *in.TargetID == "") {
		return nil, errors.New("target_id required for target_type=agent|group")
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	argsJSON, _ := json.Marshal(orEmpty(in.InlineArgs))

	row := tx.QueryRow(ctx, `
		INSERT INTO jobs (name, description, source, template_id, inline_command, inline_args,
		                  inline_timeout, target_type, target_id, created_by)
		VALUES ($1, NULLIF($2,''), $3, NULLIF($4,'')::uuid, $5, $6::jsonb, $7, $8, NULLIF($9,'')::uuid, $10)
		RETURNING id
	`, in.Name, in.Description, in.Source, derefStr(in.TemplateID), in.InlineCommand, string(argsJSON),
		in.InlineTimeout, in.TargetType, derefStr(in.TargetID), in.CreatedBy)
	var jobID string
	if err := row.Scan(&jobID); err != nil {
		return nil, err
	}

	// Resolver agentes objetivo.
	agentIDs, err := resolveAgents(ctx, tx, in)
	if err != nil {
		return nil, err
	}
	for _, aid := range agentIDs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO job_items (job_id, agent_id) VALUES ($1, $2)
		`, jobID, aid); err != nil {
			return nil, err
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE jobs SET total_items = $1, pending_items = $1 WHERE id = $2
	`, len(agentIDs), jobID); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return Get(ctx, pool, jobID)
}

func resolveAgents(ctx context.Context, tx pgx.Tx, in CreateInput) ([]string, error) {
	switch in.TargetType {
	case TargetAgent:
		return []string{*in.TargetID}, nil
	case TargetAll:
		rows, err := tx.Query(ctx, `SELECT id FROM agents`)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return nil, err
			}
			out = append(out, id)
		}
		return out, rows.Err()
	case TargetGroup:
		rows, err := tx.Query(ctx, `SELECT agent_id FROM agent_group_members WHERE group_id = $1`, *in.TargetID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return nil, err
			}
			out = append(out, id)
		}
		return out, rows.Err()
	}
	return nil, fmt.Errorf("unsupported target_type: %s", in.TargetType)
}

// Get devuelve un trabajo con conteos.
func Get(ctx context.Context, pool *pgxpool.Pool, id string) (*Job, error) {
	row := pool.QueryRow(ctx, jobSelect+` WHERE id = $1`, id)
	return scanJob(row)
}

// ListOptions filtros para List.
type ListOptions struct {
	Status    string
	CreatedBy string
	Page      int
	PerPage   int
}

// List devuelve trabajos paginados.
func List(ctx context.Context, pool *pgxpool.Pool, opts ListOptions) ([]*Job, int, error) {
	if opts.PerPage <= 0 || opts.PerPage > 200 {
		opts.PerPage = 25
	}
	if opts.Page <= 0 {
		opts.Page = 1
	}
	where := []string{"1=1"}
	args := []any{}
	idx := 1
	if opts.Status != "" {
		where = append(where, fmt.Sprintf("status = $%d", idx))
		args = append(args, opts.Status)
		idx++
	}
	if opts.CreatedBy != "" {
		where = append(where, fmt.Sprintf("created_by = $%d", idx))
		args = append(args, opts.CreatedBy)
		idx++
	}
	whereSQL := strings.Join(where, " AND ")

	var total int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM jobs WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	limitIdx := idx
	offsetIdx := idx + 1
	args = append(args, opts.PerPage, (opts.Page-1)*opts.PerPage)

	q := fmt.Sprintf(jobSelect+`
		WHERE %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d
	`, whereSQL, limitIdx, offsetIdx)

	rows, err := pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, j)
	}
	return out, total, rows.Err()
}

// Cancel marca el trabajo como cancelado y sus items pendientes como cancelled.
func Cancel(ctx context.Context, pool *pgxpool.Pool, id string) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var status string
	if err := tx.QueryRow(ctx, `SELECT status FROM jobs WHERE id = $1 FOR UPDATE`, id).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if status == StatusCompleted || status == StatusCancelled || status == StatusFailed {
		return fmt.Errorf("cannot cancel job in status %s", status)
	}
	if _, err := tx.Exec(ctx, `UPDATE jobs SET status = $1, completed_at = now() WHERE id = $2`,
		StatusCancelled, id); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE job_items SET status = $1, completed_at = now()
		WHERE job_id = $2 AND status IN ('pending','dispatched','running')
	`, ItemCancelled, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ListItems devuelve los items de un trabajo paginados.
func ListItems(ctx context.Context, pool *pgxpool.Pool, jobID string, page, perPage int) ([]*Item, int, error) {
	if perPage <= 0 || perPage > 500 {
		perPage = 50
	}
	if page <= 0 {
		page = 1
	}
	var total int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM job_items WHERE job_id = $1`, jobID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := pool.Query(ctx, `
		SELECT i.id, i.job_id, i.agent_id, COALESCE(a.hostname,''), COALESCE(a.os,''),
		       i.status, i.exit_code, COALESCE(i.stdout,''), COALESCE(i.stderr,''),
		       COALESCE(i.error_msg,''), i.started_at, i.completed_at, i.created_at
		FROM job_items i
		LEFT JOIN agents a ON a.id = i.agent_id
		WHERE i.job_id = $1
		ORDER BY i.created_at
		LIMIT $2 OFFSET $3
	`, jobID, perPage, (page-1)*perPage)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*Item
	for rows.Next() {
		it := &Item{}
		if err := rows.Scan(&it.ID, &it.JobID, &it.AgentID, &it.AgentHost, &it.AgentOS,
			&it.Status, &it.ExitCode, &it.Stdout, &it.Stderr, &it.ErrorMsg,
			&it.StartedAt, &it.CompletedAt, &it.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, it)
	}
	return out, total, rows.Err()
}

// GetItem devuelve un item con stdout/stderr completos.
func GetItem(ctx context.Context, pool *pgxpool.Pool, jobID, itemID string) (*Item, error) {
	row := pool.QueryRow(ctx, `
		SELECT i.id, i.job_id, i.agent_id, COALESCE(a.hostname,''), COALESCE(a.os,''),
		       i.status, i.exit_code, COALESCE(i.stdout,''), COALESCE(i.stderr,''),
		       COALESCE(i.error_msg,''), i.started_at, i.completed_at, i.created_at
		FROM job_items i
		LEFT JOIN agents a ON a.id = i.agent_id
		WHERE i.id = $1 AND i.job_id = $2
	`, itemID, jobID)
	it := &Item{}
	if err := row.Scan(&it.ID, &it.JobID, &it.AgentID, &it.AgentHost, &it.AgentOS,
		&it.Status, &it.ExitCode, &it.Stdout, &it.Stderr, &it.ErrorMsg,
		&it.StartedAt, &it.CompletedAt, &it.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return it, nil
}

// -----------------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------------

const jobSelect = `
	SELECT id, name, COALESCE(description,''), source, template_id,
	       inline_command, inline_args, inline_timeout,
	       target_type, target_id, status, total_items, pending_items,
	       success_items, failed_items, scheduled_at, started_at, completed_at,
	       created_by, created_at
	FROM jobs
`

func scanJob(r interface{ Scan(...any) error }) (*Job, error) {
	var (
		j  Job
		ab []byte
	)
	if err := r.Scan(&j.ID, &j.Name, &j.Description, &j.Source, &j.TemplateID,
		&j.InlineCommand, &ab, &j.InlineTimeout,
		&j.TargetType, &j.TargetID, &j.Status, &j.TotalItems, &j.PendingItems,
		&j.SuccessItems, &j.FailedItems, &j.ScheduledAt, &j.StartedAt, &j.CompletedAt,
		&j.CreatedBy, &j.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if len(ab) > 0 {
		_ = json.Unmarshal(ab, &j.InlineArgs)
	}
	if j.InlineArgs == nil {
		j.InlineArgs = []string{}
	}
	return &j, nil
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// Errores públicos.
var ErrNotFound = errors.New("job not found")
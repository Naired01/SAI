package api

import (
	"context"
	"encoding/csv"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Naired01/SAI/internal/httpx"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AuditEventDTO es la fila plana que devolvemos al panel.
type AuditEventDTO struct {
	ID          int64          `json:"id"`
	OccurredAt  time.Time      `json:"occurred_at"`
	ActorType   string         `json:"actor_type"`
	ActorID     *string        `json:"actor_id,omitempty"`
	ActorLabel  string         `json:"actor_label"`
	Action      string         `json:"action"`
	TargetType  *string        `json:"target_type,omitempty"`
	TargetID    *string        `json:"target_id,omitempty"`
	TargetLabel *string        `json:"target_label,omitempty"`
	IP          *string        `json:"ip,omitempty"`
	UserAgent   *string        `json:"user_agent,omitempty"`
	Metadata    map[string]any `json:"metadata"`
}

// AuditFilters filtros soportados.
type AuditFilters struct {
	DateFrom   string
	DateTo     string
	ActorType  string
	Action     string
	TargetType string
	Search     string
	Page       int
	PerPage    int
}

// handleAuditList GET /api/v1/audit/events
func (s *Server) handleAuditList(w http.ResponseWriter, r *http.Request) {
	f := parseAuditFilters(r)
	items, total, err := queryAuditEvents(r.Context(), s.Pool, f)
	if err != nil {
		s.Logger.Error("audit list", "err", err)
		httpx.RenderInternalError(w, r, s.Bundle)
		return
	}
	httpx.RenderJSON(w, http.StatusOK, map[string]any{
		"items":    items,
		"total":    total,
		"page":     f.Page,
		"per_page": f.PerPage,
	})
}

// handleAuditGet GET /api/v1/audit/events/{id}
func (s *Server) handleAuditGet(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		httpx.RenderValidationError(w, r, s.Bundle, "invalid id")
		return
	}
	row := s.Pool.QueryRow(r.Context(), `
		SELECT id, occurred_at, actor_type, actor_id, actor_label, action,
		       target_type, target_id, target_label,
		       host(ip), user_agent, metadata
		FROM audit_events WHERE id = $1
	`, id)
	var e AuditEventDTO
	if err := row.Scan(&e.ID, &e.OccurredAt, &e.ActorType, &e.ActorID, &e.ActorLabel, &e.Action,
		&e.TargetType, &e.TargetID, &e.TargetLabel, &e.IP, &e.UserAgent, &e.Metadata); err != nil {
		httpx.RenderNotFound(w, r, s.Bundle)
		return
	}
	httpx.RenderJSON(w, http.StatusOK, e)
}

// handleAuditActions GET /api/v1/audit/actions
func (s *Server) handleAuditActions(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Pool.Query(r.Context(),
		`SELECT DISTINCT action FROM audit_events ORDER BY action`)
	if err != nil {
		httpx.RenderInternalError(w, r, s.Bundle)
		return
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var a string
		_ = rows.Scan(&a)
		out = append(out, a)
	}
	httpx.RenderJSON(w, http.StatusOK, map[string]any{"items": out})
}

// handleAuditExportCSV GET /api/v1/audit/export.csv
func (s *Server) handleAuditExportCSV(w http.ResponseWriter, r *http.Request) {
	f := parseAuditFilters(r)
	f.PerPage = 100000 // cap razonable
	items, _, err := queryAuditEvents(r.Context(), s.Pool, f)
	if err != nil {
		httpx.RenderInternalError(w, r, s.Bundle)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="audit.csv"`)
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"id", "occurred_at", "actor_type", "actor_label", "action", "target_type", "target_label", "ip"})
	for _, e := range items {
		_ = cw.Write([]string{
			strconv.FormatInt(e.ID, 10),
			e.OccurredAt.UTC().Format(time.RFC3339),
			e.ActorType, e.ActorLabel, e.Action,
			deref(e.TargetType), deref(e.TargetLabel), deref(e.IP),
		})
	}
	cw.Flush()
}

func parseAuditFilters(r *http.Request) AuditFilters {
	return AuditFilters{
		DateFrom:   httpx.QueryString(r, "date_from", ""),
		DateTo:     httpx.QueryString(r, "date_to", ""),
		ActorType:  httpx.QueryString(r, "actor_type", ""),
		Action:     httpx.QueryString(r, "action", ""),
		TargetType: httpx.QueryString(r, "target_type", ""),
		Search:     httpx.QueryString(r, "q", ""),
		Page:       httpx.QueryInt(r, "page", 1),
		PerPage:    httpx.QueryInt(r, "per_page", 25),
	}
}

func queryAuditEvents(ctx context.Context, pool *pgxpool.Pool, f AuditFilters) ([]*AuditEventDTO, int, error) {
	if f.PerPage <= 0 || f.PerPage > 500 {
		f.PerPage = 25
	}
	if f.Page <= 0 {
		f.Page = 1
	}
	where := []string{"1=1"}
	args := []any{}
	idx := 1
	if f.DateFrom != "" {
		where = append(where, "occurred_at >= $"+strconv.Itoa(idx))
		args = append(args, f.DateFrom)
		idx++
	}
	if f.DateTo != "" {
		where = append(where, "occurred_at <= $"+strconv.Itoa(idx))
		args = append(args, f.DateTo)
		idx++
	}
	if f.ActorType != "" {
		where = append(where, "actor_type = $"+strconv.Itoa(idx))
		args = append(args, f.ActorType)
		idx++
	}
	if f.Action != "" {
		where = append(where, "action = $"+strconv.Itoa(idx))
		args = append(args, f.Action)
		idx++
	}
	if f.TargetType != "" {
		where = append(where, "target_type = $"+strconv.Itoa(idx))
		args = append(args, f.TargetType)
		idx++
	}
	if s := strings.TrimSpace(f.Search); s != "" {
		where = append(where, "(actor_label ILIKE $"+strconv.Itoa(idx)+" OR target_label ILIKE $"+strconv.Itoa(idx)+" OR action ILIKE $"+strconv.Itoa(idx)+")")
		args = append(args, "%"+s+"%")
		idx++
	}
	whereSQL := strings.Join(where, " AND ")

	var total int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_events WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, f.PerPage, (f.Page-1)*f.PerPage)
	q := `SELECT id, occurred_at, actor_type, actor_id, actor_label, action,
	             target_type, target_id, target_label, host(ip), user_agent, metadata
	      FROM audit_events WHERE ` + whereSQL + `
	      ORDER BY occurred_at DESC LIMIT $` + strconv.Itoa(idx) + ` OFFSET $` + strconv.Itoa(idx+1)
	rows, err := pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*AuditEventDTO
	for rows.Next() {
		var e AuditEventDTO
		if err := rows.Scan(&e.ID, &e.OccurredAt, &e.ActorType, &e.ActorID, &e.ActorLabel, &e.Action,
			&e.TargetType, &e.TargetID, &e.TargetLabel, &e.IP, &e.UserAgent, &e.Metadata); err != nil {
			return nil, 0, err
		}
		out = append(out, &e)
	}
	return out, total, rows.Err()
}
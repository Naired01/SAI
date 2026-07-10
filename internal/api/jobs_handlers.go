package api

import (
	"encoding/csv"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/Naired01/SAI/internal/audit"
	"github.com/Naired01/SAI/internal/httpx"
	"github.com/Naired01/SAI/internal/jobs"
	"github.com/go-chi/chi/v5"
)

// handleJobsList GET /api/v1/jobs
func (s *Server) handleJobsList(w http.ResponseWriter, r *http.Request) {
	opts := jobs.ListOptions{
		Status:    httpx.QueryString(r, "status", ""),
		CreatedBy: httpx.QueryString(r, "created_by", ""),
		Page:      httpx.QueryInt(r, "page", 1),
		PerPage:   httpx.QueryInt(r, "per_page", 25),
	}
	items, total, err := jobs.List(r.Context(), s.Pool, opts)
	if err != nil {
		s.Logger.Error("jobs list", "err", err)
		httpx.RenderInternalError(w, r, s.Bundle)
		return
	}
	httpx.RenderJSON(w, http.StatusOK, map[string]any{
		"items":    items,
		"total":    total,
		"page":     opts.Page,
		"per_page": opts.PerPage,
	})
}

// handleJobsCreate POST /api/v1/jobs
func (s *Server) handleJobsCreate(w http.ResponseWriter, r *http.Request) {
	var body jobs.CreateInput
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.RenderValidationError(w, r, s.Bundle, "invalid body")
		return
	}
	uid := userIDFromContext(r.Context())
	body.CreatedBy = uid
	j, err := jobs.Create(r.Context(), s.Pool, body)
	if err != nil {
		httpx.RenderValidationError(w, r, s.Bundle, err.Error())
		return
	}
	audit.Record(r.Context(), s.Pool, audit.Event{
		Actor:   audit.Actor{Type: "user", ID: &uid, Label: emailFromContext(r.Context())},
		Action:  audit.ActionJobCreate,
		Target:  &audit.Target{Type: "job", ID: &j.ID, Label: j.Name},
		Request: r,
		Metadata: map[string]any{
			"source":     j.Source,
			"target_type": j.TargetType,
			"items":      j.TotalItems,
		},
	})
	httpx.RenderJSON(w, http.StatusCreated, j)
}

// handleJobsGet GET /api/v1/jobs/{id}
func (s *Server) handleJobsGet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	j, err := jobs.Get(r.Context(), s.Pool, id)
	if err != nil {
		if errors.Is(err, jobs.ErrNotFound) {
			httpx.RenderNotFound(w, r, s.Bundle)
			return
		}
		httpx.RenderInternalError(w, r, s.Bundle)
		return
	}
	httpx.RenderJSON(w, http.StatusOK, j)
}

// handleJobsCancel POST /api/v1/jobs/{id}/cancel
func (s *Server) handleJobsCancel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := jobs.Cancel(r.Context(), s.Pool, id); err != nil {
		if errors.Is(err, jobs.ErrNotFound) {
			httpx.RenderNotFound(w, r, s.Bundle)
			return
		}
		httpx.RenderValidationError(w, r, s.Bundle, err.Error())
		return
	}
	uid := userIDFromContext(r.Context())
	audit.Record(r.Context(), s.Pool, audit.Event{
		Actor:   audit.Actor{Type: "user", ID: &uid, Label: emailFromContext(r.Context())},
		Action:  audit.ActionJobCancel,
		Target:  &audit.Target{Type: "job", ID: &id, Label: id},
		Request: r,
	})
	httpx.RenderJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleJobsItems GET /api/v1/jobs/{id}/items
func (s *Server) handleJobsItems(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	page := httpx.QueryInt(r, "page", 1)
	perPage := httpx.QueryInt(r, "per_page", 50)
	items, total, err := jobs.ListItems(r.Context(), s.Pool, id, page, perPage)
	if err != nil {
		httpx.RenderInternalError(w, r, s.Bundle)
		return
	}
	httpx.RenderJSON(w, http.StatusOK, map[string]any{
		"items":    items,
		"total":    total,
		"page":     page,
		"per_page": perPage,
	})
}

// handleJobsItem GET /api/v1/jobs/{id}/items/{itemId}
func (s *Server) handleJobsItem(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "id")
	itemID := chi.URLParam(r, "itemId")
	it, err := jobs.GetItem(r.Context(), s.Pool, jobID, itemID)
	if err != nil {
		if errors.Is(err, jobs.ErrNotFound) {
			httpx.RenderNotFound(w, r, s.Bundle)
			return
		}
		httpx.RenderInternalError(w, r, s.Bundle)
		return
	}
	httpx.RenderJSON(w, http.StatusOK, it)
}

// handleAgentJobs GET /api/v1/agents/{id}/jobs
//
// Devuelve los ultimos N job_items del agente con el nombre del job
// incluido, ordenados por created_at desc. Usado por la tab "Comandos"
// de AgentDetail para mostrar el historial reciente.
func (s *Server) handleAgentJobs(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "id")
	limit := httpx.QueryInt(r, "limit", 20)
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	rows, err := s.Pool.Query(r.Context(), `
		SELECT i.id, i.job_id, i.status, i.exit_code, i.error_msg,
		       i.started_at, i.completed_at, j.name AS job_name
		FROM job_items i
		JOIN jobs j ON j.id = i.job_id
		WHERE i.agent_id = $1
		ORDER BY i.created_at DESC
		LIMIT $2
	`, agentID, limit)
	if err != nil {
		s.Logger.Error("agent jobs query", "agent_id", agentID, "err", err)
		httpx.RenderInternalError(w, r, s.Bundle)
		return
	}
	defer rows.Close()
	type row struct {
		JobItemID    string     `json:"id"`
		JobID        string     `json:"job_id"`
		JobName      string     `json:"job_name"`
		Status       string     `json:"status"`
		ExitCode     *int       `json:"exit_code,omitempty"`
		ErrorMsg     string     `json:"error_msg,omitempty"`
		StartedAt    *time.Time `json:"started_at,omitempty"`
		CompletedAt  *time.Time `json:"completed_at,omitempty"`
	}
	var out []row
	for rows.Next() {
		var item row
		if err := rows.Scan(&item.JobItemID, &item.JobID, &item.Status, &item.ExitCode, &item.ErrorMsg,
			&item.StartedAt, &item.CompletedAt, &item.JobName); err != nil {
			s.Logger.Error("agent jobs scan", "err", err)
			httpx.RenderInternalError(w, r, s.Bundle)
			return
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		httpx.RenderInternalError(w, r, s.Bundle)
		return
	}
	httpx.RenderJSON(w, http.StatusOK, map[string]any{"items": out, "total": len(out)})
}

// handleJobsExportCSV GET /api/v1/jobs/{id}/export.csv
func (s *Server) handleJobsExportCSV(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	items, _, err := jobs.ListItems(r.Context(), s.Pool, id, 1, 10000)
	if err != nil {
		httpx.RenderInternalError(w, r, s.Bundle)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="job-`+id+`.csv"`)
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"agent_id", "agent_hostname", "agent_os", "status", "exit_code", "started_at", "completed_at", "stdout", "stderr", "error_msg"})
	for _, it := range items {
		_ = cw.Write([]string{
			it.AgentID, it.AgentHost, it.AgentOS, it.Status,
			strconv.Itoa(derefInt(it.ExitCode)),
			fmtTime(it.StartedAt), fmtTime(it.CompletedAt),
			it.Stdout, it.Stderr, it.ErrorMsg,
		})
	}
	cw.Flush()
}

func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func fmtTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
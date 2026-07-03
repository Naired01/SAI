package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/Naired01/SAI/internal/audit"
	"github.com/Naired01/SAI/internal/httpx"
	"github.com/Naired01/SAI/internal/jobs"
	"github.com/Naired01/SAI/internal/templates"
	"github.com/go-chi/chi/v5"
)

// handleTemplatesList GET /api/v1/templates
func (s *Server) handleTemplatesList(w http.ResponseWriter, r *http.Request) {
	opts := templates.ListOptions{
		Category: httpx.QueryString(r, "category", ""),
		Search:   httpx.QueryString(r, "q", ""),
	}
	items, err := templates.List(r.Context(), s.Pool, opts)
	if err != nil {
		s.Logger.Error("templates list", "err", err)
		httpx.RenderInternalError(w, r, s.Bundle)
		return
	}
	httpx.RenderJSON(w, http.StatusOK, map[string]any{"items": items})
}

// handleTemplatesCreate POST /api/v1/templates
func (s *Server) handleTemplatesCreate(w http.ResponseWriter, r *http.Request) {
	var body templates.CreateInput
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.RenderValidationError(w, r, s.Bundle, "invalid body: "+err.Error())
		return
	}
	uid := userIDFromContext(r.Context())
	body.CreatedBy = &uid
	t, err := templates.Create(r.Context(), s.Pool, body)
	if err != nil {
		httpx.RenderValidationError(w, r, s.Bundle, err.Error())
		return
	}
	audit.Record(r.Context(), s.Pool, audit.Event{
		Actor:   audit.Actor{Type: "user", ID: &uid, Label: emailFromContext(r.Context())},
		Action:  audit.ActionTemplateCreate,
		Target:  &audit.Target{Type: "template", ID: &t.ID, Label: t.Name},
		Request: r,
		Metadata: map[string]any{"category": t.Category},
	})
	httpx.RenderJSON(w, http.StatusCreated, t)
}

// handleTemplatesGet GET /api/v1/templates/{id}
func (s *Server) handleTemplatesGet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	t, err := templates.Get(r.Context(), s.Pool, id)
	if err != nil {
		if errors.Is(err, templates.ErrNotFound) {
			httpx.RenderNotFound(w, r, s.Bundle)
			return
		}
		httpx.RenderInternalError(w, r, s.Bundle)
		return
	}
	httpx.RenderJSON(w, http.StatusOK, t)
}

// handleTemplatesUpdate PATCH /api/v1/templates/{id}
func (s *Server) handleTemplatesUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var body templates.UpdateInput
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.RenderValidationError(w, r, s.Bundle, "invalid body")
		return
	}
	if err := templates.Update(r.Context(), s.Pool, id, body); err != nil {
		if errors.Is(err, templates.ErrBuiltin) {
			httpx.RenderJSON(w, http.StatusForbidden, httpx.Error{Code: "builtin", Message: err.Error()})
			return
		}
		if errors.Is(err, templates.ErrNotFound) {
			httpx.RenderNotFound(w, r, s.Bundle)
			return
		}
		httpx.RenderValidationError(w, r, s.Bundle, err.Error())
		return
	}
	uid := userIDFromContext(r.Context())
	audit.Record(r.Context(), s.Pool, audit.Event{
		Actor:   audit.Actor{Type: "user", ID: &uid, Label: emailFromContext(r.Context())},
		Action:  audit.ActionTemplateUpdate,
		Target:  &audit.Target{Type: "template", ID: &id, Label: id},
		Request: r,
	})
	t, _ := templates.Get(r.Context(), s.Pool, id)
	httpx.RenderJSON(w, http.StatusOK, t)
}

// handleTemplatesDelete DELETE /api/v1/templates/{id}
func (s *Server) handleTemplatesDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := templates.Delete(r.Context(), s.Pool, id); err != nil {
		if errors.Is(err, templates.ErrBuiltin) {
			httpx.RenderJSON(w, http.StatusForbidden, httpx.Error{Code: "builtin", Message: err.Error()})
			return
		}
		if errors.Is(err, templates.ErrNotFound) {
			httpx.RenderNotFound(w, r, s.Bundle)
			return
		}
		httpx.RenderInternalError(w, r, s.Bundle)
		return
	}
	uid := userIDFromContext(r.Context())
	audit.Record(r.Context(), s.Pool, audit.Event{
		Actor:   audit.Actor{Type: "user", ID: &uid, Label: emailFromContext(r.Context())},
		Action:  audit.ActionTemplateDelete,
		Target:  &audit.Target{Type: "template", ID: &id, Label: id},
		Request: r,
	})
	httpx.RenderJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleTemplatesRun POST /api/v1/templates/{id}/run
func (s *Server) handleTemplatesRun(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	t, err := templates.Get(r.Context(), s.Pool, id)
	if err != nil {
		if errors.Is(err, templates.ErrNotFound) {
			httpx.RenderNotFound(w, r, s.Bundle)
			return
		}
		httpx.RenderInternalError(w, r, s.Bundle)
		return
	}
	var body struct {
		Name        string  `json:"name"`
		TargetType  string  `json:"target_type"`
		TargetID    *string `json:"target_id"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.RenderValidationError(w, r, s.Bundle, "invalid body")
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		body.Name = "Run: " + t.Name
	}
	uid := userIDFromContext(r.Context())
	job, err := jobs.Create(r.Context(), s.Pool, jobs.CreateInput{
		Name:          body.Name,
		Source:        jobs.SourceTemplate,
		TemplateID:    &t.ID,
		InlineCommand: nil,
		InlineArgs:    nil,
		InlineTimeout: nil,
		TargetType:    body.TargetType,
		TargetID:      body.TargetID,
		CreatedBy:     uid,
	})
	if err != nil {
		httpx.RenderValidationError(w, r, s.Bundle, err.Error())
		return
	}
	audit.Record(r.Context(), s.Pool, audit.Event{
		Actor:   audit.Actor{Type: "user", ID: &uid, Label: emailFromContext(r.Context())},
		Action:  audit.ActionTemplateExecute,
		Target:  &audit.Target{Type: "template", ID: &t.ID, Label: t.Name},
		Request: r,
		Metadata: map[string]any{"job_id": job.ID, "target_type": body.TargetType},
	})
	httpx.RenderJSON(w, http.StatusCreated, job)
}
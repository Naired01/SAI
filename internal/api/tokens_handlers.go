package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/Naired01/SAI/internal/audit"
	"github.com/Naired01/SAI/internal/httpx"
	"github.com/Naired01/SAI/internal/tokens"
	"github.com/go-chi/chi/v5"
)

// handleTokensList GET /api/v1/tokens
func (s *Server) handleTokensList(w http.ResponseWriter, r *http.Request) {
	toks, err := tokens.List(r.Context(), s.Pool)
	if err != nil {
		s.Logger.Error("tokens list", "err", err)
		httpx.RenderInternalError(w, r, s.Bundle)
		return
	}
	httpx.RenderJSON(w, http.StatusOK, map[string]any{"items": toks})
}

// handleTokensCreate POST /api/v1/tokens
func (s *Server) handleTokensCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Label    string `json:"label"`
		MaxUses  int    `json:"max_uses"`
		TTLHours int    `json:"ttl_hours"` // 0 = default
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.RenderValidationError(w, r, s.Bundle, "invalid body")
		return
	}
	if strings.TrimSpace(body.Label) == "" {
		body.Label = "token"
	}
	if body.MaxUses <= 0 {
		body.MaxUses = 1
	}
	ttl := time.Duration(body.TTLHours) * time.Hour
	uid := userIDFromContext(r.Context())
	plain, t, err := tokens.Create(r.Context(), s.Pool, body.Label, uid, body.MaxUses, ttl)
	if err != nil {
		s.Logger.Error("tokens create", "err", err)
		httpx.RenderInternalError(w, r, s.Bundle)
		return
	}
	audit.Record(r.Context(), s.Pool, audit.Event{
		Actor:   audit.Actor{Type: "user", ID: &uid, Label: emailFromContext(r.Context())},
		Action:  audit.ActionTokenCreate,
		Target:  &audit.Target{Type: "token", ID: &t.ID, Label: t.Label},
		Request: r,
		Metadata: map[string]any{"max_uses": t.MaxUses, "ttl_seconds": int64(ttl.Seconds())},
	})
	httpx.RenderJSON(w, http.StatusCreated, map[string]any{
		"token":        t,
		"plain":        plain,
		"download_url": "/api/v1/agents/download?token=" + plain,
	})
}

// handleTokensRevoke POST /api/v1/tokens/{id}/revoke
func (s *Server) handleTokensRevoke(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := tokens.Revoke(r.Context(), s.Pool, id); err != nil {
		if err == tokens.ErrNotFound {
			httpx.RenderNotFound(w, r, s.Bundle)
			return
		}
		s.Logger.Error("tokens revoke", "err", err)
		httpx.RenderInternalError(w, r, s.Bundle)
		return
	}
	uid := userIDFromContext(r.Context())
	audit.Record(r.Context(), s.Pool, audit.Event{
		Actor:   audit.Actor{Type: "user", ID: &uid, Label: emailFromContext(r.Context())},
		Action:  audit.ActionTokenRevoke,
		Target:  &audit.Target{Type: "token", ID: &id, Label: id},
		Request: r,
	})
	httpx.RenderJSON(w, http.StatusOK, map[string]any{"ok": true})
}
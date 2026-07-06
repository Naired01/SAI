package api

import (
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/Naired01/SAI/internal/agents"
	"github.com/Naired01/SAI/internal/audit"
	"github.com/Naired01/SAI/internal/httpx"
	"github.com/Naired01/SAI/internal/inventory"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// refreshRateWindow es la cota anti-DoS por agente. Un admin que hace spam
// al botón "Solicitar inventario" no debe poder tumbar el flujo de un equipo.
const refreshRateWindow = 30 * time.Second

// inventoryRateLimiter mantiene un timestamp por agente. Si el último refresh
// fue hace menos de `window`, devuelve false. GC oportunista cada llamada.
type inventoryRateLimiter struct {
	mu     sync.Mutex
	last   map[string]time.Time
	window time.Duration
}

func newInventoryRateLimiter() *inventoryRateLimiter {
	return &inventoryRateLimiter{last: make(map[string]time.Time), window: refreshRateWindow}
}

func (r *inventoryRateLimiter) allowed(agentID string) bool {
	if r == nil {
		return true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	if prev, ok := r.last[agentID]; ok && now.Sub(prev) < r.window {
		return false
	}
	r.last[agentID] = now
	for k, v := range r.last {
		if now.Sub(v) > time.Hour {
			delete(r.last, k)
		}
	}
	return true
}

// -----------------------------------------------------------------------------
// Handlers
// -----------------------------------------------------------------------------

// handleInventoryRefresh POST /api/v1/agents/{id}/inventory/refresh
//
// Envía un inventory_request por WS al agente. Si está offline, devolvemos 202
// igualmente: cuando se reconecte el server-push on-welcome lo recogerá si
// el snapshot sigue stale.
func (s *Server) handleInventoryRefresh(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "id")
	if _, err := uuid.Parse(agentID); err != nil {
		httpx.RenderValidationError(w, r, s.Bundle, "invalid agent id")
		return
	}
	if !s.refreshLimiter.allowed(agentID) {
		httpx.RenderError(w, r, s.Bundle, http.StatusTooManyRequests, "common.error.too_many_requests")
		return
	}
	if _, err := agents.Get(r.Context(), s.Pool, agentID); err != nil {
		if errors.Is(err, agents.ErrNotFound) {
			httpx.RenderNotFound(w, r, s.Bundle)
			return
		}
		s.Logger.Error("inventory refresh: agent lookup", "err", err)
		httpx.RenderInternalError(w, r, s.Bundle)
		return
	}

	stale, err := inventory.StaleOrMissing(r.Context(), s.Pool, agentID, inventory.DefaultTTL)
	if err != nil {
		s.Logger.Error("inventory refresh: stale check", "err", err)
		httpx.RenderInternalError(w, r, s.Bundle)
		return
	}
	reqID := uuid.New().String()
	req := inventory.ReqMsg{Type: "inventory_request", ID: reqID, Include: []string{"hardware"}}
	delivered := s.Hub != nil && s.Hub.SendTo(agentID, req)

	actor := auditActorFromRequest(r)
	audit.Record(r.Context(), s.Pool, audit.Event{
		Actor:  actor,
		Action: audit.ActionInventoryRequested,
		Target: &audit.Target{Type: "agent", ID: &agentID, Label: agentID},
		Request: r,
		Metadata: map[string]any{
			"reason":     "manual",
			"request_id": reqID,
			"delivered":  delivered,
			"stale":      stale,
		},
	})
	_ = inventory.RecordEvent(r.Context(), s.Pool, agentID, "requested", uuid.MustParse(reqID), "", "")

	httpx.RenderJSON(w, http.StatusAccepted, map[string]any{
		"agent_id":   agentID,
		"request_id": reqID,
		"delivered":  delivered,
		"stale":      stale,
	})
}

// handleInventoryLatest GET /api/v1/agents/{id}/inventory
func (s *Server) handleInventoryLatest(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "id")
	if _, err := uuid.Parse(agentID); err != nil {
		httpx.RenderValidationError(w, r, s.Bundle, "invalid agent id")
		return
	}
	rec, err := inventory.Latest(r.Context(), s.Pool, agentID)
	if err != nil {
		if errors.Is(err, inventory.ErrNotFound) {
			httpx.RenderNotFound(w, r, s.Bundle)
			return
		}
		s.Logger.Error("inventory latest", "err", err)
		httpx.RenderInternalError(w, r, s.Bundle)
		return
	}
	httpx.RenderJSON(w, http.StatusOK, rec)
}

// handleInventoryHistory GET /api/v1/agents/{id}/inventory/history?limit=&before=
func (s *Server) handleInventoryHistory(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "id")
	if _, err := uuid.Parse(agentID); err != nil {
		httpx.RenderValidationError(w, r, s.Bundle, "invalid agent id")
		return
	}
	opts := inventory.HistoryOptions{
		Limit: httpx.QueryInt(r, "limit", 50),
	}
	if before := httpx.QueryString(r, "before", ""); before != "" {
		t, err := time.Parse(time.RFC3339Nano, before)
		if err != nil {
			httpx.RenderValidationError(w, r, s.Bundle, "before must be RFC3339Nano")
			return
		}
		opts.Before = t
	}
	recs, err := inventory.History(r.Context(), s.Pool, agentID, opts)
	if err != nil {
		s.Logger.Error("inventory history", "err", err)
		httpx.RenderInternalError(w, r, s.Bundle)
		return
	}
	httpx.RenderJSON(w, http.StatusOK, map[string]any{
		"items": recs,
	})
}

// -----------------------------------------------------------------------------
// Helpers locales
// -----------------------------------------------------------------------------

// auditActorFromRequest devuelve el Actor de auditoría a partir del usuario
// autenticado en el contexto (puesto por el middleware de admin).
func auditActorFromRequest(r *http.Request) audit.Actor {
	if uid, ok := r.Context().Value(ctxUserID).(string); ok && uid != "" {
		uid := uid
		email := emailFromContext(r.Context())
		if email == "" {
			email = uid
		}
		return audit.Actor{Type: "user", ID: &uid, Label: email}
	}
	return audit.Actor{Type: "system", Label: "system"}
}

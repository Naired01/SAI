package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/Naired01/SAI/internal/agents"
	"github.com/Naired01/SAI/internal/audit"
	"github.com/Naired01/SAI/internal/bundles"
	"github.com/Naired01/SAI/internal/groups"
	"github.com/Naired01/SAI/internal/httpx"
	"github.com/Naired01/SAI/internal/tokens"
	"github.com/Naired01/SAI/internal/ws"
	"github.com/go-chi/chi/v5"
)

// handleAgentsList GET /api/v1/agents
func (s *Server) handleAgentsList(w http.ResponseWriter, r *http.Request) {
	opts := agents.ListOptions{
		GroupID:   httpx.QueryString(r, "group_id", ""),
		Ungrouped: httpx.QueryBool(r, "ungrouped", false),
		Status:    httpx.QueryString(r, "status", ""),
		Search:    httpx.QueryString(r, "q", ""),
		Page:      httpx.QueryInt(r, "page", 1),
		PerPage:   httpx.QueryInt(r, "per_page", 25),
	}
	items, total, err := agents.List(r.Context(), s.Pool, opts)
	if err != nil {
		s.Logger.Error("agents list", "err", err)
		httpx.RenderInternalError(w, r, s.Bundle)
		return
	}
	httpx.RenderJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"total": total,
		"page":  opts.Page,
		"per_page": opts.PerPage,
	})
}

// handleAgentsGet GET /api/v1/agents/{id}
func (s *Server) handleAgentsGet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	a, err := agents.Get(r.Context(), s.Pool, id)
	if err != nil {
		if errors.Is(err, agents.ErrNotFound) {
			httpx.RenderNotFound(w, r, s.Bundle)
			return
		}
		httpx.RenderInternalError(w, r, s.Bundle)
		return
	}
	// marcar online desde el hub
	if s.Hub != nil {
		a.Online = s.Hub.IsConnected(a.ID)
	}
	httpx.RenderJSON(w, http.StatusOK, a)
}

// handleAgentsUpdate PATCH /api/v1/agents/{id}
func (s *Server) handleAgentsUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var in agents.Update
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.RenderValidationError(w, r, s.Bundle, "invalid body")
		return
	}
	uid := userIDFromContext(r.Context())
	if err := agents.UpdateAgent(r.Context(), s.Pool, id, in, &uid); err != nil {
		s.Logger.Error("agents update", "err", err)
		httpx.RenderInternalError(w, r, s.Bundle)
		return
	}
	// Audit granular
	if in.Visibility != nil {
		audit.Record(r.Context(), s.Pool, audit.Event{
			Actor:   audit.Actor{Type: "user", ID: &uid, Label: emailFromContext(r.Context())},
			Action:  audit.ActionAgentSetVisibility,
			Target:  &audit.Target{Type: "agent", ID: &id, Label: id},
			Request: r,
			Metadata: map[string]any{"visibility": *in.Visibility},
		})
	}
	if in.Labels != nil {
		audit.Record(r.Context(), s.Pool, audit.Event{
			Actor:   audit.Actor{Type: "user", ID: &uid, Label: emailFromContext(r.Context())},
			Action:  audit.ActionAgentUpdateLabels,
			Target:  &audit.Target{Type: "agent", ID: &id, Label: id},
			Request: r,
		})
	}
	if in.GroupIDs != nil {
		audit.Record(r.Context(), s.Pool, audit.Event{
			Actor:   audit.Actor{Type: "user", ID: &uid, Label: emailFromContext(r.Context())},
			Action:  audit.ActionGroupMoveAgent,
			Target:  &audit.Target{Type: "agent", ID: &id, Label: id},
			Request: r,
			Metadata: map[string]any{"group_ids": *in.GroupIDs},
		})
	}
	a, _ := agents.Get(r.Context(), s.Pool, id)
	httpx.RenderJSON(w, http.StatusOK, a)
}

// handleAgentsDelete DELETE /api/v1/agents/{id}
func (s *Server) handleAgentsDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := s.Pool.Exec(r.Context(), `DELETE FROM agents WHERE id = $1`, id); err != nil {
		s.Logger.Error("agents delete", "err", err)
		httpx.RenderInternalError(w, r, s.Bundle)
		return
	}
	uid := userIDFromContext(r.Context())
	audit.Record(r.Context(), s.Pool, audit.Event{
		Actor:   audit.Actor{Type: "user", ID: &uid, Label: emailFromContext(r.Context())},
		Action:  audit.ActionAgentDelete,
		Target:  &audit.Target{Type: "agent", ID: &id, Label: id},
		Request: r,
	})
	httpx.RenderJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleAgentsEvents GET /api/v1/agents/{id}/events
func (s *Server) handleAgentsEvents(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	limit := httpx.QueryInt(r, "limit", 100)
	events, err := agents.ListEvents(r.Context(), s.Pool, id, limit)
	if err != nil {
		s.Logger.Error("agents events", "err", err)
		httpx.RenderInternalError(w, r, s.Bundle)
		return
	}
	httpx.RenderJSON(w, http.StatusOK, map[string]any{"items": events})
}

// handleAgentDownload GET /api/v1/agents/download?token=...
//
// Endpoint PÚBLICO: valida el token, determina OS/arch, y sirve el ZIP.
func (s *Server) handleAgentDownload(w http.ResponseWriter, r *http.Request) {
	plain := r.URL.Query().Get("token")
	if strings.TrimSpace(plain) == "" {
		httpx.RenderValidationError(w, r, s.Bundle, "missing token")
		return
	}
	// canjear token (incrementa uses)
	if _, err := tokens.Redeem(r.Context(), s.Pool, plain); err != nil {
		httpx.RenderJSON(w, http.StatusForbidden, httpx.Error{
			Code:    "invalid_token",
			Message: err.Error(),
		})
		return
	}
	// armar bundle
	builder := bundles.New(s.BundleDir)
	osName := r.URL.Query().Get("os")
	arch := r.URL.Query().Get("arch")
	asset, err := builder.Asset(osName, arch)
	if err != nil {
		httpx.RenderJSON(w, http.StatusNotFound, httpx.Error{
			Code:    "binary_not_available",
			Message: err.Error(),
		})
		return
	}
	serverURL := s.PublicURL + "/api/v1/agent/ws"
	if !strings.HasPrefix(serverURL, "ws") && !strings.HasPrefix(serverURL, "http") {
		// construir WSS desde PublicURL
		serverURL = strings.Replace(s.PublicURL, "https://", "wss://", 1)
		serverURL = strings.Replace(serverURL, "http://", "ws://", 1)
		serverURL += "/api/v1/agent/ws"
	}
	cfg := bundles.Config{
		ServerURL:       serverURL,
		EnrollmentToken: plain,
		Labels:          map[string]any{},
	}
	data, err := builder.Build(asset, cfg, asset.OS == "windows")
	if err != nil {
		s.Logger.Error("build bundle", "err", err)
		httpx.RenderInternalError(w, r, s.Bundle)
		return
	}
	filename := "sai-agent-" + asset.OS + "-" + asset.Arch + ".zip"
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Header().Set("Content-Length", itoa(len(data)))
	_, _ = w.Write(data)
	// Audit (sin http.Request para no contaminar; ya se loggea en Redeem)
	audit.Record(r.Context(), s.Pool, audit.Event{
		Actor:  audit.Actor{Type: "token", Label: "enrollment"},
		Action: audit.ActionTokenConsumed,
		Metadata: map[string]any{"os": asset.OS, "arch": asset.Arch},
	})
}

func itoa(n int) string {
	// evitar strconv por simplicidad en este hot-path
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	buf := [20]byte{}
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// handleAgentWS GET /api/v1/agent/ws (WebSocket)
func (s *Server) handleAgentWS(w http.ResponseWriter, r *http.Request) {
	// El hub real se inyecta como *ws.Hub desde main.go (ver Server.Hub real).
	if s.Hub == nil {
		http.Error(w, "ws hub not configured", http.StatusInternalServerError)
		return
	}
	hub, ok := s.Hub.(*ws.Hub)
	if !ok {
		http.Error(w, "ws hub wrong type", http.StatusInternalServerError)
		return
	}
	handler := ws.Handler(ws.HandlerOptions{
		Pool:   s.Pool,
		Hub:    hub,
		Secret: s.AgentJWTSecret,
		Logger: s.Logger,
	})
	handler.ServeHTTP(w, r)
}

// helper para no importar groups en otros archivos
var _ = groups.Flat{}
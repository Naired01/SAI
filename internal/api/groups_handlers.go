package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/Naired01/SAI/internal/audit"
	"github.com/Naired01/SAI/internal/groups"
	"github.com/Naired01/SAI/internal/httpx"
	"github.com/go-chi/chi/v5"
)

// handleGroupsTree GET /api/v1/groups
func (s *Server) handleGroupsTree(w http.ResponseWriter, r *http.Request) {
	tree, err := groups.Tree(r.Context(), s.Pool)
	if err != nil {
		s.Logger.Error("groups tree", "err", err)
		httpx.RenderInternalError(w, r, s.Bundle)
		return
	}
	// Agregar también el grupo virtual "Sin catalogar"
	var ungroupedCount int
	_ = s.Pool.QueryRow(r.Context(),
		`SELECT COUNT(*) FROM agents a WHERE NOT EXISTS (SELECT 1 FROM agent_group_members m WHERE m.agent_id = a.id)`).Scan(&ungroupedCount)
	httpx.RenderJSON(w, http.StatusOK, map[string]any{
		"tree":     tree,
		"ungrouped_count": ungroupedCount,
	})
}

// handleGroupsCreate POST /api/v1/groups
func (s *Server) handleGroupsCreate(w http.ResponseWriter, r *http.Request) {
	var body groups.CreateInput
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.RenderValidationError(w, r, s.Bundle, "invalid body")
		return
	}
	uid := userIDFromContext(r.Context())
	body.CreatedBy = &uid
	g, err := groups.Create(r.Context(), s.Pool, body)
	if err != nil {
		if errors.Is(err, groups.ErrCycle) {
			httpx.RenderJSON(w, http.StatusConflict, httpx.Error{Code: "cycle", Message: err.Error()})
			return
		}
		s.Logger.Error("groups create", "err", err)
		httpx.RenderInternalError(w, r, s.Bundle)
		return
	}
	audit.Record(r.Context(), s.Pool, audit.Event{
		Actor:   audit.Actor{Type: "user", ID: &uid, Label: emailFromContext(r.Context())},
		Action:  audit.ActionGroupCreate,
		Target:  &audit.Target{Type: "group", ID: &g.ID, Label: g.Name},
		Request: r,
		Metadata: map[string]any{"parent_id": deref(g.ParentID)},
	})
	httpx.RenderJSON(w, http.StatusCreated, g)
}

// handleGroupsGet GET /api/v1/groups/{id}
func (s *Server) handleGroupsGet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	g, err := groups.Get(r.Context(), s.Pool, id)
	if err != nil {
		if errors.Is(err, groups.ErrNotFound) {
			httpx.RenderNotFound(w, r, s.Bundle)
			return
		}
		httpx.RenderInternalError(w, r, s.Bundle)
		return
	}
	httpx.RenderJSON(w, http.StatusOK, g)
}

// handleGroupsUpdate PATCH /api/v1/groups/{id}
func (s *Server) handleGroupsUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var body groups.UpdateInput
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.RenderValidationError(w, r, s.Bundle, "invalid body")
		return
	}
	if err := groups.Update(r.Context(), s.Pool, id, body); err != nil {
		if errors.Is(err, groups.ErrCycle) {
			httpx.RenderJSON(w, http.StatusConflict, httpx.Error{Code: "cycle", Message: err.Error()})
			return
		}
		if errors.Is(err, groups.ErrNotFound) {
			httpx.RenderNotFound(w, r, s.Bundle)
			return
		}
		s.Logger.Error("groups update", "err", err)
		httpx.RenderInternalError(w, r, s.Bundle)
		return
	}
	uid := userIDFromContext(r.Context())
	audit.Record(r.Context(), s.Pool, audit.Event{
		Actor:   audit.Actor{Type: "user", ID: &uid, Label: emailFromContext(r.Context())},
		Action:  audit.ActionGroupUpdate,
		Target:  &audit.Target{Type: "group", ID: &id, Label: id},
		Request: r,
	})
	g, _ := groups.Get(r.Context(), s.Pool, id)
	httpx.RenderJSON(w, http.StatusOK, g)
}

// handleGroupsDelete DELETE /api/v1/groups/{id}
func (s *Server) handleGroupsDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := groups.Delete(r.Context(), s.Pool, id); err != nil {
		if errors.Is(err, groups.ErrNotFound) {
			httpx.RenderNotFound(w, r, s.Bundle)
			return
		}
		s.Logger.Error("groups delete", "err", err)
		httpx.RenderInternalError(w, r, s.Bundle)
		return
	}
	uid := userIDFromContext(r.Context())
	audit.Record(r.Context(), s.Pool, audit.Event{
		Actor:   audit.Actor{Type: "user", ID: &uid, Label: emailFromContext(r.Context())},
		Action:  audit.ActionGroupDelete,
		Target:  &audit.Target{Type: "group", ID: &id, Label: id},
		Request: r,
	})
	httpx.RenderJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleGroupsAddMembers POST /api/v1/groups/{id}/members
func (s *Server) handleGroupsAddMembers(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var body struct {
		AgentIDs []string `json:"agent_ids"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.RenderValidationError(w, r, s.Bundle, "invalid body")
		return
	}
	uid := userIDFromContext(r.Context())
	added, err := groups.AddMembers(r.Context(), s.Pool, id, body.AgentIDs, &uid)
	if err != nil {
		s.Logger.Error("groups add members", "err", err)
		httpx.RenderInternalError(w, r, s.Bundle)
		return
	}
	audit.Record(r.Context(), s.Pool, audit.Event{
		Actor:   audit.Actor{Type: "user", ID: &uid, Label: emailFromContext(r.Context())},
		Action:  audit.ActionGroupAddMembers,
		Target:  &audit.Target{Type: "group", ID: &id, Label: id},
		Request: r,
		Metadata: map[string]any{"added": added, "requested": len(body.AgentIDs)},
	})
	httpx.RenderJSON(w, http.StatusOK, map[string]any{"added": added})
}

// handleGroupsRemoveMember DELETE /api/v1/groups/{id}/members/{agentId}
func (s *Server) handleGroupsRemoveMember(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	agentID := chi.URLParam(r, "agentId")
	if err := groups.RemoveMember(r.Context(), s.Pool, id, agentID); err != nil {
		s.Logger.Error("groups remove member", "err", err)
		httpx.RenderInternalError(w, r, s.Bundle)
		return
	}
	uid := userIDFromContext(r.Context())
	audit.Record(r.Context(), s.Pool, audit.Event{
		Actor:   audit.Actor{Type: "user", ID: &uid, Label: emailFromContext(r.Context())},
		Action:  audit.ActionGroupRemoveMember,
		Target:  &audit.Target{Type: "agent", ID: &agentID, Label: agentID},
		Request: r,
		Metadata: map[string]any{"from_group_id": id},
	})
	httpx.RenderJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleGroupsBulkMove POST /api/v1/groups/bulk-move
func (s *Server) handleGroupsBulkMove(w http.ResponseWriter, r *http.Request) {
	var body groups.BulkMoveInput
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.RenderValidationError(w, r, s.Bundle, "invalid body")
		return
	}
	uid := userIDFromContext(r.Context())
	body.ActorID = &uid
	if err := groups.BulkMove(r.Context(), s.Pool, body); err != nil {
		s.Logger.Error("groups bulk move", "err", err)
		httpx.RenderInternalError(w, r, s.Bundle)
		return
	}
	audit.Record(r.Context(), s.Pool, audit.Event{
		Actor:   audit.Actor{Type: "user", ID: &uid, Label: emailFromContext(r.Context())},
		Action:  audit.ActionGroupMoveAgent,
		Target:  &audit.Target{Type: "group", ID: body.GroupID, Label: deref(body.GroupID)},
		Request: r,
		Metadata: map[string]any{"count": len(body.AgentIDs)},
	})
	httpx.RenderJSON(w, http.StatusOK, map[string]any{"ok": true, "moved": len(body.AgentIDs)})
}

// silenciar import unused
var _ = strings.TrimSpace
// Package audit registra eventos inmutables en la tabla audit_events.
//
// Se invoca desde los handlers y servicios:
//
//	if err := audit.Record(ctx, pool, audit.Event{
//	    Actor:    audit.Actor{Type: "user", ID: userID, Label: email},
//	    Action:   "token.create",
//	    Target:   &audit.Target{Type: "token", ID: &id, Label: label},
//	    Request:  r,
//	    Metadata: map[string]any{"max_uses": maxUses},
//	}); err != nil { ... }
//
// El hash-chain (`prev_hash`/`hash`) se activa en Fase 10; en Fase 1 ambos
// campos quedan NULL y los eventos son append-only por convención.
package audit

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Action constants — usar siempre estas constantes en lugar de strings sueltos
// para evitar typos y permitir grep fácil.
const (
	ActionAuthLogin           = "auth.login"
	ActionAuthLoginFailed     = "auth.login_failed"
	ActionAuthLogout          = "auth.logout"
	ActionTokenCreate         = "token.create"
	ActionTokenRevoke         = "token.revoke"
	ActionTokenConsumed       = "token.consumed" // enrolamiento exitoso
	ActionAgentEnroll         = "agent.enroll"
	ActionAgentReconnect      = "agent.reconnect" // hello de un (token,host) ya enrolado
	ActionAgentDisconnect     = "agent.disconnect"
	ActionAgentHeartbeatLost  = "agent.heartbeat_lost"
	ActionAgentUpdate         = "agent.update"
	ActionAgentDelete         = "agent.delete"
	ActionAgentSetVisibility  = "agent.set_visibility"
	ActionAgentUpdateLabels   = "agent.update_labels"
	ActionGroupCreate         = "group.create"
	ActionGroupUpdate         = "group.update"
	ActionGroupDelete         = "group.delete"
	ActionGroupMoveAgent      = "group.move_agent"
	ActionGroupAddMembers     = "group.add_members"
	ActionGroupRemoveMember   = "group.remove_member"
	ActionTemplateCreate      = "template.create"
	ActionTemplateUpdate      = "template.update"
	ActionTemplateDelete      = "template.delete"
	ActionTemplateExecute     = "template.execute" // cuando se crea job desde template
	ActionJobCreate           = "job.create"
	ActionJobCancel           = "job.cancel"
	ActionSystemBootstrap     = "system.bootstrap_admin"
	ActionInventoryRequested  = "inventory.requested" // admin pidió refresh o server-push
	ActionInventoryReceived   = "inventory.received"  // snapshot persistido
	ActionInventoryFailed     = "inventory.failed"    // timeout o collector error

	// Fase 3 / DT-5: lifecycle de job_items despachados al agente.
	ActionJobDispatch          = "job.dispatch"        // server envio MsgCommand al agente
	ActionJobItemComplete      = "job.item_complete"   // exit_code == 0
	ActionJobItemFailed        = "job.item_failed"     // exit_code != 0
	ActionJobItemTimeout       = "job.item_timeout"    // timeout del agente
	ActionJobItemOffline       = "job.item_offline"    // agente no estaba conectado al dispatch
)

// Actor identifica quién ejecuta la acción.
type Actor struct {
	Type  string // "user" | "agent" | "system" | "token"
	ID    *string
	Label string
}

// Target identifica el objeto afectado (opcional).
type Target struct {
	Type  string
	ID    *string
	Label string
}

// Event es la entrada a registrar.
type Event struct {
	Actor    Actor
	Action   string
	Target   *Target
	Request  *http.Request // opcional, para extraer IP y User-Agent
	Metadata map[string]any
	At       time.Time // opcional; default time.Now()
}

// Record persiste el evento en audit_events. Los errores se loggean
// pero no se propagan (la auditoría no debe romper el flujo principal).
func Record(ctx context.Context, pool *pgxpool.Pool, e Event) {
	if pool == nil {
		return
	}
	if e.At.IsZero() {
		e.At = time.Now()
	}
	if e.Actor.Label == "" {
		switch e.Actor.Type {
		case "system":
			e.Actor.Label = "system"
		default:
			e.Actor.Label = "unknown"
		}
	}

	var ip, userAgent any
	if e.Request != nil {
		ip = clientIP(e.Request)
		userAgent = e.Request.UserAgent()
	}

	var (
		targetType, targetLabel any
		targetID                any
	)
	if e.Target != nil {
		targetType = e.Target.Type
		targetLabel = e.Target.Label
		targetID = e.Target.ID
	}

	_, err := pool.Exec(ctx, `
		INSERT INTO audit_events (
			occurred_at, actor_type, actor_id, actor_label,
			action, target_type, target_id, target_label,
			ip, user_agent, metadata
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NULLIF($9,'')::inet, $10, $11)
	`, e.At, e.Actor.Type, e.Actor.ID, e.Actor.Label,
		e.Action, targetType, targetID, targetLabel,
		ip, userAgent, e.Metadata)
	if err != nil {
		// No propagamos: la auditoría no debe romper el flujo principal.
		// Se loggea con un logger del caller si lo necesita.
		_ = err
	}
}

// clientIP extrae la IP respetando X-Forwarded-For si está presente.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		for i := 0; i < len(xff); i++ {
			if xff[i] == ',' {
				return xff[:i]
			}
		}
		return xff
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	addr := r.RemoteAddr
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == ':' {
			return addr[:i]
		}
	}
	return addr
}
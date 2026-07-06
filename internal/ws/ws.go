// Package ws implementa el hub de conexiones WebSocket para los agentes.
// Maneja el handshake de enrolamiento, heartbeat, y registro en el catálogo
// en memoria. La ejecución real de comandos se agrega en Fase 3.
package ws

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/Naired01/SAI/internal/agents"
	"github.com/Naired01/SAI/internal/audit"
	"github.com/Naired01/SAI/internal/inventory"
	"github.com/Naired01/SAI/internal/tokens"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Protocol message types (ver PLAN.md §5).
const (
	MsgHello           = "hello"
	MsgWelcome         = "welcome"
	MsgHeartbeat       = "heartbeat"
	MsgHeartbeatAck    = "heartbeat_ack"
	MsgPing            = "ping"
	MsgPong            = "pong"
	MsgSetVisibility   = "set_visibility"
	MsgCommand         = "command"
	MsgCommandResult   = "command_result"
	MsgInventoryReq    = "inventory_request"
	MsgInventorySnap   = "inventory_snapshot"
	MsgError           = "error"
)

// HelloMsg es el primer mensaje del agente al servidor.
type HelloMsg struct {
	Type         string `json:"type"`
	Token        string `json:"token"`
	AgentVersion string `json:"agent_version"`
	OS           string `json:"os"`
	OSVersion    string `json:"os_version,omitempty"`
	Arch         string `json:"arch,omitempty"`
	Hostname     string `json:"hostname"`
	Labels       map[string]any `json:"labels,omitempty"`
}

// WelcomeMsg es la respuesta del servidor.
type WelcomeMsg struct {
	Type       string `json:"type"`
	AgentID    string `json:"agent_id"`
	SessionJWT string `json:"session_jwt"`
	ServerTime string `json:"server_time"`
}

// HeartbeatMsg enviado por el agente cada 30s.
type HeartbeatMsg struct {
	Type string    `json:"type"`
	TS   time.Time `json:"ts"`
}

// ErrorMsg mensaje de error genérico.
type ErrorMsg struct {
	Type    string `json:"type"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Conn representa una conexión activa de un agente.
type Conn struct {
	AgentID string
	Send    chan []byte
}

// Hub mantiene el conjunto de conexiones activas.
type Hub struct {
	mu    sync.RWMutex
	conns map[string]*Conn // agent_id -> conn
}

// NewHub crea un hub vacío.
func NewHub() *Hub {
	return &Hub{conns: make(map[string]*Conn)}
}

// Register registra una conexión; si ya existía una, la cierra.
func (h *Hub) Register(c *Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if old, ok := h.conns[c.AgentID]; ok {
		close(old.Send)
	}
	h.conns[c.AgentID] = c
}

// Unregister elimina una conexión.
func (h *Hub) Unregister(agentID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if c, ok := h.conns[agentID]; ok {
		close(c.Send)
		delete(h.conns, agentID)
	}
}

// SendTo envía un mensaje a un agente por su ID.
// Devuelve false si el agente no está conectado.
func (h *Hub) SendTo(agentID string, msg any) bool {
	b, err := json.Marshal(msg)
	if err != nil {
		return false
	}
	h.mu.RLock()
	c, ok := h.conns[agentID]
	h.mu.RUnlock()
	if !ok {
		return false
	}
	select {
	case c.Send <- b:
		return true
	default:
		// buffer lleno
		return false
	}
}

// ConnectedAgents devuelve los IDs de agentes conectados (snapshot).
func (h *Hub) ConnectedAgents() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]string, 0, len(h.conns))
	for id := range h.conns {
		out = append(out, id)
	}
	return out
}

// IsConnected devuelve si un agente está conectado.
func (h *Hub) IsConnected(agentID string) bool {
	h.mu.RLock()
	_, ok := h.conns[agentID]
	h.mu.RUnlock()
	return ok
}

// -----------------------------------------------------------------------------
// HTTP handler
// -----------------------------------------------------------------------------

// HandlerOptions opciones para crear el handler HTTP del hub.
type HandlerOptions struct {
	Pool   *pgxpool.Pool
	Hub    *Hub
	Secret string
	Logger *slog.Logger
}

// Handler devuelve un http.Handler que upgradea a WS.
func Handler(opts HandlerOptions) http.Handler {
	upgrader := websocket.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
		CheckOrigin:     func(r *http.Request) bool { return true }, // el TLS/proxy valida
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			opts.Logger.Error("ws upgrade failed", "err", err)
			return
		}
		go serveAgent(r.Context(), opts, conn)
	})
}

func serveAgent(ctx context.Context, opts HandlerOptions, conn *websocket.Conn) {
	defer conn.Close()

	// 1) Esperar hello
	conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		opts.Logger.Warn("ws read hello failed", "err", err)
		return
	}
	var hello HelloMsg
	if err := json.Unmarshal(raw, &hello); err != nil || hello.Type != MsgHello {
		sendError(conn, "bad_hello", "first message must be hello")
		return
	}

	// 2) Canjear token
	cr, err := tokens.Redeem(ctx, opts.Pool, hello.Token)
	if err != nil {
		opts.Logger.Info("token redeem failed", "err", err)
		sendError(conn, "invalid_token", err.Error())
		return
	}

	// 3) Reusar agente existente si (token, host) ya se enroló antes;
	//    si no, crearlo. Antes de v1.2 esto siempre creaba una fila nueva
	//    por cada reconexión, lo que llenaba `agents` de duplicados.
	enrID := cr.TokenID
	agent, secret, reconnect, err := findOrCreateAgent(ctx, opts, enrID, hello)
	if err != nil {
		opts.Logger.Error("agent create/find failed", "err", err)
		sendError(conn, "internal", "could not register agent")
		return
	}

	// 4) Issue JWT de sesión para el agente. En Fase 1 el JWT se emite
	//    y devuelve al agent, pero el server todavía no lo valida en frames
	//    posteriores: el agent no lo persiste (cada reconexión reusa
	//    el enrollment token). La validación llega en Fase 3 con firma
	//    usando `secret` (unique per-agent → revocación granular).
	ttl := 1 * time.Hour
	jwt, _, err := issueAgentJWT(opts.Secret, agent.ID, secret, ttl)
	if err != nil {
		opts.Logger.Error("issue jwt failed", "err", err)
		sendError(conn, "internal", "could not issue session")
		return
	}

	welcome := WelcomeMsg{
		Type:       MsgWelcome,
		AgentID:    agent.ID,
		SessionJWT: jwt,
		ServerTime: time.Now().UTC().Format(time.RFC3339),
	}
	if err := conn.WriteJSON(welcome); err != nil {
		opts.Logger.Warn("ws write welcome failed", "err", err)
		return
	}

	// 5) Registrar en el hub
	send := make(chan []byte, 32)
	c := &Conn{AgentID: agent.ID, Send: send}
	opts.Hub.Register(c)
	defer opts.Hub.Unregister(agent.ID)

	// Auditoría
	action := audit.ActionAgentEnroll
	if reconnect {
		action = audit.ActionAgentReconnect
	}
	audit.Record(ctx, opts.Pool, audit.Event{
		Actor:  audit.Actor{Type: "agent", ID: &agent.ID, Label: agent.Hostname},
		Action: action,
		Target: &audit.Target{Type: "token", ID: &enrID, Label: "enrollment_token"},
		Request: nil, // sin http.Request en WS
		Metadata: map[string]any{
			"os":           agent.OS,
			"arch":         agent.Arch,
			"agent_version": agent.AgentVersion,
			"reconnect":    reconnect,
		},
	})
	_ = agents.RecordEvent(ctx, opts.Pool, agent.ID, "connect", map[string]any{"at": time.Now()})
	_ = agents.Touch(ctx, opts.Pool, agent.ID, time.Now())

	// 5b) Fase 2 — server-push de inventory_request si el último snapshot es
	// stale o no existe. Fire & forget; el agente responde con inventory_snapshot
	// que entra por readerLoop → handleInventorySnapshot. No bloqueamos el welcome.
	go maybeRequestInventory(ctx, opts, agent.ID)

	// 6) Loop principal
	conn.SetReadDeadline(time.Time{}) // sin timeout para heartbeats
	done := make(chan struct{})
	go writerLoop(conn, send, done)
	readerLoop(ctx, opts, conn, agent.ID, done)
	close(done)
}

func readerLoop(ctx context.Context, opts HandlerOptions, conn *websocket.Conn, agentID string, done chan struct{}) {
	conn.SetReadLimit(1 << 20) // 1 MiB
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			if !errors.Is(err, &websocket.CloseError{Code: websocket.CloseNormalClosure}) {
				opts.Logger.Debug("ws read closed", "agent", agentID, "err", err)
			}
			_ = agents.RecordEvent(context.Background(), opts.Pool, agentID, "disconnect", map[string]any{"at": time.Now()})
			audit.Record(context.Background(), opts.Pool, audit.Event{
				Actor:  audit.Actor{Type: "agent", ID: &agentID, Label: agentID},
				Action: audit.ActionAgentDisconnect,
				Metadata: map[string]any{"reason": err.Error()},
			})
			return
		}
		var msg struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &msg); err != nil {
			continue
		}
		switch msg.Type {
		case MsgHeartbeat:
			_ = agents.Touch(ctx, opts.Pool, agentID, time.Now())
			// ack opcional
			opts.Hub.SendTo(agentID, HeartbeatAckMsg(time.Now()))
		case MsgPong:
			// latency check; no-op por ahora
		case MsgError:
			_ = agents.RecordEvent(ctx, opts.Pool, agentID, "agent_error", map[string]any{"raw": string(raw)})
		case MsgCommandResult:
			// Llegará en Fase 3; por ahora solo loggeamos.
			opts.Logger.Debug("phase-3 message received", "type", msg.Type, "agent", agentID)
		case MsgInventorySnap:
			handleInventorySnapshot(ctx, opts, agentID, raw)
		}
	}
}

func writerLoop(conn *websocket.Conn, send <-chan []byte, done <-chan struct{}) {
	pingTicker := time.NewTicker(30 * time.Second)
	defer pingTicker.Stop()
	for {
		select {
		case msg, ok := <-send:
			if !ok {
				_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseGoingAway, ""))
				return
			}
			if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-pingTicker.C:
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case <-done:
			return
		}
	}
}

func sendError(conn *websocket.Conn, code, msg string) {
	b, _ := json.Marshal(ErrorMsg{Type: MsgError, Code: code, Message: msg})
	_ = conn.WriteMessage(websocket.TextMessage, b)
	_ = conn.WriteMessage(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.ClosePolicyViolation, code))
}

// -----------------------------------------------------------------------------
// Inventory helpers (Fase 2)
// -----------------------------------------------------------------------------

// maybeRequestInventory envía un inventory_request al agente si su último
// snapshot es stale o no existe. Fire & forget: si el agente ya no está
// conectado al momento de enviar, Hub.SendTo devuelve false y se descarta.
func maybeRequestInventory(ctx context.Context, opts HandlerOptions, agentID string) {
	stale, err := inventory.StaleOrMissing(ctx, opts.Pool, agentID, inventory.DefaultTTL)
	if err != nil {
		opts.Logger.Warn("inventory stale check failed", "agent", agentID, "err", err)
		return
	}
	if !stale {
		return
	}
	reqID := uuid.New().String()
	req := inventory.ReqMsg{Type: MsgInventoryReq, ID: reqID, Include: []string{"hardware"}}
	if !opts.Hub.SendTo(agentID, req) {
		opts.Logger.Debug("inventory_request dropped (agent not connected)", "agent", agentID)
		return
	}
	_ = inventory.RecordEvent(ctx, opts.Pool, agentID, "requested", uuid.MustParse(reqID), "", "")
	audit.Record(ctx, opts.Pool, audit.Event{
		Actor:  audit.Actor{Type: "system", Label: "server"},
		Action: audit.ActionInventoryRequested,
		Target: &audit.Target{Type: "agent", ID: &agentID, Label: agentID},
		Metadata: map[string]any{
			"reason":     "stale_or_missing",
			"request_id": reqID,
		},
	})
	opts.Logger.Info("inventory_request sent", "agent", agentID, "request_id", reqID)
}

// handleInventorySnapshot valida y persiste el snapshot recibido.
// Errores se loggean pero no se devuelven al agente (la conexión ya está
// abierta para heartbeats; no queremos cerrarla).
func handleInventorySnapshot(ctx context.Context, opts HandlerOptions, agentID string, raw []byte) {
	var msg inventory.SnapshotMsg
	if err := json.Unmarshal(raw, &msg); err != nil {
		opts.Logger.Warn("inventory_snapshot bad json", "agent", agentID, "err", err)
		_ = inventory.RecordEvent(ctx, opts.Pool, agentID, "failed", uuid.Nil, "", "bad_json")
		return
	}
	if err := msg.Validate(); err != nil {
		opts.Logger.Warn("inventory_snapshot invalid", "agent", agentID, "err", err)
		_ = inventory.RecordEvent(ctx, opts.Pool, agentID, "failed", uuid.Nil, msg.AgentVersion, err.Error())
		audit.Record(ctx, opts.Pool, audit.Event{
			Actor:   audit.Actor{Type: "agent", ID: &agentID, Label: agentID},
			Action:  audit.ActionInventoryFailed,
			Target:  &audit.Target{Type: "agent", ID: &agentID, Label: agentID},
			Metadata: map[string]any{"reason": err.Error(), "id": msg.ID},
		})
		return
	}
	snap := inventory.Snapshot{
		SchemaVer:    msg.SchemaVer,
		AgentVersion: msg.AgentVersion,
		Hardware:     *msg.Hardware,
		Software:     derefSoftware(msg.Software),
		Error:        msg.Error,
		CollectedAt:  msg.CollectedAt,
	}
	if snap.CollectedAt.IsZero() {
		snap.CollectedAt = time.Now().UTC()
	}
	if err := inventory.UpsertLatest(ctx, opts.Pool, agentID, snap); err != nil {
		opts.Logger.Error("inventory upsert failed", "agent", agentID, "err", err)
		_ = inventory.RecordEvent(ctx, opts.Pool, agentID, "failed", uuid.Nil, snap.AgentVersion, err.Error())
		audit.Record(ctx, opts.Pool, audit.Event{
			Actor:   audit.Actor{Type: "agent", ID: &agentID, Label: agentID},
			Action:  audit.ActionInventoryFailed,
			Target:  &audit.Target{Type: "agent", ID: &agentID, Label: agentID},
			Metadata: map[string]any{"reason": err.Error(), "id": msg.ID},
		})
		return
	}
	opts.Logger.Info("inventory stored",
		"agent", agentID,
		"request_id", msg.ID,
		"agent_version", msg.AgentVersion,
		"hw_host", msg.Hardware.Host.Hostname,
	)
	audit.Record(ctx, opts.Pool, audit.Event{
		Actor:   audit.Actor{Type: "agent", ID: &agentID, Label: agentID},
		Action:  audit.ActionInventoryReceived,
		Target:  &audit.Target{Type: "agent", ID: &agentID, Label: agentID},
		Metadata: map[string]any{
			"request_id": msg.ID,
			"agent_version": msg.AgentVersion,
			"has_error": msg.Error != "",
		},
	})
}

func derefSoftware(s *inventory.Software) inventory.Software {
	if s == nil {
		return inventory.Software{}
	}
	return *s
}


// findOrCreateAgent busca el agente existente para (enrollment_id, hostname)
// y, si no lo encuentra, lo crea. Devuelve reconnect=true cuando se reutilizó
// (segundo o enésimo hello del mismo host con el mismo token).
func findOrCreateAgent(ctx context.Context, opts HandlerOptions, enrID string, hello HelloMsg) (*agents.Agent, string, bool, error) {
	return findOrCreateAgentWith(ctx, &agentsRepoFromPool{pool: opts.Pool}, enrID, hello)
}

// agentFinder / agentCreator son las dependencias mínimas que
// findOrCreateAgentWith necesita. Se extraen a interfaces para poder testear
// la decisión pura (lookup → reuse | create) sin tocar la DB: en los tests
// pasamos fakes que devuelven lo que queremos.
type agentFinder interface {
	FindByEnrollmentAndHost(ctx context.Context, enrID, hostname string) (*agents.Agent, string, error)
}
type agentCreator interface {
	CreateAgent(ctx context.Context, enrID, hostname, osName, osVersion, arch, agentVersion string, labels map[string]any) (*agents.Agent, string, error)
}

type repoPool interface {
	agentFinder
	agentCreator
}

// agentsRepoFromPool adapta *pgxpool.Pool a las interfaces agentFinder /
// agentCreator delegando a las funciones del paquete agents. En producción
// findOrCreateAgent lo construye; en los tests inyectamos un fake que
// implementa repoPool.
type agentsRepoFromPool struct {
	pool *pgxpool.Pool
}

func (r *agentsRepoFromPool) FindByEnrollmentAndHost(ctx context.Context, enrID, hostname string) (*agents.Agent, string, error) {
	return agents.FindByEnrollmentAndHost(ctx, r.pool, enrID, hostname)
}
func (r *agentsRepoFromPool) CreateAgent(ctx context.Context, enrID, hostname, osName, osVersion, arch, agentVersion string, labels map[string]any) (*agents.Agent, string, error) {
	return agents.Create(ctx, r.pool, enrID, hostname, osName, osVersion, arch, agentVersion, labels)
}

// findOrCreateAgentWith es la implementación testeable: separa lookup y
// create para poder inyectar fakes. Devuelve (agent, secret, reconnect, err).
// Reglas:
//   - lookup OK (sin error) → reuse fila + secret, reconnect=true
//   - lookup ErrNotFound    → create, reconnect=false
//   - lookup otro error     → propagar
//   - create error          → propagar
func findOrCreateAgentWith(ctx context.Context, repo repoPool, enrID string, hello HelloMsg) (*agents.Agent, string, bool, error) {
	existing, secret, err := repo.FindByEnrollmentAndHost(ctx, enrID, hello.Hostname)
	if err == nil {
		return existing, secret, true, nil
	}
	if !errors.Is(err, agents.ErrNotFound) {
		return nil, "", false, err
	}
	a, secret, err := repo.CreateAgent(ctx, enrID,
		hello.Hostname, hello.OS, hello.OSVersion, hello.Arch, hello.AgentVersion, hello.Labels)
	if err != nil {
		return nil, "", false, err
	}
	return a, secret, false, nil
}

func issueAgentJWT(secret, agentID, agentSecret string, ttl time.Duration) (string, time.Time, error) {
	// Por ahora usamos solo el secreto general; en Fase 3 firmamos con
	// agentSecret por-agente para revocación granular.
	return agents.IssueDevJWT(secret, agentID, ttl)
}

// HeartbeatAckMsg construye el mensaje de ack.
func HeartbeatAckMsg(ts time.Time) any {
	return struct {
		Type string    `json:"type"`
		TS   time.Time `json:"ts"`
	}{Type: MsgHeartbeatAck, TS: ts}
}
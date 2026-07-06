// Package agents administra el catálogo de agentes: registro al enrolarse,
// última conexión, eventos, y cambio de visibilidad.
package agents

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Visibility modes.
const (
	VisibilityVisible   = "visible"
	VisibilityInvisible = "invisible"
)

// OnlineThreshold es la ventana sin heartbeats a partir de la cual un agente se
// considera offline. Single source of truth para `Agent.Online`, los filtros
// de `List(status=online|offline)` y los KPIs del dashboard. Si se cambia,
// revisar también los tests de `agents_test.go` y `dashboard_test.go`.
const OnlineThreshold = 2 * time.Minute

// Agent representa un agente enrolado.
type Agent struct {
	ID              string         `json:"id"`
	Hostname        string         `json:"hostname"`
	OS              string         `json:"os"`
	OSVersion       string         `json:"os_version,omitempty"`
	Arch            string         `json:"arch,omitempty"`
	AgentVersion    string         `json:"agent_version,omitempty"`
	EnrollmentID    *string        `json:"enrollment_id,omitempty"`
	Labels          map[string]any `json:"labels"`
	Visibility      string         `json:"visibility"`
	LastSeenAt      *time.Time     `json:"last_seen_at,omitempty"`
	FirstSeenAt     time.Time      `json:"first_seen_at"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	GroupIDs        []string       `json:"group_ids"`
	Online          bool           `json:"online"`
	LastInventoryAt *time.Time     `json:"last_inventory_at,omitempty"`
}

// Create registra un agente nuevo a partir de un handshake de enrolamiento
// y devuelve el agente + el secreto JWT por-agente.
func Create(ctx context.Context, pool *pgxpool.Pool, enrollmentID, hostname, osName, osVersion, arch, agentVersion string, labels map[string]any) (*Agent, string, error) {
	if labels == nil {
		labels = map[string]any{}
	}
	labelsJSON, _ := json.Marshal(labels)

	var (
		enrID *string
	)
	if enrollmentID != "" {
		enrID = &enrollmentID
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	row := tx.QueryRow(ctx, `
		INSERT INTO agents (hostname, os, os_version, arch, agent_version, enrollment_id, labels)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)
		RETURNING id, hostname, os, COALESCE(os_version,''), COALESCE(arch,''),
		          COALESCE(agent_version,''), enrollment_id, labels, visibility,
		          last_seen_at, first_seen_at, created_at, updated_at
	`, hostname, osName, osVersion, arch, agentVersion, enrID, string(labelsJSON))

	a := &Agent{}
	if err := row.Scan(&a.ID, &a.Hostname, &a.OS, &a.OSVersion, &a.Arch,
		&a.AgentVersion, &a.EnrollmentID, &a.Labels, &a.Visibility,
		&a.LastSeenAt, &a.FirstSeenAt, &a.CreatedAt, &a.UpdatedAt); err != nil {
		return nil, "", fmt.Errorf("insert agent: %w", err)
	}
	a.Online = a.LastSeenAt != nil && time.Since(*a.LastSeenAt) < OnlineThreshold

	// Generar secreto JWT único por agente
	secret, err := newSecret()
	if err != nil {
		return nil, "", err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO agent_credentials (agent_id, jwt_secret) VALUES ($1, $2)
	`, a.ID, secret); err != nil {
		return nil, "", fmt.Errorf("insert credentials: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, "", err
	}
	return a, secret, nil
}

// Get devuelve un agente por ID, incluyendo sus grupos.
func Get(ctx context.Context, pool *pgxpool.Pool, id string) (*Agent, error) {
	row := pool.QueryRow(ctx, agentSelect+` WHERE a.id = $1`, id)
	return scanAgent(row)
}

// FindByEnrollmentAndHost devuelve el agente existente enrolado con el mismo
// (enrollment_id, hostname) si lo hay, junto con su secreto JWT. Devuelve
// (nil, "", ErrNotFound) si no existe — el caller debe entonces llamar a
// Create. Se usa en el handshake WS para idempotenciar reconexiones: cada
// (token, host) -> una sola fila en `agents` + un solo `agent_credentials`.
func FindByEnrollmentAndHost(ctx context.Context, pool *pgxpool.Pool, enrollmentID, hostname string) (*Agent, string, error) {
	if enrollmentID == "" || hostname == "" {
		return nil, "", ErrNotFound
	}
	row := pool.QueryRow(ctx, agentSelect+`
		WHERE a.enrollment_id = $1 AND a.hostname = $2
		ORDER BY a.created_at DESC
		LIMIT 1
	`, enrollmentID, hostname)
	a, err := scanAgent(row)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, "", ErrNotFound
		}
		return nil, "", err
	}
	secret, err := GetSecret(ctx, pool, a.ID)
	if err != nil {
		return nil, "", err
	}
	return a, secret, nil
}

// ListOptions filtros para List.
type ListOptions struct {
	GroupID    string
	Ungrouped  bool
	Status     string // "online" | "offline" | ""
	Search     string
	Page       int
	PerPage    int
}

// List devuelve agentes paginados (1-based) según filtros.
func List(ctx context.Context, pool *pgxpool.Pool, opts ListOptions) ([]*Agent, int, error) {
	if opts.PerPage <= 0 || opts.PerPage > 200 {
		opts.PerPage = 25
	}
	if opts.Page <= 0 {
		opts.Page = 1
	}

	where := []string{"1=1"}
	args := []any{}
	idx := 1
	if opts.GroupID != "" {
		where = append(where, fmt.Sprintf("EXISTS (SELECT 1 FROM agent_group_members m WHERE m.agent_id = a.id AND m.group_id = $%d)", idx))
		args = append(args, opts.GroupID)
		idx++
	}
	if opts.Ungrouped {
		where = append(where, "NOT EXISTS (SELECT 1 FROM agent_group_members m WHERE m.agent_id = a.id)")
	}
	if opts.Status == "online" {
		where = append(where, fmt.Sprintf("a.last_seen_at IS NOT NULL AND a.last_seen_at > $%d", idx))
		args = append(args, time.Now().Add(-OnlineThreshold))
		idx++
	} else if opts.Status == "offline" {
		where = append(where, fmt.Sprintf("(a.last_seen_at IS NULL OR a.last_seen_at <= $%d)", idx))
		args = append(args, time.Now().Add(-OnlineThreshold))
		idx++
	}
	if s := strings.TrimSpace(opts.Search); s != "" {
		where = append(where, fmt.Sprintf("(a.hostname ILIKE $%d OR a.os ILIKE $%d OR a.os_version ILIKE $%d)", idx, idx, idx))
		args = append(args, "%"+s+"%")
		idx++
	}

	whereSQL := strings.Join(where, " AND ")

	// total
	var total int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM agents a WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	limitIdx := idx
	offsetIdx := idx + 1
	args = append(args, opts.PerPage, (opts.Page-1)*opts.PerPage)

	query := fmt.Sprintf(agentSelect+`
		WHERE %s
		ORDER BY COALESCE(a.last_seen_at, a.created_at) DESC
		LIMIT $%d OFFSET $%d
	`, whereSQL, limitIdx, offsetIdx)

	rows, err := pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []*Agent
	for rows.Next() {
		a, err := scanAgent(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, a)
	}
	return out, total, rows.Err()
}

// UpdateLabels / UpdateVisibility: cambios desde el panel.
type Update struct {
	Visibility *string                  `json:"visibility,omitempty"`
	Labels     *map[string]any          `json:"labels,omitempty"`
	GroupIDs   *[]string                `json:"group_ids,omitempty"` // nil = no cambiar, [] = vaciar
}

// Update aplica cambios parciales al agente y, opcionalmente, a sus grupos.
func UpdateAgent(ctx context.Context, pool *pgxpool.Pool, id string, u Update, actorID *string) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if u.Visibility != nil {
		if *u.Visibility != VisibilityVisible && *u.Visibility != VisibilityInvisible {
			return fmt.Errorf("invalid visibility: %s", *u.Visibility)
		}
		if _, err := tx.Exec(ctx, `UPDATE agents SET visibility = $1, updated_at = now() WHERE id = $2`,
			*u.Visibility, id); err != nil {
			return err
		}
	}
	if u.Labels != nil {
		b, _ := json.Marshal(*u.Labels)
		if _, err := tx.Exec(ctx, `UPDATE agents SET labels = $1::jsonb, updated_at = now() WHERE id = $2`,
			string(b), id); err != nil {
			return err
		}
	}
	if u.GroupIDs != nil {
		// Sincronizar membresía: borra todas y reinserta las nuevas.
		if _, err := tx.Exec(ctx, `DELETE FROM agent_group_members WHERE agent_id = $1`, id); err != nil {
			return err
		}
		for _, gid := range *u.GroupIDs {
			if _, err := tx.Exec(ctx, `
				INSERT INTO agent_group_members (agent_id, group_id, added_by)
				VALUES ($1, $2, $3)
				ON CONFLICT DO NOTHING
			`, id, gid, actorID); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE agents SET updated_at = now() WHERE id = $1`, id); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// Touch actualiza last_seen_at al timestamp dado.
func Touch(ctx context.Context, pool *pgxpool.Pool, id string, when time.Time) error {
	_, err := pool.Exec(ctx, `UPDATE agents SET last_seen_at = $1 WHERE id = $2`, when, id)
	return err
}

// RecordEvent graba un agent_event.
func RecordEvent(ctx context.Context, pool *pgxpool.Pool, agentID, eventType string, payload map[string]any) error {
	if payload == nil {
		payload = map[string]any{}
	}
	b, _ := json.Marshal(payload)
	_, err := pool.Exec(ctx, `
		INSERT INTO agent_events (agent_id, type, payload) VALUES ($1, $2, $3::jsonb)
	`, agentID, eventType, string(b))
	return err
}

// ListEvents devuelve los últimos N eventos de un agente.
type Event struct {
	ID        int64                  `json:"id"`
	Type      string                 `json:"type"`
	Payload   map[string]any         `json:"payload"`
	CreatedAt time.Time              `json:"created_at"`
}
func ListEvents(ctx context.Context, pool *pgxpool.Pool, agentID string, limit int) ([]*Event, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := pool.Query(ctx, `
		SELECT id, type, payload, created_at
		FROM agent_events WHERE agent_id = $1 ORDER BY created_at DESC LIMIT $2
	`, agentID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Event
	for rows.Next() {
		var (
			e   Event
			pay []byte
		)
		if err := rows.Scan(&e.ID, &e.Type, &pay, &e.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(pay, &e.Payload)
		out = append(out, &e)
	}
	return out, rows.Err()
}

// GetSecret devuelve el secreto JWT del agente.
func GetSecret(ctx context.Context, pool *pgxpool.Pool, agentID string) (string, error) {
	var s string
	err := pool.QueryRow(ctx, `SELECT jwt_secret FROM agent_credentials WHERE agent_id = $1`, agentID).Scan(&s)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return s, err
}

// IssueDevJWT genera un JWT de sesión para el agente usando el secreto
// general del servidor. En Fase 3 se firmará con el secreto único del
// agente (agent_credentials.jwt_secret) para revocación granular.
func IssueDevJWT(serverSecret, agentID string, ttl time.Duration) (string, time.Time, error) {
	now := time.Now()
	exp := now.Add(ttl)
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iss":  "sai",
		"sub":  agentID,
		"iat":  now.Unix(),
		"exp":  exp.Unix(),
		"kind": "agent",
	})
	signed, err := tok.SignedString([]byte(serverSecret))
	if err != nil {
		return "", time.Time{}, err
	}
	return signed, exp, nil
}

// -----------------------------------------------------------------------------
// Internals
// -----------------------------------------------------------------------------

const agentSelect = `
	SELECT a.id, a.hostname, a.os, COALESCE(a.os_version,''), COALESCE(a.arch,''),
	       COALESCE(a.agent_version,''), a.enrollment_id, a.labels, a.visibility,
	       a.last_seen_at, a.first_seen_at, a.created_at, a.updated_at,
	       COALESCE((SELECT array_agg(group_id) FROM agent_group_members WHERE agent_id = a.id), '{}'),
	       (SELECT MAX(received_at) FROM agent_inventory WHERE agent_id = a.id)
	FROM agents a
`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanAgent(r rowScanner) (*Agent, error) {
	var (
		a            Agent
		groupIDs     []string
		labelsRaw    []byte
		enrollmentID *string
		lastSeen     *time.Time
		lastInv      *time.Time
	)
	if err := r.Scan(&a.ID, &a.Hostname, &a.OS, &a.OSVersion, &a.Arch,
		&a.AgentVersion, &enrollmentID, &labelsRaw, &a.Visibility,
		&lastSeen, &a.FirstSeenAt, &a.CreatedAt, &a.UpdatedAt, &groupIDs,
		&lastInv); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	a.EnrollmentID = enrollmentID
	a.LastSeenAt = lastSeen
	a.LastInventoryAt = lastInv
	a.Online = lastSeen != nil && time.Since(*lastSeen) < OnlineThreshold
	a.GroupIDs = groupIDs
	if len(labelsRaw) > 0 {
		_ = json.Unmarshal(labelsRaw, &a.Labels)
	}
	if a.Labels == nil {
		a.Labels = map[string]any{}
	}
	return &a, nil
}

func newSecret() (string, error) {
	b := make([]byte, 48)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Errores públicos.
var ErrNotFound = errors.New("agent not found")
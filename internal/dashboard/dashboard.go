// Package dashboard agrupa los datos para el dashboard principal del panel:
// KPIs, agentes con problemas, acciones rápidas y trabajos recientes.
package dashboard

import (
	"context"
	"time"

	"github.com/Naired01/SAI/internal/agents"
	"github.com/Naired01/SAI/internal/jobs"
	"github.com/Naired01/SAI/internal/templates"
	"github.com/Naired01/SAI/internal/tokens"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Summary es la respuesta de GET /api/v1/dashboard/summary.
type Summary struct {
	KPIs          KPIs           `json:"kpis"`
	ProblemAgents []*agents.Agent `json:"problem_agents"`
	QuickActions  []*templates.Template `json:"quick_actions"`
	RecentJobs    []*jobs.Job    `json:"recent_jobs"`
}

// KPIs tarjetas de la parte superior.
type KPIs struct {
	AgentsOnline  int `json:"agents_online"`
	AgentsOffline int `json:"agents_offline"`
	AgentsProblem int `json:"agents_problem"`
	ActiveTokens  int `json:"active_tokens"`
	RunningJobs   int `json:"running_jobs"`
}

// ProblemThreshold define cuándo un agente es considerado "con problemas".
const ProblemThreshold = 5 * time.Minute

// Build compone el summary ejecutando las consultas en paralelo (secuencial
// por simplicidad — son pocas y rápidas).
func Build(ctx context.Context, pool *pgxpool.Pool) (*Summary, error) {
	s := &Summary{}

	// KPIs
	if err := pool.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE last_seen_at IS NOT NULL AND last_seen_at > now() - INTERVAL '2 minutes') AS online,
			COUNT(*) FILTER (WHERE last_seen_at IS NULL OR last_seen_at <= now() - INTERVAL '2 minutes') AS offline,
			COUNT(*) FILTER (WHERE last_seen_at IS NOT NULL AND last_seen_at <= now() - INTERVAL '2 minutes' AND last_seen_at > now() - INTERVAL '30 days') AS problem
		FROM agents
	`).Scan(&s.KPIs.AgentsOnline, &s.KPIs.AgentsOffline, &s.KPIs.AgentsProblem); err != nil {
		return nil, err
	}
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM enrollment_tokens
		WHERE revoked_at IS NULL
		  AND (expires_at IS NULL OR expires_at > now())
		  AND uses < max_uses
	`).Scan(&s.KPIs.ActiveTokens); err != nil {
		return nil, err
	}
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM jobs WHERE status IN ('pending','dispatching','running')
	`).Scan(&s.KPIs.RunningJobs); err != nil {
		return nil, err
	}

	// Problem agents: agentes que antes estaban online y ahora no,
	// o que tienen un error reciente en agent_events.
	problem, _, err := agents.List(ctx, pool, agents.ListOptions{
		Status:  "offline",
		PerPage: 50,
		Page:    1,
	})
	if err != nil {
		return nil, err
	}
	// Filtrar para mostrar solo los "problemáticos" (los que alguna vez se vieron)
	filtered := problem[:0]
	for _, a := range problem {
		if a.LastSeenAt != nil {
			filtered = append(filtered, a)
		}
	}
	if len(filtered) > 10 {
		filtered = filtered[:10]
	}
	s.ProblemAgents = filtered

	// Quick actions: plantillas con show_in_dashboard = true
	allTemplates, err := templates.List(ctx, pool, templates.ListOptions{})
	if err != nil {
		return nil, err
	}
	for _, t := range allTemplates {
		if t.ShowInDashboard {
			s.QuickActions = append(s.QuickActions, t)
		}
	}

	// Recent jobs (últimos 10)
	recent, _, err := jobs.List(ctx, pool, jobs.ListOptions{Page: 1, PerPage: 10})
	if err != nil {
		return nil, err
	}
	s.RecentJobs = recent

	// Touch tokens para que aparezcan en /tokens aunque no se listen.
	_ = tokens.List

	return s, nil
}
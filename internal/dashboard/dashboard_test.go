package dashboard

import (
	"testing"
	"time"

	"github.com/Naired01/SAI/internal/agents"
)

// TestComputeCutoffsUsesAgentsOnlineThreshold garantiza que el dashboard
// comparte la misma ventana que `agents.Online`. Si se cambia
// `agents.OnlineThreshold`, esto falla y obliga a revisar las queries.
func TestComputeCutoffsUsesAgentsOnlineThreshold(t *testing.T) {
	now := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	c := ComputeCutoffs(now)

	wantOnline := now.Add(-agents.OnlineThreshold)
	if !c.OnlineCutoff.Equal(wantOnline) {
		t.Fatalf("OnlineCutoff=%v want %v (delta=%v)", c.OnlineCutoff, wantOnline, c.OnlineCutoff.Sub(wantOnline))
	}

	wantProblem := now.Add(-ProblemLookback)
	if !c.ProblemCutoff.Equal(wantProblem) {
		t.Fatalf("ProblemCutoff=%v want %v (delta=%v)", c.ProblemCutoff, wantProblem, c.ProblemCutoff.Sub(wantProblem))
	}

	// Invariante: problemCutoff está estrictamente antes que onlineCutoff
	// (un agente que se vio hace 30 días NO es "online", pero tampoco
	// entra como "problem" en el KPI).
	if !c.ProblemCutoff.Before(c.OnlineCutoff) {
		t.Fatalf("problemCutoff (%v) debe ser < onlineCutoff (%v)", c.ProblemCutoff, c.OnlineCutoff)
	}
}

// TestComputeCutoffsDeltaMatchesProblemLookback verifica el ancho de la
// ventana "problem": la distancia entre los dos cutoffs debe ser exactamente
// ProblemLookback - agents.OnlineThreshold. Esto bloquea cambios accidentales
// que rompan la semántica de "agente con problemas" en el panel.
func TestComputeCutoffsDeltaMatchesProblemLookback(t *testing.T) {
	now := time.Now()
	c := ComputeCutoffs(now)

	gap := c.OnlineCutoff.Sub(c.ProblemCutoff)
	wantGap := ProblemLookback - agents.OnlineThreshold
	if gap != wantGap {
		t.Fatalf("gap entre cutoffs = %v, want %v", gap, wantGap)
	}
}

// TestProblemLookbackIsWiderThanOnlineThreshold es una invariante de
// negocio: los agentes con problemas (vistos hace 2m–30d) ocupan una ventana
// más amplia que online (vistos hace <2m). Si alguien la rompe, el KPI
// "problem_agents" queda vacío o se superpone con offline.
func TestProblemLookbackIsWiderThanOnlineThreshold(t *testing.T) {
	if ProblemLookback <= agents.OnlineThreshold {
		t.Fatalf("ProblemLookback (%v) debe ser > agents.OnlineThreshold (%v)",
			ProblemLookback, agents.OnlineThreshold)
	}
}

// TestProblemLookbackDefaultsToThirtyDays blinda el default. Si alguien lo
// cambia "para siempre", este test obliga a revisar release notes.
func TestProblemLookbackDefaultsToThirtyDays(t *testing.T) {
	want := 30 * 24 * time.Hour
	if ProblemLookback != want {
		t.Fatalf("ProblemLookback=%v want %v (revisar release notes antes de cambiar)",
			ProblemLookback, want)
	}
}
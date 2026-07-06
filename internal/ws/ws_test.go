package ws

import (
	"context"
	"errors"
	"testing"

	"github.com/Naired01/SAI/internal/agents"
)

// fakeRepo implementa repoPool para testear findOrCreateAgentWith sin DB.
// findFn se invoca primero; si devuelve ErrNotFound se llama a createFn.
// createCalls cuenta cuántas veces se invocó createFn.
type fakeRepo struct {
	findFn      func(enrID, hostname string) (*agents.Agent, string, error)
	createFn    func(enrID, hostname, osName, osVersion, arch, agentVersion string, labels map[string]any) (*agents.Agent, string, error)
	findCalls   int
	createCalls int
}

func (f *fakeRepo) FindByEnrollmentAndHost(_ context.Context, enrID, hostname string) (*agents.Agent, string, error) {
	f.findCalls++
	return f.findFn(enrID, hostname)
}

func (f *fakeRepo) CreateAgent(_ context.Context, enrID, hostname, osName, osVersion, arch, agentVersion string, labels map[string]any) (*agents.Agent, string, error) {
	f.createCalls++
	return f.createFn(enrID, hostname, osName, osVersion, arch, agentVersion, labels)
}

// TestFindOrCreateAgent_ReuseExisting cubre el camino feliz de reconexión:
// el agente ya existe para (enrID, host) → se reutiliza, reconnect=true,
// create no se llama, el secret devuelto coincide.
func TestFindOrCreateAgent_ReuseExisting(t *testing.T) {
	enrID := "tok-123"
	host := "LAPTOP-01"
	wantAgent := &agents.Agent{ID: "agent-existing", Hostname: host}
	wantSecret := "secret-from-db"

	repo := &fakeRepo{
		findFn: func(e, h string) (*agents.Agent, string, error) {
			if e != enrID || h != host {
				t.Fatalf("lookup args wrong: enr=%s host=%s", e, h)
			}
			return wantAgent, wantSecret, nil
		},
		createFn: func(e, h, os, osv, arch, av string, _ map[string]any) (*agents.Agent, string, error) {
			t.Fatal("create no debe llamarse cuando el lookup ya encontró el agente")
			return nil, "", nil
		},
	}

	gotAgent, gotSecret, reconnect, err := findOrCreateAgentWith(context.Background(), repo, enrID, HelloMsg{
		Hostname: host,
		OS:       "windows",
		Arch:     "amd64",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reconnect {
		t.Fatal("reconnect debe ser true cuando se reusa el agente")
	}
	if gotAgent != wantAgent {
		t.Fatalf("agent: got %v want %v", gotAgent, wantAgent)
	}
	if gotSecret != wantSecret {
		t.Fatalf("secret: got %q want %q", gotSecret, wantSecret)
	}
	if repo.createCalls != 0 {
		t.Fatalf("createCalls=%d, want 0", repo.createCalls)
	}
	if repo.findCalls != 1 {
		t.Fatalf("findCalls=%d, want 1", repo.findCalls)
	}
}

// TestFindOrCreateAgent_FirstEnrollment cubre el caso del primer hello:
// lookup devuelve ErrNotFound → se llama a create, reconnect=false.
func TestFindOrCreateAgent_FirstEnrollment(t *testing.T) {
	enrID := "tok-new"
	host := "NEW-HOST"
	newAgent := &agents.Agent{ID: "agent-new", Hostname: host}
	newSecret := "fresh-secret"

	repo := &fakeRepo{
		findFn: func(_, _ string) (*agents.Agent, string, error) {
			return nil, "", agents.ErrNotFound
		},
		createFn: func(e, h, osName, osVersion, arch, agentVersion string, labels map[string]any) (*agents.Agent, string, error) {
			if e != enrID || h != host {
				t.Fatalf("create args wrong: enr=%s host=%s", e, h)
			}
			if osName != "linux" || arch != "arm64" {
				t.Fatalf("hello fields not forwarded: os=%s arch=%s", osName, arch)
			}
			return newAgent, newSecret, nil
		},
	}

	gotAgent, gotSecret, reconnect, err := findOrCreateAgentWith(context.Background(), repo, enrID, HelloMsg{
		Hostname:     host,
		OS:           "linux",
		OSVersion:    "6.1.0",
		Arch:         "arm64",
		AgentVersion: "0.2.0",
		Labels:       map[string]any{"site": "oficina"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reconnect {
		t.Fatal("reconnect debe ser false en el primer hello")
	}
	if gotAgent != newAgent {
		t.Fatalf("agent: got %v want %v", gotAgent, newAgent)
	}
	if gotSecret != newSecret {
		t.Fatalf("secret: got %q want %q", gotSecret, newSecret)
	}
	if repo.findCalls != 1 {
		t.Fatalf("findCalls=%d, want 1", repo.findCalls)
	}
	if repo.createCalls != 1 {
		t.Fatalf("createCalls=%d, want 1", repo.createCalls)
	}
}

// TestFindOrCreateAgent_LookupErrorPropagates verifica que un error de DB
// (no ErrNotFound) en el lookup NO cae al create y se propaga tal cual.
func TestFindOrCreateAgent_LookupErrorPropagates(t *testing.T) {
	dbErr := errors.New("connection refused")
	repo := &fakeRepo{
		findFn: func(_, _ string) (*agents.Agent, string, error) {
			return nil, "", dbErr
		},
		createFn: func(_, _, _, _, _, _ string, _ map[string]any) (*agents.Agent, string, error) {
			t.Fatal("create no debe llamarse cuando el lookup falló con un error no-NotFound")
			return nil, "", nil
		},
	}

	_, _, _, err := findOrCreateAgentWith(context.Background(), repo, "tok-x", HelloMsg{Hostname: "h"})
	if err == nil || !errors.Is(err, dbErr) {
		t.Fatalf("expected dbErr propagated, got %v", err)
	}
	if repo.createCalls != 0 {
		t.Fatalf("createCalls=%d, want 0", repo.createCalls)
	}
}

// TestFindOrCreateAgent_CreateErrorPropagates verifica que si el lookup
// devuelve ErrNotFound pero el create falla, el error del create se
// propaga (no se hace reconnect=true silencioso).
func TestFindOrCreateAgent_CreateErrorPropagates(t *testing.T) {
	createErr := errors.New("insert failed: duplicate key")
	repo := &fakeRepo{
		findFn: func(_, _ string) (*agents.Agent, string, error) {
			return nil, "", agents.ErrNotFound
		},
		createFn: func(_, _, _, _, _, _ string, _ map[string]any) (*agents.Agent, string, error) {
			return nil, "", createErr
		},
	}

	_, _, reconnect, err := findOrCreateAgentWith(context.Background(), repo, "tok-y", HelloMsg{Hostname: "h"})
	if err == nil || !errors.Is(err, createErr) {
		t.Fatalf("expected createErr propagated, got %v", err)
	}
	if reconnect {
		t.Fatal("reconnect debe ser false si la creación falló")
	}
}
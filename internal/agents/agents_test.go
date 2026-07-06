package agents

import (
	"strings"
	"testing"
	"time"
)

func TestOnlineThresholdValue(t *testing.T) {
	// Single source of truth para "qué significa online". Si esto cambia,
	// ajustar la doc y los tests de dashboard que asumen este valor.
	if OnlineThreshold != 2*time.Minute {
		t.Fatalf("OnlineThreshold=%v, want 2m", OnlineThreshold)
	}
}

func TestAgentOnlineFieldSemantics(t *testing.T) {
	// Test puro de la lógica online/offline que vive en scanAgent.
	// No podemos invocarlo sin un row real, pero podemos verificar la
	// invariante: now - lastSeen < OnlineThreshold => online.
	now := time.Now()
	cases := []struct {
		name     string
		lastSeen *time.Time
		want     bool
	}{
		{"never seen", nil, false},
		{"seen 1m ago", ptrTime(now.Add(-1 * time.Minute)), true},
		{"seen 5m ago", ptrTime(now.Add(-5 * time.Minute)), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := c.lastSeen != nil && now.Sub(*c.lastSeen) < OnlineThreshold
			if got != c.want {
				t.Fatalf("online=%v, want %v", got, c.want)
			}
		})
	}
}

func TestNewSecretFormatAndUniqueness(t *testing.T) {
	s1, err := newSecret()
	if err != nil {
		t.Fatalf("newSecret: %v", err)
	}
	s2, err := newSecret()
	if err != nil {
		t.Fatalf("newSecret: %v", err)
	}
	if s1 == s2 {
		t.Fatal("two secrets must differ")
	}
	// 48 bytes → 64 chars base64-url (raw, sin padding).
	if len(s1) != 64 {
		t.Fatalf("len=%d, want 64", len(s1))
	}
	if strings.ContainsAny(s1, "+/=") {
		t.Fatalf("secret should be raw URL-safe base64, got %q", s1)
	}
}

func ptrTime(t time.Time) *time.Time { return &t }

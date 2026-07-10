package agents

import (
	"strings"
	"testing"
	"time"

	"github.com/Naired01/SAI/internal/auth"
	jwt "github.com/golang-jwt/jwt/v5"
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

// TestIssueAgentJWTRoundtrip verifica que un JWT firmado con un secret
// se valida correctamente con ese mismo secret y se rechaza con uno
// distinto. Cubre la propiedad central de la firma per-agente.
func TestIssueAgentJWTRoundtrip(t *testing.T) {
	const (
		agentID = "agent-123"
		secretA = "secret-for-agent-a-32chars-min"
		secretB = "secret-for-agent-b-different"
		ttl     = time.Hour
	)
	tok, exp, err := auth.IssueAgentJWT(secretA, agentID, ttl)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if tok == "" {
		t.Fatal("token is empty")
	}
	if time.Until(exp) <= 0 || time.Until(exp) > ttl+time.Second {
		t.Fatalf("exp=%v not within ttl", exp)
	}

	claims, err := auth.ParseAgentJWT(secretA, tok)
	if err != nil {
		t.Fatalf("parse with same secret: %v", err)
	}
	if claims.AgentID != agentID {
		t.Fatalf("AgentID=%q, want %q", claims.AgentID, agentID)
	}
	if claims.Kind != "agent" {
		t.Fatalf("Kind=%q, want agent", claims.Kind)
	}

	// Mismo token, secret diferente -> rechazo.
	if _, err := auth.ParseAgentJWT(secretB, tok); err == nil {
		t.Fatal("parse with wrong secret should fail")
	}
}

// TestParseAgentJWTRejectsTamperedToken verifica que modificar cualquier
// byte del token (header.payload.signature) rompe la validación.
func TestParseAgentJWTRejectsTamperedToken(t *testing.T) {
	const secret = "test-secret-32-chars-minimum-aaaa"
	tok, _, err := auth.IssueAgentJWT(secret, "agent-1", time.Hour)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	// Flip el último caracter de la firma.
	tampered := tok[:len(tok)-1]
	if tampered[len(tampered)-1] == 'A' {
		tampered = tampered[:len(tampered)-1] + "B"
	} else {
		tampered = tampered[:len(tampered)-1] + "A"
	}
	if _, err := auth.ParseAgentJWT(secret, tampered); err == nil {
		t.Fatal("tampered token should fail")
	}
}

// TestParseAgentJWTRejectsWrongKind verifica que un JWT firmado con el
// secret correcto pero con kind distinto de "agent" es rechazado.
// Evita confusion attacks donde se le pasa un JWT admin al handshake del agente.
func TestParseAgentJWTRejectsWrongKind(t *testing.T) {
	const secret = "test-secret-32-chars-minimum-bbbb"
	tok, _, err := issueAdminStyleJWT(secret, time.Hour)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, err := auth.ParseAgentJWT(secret, tok); err == nil {
		t.Fatal("admin JWT should be rejected as agent JWT")
	}
}

// issueAdminStyleJWT firma un JWT con claims de admin (uid/eml/rol/csrf)
// pero Kind = "user" en lugar de "agent". Se usa sólo en tests para
// verificar que ParseAgentJWT no confunde tipos.
func issueAdminStyleJWT(secret string, ttl time.Duration) (string, time.Time, error) {
	now := time.Now()
	exp := now.Add(ttl)
	claims := jwt.MapClaims{
		"iss":  "sai",
		"iat":  now.Unix(),
		"exp":  exp.Unix(),
		"uid":  "u-1",
		"eml":  "a@b.c",
		"rol":  "admin",
		"csrf": "x",
		"kind": "user",
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString([]byte(secret))
	if err != nil {
		return "", time.Time{}, err
	}
	return signed, exp, nil
}

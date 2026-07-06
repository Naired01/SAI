package auth

import (
	"strings"
	"testing"
	"time"
)

func TestHashPasswordVerifyRoundtrip(t *testing.T) {
	const pw = "correct horse battery staple"
	h, err := HashPassword(pw)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$") {
		t.Fatalf("hash missing argon2id header: %q", h)
	}
	if err := VerifyPassword(pw, h); err != nil {
		t.Fatalf("VerifyPassword(same): %v", err)
	}
	if err := VerifyPassword(pw+"x", h); err == nil {
		t.Fatalf("VerifyPassword(wrong) should fail")
	}
}

func TestVerifyPasswordRejectsBadFormat(t *testing.T) {
	cases := []string{
		"",
		"not-a-hash",
		"$argon2id$v=19$m=65536,t=3,p=2$",            // faltan partes
		"$bcrypt$v=19$m=65536,t=3,p=2$abcd$efgh",     // algoritmo equivocado
		"$argon2id$v=XX$m=65536,t=3,p=2$abcd$efgh",   // version inválida
		"$argon2id$v=19$m=abc,t=3,p=2$abcd$efgh",     // params inválidos
	}
	for _, h := range cases {
		if err := VerifyPassword("x", h); err == nil {
			t.Fatalf("VerifyPassword(%q) should fail", h)
		}
	}
}

func TestHashPasswordIsUniquePerCall(t *testing.T) {
	const pw = "same-password-everywhere"
	a, err := HashPassword(pw)
	if err != nil {
		t.Fatal(err)
	}
	b, err := HashPassword(pw)
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatalf("two hashes of same password should differ (salts are random): got equal")
	}
	// Ambas deben verificar el mismo password.
	if err := VerifyPassword(pw, a); err != nil {
		t.Fatalf("verify a: %v", err)
	}
	if err := VerifyPassword(pw, b); err != nil {
		t.Fatalf("verify b: %v", err)
	}
}

func TestIssueJWTParseJWTRoundtrip(t *testing.T) {
	const (
		secret = "test-secret-must-be-long-enough-for-hs256"
		uid    = "11111111-2222-3333-4444-555555555555"
		email  = "admin@sai.local"
		role   = "admin"
		csrf   = "csrf-token-xyz"
	)
	tok, exp, err := IssueJWT(secret, uid, email, role, csrf, time.Hour)
	if err != nil {
		t.Fatalf("IssueJWT: %v", err)
	}
	if tok == "" {
		t.Fatal("empty token")
	}
	if time.Until(exp) < 30*time.Minute {
		t.Fatalf("expected ~1h TTL, got %v", time.Until(exp))
	}
	c, err := ParseJWT(secret, tok)
	if err != nil {
		t.Fatalf("ParseJWT: %v", err)
	}
	if c.UserID != uid || c.Email != email || c.Role != role || c.CSRF != csrf {
		t.Fatalf("claims mismatch: %+v", c)
	}
}

func TestParseJWTRejectsTamperedSignature(t *testing.T) {
	const secret = "test-secret-must-be-long-enough-for-hs256"
	tok, _, err := IssueJWT(secret, "uid", "admin@sai.local", "admin", "csrf", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	// Voltear el último carácter del signature para tampering.
	tampered := tok[:len(tok)-1] + flipChar(string(tok[len(tok)-1]))
	if _, err := ParseJWT(secret, tampered); err == nil {
		t.Fatal("ParseJWT should fail on tampered signature")
	}
}

func TestParseJWTRejectsWrongSecret(t *testing.T) {
	tok, _, err := IssueJWT("secret-a-secret-a-secret-a-32+", "uid", "admin@sai.local", "admin", "csrf", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseJWT("secret-b-secret-b-secret-b-32+", tok); err == nil {
		t.Fatal("ParseJWT should reject token signed with different secret")
	}
}

func TestParseJWTRejectsExpired(t *testing.T) {
	const secret = "test-secret-must-be-long-enough-for-hs256"
	// TTL de 1ms en el pasado para forzar expiración.
	tok, _, err := IssueJWT(secret, "uid", "admin@sai.local", "admin", "csrf", -time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseJWT(secret, tok); err == nil {
		t.Fatal("ParseJWT should reject expired token")
	}
}

func TestNewTokenLengthAndUniqueness(t *testing.T) {
	a := newToken(32)
	b := newToken(32)
	if a == b {
		t.Fatalf("newToken should produce unique values: got equal %q", a)
	}
	// 32 bytes → 43 chars base64-url (sin padding).
	if len(a) < 40 || len(a) > 50 {
		t.Fatalf("newToken(32) length out of expected range: got %d chars", len(a))
	}
}

func flipChar(s string) string {
	if s == "A" {
		return "B"
	}
	return "A"
}

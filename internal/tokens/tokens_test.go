package tokens

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

func TestGeneratePlainLengthAndCharset(t *testing.T) {
	tok, err := generatePlain()
	if err != nil {
		t.Fatalf("generatePlain: %v", err)
	}
	// 32 bytes → exactamente 43 chars base64-url (sin padding).
	if len(tok) != 43 {
		t.Fatalf("len=%d, want 43", len(tok))
	}
	if strings.ContainsAny(tok, "+/=") {
		t.Fatalf("token should be URL-safe base64 (raw, no padding), got %q", tok)
	}
	if _, err := base64.RawURLEncoding.DecodeString(tok); err != nil {
		t.Fatalf("token is not valid raw-url base64: %v", err)
	}
}

func TestGeneratePlainIsUnique(t *testing.T) {
	a, _ := generatePlain()
	b, _ := generatePlain()
	if a == b {
		t.Fatalf("two generates should differ: both %q", a)
	}
}

func TestHashPlainIsSHA256Deterministic(t *testing.T) {
	const plain = "abc123"
	h1 := hashToken(plain)
	h2 := hashToken(plain)
	if h1 != h2 {
		t.Fatalf("hash should be deterministic: %s vs %s", h1, h2)
	}
	want := sha256.Sum256([]byte(plain))
	if h1 != hex.EncodeToString(want[:]) {
		t.Fatalf("hash is not SHA-256(plain) hex")
	}
}

func TestHashPlainDistinctInputsDistinctHashes(t *testing.T) {
	a := hashToken("token-a-aaaaaaaaaaaaaaaa")
	b := hashToken("token-b-bbbbbbbbbbbbbbbb")
	if a == b {
		t.Fatal("distinct inputs must produce distinct hashes")
	}
}

func TestHashPlainExposedEqualsInternal(t *testing.T) {
	// HashPlain es la API pública usada por el endpoint /agents/download;
	// debe coincidir exactamente con hashToken() (la interna) para que
	// la validación de tokens no rompa entre módulos.
	const plain = "any-plain-token"
	if got, want := HashPlain(plain), hashToken(plain); got != want {
		t.Fatalf("HashPlain(%q)=%s, want %s", plain, got, want)
	}
}

func TestTokenHasUses(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name   string
		tok    Token
		wantOK bool
	}{
		{"fresh with uses left", Token{MaxUses: 5, Uses: 0}, true},
		{"exhausted at limit", Token{MaxUses: 1, Uses: 1}, false},
		{"revoked", Token{MaxUses: 5, Uses: 0, RevokedAt: ptrTime(now.Add(-time.Minute))}, false},
		{"expired in past", Token{MaxUses: 5, Uses: 0, ExpiresAt: ptrTime(now.Add(-time.Minute))}, false},
		{"not yet expired", Token{MaxUses: 5, Uses: 0, ExpiresAt: ptrTime(now.Add(time.Hour))}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.tok.HasUses(); got != c.wantOK {
				t.Fatalf("HasUses()=%v, want %v", got, c.wantOK)
			}
		})
	}
}

func ptrTime(t time.Time) *time.Time { return &t }

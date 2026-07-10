package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestClientIP garantiza que el server registra correctamente la IP del
// cliente en la columna INET de `sessions.ip`. El bug original escaneaba
// ':' de derecha a izquierda, lo que devolvía "[::1]" (con brackets) para
// conexiones IPv6 — Postgres rechaza eso como INET y rompía el login.
func TestClientIP(t *testing.T) {
	cases := []struct {
		name       string
		xff        string
		xri        string
		remoteAddr string
		want       string
	}{
		{"ipv4 con puerto", "", "", "192.0.2.10:54321", "192.0.2.10"},
		{"ipv6 loopback con puerto", "", "", "[::1]:54321", "::1"},
		{"ipv6 full con puerto", "", "", "[2001:db8::1]:443", "2001:db8::1"},
		{"xff primer hop", "203.0.113.5, 10.0.0.1, 10.0.0.2", "", "10.0.0.99:54321", "203.0.113.5"},
		{"xff solo", "198.51.100.7", "", "10.0.0.99:54321", "198.51.100.7"},
		{"xff con espacios", "  198.51.100.7  , 10.0.0.1", "", "10.0.0.99:54321", "198.51.100.7"},
		{"xri gana sobre remoteaddr", "", "203.0.113.42", "[::1]:54321", "203.0.113.42"},
		{"xff gana sobre xri", "203.0.113.5", "203.0.113.42", "[::1]:54321", "203.0.113.5"},
		{"sin puerto (test socket)", "", "", "192.0.2.10", "192.0.2.10"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
			r.RemoteAddr = c.remoteAddr
			if c.xff != "" {
				r.Header.Set("X-Forwarded-For", c.xff)
			}
			if c.xri != "" {
				r.Header.Set("X-Real-IP", c.xri)
			}
			if got := clientIP(r); got != c.want {
				t.Fatalf("clientIP=%q, want %q", got, c.want)
			}
		})
	}
}
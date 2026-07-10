package api

import (
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/Naired01/SAI/internal/audit"
	"github.com/Naired01/SAI/internal/auth"
	"github.com/Naired01/SAI/internal/httpx"
)

// loginRateLimiter limita intentos de login por IP.
var loginRateLimiter = newLoginLimiter(10, time.Minute)

// handleLogin POST /api/v1/auth/login
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	ip := r.RemoteAddr
	if !loginRateLimiter.allow(ip) {
		httpx.RenderJSON(w, http.StatusTooManyRequests, httpx.Error{
			Code:    "rate_limited",
			Message: "too many login attempts",
		})
		return
	}

	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.RenderValidationError(w, r, s.Bundle, "invalid body")
		return
	}

	var (
		userID        string
		email         string
		role          string
		passwordHash  string
	)
	err := s.Pool.QueryRow(r.Context(), `
		SELECT id, email, role, password_hash FROM users
		WHERE email = $1 AND is_active = TRUE
	`, body.Email).Scan(&userID, &email, &role, &passwordHash)
	if err != nil {
		audit.Record(r.Context(), s.Pool, audit.Event{
			Actor:  audit.Actor{Type: "user", Label: body.Email},
			Action: audit.ActionAuthLoginFailed,
			Metadata: map[string]any{"reason": "user_not_found_or_inactive"},
		})
		respondError(w, r, s, http.StatusUnauthorized, "auth.login.invalid_credentials")
		return
	}
	if err := auth.VerifyPassword(body.Password, passwordHash); err != nil {
		audit.Record(r.Context(), s.Pool, audit.Event{
			Actor:  audit.Actor{Type: "user", ID: &userID, Label: email},
			Action: audit.ActionAuthLoginFailed,
			Metadata: map[string]any{"reason": "bad_password"},
		})
		respondError(w, r, s, http.StatusUnauthorized, "auth.login.invalid_credentials")
		return
	}

	// Crear sesión
	ttl := 8 * time.Hour
	sessionID, csrf, expires, err := auth.CreateSession(r.Context(), s.Pool, userID, r.UserAgent(), clientIP(r), ttl)
	if err != nil {
		s.Logger.Error("create session failed", "err", err)
		httpx.RenderInternalError(w, r, s.Bundle)
		return
	}
	setSessionCookie(w, sessionID, s.PublicURL != "" && s.PublicURL[:5] == "https")
	_, _ = s.Pool.Exec(r.Context(), `UPDATE users SET last_login_at = now() WHERE id = $1`, userID)

	audit.Record(r.Context(), s.Pool, audit.Event{
		Actor:   audit.Actor{Type: "user", ID: &userID, Label: email},
		Action:  audit.ActionAuthLogin,
		Request: r,
	})

	httpx.RenderJSON(w, http.StatusOK, map[string]any{
		"user": map[string]any{
			"id":    userID,
			"email": email,
			"role":  role,
		},
		"csrf":      csrf,
		"expires_at": expires,
	})
}

// handleLogout POST /api/v1/auth/logout
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	sid, _ := r.Context().Value(ctxSessionID).(string)
	if sid != "" {
		_ = auth.DeleteSession(r.Context(), s.Pool, sid)
	}
	clearSessionCookie(w, s.PublicURL != "" && s.PublicURL[:5] == "https")
	uid := userIDFromContext(r.Context())
	email := emailFromContext(r.Context())
	audit.Record(r.Context(), s.Pool, audit.Event{
		Actor:   audit.Actor{Type: "user", ID: &uid, Label: email},
		Action:  audit.ActionAuthLogout,
		Request: r,
	})
	httpx.RenderJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleMe GET /api/v1/auth/me
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	httpx.RenderJSON(w, http.StatusOK, map[string]any{
		"id":    userIDFromContext(r.Context()),
		"email": emailFromContext(r.Context()),
		"role":  roleFromContext(r.Context()),
	})
}

// handleCSRF GET /api/v1/auth/csrf
func (s *Server) handleCSRF(w http.ResponseWriter, r *http.Request) {
	httpx.RenderJSON(w, http.StatusOK, map[string]any{
		"csrf": csrfFromContext(r.Context()),
	})
}

func clientIP(r *http.Request) string {
	// X-Forwarded-For / X-Real-IP sólo se respetan cuando el server corre
	// detrás de un reverse proxy. En desarrollo / conexión directa caen
	// al RemoteAddr.
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Tomamos el primer hop (cliente original).
		if i := strings.IndexByte(xff, ','); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	if xri := strings.TrimSpace(r.Header.Get("X-Real-IP")); xri != "" {
		return xri
	}
	// net.SplitHostPort maneja correctamente IPv4 ("1.2.3.4:5678"),
	// IPv6 con brackets ("[::1]:5678" → "::1") y domain sockets. Antes
	// escaneábamos ':' de derecha a izquierda, lo que para IPv6 con
	// brackets devolvía "[::1]" — Postgres rechaza "[::1]" como tipo INET.
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	// Sin puerto (caso de algunos test o unix sockets): RemoteAddr entero.
	return r.RemoteAddr
}

// helper
func mustGet[T any](v T, err error) T {
	if err != nil && !errors.Is(err, errors.New("not found")) {
		// intentionally empty
	}
	return v
}
// Package api monta el router HTTP (chi) con todos los middlewares y handlers
// del backend. Sirve también el panel admin (SPA estática) bajo "/".
package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Naired01/SAI/internal/audit"
	"github.com/Naired01/SAI/internal/auth"
	"github.com/Naired01/SAI/internal/httpx"
	"github.com/Naired01/SAI/internal/i18n"
	"github.com/Naired01/SAI/internal/version"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Server agrupa las dependencias para los handlers.
type Server struct {
	Pool           *pgxpool.Pool
	BundleDir      string
	PublicURL      string
	WebDist        string
	Bundle         *i18n.Bundle
	Hub            WSHubs
	JWTSecret      string
	AgentJWTSecret string
	Logger         *slog.Logger
	StartTime      time.Time
}

// WSHubs define lo que el handler /api/v1/agents/download necesita del hub
// (placeholder mientras creamos la dependencia real).
type WSHubs interface {
	IsConnected(agentID string) bool
}

// NewRouter construye el chi.Mux completo.
func NewRouter(s *Server) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))

	// i18n: leer Accept-Language y exponer en el contexto
	r.Use(i18nMiddleware(s.Bundle))

	// logger
	r.Use(loggerMiddleware(s.Logger))

	// static + health
	r.Get("/api/v1/health", s.handleHealth)
	r.Get("/api/v1/version", s.handleVersion)
	r.Get("/api/v1/agents/download", s.handleAgentDownload)

	// auth
	r.Route("/api/v1/auth", func(r chi.Router) {
		r.Post("/login", s.handleLogin)
		r.With(s.requireAuth()).Post("/logout", s.handleLogout)
		r.With(s.requireAuth()).Get("/me", s.handleMe)
		r.With(s.requireAuth()).Get("/csrf", s.handleCSRF)
	})

	// resources (todos requieren admin)
	r.Route("/api/v1", func(r chi.Router) {
		r.Use(s.requireAuth())

		r.Route("/tokens", func(r chi.Router) {
			r.Get("/", s.handleTokensList)
			r.Post("/", s.handleTokensCreate)
			r.Post("/{id}/revoke", s.handleTokensRevoke)
		})

		r.Route("/agents", func(r chi.Router) {
			r.Get("/", s.handleAgentsList)
			r.Get("/{id}", s.handleAgentsGet)
			r.Patch("/{id}", s.handleAgentsUpdate)
			r.Delete("/{id}", s.handleAgentsDelete)
			r.Get("/{id}/events", s.handleAgentsEvents)
		})

		r.Route("/groups", func(r chi.Router) {
			r.Get("/", s.handleGroupsTree)
			r.Post("/", s.handleGroupsCreate)
			r.Get("/{id}", s.handleGroupsGet)
			r.Patch("/{id}", s.handleGroupsUpdate)
			r.Delete("/{id}", s.handleGroupsDelete)
			r.Post("/{id}/members", s.handleGroupsAddMembers)
			r.Delete("/{id}/members/{agentId}", s.handleGroupsRemoveMember)
			r.Post("/bulk-move", s.handleGroupsBulkMove)
		})

		r.Route("/templates", func(r chi.Router) {
			r.Get("/", s.handleTemplatesList)
			r.Post("/", s.handleTemplatesCreate)
			r.Get("/{id}", s.handleTemplatesGet)
			r.Patch("/{id}", s.handleTemplatesUpdate)
			r.Delete("/{id}", s.handleTemplatesDelete)
			r.Post("/{id}/run", s.handleTemplatesRun)
		})

		r.Route("/jobs", func(r chi.Router) {
			r.Get("/", s.handleJobsList)
			r.Post("/", s.handleJobsCreate)
			r.Get("/{id}", s.handleJobsGet)
			r.Post("/{id}/cancel", s.handleJobsCancel)
			r.Get("/{id}/items", s.handleJobsItems)
			r.Get("/{id}/items/{itemId}", s.handleJobsItem)
			r.Get("/{id}/export.csv", s.handleJobsExportCSV)
		})

		r.Route("/audit", func(r chi.Router) {
			r.Get("/events", s.handleAuditList)
			r.Get("/events/{id}", s.handleAuditGet)
			r.Get("/actions", s.handleAuditActions)
			r.Get("/export.csv", s.handleAuditExportCSV)
		})

		r.Get("/dashboard/summary", s.handleDashboardSummary)
	})

	// WebSocket (no requiere admin; usa enrollment token)
	r.Get("/api/v1/agent/ws", s.handleAgentWS)

	// SPA: servir web/dist/ bajo "/"
	r.Handle("/*", spaHandler(s.WebDist))

	return r
}

// -----------------------------------------------------------------------------
// i18n middleware
// -----------------------------------------------------------------------------

func i18nMiddleware(bundle *i18n.Bundle) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			lang := bundle.Lookup(detectLang(r.Header.Get("Accept-Language")), "_test")
			_ = lang
			ctx := i18n.WithLang(r.Context(), detectLang(r.Header.Get("Accept-Language")))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func detectLang(accept string) string {
	if accept == "" {
		return "es"
	}
	for _, part := range strings.Split(accept, ",") {
		tag := strings.TrimSpace(part)
		if i := strings.IndexByte(tag, ';'); i >= 0 {
			tag = tag[:i]
		}
		tag = strings.TrimSpace(tag)
		if tag == "es" || tag == "en" {
			return tag
		}
		if i := strings.IndexByte(tag, '-'); i > 0 {
			prefix := tag[:i]
			if prefix == "es" || prefix == "en" {
				return prefix
			}
		}
	}
	return "es"
}

// -----------------------------------------------------------------------------
// logger middleware
// -----------------------------------------------------------------------------

func loggerMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()
			next.ServeHTTP(ww, r)
			logger.Info("http",
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"size", ww.BytesWritten(),
				"dur_ms", time.Since(start).Milliseconds(),
				"req_id", middleware.GetReqID(r.Context()),
				"ip", r.RemoteAddr,
			)
		})
	}
}

// -----------------------------------------------------------------------------
// auth middleware
// -----------------------------------------------------------------------------

type ctxKey int

const (
	ctxUserID ctxKey = iota
	ctxEmail
	ctxRole
	ctxCSRF
	ctxSessionID
)

const cookieSession = "sai_session"

func setSessionCookie(w http.ResponseWriter, sessionID string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieSession,
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
		MaxAge:   int((8 * time.Hour).Seconds()),
	})
}

func clearSessionCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieSession,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
		MaxAge:   -1,
	})
}

func sessionFromCookie(r *http.Request) string {
	c, err := r.Cookie(cookieSession)
	if err != nil {
		return ""
	}
	return c.Value
}

// requireAuth devuelve un middleware que exige sesión admin válida.
// Se inyecta vía closure con el *Server para acceder al Pool y al Bundle i18n.
func (s *Server) requireAuth() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sid := sessionFromCookie(r)
			if sid == "" {
				httpx.RenderUnauthorized(w, r, s.Bundle)
				return
			}
			sess, err := auth.LookupSession(r.Context(), s.Pool, sid)
			if err != nil {
				clearSessionCookie(w, secureFromRequest(r))
				httpx.RenderUnauthorized(w, r, s.Bundle)
				return
			}
			// CSRF para métodos no-GET
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				hdr := r.Header.Get("X-CSRF-Token")
				if hdr == "" || hdr != sess.CSRFToken {
					httpx.RenderForbidden(w, r, s.Bundle)
					return
				}
			}
			// cargar user
			var (
				userID, email, role string
			)
			err = s.Pool.QueryRow(r.Context(),
				`SELECT id, email, role FROM users WHERE id = $1 AND is_active = TRUE`,
				sess.UserID).Scan(&userID, &email, &role)
			if err != nil {
				clearSessionCookie(w, secureFromRequest(r))
				httpx.RenderUnauthorized(w, r, s.Bundle)
				return
			}
			ctx := context.WithValue(r.Context(), ctxUserID, userID)
			ctx = context.WithValue(ctx, ctxEmail, email)
			ctx = context.WithValue(ctx, ctxRole, role)
			ctx = context.WithValue(ctx, ctxCSRF, sess.CSRFToken)
			ctx = context.WithValue(ctx, ctxSessionID, sid)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// secureFromRequest devuelve true si la request llegó por HTTPS.
// Se usa para marcar la cookie como Secure cuando corresponde.
func secureFromRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if r.Header.Get("X-Forwarded-Proto") == "https" {
		return true
	}
	return false
}

// -----------------------------------------------------------------------------
// rate limit (login) — token bucket muy simple por IP
// -----------------------------------------------------------------------------

type loginLimiter struct {
	mu       sync.Mutex
	buckets  map[string]*loginBucket
	limit    int
	interval time.Duration
}
type loginBucket struct {
	count   int
	resetAt time.Time
}

func newLoginLimiter(limit int, interval time.Duration) *loginLimiter {
	return &loginLimiter{buckets: map[string]*loginBucket{}, limit: limit, interval: interval}
}
func (l *loginLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	b, ok := l.buckets[ip]
	if !ok || now.After(b.resetAt) {
		l.buckets[ip] = &loginBucket{count: 1, resetAt: now.Add(l.interval)}
		return true
	}
	if b.count >= l.limit {
		return false
	}
	b.count++
	return true
}

// -----------------------------------------------------------------------------
// errors helper
// -----------------------------------------------------------------------------

var errUnauthorized = errors.New("unauthorized")

// userIDFromContext devuelve el user id del admin en sesión.
func userIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ctxUserID).(string); ok {
		return v
	}
	return ""
}
func roleFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ctxRole).(string); ok {
		return v
	}
	return ""
}
func emailFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ctxEmail).(string); ok {
		return v
	}
	return ""
}
func csrfFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ctxCSRF).(string); ok {
		return v
	}
	return ""
}

// respondError helper común
func respondError(w http.ResponseWriter, r *http.Request, srv *Server, status int, key string) {
	httpx.RenderError(w, r, srv.Bundle, status, key)
}

// auditRecordFromCtx convenience
func auditRecordFromCtx(ctx context.Context, srv *Server, action string, targetType string, targetID, targetLabel *string, metadata map[string]any) {
	uid := userIDFromContext(ctx)
	email := emailFromContext(ctx)
	audit.Record(ctx, srv.Pool, audit.Event{
		Actor:    audit.Actor{Type: "user", ID: &uid, Label: email},
		Action:   action,
		Target:   &audit.Target{Type: targetType, ID: targetID, Label: deref(targetLabel)},
		Request:  nil, // se pasa en handlers que tengan *http.Request
		Metadata: metadata,
	})
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// -----------------------------------------------------------------------------
// SPA
// -----------------------------------------------------------------------------

func spaHandler(dist string) http.Handler {
	fs := http.FileServer(http.Dir(dist))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// intentar servir archivo; si no existe, fallback a index.html (SPA routing)
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			p = "index.html"
		}
		// no permitir path traversal
		if strings.Contains(p, "..") {
			http.NotFound(w, r)
			return
		}
		// servir archivos existentes; si no, index.html
		if _, err := os.Stat(dist + "/" + p); err == nil {
			fs.ServeHTTP(w, r)
			return
		}
		// SPA fallback
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		fs.ServeHTTP(w, r2)
	})
}

// helper para incluir versión
func (s *Server) versionInfo() version.Info { return version.Get() }

// helper os
func isWindows(r *http.Request) bool {
	return strings.Contains(strings.ToLower(r.UserAgent()), "windows") ||
		strings.HasPrefix(r.Header.Get("Sec-Ch-Ua-Platform"), `"Windows`)
}
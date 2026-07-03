// Command server es el servidor central SAI: HTTP API + WebSocket hub +
// servicio de UI estática.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Naired01/SAI/internal/agents"
	"github.com/Naired01/SAI/internal/api"
	"github.com/Naired01/SAI/internal/auth"
	"github.com/Naired01/SAI/internal/config"
	"github.com/Naired01/SAI/internal/db"
	"github.com/Naired01/SAI/internal/i18n"
	"github.com/Naired01/SAI/internal/templates"
	"github.com/Naired01/SAI/internal/version"
	"github.com/Naired01/SAI/internal/ws"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	var (
		bootstrap       = flag.Bool("bootstrap", false, "Crear o resetear admin. Comportamiento idempotente: si la tabla users está vacía lo crea; si el email coincide con un admin existente resetea su password; si hay otro admin requiere --force-reset")
		forceReset      = flag.Bool("force-reset", false, "En --bootstrap, si existe un admin con email distinto al solicitado, lo reemplaza (¡peligroso!)")
		adminEmail      = flag.String("admin-email", "", "Email del admin a crear/resetear en --bootstrap")
		adminPassword   = flag.String("admin-password", "", "Password del admin a crear/resetear en --bootstrap")
		showVersion     = flag.Bool("version", false, "Muestra la versión y sale")
	)
	flag.Parse()

	if *showVersion {
		fmt.Println("sai-server", version.Version, "commit="+version.Commit, "built="+version.BuildTime)
		return
	}

	logger := newLogger(os.Getenv("SAI_LOG_LEVEL"))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		logger.Error("config load failed", "err", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	logger.Info("sai-server starting",
		"version", version.Version,
		"env", string(cfg.Env),
		"bind", cfg.Bind,
		"db", redactDSN(cfg.DBURL),
	)

	// DB pool
	pool, err := db.Open(ctx, cfg.DBURL)
	if err != nil {
		logger.Error("db open failed", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	// Migraciones
	if err := pool.Migrate(ctx); err != nil {
		logger.Error("migrations failed", "err", err)
		os.Exit(1)
	}
	logger.Info("migrations applied")

	// i18n bundle
	bundle, err := i18n.NewBundle(cfg.DefaultLang)
	if err != nil {
		logger.Error("i18n bundle", "err", err)
		os.Exit(1)
	}

	// Seed templates builtin (idempotente)
	if err := templates.SeedBuiltins(ctx, pool.Pool); err != nil {
		logger.Warn("seed templates failed (non-fatal)", "err", err)
	}

	// Bootstrap admin si se pidió
	if *bootstrap || cfg.BootstrapEmail != "" {
		email := *adminEmail
		if email == "" {
			email = cfg.BootstrapEmail
		}
		password := *adminPassword
		if password == "" {
			password = cfg.BootstrapPassword
		}
		if email == "" || password == "" {
			logger.Error("--bootstrap requires --admin-email and --admin-password (or env vars SAI_BOOTSTRAP_EMAIL/SAI_BOOTSTRAP_PASSWORD)")
			os.Exit(1)
		}
		if err := bootstrapAdmin(ctx, pool.Pool, email, password, *forceReset); err != nil {
			logger.Error("bootstrap admin failed", "err", err)
			os.Exit(1)
		}
		logger.Info("bootstrap admin ready", "email", email)
		return // --bootstrap es una acción única, no levanta el server
	}

	// Resolver bundle dir
	bundleDir := cfg.BundleDir
	if abs, err := filepath.Abs(bundleDir); err == nil {
		bundleDir = abs
	}
	if entries, err := os.ReadDir(bundleDir); err == nil {
		logger.Info("agent bundle dir", "path", bundleDir, "entries", len(entries))
	}

	// Resolver web dist
	webDist := cfg.WebDist
	if _, err := os.Stat(webDist); err != nil {
		logger.Warn("web dist not found (panel will not be served)", "path", webDist, "err", err)
	}

	// WS hub
	hub := ws.NewHub()

	// API server
	srv := &api.Server{
		Pool:           pool.Pool,
		BundleDir:      bundleDir,
		PublicURL:      cfg.PublicURL,
		WebDist:        webDist,
		Bundle:         bundle,
		Hub:            hub,
		JWTSecret:      cfg.JWTSecret,
		AgentJWTSecret: cfg.AgentJWTSecret,
		Logger:         logger,
		StartTime:      time.Now(),
	}
	router := api.NewRouter(srv)

	httpSrv := &http.Server{
		Addr:              cfg.Bind,
		Handler:           router,
		ReadHeaderTimeout: 15 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// Background: purga de sesiones expiradas cada hora
	go func() {
		t := time.NewTicker(1 * time.Hour)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if n, err := auth.PurgeExpiredSessions(context.Background(), pool.Pool); err == nil && n > 0 {
					logger.Info("purged expired sessions", "count", n)
				}
			}
		}
	}()

	// Listen
	errCh := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", cfg.Bind)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	case err := <-errCh:
		logger.Error("http server error", "err", err)
	}

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()
	_ = httpSrv.Shutdown(shutdownCtx)
	logger.Info("bye")
}

// -----------------------------------------------------------------------------
// helpers
// -----------------------------------------------------------------------------

func newLogger(level string) *slog.Logger {
	var lv slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lv = slog.LevelDebug
	case "warn":
		lv = slog.LevelWarn
	case "error":
		lv = slog.LevelError
	default:
		lv = slog.LevelInfo
	}
	h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lv})
	return slog.New(h)
}

func redactDSN(dsn string) string {
	if i := strings.Index(dsn, "://"); i >= 0 {
		if j := strings.Index(dsn[i+3:], "@"); j >= 0 {
			return dsn[:i+3] + "***" + dsn[i+3+j:]
		}
	}
	return dsn
}

func bootstrapAdmin(ctx context.Context, pool *pgxpool.Pool, email, password string, forceReset bool) error {
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}

	// Listar usuarios existentes para decidir el camino.
	rows, err := pool.Query(ctx, `SELECT email FROM users ORDER BY created_at`)
	if err != nil {
		return err
	}
	var existing []string
	for rows.Next() {
		var e string
		if err := rows.Scan(&e); err != nil {
			rows.Close()
			return err
		}
		existing = append(existing, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	switch len(existing) {
	case 0:
		// DB fresca → crear admin.
		if _, err := pool.Exec(ctx, `
			INSERT INTO users (email, password_hash, role, is_active)
			VALUES ($1, $2, 'admin', TRUE)
		`, email, hash); err != nil {
			return err
		}
		_, _ = pool.Exec(ctx, `
			INSERT INTO audit_events (actor_type, actor_label, action, metadata)
			VALUES ('system', 'system', 'system.bootstrap_admin', jsonb_build_object('email', $1::text, 'mode', 'create'))
		`, email)

	case 1:
		if existing[0] == email {
			// Mismo email → resetear password (olvido de contraseña).
			if _, err := pool.Exec(ctx, `
				UPDATE users SET password_hash = $1, updated_at = now(), is_active = TRUE
				WHERE email = $2
			`, hash, email); err != nil {
				return err
			}
			_, _ = pool.Exec(ctx, `
				INSERT INTO audit_events (actor_type, actor_label, action, metadata)
				VALUES ('system', 'system', 'system.bootstrap_admin', jsonb_build_object('email', $1::text, 'mode', 'reset_password'))
			`, email)
		} else {
			if !forceReset {
				return fmt.Errorf("users table already has admin %q; use --force-reset to replace it (this will DELETE the existing admin)", existing[0])
			}
			// Force: reemplazar el admin existente.
			tx, err := pool.Begin(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = tx.Rollback(ctx) }()
			if _, err := tx.Exec(ctx, `DELETE FROM users WHERE email = $1`, existing[0]); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO users (email, password_hash, role, is_active)
				VALUES ($1, $2, 'admin', TRUE)
			`, email, hash); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO audit_events (actor_type, actor_label, action, metadata)
				VALUES ('system', 'system', 'system.bootstrap_admin', jsonb_build_object('email', $1::text, 'mode', 'force_replace', 'replaced', $2::text))
			`, email, existing[0]); err != nil {
				return err
			}
			if err := tx.Commit(ctx); err != nil {
				return err
			}
		}

	default:
		return fmt.Errorf("users table has %d users; bootstrap only works on a fresh DB (1 user). Use SQL to manage existing users", len(existing))
	}
	return nil
}

// silenciar import unused
var _ = agents.VisibilityVisible
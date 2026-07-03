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
		bootstrap       = flag.Bool("bootstrap", false, "Crear admin inicial si la tabla users está vacía")
		adminEmail      = flag.String("admin-email", "", "Email del admin a crear en --bootstrap")
		adminPassword   = flag.String("admin-password", "", "Password del admin a crear en --bootstrap")
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
		if err := bootstrapAdmin(ctx, pool.Pool, email, password); err != nil {
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

func bootstrapAdmin(ctx context.Context, pool *pgxpool.Pool, email, password string) error {
	// ¿ya existe un admin?
	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("users table is not empty; refusing to bootstrap")
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO users (email, password_hash, role, is_active)
		VALUES ($1, $2, 'admin', TRUE)
	`, email, hash)
	if err != nil {
		return err
	}
	// Audit (como "system")
	_, _ = pool.Exec(ctx, `
		INSERT INTO audit_events (actor_type, actor_label, action, metadata)
		VALUES ('system', 'system', 'system.bootstrap_admin', jsonb_build_object('email', $1::text))
	`, email)
	return nil
}

// silenciar import unused
var _ = agents.VisibilityVisible
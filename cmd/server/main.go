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
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/Naired01/SAI/internal/api"
	"github.com/Naired01/SAI/internal/auth"
	"github.com/Naired01/SAI/internal/config"
	"github.com/Naired01/SAI/internal/db"
	"github.com/Naired01/SAI/internal/i18n"
	"github.com/Naired01/SAI/internal/inventory"
	"github.com/Naired01/SAI/internal/jobs"
	"github.com/Naired01/SAI/internal/templates"
	"github.com/Naired01/SAI/internal/version"
	"github.com/Naired01/SAI/internal/ws"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	// Captura panics que ocurran ANTES de tener logger configurado
	// y los escribe a stderr + un archivo crash.log para diagnóstico.
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "\n=== PANIC RECOVERED ===\n")
			fmt.Fprintf(os.Stderr, "panic: %v\n", r)
			fmt.Fprintf(os.Stderr, "stack:\n%s\n", debug.Stack())
			writeCrashLog(r)
			os.Exit(2)
		}
	}()

	var (
		bootstrap   = flag.Bool("bootstrap", false, "Crear o resetear admin. Comportamiento idempotente.")
		forceReset  = flag.Bool("force-reset", false, "En --bootstrap, reemplaza admin existente con email distinto.")
		adminEmail  = flag.String("admin-email", "", "Email del admin a crear/resetear.")
		adminPass   = flag.String("admin-password", "", "Password del admin a crear/resetear.")
		showVersion = flag.Bool("version", false, "Muestra la versión y sale.")
		healthcheck = flag.Bool("healthcheck", false, "Probe HTTP a /api/v1/health (Docker HEALTHCHECK).")
	)
	flag.Parse()

	if *showVersion {
		fmt.Println("sai-server", version.Version, "commit="+version.Commit, "built="+version.BuildTime)
		return
	}

	if *healthcheck {
		runHealthcheck()
		return
	}

	logger := newLogger(os.Getenv("SAI_LOG_LEVEL"))
	slog.SetDefault(logger)

	logger.Info("========================================")
	logger.Info("sai-server startup",
		"step", "0/init",
		"version", version.Version,
		"commit", version.Commit,
		"built", version.BuildTime,
		"go_version", version.GoVersion,
		"pid", os.Getpid(),
	)
	logger.Info("========================================")

	// STEP 1: Cargar configuración
	logger.Info("startup step", "step", "1/config", "msg", "loading configuration")
	cfg, err := config.Load()
	if err != nil {
		logger.Error("startup failed", "step", "1/config", "err", err)
		os.Exit(1)
	}
	logger.Info("startup ok", "step", "1/config",
		"env", string(cfg.Env),
		"bind", cfg.Bind,
		"log_level", cfg.LogLvl,
		"db", redactDSN(cfg.DBURL),
		"bundle_dir", cfg.BundleDir,
		"web_dist", cfg.WebDist,
		"default_lang", cfg.DefaultLang,
	)

	// Setup signal context
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// STEP 2: Abrir pool de DB
	logger.Info("startup step", "step", "2/db_open", "msg", "connecting to postgres")
	pool, err := db.Open(ctx, cfg.DBURL)
	if err != nil {
		logger.Error("startup failed", "step", "2/db_open", "err", err, "hint", "verifica SAI_DB_URL, que postgres esté arriba, y credenciales")
		os.Exit(1)
	}
	defer func() {
		logger.Info("shutdown", "step", "shutdown/db_close", "msg", "closing db pool")
		pool.Close()
	}()
	logger.Info("startup ok", "step", "2/db_open", "msg", "postgres pool ready")

	// STEP 3: Aplicar migraciones
	logger.Info("startup step", "step", "3/migrate", "msg", "applying migrations")
	if err := pool.Migrate(ctx); err != nil {
		logger.Error("startup failed", "step", "3/migrate", "err", err)
		os.Exit(1)
	}
	logger.Info("startup ok", "step", "3/migrate", "msg", "migrations applied")

	// STEP 4: Cargar i18n
	logger.Info("startup step", "step", "4/i18n", "msg", "loading i18n bundle", "default_lang", cfg.DefaultLang)
	bundle, err := i18n.NewBundle(cfg.DefaultLang)
	if err != nil {
		logger.Error("startup failed", "step", "4/i18n", "err", err)
		os.Exit(1)
	}
	logger.Info("startup ok", "step", "4/i18n", "languages", bundle.Languages())

	// STEP 5: Seed templates builtin
	logger.Info("startup step", "step", "5/seed_templates", "msg", "seeding builtin templates")
	if err := templates.SeedBuiltins(ctx, pool.Pool); err != nil {
		logger.Warn("startup warn", "step", "5/seed_templates", "err", err, "msg", "continuing without builtin templates")
	} else {
		logger.Info("startup ok", "step", "5/seed_templates", "msg", "builtin templates ensured")
	}

	// STEP 6: Bootstrap admin
	//
	// Hay DOS caminos completamente independientes:
	//
	// A) CLI explícito (`--bootstrap`): operator-driven, one-shot. Crea/resetea
	//    admin y SALE del proceso. No arranca el servidor. Usado desde
	//    `docker compose exec … --bootstrap` o `go run ./cmd/server --bootstrap …`.
	//
	// B) Auto-bootstrap silencioso por env vars (`SAI_BOOTSTRAP_EMAIL` +
	//    `SAI_BOOTSTRAP_PASSWORD`): se evalúa al arrancar el server como un
	//    paso normal de startup. Sólo crea el admin en una DB fresca
	//    (tabla `users` vacía). Si ya existe cualquier usuario, NO TOCA
	//    contraseñas. Tras el chequeo (con o sin creación) el flujo continúa
	//    al STEP 7 para arrancar el HTTP server.
	//
	// Esta separación evita el bug "loop de reinicios": antes, ambos caminos
	// compartían la misma rama y hacían `return` al final, exitando el proceso
	// al arrancar el container y haciendo que `restart: unless-stopped` lo
	// levantara otra vez — y otra — generando además un reset silencioso de la
	// contraseña del admin existente en cada reinicio.

	// Camino A: CLI bootstrap (one-shot, sale tras éxito)
	if *bootstrap {
		email := *adminEmail
		if email == "" {
			email = cfg.BootstrapEmail
		}
		password := *adminPass
		if password == "" {
			password = cfg.BootstrapPassword
		}
		if email == "" || password == "" {
			logger.Error("startup failed", "step", "6/bootstrap", "mode", "cli",
				"err", "missing credentials",
				"hint", "pasa --admin-email y --admin-password (o env SAI_BOOTSTRAP_EMAIL/SAI_BOOTSTRAP_PASSWORD)")
			os.Exit(1)
		}
		logger.Info("startup step", "step", "6/bootstrap", "mode", "cli",
			"msg", "running CLI bootstrap (one-shot, will exit)", "email", email, "force_reset", *forceReset)
		if err := bootstrapAdminCLI(ctx, pool.Pool, email, password, *forceReset); err != nil {
			logger.Error("startup failed", "step", "6/bootstrap", "mode", "cli", "err", err)
			os.Exit(1)
		}
		logger.Info("startup ok", "step", "6/bootstrap", "mode", "cli",
			"email", email, "msg", "bootstrap complete; exiting (no server started)")
		return
	}

	// Camino B: Auto-bootstrap silencioso desde env vars
	if cfg.BootstrapEmail != "" && cfg.BootstrapPassword != "" {
		logger.Info("startup step", "step", "6/bootstrap", "mode", "startup",
			"msg", "checking first-run auto-bootstrap", "email", cfg.BootstrapEmail)
		n, created, err := bootstrapAdminStartup(ctx, pool.Pool, cfg.BootstrapEmail, cfg.BootstrapPassword)
		if err != nil {
			logger.Error("startup failed", "step", "6/bootstrap", "mode", "startup", "err", err)
			os.Exit(1)
		}
		if created {
			logger.Info("startup ok", "step", "6/bootstrap", "mode", "startup",
				"email", cfg.BootstrapEmail, "msg", "admin created on empty DB")
		} else {
			logger.Info("startup ok", "step", "6/bootstrap", "mode", "startup",
				"msg", "skipping auto-bootstrap; users table already populated",
				"users", n)
		}
	}

	// STEP 7: Resolver bundle dir (binarios del agente)
	logger.Info("startup step", "step", "7/bundle_dir", "msg", "resolving agent bundle dir", "path", cfg.BundleDir)
	bundleDir := cfg.BundleDir
	if abs, err := filepath.Abs(bundleDir); err == nil {
		bundleDir = abs
	}
	if entries, err := os.ReadDir(bundleDir); err != nil {
		logger.Warn("startup warn", "step", "7/bundle_dir",
			"path", bundleDir, "err", err,
			"msg", "agent downloads will fail until binaries are placed in this dir")
	} else {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		logger.Info("startup ok", "step", "7/bundle_dir",
			"path", bundleDir, "count", len(entries), "files", strings.Join(names, ","))
	}

	// STEP 8: Verificar web dist (panel)
	logger.Info("startup step", "step", "8/web_dist", "msg", "checking web dist", "path", cfg.WebDist)
	webDist := cfg.WebDist
	if abs, err := filepath.Abs(webDist); err == nil {
		webDist = abs
	}
	if info, err := os.Stat(webDist); err != nil {
		logger.Warn("startup warn", "step", "8/web_dist",
			"path", webDist, "err", err,
			"msg", "panel will not be served (404 on /)")
	} else if !info.IsDir() {
		logger.Warn("startup warn", "step", "8/web_dist",
			"path", webDist, "msg", "path exists but is not a directory")
	} else {
		indexPath := filepath.Join(webDist, "index.html")
		if _, err := os.Stat(indexPath); err != nil {
			logger.Warn("startup warn", "step", "8/web_dist",
				"path", webDist, "err", err,
				"msg", "web dist dir exists but index.html is missing; panel will not be served")
		} else {
			logger.Info("startup ok", "step", "8/web_dist", "path", webDist)
		}
	}

	// STEP 9: WS hub + jobs dispatcher (Fase 3 / DT-5). El dispatcher se
	// crea aquí para que el api.Server pueda inyectarlo en el handler
	// WS (case command_result -> dispatcher.HandleCommandResult). La
	// goroutine de tick arranca en step 12c.
	logger.Info("startup step", "step", "9/ws_hub", "msg", "initializing websocket hub")
	hub := ws.NewHub()
	dispatcher := jobs.NewDispatcher(pool.Pool, hub, logger)
	logger.Info("startup ok", "step", "9/ws_hub")

	// STEP 10: API server
	logger.Info("startup step", "step", "10/api_server", "msg", "building api server")
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
		Dispatcher:     dispatcher,
	}
	logger.Info("startup step", "step", "10/api_server", "msg", "building router")
	router := api.NewRouter(srv)
	logger.Info("startup ok", "step", "10/api_server")

	// STEP 11: HTTP server
	logger.Info("startup step", "step", "11/http_server", "msg", "configuring http server", "addr", cfg.Bind)
	httpSrv := &http.Server{
		Addr:              cfg.Bind,
		Handler:           router,
		ReadHeaderTimeout: 15 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	logger.Info("startup ok", "step", "11/http_server")

	// STEP 12: Background session purger
	logger.Info("startup step", "step", "12/bg_purger", "msg", "starting session purger goroutine")
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
	logger.Info("startup ok", "step", "12/bg_purger")

	// STEP 12b: Inventory snapshot purger (Fase 2). Mantiene las últimas
	// N snapshots por agente (N = inventory.MaxSnapshotsPerAgent). Corre
	// cada hora en paralelo con el purger de sesiones.
	logger.Info("startup step", "step", "12b/inventory_purger", "msg", "starting inventory snapshot purger")
	go func() {
		t := time.NewTicker(1 * time.Hour)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if n, err := inventory.PurgeHistory(context.Background(), pool.Pool, inventory.MaxSnapshotsPerAgent); err == nil && n > 0 {
					logger.Info("purged inventory snapshots", "count", n, "keep", inventory.MaxSnapshotsPerAgent)
				}
			}
		}
	}()
	logger.Info("startup ok", "step", "12b/inventory_purger")

	// STEP 12c: Jobs dispatcher (Fase 3 / DT-5). El dispatcher se creó
	// en step 9 (necesario para api.Server.Dispatcher); acá sólo
	// arrancamos su goroutine de tick.
	logger.Info("startup step", "step", "12c/dispatcher", "msg", "starting jobs dispatcher goroutine")
	dispatcher.Start(ctx)
	logger.Info("startup ok", "step", "12c/dispatcher")

	// STEP 13: Listen (TCP bind + serve)
	logger.Info("startup step", "step", "13/listen", "msg", "binding tcp and starting serve", "addr", cfg.Bind)
	errCh := make(chan error, 1)
	go func() {
		logger.Info("startup complete", "step", "13/listen",
			"msg", "READY: sai-server is listening",
			"addr", cfg.Bind,
			"public_url", cfg.PublicURL,
			"env", string(cfg.Env),
		)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	// Esperar señal o error fatal
	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received", "signal", "SIGINT/SIGTERM")
	case err := <-errCh:
		logger.Error("http server fatal error", "err", err, "hint", "chequear puerto en uso, permisos, o ulimits")
	}

	// Graceful shutdown
	logger.Info("shutdown", "msg", "starting graceful shutdown", "timeout", "10s")
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown error", "err", err)
	}
	logger.Info("bye", "msg", "sai-server exited cleanly")
}

// -----------------------------------------------------------------------------
// healthcheck probe
// -----------------------------------------------------------------------------

func runHealthcheck() {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://127.0.0.1:8080/api/v1/health")
	if err != nil {
		fmt.Fprintf(os.Stderr, "healthcheck: probe failed: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "healthcheck: unexpected status %d\n", resp.StatusCode)
		os.Exit(1)
	}
	os.Exit(0)
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
	// Multi-handler: stderr (tiempo real) + archivo (persistente, útil en docker).
	writers := []slog.Handler{
		slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lv}),
	}
	if f, err := openLogFile(); err == nil {
		writers = append(writers, slog.NewTextHandler(f, &slog.HandlerOptions{Level: lv}))
	}
	return slog.New(multiHandler(writers))
}

type multiHandler []slog.Handler

func (m multiHandler) Enabled(ctx context.Context, l slog.Level) bool {
	for _, h := range m {
		if h.Enabled(ctx, l) {
			return true
		}
	}
	return false
}
func (m multiHandler) Handle(ctx context.Context, r slog.Record) error {
	for _, h := range m {
		if h.Enabled(ctx, r.Level) {
			_ = h.Handle(ctx, r.Clone())
		}
	}
	return nil
}
func (m multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := make(multiHandler, len(m))
	for i, h := range m {
		out[i] = h.WithAttrs(attrs)
	}
	return out
}
func (m multiHandler) WithGroup(name string) slog.Handler {
	out := make(multiHandler, len(m))
	for i, h := range m {
		out[i] = h.WithGroup(name)
	}
	return out
}

func openLogFile() (*os.File, error) {
	paths := []string{"/var/log/sai/server.log", "./sai-server.log"}
	for _, p := range paths {
		if dir := filepath.Dir(p); dir != "." && dir != "" {
			if err := os.MkdirAll(dir, 0755); err != nil {
				continue
			}
		}
		f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err == nil {
			return f, nil
		}
	}
	return nil, errors.New("no se pudo abrir archivo de log")
}

func redactDSN(dsn string) string {
	if i := strings.Index(dsn, "://"); i >= 0 {
		if j := strings.Index(dsn[i+3:], "@"); j >= 0 {
			return dsn[:i+3] + "***" + dsn[i+3+j:]
		}
	}
	return dsn
}

func writeCrashLog(r any) {
	paths := []string{"/var/log/sai/crash.log", "./sai-server.crash.log"}
	for _, p := range paths {
		if dir := filepath.Dir(p); dir != "." && dir != "" {
			if err := os.MkdirAll(dir, 0755); err != nil {
				continue
			}
		}
		body := fmt.Sprintf("panic: %v\ntime: %s\npid: %d\nversion: %s\nstack:\n%s\n",
			r, time.Now().Format(time.RFC3339), os.Getpid(), version.Version, debug.Stack())
		if err := os.WriteFile(p, []byte(body), 0644); err == nil {
			fmt.Fprintf(os.Stderr, "crash log written to %s\n", p)
			return
		}
	}
}

// bootstrapAdminCLI es el bootstrap "operator-driven" usado por el flag CLI
// --bootstrap. Es idempotente y soporta rotación/reemplazo de credenciales:
//   - DB vacía         → crea admin con el email/password dados.
//   - 1 usuario, mismo email → resetea el password (olvido de contraseña).
//   - 1 usuario, email distinto → rechaza salvo --force-reset, en cuyo caso
//     borra al admin previo y crea uno nuevo.
//
// Tras ejecutarse exitosamente, el caller hace `return` desde main: el server
// NO arranca. Éste es el modo que mantienen los scripts verify-docker.sh/ps1
// y el quickstart del README.
//
// Para auto-bootstrap en primer arranque desde env vars, ver
// bootstrapAdminStartup más abajo.
func bootstrapAdminCLI(ctx context.Context, pool *pgxpool.Pool, email, password string, forceReset bool) error {
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

// bootstrapAdminStartup es el auto-bootstrap "de primer arranque" que se evalúa
// como un paso más de startup cuando SAI_BOOTSTRAP_EMAIL y SAI_BOOTSTRAP_PASSWORD
// están definidas en el entorno. Su semántica es deliberadamente conservadora:
//
//   - Cuenta cuántos usuarios existen en la tabla `users`.
//   - Si hay 0 usuarios (DB fresca) → crea el admin con el email/password dados
//     y registra un audit_event. Devuelve created=true.
//   - Si hay ≥1 usuario → NO TOCA nada. Devuelve created=false y el caller
//     registra el skip en el log. Esto es la pieza crítica del fix:
//     antes, este camino reusaba la lógica CLI y reseteaba silenciosamente
//     la contraseña del admin existente en cada restart del container.
//
// Tras retornar, el caller continúa al STEP 7 (el server SIEMPRE arranca).
//
// Devuelve (usersCount, created, error).
func bootstrapAdminStartup(ctx context.Context, pool *pgxpool.Pool, email, password string) (int, bool, error) {
	hash, err := auth.HashPassword(password)
	if err != nil {
		return 0, false, err
	}

	var n int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return 0, false, err
	}
	if n > 0 {
		return n, false, nil
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO users (email, password_hash, role, is_active)
		VALUES ($1, $2, 'admin', TRUE)
	`, email, hash); err != nil {
		return 0, false, err
	}
	_, _ = pool.Exec(ctx, `
		INSERT INTO audit_events (actor_type, actor_label, action, metadata)
		VALUES ('system', 'system', 'system.bootstrap_admin_startup', jsonb_build_object('email', $1::text, 'mode', 'first_run'))
	`, email)
	return 1, true, nil
}
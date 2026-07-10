// Command agent es el agente SAI: corre como servicio nativo y mantiene
// una conexión WSS reversa con el servidor.
package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/Naired01/SAI/internal/inventory"
	"github.com/Naired01/SAI/internal/version"
	"github.com/gorilla/websocket"
)

type Config struct {
	ServerURL       string         `json:"server_url"`
	EnrollmentToken string         `json:"enrollment_token"`
	AgentID         string         `json:"agent_id"`
	// SessionJWT (Fase 3 / DT-3): JWT persistente firmado por el server
	// con agent_credentials.jwt_secret. Se carga desde session.jwt al
	// arrancar y reemplaza al enrollment_token en reconexiones. No se
	// persiste en config.json por seguridad.
	SessionJWT      string         `json:"-"`
	Labels          map[string]any `json:"labels,omitempty"`
	InsecureSkipTLS bool           `json:"insecure_skip_tls,omitempty"`
	HeartbeatSecs   int            `json:"heartbeat_secs,omitempty"`
}

// errReauthRequired indica que el server rechazó el session_jwt (por
// ejemplo, el secret fue rotado). El agente borra session.jwt y la
// proxima reconexión cae al path legacy de enrollment_token.
var errReauthRequired = errors.New("reauth_required: server rejected session jwt")

// isJWTNonEmpty valida que un string parezca un JWT (tres segmentos
// separados por '.'). Evita guardar session.jwt cuando el server v0.2.1
// (que no emite jwt) responde con welcome vacío en ese campo.
func isJWTNonEmpty(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	parts := strings.Split(s, ".")
	return len(parts) == 3
}

func main() {
	var (
		cfgPath  = flag.String("config", "", "Path al config.json")
		showVer  = flag.Bool("version", false, "Muestra la versión y sale")
		logFile  = flag.String("log-file", "", "Path al archivo de log (opcional; default = stderr)")
		jwtFileF = flag.String("jwt-file", "", "Path al session.jwt (opcional; default = <dir(config)>/session.jwt)")
	)
	flag.Parse()

	if *showVer {
		fmt.Println("sai-agent", version.Version, "commit="+version.Commit, "built="+version.BuildTime)
		return
	}
	if *cfgPath == "" {
		fmt.Fprintln(os.Stderr, "--config <path> required")
		os.Exit(2)
	}

	logger := newLogger(*logFile)

	cfg, err := loadConfig(*cfgPath)
	if err != nil {
		logger.Error("load config", "err", err)
		os.Exit(1)
	}
	if cfg.HeartbeatSecs == 0 {
		cfg.HeartbeatSecs = 30
	}

	// Sesión JWT persistente (Fase 3 / DT-3): si existe el archivo session.jwt
	// (de un arranque previo), lo cargamos en cfg.SessionJWT y el handshake
	// lo usará en lugar del enrollment_token. Esto evita que el agente quede
	// afuera si el enrollment_token se agota o se revoca tras el primer
	// enrolamiento.
	jwtPath := jwtFilePath(*cfgPath, *jwtFileF)
	if jwtPath != "" {
		if tok, err := loadJWT(jwtPath); err == nil {
			cfg.SessionJWT = tok
			logger.Info("loaded session jwt", "path", jwtPath, "len", len(tok))
		} else if !errors.Is(err, ErrNoJWT) {
			logger.Warn("session jwt load failed", "path", jwtPath, "err", err)
		}
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "unknown"
	}

	// Modo servicio de Windows: el SCM arranca el binario directamente y
	// espera que se registre via svc.Run. Si NO estamos bajo SCM, corremos
	// el loop normal como proceso interactivo.
	if ran, err := runAsService("sai-agent", ctx, cancel, logger, cfg, hostname, jwtPath); ran {
		if err != nil {
			logger.Error("service run failed", "err", err)
			os.Exit(1)
		}
		return
	}

	runMainLoop(ctx, logger, cfg, hostname, jwtPath)
}

// runMainLoop es el loop de reconexion infinito: corre runOnce, espera con
// backoff+jitter, reintenta. Termina cuando ctx se cancela (Ctrl-C, Stop
// del SCM, etc.). Llamado directamente por main() en modo interactivo, y
// desde la goroutine de saiService.Execute en modo servicio.
//
// jwtPath es la ruta del session.jwt que runOnce persiste tras cada welcome
// exitoso. Si está vacío, no persiste (modo deprecado sin Fase 3).
func runMainLoop(ctx context.Context, logger *slog.Logger, cfg *Config, hostname, jwtPath string) {
	backoff := time.Second
	for {
		if err := runOnce(ctx, logger, cfg, hostname, jwtPath); err != nil {
			logger.Warn("connection lost; will retry", "err", err, "backoff", backoff.String())
		}
		if ctx.Err() != nil {
			return
		}
		// jitter
		jitter := time.Duration(rand.Int63n(int64(backoff) / 4))
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff + jitter):
		}
		if backoff < 5*time.Minute {
			backoff *= 2
		}
	}
}

func runOnce(ctx context.Context, logger *slog.Logger, cfg *Config, hostname, jwtPath string) error {
	dialer := *websocket.DefaultDialer
	dialer.HandshakeTimeout = 15 * time.Second
	if cfg.InsecureSkipTLS {
		dialer.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	// Fase 3 (DT-3): el handshake admite dos caminos:
	//   a) cfg.SessionJWT != ""  -> enviar Authorization: Bearer <jwt> en
	//      headers. El server valida contra agent_credentials.jwt_secret.
	//      Enrollment token NO se consume.
	//   b) cfg.SessionJWT == ""  -> fallback legacy: enviar enrollment_token
	//      en el cuerpo del hello. El server lo canjea via tokens.Redeem.
	// Si el server rechaza el JWT (e.g. secret rotado), el server emite un
	// error code "reauth_required"; runOnce devuelve un error especial
	// para que el caller borre session.jwt y reintente con enrollment_token.
	headers := http.Header{}
	if cfg.SessionJWT != "" {
		headers.Set("Authorization", "Bearer "+cfg.SessionJWT)
	}

	logger.Info("connecting", "url", cfg.ServerURL, "has_jwt", cfg.SessionJWT != "")
	conn, resp, err := dialer.Dial(cfg.ServerURL, headers)
	if err != nil {
		if resp != nil {
			return fmt.Errorf("dial: %w (status %d)", err, resp.StatusCode)
		}
		return err
	}
	defer conn.Close()
	logger.Info("connected")

	// 1) Enviar hello. El campo `token` se manda siempre para mantener
	// compat con server v0.2.1 (que solo mira el body). Cuando el server
	// valide el JWT (v0.3.0+), aceptará el bearer del header y skipeará
	// Redeem. Si el bearer falla, canjeamos el token normalmente.
	hello := map[string]any{
		"type":          "hello",
		"token":         cfg.EnrollmentToken,
		"agent_version": version.Version,
		"os":            runtime.GOOS,
		"arch":          runtime.GOARCH,
		"hostname":      hostname,
		"os_version":    runtimeOSVersion(),
		"labels":        cfg.Labels,
	}
	if err := conn.WriteJSON(hello); err != nil {
		return fmt.Errorf("write hello: %w", err)
	}

	// 2) Esperar welcome (con timeout)
	conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("read welcome: %w", err)
	}
	var welcome map[string]any
	if err := json.Unmarshal(raw, &welcome); err != nil {
		return fmt.Errorf("welcome parse: %w", err)
	}
	if welcome["type"] != "welcome" {
		// Si el server respondió con un error (e.g. "reauth_required"),
		// limpiamos el JWT persistido para forzar reenrollment.
		if welcome["type"] == "error" {
			if code, _ := welcome["code"].(string); code == "reauth_required" && jwtPath != "" {
				logger.Warn("server rejected session jwt, clearing", "path", jwtPath)
				_ = clearJWT(jwtPath)
				cfg.SessionJWT = ""
				return errReauthRequired
			}
		}
		return fmt.Errorf("expected welcome, got %v", welcome["type"])
	}
	agentID, _ := welcome["agent_id"].(string)
	logger.Info("welcomed", "agent_id", agentID)

	// 2b) Persistir el session_jwt que el server acaba de emitir. Si el
	// server está en v0.2.1 (no emite jwt), welcome["session_jwt"] será ""
	// y saveJWT guardará un archivo vacío — guarded por isJWTNonEmpty abajo.
	if jwtPath != "" {
		if tok, _ := welcome["session_jwt"].(string); isJWTNonEmpty(tok) {
			if err := saveJWT(jwtPath, tok); err != nil {
				logger.Warn("save session jwt failed", "path", jwtPath, "err", err)
			} else {
				logger.Info("persisted session jwt", "path", jwtPath)
			}
		}
	}

	// 3) Loop: heartbeats + procesamiento de mensajes del server.
	//
	// Usamos una goroutine reader dedicada que bloquea en ReadMessage y
	// entrega cada mensaje por msgCh. Esto evita el patron anterior de
	// `default:` con ReadMessage no bloqueante, que tras un error del server
	// seguia llamando ReadMessage y disparaba el panic de gorilla
	// "repeated read on failed websocket connection" tras ~1000 llamadas.
	conn.SetReadDeadline(time.Time{})
	msgCh := make(chan []byte, 16)
	readErrCh := make(chan error, 1)
	go func() {
		defer close(msgCh)
		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				readErrCh <- err
				return
			}
			select {
			case msgCh <- raw:
			case <-ctx.Done():
				return
			}
		}
	}()

	hb := time.NewTicker(time.Duration(cfg.HeartbeatSecs) * time.Second)
	defer hb.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = conn.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, "shutdown"))
			return nil
		case <-hb.C:
			if err := conn.WriteJSON(map[string]any{
				"type": "heartbeat",
				"ts":   time.Now().UTC().Format(time.RFC3339),
			}); err != nil {
				return fmt.Errorf("write heartbeat: %w", err)
			}
		case raw, ok := <-msgCh:
			if !ok {
				return fmt.Errorf("reader goroutine closed")
			}
			handleServerMessage(ctx, logger, conn, raw)
		case err := <-readErrCh:
			return fmt.Errorf("read: %w", err)
		}
	}
}

// handleServerMessage despacha por tipo. Hoy: inventory_request (Fase 2).
// Otros tipos se loggean en debug. Esta función nunca retorna error: si falla,
// loggea y continúa; la conexión sigue siendo utilizable para heartbeats.
func handleServerMessage(ctx context.Context, logger *slog.Logger, conn *websocket.Conn, raw []byte) {
	var hdr struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	}
	if err := json.Unmarshal(raw, &hdr); err != nil {
		logger.Debug("server msg: bad json", "raw", string(raw))
		return
	}
	switch hdr.Type {
	case "inventory_request":
		handleInventoryRequest(ctx, logger, conn, hdr.ID)
	default:
		logger.Debug("server msg (unhandled)", "type", hdr.Type)
	}
}

// handleInventoryRequest recolecta el inventario HW del equipo y responde
// con un inventory_snapshot. El collectedAt lleva la hora del agente, no la
// del server, para que la UI pueda mostrar "Recolectado el..." coherente con
// la captura real.
func handleInventoryRequest(ctx context.Context, logger *slog.Logger, conn *websocket.Conn, reqID string) {
	if reqID == "" {
		logger.Warn("inventory_request sin id; se ignora")
		return
	}
	collectCtx, cancel := context.WithTimeout(ctx, inventory.CollectTimeout)
	defer cancel()

	start := time.Now()
	snap := inventory.Collect(collectCtx, version.Version)
	// Collect nunca retorna error; los problemas parciales van al campo Error.
	dur := time.Since(start)

	hw := snap.Hardware
	sw := snap.Software
	resp := inventory.SnapshotMsg{
		Type:         "inventory_snapshot",
		ID:           reqID,
		SchemaVer:    inventory.SchemaVer,
		AgentVersion: version.Version,
		Hardware:     &hw,
		Software:     &sw,
		Error:        snap.Error,
		CollectedAt:  snap.CollectedAt,
	}
	if err := conn.WriteJSON(resp); err != nil {
		logger.Error("write inventory_snapshot", "err", err)
		return
	}
	logger.Info("inventory_snapshot sent",
		"request_id", reqID,
		"agent_version", version.Version,
		"duration_ms", dur.Milliseconds(),
		"has_error", snap.Error != "",
	)
}

func loadConfig(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// newLogger devuelve un slog logger que escribe a stderr o, si se pasa
// logFile, a ese archivo (creando el directorio si hace falta). Usado
// cuando el agente corre como servicio de Windows y no hay stderr visible.
func newLogger(logFile string) *slog.Logger {
	if logFile == "" {
		return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	}
	if dir := filepath.Dir(logFile); dir != "" {
		_ = os.MkdirAll(dir, 0o755)
	}
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		// Fallback a stderr si no se puede abrir el archivo.
		fmt.Fprintf(os.Stderr, "open log file %q failed: %v; falling back to stderr\n", logFile, err)
		return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	}
	return slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

func runtimeOSVersion() string {
	// Para versiones específicas del OS usamos gopsutil desde Fase 2;
	// aquí devolvemos un placeholder corto. Inventory lo sobreescribe
	// cuando esté disponible con información real del kernel.
	return runtime.GOOS + "/" + runtime.GOARCH
}
// Command agent es el agente SAI: corre como servicio nativo y mantiene
// una conexión WSS reversa con el servidor.
package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"runtime"
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
	Labels          map[string]any `json:"labels,omitempty"`
	InsecureSkipTLS bool           `json:"insecure_skip_tls,omitempty"`
	HeartbeatSecs   int            `json:"heartbeat_secs,omitempty"`
}

func main() {
	var (
		cfgPath = flag.String("config", "", "Path al config.json")
		showVer = flag.Bool("version", false, "Muestra la versión y sale")
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

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := loadConfig(*cfgPath)
	if err != nil {
		logger.Error("load config", "err", err)
		os.Exit(1)
	}
	if cfg.HeartbeatSecs == 0 {
		cfg.HeartbeatSecs = 30
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "unknown"
	}

	backoff := time.Second
	for {
		if err := runOnce(ctx, logger, cfg, hostname); err != nil {
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

func runOnce(ctx context.Context, logger *slog.Logger, cfg *Config, hostname string) error {
	dialer := *websocket.DefaultDialer
	dialer.HandshakeTimeout = 15 * time.Second
	if cfg.InsecureSkipTLS {
		dialer.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	// Fase 1: el handshake es siempre hello+token. El server emite un
	// session_jwt en el welcome pero aún no lo validamos: el agent no lo
	// persiste (cada reconexión reusa el enrollment token, que ya está
	// idempotente en el server via FindByEnrollmentAndHost). La validación
	// JWT llega en Fase 3.
	headers := http.Header{}

	logger.Info("connecting", "url", cfg.ServerURL)
	conn, resp, err := dialer.Dial(cfg.ServerURL, headers)
	if err != nil {
		if resp != nil {
			return fmt.Errorf("dial: %w (status %d)", err, resp.StatusCode)
		}
		return err
	}
	defer conn.Close()
	logger.Info("connected")

	// 1) Enviar hello
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
		return fmt.Errorf("expected welcome, got %v", welcome["type"])
	}
	agentID, _ := welcome["agent_id"].(string)
	logger.Info("welcomed", "agent_id", agentID)

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

func runtimeOSVersion() string {
	// Para versiones específicas del OS usamos gopsutil desde Fase 2;
	// aquí devolvemos un placeholder corto. Inventory lo sobreescribe
	// cuando esté disponible con información real del kernel.
	return runtime.GOOS + "/" + runtime.GOARCH
}
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
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

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

	// 3) Loop: heartbeats
	conn.SetReadDeadline(time.Time{})
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
		default:
			// lectura no bloqueante: si llega algo del server lo procesamos
			conn.SetReadDeadline(time.Now().Add(1 * time.Second))
			_, raw, err := conn.ReadMessage()
			if err != nil {
				if ne, ok := err.(net.Error); ok && ne.Timeout() {
					continue
				}
				return err
			}
			logger.Debug("server msg", "raw", string(raw))
		}
	}
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
	// Por ahora no parseamos versiones específicas; Fase 2 usa gopsutil.
	return runtime.GOOS + "/" + runtime.GOARCH
}
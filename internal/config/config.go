// Package config carga la configuración del servidor desde variables de entorno.
//
// Uso:
//
//	cfg, err := config.Load()
//	if err != nil { ... }
package config

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/caarlos0/env/v10"
)

type TLSMode string

const (
	TLSReverseProxy TLSMode = "reverse_proxy"
	TLSDirect       TLSMode = "direct"
)

type Env string

const (
	EnvDevelopment Env = "development"
	EnvProduction  Env = "production"
)

// Config agrupa toda la configuración del proceso del servidor.
type Config struct {
	// Runtime
	Env    Env     `env:"SAI_ENV"    envDefault:"development"`
	Bind   string  `env:"SAI_BIND"   envDefault:":8080"`
	LogLvl string  `env:"SAI_LOG_LEVEL" envDefault:"info"`
	TLS    TLSMode `env:"SAI_TLS_MODE" envDefault:"reverse_proxy"`

	// URLs / red
	PublicURL  string `env:"SAI_PUBLIC_URL"`
	WebhookURL string `env:"SAI_WEBHOOK_URL"`

	// DB
	DBURL string `env:"SAI_DB_URL" envDefault:""`

	// Auth admin
	JWTSecret string `env:"SAI_JWT_SECRET" envDefault:""`

	// Auth agente
	AgentJWTSecret string `env:"SAI_AGENT_JWT_SECRET" envDefault:""`

	// Tokens de enrolamiento
	AgentTokenTTL time.Duration `env:"SAI_AGENT_TOKEN_TTL" envDefault:"24h"`

	// Bundle del agente
	BundleDir string `env:"SAI_BUNDLE_DIR" envDefault:"./dist"`

	// i18n
	DefaultLang string `env:"SAI_DEFAULT_LANG" envDefault:"es"`

	// Sesiones admin
	SessionTTL time.Duration `env:"SAI_SESSION_TTL" envDefault:"8h"`

	// Web UI
	WebDist string `env:"SAI_WEB_DIST" envDefault:"./web/dist"`

	// URL firmada de descarga
	AgentDownloadURLTTL time.Duration `env:"SAI_AGENT_DOWNLOAD_URL_TTL" envDefault:"15m"`

	// Bootstrap admin (CLI)
	BootstrapEmail    string `env:"SAI_BOOTSTRAP_EMAIL" envDefault:""`
	BootstrapPassword string `env:"SAI_BOOTSTRAP_PASSWORD" envDefault:""`
}

// Load parsea las variables de entorno, aplica defaults y valida.
func Load() (*Config, error) {
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("parse env: %w", err)
	}
	cfg.applyDevDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) applyDevDefaults() {
	if c.Env == EnvDevelopment {
		if c.DBURL == "" {
			c.DBURL = "postgres://sai:sai@localhost:5432/sai?sslmode=disable"
		}
		if c.JWTSecret == "" {
			c.JWTSecret = "dev-jwt-secret-change-me-please-change-me-please-change-me"
		}
		if c.AgentJWTSecret == "" {
			c.AgentJWTSecret = "dev-agent-jwt-secret-change-me-please-change-me-please"
		}
	}
}

// Validate revisa invariantes de la configuración.
func (c *Config) Validate() error {
	var errs []string
	if c.Env != EnvDevelopment && c.Env != EnvProduction {
		errs = append(errs, fmt.Sprintf("SAI_ENV inválido: %q", c.Env))
	}
	if c.TLS != TLSReverseProxy && c.TLS != TLSDirect {
		errs = append(errs, fmt.Sprintf("SAI_TLS_MODE inválido: %q", c.TLS))
	}
	if c.DBURL == "" {
		errs = append(errs, "SAI_DB_URL es requerido")
	}
	if len(c.JWTSecret) < 32 {
		errs = append(errs, "SAI_JWT_SECRET debe tener al menos 32 caracteres")
	}
	if len(c.AgentJWTSecret) < 32 {
		errs = append(errs, "SAI_AGENT_JWT_SECRET debe tener al menos 32 caracteres")
	}
	if c.DefaultLang != "es" && c.DefaultLang != "en" {
		errs = append(errs, fmt.Sprintf("SAI_DEFAULT_LANG inválido: %q (usa 'es' o 'en')", c.DefaultLang))
	}
	if len(errs) > 0 {
		return errors.New("config inválida: " + strings.Join(errs, "; "))
	}
	return nil
}

// IsProduction reporta si el entorno es producción.
func (c *Config) IsProduction() bool { return c.Env == EnvProduction }

// IsDevelopment reporta si el entorno es desarrollo.
func (c *Config) IsDevelopment() bool { return c.Env == EnvDevelopment }
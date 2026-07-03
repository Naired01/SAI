// Package i18n carga mensajes de backend desde archivos JSON embebidos y
// selecciona el idioma según el header `Accept-Language` de la request.
package i18n

import (
	"context"
	"embed"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
)

type ctxKey struct{}

//go:embed locales/*.json
var localesFS embed.FS

type Bundle struct {
	mu       sync.RWMutex
	messages map[string]map[string]string // lang -> key -> message
	fallback string
}

func NewBundle(fallback string) (*Bundle, error) {
	if fallback == "" {
		fallback = "es"
	}
	b := &Bundle{
		messages: make(map[string]map[string]string),
		fallback: fallback,
	}
	entries, err := localesFS.ReadDir("locales")
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		lang := strings.TrimSuffix(e.Name(), ".json")
		body, err := localesFS.ReadFile("locales/" + e.Name())
		if err != nil {
			return nil, err
		}
		var msgs map[string]string
		if err := json.Unmarshal(body, &msgs); err != nil {
			return nil, err
		}
		b.messages[lang] = msgs
	}
	return b, nil
}

// Languages devuelve los idiomas cargados.
func (b *Bundle) Languages() []string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]string, 0, len(b.messages))
	for k := range b.messages {
		out = append(out, k)
	}
	return out
}

// T devuelve el mensaje para la clave en el idioma del contexto.
// Si no existe en el idioma, cae al fallback; si tampoco, devuelve la clave.
// Nil-safe: si b es nil devuelve la clave tal cual.
func (b *Bundle) T(ctx context.Context, key string) string {
	if b == nil {
		return key
	}
	lang := LangFromContext(ctx)
	return b.Lookup(lang, key)
}

// Lookup devuelve el mensaje para (lang, key) con fallback.
func (b *Bundle) Lookup(lang, key string) string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if msgs, ok := b.messages[lang]; ok {
		if v, ok := msgs[key]; ok {
			return v
		}
	}
	if msgs, ok := b.messages[b.fallback]; ok {
		if v, ok := msgs[key]; ok {
			return v
		}
	}
	return key
}

// SetLang enriquece el request con el idioma detectado para usar en handlers.
func SetLang(r *http.Request, defaultLang string) *http.Request {
	lang := detectLang(r.Header.Get("Accept-Language"), defaultLang)
	ctx := context.WithValue(r.Context(), ctxKey{}, lang)
	return r.WithContext(ctx)
}

// WithLang crea un nuevo contexto con el idioma dado.
func WithLang(ctx context.Context, lang string) context.Context {
	return context.WithValue(ctx, ctxKey{}, lang)
}

// LangFromContext extrae el idioma del contexto (default "es").
func LangFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKey{}).(string); ok && v != "" {
		return v
	}
	return "es"
}

func detectLang(accept, defaultLang string) string {
	if accept == "" {
		return defaultLang
	}
	// Parse simple: "es-MX,es;q=0.9,en;q=0.8" → toma el primer tag
	for _, part := range strings.Split(accept, ",") {
		tag := strings.TrimSpace(part)
		if idx := strings.IndexByte(tag, ';'); idx >= 0 {
			tag = tag[:idx]
		}
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		// match exacto ("es")
		if _, ok := map[string]struct{}{"es": {}, "en": {}}[tag]; ok {
			return tag
		}
		// match por prefijo ("es-MX" → "es")
		if idx := strings.IndexByte(tag, '-'); idx > 0 {
			prefix := tag[:idx]
			if _, ok := map[string]struct{}{"es": {}, "en": {}}[prefix]; ok {
				return prefix
			}
		}
	}
	return defaultLang
}
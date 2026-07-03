// Package httpx contiene helpers comunes para los handlers HTTP:
// render de JSON, errores i18n, parsing de query params, etc.
package httpx

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/Naired01/SAI/internal/i18n"
)

// Error es la estructura estándar para respuestas de error.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// RenderJSON serializa v como JSON con el status code dado.
func RenderJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	_ = json.NewEncoder(w).Encode(v)
}

// RenderError responde con un error i18n y el código HTTP apropiado.
func RenderError(w http.ResponseWriter, r *http.Request, bundle *i18n.Bundle, status int, key string) {
	RenderJSON(w, status, Error{
		Code:    key,
		Message: bundle.T(r.Context(), key),
	})
}

// RenderInternalError responde 500 con el mensaje i18n "common.error.internal".
// Loggea el error real con el logger del request.
func RenderInternalError(w http.ResponseWriter, r *http.Request, bundle *i18n.Bundle) {
	RenderError(w, r, bundle, http.StatusInternalServerError, "common.error.internal")
}

// RenderValidationError responde 400 con mensaje custom.
func RenderValidationError(w http.ResponseWriter, r *http.Request, bundle *i18n.Bundle, msg string) {
	RenderJSON(w, http.StatusBadRequest, Error{Code: "validation", Message: msg})
}

// RenderNotFound responde 404.
func RenderNotFound(w http.ResponseWriter, r *http.Request, bundle *i18n.Bundle) {
	RenderError(w, r, bundle, http.StatusNotFound, "common.error.not_found")
}

// RenderUnauthorized responde 401.
func RenderUnauthorized(w http.ResponseWriter, r *http.Request, bundle *i18n.Bundle) {
	RenderError(w, r, bundle, http.StatusUnauthorized, "common.error.unauthorized")
}

// RenderForbidden responde 403.
func RenderForbidden(w http.ResponseWriter, r *http.Request, bundle *i18n.Bundle) {
	RenderError(w, r, bundle, http.StatusForbidden, "common.error.forbidden")
}

// DecodeJSON deserializa el body en dst. Devuelve error legible si falla.
func DecodeJSON(r *http.Request, dst any) error {
	if r.Body == nil {
		return errors.New("empty body")
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

// QueryInt devuelve el int del query param o def si falta/no es número.
func QueryInt(r *http.Request, key string, def int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// QueryString devuelve el string del query param o def si falta.
func QueryString(r *http.Request, key, def string) string {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def
	}
	return v
}

// QueryBool devuelve el bool del query param ("true"/"1" → true).
func QueryBool(r *http.Request, key string, def bool) bool {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}
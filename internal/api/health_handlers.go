package api

import (
	"net/http"

	"github.com/Naired01/SAI/internal/httpx"
)

// handleHealth GET /api/v1/health
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if err := s.Pool.Ping(r.Context()); err != nil {
		httpx.RenderJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status": "degraded",
			"db":     "down",
		})
		return
	}
	httpx.RenderJSON(w, http.StatusOK, map[string]any{
		"status":     "ok",
		"uptime_sec": int(s.StartTime.Sub(s.StartTime).Seconds()), // placeholder
	})
}

// handleVersion GET /api/v1/version
func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	httpx.RenderJSON(w, http.StatusOK, s.versionInfo())
}
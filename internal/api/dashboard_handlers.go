package api

import (
	"net/http"

	"github.com/Naired01/SAI/internal/dashboard"
	"github.com/Naired01/SAI/internal/httpx"
)

// handleDashboardSummary GET /api/v1/dashboard/summary
func (s *Server) handleDashboardSummary(w http.ResponseWriter, r *http.Request) {
	summary, err := dashboard.Build(r.Context(), s.Pool)
	if err != nil {
		s.Logger.Error("dashboard summary", "err", err)
		httpx.RenderInternalError(w, r, s.Bundle)
		return
	}
	httpx.RenderJSON(w, http.StatusOK, summary)
}
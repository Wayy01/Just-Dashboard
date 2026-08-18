package api

import (
	"net/http"

	"github.com/Wayy01/vps-dashboard/backend/internal/httpx"
)

func (s *Server) handleDeployWebhook(w http.ResponseWriter, r *http.Request) error {
	return httpx.Err(http.StatusServiceUnavailable, "unavailable", "deploy module not initialised")
}

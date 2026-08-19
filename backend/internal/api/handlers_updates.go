package api

import (
	"errors"
	"net/http"

	"github.com/Wayy01/Just-Dashboard/backend/internal/httpx"
	"github.com/Wayy01/Just-Dashboard/backend/internal/updates"
	"github.com/go-chi/chi/v5"
)

func (s *Server) mountUpdateRoutes(r chi.Router) {
	r.Route("/updates", func(r chi.Router) {
		r.Method(http.MethodGet, "/", s.handle(s.handleUpdatesCheck))
		// Applying updates restarts services and can change how the machine
		// boots, so it sits with the other irreversible operations.
		s.destructive(r, func(r chi.Router) {
			r.Method(http.MethodPost, "/apply", s.handle(s.handleUpdatesApply))
		})
	})
}

func (s *Server) handleUpdatesCheck(w http.ResponseWriter, r *http.Request) error {
	rep, err := s.modules.updates.Check(r.Context())
	if err != nil {
		if errors.Is(err, updates.ErrNotSupported) {
			return httpx.Err(http.StatusServiceUnavailable, "not_installed", err.Error())
		}
		return httpx.Internal(err)
	}
	httpx.JSON(w, http.StatusOK, rep)
	return nil
}

func (s *Server) handleUpdatesApply(w http.ResponseWriter, r *http.Request) error {
	securityOnly := r.URL.Query().Get("security") == "true"
	phrase := "upgrade packages"
	if securityOnly {
		phrase = "install security updates"
	}
	if err := httpx.RequireTypedConfirmation(w, r, phrase); err != nil {
		return err
	}
	out, err := s.modules.updates.Upgrade(r.Context(), securityOnly)
	httpx.SetAudit(r, "system.updates.apply", "", map[string]any{
		"securityOnly": securityOnly,
		"ok":           err == nil,
	})
	if err != nil {
		return httpx.Err(http.StatusBadGateway, "upgrade_failed", trimOutput(out))
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"output": trimOutput(out)})
	return nil
}

// trimOutput keeps an apt transcript to something a browser can render; the
// tail is the part that says whether it worked.
func trimOutput(out string) string {
	const max = 64 * 1024
	if len(out) <= max {
		return out
	}
	return "… earlier output trimmed …\n" + out[len(out)-max:]
}

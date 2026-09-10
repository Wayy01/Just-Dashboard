package api

import (
	"errors"
	"net"
	"net/http"
	"strconv"

	"github.com/Wayy01/Just-Dashboard/backend/internal/httpx"
	"github.com/Wayy01/Just-Dashboard/backend/internal/selfcfg"
)

// The dashboard's own settings: where it listens, who may reach it, and the
// buttons that restart it into a change.
//
// It sits beside /dashboard/update rather than under /system for the same
// reason that one does: /system is the operator's server, this is the tool
// they are looking at it through, and the two being confusable is exactly why
// they are named apart.

// handleSelfConfigStatus answers everything the settings page needs in one
// request: the configuration on disk, the endpoint it implies, anything the
// running process disagrees with, and the restart in flight if there is one.
func (s *Server) handleSelfConfigStatus(w http.ResponseWriter, r *http.Request) error {
	httpx.JSON(w, http.StatusOK, s.modules.selfConfig.Report(r.Context()))
	return nil
}

// handleSelfConfigApply writes new settings and restarts into them.
//
// It answers 202 before the work finishes, because the work replaces the
// process serving the request. The browser then follows the run record, which
// is on disk and survives everything that is about to happen — including,
// after a port change, the fact that the address it was reading it from is no
// longer the address the dashboard answers on.
func (s *Server) handleSelfConfigApply(w http.ResponseWriter, r *http.Request) error {
	var next selfcfg.Settings
	if err := httpx.DecodeJSON(r, &next); err != nil {
		return err
	}

	rep := s.modules.selfConfig.Report(r.Context())
	if !rep.Supported {
		return httpx.Err(http.StatusServiceUnavailable, "config_unsupported", rep.Reason)
	}

	// A change that moves the address is the one an operator cannot undo from
	// their browser: when the dashboard comes back somewhere else, this tab is
	// pointing at a port nothing is listening on any more. That earns the
	// typed phrase — the new address itself, so what is confirmed is the fact
	// that has to be read.
	if selfcfg.MovesEndpoint(rep.Settings, next) {
		phrase := net.JoinHostPort(next.Site, strconv.Itoa(next.Port))
		if err := httpx.RequireTypedConfirmation(w, r, phrase); err != nil {
			return err
		}
	}

	actor := httpx.MustPrincipal(r).Username()
	run, err := s.modules.selfConfig.Apply(r.Context(), next, actor, httpx.ClientIP(r))
	if err != nil {
		return mapSelfConfigError(err)
	}
	httpx.SetAudit(r, "dashboard.config.apply", run.Endpoint, map[string]any{
		"changes": run.Changes, "run": run.ID,
	})
	httpx.JSON(w, http.StatusAccepted, run)
	return nil
}

// handleSelfConfigRestart recreates the stack on the configuration already on
// disk, rebuilding the images first when asked.
func (s *Server) handleSelfConfigRestart(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Rebuild bool `json:"rebuild"`
	}
	if r.ContentLength > 0 {
		if err := httpx.DecodeJSON(r, &body); err != nil {
			return err
		}
	}
	actor := httpx.MustPrincipal(r).Username()
	run, err := s.modules.selfConfig.Restart(r.Context(), body.Rebuild, actor)
	if err != nil {
		return mapSelfConfigError(err)
	}
	httpx.SetAudit(r, "dashboard.restart", string(run.Action), map[string]any{"run": run.ID})
	httpx.JSON(w, http.StatusAccepted, run)
	return nil
}

// handleSelfConfigDismiss forgets a finished run, which is what clears the
// card once it has been read.
func (s *Server) handleSelfConfigDismiss(w http.ResponseWriter, r *http.Request) error {
	if err := s.modules.selfConfig.Dismiss(); err != nil {
		return mapSelfConfigError(err)
	}
	httpx.NoContent(w)
	return nil
}

func mapSelfConfigError(err error) error {
	switch {
	case errors.Is(err, selfcfg.ErrInProgress):
		return httpx.Err(http.StatusConflict, "restart_in_progress", err.Error())
	case errors.Is(err, selfcfg.ErrNoChange):
		return httpx.BadRequest("%v", err)
	case errors.Is(err, selfcfg.ErrNoLocation):
		return httpx.Err(http.StatusServiceUnavailable, "config_unsupported", err.Error())
	default:
		// Everything else reaching here is a validation failure with a
		// sentence written for the person reading it, so it is passed through
		// rather than flattened into "internal error".
		return httpx.BadRequest("%v", err)
	}
}

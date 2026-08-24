package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/Wayy01/Just-Dashboard/backend/internal/httpx"
	"github.com/Wayy01/Just-Dashboard/backend/internal/jobs"
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

// handleUpdatesApply starts the upgrade and hands back the job to watch.
//
// This was a request that held the connection open for up to half an hour,
// which is indistinguishable from a broken dashboard — and if the browser gave
// up, the operator was left with no idea whether apt was still running. It is
// a job now: it survives the tab, and the transcript is there to read
// afterwards whether or not anybody watched it happen.
func (s *Server) handleUpdatesApply(w http.ResponseWriter, r *http.Request) error {
	securityOnly := r.URL.Query().Get("security") == "true"
	phrase := "upgrade packages"
	if securityOnly {
		phrase = "install security updates"
	}
	if err := httpx.RequireTypedConfirmation(w, r, phrase); err != nil {
		return err
	}
	name, args, env, err := s.modules.updates.UpgradeCommand(securityOnly)
	if err != nil {
		if errors.Is(err, updates.ErrNotSupported) {
			return httpx.Err(http.StatusServiceUnavailable, "not_installed", err.Error())
		}
		return httpx.BadRequest("%v", err)
	}
	httpx.SetAudit(r, "system.updates.apply", name, map[string]any{
		"securityOnly": securityOnly, "streamed": true,
	})

	title := "Upgrading packages with " + name
	if securityOnly {
		title = "Installing security updates with " + name
	}
	s.startJob(w, r, jobs.Spec{
		Kind: "updates.apply", Title: title, Target: name, Timeout: 2 * time.Hour,
	}, func(ctx context.Context, out jobs.Emitter) error {
		code, err := out.RunEnv(ctx, env, name, args...)
		if err != nil {
			return err
		}
		if code != 0 {
			return fmt.Errorf("%s exited %d — the last lines above say why", name, code)
		}
		// The reboot flag is only meaningful after the packages have landed,
		// and it is the one thing an operator needs to know that the
		// transcript does not say plainly.
		if rep, err := s.modules.updates.Check(ctx); err == nil && rep.RebootRequired {
			out.Status("A restart is required: the running kernel and libraries are still the old ones.")
		}
		return nil
	})
	return nil
}

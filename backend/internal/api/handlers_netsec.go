package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/Wayy01/Just-Dashboard/backend/internal/auth"
	"github.com/Wayy01/Just-Dashboard/backend/internal/httpx"
	"github.com/Wayy01/Just-Dashboard/backend/internal/netsec"
	"github.com/go-chi/chi/v5"
)

func (s *Server) mountNetSecRoutes(r chi.Router) {
	// How the dashboard itself is reachable. Any signed-in principal may read
	// it: knowing you are exposed is not privileged information, and hiding it
	// from a limited role would only delay somebody noticing.
	r.Method(http.MethodGet, "/exposure", s.handle(s.handleExposure))

	r.Route("/firewall", func(r chi.Router) {
		r.Method(http.MethodGet, "/", s.handle(s.handleFirewallStatus))
		r.Group(func(r chi.Router) {
			r.Use(httpx.RequireCapability(auth.CapSystemAdmin))
			r.Method(http.MethodPost, "/rules", s.handle(s.handleFirewallAddRule))
			s.destructive(r, func(r chi.Router) {
				// Turning the firewall off, or deleting the rule that admits
				// you, is how an operator locks themselves out of the box.
				r.Method(http.MethodPost, "/enabled", s.handle(s.handleFirewallToggle))
				r.Method(http.MethodDelete, "/rules/{number}", s.handle(s.handleFirewallDeleteRule))
			})
		})
	})

	r.Route("/fail2ban", func(r chi.Router) {
		r.Method(http.MethodGet, "/", s.handle(s.handleFail2banStatus))
		r.Group(func(r chi.Router) {
			r.Use(httpx.RequireCapability(auth.CapSystemAdmin))
			r.Method(http.MethodPost, "/{jail}/unban", s.handle(s.handleFail2banUnban))
			r.Method(http.MethodPost, "/{jail}/ban", s.handle(s.handleFail2banBan))
		})
	})

	r.Method(http.MethodGet, "/ssh-sessions", s.handle(s.handleSSHSessions))

	r.Route("/logins", func(r chi.Router) {
		r.Method(http.MethodGet, "/", s.handle(s.handleLoginHistory))
		// btmp records whatever was typed at a login prompt, and what people
		// type at a login prompt is sometimes their password in the username
		// field. Successful logins are ordinary operational history; the
		// failed ones are closer to reading somebody's keystrokes, so they
		// need the capability that already implies full access.
		r.Group(func(r chi.Router) {
			r.Use(httpx.RequireCapability(auth.CapSystemAdmin))
			r.Method(http.MethodGet, "/failed", s.handle(s.handleFailedLogins))
		})
	})
}

// handleLoginHistory answers "who got in while I was not watching".
//
// The SSH sessions table only ever showed the logins in progress at the
// instant it was loaded, which is the one moment nobody is worried about. This
// reads the host's own wtmp — the record was always there, the dashboard just
// never showed it.
func (s *Server) handleLoginHistory(w http.ResponseWriter, r *http.Request) error {
	records, err := s.modules.netsec.LoginHistory(r.Context(), loginLimit(r))
	if err != nil {
		return httpx.Err(http.StatusServiceUnavailable, "login_history_unavailable",
			"the host's login record could not be read on this system")
	}
	httpx.JSON(w, http.StatusOK, records)
	return nil
}

func (s *Server) handleFailedLogins(w http.ResponseWriter, r *http.Request) error {
	records, err := s.modules.netsec.FailedLogins(r.Context(), loginLimit(r))
	if err != nil {
		return httpx.Err(http.StatusServiceUnavailable, "login_history_unavailable",
			"the host keeps no failed-login record, or it could not be read")
	}
	httpx.JSON(w, http.StatusOK, records)
	return nil
}

// loginLimit caps the read. btmp on an internet-facing host runs to hundreds
// of thousands of lines, and a table nobody can scroll is not more honest than
// a capped one.
func loginLimit(r *http.Request) int {
	limit := atoiDefault(r.URL.Query().Get("limit"), 100)
	if limit < 1 {
		limit = 1
	}
	if limit > 500 {
		limit = 500
	}
	return limit
}

func (s *Server) handleFirewallStatus(w http.ResponseWriter, r *http.Request) error {
	st, err := s.modules.netsec.Status(r.Context())
	if err != nil {
		return httpx.Internal(err)
	}
	httpx.JSON(w, http.StatusOK, st)
	return nil
}

func (s *Server) handleFirewallAddRule(w http.ResponseWriter, r *http.Request) error {
	var req netsec.RuleRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	// The caller's own address is handed to the firewall layer so it can
	// refuse a rule that would sever this very connection.
	out, err := s.modules.netsec.AddRule(r.Context(), req, httpx.ClientIP(r))
	if err != nil {
		if errors.Is(err, netsec.ErrLockout) {
			httpx.SetAudit(r, "firewall.rule.add", req.Port, map[string]any{"result": "refused_lockout"})
			return httpx.Err(http.StatusConflict, "would_lock_you_out", err.Error())
		}
		return httpx.BadRequest("%v", err)
	}
	httpx.SetAudit(r, "firewall.rule.add", req.Port, req)
	httpx.JSON(w, http.StatusOK, map[string]string{"output": out})
	return nil
}

func (s *Server) handleFirewallDeleteRule(w http.ResponseWriter, r *http.Request) error {
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		return httpx.BadRequest("invalid rule number")
	}
	if err := httpx.RequireTypedConfirmation(w, r, "delete rule "+strconv.Itoa(number)); err != nil {
		return err
	}
	out, err := s.modules.netsec.DeleteRule(r.Context(), number)
	if err != nil {
		return httpx.BadRequest("%v", err)
	}
	httpx.SetAudit(r, "firewall.rule.delete", strconv.Itoa(number), nil)
	httpx.JSON(w, http.StatusOK, map[string]string{"output": out})
	return nil
}

type firewallToggleRequest struct {
	Enabled bool `json:"enabled"`
}

func (s *Server) handleFirewallToggle(w http.ResponseWriter, r *http.Request) error {
	var req firewallToggleRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	// Enabling ufw applies its default-deny policy immediately; if the
	// dashboard's own port is not already allowed, that is a lockout.
	phrase := "disable firewall"
	if req.Enabled {
		phrase = "enable firewall"
	}
	if err := httpx.RequireTypedConfirmation(w, r, phrase); err != nil {
		return err
	}
	out, err := s.modules.netsec.SetEnabled(r.Context(), req.Enabled)
	if err != nil {
		return httpx.BadRequest("%v", err)
	}
	httpx.SetAudit(r, "firewall.toggle", "", map[string]any{"enabled": req.Enabled})
	httpx.JSON(w, http.StatusOK, map[string]string{"output": out})
	return nil
}

func (s *Server) handleFail2banStatus(w http.ResponseWriter, r *http.Request) error {
	st, err := s.modules.netsec.Fail2banStatus(r.Context())
	if err != nil {
		return httpx.Internal(err)
	}
	httpx.JSON(w, http.StatusOK, st)
	return nil
}

type banRequest struct {
	IP string `json:"ip"`
}

func (s *Server) handleFail2banUnban(w http.ResponseWriter, r *http.Request) error {
	var req banRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	jail := chi.URLParam(r, "jail")
	out, err := s.modules.netsec.Unban(r.Context(), jail, req.IP)
	if err != nil {
		return httpx.BadRequest("%v", err)
	}
	httpx.SetAudit(r, "fail2ban.unban", jail, map[string]any{"ip": req.IP})
	httpx.JSON(w, http.StatusOK, map[string]string{"output": out})
	return nil
}

func (s *Server) handleFail2banBan(w http.ResponseWriter, r *http.Request) error {
	var req banRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	jail := chi.URLParam(r, "jail")
	out, err := s.modules.netsec.Ban(r.Context(), jail, req.IP)
	if err != nil {
		return httpx.BadRequest("%v", err)
	}
	httpx.SetAudit(r, "fail2ban.ban", jail, map[string]any{"ip": req.IP})
	httpx.JSON(w, http.StatusOK, map[string]string{"output": out})
	return nil
}

func (s *Server) handleSSHSessions(w http.ResponseWriter, r *http.Request) error {
	sessions, err := s.modules.netsec.Sessions(r.Context())
	if err != nil {
		return httpx.Internal(err)
	}
	httpx.JSON(w, http.StatusOK, sessions)
	return nil
}

// handleExposure reports who can reach this dashboard, graded. It reads the
// allowlist the process actually booted with rather than re-reading a file, so
// it describes the running configuration and not an edited one that has yet to
// be applied.
func (s *Server) handleExposure(w http.ResponseWriter, r *http.Request) error {
	httpx.JSON(w, http.StatusOK, netsec.DescribeExposure(s.Cfg.AllowedCIDRs))
	return nil
}

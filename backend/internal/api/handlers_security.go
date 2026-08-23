package api

import (
	"errors"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/Wayy01/Just-Dashboard/backend/internal/auth"
	"github.com/Wayy01/Just-Dashboard/backend/internal/httpx"
	"github.com/Wayy01/Just-Dashboard/backend/internal/netsec"
	"github.com/Wayy01/Just-Dashboard/backend/internal/proxysvc"
	"github.com/go-chi/chi/v5"
)

// The second half of the Security page: the verdict, SSH, the connection
// table and the diagnostic tools.
//
// Mounted from mountNetSecRoutes rather than from Routes, so the whole
// security surface still has one place to be read — the same arrangement
// handlers_docker_manage.go has with Docker.
func (s *Server) mountSecurityRoutes(r chi.Router) {
	// The posture verdict and the service catalogue are readable by any
	// signed-in principal, for the reason the exposure grade is: knowing the
	// machine is badly configured is not privileged information, and hiding
	// it from a limited role only delays somebody noticing.
	r.Method(http.MethodGet, "/security/posture", s.handle(s.handleSecurityPosture))
	r.Method(http.MethodGet, "/security/services", s.handle(s.handleServiceCatalogue))
	r.Method(http.MethodGet, "/connections", s.handle(s.handleConnections))
	r.Method(http.MethodGet, "/network", s.handle(s.handleNetworkInfo))

	// Ending somebody's session is destructive and rare — a handful of times
	// a year, not a dozen a day — so it takes a typed phrase by the same
	// frequency test the terminal close routes fail.
	r.Route("/ssh-sessions", func(r chi.Router) {
		r.Use(httpx.RequireCapability(auth.CapSystemAdmin))
		s.destructive(r, func(r chi.Router) {
			r.Method(http.MethodPost, "/{pid}/disconnect", s.handle(s.handleDisconnectSession))
		})
	})

	r.Route("/ssh", func(r chi.Router) {
		// The SSH configuration names the accounts that hold keys, which is a
		// map of who can reach this machine. That belongs with the capability
		// that already implies full access.
		r.Use(httpx.RequireCapability(auth.CapSystemAdmin))
		r.Method(http.MethodGet, "/config", s.handle(s.handleSSHConfig))
		s.destructive(r, func(r chi.Router) {
			// Not destructive in the sense of losing data. Destructive in the
			// sense the marker exists for: get it wrong and the way back into
			// the machine is gone, and no amount of undo in this UI helps
			// because reaching this UI is what you have lost.
			r.Method(http.MethodPost, "/config", s.handle(s.handleSSHApply))
		})
	})

	// Probes make the server emit traffic to an address the caller chose.
	// That is a scanner if it is handed to everybody, so it sits behind the
	// capability that already means "this person administers the host".
	r.Route("/network/probe", func(r chi.Router) {
		r.Use(httpx.RequireCapability(auth.CapSystemAdmin))
		r.Method(http.MethodPost, "/", s.handle(s.handleNetworkProbe))
	})
}

// handleSecurityPosture gathers every input and grades the host.
//
// The gathering is concurrent because each piece is a subprocess or a file
// read on a host that may be busy, and seven of them in sequence is the
// difference between a page that fills in and one that appears to hang. The
// grading itself is a pure function of what came back, which is what makes the
// rules testable without a firewall or an sshd.
func (s *Server) handleSecurityPosture(w http.ResponseWriter, r *http.Request) error {
	ctx, cancel := timeoutCtx(r, 30*time.Second)
	defer cancel()

	in := netsec.AssessInput{Now: time.Now()}
	in.Exposure = ptr(netsec.DescribeExposure(s.Cfg.AllowedCIDRs))

	var wg sync.WaitGroup
	run := func(fn func()) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fn()
		}()
	}
	run(func() {
		if st, err := s.modules.netsec.Status(ctx); err == nil {
			in.Firewall = st
		}
	})
	run(func() {
		if st, err := s.modules.netsec.Fail2banStatus(ctx); err == nil {
			in.Fail2ban = st
		}
	})
	run(func() { in.SSH = s.modules.netsec.SSHDStatus(ctx) })
	run(func() {
		listeners, err := proxysvc.ListListeners(ctx)
		if err != nil {
			return
		}
		for _, l := range listeners {
			in.Listeners = append(in.Listeners, netsec.ExposedPort{
				Port: l.Port, Protocol: l.Protocol, Address: l.Address,
				Process: l.Process, Exposed: l.Exposed,
			})
		}
	})
	run(func() {
		certs, err := s.modules.proxy.ListCertificates(ctx)
		if err != nil {
			return
		}
		for _, c := range certs {
			if c.Error != "" {
				continue
			}
			in.Certificates = append(in.Certificates, netsec.CertSummary{
				Name: c.Name, DaysLeft: c.DaysLeft, Expired: c.Expired,
			})
		}
	})
	run(func() {
		// Failed logins are the volume of attempts, not their content, so the
		// count is fine to feed a verdict every role can read even though the
		// btmp listing itself is admin-only.
		if records, err := s.modules.netsec.FailedLogins(ctx, 500); err == nil {
			in.FailedLogins = len(records)
		}
	})
	run(func() {
		if events, err := s.modules.netsec.BanHistory(ctx, 500); err == nil {
			in.RecentBans = len(events)
		}
	})
	run(func() {
		if report, err := s.modules.updates.Check(ctx); err == nil {
			in.SecurityUpdates = report.SecurityCount
			in.RebootRequired = report.RebootRequired
		}
	})
	wg.Wait()

	httpx.JSON(w, http.StatusOK, netsec.Assess(in))
	return nil
}

func ptr[T any](v T) *T { return &v }

// handleServiceCatalogue hands the port catalogue to the rule form, so the
// names and the warnings are the server's list rather than a second copy in
// TypeScript that would drift from the one the audit reads.
func (s *Server) handleServiceCatalogue(w http.ResponseWriter, r *http.Request) error {
	httpx.JSON(w, http.StatusOK, netsec.ServiceCatalogue)
	return nil
}

func (s *Server) handleFirewallApps(w http.ResponseWriter, r *http.Request) error {
	profiles, err := s.modules.netsec.AppProfiles(r.Context())
	if err != nil {
		// A host without ufw has no profiles, which is information rather
		// than a failure.
		httpx.JSON(w, http.StatusOK, []netsec.AppProfile{})
		return nil
	}
	httpx.JSON(w, http.StatusOK, profiles)
	return nil
}

type firewallPolicyRequest struct {
	Direction string `json:"direction"`
	Policy    string `json:"policy"`
}

func (s *Server) handleFirewallPolicy(w http.ResponseWriter, r *http.Request) error {
	var req firewallPolicyRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	// Only the inbound default can cut the caller off, and only when it stops
	// being allow. The others change what the host may do, not who may reach
	// it, and a phrase in front of them would be the fourth exception the
	// confirmation rule warns about.
	if req.Direction == "incoming" && req.Policy != "allow" {
		if err := httpx.RequireTypedConfirmation(w, r, "deny incoming"); err != nil {
			return err
		}
	}
	out, err := s.modules.netsec.SetDefaultPolicy(r.Context(), req.Direction, req.Policy)
	if err != nil {
		if errors.Is(err, netsec.ErrLockout) {
			httpx.SetAudit(r, "firewall.policy", req.Direction, map[string]any{"result": "refused_lockout"})
			return httpx.Err(http.StatusConflict, "would_lock_you_out", err.Error())
		}
		return mapFirewallError(err)
	}
	httpx.SetAudit(r, "firewall.policy", req.Direction, map[string]any{"policy": req.Policy})
	httpx.JSON(w, http.StatusOK, map[string]string{"output": out})
	return nil
}

type firewallLoggingRequest struct {
	Level string `json:"level"`
}

func (s *Server) handleFirewallLogging(w http.ResponseWriter, r *http.Request) error {
	var req firewallLoggingRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	out, err := s.modules.netsec.SetLogging(r.Context(), req.Level)
	if err != nil {
		return mapFirewallError(err)
	}
	httpx.SetAudit(r, "firewall.logging", req.Level, nil)
	httpx.JSON(w, http.StatusOK, map[string]string{"output": out})
	return nil
}

func (s *Server) handleFirewallReset(w http.ResponseWriter, r *http.Request) error {
	if err := httpx.RequireTypedConfirmation(w, r, "reset firewall"); err != nil {
		return err
	}
	out, err := s.modules.netsec.Reset(r.Context())
	if err != nil {
		return mapFirewallError(err)
	}
	httpx.SetAudit(r, "firewall.reset", "", nil)
	httpx.JSON(w, http.StatusOK, map[string]string{"output": out})
	return nil
}

func (s *Server) handleSSHConfig(w http.ResponseWriter, r *http.Request) error {
	httpx.JSON(w, http.StatusOK, s.modules.netsec.SSHDStatus(r.Context()))
	return nil
}

type sshApplyRequest struct {
	Settings map[string]string `json:"settings"`
}

func (s *Server) handleSSHApply(w http.ResponseWriter, r *http.Request) error {
	var req sshApplyRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if err := httpx.RequireTypedConfirmation(w, r, "change ssh"); err != nil {
		return err
	}
	res, err := s.modules.netsec.ApplySSHSettings(r.Context(), req.Settings)
	if err != nil {
		if errors.Is(err, netsec.ErrLockout) {
			httpx.SetAudit(r, "ssh.config", "", map[string]any{"result": "refused_lockout"})
			return httpx.Err(http.StatusConflict, "would_lock_you_out", err.Error())
		}
		if errors.Is(err, netsec.ErrSSHInvalid) {
			httpx.SetAudit(r, "ssh.config", "", map[string]any{"result": "rejected"})
			return httpx.Err(http.StatusUnprocessableEntity, "invalid_config", res.Output)
		}
		return httpx.BadRequest("%v", err)
	}
	httpx.SetAudit(r, "ssh.config", res.File, map[string]any{
		"applied": res.Applied, "reloaded": res.Reloaded,
	})
	httpx.JSON(w, http.StatusOK, res)
	return nil
}

// handleDisconnectSession ends one interactive login.
//
// The PID is checked against the live session list inside netsec rather than
// here, because that check is what stops this being a "kill any process"
// route wearing a sensible name.
func (s *Server) handleDisconnectSession(w http.ResponseWriter, r *http.Request) error {
	pid, err := strconv.Atoi(chi.URLParam(r, "pid"))
	if err != nil {
		return httpx.BadRequest("invalid process id")
	}
	if err := httpx.RequireTypedConfirmation(w, r, "disconnect "+strconv.Itoa(pid)); err != nil {
		return err
	}
	session, err := s.modules.netsec.Disconnect(r.Context(), int32(pid))
	if err != nil {
		return httpx.BadRequest("%v", err)
	}
	httpx.SetAudit(r, "ssh.session.disconnect", session.User,
		map[string]any{"pid": pid, "tty": session.TTY, "from": session.From})
	httpx.JSON(w, http.StatusOK, session)
	return nil
}

func (s *Server) handleConnections(w http.ResponseWriter, r *http.Request) error {
	conns, err := s.modules.netsec.Connections(r.Context())
	if err != nil {
		return httpx.Err(http.StatusServiceUnavailable, "connections_unavailable",
			"the host's connection table could not be read")
	}
	httpx.JSON(w, http.StatusOK, conns)
	return nil
}

func (s *Server) handleNetworkInfo(w http.ResponseWriter, r *http.Request) error {
	info, err := s.modules.netsec.NetworkInfo(r.Context())
	if err != nil {
		return httpx.Internal(err)
	}
	httpx.JSON(w, http.StatusOK, info)
	return nil
}

type probeRequest struct {
	Tool   string `json:"tool"`
	Target string `json:"target"`
	Port   int    `json:"port,omitempty"`
	Record string `json:"record,omitempty"`
}

func (s *Server) handleNetworkProbe(w http.ResponseWriter, r *http.Request) error {
	var req probeRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	ctx, cancel := timeoutCtx(r, 90*time.Second)
	defer cancel()

	var res *netsec.ProbeResult
	var err error
	switch req.Tool {
	case "ping":
		res, err = s.modules.netsec.Ping(ctx, req.Target)
	case "traceroute":
		res, err = s.modules.netsec.Traceroute(ctx, req.Target)
	case "dns":
		res, err = s.modules.netsec.Lookup(ctx, req.Target, req.Record)
	case "port":
		res, err = s.modules.netsec.PortCheck(ctx, req.Target, req.Port)
	default:
		return httpx.BadRequest("tool must be ping, traceroute, dns or port")
	}
	if err != nil {
		return httpx.BadRequest("%v", err)
	}
	httpx.SetAudit(r, "network.probe", req.Tool, map[string]any{"target": req.Target})
	httpx.JSON(w, http.StatusOK, res)
	return nil
}

func (s *Server) handleJailConfig(w http.ResponseWriter, r *http.Request) error {
	cfg, err := s.modules.netsec.JailConfig(r.Context(), chi.URLParam(r, "jail"))
	if err != nil {
		return httpx.BadRequest("%v", err)
	}
	httpx.JSON(w, http.StatusOK, cfg)
	return nil
}

type jailParamRequest struct {
	// Params carries every parameter at once, because they are one policy —
	// "this many failures inside this window earns this long a ban" — and
	// applying them one request at a time leaves a jail half-tuned if the tab
	// closes in between.
	Params map[string]int `json:"params"`
	// The single-parameter form the first version of this route took, kept so
	// a scripted caller does not break.
	Param string `json:"param,omitempty"`
	Value int    `json:"value,omitempty"`
}

func (s *Server) handleJailParam(w http.ResponseWriter, r *http.Request) error {
	var req jailParamRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	params := req.Params
	if len(params) == 0 && req.Param != "" {
		params = map[string]int{req.Param: req.Value}
	}
	jail := chi.URLParam(r, "jail")
	res, err := s.modules.netsec.SetJailParams(r.Context(), jail, params)
	if err != nil {
		return httpx.BadRequest("%v", err)
	}
	httpx.SetAudit(r, "fail2ban.param", jail, map[string]any{
		"params": params, "persisted": res.Persisted,
	})
	httpx.JSON(w, http.StatusOK, res)
	return nil
}

type jailIgnoreRequest struct {
	IP  string `json:"ip"`
	Add bool   `json:"add"`
}

func (s *Server) handleJailIgnore(w http.ResponseWriter, r *http.Request) error {
	var req jailIgnoreRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	jail := chi.URLParam(r, "jail")
	out, err := s.modules.netsec.IgnoreIP(r.Context(), jail, req.IP, req.Add)
	if err != nil {
		return httpx.BadRequest("%v", err)
	}
	httpx.SetAudit(r, "fail2ban.ignore", jail, map[string]any{"ip": req.IP, "add": req.Add})
	httpx.JSON(w, http.StatusOK, map[string]string{"output": out})
	return nil
}

func (s *Server) handleJailUnbanAll(w http.ResponseWriter, r *http.Request) error {
	jail := chi.URLParam(r, "jail")
	count, err := s.modules.netsec.UnbanAll(r.Context(), jail)
	if err != nil {
		return httpx.BadRequest("%v", err)
	}
	httpx.SetAudit(r, "fail2ban.unban.all", jail, map[string]any{"count": count})
	httpx.JSON(w, http.StatusOK, map[string]int{"unbanned": count})
	return nil
}

// handleBanOffenders answers "who keeps coming back", which the jail's own
// list cannot: a ban expires, so the address banned eleven times this week is
// invisible the moment its eleventh ban lapses.
func (s *Server) handleBanOffenders(w http.ResponseWriter, r *http.Request) error {
	events, err := s.modules.netsec.BanHistory(r.Context(), 2000)
	if err != nil {
		return httpx.Internal(err)
	}
	if len(events) == 0 && !s.modules.netsec.Fail2banAvailable() {
		return httpx.Err(http.StatusServiceUnavailable, "fail2ban_unavailable",
			"fail2ban is not installed on this host")
	}
	top := atoiDefault(r.URL.Query().Get("top"), 10)
	if top < 1 || top > 100 {
		top = 10
	}
	httpx.JSON(w, http.StatusOK, netsec.SummariseBans(events, top))
	return nil
}

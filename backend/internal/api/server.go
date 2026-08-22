package api

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/Wayy01/Just-Dashboard/backend/internal/agent"
	"github.com/Wayy01/Just-Dashboard/backend/internal/audit"
	"github.com/Wayy01/Just-Dashboard/backend/internal/auth"
	"github.com/Wayy01/Just-Dashboard/backend/internal/config"
	"github.com/Wayy01/Just-Dashboard/backend/internal/httpx"
	"github.com/Wayy01/Just-Dashboard/backend/internal/store"
	"github.com/Wayy01/Just-Dashboard/backend/internal/wsx"
)

// Server owns the dependency graph the handlers close over. Feature modules
// are attached as fields so a module that cannot initialise on this host
// (no Docker socket, no systemd) degrades to a clear error on its own routes
// instead of preventing the dashboard from starting.
type Server struct {
	Cfg    *config.Config
	Log    *slog.Logger
	Store  *store.Store
	Auth   *auth.Service
	Sealer *auth.Sealer
	Audit  *audit.Logger
	Authn  *httpx.Authenticator
	WS     *wsx.Upgrader
	// Agent is non-nil only in agent mode, where it is both the TLS identity
	// and the record of which hub this server answers to.
	Agent    *agent.Identity
	loginLim *httpx.Limiter
	apiLim   *httpx.Limiter
	destrLim *httpx.Limiter

	modules moduleSet
}

func New(cfg *config.Config, log *slog.Logger, st *store.Store, svc *auth.Service, sealer *auth.Sealer, aud *audit.Logger, id *agent.Identity) *Server {
	s := &Server{
		Cfg:    cfg,
		Log:    log,
		Store:  st,
		Auth:   svc,
		Sealer: sealer,
		Audit:  aud,
		Agent:  id,
		Authn:  &httpx.Authenticator{Svc: svc, Secure: !cfg.Dev},
		WS:     wsx.NewUpgrader(cfg.AllowedOrigins),
		// Login is deliberately tight: five attempts a minute per address on
		// top of the per-account lockout.
		loginLim: httpx.NewLimiter(10, 5),
		apiLim:   httpx.NewLimiter(600, 120),
		// Destructive routes get their own budget so a scripted delete loop
		// cannot run away even with a valid admin token.
		destrLim: httpx.NewLimiter(30, 10),
	}
	s.initModules()
	return s
}

func (s *Server) handle(fn httpx.Handler) http.Handler { return fn }

// Start brings up the background workers the API owns. It is separate from
// New so that a failure to schedule backups is reported by main rather than
// swallowed during construction.
func (s *Server) Start(ctx context.Context) error {
	// The metrics recorder is started here rather than lazily on the first
	// request precisely because nothing may ever request it: its whole
	// purpose is to have been running while nobody was looking.
	if err := s.modules.metrics.Start(ctx); err != nil {
		return err
	}
	// The Docker event log is started for the same reason and never fails:
	// Docker's stream is the only record of an OOM kill or a health check
	// going red, and the daemon keeps none of it. A host with no Docker
	// simply never connects, which is a steady state rather than an error.
	s.modules.dockerEvents.Start(ctx)
	return s.modules.backupSched.Start(ctx)
}

// Shutdown releases the resources that outlive a request: database pools,
// live PTY sessions, the metrics sampler and the backup scheduler.
func (s *Server) Shutdown() {
	s.modules.metrics.Stop()
	s.modules.backupSched.Stop()
	s.modules.term.Shutdown()
	s.modules.dbs.Shutdown()
	s.modules.docker.Close()
}

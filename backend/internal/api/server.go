package api

import (
	"log/slog"
	"net/http"

	"github.com/Wayy01/vps-dashboard/backend/internal/audit"
	"github.com/Wayy01/vps-dashboard/backend/internal/auth"
	"github.com/Wayy01/vps-dashboard/backend/internal/config"
	"github.com/Wayy01/vps-dashboard/backend/internal/httpx"
	"github.com/Wayy01/vps-dashboard/backend/internal/store"
	"github.com/Wayy01/vps-dashboard/backend/internal/wsx"
)

// Server owns the dependency graph the handlers close over. Feature modules
// are attached as fields so a module that cannot initialise on this host
// (no Docker socket, no systemd) degrades to a clear error on its own routes
// instead of preventing the dashboard from starting.
type Server struct {
	Cfg      *config.Config
	Log      *slog.Logger
	Store    *store.Store
	Auth     *auth.Service
	Sealer   *auth.Sealer
	Audit    *audit.Logger
	Authn    *httpx.Authenticator
	WS       *wsx.Upgrader
	loginLim *httpx.Limiter
	apiLim   *httpx.Limiter
	destrLim *httpx.Limiter

	modules moduleSet
}

func New(cfg *config.Config, log *slog.Logger, st *store.Store, svc *auth.Service, sealer *auth.Sealer, aud *audit.Logger) *Server {
	s := &Server{
		Cfg:    cfg,
		Log:    log,
		Store:  st,
		Auth:   svc,
		Sealer: sealer,
		Audit:  aud,
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

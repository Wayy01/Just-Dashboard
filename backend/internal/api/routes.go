package api

import (
	"net/http"

	"github.com/Wayy01/vps-dashboard/backend/internal/auth"
	"github.com/Wayy01/vps-dashboard/backend/internal/httpx"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Routes builds the whole API surface.
//
// The layering is deliberate and is the security contract of this service:
//
//	network allowlist  →  rate limit  →  authentication  →  capability  →  handler
//
// Nothing below /api is reachable without passing every layer above it. There
// is no "UI-only" protection anywhere: the frontend hides controls a role
// cannot use, but the server re-decides every request on its own.
func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(httpx.RealIP(s.Cfg.TrustedProxies))
	r.Use(httpx.Recoverer(s.Log))
	r.Use(httpx.RequestLogger(s.Log))
	r.Use(httpx.SecurityHeaders)

	// Liveness is the only unauthenticated route, and it reveals nothing:
	// a fixed body, no version, no hostname.
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(httpx.AllowlistCIDRs(s.Cfg.AllowedCIDRs, s.Log))

		// Deploy webhooks authenticate with their own per-project HMAC
		// signature rather than a dashboard session, since they are called
		// by CI. They still sit behind the network allowlist.
		r.Route("/hooks", func(r chi.Router) {
			r.Use(s.apiLim.Middleware)
			r.Method(http.MethodPost, "/deploy/{hookID}", s.handle(s.handleDeployWebhook))
		})

		r.Group(func(r chi.Router) {
			r.Use(s.loginLim.Middleware)
			r.Use(httpx.AuditMutations(s.Audit))
			r.Method(http.MethodPost, "/auth/login", s.handle(s.handleLogin))
		})

		// Half-authenticated: password proved, second factor outstanding.
		r.Group(func(r chi.Router) {
			r.Use(s.Authn.AuthenticatePartial)
			r.Use(httpx.AuditMutations(s.Audit))
			r.Method(http.MethodGet, "/auth/session", s.handle(s.handleSession))
			r.Method(http.MethodPost, "/auth/2fa/setup", s.handle(s.handleTOTPSetup))
			r.Method(http.MethodPost, "/auth/2fa/enable", s.handle(s.handleTOTPEnable))
			r.Method(http.MethodPost, "/auth/2fa/verify", s.handle(s.handleTOTPVerify))
			r.Method(http.MethodPost, "/auth/logout", s.handle(s.handleLogout))
		})

		// Fully authenticated surface.
		r.Group(func(r chi.Router) {
			r.Use(s.Authn.Authenticate)
			r.Use(s.apiLim.ByPrincipal)
			r.Use(httpx.AuditMutations(s.Audit))

			s.mountAccountRoutes(r)
			s.mountSystemRoutes(r)
			s.mountDockerRoutes(r)
			s.mountProcessRoutes(r)
			s.mountLogRoutes(r)
			s.mountTerminalRoutes(r)
			s.mountFileRoutes(r)
			s.mountGitRoutes(r)
			s.mountUpdateRoutes(r)
			s.mountProxyRoutes(r)
			s.mountDatabaseRoutes(r)
			s.mountLinuxUserRoutes(r)
			s.mountNetSecRoutes(r)
			s.mountBackupRoutes(r)
			s.mountDeployRoutes(r)
		})
	})
	return r
}

func (s *Server) mountAccountRoutes(r chi.Router) {
	r.Route("/account", func(r chi.Router) {
		r.Use(httpx.RequireSession)
		r.Method(http.MethodPost, "/password", s.handle(s.handleChangePassword))
		r.Method(http.MethodPost, "/recovery-codes", s.handle(s.handleRecoveryCodesRegen))
		r.Method(http.MethodGet, "/sessions", s.handle(s.handleListOwnSessions))
		r.Method(http.MethodDelete, "/sessions/{id}", s.handle(s.handleRevokeOwnSession))
	})

	r.Route("/tokens", func(r chi.Router) {
		r.Method(http.MethodGet, "/", s.handle(s.handleListTokens))
		r.Group(func(r chi.Router) {
			r.Use(httpx.RequireSession)
			r.Method(http.MethodPost, "/", s.handle(s.handleCreateToken))
			r.Method(http.MethodDelete, "/{id}", s.handle(s.handleRevokeToken))
		})
	})

	r.Route("/dashboard-users", func(r chi.Router) {
		r.Use(httpx.RequireCapability(auth.CapSystemAdmin))
		r.Use(httpx.RequireSession)
		r.Method(http.MethodGet, "/", s.handle(s.handleListUsers))
		r.Method(http.MethodPost, "/", s.handle(s.handleCreateUser))
		r.Method(http.MethodPatch, "/{id}", s.handle(s.handleUpdateUser))
		r.Method(http.MethodPost, "/{id}/reset-totp", s.handle(s.handleResetUserTOTP))
		r.Group(func(r chi.Router) {
			r.Use(s.destrLim.ByPrincipal)
			r.Method(http.MethodDelete, "/{id}", s.handle(s.handleDeleteUser))
		})
	})

	r.Route("/audit", func(r chi.Router) {
		r.Use(httpx.RequireCapability(auth.CapSystemAdmin))
		r.Method(http.MethodGet, "/", s.handle(s.handleAuditList))
	})
}

// destructive wraps routes whose effects cannot be undone. They get a tighter
// rate budget on top of the capability check; the typed-confirmation phrase is
// enforced inside each handler, where the expected phrase is known.
func (s *Server) destructive(r chi.Router, fn func(chi.Router)) {
	r.Group(func(r chi.Router) {
		r.Use(httpx.RequireCapability(auth.CapDestructive))
		r.Use(s.destrLim.ByPrincipal)
		fn(r)
	})
}

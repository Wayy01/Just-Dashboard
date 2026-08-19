package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/Wayy01/Just-Dashboard/backend/internal/auth"
	"github.com/Wayy01/Just-Dashboard/backend/internal/httpx"
	"github.com/Wayy01/Just-Dashboard/backend/internal/proxysvc"
	"github.com/go-chi/chi/v5"
)

func (s *Server) mountProxyRoutes(r chi.Router) {
	r.Route("/proxy", func(r chi.Router) {
		r.Method(http.MethodGet, "/status", s.handle(s.handleProxyStatus))
		r.Method(http.MethodGet, "/vhosts", s.handle(s.handleVHostList))
		r.Method(http.MethodGet, "/config", s.handle(s.handleProxyConfigRead))
		r.Method(http.MethodPost, "/validate", s.handle(s.handleProxyValidate))
		r.Group(func(r chi.Router) {
			r.Use(httpx.RequireCapability(auth.CapSystemAdmin))
			r.Method(http.MethodPut, "/config", s.handle(s.handleProxyConfigWrite))
			r.Method(http.MethodPost, "/vhosts/{name}/enabled", s.handle(s.handleVHostToggle))
			r.Method(http.MethodPost, "/reload", s.handle(s.handleProxyReload))
		})
	})

	r.Route("/certificates", func(r chi.Router) {
		r.Method(http.MethodGet, "/", s.handle(s.handleCertList))
		r.Method(http.MethodGet, "/check", s.handle(s.handleCertCheck))
		r.Method(http.MethodGet, "/certbot", s.handle(s.handleCertbot))
		r.Method(http.MethodGet, "/watched", s.handle(s.handleWatchedDomains))
		r.Group(func(r chi.Router) {
			r.Use(httpx.RequireCapability(auth.CapSystemAdmin))
			r.Method(http.MethodPost, "/watched", s.handle(s.handleWatchDomain))
			r.Method(http.MethodDelete, "/watched/{id}", s.handle(s.handleUnwatchDomain))
		})
	})

	r.Method(http.MethodGet, "/ports", s.handle(s.handlePortList))
}

func (s *Server) handleProxyStatus(w http.ResponseWriter, r *http.Request) error {
	httpx.JSON(w, http.StatusOK, s.modules.proxy.Availability(r.Context()))
	return nil
}

func (s *Server) handleVHostList(w http.ResponseWriter, r *http.Request) error {
	hosts, err := s.modules.proxy.ListVHosts(r.Context())
	if err != nil {
		return httpx.Internal(err)
	}
	httpx.JSON(w, http.StatusOK, hosts)
	return nil
}

func (s *Server) handleProxyConfigRead(w http.ResponseWriter, r *http.Request) error {
	path := r.URL.Query().Get("path")
	if path == "" {
		return httpx.BadRequest("path query parameter is required")
	}
	content, err := s.modules.proxy.ReadConfig(path)
	if err != nil {
		return mapProxyError(err)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"path": path, "content": content})
	return nil
}

type proxyConfigRequest struct {
	Kind    proxysvc.Kind `json:"kind"`
	Path    string        `json:"path"`
	Content string        `json:"content"`
	Reload  bool          `json:"reload"`
}

// handleProxyValidate is a dry run: it tells the operator whether a config
// would be accepted without changing what is currently serving traffic.
func (s *Server) handleProxyValidate(w http.ResponseWriter, r *http.Request) error {
	var req proxyConfigRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	res, err := s.modules.proxy.Validate(r.Context(), req.Kind, req.Path, req.Content)
	if err != nil {
		return mapProxyError(err)
	}
	httpx.SkipAudit(r)
	httpx.JSON(w, http.StatusOK, res)
	return nil
}

func (s *Server) handleProxyConfigWrite(w http.ResponseWriter, r *http.Request) error {
	var req proxyConfigRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	res, err := s.modules.proxy.WriteConfig(r.Context(), req.Kind, req.Path, req.Content)
	if err != nil {
		if errors.Is(err, proxysvc.ErrInvalidConf) {
			httpx.SetAudit(r, "proxy.config.write", req.Path, map[string]any{"result": "rejected"})
			return httpx.Err(http.StatusUnprocessableEntity, "invalid_config", res.Output)
		}
		return mapProxyError(err)
	}
	out := map[string]any{"validation": res}
	if req.Reload {
		reload, err := s.modules.proxy.Reload(r.Context(), req.Kind)
		out["reload"] = reload
		if err != nil {
			httpx.SetAudit(r, "proxy.config.write", req.Path, map[string]any{"reloaded": false})
			return httpx.Err(http.StatusBadGateway, "reload_failed", err.Error())
		}
	}
	httpx.SetAudit(r, "proxy.config.write", req.Path,
		map[string]any{"kind": req.Kind, "reloaded": req.Reload, "bytes": len(req.Content)})
	httpx.JSON(w, http.StatusOK, out)
	return nil
}

type vhostToggleRequest struct {
	Enabled bool `json:"enabled"`
	Reload  bool `json:"reload"`
}

func (s *Server) handleVHostToggle(w http.ResponseWriter, r *http.Request) error {
	name := chi.URLParam(r, "name")
	var req vhostToggleRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	// Disabling a vhost takes a site offline, so it is confirmed by name the
	// same way a container stop is.
	if !req.Enabled {
		if err := httpx.RequireTypedConfirmation(w, r, name); err != nil {
			return err
		}
	}
	if err := s.modules.proxy.SetVHostEnabled(r.Context(), name, req.Enabled); err != nil {
		return mapProxyError(err)
	}
	out := map[string]any{"name": name, "enabled": req.Enabled}
	if req.Reload {
		reload, err := s.modules.proxy.Reload(r.Context(), proxysvc.KindNginx)
		out["reload"] = reload
		if err != nil {
			// The symlink change is already applied; report the reload
			// failure rather than pretending the toggle did not happen.
			httpx.SetAudit(r, "proxy.vhost.toggle", name,
				map[string]any{"enabled": req.Enabled, "reloadError": err.Error()})
			return httpx.Err(http.StatusBadGateway, "reload_failed", err.Error())
		}
	}
	httpx.SetAudit(r, "proxy.vhost.toggle", name, map[string]any{"enabled": req.Enabled})
	httpx.JSON(w, http.StatusOK, out)
	return nil
}

type reloadRequest struct {
	Kind proxysvc.Kind `json:"kind"`
}

func (s *Server) handleProxyReload(w http.ResponseWriter, r *http.Request) error {
	var req reloadRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if req.Kind == "" {
		req.Kind = proxysvc.KindNginx
	}
	res, err := s.modules.proxy.Reload(r.Context(), req.Kind)
	if err != nil {
		httpx.SetAudit(r, "proxy.reload", string(req.Kind), map[string]any{"result": "failed"})
		if errors.Is(err, proxysvc.ErrInvalidConf) {
			return httpx.Err(http.StatusUnprocessableEntity, "invalid_config", res.Validation.Output)
		}
		return httpx.Err(http.StatusBadGateway, "reload_failed", err.Error())
	}
	httpx.SetAudit(r, "proxy.reload", string(req.Kind), nil)
	httpx.JSON(w, http.StatusOK, res)
	return nil
}

func mapProxyError(err error) error {
	switch {
	case errors.Is(err, proxysvc.ErrUnsafePath):
		return httpx.Err(http.StatusForbidden, "outside_root", err.Error())
	case errors.Is(err, proxysvc.ErrNoProxy):
		return httpx.Err(http.StatusServiceUnavailable, "no_proxy", err.Error())
	default:
		return httpx.BadRequest("%v", err)
	}
}

func (s *Server) handleCertList(w http.ResponseWriter, r *http.Request) error {
	certs, err := s.modules.proxy.ListCertificates(r.Context())
	if err != nil {
		return httpx.Internal(err)
	}
	httpx.JSON(w, http.StatusOK, certs)
	return nil
}

func (s *Server) handleCertCheck(w http.ResponseWriter, r *http.Request) error {
	domain := r.URL.Query().Get("domain")
	if domain == "" {
		return httpx.BadRequest("domain query parameter is required")
	}
	ctx, cancel := timeoutCtx(r, 20*time.Second)
	defer cancel()
	cert, err := proxysvc.CheckDomain(ctx, domain, atoiDefault(r.URL.Query().Get("port"), 443))
	if err != nil {
		return httpx.BadRequest("%v", err)
	}
	httpx.JSON(w, http.StatusOK, cert)
	return nil
}

func (s *Server) handleCertbot(w http.ResponseWriter, r *http.Request) error {
	out, err := proxysvc.CertbotCertificates(r.Context())
	if err != nil {
		return httpx.Err(http.StatusServiceUnavailable, "certbot_unavailable", err.Error())
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"output": out})
	return nil
}

func (s *Server) handlePortList(w http.ResponseWriter, r *http.Request) error {
	listeners, err := proxysvc.ListListeners(r.Context())
	if err != nil {
		return httpx.Internal(err)
	}
	httpx.JSON(w, http.StatusOK, listeners)
	return nil
}

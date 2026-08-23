package api

import (
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/Wayy01/Just-Dashboard/backend/internal/auth"
	"github.com/Wayy01/Just-Dashboard/backend/internal/httpx"
	"github.com/Wayy01/Just-Dashboard/backend/internal/proxysvc"
	"github.com/go-chi/chi/v5"
)

// The site builder, the live TLS report and certbot.
//
// Mounted from mountProxyRoutes so the whole proxy surface stays readable in
// one place, the same way the Docker write surface lives in its own file but
// is mounted from mountDockerRoutes.
func (s *Server) mountSiteRoutes(r chi.Router) {
	r.Route("/proxy/sites", func(r chi.Router) {
		r.Method(http.MethodGet, "/{name}", s.handle(s.handleSiteSpec))
		r.Group(func(r chi.Router) {
			r.Use(httpx.RequireCapability(auth.CapSystemAdmin))
			// Preview renders and touches nothing, but it lives inside the
			// admin group because the form that calls it is admin-only and a
			// separate gate would be a claim about a boundary that is not
			// there.
			r.Method(http.MethodPost, "/preview", s.handle(s.handleSitePreview))
			r.Method(http.MethodPost, "/", s.handle(s.handleSiteApply))
			s.destructive(r, func(r chi.Router) {
				r.Method(http.MethodDelete, "/{name}", s.handle(s.handleSiteDelete))
			})
		})
	})
}

func (s *Server) handleSiteSpec(w http.ResponseWriter, r *http.Request) error {
	name := chi.URLParam(r, "name")
	if name == "" || strings.ContainsAny(name, "/\\") {
		return httpx.BadRequest("invalid site name")
	}
	// Read through the same allowlist the config editor uses rather than
	// joining a path here: this endpoint takes a name, and a name that turns
	// out to be a path is exactly what that check exists for.
	var content string
	var err error
	for _, candidate := range []string{
		filepath.Join(s.Cfg.NginxDir, "sites-available", name),
		filepath.Join(s.Cfg.NginxDir, "conf.d", name),
	} {
		content, err = s.modules.proxy.ReadConfig(candidate)
		if err == nil {
			break
		}
	}
	if err != nil {
		return httpx.ErrNotFound
	}
	spec, managed := proxysvc.ParseSiteSpec(name, content)
	httpx.JSON(w, http.StatusOK, map[string]any{
		"spec": spec, "managed": managed, "content": content,
		"warnings": proxysvc.SpecWarnings(spec),
	})
	return nil
}

type siteRequest struct {
	Spec      proxysvc.SiteSpec `json:"spec"`
	Enable    bool              `json:"enable"`
	Reload    bool              `json:"reload"`
	Overwrite bool              `json:"overwrite"`
}

// handleSitePreview shows the config a spec would produce, live, as the form
// is filled in. Rendering on the server is what keeps one implementation of
// "what does this spec mean"; a second one in the browser would drift, and the
// one that mattered would be the one nobody was reading.
func (s *Server) handleSitePreview(w http.ResponseWriter, r *http.Request) error {
	var req siteRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	content, err := proxysvc.RenderNginx(&req.Spec)
	if err != nil {
		return httpx.BadRequest("%v", err)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"content": content, "warnings": proxysvc.SpecWarnings(&req.Spec),
	})
	return nil
}

func (s *Server) handleSiteApply(w http.ResponseWriter, r *http.Request) error {
	var req siteRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	ctx, cancel := timeoutCtx(r, 60*time.Second)
	defer cancel()
	res, err := s.modules.proxy.ApplySite(ctx, &req.Spec, req.Enable, req.Reload, req.Overwrite)
	if err != nil {
		if errors.Is(err, proxysvc.ErrInvalidConf) {
			httpx.SetAudit(r, "proxy.site.apply", req.Spec.Name, map[string]any{"result": "rejected"})
			return httpx.Err(http.StatusUnprocessableEntity, "invalid_config", res.Validation.Output)
		}
		return mapProxyError(err)
	}
	httpx.SetAudit(r, "proxy.site.apply", req.Spec.Name, map[string]any{
		"domains": req.Spec.Domains, "kind": req.Spec.Kind,
		"tls": req.Spec.TLS, "reloaded": res.Reloaded,
	})
	httpx.JSON(w, http.StatusOK, res)
	return nil
}

func (s *Server) handleSiteDelete(w http.ResponseWriter, r *http.Request) error {
	name := chi.URLParam(r, "name")
	if err := httpx.RequireTypedConfirmation(w, r, name); err != nil {
		return err
	}
	if err := s.modules.proxy.DeleteSite(r.Context(), name); err != nil {
		return httpx.BadRequest("%v", err)
	}
	// The site is gone from disk; nginx keeps serving it until it reloads,
	// which is reported rather than hidden.
	reload, reloadErr := s.modules.proxy.Reload(r.Context(), proxysvc.KindNginx)
	httpx.SetAudit(r, "proxy.site.delete", name, map[string]any{"reloaded": reloadErr == nil})
	out := map[string]any{"name": name, "reload": reload}
	if reloadErr != nil {
		out["reloadError"] = reloadErr.Error()
	}
	httpx.JSON(w, http.StatusOK, out)
	return nil
}

// handleTLSScan is the live report: protocol versions, the chain as presented,
// and the headers the site actually sends. Everything else on this page reads
// files, and a file is not what a visitor gets.
func (s *Server) handleTLSScan(w http.ResponseWriter, r *http.Request) error {
	domain := r.URL.Query().Get("domain")
	if domain == "" {
		return httpx.BadRequest("domain query parameter is required")
	}
	ctx, cancel := timeoutCtx(r, 60*time.Second)
	defer cancel()
	scan := proxysvc.ScanTLS(ctx, domain, atoiDefault(r.URL.Query().Get("port"), 443))
	httpx.JSON(w, http.StatusOK, scan)
	return nil
}

func (s *Server) handleDomainDNS(w http.ResponseWriter, r *http.Request) error {
	domain := r.URL.Query().Get("domain")
	if domain == "" {
		return httpx.BadRequest("domain query parameter is required")
	}
	ctx, cancel := timeoutCtx(r, 20*time.Second)
	defer cancel()
	httpx.JSON(w, http.StatusOK, proxysvc.CheckDomainDNS(ctx, domain))
	return nil
}

func (s *Server) handleCertIssue(w http.ResponseWriter, r *http.Request) error {
	var req proxysvc.IssueRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	ctx, cancel := timeoutCtx(r, 6*time.Minute)
	defer cancel()
	out, err := s.modules.proxy.Issue(ctx, req)
	if err != nil {
		httpx.SetAudit(r, "certificates.issue", strings.Join(req.Domains, ","),
			map[string]any{"result": "failed", "staging": req.Staging})
		return httpx.Err(http.StatusBadGateway, "issue_failed", err.Error())
	}
	httpx.SetAudit(r, "certificates.issue", strings.Join(req.Domains, ","),
		map[string]any{"method": req.Method, "staging": req.Staging})
	httpx.JSON(w, http.StatusOK, map[string]string{"output": out})
	return nil
}

type renewRequest struct {
	Name   string `json:"name"`
	DryRun bool   `json:"dryRun"`
	Force  bool   `json:"force"`
}

func (s *Server) handleCertRenew(w http.ResponseWriter, r *http.Request) error {
	var req renewRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	ctx, cancel := timeoutCtx(r, 6*time.Minute)
	defer cancel()
	out, err := s.modules.proxy.Renew(ctx, req.Name, req.DryRun, req.Force)
	if err != nil {
		httpx.SetAudit(r, "certificates.renew", req.Name, map[string]any{"result": "failed"})
		return httpx.Err(http.StatusBadGateway, "renew_failed", err.Error())
	}
	httpx.SetAudit(r, "certificates.renew", req.Name,
		map[string]any{"dryRun": req.DryRun, "force": req.Force})
	httpx.JSON(w, http.StatusOK, map[string]string{"output": out})
	return nil
}

type revokeRequest struct {
	Name string `json:"name"`
}

// handleCertRevoke is irreversible in the way that matters: the authority
// records the certificate as untrusted and there is no undo, so every client
// holding it starts refusing the site.
func (s *Server) handleCertRevoke(w http.ResponseWriter, r *http.Request) error {
	var req revokeRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if err := httpx.RequireTypedConfirmation(w, r, "revoke "+req.Name); err != nil {
		return err
	}
	ctx, cancel := timeoutCtx(r, 4*time.Minute)
	defer cancel()
	out, err := s.modules.proxy.Revoke(ctx, req.Name)
	if err != nil {
		return httpx.Err(http.StatusBadGateway, "revoke_failed", err.Error())
	}
	httpx.SetAudit(r, "certificates.revoke", req.Name, nil)
	httpx.JSON(w, http.StatusOK, map[string]string{"output": out})
	return nil
}

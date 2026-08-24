package api

import (
	"errors"
	"io"
	"net/http"

	"github.com/Wayy01/Just-Dashboard/backend/internal/audit"
	"github.com/Wayy01/Just-Dashboard/backend/internal/auth"
	"github.com/Wayy01/Just-Dashboard/backend/internal/deploy"
	"github.com/Wayy01/Just-Dashboard/backend/internal/httpx"
	"github.com/go-chi/chi/v5"
)

func (s *Server) mountDeployRoutes(r chi.Router) {
	r.Route("/deploy", func(r chi.Router) {
		r.Method(http.MethodGet, "/", s.handle(s.handleDeployList))
		r.Method(http.MethodGet, "/{id}", s.handle(s.handleDeployGet))
		r.Method(http.MethodGet, "/{id}/runs", s.handle(s.handleDeployRuns))
		r.Method(http.MethodGet, "/{id}/commits", s.handle(s.handleDeployCommits))
		r.Method(http.MethodGet, "/{id}/env", s.handle(s.handleDeployEnvList))

		r.Group(func(r chi.Router) {
			r.Use(httpx.RequireCapability(auth.CapServiceControl))
			r.Method(http.MethodPost, "/{id}/run", s.handle(s.handleDeployRun))
		})
		r.Group(func(r chi.Router) {
			r.Use(httpx.RequireCapability(auth.CapSystemAdmin))
			r.Method(http.MethodPost, "/", s.handle(s.handleDeployCreate))
			r.Method(http.MethodPut, "/{id}", s.handle(s.handleDeployUpdate))
			r.Method(http.MethodPost, "/{id}/rotate-secret", s.handle(s.handleDeployRotateSecret))
			r.Method(http.MethodPut, "/{id}/env", s.handle(s.handleDeployEnvSet))
			r.Method(http.MethodGet, "/{id}/env/reveal", s.handle(s.handleDeployEnvReveal))
			r.Method(http.MethodDelete, "/{id}/env/{key}", s.handle(s.handleDeployEnvDelete))
		})
		s.destructive(r, func(r chi.Router) {
			r.Method(http.MethodDelete, "/{id}", s.handle(s.handleDeployDelete))
			r.Method(http.MethodPost, "/{id}/rollback", s.handle(s.handleDeployRollback))
		})
	})
}

func mapDeployError(err error) error {
	switch {
	case errors.Is(err, deploy.ErrNotFound):
		return httpx.ErrNotFound
	case errors.Is(err, deploy.ErrAlreadyDeploying):
		return httpx.Err(http.StatusConflict, "already_running", err.Error())
	case errors.Is(err, deploy.ErrDisabled):
		return httpx.Err(http.StatusForbidden, "hook_disabled", err.Error())
	case errors.Is(err, deploy.ErrBadSignature):
		return httpx.Err(http.StatusUnauthorized, "bad_signature", err.Error())
	default:
		return httpx.BadRequest("%v", err)
	}
}

func (s *Server) enrichProject(r *http.Request, p *deploy.Project) {
	p.HookURL = "/api/v1/hooks/deploy/" + p.HookID
	if state, err := s.modules.deployer.Inspect(r.Context(), p); err == nil {
		p.CurrentSHA, p.CurrentRef, p.Dirty = state.SHA, state.Ref, state.Dirty
	}
}

func (s *Server) handleDeployList(w http.ResponseWriter, r *http.Request) error {
	projects, err := s.modules.deployStore.List(r.Context())
	if err != nil {
		return httpx.Internal(err)
	}
	for _, p := range projects {
		s.enrichProject(r, p)
	}
	httpx.JSON(w, http.StatusOK, projects)
	return nil
}

func (s *Server) handleDeployGet(w http.ResponseWriter, r *http.Request) error {
	id, err := parseID(r)
	if err != nil {
		return err
	}
	p, err := s.modules.deployStore.Get(r.Context(), id)
	if err != nil {
		return mapDeployError(err)
	}
	s.enrichProject(r, p)
	if last, err := s.modules.deployStore.LastRun(r.Context(), id); err == nil {
		p.LastRun = last
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"project": p,
		"running": s.modules.deployer.IsRunning(id),
	})
	return nil
}

type deployProjectRequest struct {
	Name        string `json:"name"`
	RepoPath    string `json:"repoPath"`
	Branch      string `json:"branch"`
	ComposeFile string `json:"composeFile"`
	PreCommand  string `json:"preCommand"`
	PostCommand string `json:"postCommand"`
	Enabled     bool   `json:"enabled"`
}

func (req *deployProjectRequest) toProject() *deploy.Project {
	return &deploy.Project{
		Name: req.Name, RepoPath: req.RepoPath, Branch: req.Branch,
		ComposeFile: req.ComposeFile, PreCommand: req.PreCommand,
		PostCommand: req.PostCommand, Enabled: req.Enabled,
	}
}

func (s *Server) handleDeployCreate(w http.ResponseWriter, r *http.Request) error {
	var req deployProjectRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	project, secret, err := s.modules.deployStore.Create(r.Context(), req.toProject())
	if err != nil {
		return mapDeployError(err)
	}
	s.enrichProject(r, project)
	httpx.SetAudit(r, "deploy.project.create", project.Name,
		map[string]any{"repoPath": project.RepoPath, "branch": project.Branch})
	// The webhook secret is shown exactly once; only its sealed form is kept.
	httpx.JSON(w, http.StatusCreated, map[string]any{"project": project, "secret": secret})
	return nil
}

func (s *Server) handleDeployUpdate(w http.ResponseWriter, r *http.Request) error {
	id, err := parseID(r)
	if err != nil {
		return err
	}
	var req deployProjectRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	project, err := s.modules.deployStore.Update(r.Context(), id, req.toProject())
	if err != nil {
		return mapDeployError(err)
	}
	s.enrichProject(r, project)
	httpx.SetAudit(r, "deploy.project.update", project.Name, req)
	httpx.JSON(w, http.StatusOK, project)
	return nil
}

func (s *Server) handleDeployDelete(w http.ResponseWriter, r *http.Request) error {
	id, err := parseID(r)
	if err != nil {
		return err
	}
	project, err := s.modules.deployStore.Get(r.Context(), id)
	if err != nil {
		return mapDeployError(err)
	}
	// No typed phrase: this removes a hook and its history, and the checkout on
	// disk is deliberately left where it is — the running site does not notice.
	if err := s.modules.deployStore.Delete(r.Context(), id); err != nil {
		return httpx.Internal(err)
	}
	// The checkout on disk is left alone: removing the hook should not remove
	// the running application.
	httpx.SetAudit(r, "deploy.project.delete", project.Name, nil)
	httpx.NoContent(w)
	return nil
}

func (s *Server) handleDeployRotateSecret(w http.ResponseWriter, r *http.Request) error {
	id, err := parseID(r)
	if err != nil {
		return err
	}
	project, err := s.modules.deployStore.Get(r.Context(), id)
	if err != nil {
		return mapDeployError(err)
	}
	secret, err := s.modules.deployStore.RotateSecret(r.Context(), id)
	if err != nil {
		return mapDeployError(err)
	}
	httpx.SetAudit(r, "deploy.project.rotate_secret", project.Name, nil)
	httpx.JSON(w, http.StatusOK, map[string]string{"secret": secret})
	return nil
}

func (s *Server) handleDeployRun(w http.ResponseWriter, r *http.Request) error {
	id, err := parseID(r)
	if err != nil {
		return err
	}
	project, err := s.modules.deployStore.Get(r.Context(), id)
	if err != nil {
		return mapDeployError(err)
	}
	if s.modules.deployer.IsRunning(id) {
		return httpx.Err(http.StatusConflict, "already_running", deploy.ErrAlreadyDeploying.Error())
	}
	p := httpx.MustPrincipal(r)
	// A build can run for many minutes, so the deployment is detached and the
	// run row is what the UI follows.
	go func(actor string) {
		ctx, cancel := detachedContext(60 * 60)
		defer cancel()
		if _, err := s.modules.deployer.Deploy(ctx, id, "manual", actor); err != nil {
			s.Log.Error("deployment failed", "project", project.Name, "err", err)
		}
	}(p.Username())
	httpx.SetAudit(r, "deploy.run", project.Name, map[string]any{"branch": project.Branch})
	httpx.JSON(w, http.StatusAccepted, map[string]any{"started": true, "project": project.Name})
	return nil
}

type rollbackRequest struct {
	Commit string `json:"commit"`
}

func (s *Server) handleDeployRollback(w http.ResponseWriter, r *http.Request) error {
	id, err := parseID(r)
	if err != nil {
		return err
	}
	var req rollbackRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if err := deploy.ValidateSHA(req.Commit); err != nil {
		return httpx.BadRequest("%v", err)
	}
	project, err := s.modules.deployStore.Get(r.Context(), id)
	if err != nil {
		return mapDeployError(err)
	}
	// No typed phrase: a rollback is itself the recovery action, reached under
	// exactly the pressure that makes a typing exercise harmful, and it is
	// undone by deploying forward again.
	if s.modules.deployer.IsRunning(id) {
		return httpx.Err(http.StatusConflict, "already_running", deploy.ErrAlreadyDeploying.Error())
	}
	p := httpx.MustPrincipal(r)
	go func(actor string) {
		ctx, cancel := detachedContext(60 * 60)
		defer cancel()
		if _, err := s.modules.deployer.Rollback(ctx, id, req.Commit, actor); err != nil {
			s.Log.Error("rollback failed", "project", project.Name, "commit", req.Commit, "err", err)
		}
	}(p.Username())
	httpx.SetAudit(r, "deploy.rollback", project.Name, map[string]any{"commit": req.Commit})
	httpx.JSON(w, http.StatusAccepted, map[string]any{"started": true, "commit": req.Commit})
	return nil
}

func (s *Server) handleDeployRuns(w http.ResponseWriter, r *http.Request) error {
	id, err := parseID(r)
	if err != nil {
		return err
	}
	runs, err := s.modules.deployStore.Runs(r.Context(), id, atoiDefault(r.URL.Query().Get("limit"), 30))
	if err != nil {
		return httpx.Internal(err)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"runs":    runs,
		"running": s.modules.deployer.IsRunning(id),
	})
	return nil
}

func (s *Server) handleDeployCommits(w http.ResponseWriter, r *http.Request) error {
	id, err := parseID(r)
	if err != nil {
		return err
	}
	project, err := s.modules.deployStore.Get(r.Context(), id)
	if err != nil {
		return mapDeployError(err)
	}
	commits, err := s.modules.deployer.History(r.Context(), project,
		atoiDefault(r.URL.Query().Get("limit"), 30))
	if err != nil {
		return httpx.BadRequest("%v", err)
	}
	httpx.JSON(w, http.StatusOK, commits)
	return nil
}

func (s *Server) handleDeployEnvList(w http.ResponseWriter, r *http.Request) error {
	id, err := parseID(r)
	if err != nil {
		return err
	}
	vars, err := s.modules.deployStore.ListEnv(r.Context(), id, false)
	if err != nil {
		return httpx.Internal(err)
	}
	httpx.JSON(w, http.StatusOK, vars)
	return nil
}

// handleDeployEnvReveal returns the plaintext values. It is a separate,
// admin-only, audited endpoint precisely so that reading a project's secrets
// leaves a trail naming who read them and when.
func (s *Server) handleDeployEnvReveal(w http.ResponseWriter, r *http.Request) error {
	id, err := parseID(r)
	if err != nil {
		return err
	}
	project, err := s.modules.deployStore.Get(r.Context(), id)
	if err != nil {
		return mapDeployError(err)
	}
	vars, err := s.modules.deployStore.ListEnv(r.Context(), id, true)
	if err != nil {
		return httpx.Internal(err)
	}
	p := httpx.MustPrincipal(r)
	s.Audit.Record(r.Context(), audit.Entry{
		UserID: p.UserID(), Username: p.Username(), Role: string(p.Role),
		IP: httpx.ClientIP(r), Actor: p.Kind,
		Action: "deploy.env.reveal", Target: project.Name,
		Method: r.Method, Path: r.URL.Path, Status: http.StatusOK, Success: true,
		Detail: audit.Detail(map[string]any{"count": len(vars)}),
	})
	httpx.JSON(w, http.StatusOK, vars)
	return nil
}

type envSetRequest struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

func (s *Server) handleDeployEnvSet(w http.ResponseWriter, r *http.Request) error {
	id, err := parseID(r)
	if err != nil {
		return err
	}
	var req envSetRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	project, err := s.modules.deployStore.Get(r.Context(), id)
	if err != nil {
		return mapDeployError(err)
	}
	if err := s.modules.deployStore.SetEnv(r.Context(), id, req.Key, req.Value); err != nil {
		return mapDeployError(err)
	}
	// Only the variable name is recorded — auditing the value would defeat
	// the point of sealing it.
	httpx.SetAudit(r, "deploy.env.set", project.Name, map[string]any{"key": req.Key})
	httpx.NoContent(w)
	return nil
}

func (s *Server) handleDeployEnvDelete(w http.ResponseWriter, r *http.Request) error {
	id, err := parseID(r)
	if err != nil {
		return err
	}
	key := chi.URLParam(r, "key")
	project, err := s.modules.deployStore.Get(r.Context(), id)
	if err != nil {
		return mapDeployError(err)
	}
	if err := s.modules.deployStore.DeleteEnv(r.Context(), id, key); err != nil {
		return httpx.Internal(err)
	}
	httpx.SetAudit(r, "deploy.env.delete", project.Name, map[string]any{"key": key})
	httpx.NoContent(w)
	return nil
}

// maxHookBody bounds what a webhook may post. Provider payloads are tens of
// kilobytes; anything larger is not a push event.
const maxHookBody = 1 << 20

// handleDeployWebhook is the CI entry point. It is the only route reached
// without a dashboard session, so it authenticates by HMAC over the raw body
// with the project's own secret — and it still sits behind the network
// allowlist that fronts the whole API.
func (s *Server) handleDeployWebhook(w http.ResponseWriter, r *http.Request) error {
	hookID := chi.URLParam(r, "hookID")
	httpx.SetAuditActor(r, "webhook")
	project, err := s.modules.deployStore.ByHookID(r.Context(), hookID)
	if err != nil {
		// A wrong hook id and a wrong signature are both reported as
		// unauthorized so the endpoint does not confirm which ids exist. The
		// attempted id goes into the audit trail, though: a run of these is
		// what an enumeration scan looks like from the inside.
		httpx.SetAudit(r, "deploy.webhook", hookID, map[string]any{"reason": "unknown hook id"})
		return httpx.Err(http.StatusUnauthorized, "unauthorized", "unknown or unauthorised deploy hook")
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxHookBody))
	if err != nil {
		return httpx.BadRequest("could not read request body")
	}
	signature := r.Header.Get("X-Hub-Signature-256")
	if signature == "" {
		signature = r.Header.Get("X-Signature-256")
	}
	if err := s.modules.deployStore.VerifySignature(r.Context(), project.ID, body, signature); err != nil {
		httpx.SetAudit(r, "deploy.webhook", project.Name, map[string]any{"reason": "signature mismatch"})
		return httpx.Err(http.StatusUnauthorized, "unauthorized", "unknown or unauthorised deploy hook")
	}
	if !project.Enabled {
		httpx.SetAudit(r, "deploy.webhook", project.Name, map[string]any{"reason": "hook disabled"})
		return httpx.Err(http.StatusForbidden, "hook_disabled", deploy.ErrDisabled.Error())
	}
	if s.modules.deployer.IsRunning(project.ID) {
		httpx.SetAudit(r, "deploy.webhook", project.Name, map[string]any{"reason": "already deploying"})
		return httpx.Err(http.StatusConflict, "already_running", deploy.ErrAlreadyDeploying.Error())
	}
	httpx.SetAudit(r, "deploy.webhook", project.Name,
		map[string]any{"branch": project.Branch, "bytes": len(body)})
	go func() {
		ctx, cancel := detachedContext(60 * 60)
		defer cancel()
		if _, err := s.modules.deployer.Deploy(ctx, project.ID, "webhook", "ci"); err != nil {
			s.Log.Error("webhook deployment failed", "project", project.Name, "err", err)
		}
	}()
	httpx.JSON(w, http.StatusAccepted, map[string]any{
		"accepted": true, "project": project.Name, "branch": project.Branch,
	})
	return nil
}

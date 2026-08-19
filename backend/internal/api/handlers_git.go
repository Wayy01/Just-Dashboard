package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/Wayy01/Just-Dashboard/backend/internal/auth"
	"github.com/Wayy01/Just-Dashboard/backend/internal/gitx"
	"github.com/Wayy01/Just-Dashboard/backend/internal/httpx"
	"github.com/go-chi/chi/v5"
)

// Git routes are split by what they can cost you.
//
//	read            anyone authenticated
//	service.control fetch, pull, push, checkout, branch, stash — recoverable
//	destructive     discard and reset — these throw away uncommitted work
func (s *Server) mountGitRoutes(r chi.Router) {
	r.Route("/git", func(r chi.Router) {
		r.Method(http.MethodGet, "/", s.handle(s.handleGitRepos))
		r.Method(http.MethodGet, "/status", s.handle(s.handleGitStatus))
		r.Method(http.MethodGet, "/log", s.handle(s.handleGitLog))
		r.Method(http.MethodGet, "/branches", s.handle(s.handleGitBranches))
		r.Method(http.MethodGet, "/diff", s.handle(s.handleGitDiff))

		r.Group(func(r chi.Router) {
			r.Use(httpx.RequireCapability(auth.CapServiceControl))
			r.Method(http.MethodPost, "/fetch", s.handle(s.handleGitFetch))
			r.Method(http.MethodPost, "/pull", s.handle(s.handleGitPull))
			r.Method(http.MethodPost, "/push", s.handle(s.handleGitPush))
			r.Method(http.MethodPost, "/checkout", s.handle(s.handleGitCheckout))
			r.Method(http.MethodPost, "/branch", s.handle(s.handleGitBranch))
			r.Method(http.MethodPost, "/stash", s.handle(s.handleGitStash))
			r.Method(http.MethodPost, "/stash/pop", s.handle(s.handleGitStashPop))
		})

		s.destructive(r, func(r chi.Router) {
			r.Method(http.MethodPost, "/discard", s.handle(s.handleGitDiscard))
			r.Method(http.MethodPost, "/reset", s.handle(s.handleGitReset))
		})
	})
}

// gitRepo resolves the ?path= parameter to a repository inside the configured
// roots, turning the package's sentinel errors into the right status codes.
func (s *Server) gitRepo(r *http.Request) (string, error) {
	raw := r.URL.Query().Get("path")
	if raw == "" {
		return "", httpx.BadRequest("path query parameter is required")
	}
	path, err := s.modules.git.Resolve(raw)
	if err != nil {
		return "", gitErr(err)
	}
	return path, nil
}

func gitErr(err error) error {
	switch {
	case errors.Is(err, gitx.ErrNotInstalled):
		return httpx.Err(http.StatusServiceUnavailable, "not_installed", err.Error())
	case errors.Is(err, gitx.ErrOutsideRoots):
		return httpx.Err(http.StatusForbidden, "outside_roots", err.Error())
	case errors.Is(err, gitx.ErrNotARepo), errors.Is(err, gitx.ErrInvalidRef):
		return httpx.BadRequest("%v", err)
	}
	return httpx.Internal(err)
}

func (s *Server) handleGitRepos(w http.ResponseWriter, r *http.Request) error {
	repos, err := s.modules.git.Discover(r.Context())
	if err != nil {
		return gitErr(err)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"available": s.modules.git.Available(),
		"repos":     repos,
	})
	return nil
}

func (s *Server) handleGitStatus(w http.ResponseWriter, r *http.Request) error {
	path, err := s.gitRepo(r)
	if err != nil {
		return err
	}
	st, err := s.modules.git.Status(r.Context(), path)
	if err != nil {
		return gitErr(err)
	}
	httpx.JSON(w, http.StatusOK, st)
	return nil
}

func (s *Server) handleGitLog(w http.ResponseWriter, r *http.Request) error {
	path, err := s.gitRepo(r)
	if err != nil {
		return err
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	commits, err := s.modules.git.Log(r.Context(), path, r.URL.Query().Get("ref"), limit)
	if err != nil {
		return gitErr(err)
	}
	httpx.JSON(w, http.StatusOK, commits)
	return nil
}

func (s *Server) handleGitBranches(w http.ResponseWriter, r *http.Request) error {
	path, err := s.gitRepo(r)
	if err != nil {
		return err
	}
	branches, err := s.modules.git.Branches(r.Context(), path)
	if err != nil {
		return gitErr(err)
	}
	httpx.JSON(w, http.StatusOK, branches)
	return nil
}

func (s *Server) handleGitDiff(w http.ResponseWriter, r *http.Request) error {
	path, err := s.gitRepo(r)
	if err != nil {
		return err
	}
	q := r.URL.Query()
	diff, err := s.modules.git.Diff(r.Context(), path, q.Get("ref"), q.Get("file"), q.Get("staged") == "true")
	if err != nil {
		return gitErr(err)
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"diff": diff})
	return nil
}

// gitAction runs one operation and records it, so every state change to a
// repository is in the audit trail with the repository it touched.
func (s *Server) gitAction(w http.ResponseWriter, r *http.Request, action string,
	fn func(path string) (*gitx.Result, error),
) error {
	path, err := s.gitRepo(r)
	if err != nil {
		return err
	}
	res, err := fn(path)
	httpx.SetAudit(r, "git."+action, path, map[string]any{"ok": err == nil})
	if err != nil {
		// git's own message is the useful part; a failed pull or checkout is
		// an ordinary outcome, not a server fault.
		if res != nil {
			return httpx.BadRequest("%s", res.Output)
		}
		return gitErr(err)
	}
	httpx.JSON(w, http.StatusOK, res)
	return nil
}

func (s *Server) handleGitFetch(w http.ResponseWriter, r *http.Request) error {
	prune := r.URL.Query().Get("prune") == "true"
	return s.gitAction(w, r, "fetch", func(p string) (*gitx.Result, error) {
		return s.modules.git.Fetch(r.Context(), p, prune)
	})
}

func (s *Server) handleGitPull(w http.ResponseWriter, r *http.Request) error {
	return s.gitAction(w, r, "pull", func(p string) (*gitx.Result, error) {
		return s.modules.git.Pull(r.Context(), p)
	})
}

func (s *Server) handleGitPush(w http.ResponseWriter, r *http.Request) error {
	return s.gitAction(w, r, "push", func(p string) (*gitx.Result, error) {
		return s.modules.git.Push(r.Context(), p)
	})
}

type gitRefRequest struct {
	Ref     string `json:"ref"`
	Message string `json:"message"`
	File    string `json:"file"`
	Hard    bool   `json:"hard"`
}

func (s *Server) handleGitCheckout(w http.ResponseWriter, r *http.Request) error {
	var req gitRefRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	return s.gitAction(w, r, "checkout", func(p string) (*gitx.Result, error) {
		return s.modules.git.Checkout(r.Context(), p, req.Ref)
	})
}

func (s *Server) handleGitBranch(w http.ResponseWriter, r *http.Request) error {
	var req gitRefRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	return s.gitAction(w, r, "branch.create", func(p string) (*gitx.Result, error) {
		return s.modules.git.CreateBranch(r.Context(), p, req.Ref)
	})
}

func (s *Server) handleGitStash(w http.ResponseWriter, r *http.Request) error {
	var req gitRefRequest
	_ = httpx.DecodeJSON(r, &req)
	return s.gitAction(w, r, "stash", func(p string) (*gitx.Result, error) {
		return s.modules.git.Stash(r.Context(), p, req.Message)
	})
}

func (s *Server) handleGitStashPop(w http.ResponseWriter, r *http.Request) error {
	return s.gitAction(w, r, "stash.pop", func(p string) (*gitx.Result, error) {
		return s.modules.git.StashPop(r.Context(), p)
	})
}

func (s *Server) handleGitDiscard(w http.ResponseWriter, r *http.Request) error {
	var req gitRefRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	// Discarding rewrites a file to its committed state; the copy being
	// overwritten exists nowhere else.
	if err := httpx.RequireTypedConfirmation(w, r, "discard changes"); err != nil {
		return err
	}
	return s.gitAction(w, r, "discard", func(p string) (*gitx.Result, error) {
		return s.modules.git.Discard(r.Context(), p, req.File)
	})
}

func (s *Server) handleGitReset(w http.ResponseWriter, r *http.Request) error {
	var req gitRefRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	phrase := "reset branch"
	if req.Hard {
		phrase = "reset hard"
	}
	if err := httpx.RequireTypedConfirmation(w, r, phrase); err != nil {
		return err
	}
	return s.gitAction(w, r, "reset", func(p string) (*gitx.Result, error) {
		return s.modules.git.Reset(r.Context(), p, req.Ref, req.Hard)
	})
}

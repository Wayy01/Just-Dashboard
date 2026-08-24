package api

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/Wayy01/Just-Dashboard/backend/internal/auth"
	"github.com/Wayy01/Just-Dashboard/backend/internal/gitx"
	"github.com/Wayy01/Just-Dashboard/backend/internal/httpx"
	"github.com/go-chi/chi/v5"
)

// Git routes are split by what they can cost you.
//
//	read            anyone authenticated
//	service.control fetch, pull, push, checkout, branch, stash, stage, commit — recoverable
//	destructive     discard and reset — these throw away uncommitted work
func (s *Server) mountGitRoutes(r chi.Router) {
	r.Route("/git", func(r chi.Router) {
		r.Method(http.MethodGet, "/", s.handle(s.handleGitRepos))
		// detect answers "is this shell sitting in a checkout, and where is its
		// root", for the terminal page's git panel. It takes an arbitrary
		// directory rather than a repository the list already knows, which is
		// why it lives beside the list rather than inside gitRepo's resolver.
		r.Method(http.MethodGet, "/detect", s.handle(s.handleGitDetect))
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
			// Staging, unstaging and committing are recoverable: nothing here
			// destroys work that exists nowhere else (a commit can be reset, a
			// stage unstaged), so they share the service.control tier rather
			// than the destructive one.
			r.Method(http.MethodPost, "/stage", s.handle(s.handleGitStage))
			r.Method(http.MethodPost, "/unstage", s.handle(s.handleGitUnstage))
			r.Method(http.MethodPost, "/commit", s.handle(s.handleGitCommit))
		})

		s.destructive(r, func(r chi.Router) {
			r.Method(http.MethodPost, "/discard", s.handle(s.handleGitDiscard))
			r.Method(http.MethodPost, "/reset", s.handle(s.handleGitReset))
			// Deleting a branch can strand commits when forced, so it sits with
			// the other irreversible operations behind a typed confirmation.
			r.Method(http.MethodPost, "/branch/delete", s.handle(s.handleGitBranchDelete))
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

// handleGitDetect resolves an arbitrary directory — the terminal's current
// working directory — to the checkout that contains it, if any. It answers in
// three shapes, all with HTTP 200 because "not a repository" is an ordinary
// answer and not a failure:
//
//	{available:false}                      git is not installed
//	{available:true}                       the directory is not inside a checkout
//	{available:true, root, inRoots:false}  a checkout, but outside JD_GIT_ROOTS
//	{available:true, inRoots:true, repo}   a checkout the panel can operate on
//
// The inRoots distinction matters: everything else in /git is gated on the
// configured roots, so a repo outside them can be reported but its buttons
// would fail. Saying which it is up front is kinder than letting them 403.
func (s *Server) handleGitDetect(w http.ResponseWriter, r *http.Request) error {
	raw := r.URL.Query().Get("path")
	if raw == "" {
		return httpx.BadRequest("path query parameter is required")
	}
	if !s.modules.git.Available() {
		httpx.JSON(w, http.StatusOK, map[string]any{"available": false})
		return nil
	}
	ctx, cancel := timeoutCtx(r, 15*time.Second)
	defer cancel()
	root, err := s.modules.git.Toplevel(ctx, raw)
	if err != nil {
		httpx.JSON(w, http.StatusOK, map[string]any{"available": true})
		return nil
	}
	resolved, rerr := s.modules.git.Resolve(root)
	if rerr != nil {
		httpx.JSON(w, http.StatusOK, map[string]any{"available": true, "root": root, "inRoots": false})
		return nil
	}
	repo, err := s.modules.git.Summary(ctx, resolved)
	if err != nil {
		return gitErr(err)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"available": true, "inRoots": true, "repo": repo})
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
	Ref     string   `json:"ref"`
	Message string   `json:"message"`
	File    string   `json:"file"`
	Hard    bool     `json:"hard"`
	Files   []string `json:"files"`
	Amend   bool     `json:"amend"`
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

func (s *Server) handleGitStage(w http.ResponseWriter, r *http.Request) error {
	var req gitRefRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	return s.gitAction(w, r, "stage", func(p string) (*gitx.Result, error) {
		return s.modules.git.Stage(r.Context(), p, req.Files)
	})
}

func (s *Server) handleGitUnstage(w http.ResponseWriter, r *http.Request) error {
	var req gitRefRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	return s.gitAction(w, r, "unstage", func(p string) (*gitx.Result, error) {
		return s.modules.git.Unstage(r.Context(), p, req.Files)
	})
}

func (s *Server) handleGitCommit(w http.ResponseWriter, r *http.Request) error {
	var req gitRefRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	return s.gitAction(w, r, "commit", func(p string) (*gitx.Result, error) {
		return s.modules.git.Commit(r.Context(), p, req.Message, req.Amend)
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

func (s *Server) handleGitBranchDelete(w http.ResponseWriter, r *http.Request) error {
	var req gitRefRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	// The confirmation stands whether or not force is set: a safe delete of a
	// merged branch is still a deliberate act, and the phrase is the same either
	// way so the habit does not depend on which kind it turned out to be.
	if err := httpx.RequireTypedConfirmation(w, r, "delete branch"); err != nil {
		return err
	}
	return s.gitAction(w, r, "branch.delete", func(p string) (*gitx.Result, error) {
		return s.modules.git.DeleteBranch(r.Context(), p, req.Ref, req.Hard)
	})
}

func (s *Server) handleGitReset(w http.ResponseWriter, r *http.Request) error {
	var req gitRefRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	// Only a hard reset is typed for. A soft or mixed reset moves the branch
	// pointer and leaves the working tree alone, so the work is still on disk;
	// --hard is the one that overwrites it with the commit and leaves no copy.
	if req.Hard {
		if err := httpx.RequireTypedConfirmation(w, r, "reset hard"); err != nil {
			return err
		}
	}
	return s.gitAction(w, r, "reset", func(p string) (*gitx.Result, error) {
		return s.modules.git.Reset(r.Context(), p, req.Ref, req.Hard)
	})
}

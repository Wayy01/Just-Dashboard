package gitx

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Result is what an operation reports back: the command that ran and what git
// said about it. The output is shown verbatim, because git's own messages are
// better than anything this layer could paraphrase.
type Result struct {
	Command string `json:"command"`
	Output  string `json:"output"`
	OK      bool   `json:"ok"`
}

func (s *Service) op(ctx context.Context, path string, timeout time.Duration, args ...string) (*Result, error) {
	out, err := s.runTimeout(ctx, path, timeout, args...)
	res := &Result{Command: "git " + strings.Join(args, " "), Output: strings.TrimSpace(out), OK: err == nil}
	if err != nil {
		return res, err
	}
	if res.Output == "" {
		res.Output = "Done."
	}
	return res, nil
}

// Fetch updates remote-tracking refs without touching the working tree. It is
// the one network operation that cannot lose anything, which is why it is not
// gated behind a confirmation.
func (s *Service) Fetch(ctx context.Context, path string, prune bool) (*Result, error) {
	args := []string{"fetch", "--all", "--tags"}
	if prune {
		args = append(args, "--prune")
	}
	return s.op(ctx, path, 3*time.Minute, args...)
}

// Pull refuses anything but a fast-forward.
//
// A merge or rebase here could stop halfway and leave conflict markers in a
// working tree the operator cannot easily fix from a web page. Refusing is a
// clear failure they can resolve deliberately; a half-finished merge is not.
func (s *Service) Pull(ctx context.Context, path string) (*Result, error) {
	return s.op(ctx, path, 3*time.Minute, "pull", "--ff-only")
}

// Push sends the current branch to its upstream. No force, ever: this runs
// unattended behind a web request, which is the worst possible place to
// discard someone else's commits.
func (s *Service) Push(ctx context.Context, path string) (*Result, error) {
	return s.op(ctx, path, 3*time.Minute, "push")
}

// Checkout switches branches. It does not use --force, so git refuses when the
// switch would overwrite local modifications, and the operator is told rather
// than silently losing them.
func (s *Service) Checkout(ctx context.Context, path, ref string) (*Result, error) {
	if err := ValidateRef(ref); err != nil {
		return nil, err
	}
	return s.op(ctx, path, time.Minute, "checkout", ref, "--")
}

// CreateBranch branches from the current HEAD and switches to it.
func (s *Service) CreateBranch(ctx context.Context, path, name string) (*Result, error) {
	if err := ValidateRef(name); err != nil {
		return nil, err
	}
	return s.op(ctx, path, time.Minute, "checkout", "-b", name, "--")
}

// Stash puts local modifications aside, including untracked files, so the
// operator can switch branches without losing work.
func (s *Service) Stash(ctx context.Context, path, message string) (*Result, error) {
	args := []string{"stash", "push", "--include-untracked"}
	if m := strings.TrimSpace(message); m != "" {
		if len(m) > 200 {
			m = m[:200]
		}
		if strings.HasPrefix(m, "-") {
			return nil, fmt.Errorf("%w: message may not start with a dash", ErrInvalidRef)
		}
		args = append(args, "-m", m)
	}
	return s.op(ctx, path, time.Minute, args...)
}

// StashPop restores the most recent stash.
func (s *Service) StashPop(ctx context.Context, path string) (*Result, error) {
	return s.op(ctx, path, time.Minute, "stash", "pop")
}

// Discard throws away uncommitted changes to one file. This destroys work that
// exists nowhere else, which is why the route above it demands a typed
// confirmation.
func (s *Service) Discard(ctx context.Context, path, file string) (*Result, error) {
	if file == "" || strings.Contains(file, "..") || strings.HasPrefix(file, "/") || strings.HasPrefix(file, "-") {
		return nil, fmt.Errorf("%w: %q", ErrInvalidRef, file)
	}
	return s.op(ctx, path, time.Minute, "checkout", "--", file)
}

// Reset moves the branch to a commit. Hard mode discards the working tree
// along with it — the most destructive thing this package can do.
func (s *Service) Reset(ctx context.Context, path, ref string, hard bool) (*Result, error) {
	if err := ValidateRef(ref); err != nil {
		return nil, err
	}
	mode := "--mixed"
	if hard {
		mode = "--hard"
	}
	return s.op(ctx, path, time.Minute, "reset", mode, ref, "--")
}

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
//
// A branch with no upstream is published rather than refused. Plain `git push`
// answers that case with "the current branch has no upstream branch" and a
// command to copy — which is fine in a terminal and useless here, where the
// operator has just made a branch in this very page and the only thing they
// can do about it is open a shell. Setting the upstream is what they would
// have typed, and it is what makes a pull request possible at all: gh needs
// the branch to exist on the remote before it can open one against it.
func (s *Service) Push(ctx context.Context, path string) (*Result, error) {
	if s.hasUpstream(ctx, path) {
		return s.op(ctx, path, 3*time.Minute, "push")
	}
	branch, err := s.CurrentBranch(ctx, path)
	if err != nil {
		// Detached, or no commits yet. Let plain push produce git's own
		// diagnosis rather than inventing one.
		return s.op(ctx, path, 3*time.Minute, "push")
	}
	return s.op(ctx, path, 3*time.Minute, "push", "--set-upstream", "origin", branch)
}

// CurrentBranch is the branch HEAD is on, and an error when it is on none.
func (s *Service) CurrentBranch(ctx context.Context, path string) (string, error) {
	out, err := s.run(ctx, path, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	branch := strings.TrimSpace(out)
	if branch == "" || branch == "HEAD" {
		return "", fmt.Errorf("%w: HEAD is not on a branch", ErrInvalidRef)
	}
	if err := ValidateRef(branch); err != nil {
		return "", err
	}
	return branch, nil
}

func (s *Service) hasUpstream(ctx context.Context, path string) bool {
	_, err := s.run(ctx, path, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	return err == nil
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

// DeleteBranch removes a local branch. Without force git refuses to delete one
// whose commits are not merged anywhere — the safety a non-expert most wants —
// and force (-D) overrides that check, which can strand commits, so the route
// above it takes a typed confirmation. The `--` ends the options so the name
// can never be read as one, on top of ValidateRef already refusing a leading
// dash.
func (s *Service) DeleteBranch(ctx context.Context, path, name string, force bool) (*Result, error) {
	if err := ValidateRef(name); err != nil {
		return nil, err
	}
	flag := "-d"
	if force {
		flag = "-D"
	}
	return s.op(ctx, path, time.Minute, "branch", flag, "--", name)
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

// validatePaths refuses anything that could climb out of the working tree or
// be read as an option. The `--` separator every caller adds stops a leading
// dash from becoming a flag, but a `..` still escapes the repository, so both
// checks stay.
func validatePaths(files []string) error {
	for _, f := range files {
		if f == "" || strings.Contains(f, "..") || strings.HasPrefix(f, "/") || strings.HasPrefix(f, "-") {
			return fmt.Errorf("%w: %q", ErrInvalidRef, f)
		}
	}
	return nil
}

// Stage adds paths to the index so the next commit records them. With no paths
// it stages every change in the working tree — additions, modifications and
// deletions alike — which is the "stage everything" the commit box offers.
//
// Staging is the inverse of Unstage and loses nothing, so neither is gated
// behind a typed confirmation; both sit under service.control with the rest of
// the recoverable operations.
func (s *Service) Stage(ctx context.Context, path string, files []string) (*Result, error) {
	if err := validatePaths(files); err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return s.op(ctx, path, time.Minute, "add", "-A")
	}
	return s.op(ctx, path, time.Minute, append([]string{"add", "--"}, files...)...)
}

// Unstage moves paths back out of the index without touching the working tree,
// so the edits themselves are never at risk. `reset -q HEAD` is used rather
// than the newer `restore --staged` because it works on every git a server is
// likely to carry; the reset is index-only and cannot lose the file's content.
func (s *Service) Unstage(ctx context.Context, path string, files []string) (*Result, error) {
	if err := validatePaths(files); err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return s.op(ctx, path, time.Minute, "reset", "-q", "HEAD")
	}
	return s.op(ctx, path, time.Minute, append([]string{"reset", "-q", "HEAD", "--"}, files...)...)
}

// Commit records whatever is staged. An empty message is refused unless this is
// an --amend that keeps the previous one, since git would otherwise open an
// editor this request cannot answer. The message reaches git as the argument to
// -m, so it cannot be read as an option however it begins.
//
// A commit needs user.name and user.email in the owner's git config; when they
// are missing git says exactly that, and that message is more useful than
// anything this layer could paraphrase — the operation runs AsOwner, so it is
// the account owning the repository whose identity is used.
func (s *Service) Commit(ctx context.Context, path, message string, amend bool) (*Result, error) {
	msg := strings.TrimSpace(message)
	if msg == "" && !amend {
		return nil, fmt.Errorf("%w: a commit message is required", ErrInvalidRef)
	}
	args := []string{"commit"}
	if amend {
		args = append(args, "--amend")
	}
	if msg != "" {
		args = append(args, "-m", msg)
	} else {
		// --amend with no new message keeps the old one rather than opening an
		// editor the web request has no way to drive.
		args = append(args, "--no-edit")
	}
	return s.op(ctx, path, time.Minute, args...)
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

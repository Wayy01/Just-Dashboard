package gitx

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A ref reaches an argument vector, so anything that could be read as an
// option or escape the repository has to be refused before it gets there.
func TestValidateRef(t *testing.T) {
	ok := []string{
		"main", "feature/login", "release-1.2.3", "v2.0", "user@host",
		"a_b.c-d", "origin/main", "HEAD", "abc123def",
	}
	for _, r := range ok {
		if err := ValidateRef(r); err != nil {
			t.Errorf("ValidateRef(%q) rejected a valid ref: %v", r, err)
		}
	}
	bad := []string{
		"", "--upload-pack=/bin/sh", "--exec=rm -rf /", "-x",
		"../../etc/passwd", "main..dev", "branch.lock",
		"has space", "semi;colon", "pipe|char", "dollar$sub", "back`tick",
		"quote'x", `dquote"x`, "new\nline", strings.Repeat("a", 256),
	}
	for _, r := range bad {
		if err := ValidateRef(r); err == nil {
			t.Errorf("ValidateRef(%q) accepted a ref it should refuse", r)
		}
	}
}

// The roots are the boundary an operator configured; a path outside them must
// be refused even when it really is a repository.
func TestResolveRejectsOutsideRoots(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "inside")
	mkRepo(t, repo)

	outside := t.TempDir()
	mkRepo(t, filepath.Join(outside, "other"))

	s := New([]string{dir})

	if _, err := s.Resolve(repo); err != nil {
		t.Errorf("repo inside a root was rejected: %v", err)
	}
	if _, err := s.Resolve(filepath.Join(outside, "other")); err == nil {
		t.Error("repo outside every root was accepted")
	}
	// A prefix match must not be a substring match: /tmp/xyz-evil is not
	// inside /tmp/xyz.
	sibling := dir + "-evil"
	if _, err := s.Resolve(sibling); err == nil {
		t.Error("a sibling directory sharing a name prefix was accepted")
	}
}

func TestResolveRejectsNonRepo(t *testing.T) {
	dir := t.TempDir()
	s := New([]string{dir})
	if _, err := s.Resolve(dir); err == nil {
		t.Error("a plain directory was accepted as a repository")
	}
}

// A remote may carry a token; it is rendered on a list page, so it is scrubbed.
func TestScrubRemote(t *testing.T) {
	cases := map[string]string{
		"https://user:ghp_secrettoken@github.com/a/b.git": "https://***@github.com/a/b.git",
		"https://github.com/a/b.git":                      "https://github.com/a/b.git",
		"git@github.com:a/b.git":                          "git@github.com:a/b.git",
		"":                                                "",
	}
	for in, want := range cases {
		if got := scrubRemote(in); got != want {
			t.Errorf("scrubRemote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTrackCount(t *testing.T) {
	cases := []struct {
		in, key string
		want    int
	}{
		{"[ahead 2, behind 1]", "ahead ", 2},
		{"[ahead 2, behind 1]", "behind ", 1},
		{"[ahead 12]", "ahead ", 12},
		{"[behind 3]", "ahead ", 0},
		{"", "ahead ", 0},
	}
	for _, c := range cases {
		if got := trackCount(c.in, c.key); got != c.want {
			t.Errorf("trackCount(%q,%q) = %d, want %d", c.in, c.key, got, c.want)
		}
	}
}

// A path list reaches an argument vector after `--`; a leading dash cannot
// become a flag there, but `..` still climbs out of the working tree, so both
// are refused before the command runs.
func TestValidatePaths(t *testing.T) {
	ok := [][]string{
		nil, {},
		{"main.go"}, {"a/b/c.txt", "d.md"}, {"weird name.txt"}, {".env"},
	}
	for _, in := range ok {
		if err := validatePaths(in); err != nil {
			t.Errorf("validatePaths(%q) rejected valid paths: %v", in, err)
		}
	}
	bad := [][]string{
		{""}, {"../escape"}, {"a/../../etc/passwd"}, {"/abs/path"}, {"-x"},
		{"ok.txt", "../bad"},
	}
	for _, in := range bad {
		if err := validatePaths(in); err == nil {
			t.Errorf("validatePaths(%q) accepted paths it should refuse", in)
		}
	}
}

// The stage → commit → unstage round-trip is the whole reason these operations
// exist, and each one's effect is only visible in what `git status` reports
// afterwards — so the flow is exercised end to end against a real repository.
// It skips where git is absent, the same bargain the term package strikes with
// tmux, because a fake git would pass while the product stayed broken.
func TestStageCommitUnstage(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_CONFIG_NOSYSTEM=1", "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("config", "user.name", "t")
	run("config", "user.email", "t@e")

	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := New([]string{dir})
	ctx := context.Background()

	// An untracked file: staging it makes it show up staged.
	if _, err := s.Stage(ctx, dir, []string{"a.txt"}); err != nil {
		t.Fatalf("Stage: %v", err)
	}
	st, err := s.Status(ctx, dir)
	if err != nil {
		t.Fatalf("Status after stage: %v", err)
	}
	if len(st.Files) != 1 || !st.Files[0].Staged {
		t.Fatalf("after Stage, want one staged file, got %+v", st.Files)
	}

	// An empty message is refused rather than opening an editor the request
	// cannot answer.
	if _, err := s.Commit(ctx, dir, "  ", false); err == nil {
		t.Error("Commit accepted an empty message")
	}

	if _, err := s.Commit(ctx, dir, "add a.txt", false); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if st, err = s.Status(ctx, dir); err != nil || !st.Clean {
		t.Fatalf("after Commit the tree should be clean, got clean=%v err=%v", st.Clean, err)
	}

	// Modify, stage everything, then unstage it: the change survives, only the
	// index pointer moves back.
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello world\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Stage(ctx, dir, nil); err != nil {
		t.Fatalf("Stage all: %v", err)
	}
	if st, err = s.Status(ctx, dir); err != nil || len(st.Files) != 1 || !st.Files[0].Staged {
		t.Fatalf("after Stage all, want one staged file, got %+v (err %v)", st.Files, err)
	}
	if _, err := s.Unstage(ctx, dir, nil); err != nil {
		t.Fatalf("Unstage all: %v", err)
	}
	if st, err = s.Status(ctx, dir); err != nil || len(st.Files) != 1 || st.Files[0].Staged {
		t.Fatalf("after Unstage the file should be modified-not-staged, got %+v (err %v)", st.Files, err)
	}

	// A branch can be created and then deleted; the delete must actually remove
	// it from the branch listing.
	if _, err := s.CreateBranch(ctx, dir, "scratch"); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	if _, err := s.Checkout(ctx, dir, "main"); err != nil {
		t.Fatalf("Checkout main: %v", err)
	}
	if _, err := s.DeleteBranch(ctx, dir, "scratch", true); err != nil {
		t.Fatalf("DeleteBranch: %v", err)
	}
	branches, err := s.Branches(ctx, dir)
	if err != nil {
		t.Fatalf("Branches: %v", err)
	}
	for _, b := range branches {
		if b.Name == "scratch" {
			t.Fatalf("DeleteBranch left 'scratch' in the listing: %+v", branches)
		}
	}
}

func mkRepo(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(path, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// A local branch may contain a slash, so only the full refname distinguishes
// local from remote.
func TestBranchRemoteDetection(t *testing.T) {
	cases := map[string]bool{
		"refs/heads/main":               false,
		"refs/heads/fix/audit-findings": false,
		"refs/heads/feature/a/b":        false,
		"refs/remotes/origin/main":      true,
		"refs/remotes/upstream/dev":     true,
	}
	for refname, want := range cases {
		if got := strings.HasPrefix(refname, "refs/remotes/"); got != want {
			t.Errorf("%q classified remote=%v, want %v", refname, got, want)
		}
	}
}

// Package deploy turns a webhook or a button press into a git pull plus a
// container rebuild, and keeps enough history to put the previous commit back.
//
// Webhooks are the one part of the API that is not authenticated by a
// dashboard session — CI has no session. They are authenticated instead by an
// HMAC signature over the request body using a per-project secret, and they
// still sit behind the network allowlist like everything else.
package deploy

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type Project struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	RepoPath    string    `json:"repoPath"`
	Branch      string    `json:"branch"`
	ComposeFile string    `json:"composeFile"`
	PreCommand  string    `json:"preCommand,omitempty"`
	PostCommand string    `json:"postCommand,omitempty"`
	HookID      string    `json:"hookId"`
	Enabled     bool      `json:"enabled"`
	CreatedAt   time.Time `json:"createdAt"`

	// HookURL is assembled for display; the secret itself is only ever shown
	// once, at creation time.
	HookURL     string `json:"hookUrl,omitempty"`
	CurrentSHA  string `json:"currentSha,omitempty"`
	CurrentRef  string `json:"currentRef,omitempty"`
	Dirty       bool   `json:"dirty,omitempty"`
	LastRun     *Run   `json:"lastRun,omitempty"`
	EnvVarCount int    `json:"envVarCount"`
}

type RunStatus string

const (
	StatusRunning RunStatus = "running"
	StatusSuccess RunStatus = "success"
	StatusFailed  RunStatus = "failed"
)

type Run struct {
	ID         int64      `json:"id"`
	ProjectID  int64      `json:"projectId"`
	StartedAt  time.Time  `json:"startedAt"`
	EndedAt    *time.Time `json:"endedAt,omitempty"`
	Status     RunStatus  `json:"status"`
	Trigger    string     `json:"trigger"`
	Actor      string     `json:"actor"`
	FromCommit string     `json:"fromCommit"`
	ToCommit   string     `json:"toCommit"`
	Log        string     `json:"log"`
	Duration   string     `json:"duration,omitempty"`
	// Rollbackable is true when this run recorded a commit that the project
	// can be returned to.
	Rollbackable bool `json:"rollbackable"`
}

type EnvVar struct {
	Key       string    `json:"key"`
	Value     string    `json:"value,omitempty"`
	UpdatedAt time.Time `json:"updatedAt"`
	// Masked carries a redacted preview so the UI can show that a value
	// exists without revealing it in a list view.
	Masked string `json:"masked"`
}

var (
	projectNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	branchRe      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,127}$`)
	envKeyRe      = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)
	shaRe         = regexp.MustCompile(`^[0-9a-f]{7,40}$`)
)

// Validate checks a project against the configured deploy roots.
//
// "Absolute" was the only rule the repository path had to satisfy, so a deploy
// project could name any directory on the filesystem and the JD_DEPLOY_ROOT
// setting that was supposed to bound it went unread. Every other place the
// dashboard takes a host path from a client — files, git, compose, logs — is
// bounded by a configured root, and this is now one of them.
func (p *Project) Validate(roots []string) error {
	if !projectNameRe.MatchString(p.Name) {
		return fmt.Errorf("name must start with a letter or digit and contain only letters, digits, dots, dashes and underscores")
	}
	if !strings.HasPrefix(p.RepoPath, "/") {
		return fmt.Errorf("repoPath must be an absolute path")
	}
	p.RepoPath = filepath.Clean(p.RepoPath)
	if !withinRoots(p.RepoPath, roots) {
		return fmt.Errorf("repoPath must be inside one of the deploy roots (%s)", strings.Join(roots, ", "))
	}
	if p.Branch == "" {
		p.Branch = "main"
	}
	if !branchRe.MatchString(p.Branch) {
		return fmt.Errorf("branch name %q is not valid", p.Branch)
	}
	if p.ComposeFile == "" {
		p.ComposeFile = "docker-compose.yml"
	}
	if strings.Contains(p.ComposeFile, "..") || strings.HasPrefix(p.ComposeFile, "/") {
		return fmt.Errorf("composeFile must be a path relative to the repository")
	}
	return nil
}

func withinRoots(path string, roots []string) bool {
	// An empty root list means the operator was never given the setting; the
	// only safe reading is "nothing is permitted", but that would break an
	// install on upgrade, so it is treated as unconfigured and permissive.
	// config.Load always supplies a default, so this is the zero-value path.
	if len(roots) == 0 {
		return true
	}
	resolved := path
	if r, err := filepath.EvalSymlinks(path); err == nil {
		resolved = r
	}
	for _, root := range roots {
		root = filepath.Clean(root)
		for _, candidate := range []string{path, resolved} {
			if root == "/" || candidate == root || strings.HasPrefix(candidate, root+string(os.PathSeparator)) {
				return true
			}
		}
	}
	return false
}

func ValidateEnvKey(key string) error {
	if !envKeyRe.MatchString(key) {
		return fmt.Errorf("%q is not a valid environment variable name", key)
	}
	return nil
}

func ValidateSHA(sha string) error {
	if !shaRe.MatchString(sha) {
		return fmt.Errorf("%q is not a valid commit hash", sha)
	}
	return nil
}

// mask keeps the shape of a secret visible without disclosing it, which is
// enough for an operator to recognise a value they set earlier.
func mask(value string) string {
	switch {
	case value == "":
		return ""
	case len(value) <= 4:
		return strings.Repeat("•", len(value))
	case len(value) <= 12:
		return value[:1] + strings.Repeat("•", len(value)-1)
	default:
		return value[:2] + strings.Repeat("•", 8) + value[len(value)-2:]
	}
}

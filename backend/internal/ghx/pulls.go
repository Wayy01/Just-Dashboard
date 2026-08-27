package ghx

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Repo is what GitHub knows about this checkout: which repository the remote
// points at, and what a pull request would target by default.
type Repo struct {
	NameWithOwner string `json:"nameWithOwner"`
	DefaultBranch string `json:"defaultBranch"`
	URL           string `json:"url"`
	Private       bool   `json:"private"`
	// Permission is the viewer's own — READ means a pull request has to come
	// from a fork, which is worth saying before the button is pressed rather
	// than after.
	Permission string `json:"permission,omitempty"`
}

// PullRequest is one open pull request, in the shape the page lists them.
type PullRequest struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	URL       string    `json:"url"`
	State     string    `json:"state"`
	Draft     bool      `json:"draft"`
	Head      string    `json:"head"`
	Base      string    `json:"base"`
	Author    string    `json:"author,omitempty"`
	CreatedAt time.Time `json:"createdAt,omitempty"`
	Comments  int       `json:"comments"`
}

// RepoInfo reads the repository behind the checkout's remote.
//
// It is a separate call from the pull request list because it answers a
// different question and is wanted at a different time — the list feeds a tab
// that polls, this feeds the create dialog when it opens.
func (s *Service) RepoInfo(ctx context.Context, dir string) (*Repo, error) {
	if !s.Available() {
		return nil, ErrNotInstalled
	}
	out, err := s.run(ctx, dir, "", "repo", "view",
		"--json", "nameWithOwner,defaultBranchRef,url,isPrivate,viewerPermission")
	if err != nil {
		return nil, ghErr("read this repository on GitHub", out)
	}
	var body struct {
		NameWithOwner    string `json:"nameWithOwner"`
		DefaultBranchRef struct {
			Name string `json:"name"`
		} `json:"defaultBranchRef"`
		URL        string `json:"url"`
		IsPrivate  bool   `json:"isPrivate"`
		Permission string `json:"viewerPermission"`
	}
	if err := json.Unmarshal([]byte(out), &body); err != nil {
		return nil, fmt.Errorf("could not read gh's answer: %w", err)
	}
	return &Repo{
		NameWithOwner: body.NameWithOwner,
		DefaultBranch: body.DefaultBranchRef.Name,
		URL:           body.URL,
		Private:       body.IsPrivate,
		Permission:    body.Permission,
	}, nil
}

// ListPulls returns the open pull requests, newest first.
func (s *Service) ListPulls(ctx context.Context, dir string, limit int) ([]PullRequest, error) {
	if !s.Available() {
		return nil, ErrNotInstalled
	}
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	out, err := s.run(ctx, dir, "", "pr", "list",
		"--state", "open", "--limit", fmt.Sprint(limit),
		"--json", "number,title,url,state,isDraft,headRefName,baseRefName,author,createdAt,comments")
	if err != nil {
		return nil, ghErr("list pull requests", out)
	}
	var body []struct {
		Number  int    `json:"number"`
		Title   string `json:"title"`
		URL     string `json:"url"`
		State   string `json:"state"`
		IsDraft bool   `json:"isDraft"`
		Head    string `json:"headRefName"`
		Base    string `json:"baseRefName"`
		Author  struct {
			Login string `json:"login"`
		} `json:"author"`
		CreatedAt time.Time  `json:"createdAt"`
		Comments  []struct{} `json:"comments"`
	}
	if err := json.Unmarshal([]byte(out), &body); err != nil {
		return nil, fmt.Errorf("could not read gh's answer: %w", err)
	}
	pulls := make([]PullRequest, 0, len(body))
	for _, p := range body {
		pulls = append(pulls, PullRequest{
			Number: p.Number, Title: p.Title, URL: p.URL, State: strings.ToLower(p.State),
			Draft: p.IsDraft, Head: p.Head, Base: p.Base, Author: p.Author.Login,
			CreatedAt: p.CreatedAt, Comments: len(p.Comments),
		})
	}
	return pulls, nil
}

// NewPull is the description of a pull request to open.
type NewPull struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Base  string `json:"base"`
	Head  string `json:"head"`
	Draft bool   `json:"draft"`
}

// CreatePull opens a pull request from the current branch.
//
// gh's own message is passed through on failure, because the two ways this
// fails are both things only gh can say precisely: there are no commits
// between the branches, or one already exists — and that second message
// carries the URL of the existing one, which is exactly what the operator
// wanted anyway.
func (s *Service) CreatePull(ctx context.Context, dir string, req NewPull) (*PullRequest, error) {
	if !s.Available() {
		return nil, ErrNotInstalled
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		return nil, fmt.Errorf("a title is required")
	}
	args := []string{"pr", "create", "--title", title, "--body", req.Body}
	if req.Base != "" {
		if err := validateBranch(req.Base); err != nil {
			return nil, err
		}
		args = append(args, "--base", req.Base)
	}
	if req.Head != "" {
		if err := validateBranch(req.Head); err != nil {
			return nil, err
		}
		args = append(args, "--head", req.Head)
	}
	if req.Draft {
		args = append(args, "--draft")
	}
	out, err := s.run(ctx, dir, "", args...)
	if err != nil {
		return nil, ghErr("open a pull request", out)
	}
	url := lastURL(out)
	if url == "" {
		return nil, fmt.Errorf("gh did not report a pull request URL: %s", firstMeaningfulLine(out))
	}
	return &PullRequest{Title: title, URL: url, Base: req.Base, Head: req.Head, Draft: req.Draft, State: "open"}, nil
}

// validateBranch is the same rule gitx applies to a ref, restated here because
// these names reach gh rather than git and the two packages must not have to
// import each other to agree about it.
func validateBranch(name string) error {
	if name == "" || len(name) > 255 || strings.HasPrefix(name, "-") || strings.Contains(name, "..") {
		return fmt.Errorf("branch name is not allowed: %q", name)
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case strings.ContainsRune("._-/@+", r):
		default:
			return fmt.Errorf("branch name is not allowed: %q", name)
		}
	}
	return nil
}

func lastURL(out string) string {
	found := ""
	for _, line := range strings.Fields(out) {
		if strings.HasPrefix(line, "https://") {
			found = strings.Trim(line, `"'.,`)
		}
	}
	return found
}

// ghErr keeps gh's own words. They are better than a paraphrase — "must be on
// a branch named differently than the base" is the whole diagnosis — and the
// verb says which operation produced them.
func ghErr(what, out string) error {
	msg := firstMeaningfulLine(out)
	if msg == "" {
		msg = "gh reported no reason"
	}
	return fmt.Errorf("could not %s: %s", what, msg)
}

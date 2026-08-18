package procs

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// CronJob is one schedule line. Comment and environment lines are preserved
// separately so the editor can round-trip a crontab without destroying it.
type CronJob struct {
	Line     int    `json:"line"`
	Schedule string `json:"schedule"`
	Command  string `json:"command"`
	Comment  string `json:"comment,omitempty"`
	Raw      string `json:"raw"`
	Disabled bool   `json:"disabled"`
}

type Crontab struct {
	User    string    `json:"user"`
	Source  string    `json:"source"`
	Raw     string    `json:"raw"`
	Jobs    []CronJob `json:"jobs"`
	Env     []string  `json:"env"`
	Comment []string  `json:"comments"`
}

type Cron struct{}

func NewCron() *Cron { return &Cron{} }

func (c *Cron) Available() bool { return binaryExists("crontab") }

// UserCrontab reads one user's crontab through the crontab command rather than
// /var/spool, so the platform's own permission and locking rules apply.
func (c *Cron) UserCrontab(ctx context.Context, user string) (*Crontab, error) {
	if err := ValidateName(user); err != nil {
		return nil, err
	}
	res, err := run(ctx, 15*time.Second, "crontab", "-u", user, "-l")
	if err != nil {
		// An empty crontab exits non-zero with this message; that is not an
		// error condition for a viewer.
		if strings.Contains(res.Stderr, "no crontab for") {
			return &Crontab{User: user, Source: "crontab -u " + user, Jobs: []CronJob{}, Env: []string{}, Comment: []string{}}, nil
		}
		return nil, err
	}
	ct := parseCrontab(res.Stdout)
	ct.User = user
	ct.Source = "crontab -u " + user
	return ct, nil
}

func (c *Cron) SetUserCrontab(ctx context.Context, user, content string) error {
	if err := ValidateName(user); err != nil {
		return err
	}
	if err := ValidateCrontab(content); err != nil {
		return err
	}
	tmp, err := os.CreateTemp("", "vpsd-cron-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()
	_, err = run(ctx, 15*time.Second, "crontab", "-u", user, tmp.Name())
	return err
}

// ValidateCrontab catches malformed schedules before they are installed.
// crontab itself validates on write, but rejecting here means the operator
// sees which line is wrong instead of a generic failure.
func ValidateCrontab(content string) error {
	for i, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.Contains(trimmed, "=") && !strings.HasPrefix(trimmed, "@") {
			if idx := strings.Index(trimmed, "="); idx > 0 && !strings.ContainsAny(trimmed[:idx], " \t") {
				continue // environment assignment
			}
		}
		if strings.HasPrefix(trimmed, "@") {
			continue
		}
		if len(strings.Fields(trimmed)) < 6 {
			return fmt.Errorf("line %d is not a valid cron entry: %q", i+1, trimmed)
		}
	}
	return nil
}

func parseCrontab(content string) *Crontab {
	ct := &Crontab{Raw: content, Jobs: []CronJob{}, Env: []string{}, Comment: []string{}}
	sc := bufio.NewScanner(strings.NewReader(content))
	lineNo := 0
	var pendingComment string
	for sc.Scan() {
		lineNo++
		raw := sc.Text()
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			body := strings.TrimSpace(strings.TrimPrefix(trimmed, "#"))
			// A commented-out schedule is a disabled job, not documentation.
			if job, ok := splitCronLine(body); ok {
				job.Line = lineNo
				job.Raw = raw
				job.Disabled = true
				job.Comment = pendingComment
				pendingComment = ""
				ct.Jobs = append(ct.Jobs, job)
				continue
			}
			pendingComment = body
			ct.Comment = append(ct.Comment, body)
			continue
		}
		if idx := strings.Index(trimmed, "="); idx > 0 && !strings.ContainsAny(trimmed[:idx], " \t") {
			ct.Env = append(ct.Env, trimmed)
			continue
		}
		if job, ok := splitCronLine(trimmed); ok {
			job.Line = lineNo
			job.Raw = raw
			job.Comment = pendingComment
			pendingComment = ""
			ct.Jobs = append(ct.Jobs, job)
		}
	}
	return ct
}

func splitCronLine(line string) (CronJob, bool) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return CronJob{}, false
	}
	if strings.HasPrefix(fields[0], "@") {
		if len(fields) < 2 {
			return CronJob{}, false
		}
		return CronJob{Schedule: fields[0], Command: strings.Join(fields[1:], " ")}, true
	}
	if len(fields) < 6 {
		return CronJob{}, false
	}
	return CronJob{
		Schedule: strings.Join(fields[:5], " "),
		Command:  strings.Join(fields[5:], " "),
	}, true
}

// SystemCronFiles lists the drop-in crontabs that ship with packages. They are
// read-only here: editing them belongs to the package manager, and the file
// manager can be used deliberately if an operator really means to.
func (c *Cron) SystemCronFiles(ctx context.Context) ([]Crontab, error) {
	out := []Crontab{}
	paths := []string{"/etc/crontab"}
	for _, dir := range []string{"/etc/cron.d"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
				paths = append(paths, filepath.Join(dir, e.Name()))
			}
		}
	}
	sort.Strings(paths)
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		ct := parseCrontab(string(b))
		ct.Source = p
		out = append(out, *ct)
	}
	return out, nil
}

// ListCrontabUsers returns users that have a personal crontab, so the UI can
// offer a picker instead of asking the operator to guess.
func (c *Cron) ListCrontabUsers(ctx context.Context) ([]string, error) {
	users := []string{}
	for _, spool := range []string{"/var/spool/cron/crontabs", "/var/spool/cron"} {
		entries, err := os.ReadDir(spool)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				users = append(users, e.Name())
			}
		}
		if len(users) > 0 {
			break
		}
	}
	sort.Strings(users)
	return users, nil
}

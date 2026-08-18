// Package logsx unifies the log sources on a host — syslog, nginx, PM2,
// application files and the journal — behind one tailing and search API.
package logsx

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/nxadm/tail"
)

// Service reads log files. Reads are confined to the configured roots: the
// file manager is the deliberate way to read arbitrary paths, and a log
// viewer that could open /etc/shadow would be a privilege escalation dressed
// up as a feature.
type Service struct {
	roots []string
}

func New(roots []string) *Service {
	cleaned := make([]string, 0, len(roots))
	for _, r := range roots {
		if abs, err := filepath.Abs(r); err == nil {
			cleaned = append(cleaned, filepath.Clean(abs))
		}
	}
	return &Service{roots: cleaned}
}

// Allow reports whether a path is inside a configured root, after resolving
// symlinks — otherwise a symlink planted in /var/log would defeat the check.
func (s *Service) Allow(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		// A path that does not exist yet is judged on its literal form; a
		// rotated-away log file is a normal case.
		resolved = filepath.Clean(abs)
	}
	for _, root := range s.roots {
		if resolved == root || strings.HasPrefix(resolved, root+string(os.PathSeparator)) {
			return nil
		}
	}
	return fmt.Errorf("path %q is outside the configured log roots", path)
}

// AllowExtra permits a path discovered from a trusted source (a PM2 process
// definition, an nginx config) that lies outside the log roots.
func (s *Service) AllowExtra(path string) {
	if abs, err := filepath.Abs(path); err == nil {
		s.roots = append(s.roots, filepath.Clean(abs))
	}
}

// Tail follows a file, emitting the last n lines first and then new ones as
// they arrive. It survives rotation: tail reopens the path when the inode
// changes, which is what logrotate does nightly.
func (s *Service) Tail(ctx context.Context, path string, lines int) (<-chan string, error) {
	if err := s.Allow(path); err != nil {
		return nil, err
	}
	if lines <= 0 || lines > 20000 {
		lines = 300
	}
	offset, err := seekBackLines(path, lines)
	if err != nil {
		return nil, err
	}
	t, err := tail.TailFile(path, tail.Config{
		Follow:    true,
		ReOpen:    true,
		MustExist: true,
		Location:  &tail.SeekInfo{Offset: offset, Whence: io.SeekStart},
		Logger:    tail.DiscardingLogger,
	})
	if err != nil {
		return nil, err
	}
	out := make(chan string, 256)
	go func() {
		defer close(out)
		defer t.Cleanup()
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case line, ok := <-t.Lines:
				if !ok {
					return
				}
				if line.Err != nil {
					continue
				}
				select {
				case <-ctx.Done():
					return
				case out <- line.Text:
				}
			}
		}
	}()
	return out, nil
}

// TailInto is the callback form used where the caller already owns a channel,
// such as the PM2 viewer merging stdout and stderr into one stream.
func (s *Service) TailInto(ctx context.Context, path string, lines int, fn func(string)) {
	ch, err := s.Tail(ctx, path, lines)
	if err != nil {
		return
	}
	for line := range ch {
		fn(line)
	}
}

// seekBackLines finds the byte offset n lines from the end, so tailing a
// multi-gigabyte log does not read the whole file into memory.
func seekBackLines(path string, n int) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return 0, err
	}
	size := st.Size()
	const chunk = 64 * 1024
	var (
		offset = size
		count  int
		buf    = make([]byte, chunk)
	)
	for offset > 0 && count <= n {
		read := int64(chunk)
		if offset < read {
			read = offset
		}
		offset -= read
		if _, err := f.ReadAt(buf[:read], offset); err != nil && err != io.EOF {
			return 0, err
		}
		for i := int(read) - 1; i >= 0; i-- {
			if buf[i] == '\n' {
				count++
				if count > n {
					return offset + int64(i) + 1, nil
				}
			}
		}
	}
	return 0, nil
}

// Line is a parsed log record. Level is best-effort: log formats on a typical
// server are not uniform, so the parser recognises the common shapes and
// leaves the rest unclassified rather than guessing.
type Line struct {
	Text      string     `json:"text"`
	Level     string     `json:"level,omitempty"`
	Timestamp *time.Time `json:"timestamp,omitempty"`
	Source    string     `json:"source,omitempty"`
}

var levelPattern = regexp.MustCompile(`(?i)\b(emerg|alert|crit|critical|fatal|error|err|warn|warning|notice|info|debug|trace)\b`)

// Normalise ranks a level onto a fixed scale so one filter works across
// syslog priorities, nginx's error levels and application JSON logs.
func Normalise(level string) string {
	switch strings.ToLower(level) {
	case "emerg", "alert", "crit", "critical", "fatal":
		return "critical"
	case "error", "err":
		return "error"
	case "warn", "warning":
		return "warn"
	case "notice", "info":
		return "info"
	case "debug", "trace":
		return "debug"
	}
	return ""
}

func ParseLine(text, source string) Line {
	l := Line{Text: text, Source: source}
	if m := levelPattern.FindStringSubmatch(text); m != nil {
		l.Level = Normalise(m[1])
	}
	if ts, ok := parseTimestamp(text); ok {
		l.Timestamp = &ts
	}
	return l
}

// Timestamp formats seen in the wild: syslog's "Jan  2 15:04:05", nginx's
// bracketed "02/Jan/2006:15:04:05 -0700", and ISO-8601 from anything modern.
var timeLayouts = []struct {
	layout string
	length int
}{
	{"2006-01-02T15:04:05Z07:00", 25},
	{"2006-01-02T15:04:05", 19},
	{"2006-01-02 15:04:05", 19},
	{"Jan  2 15:04:05", 15},
	{"Jan 02 15:04:05", 15},
}

func parseTimestamp(text string) (time.Time, bool) {
	if len(text) == 0 {
		return time.Time{}, false
	}
	if start := strings.Index(text, "["); start >= 0 {
		if end := strings.Index(text[start:], "]"); end > 1 {
			if t, err := time.Parse("02/Jan/2006:15:04:05 -0700", text[start+1:start+end]); err == nil {
				return t.UTC(), true
			}
		}
	}
	for _, l := range timeLayouts {
		if len(text) < l.length {
			continue
		}
		t, err := time.Parse(l.layout, strings.TrimSpace(text[:l.length]))
		if err != nil {
			continue
		}
		// Syslog omits the year; assume the current one, correcting backwards
		// when that would place the entry in the future.
		if t.Year() == 0 {
			now := time.Now()
			t = t.AddDate(now.Year(), 0, 0)
			if t.After(now.Add(24 * time.Hour)) {
				t = t.AddDate(-1, 0, 0)
			}
		}
		return t.UTC(), true
	}
	return time.Time{}, false
}

type SearchOptions struct {
	Query      string
	Regex      bool
	IgnoreCase bool
	Levels     []string
	Since      time.Time
	Until      time.Time
	Limit      int
}

type SearchResult struct {
	Lines     []Line `json:"lines"`
	Scanned   int    `json:"scanned"`
	Matched   int    `json:"matched"`
	Truncated bool   `json:"truncated"`
}

// Search greps a file server-side. Doing it here rather than shipping the
// whole file to the browser is the difference between a usable viewer and
// a 2 GB download.
func (s *Service) Search(ctx context.Context, path string, opts SearchOptions) (*SearchResult, error) {
	if err := s.Allow(path); err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var matcher func(string) bool
	switch {
	case opts.Query == "":
		matcher = func(string) bool { return true }
	case opts.Regex:
		expr := opts.Query
		if opts.IgnoreCase {
			expr = "(?i)" + expr
		}
		re, err := regexp.Compile(expr)
		if err != nil {
			return nil, fmt.Errorf("invalid regular expression: %w", err)
		}
		matcher = re.MatchString
	case opts.IgnoreCase:
		needle := strings.ToLower(opts.Query)
		matcher = func(s string) bool { return strings.Contains(strings.ToLower(s), needle) }
	default:
		matcher = func(s string) bool { return strings.Contains(s, opts.Query) }
	}

	levelSet := map[string]bool{}
	for _, l := range opts.Levels {
		if n := Normalise(l); n != "" {
			levelSet[n] = true
		}
	}
	limit := opts.Limit
	if limit <= 0 || limit > 20000 {
		limit = 2000
	}

	res := &SearchResult{Lines: []Line{}}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		res.Scanned++
		text := sc.Text()
		if !matcher(text) {
			continue
		}
		line := ParseLine(text, filepath.Base(path))
		if len(levelSet) > 0 && !levelSet[line.Level] {
			continue
		}
		if !opts.Since.IsZero() && (line.Timestamp == nil || line.Timestamp.Before(opts.Since)) {
			continue
		}
		if !opts.Until.IsZero() && (line.Timestamp == nil || line.Timestamp.After(opts.Until)) {
			continue
		}
		res.Matched++
		res.Lines = append(res.Lines, line)
		if len(res.Lines) > limit {
			// Keep the most recent window rather than the first N: when a
			// log is being searched, the tail is what matters.
			res.Lines = res.Lines[1:]
			res.Truncated = true
		}
	}
	return res, sc.Err()
}

// Range extracts the lines that fall inside a time window, which is what the
// "download by date range" action serves.
func (s *Service) Range(ctx context.Context, path string, since, until time.Time, w io.Writer) (int, error) {
	if err := s.Allow(path); err != nil {
		return 0, err
	}
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	written := 0
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		if ctx.Err() != nil {
			return written, ctx.Err()
		}
		text := sc.Text()
		ts, ok := parseTimestamp(text)
		// Lines without a recognisable timestamp are continuations of the
		// previous record (stack traces, wrapped messages) and are kept.
		if ok {
			if !since.IsZero() && ts.Before(since) {
				continue
			}
			if !until.IsZero() && ts.After(until) {
				continue
			}
		}
		if _, err := io.WriteString(w, text+"\n"); err != nil {
			return written, err
		}
		written++
	}
	return written, sc.Err()
}

func sortStrings(in []string) []string {
	sort.Strings(in)
	return in
}

package netsec

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/process"

	"github.com/Wayy01/Just-Dashboard/backend/internal/hostexec"
)

// LoginSession is one interactive login currently on the machine. Both `who`
// and the sshd process table are consulted: `who` knows about the tty and
// login time, and the process list attributes each session to a PID that can
// actually be killed.
type LoginSession struct {
	User      string     `json:"user"`
	TTY       string     `json:"tty"`
	From      string     `json:"from"`
	LoginTime *time.Time `json:"loginTime,omitempty"`
	Idle      string     `json:"idle,omitempty"`
	PID       int32      `json:"pid,omitempty"`
	Command   string     `json:"command,omitempty"`
	IsSSH     bool       `json:"isSsh"`
}

func (s *Service) Sessions(ctx context.Context) ([]LoginSession, error) {
	sessions := parseWho(ctx)
	sshProcs := sshdSessions(ctx)

	// Match a `who` row to its sshd process by user and, where the process
	// title carries it, by terminal — sshd writes "user@pts/0" into its title.
	for i := range sessions {
		for _, p := range sshProcs {
			if p.User == sessions[i].User &&
				(strings.Contains(p.Command, sessions[i].TTY) || sessions[i].TTY == "") {
				sessions[i].PID = p.PID
				sessions[i].Command = p.Command
				sessions[i].IsSSH = true
				break
			}
		}
		if sessions[i].From != "" && sessions[i].From != ":0" {
			sessions[i].IsSSH = true
		}
	}
	sort.Slice(sessions, func(i, j int) bool {
		if sessions[i].User != sessions[j].User {
			return sessions[i].User < sessions[j].User
		}
		return sessions[i].TTY < sessions[j].TTY
	})
	return sessions, nil
}

func parseWho(ctx context.Context) []LoginSession {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	// Login sessions live in the host's namespaces: modern systemd tracks
	// them under /run/systemd/sessions rather than utmp, and a container's
	// own view of both is empty. Running who on the host is what makes this
	// list the server's sessions instead of this container's.
	cmd := hostexec.CommandOnHost(ctx, "who", "-u")
	// The date is being parsed, so the locale that formats it has to be the
	// one this code expects.
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil {
		return []LoginSession{}
	}
	sessions := []LoginSession{}
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		if sess, ok := parseWhoLine(sc.Text()); ok {
			sessions = append(sessions, sess)
		}
	}
	return sessions
}

var (
	// The two date shapes `who` prints. Which one appears is the locale's
	// choice: C gives `Aug 27`, and the C.UTF-8 that Ubuntu and Debian
	// default to gives `2026-08-27`. LC_ALL=C is set for determinism and both
	// are accepted anyway, because a locale is the kind of thing that is set
	// somewhere else on somebody's server.
	whoISODateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	whoMonthRe   = regexp.MustCompile(`^(Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)$`)
	whoDayRe     = regexp.MustCompile(`^\d{1,2}$`)
)

// whoDateAt reports whether a date starts at i, and how many fields it spans.
func whoDateAt(fields []string, i int) (int, bool) {
	if whoISODateRe.MatchString(fields[i]) {
		return 1, true
	}
	if whoMonthRe.MatchString(fields[i]) && i+1 < len(fields) && whoDayRe.MatchString(fields[i+1]) {
		return 2, true
	}
	return 0, false
}

// whoTime parses the timestamp out of the fields the date spans plus the one
// after it. The C locale's form carries no year, so the current one is assumed
// and rolled back if that puts the login in the future — a session that began
// in December is otherwise reported as one that has not happened yet.
func whoTime(fields []string, start, span int, now time.Time) (time.Time, bool) {
	clock := fields[start+span]
	if span == 1 {
		ts, err := time.Parse("2006-01-02 15:04", fields[start]+" "+clock)
		return ts.UTC(), err == nil
	}
	ts, err := time.Parse("2006 Jan 2 15:04",
		fmt.Sprintf("%d %s %s %s", now.Year(), fields[start], fields[start+1], clock))
	if err != nil {
		return time.Time{}, false
	}
	if ts.After(now.Add(24 * time.Hour)) {
		ts = ts.AddDate(-1, 0, 0)
	}
	return ts.UTC(), true
}

// parseWhoLine reads one row of `who -u`, finding the columns by the date
// rather than by counting from the left.
//
// The nominal shape is `user line date time idle pid (comment)`, and counting
// works right up until the line column is two words — which is what OpenSSH 9
// writes on a current Ubuntu:
//
//	ubuntu   sshd pts/3   2026-08-27 02:07   .       2392157 (77.89.203.46)
//	ubuntu   pts/2        2026-08-26 21:33 03:03     2262305
//
// Read positionally, the first row's terminal is "sshd", its login time is
// unparseable, and its PID lands nowhere — so the one session an operator
// would ever want to end was the one with no process to end. The date is the
// only field with a shape of its own, so it is what the row is read around.
func parseWhoLine(line string) (LoginSession, bool) {
	fields := strings.Fields(line)
	if len(fields) < 4 {
		return LoginSession{}, false
	}
	date, span := -1, 0
	for i := 1; i < len(fields)-1; i++ {
		if n, ok := whoDateAt(fields, i); ok && i+n < len(fields) {
			date, span = i, n
			break
		}
	}
	if date < 2 {
		return LoginSession{}, false
	}
	// Everything between the user and the date is the line column. The last
	// word of it is the terminal; anything before it is what wrote the entry.
	sess := LoginSession{User: fields[0], TTY: fields[date-1]}
	if ts, ok := whoTime(fields, date, span, time.Now()); ok {
		sess.LoginTime = &ts
	}
	rest := fields[date+span+1:]
	if len(rest) > 0 {
		// "." is who's word for "active now", which is worth saying rather
		// than showing a full stop in a column headed Idle.
		if rest[0] == "." {
			sess.Idle = "active"
		} else if rest[0] != "old" {
			sess.Idle = rest[0]
		}
	}
	for _, f := range rest {
		if pid, err := strconv.Atoi(f); err == nil && sess.PID == 0 {
			sess.PID = int32(pid)
		}
		if strings.HasPrefix(f, "(") && strings.HasSuffix(f, ")") {
			sess.From = strings.Trim(f, "()")
		}
	}
	return sess, true
}

type sshdProc struct {
	PID     int32
	User    string
	Command string
}

func sshdSessions(ctx context.Context) []sshdProc {
	procs, err := process.ProcessesWithContext(ctx)
	if err != nil {
		return nil
	}
	out := []sshdProc{}
	for _, p := range procs {
		name, err := p.NameWithContext(ctx)
		if err != nil || name != "sshd" {
			continue
		}
		cmd, _ := p.CmdlineWithContext(ctx)
		// The listener has no user in its title; only per-session children do.
		if !strings.Contains(cmd, "@") {
			continue
		}
		user, _ := p.UsernameWithContext(ctx)
		out = append(out, sshdProc{PID: p.Pid, User: user, Command: cmd})
	}
	return out
}

// Disconnect ends one interactive login.
//
// The guard is the whole feature: the PID is looked up in the session list
// this package just built, and anything not on it is refused. Without that
// this route is a "kill any process on the host" primitive wearing a sensible
// name — the same reasoning that keeps client-supplied paths behind
// files.Resolve.
//
// SIGHUP rather than SIGKILL, because a hangup is what a dropped connection
// looks like to a shell: the session's children get the signal too and the
// login is recorded as ended rather than as a process that vanished.
func (s *Service) Disconnect(ctx context.Context, pid int32) (LoginSession, error) {
	if pid <= 1 {
		return LoginSession{}, fmt.Errorf("invalid process id")
	}
	sessions, err := s.Sessions(ctx)
	if err != nil {
		return LoginSession{}, err
	}
	var target LoginSession
	for _, sess := range sessions {
		if sess.PID == pid {
			target = sess
			break
		}
	}
	if target.PID == 0 {
		return LoginSession{}, fmt.Errorf("no interactive login is running as process %d", pid)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if out, err := hostexec.CommandOnHost(ctx, "kill", "-HUP", strconv.Itoa(int(pid))).CombinedOutput(); err != nil {
		return target, fmt.Errorf("could not end the session: %s", strings.TrimSpace(string(out)))
	}
	return target, nil
}

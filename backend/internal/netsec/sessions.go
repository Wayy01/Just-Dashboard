package netsec

import (
	"bufio"
	"context"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/process"
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
	out, err := exec.CommandContext(ctx, "who", "-u").Output()
	if err != nil {
		return []LoginSession{}
	}
	sessions := []LoginSession{}
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 4 {
			continue
		}
		sess := LoginSession{User: fields[0], TTY: fields[1]}
		// who -u prints: user tty date time idle pid (comment)
		if ts, err := time.Parse("2006-01-02 15:04", fields[2]+" "+fields[3]); err == nil {
			t := ts.UTC()
			sess.LoginTime = &t
		}
		if len(fields) >= 5 {
			sess.Idle = fields[4]
		}
		if len(fields) >= 6 {
			if pid, err := strconv.Atoi(fields[5]); err == nil {
				sess.PID = int32(pid)
			}
		}
		for _, f := range fields[4:] {
			if strings.HasPrefix(f, "(") && strings.HasSuffix(f, ")") {
				sess.From = strings.Trim(f, "()")
			}
		}
		sessions = append(sessions, sess)
	}
	return sessions
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

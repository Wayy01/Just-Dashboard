// Package term runs real PTY sessions and multiplexes them over WebSockets.
//
// This is the most dangerous surface in the dashboard: a shell here is a shell
// on the host, with whatever privileges this process holds. It is gated on the
// terminal capability, can be disabled outright in configuration, and every
// session open, attach and close is written to the audit trail.
package term

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wayy01/Just-Dashboard/backend/internal/hostexec"
	"github.com/creack/pty"
)

var (
	ErrDisabled = errors.New("the web terminal is disabled in this dashboard's configuration")
	ErrNotFound = errors.New("terminal session not found")
	ErrTooMany  = errors.New("too many terminal sessions are already open")
	// Twelve was a guess made when a session was a thing you opened and
	// closed. They are now kept — named, grouped, running for weeks — so the
	// cap has to be a number of *workspaces* rather than of visits, and the
	// cost of an idle one is a PTY and a goroutine.
	maxSessions  = 32
	scrollbackKB = 128
)

// Session is one PTY. Output is fanned out to every attached client and also
// kept in a bounded scrollback buffer, so reopening a tab restores what was on
// screen instead of an empty terminal.
type Session struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Shell string `json:"shell"`
	// User is the host account the shell runs as, which is the answer to
	// "whoami" without having to open the session and ask.
	User      string    `json:"user"`
	Persisted bool      `json:"persisted"`
	TmuxName  string    `json:"tmuxName,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	Owner     string    `json:"owner"`
	Rows      uint16    `json:"rows"`
	Cols      uint16    `json:"cols"`
	PID       int       `json:"pid"`
	// CWDHint is the directory the session was asked to start in, kept for the
	// case where /proc cannot answer.
	CWDHint string `json:"-"`

	mu          sync.Mutex
	pty         *os.File
	cmd         *exec.Cmd
	subscribers map[int64]chan []byte
	nextSub     atomic.Int64
	scrollback  *ringBuffer
	closed      bool
	lastActive  time.Time
}

func (s *Session) Attached() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.subscribers)
}

// Size reads the window under the lock Resize writes it under. The listing
// handler runs on a different goroutine from the socket that resizes, so
// reading the fields directly was a race the detector flags — benign on amd64,
// but there is no reason to keep it.
func (s *Session) Size() (rows, cols uint16) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Rows, s.Cols
}

func (s *Session) LastActive() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastActive
}

// Subscribe returns the scrollback plus a channel of subsequent output. The
// snapshot and the subscription are taken under the same lock so no output can
// slip between them.
func (s *Session) Subscribe() (snapshot []byte, id int64, ch chan []byte, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, 0, nil, ErrNotFound
	}
	id = s.nextSub.Add(1)
	ch = make(chan []byte, 64)
	s.subscribers[id] = ch
	return s.scrollback.Bytes(), id, ch, nil
}

func (s *Session) Unsubscribe(id int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ch, ok := s.subscribers[id]; ok {
		delete(s.subscribers, id)
		close(ch)
	}
}

func (s *Session) Write(p []byte) (int, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return 0, ErrNotFound
	}
	s.lastActive = time.Now()
	f := s.pty
	s.mu.Unlock()
	return f.Write(p)
}

func (s *Session) Resize(rows, cols uint16) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrNotFound
	}
	s.Rows, s.Cols = rows, cols
	return pty.Setsize(s.pty, &pty.Winsize{Rows: rows, Cols: cols})
}

// broadcast delivers output to attached clients. A client that cannot keep up
// is skipped rather than blocking the reader: a stalled browser must not be
// able to wedge the PTY for everyone else.
func (s *Session) broadcast(chunk []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastActive = time.Now()
	s.scrollback.Write(chunk)
	for _, ch := range s.subscribers {
		select {
		case ch <- chunk:
		default:
		}
	}
}

func (s *Session) readLoop(onExit func()) {
	buf := make([]byte, 32*1024)
	for {
		n, err := s.pty.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			s.broadcast(chunk)
		}
		if err != nil {
			onExit()
			return
		}
	}
}

// Close tears down the PTY. A tmux-backed session keeps running inside tmux;
// closing here only detaches this dashboard's view of it, which is what makes
// a session survive a browser reload or a dashboard restart.
func (s *Session) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	for id, ch := range s.subscribers {
		delete(s.subscribers, id)
		close(ch)
	}
	f, cmd := s.pty, s.cmd
	s.mu.Unlock()

	if f != nil {
		f.Close()
	}
	if cmd != nil && cmd.Process != nil {
		cmd.Process.Kill()
		cmd.Wait()
	}
	return nil
}

// CWD reports the shell's current directory, which is what in-session file
// upload and download resolve relative paths against.
func (s *Session) CWD() string {
	s.mu.Lock()
	pid, tmuxName := s.PID, s.TmuxName
	s.mu.Unlock()

	// A tmux-backed session is the one case the process walk below cannot
	// answer. What we spawned is the tmux *client*; the shell is a child of
	// the tmux **server**, in a different process tree entirely, so following
	// our own descendants finds the client and reports its directory — which
	// is `/`, for every session, however the operator has navigated. tmux
	// tracks the pane's directory across every `cd`, so ask the thing that
	// knows.
	if tmuxName != "" {
		if dir := tmuxPanePath(tmuxName); dir != "" {
			return dir
		}
	}
	if pid == 0 {
		return ""
	}
	// The process we spawned is no longer the shell: a session is nsenter,
	// then su, then the login shell, and each of those only forwards to the
	// next. Their cwd never changes, so reading the leader's would pin this to
	// wherever the session started and quietly stop tracking `cd` — the one
	// thing this function exists to follow.
	//
	// Walking down to the innermost descendant finds the shell again, and
	// keeps finding it when the shell itself forks (tmux adds another layer,
	// and a running command adds one more).
	if link, err := os.Readlink(fmt.Sprintf("/proc/%d/cwd", leafDescendant(pid))); err == nil {
		return link
	}
	return s.CWDHint
}

// leafDescendant follows the single-child chain from pid to its innermost
// process. It stops where the chain branches, because two children mean there
// is no longer one obvious "the shell" to follow, and the last unambiguous
// process is a better answer than an arbitrary one of them.
//
// The container shares the host's PID namespace, so these are host PIDs and
// /proc is the host's process table — the same reason the process list page
// can see the server's services.
func leafDescendant(pid int) int {
	// Bounded so that a pathological chain, or a /proc that starts pointing at
	// itself, cannot spin here.
	for depth := 0; depth < 16; depth++ {
		kids := childrenOf(pid)
		if len(kids) != 1 {
			return pid
		}
		pid = kids[0]
	}
	return pid
}

func childrenOf(pid int) []int {
	// The kernel exposes children per-thread; a shell is single-threaded in
	// practice, but reading the whole task directory is what makes this
	// correct for anything that is not.
	tasks, err := os.ReadDir(fmt.Sprintf("/proc/%d/task", pid))
	if err != nil {
		return nil
	}
	var out []int
	for _, t := range tasks {
		raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/task/%s/children", pid, t.Name()))
		if err != nil {
			continue
		}
		for _, f := range strings.Fields(string(raw)) {
			if n, err := strconv.Atoi(f); err == nil {
				out = append(out, n)
			}
		}
	}
	return out
}

// ringBuffer keeps the most recent output for reattachment, bounded so a
// process printing endlessly cannot grow the dashboard's heap.
type ringBuffer struct {
	buf  []byte
	size int
}

func newRingBuffer(size int) *ringBuffer {
	return &ringBuffer{buf: make([]byte, 0, size), size: size}
}

func (r *ringBuffer) Write(p []byte) {
	if len(p) >= r.size {
		r.buf = append(r.buf[:0], p[len(p)-r.size:]...)
		return
	}
	if len(r.buf)+len(p) > r.size {
		drop := len(r.buf) + len(p) - r.size
		r.buf = append(r.buf[:0], r.buf[drop:]...)
	}
	r.buf = append(r.buf, p...)
}

func (r *ringBuffer) Bytes() []byte {
	out := make([]byte, len(r.buf))
	copy(out, r.buf)
	return out
}

// tmuxPanePath asks tmux where a session's active pane currently is.
//
// One short-lived subprocess, and only on the paths that need it: this is
// called when an in-session upload or download has to resolve a relative path,
// not on a poll.
func tmuxPanePath(name string) string {
	cmd := hostexec.CommandOnHost(context.Background(), "tmux",
		"display-message", "-p", "-t", name, "#{pane_current_path}")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

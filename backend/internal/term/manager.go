package term

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"os/exec"
	"sort"
	"sync"
	"time"

	"github.com/creack/pty"
)

type Manager struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	enabled  bool
	shell    string
	useTmux  bool
}

func NewManager(enabled bool, shell string) *Manager {
	m := &Manager{
		sessions: map[string]*Session{},
		enabled:  enabled,
		shell:    shell,
	}
	if _, err := exec.LookPath("tmux"); err == nil {
		m.useTmux = true
	}
	go m.reap()
	return m
}

func (m *Manager) Enabled() bool       { return m.enabled }
func (m *Manager) TmuxAvailable() bool { return m.useTmux }

type CreateOptions struct {
	Title   string
	Owner   string
	Rows    uint16
	Cols    uint16
	CWD     string
	Persist bool
}

// Create spawns a session. When tmux is present and persistence is requested
// the shell runs inside `tmux new-session -A`, so the session outlives both the
// browser tab and a restart of this process — the same guarantee an operator
// gets from SSH plus tmux, which is why it is worth the extra layer.
func (m *Manager) Create(ctx context.Context, opts CreateOptions) (*Session, error) {
	if !m.enabled {
		return nil, ErrDisabled
	}
	m.mu.Lock()
	if len(m.sessions) >= maxSessions {
		m.mu.Unlock()
		return nil, ErrTooMany
	}
	m.mu.Unlock()

	id := randomID()
	if opts.Rows == 0 {
		opts.Rows = 24
	}
	if opts.Cols == 0 {
		opts.Cols = 80
	}
	shell := m.shell
	if _, err := os.Stat(shell); err != nil {
		shell = "/bin/sh"
	}

	sess := &Session{
		ID:          id,
		Title:       defaultTitle(opts.Title),
		Shell:       shell,
		CreatedAt:   time.Now().UTC(),
		Owner:       opts.Owner,
		Rows:        opts.Rows,
		Cols:        opts.Cols,
		subscribers: map[int64]chan []byte{},
		scrollback:  newRingBuffer(scrollbackKB * 1024),
		lastActive:  time.Now(),
	}

	var cmd *exec.Cmd
	if m.useTmux && opts.Persist {
		sess.TmuxName = "vpsd-" + id
		sess.Persisted = true
		cmd = exec.Command("tmux", "new-session", "-A", "-s", sess.TmuxName, shell)
	} else {
		cmd = exec.Command(shell, "-l")
	}
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"VPSD_SESSION="+id,
	)
	if opts.CWD != "" {
		if st, err := os.Stat(opts.CWD); err == nil && st.IsDir() {
			cmd.Dir = opts.CWD
		}
	}

	f, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: opts.Rows, Cols: opts.Cols})
	if err != nil {
		return nil, err
	}
	sess.pty = f
	sess.cmd = cmd
	if cmd.Process != nil {
		sess.PID = cmd.Process.Pid
	}

	m.mu.Lock()
	m.sessions[id] = sess
	m.mu.Unlock()

	go sess.readLoop(func() { m.remove(id) })
	return sess, nil
}

func defaultTitle(t string) string {
	if t == "" {
		return "shell"
	}
	if len(t) > 64 {
		return t[:64]
	}
	return t
}

func (m *Manager) Get(id string) (*Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	sess, ok := m.sessions[id]
	if !ok {
		return nil, ErrNotFound
	}
	return sess, nil
}

func (m *Manager) List() []*Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

func (m *Manager) remove(id string) {
	m.mu.Lock()
	sess, ok := m.sessions[id]
	delete(m.sessions, id)
	m.mu.Unlock()
	if ok {
		sess.Close()
	}
}

// Kill ends a session for good. For a tmux-backed one the tmux session is
// destroyed too, otherwise "close" would silently leave a live shell behind.
func (m *Manager) Kill(ctx context.Context, id string) error {
	m.mu.Lock()
	sess, ok := m.sessions[id]
	delete(m.sessions, id)
	m.mu.Unlock()
	if !ok {
		return ErrNotFound
	}
	if sess.TmuxName != "" {
		exec.CommandContext(ctx, "tmux", "kill-session", "-t", sess.TmuxName).Run()
	}
	return sess.Close()
}

// Detach drops the PTY without destroying the underlying tmux session, so the
// work continues and can be reattached later.
func (m *Manager) Detach(id string) error {
	sess, err := m.Get(id)
	if err != nil {
		return err
	}
	if sess.TmuxName == "" {
		return m.Kill(context.Background(), id)
	}
	m.mu.Lock()
	delete(m.sessions, id)
	m.mu.Unlock()
	return sess.Close()
}

// TmuxSessions lists persistent sessions that exist in tmux but are not
// currently tracked here — for instance after this process was restarted.
func (m *Manager) TmuxSessions(ctx context.Context) []string {
	if !m.useTmux {
		return nil
	}
	out, err := exec.CommandContext(ctx, "tmux", "list-sessions", "-F", "#{session_name}").Output()
	if err != nil {
		return nil
	}
	names := []string{}
	for _, line := range splitLines(string(out)) {
		if len(line) > 5 && line[:5] == "vpsd-" {
			names = append(names, line)
		}
	}
	return names
}

// Reattach binds a PTY to an existing tmux session so the operator can pick up
// exactly where they left off.
func (m *Manager) Reattach(ctx context.Context, tmuxName, owner string, rows, cols uint16) (*Session, error) {
	if !m.enabled {
		return nil, ErrDisabled
	}
	if !m.useTmux {
		return nil, ErrNotFound
	}
	if rows == 0 {
		rows = 24
	}
	if cols == 0 {
		cols = 80
	}
	id := randomID()
	sess := &Session{
		ID:          id,
		Title:       tmuxName,
		Shell:       m.shell,
		TmuxName:    tmuxName,
		Persisted:   true,
		CreatedAt:   time.Now().UTC(),
		Owner:       owner,
		Rows:        rows,
		Cols:        cols,
		subscribers: map[int64]chan []byte{},
		scrollback:  newRingBuffer(scrollbackKB * 1024),
		lastActive:  time.Now(),
	}
	cmd := exec.Command("tmux", "attach-session", "-t", tmuxName)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor")
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: rows, Cols: cols})
	if err != nil {
		return nil, err
	}
	sess.pty, sess.cmd = f, cmd
	if cmd.Process != nil {
		sess.PID = cmd.Process.Pid
	}
	m.mu.Lock()
	m.sessions[id] = sess
	m.mu.Unlock()
	go sess.readLoop(func() { m.remove(id) })
	return sess, nil
}

// reap closes sessions nobody is attached to and that have seen no output for
// an hour. A forgotten root shell is a standing risk, so it is not left open
// indefinitely; tmux-backed sessions survive this because closing them only
// detaches.
func (m *Manager) reap() {
	t := time.NewTicker(5 * time.Minute)
	for range t.C {
		cutoff := time.Now().Add(-time.Hour)
		for _, s := range m.List() {
			if s.Attached() == 0 && s.LastActive().Before(cutoff) {
				if s.Persisted {
					m.Detach(s.ID)
				} else {
					m.Kill(context.Background(), s.ID)
				}
			}
		}
	}
}

func (m *Manager) Shutdown() {
	for _, s := range m.List() {
		s.Close()
	}
}

func randomID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func splitLines(s string) []string {
	out := []string{}
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

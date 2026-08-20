package term

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/Wayy01/Just-Dashboard/backend/internal/hostexec"
	"github.com/creack/pty"
)

type Manager struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	// pending counts sessions whose PTY is being spawned. Without it the cap
	// check and the insert sat in two different critical sections, so
	// concurrent creates all passed a check that had room for one of them.
	pending int
	enabled bool
	shell   string
	useTmux bool
	// The account every session runs as, resolved once at boot. accountErr
	// is kept rather than returned from NewManager so that a bad
	// JD_TERMINAL_USER disables the terminal with a precise message instead
	// of refusing to start a dashboard whose other fourteen pages are fine.
	account    Account
	accountErr error
}

// reserve takes one of the session slots, or reports that none are free. The
// returned function gives the slot back and must be called however the caller
// returns.
func (m *Manager) reserve() (release func(), err error) {
	m.mu.Lock()
	if len(m.sessions)+m.pending >= maxSessions {
		m.mu.Unlock()
		return nil, ErrTooMany
	}
	m.pending++
	m.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			m.mu.Lock()
			m.pending--
			m.mu.Unlock()
		})
	}, nil
}

func NewManager(enabled bool, shell, username string) *Manager {
	m := &Manager{
		sessions: map[string]*Session{},
		enabled:  enabled,
		shell:    shell,
	}
	m.account, m.accountErr = resolveAccount(username)
	// The host's tmux, not this image's. Sessions are created out there now,
	// so asking the image whether tmux exists would answer a question about
	// the wrong machine — and offer persistence backed by a tmux server that
	// can never see them.
	m.useTmux = hostexec.AvailableOnHost("tmux")
	go m.reap()
	return m
}

func (m *Manager) Enabled() bool       { return m.enabled }
func (m *Manager) TmuxAvailable() bool { return m.useTmux }

// Account is who sessions run as, for the listing endpoint to show. An
// operator who cannot see which account they are about to land in has to open
// a shell and run `whoami` to find out.
func (m *Manager) Account() (Account, error) { return m.account, m.accountErr }

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
	release, err := m.reserve()
	if err != nil {
		return nil, err
	}
	defer release()

	id := randomID()
	if opts.Rows == 0 {
		opts.Rows = 24
	}
	if opts.Cols == 0 {
		opts.Cols = 80
	}
	if m.accountErr != nil {
		return nil, m.accountErr
	}
	// The shell is the account's own unless the operator pinned one. Resolving
	// it here rather than in loginArgv keeps the answer on the Session, where
	// the listing endpoint can show which shell a tab is actually running.
	shell := m.shell
	if shell == "" {
		shell = m.account.Shell
	}
	if shell == "" {
		shell = "/bin/bash"
	}

	sess := &Session{
		ID:          id,
		Title:       defaultTitle(opts.Title),
		Shell:       shell,
		User:        m.account.Name,
		CreatedAt:   time.Now().UTC(),
		Owner:       opts.Owner,
		Rows:        opts.Rows,
		Cols:        opts.Cols,
		subscribers: map[int64]chan []byte{},
		scrollback:  newRingBuffer(scrollbackKB * 1024),
		lastActive:  time.Now(),
	}

	// What ssh would have run: become the account and exec its login shell.
	argv := m.account.loginArgv(m.shell)
	if m.useTmux && opts.Persist {
		sess.TmuxName = "vpsd-" + id
		sess.Persisted = true
		// tmux wraps the login rather than replacing it, so a persistent
		// session is the same session as a plain one with a multiplexer in
		// front. `-c` is what makes a requested working directory survive the
		// wrapper; without tmux, su's own cd to the home directory wins, which
		// is the ssh behaviour and the right default.
		wrap := []string{"tmux", "new-session", "-A", "-s", sess.TmuxName}
		if dir := hostDir(opts.CWD); dir != "" {
			wrap = append(wrap, "-c", dir)
			sess.CWDHint = dir
		}
		argv = append(wrap, argv...)
	}
	// CommandOnHost always crosses into the host's namespaces, even though
	// this image happens to ship bash and tmux of its own. That is the whole
	// point: the operator's tools, dotfiles and processes live out there, and
	// a shell in here would be a convincing imitation of their server rather
	// than their server.
	cmd := hostexec.CommandOnHost(context.Background(), argv[0], argv[1:]...)
	// A login resets the environment anyway; these are the two variables that
	// survive it and that the terminal on the other end depends on to render
	// colour and cursor keys correctly.
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"JD_SESSION="+id,
	)

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

// hostDir accepts a requested working directory only if it is one. The host's
// real /home, /opt, /srv and /etc are bind-mounted here under their own names,
// so this stat asks about the same directory the session will land in.
func hostDir(dir string) string {
	if dir == "" {
		return ""
	}
	if st, err := os.Stat(dir); err == nil && st.IsDir() {
		return dir
	}
	return ""
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
		hostexec.CommandOnHost(ctx, "tmux", "kill-session", "-t", sess.TmuxName).Run()
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
//
// It always returns a non-nil slice: a nil slice marshals to JSON null, and a
// list endpoint that answers null instead of [] breaks every consumer that
// reasonably expects to iterate the result.
func (m *Manager) TmuxSessions(ctx context.Context) []string {
	names := []string{}
	if !m.useTmux {
		return names
	}
	out, err := hostexec.CommandOnHost(ctx, "tmux", "list-sessions", "-F", "#{session_name}").Output()
	if err != nil {
		return names
	}
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
	// Only the sessions this dashboard created, and only ones that actually
	// exist. Passing the name straight to `tmux attach-session` let an
	// operator attach to anybody's personal tmux session on the host — the
	// listing endpoint already restricts itself to the vpsd- ones, and this is
	// the same rule applied where it is enforced rather than displayed.
	if !slices.Contains(m.TmuxSessions(ctx), tmuxName) {
		return nil, ErrNotFound
	}
	// A reattached session is a session; it counts against the same cap.
	release, err := m.reserve()
	if err != nil {
		return nil, err
	}
	defer release()
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
		User:        m.account.Name,
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
	cmd := hostexec.CommandOnHost(context.Background(), "tmux", "attach-session", "-t", tmuxName)
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

package term

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
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
	// Folder files the new session away as it is created, so a shell opened
	// from a stack lands in that stack's group without a second step.
	Folder string
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

	// Where the session should start. Empty unless the caller asked for one
	// and it is a real directory on the host, so everything below can treat a
	// non-empty value as settled.
	startDir := hostDir(opts.CWD)
	cmdDir := ""

	// What ssh would have run: become the account and exec its login shell.
	// A requested directory changes *how* the login is assembled rather than
	// being applied on top of it — see loginArgv, where a plain login's
	// chdir-to-home is the thing standing in the way.
	argv := m.account.loginArgv(m.shell, startDir != "")
	if startDir != "" {
		// The directory the command itself starts in. It has to be handed to
		// hostexec rather than set on cmd.Dir: a host command crosses into the
		// host's mount namespace via nsenter, which resets the working
		// directory to that namespace's root and discards whatever Go chdir'd
		// to on this side.
		cmdDir = startDir
		sess.CWDHint = startDir
	}
	if m.useTmux && opts.Persist {
		sess.TmuxName = sessionPrefix + id
		sess.Persisted = true
		// tmux wraps the login rather than replacing it, so a persistent
		// session is the same session as a plain one with a multiplexer in
		// front. `-c` sets the directory tmux starts the pane in, which the
		// login above is now built to keep rather than override.
		wrap := []string{"tmux", "new-session", "-A", "-s", sess.TmuxName}
		if startDir != "" {
			wrap = append(wrap, "-c", startDir)
		}
		argv = append(wrap, argv...)
	}
	// CommandOnHost always crosses into the host's namespaces, even though
	// this image happens to ship bash and tmux of its own. That is the whole
	// point: the operator's tools, dotfiles and processes live out there, and
	// a shell in here would be a convincing imitation of their server rather
	// than their server.
	cmd := hostexec.CommandOnHostInDir(context.Background(), cmdDir, argv[0], argv[1:]...)
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

	// Written onto the tmux session so it outlives this process. A session
	// that comes back after a restart with its own name on it is the whole
	// difference between "my work is still here" and a row of hex strings.
	m.rememberTitle(sess.TmuxName, sess.Title)
	// And the command every *later* window in it should run. Without this the
	// first window is the operator's account — it was given the login
	// explicitly — and every window after it is root, because tmux falls back
	// to the shell of whoever started the tmux server, which is this
	// dashboard.
	if sess.TmuxName != "" {
		m.rememberOption(sess.TmuxName, "default-command", m.defaultCommand())
	}
	if folder := sanitiseField(opts.Folder); folder != "" {
		m.rememberOption(sess.TmuxName, folderOption, folder)
	}

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

// TmuxSession is a persistent session as tmux knows it, which is the only
// record that survives this process restarting.
type TmuxSession struct {
	Name string `json:"name"`
	// Title is the name the operator gave it, stored on the tmux session
	// itself. Without it a session that outlives the dashboard comes back as
	// `vpsd-3f2a91c4` — and three of those side by side are indistinguishable,
	// which is how somebody loses track of which shell was doing what.
	Title string `json:"title,omitempty"`
	// Folder and Favourite are how a list of fifteen becomes readable. Both
	// live on the tmux session, so they survive everything the work does.
	Folder    string    `json:"folder,omitempty"`
	Favourite bool      `json:"favourite"`
	CreatedAt time.Time `json:"createdAt"`
	// CWD is where the session's active pane is now, so a detached session can
	// be identified by the work it is in the middle of.
	CWD      string `json:"cwd,omitempty"`
	Windows  int    `json:"windows"`
	Attached bool   `json:"attached"`
}

// titleOption is a tmux user option. tmux keeps anything prefixed with `@` as
// free-form metadata on the session, which makes the session itself the store
// — exactly the property needed here, since the whole point is to survive the
// process that created it.
const titleOption = "@jd_title"

// fieldSep separates the fields of one listing line.
//
// A printable character, and that is the whole point: tmux escapes
// non-printable bytes in format output, so asking it for a 0x1f separator
// returns the four literal characters `\037` and every line parses as a single
// field. A pipe survives, and is safe here because the only fields that could
// contain one are the path — which is last, and read as the remainder — and
// the title, which is stripped of it on the way in.
const fieldSep = "|"

// TmuxSessions lists persistent sessions that exist in tmux, with enough about
// each to tell them apart — typically ones left behind by a restart, or
// detached after an hour idle.
//
// One call to tmux for all of it: the format string asks for every field at
// once rather than a subprocess per session per field.
//
// It always returns a non-nil slice: a nil slice marshals to JSON null, and a
// list endpoint that answers null instead of [] breaks every consumer that
// reasonably expects to iterate the result.
func (m *Manager) TmuxSessions(ctx context.Context) []TmuxSession {
	out := []TmuxSession{}
	if !m.useTmux {
		return out
	}
	const format = "#{session_name}" + fieldSep + "#{" + titleOption + "}" +
		fieldSep + "#{" + folderOption + "}" + fieldSep + "#{" + favouriteOption + "}" +
		fieldSep + "#{session_created}" + fieldSep + "#{session_attached}" +
		fieldSep + "#{session_windows}" + fieldSep + "#{pane_current_path}"
	raw, err := hostexec.CommandOnHost(ctx, "tmux", "list-sessions", "-F", format).Output()
	if err != nil {
		// No server running is the ordinary "nothing is persisted" case, not
		// a failure worth reporting.
		return out
	}
	for _, line := range splitLines(string(raw)) {
		if sess, ok := parseTmuxLine(line); ok {
			out = append(out, sess)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

// parseTmuxLine reads one line of the listing format.
//
// SplitN, not Split, and the path deliberately last: a directory may contain
// the separator, and everything after the fifth one is still part of it. The
// four fields before it cannot — a session name is `vpsd-` and hex, three are
// numbers, and the title has the separator stripped out when it is written.
func parseTmuxLine(line string) (TmuxSession, bool) {
	const fields = 8
	f := strings.SplitN(line, fieldSep, fields)
	if len(f) < fields || !strings.HasPrefix(f[0], sessionPrefix) {
		return TmuxSession{}, false
	}
	sess := TmuxSession{
		Name: f[0], Title: f[1], Folder: f[2], Favourite: f[3] == "1",
		CWD: f[7],
	}
	if secs, err := strconv.ParseInt(f[4], 10, 64); err == nil {
		sess.CreatedAt = time.Unix(secs, 0).UTC()
	}
	sess.Attached = f[5] != "0"
	sess.Windows, _ = strconv.Atoi(f[6])
	return sess, true
}

// tmuxNames is the membership test Reattach needs, kept separate so the richer
// listing is not what authorisation depends on.
func (m *Manager) tmuxNames(ctx context.Context) []string {
	names := []string{}
	for _, s := range m.TmuxSessions(ctx) {
		names = append(names, s.Name)
	}
	return names
}

// rememberTitle writes the operator's title onto the tmux session, so it is
// still there after this process has forgotten everything.
//
// Retried, because the session may not exist yet: `tmux new-session` has only
// just been handed to a PTY, and on a host where the tmux *server* also has to
// start, the first attempt lands before there is anything to set the option on.
// That is not hypothetical — it is why the first session opened after a reboot
// came back with no title while the second was fine.
//
// Best effort throughout: a title that never sticks costs a recognisable name
// in one list, not a working session.
func (m *Manager) rememberTitle(tmuxName, title string) {
	m.rememberOption(tmuxName, titleOption, sanitiseField(title))
}

// rememberOption writes one tmux user option, retrying briefly.
func (m *Manager) rememberOption(tmuxName, option, value string) {
	if tmuxName == "" || value == "" {
		return
	}
	go func() {
		for attempt := 0; attempt < 6; attempt++ {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			err := hostexec.CommandOnHost(ctx, "tmux", "set-option", "-t", tmuxName, option, value).Run()
			cancel()
			if err == nil {
				return
			}
			time.Sleep(500 * time.Millisecond)
		}
	}()
}

// defaultCommand is the login, as the one string tmux's `default-command`
// takes. tmux runs that value through `sh -c`, so it is the one place in this
// package that produces a shell command rather than an argument vector.
//
// What it is built from is why that is acceptable: the account name comes from
// JD_TERMINAL_USER or /etc/passwd and the shell from JD_TERMINAL_SHELL or the
// account's entry — operator configuration resolved once at boot, never
// anything supplied per request. Each field is quoted anyway, so a shell path
// or account name containing a space is passed as one word rather than two.
func (m *Manager) defaultCommand() string {
	argv := m.account.loginArgv(m.shell, true)
	quoted := make([]string, 0, len(argv))
	for _, arg := range argv {
		quoted = append(quoted, shellWord(arg))
	}
	return strings.Join(quoted, " ")
}

// shellWord quotes a token so `sh -c` sees exactly one argument.
func shellWord(s string) string {
	if s == "" {
		return "''"
	}
	safe := true
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case strings.ContainsRune("_-./:=@+,", r):
		default:
			safe = false
		}
		if !safe {
			break
		}
	}
	if safe {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
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
	if !slices.Contains(m.tmuxNames(ctx), tmuxName) {
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
	// The title the operator gave it, recovered from the tmux session rather
	// than reinvented: coming back to `vpsd-3f2a91c4` is the difference
	// between picking up where you left off and guessing which shell this was.
	title := tmuxName
	for _, s := range m.TmuxSessions(ctx) {
		if s.Name == tmuxName && s.Title != "" {
			title = s.Title
		}
	}
	id := randomID()
	sess := &Session{
		ID:          id,
		Title:       title,
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
		cutoff := time.Now().Add(-idleDetach)
		for _, s := range m.List() {
			if s.Attached() > 0 || !s.LastActive().Before(cutoff) {
				continue
			}
			if s.Persisted {
				// Detach only. The tmux session, its processes and its
				// scrollback all continue and it reappears under "still
				// running" with its name — releasing the PTY and the slot
				// without ending anybody's work.
				m.Detach(s.ID)
				continue
			}
			// Nothing is holding this one but us, so releasing the PTY *is*
			// ending it. A forgotten root shell on a host with no tmux is a
			// standing risk, and there is no third option available.
			m.Kill(context.Background(), s.ID)
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

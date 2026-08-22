package term

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/Wayy01/Just-Dashboard/backend/internal/hostexec"
)

// Organising sessions, and the windows inside them.
//
// A dashboard terminal is not one shell, it is the five an operator keeps open
// — a tail here, a build there, a database console, a root shell for the thing
// that just broke. Every tool in this class stops at "a terminal": Cockpit has
// exactly one and loses it on navigation, ttyd and Wetty have no session
// concept at all, and Portainer's console is per-container and dies with the
// tab. tmux solves the *keeping* and solves none of the *finding*: `vpsd-3f2a`
// next to `vpsd-91c4` is not an answer to "which one was the migration".
//
// So the shape borrowed here is the one that works elsewhere: VS Code's
// terminal tab list, where every tab has a name you set and keeps it, and
// Guacamole's connection groups, where a long list becomes a short one by
// being folded. Favourites pin the two or three you actually live in.
//
// All of it is stored on the tmux session itself, as user options. tmux keeps
// anything prefixed with `@` as free-form metadata, which makes the session its
// own record — the same property that makes the work survive is what makes the
// *name* of the work survive, with no table to migrate and nothing to
// reconcile after a restart.

const (
	folderOption    = "@jd_folder"
	favouriteOption = "@jd_fav"
)

// SessionMeta is everything about a session that is the operator's choice
// rather than the shell's state.
type SessionMeta struct {
	Title     string `json:"title"`
	Folder    string `json:"folder"`
	Favourite bool   `json:"favourite"`
}

var ErrNoPersistence = errors.New("this session is not tmux-backed, so it has nothing to remember settings on")

// SetMeta renames a session and files it away.
//
// Written straight through to tmux rather than to a local map: the point of
// naming a session is that the name is still there tomorrow, and a name held
// only in this process lasts until the next restart.
func (m *Manager) SetMeta(ctx context.Context, tmuxName string, meta SessionMeta) error {
	if !m.useTmux || tmuxName == "" {
		return ErrNoPersistence
	}
	if !m.owns(ctx, tmuxName) {
		return ErrNotFound
	}
	set := func(option, value string) error {
		return hostexec.CommandOnHost(ctx, "tmux", "set-option", "-t", tmuxName, option, value).Run()
	}
	if err := set(titleOption, sanitiseField(meta.Title)); err != nil {
		return err
	}
	if err := set(folderOption, sanitiseField(meta.Folder)); err != nil {
		return err
	}
	favourite := ""
	if meta.Favourite {
		favourite = "1"
	}
	if err := set(favouriteOption, favourite); err != nil {
		return err
	}

	// The live session, if one is attached, so the change shows without
	// waiting for the next listing to come back from tmux.
	if title := sanitiseField(meta.Title); title != "" {
		for _, sess := range m.List() {
			if sess.TmuxName == tmuxName {
				sess.mu.Lock()
				sess.Title = title
				sess.mu.Unlock()
			}
		}
	}
	return nil
}

// owns reports whether a tmux session is one this dashboard created.
//
// The same rule Reattach applies, for the same reason: the host's tmux server
// holds the operator's own personal sessions too, and neither renaming nor
// listing has any business reaching them.
func (m *Manager) owns(ctx context.Context, tmuxName string) bool {
	if !strings.HasPrefix(tmuxName, sessionPrefix) {
		return false
	}
	for _, s := range m.TmuxSessions(ctx) {
		if s.Name == tmuxName {
			return true
		}
	}
	return false
}

// sanitiseField strips what would break the listing format or a single line.
//
// The separator is replaced rather than dropped so a title that contained one
// stays readable, and control characters go entirely: they would be escaped by
// tmux on the way back out and reappear as literal `\017` in the UI.
func sanitiseField(v string) string {
	v = strings.ReplaceAll(v, fieldSep, "/")
	v = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, v)
	v = strings.TrimSpace(v)
	if len(v) > 64 {
		v = strings.TrimSpace(v[:64])
	}
	return v
}

// Window is one tmux window inside a session — a tab within a tab.
//
// Worth surfacing because tmux already has the concept and an operator who has
// not memorised `C-b c` cannot reach it. It is also the cheap way to keep
// related work together: one session called "deploy" with windows for the
// build, the logs and a shell beats three sessions that have to be named to be
// told apart.
type Window struct {
	Index  int    `json:"index"`
	Name   string `json:"name"`
	Active bool   `json:"active"`
	Panes  int    `json:"panes"`
	CWD    string `json:"cwd,omitempty"`
}

// Windows lists the windows of a session, in tmux's own order.
func (m *Manager) Windows(ctx context.Context, tmuxName string) ([]Window, error) {
	out := []Window{}
	if !m.useTmux || tmuxName == "" {
		return out, nil
	}
	if !m.owns(ctx, tmuxName) {
		return nil, ErrNotFound
	}
	// The path last, for the reason the session listing puts it last: a
	// directory may contain the separator and a window name may not.
	const format = "#{window_index}" + fieldSep + "#{window_name}" + fieldSep +
		"#{window_active}" + fieldSep + "#{window_panes}" + fieldSep + "#{pane_current_path}"
	raw, err := hostexec.CommandOnHost(ctx, "tmux", "list-windows", "-t", tmuxName, "-F", format).Output()
	if err != nil {
		return out, nil
	}
	for _, line := range splitLines(string(raw)) {
		f := strings.SplitN(line, fieldSep, 5)
		if len(f) < 5 {
			continue
		}
		w := Window{Name: f[1], Active: f[2] != "0", CWD: f[4]}
		w.Index, _ = strconv.Atoi(f[0])
		w.Panes, _ = strconv.Atoi(f[3])
		out = append(out, w)
	}
	return out, nil
}

// NewWindow opens another window in an existing session.
//
// `-d` so it is created without stealing focus from whatever the operator is
// watching; selecting it is a separate, deliberate act.
func (m *Manager) NewWindow(ctx context.Context, tmuxName, name, cwd string) error {
	if !m.useTmux || tmuxName == "" {
		return ErrNoPersistence
	}
	if !m.owns(ctx, tmuxName) {
		return ErrNotFound
	}
	args := []string{"new-window", "-d", "-t", tmuxName}

	// Where the new window starts. Falling back to the directory the session
	// is currently in, rather than to tmux's default of wherever the session
	// began, is what every tabbed terminal does: a new tab opens beside the
	// one you were looking at, not back at the start.
	dir := hostDir(cwd)
	if dir == "" {
		dir = hostDir(tmuxPanePath(tmuxName))
	}
	if dir != "" {
		args = append(args, "-c", dir)
	}
	if n := sanitiseField(name); n != "" {
		args = append(args, "-n", n)
	}

	// The login, passed explicitly. The session also carries a
	// `default-command` that says the same thing — which is what covers a
	// window opened with `C-b c` from inside tmux — but naming it here does
	// not depend on that option having been set, and this is the path the
	// dashboard's own button takes. Without either, tmux runs the shell of
	// whoever started the tmux server: the dashboard, as root.
	args = append(args, m.account.loginArgv(m.shell, true)...)
	return hostexec.CommandOnHost(ctx, "tmux", args...).Run()
}

// RenameWindow gives a window a name that says what it is for.
func (m *Manager) RenameWindow(ctx context.Context, tmuxName string, index int, name string) error {
	if !m.useTmux || tmuxName == "" {
		return ErrNoPersistence
	}
	if !m.owns(ctx, tmuxName) {
		return ErrNotFound
	}
	clean := sanitiseField(name)
	if clean == "" {
		return errors.New("a window name is required")
	}
	target := tmuxName + ":" + strconv.Itoa(index)
	return hostexec.CommandOnHost(ctx, "tmux", "rename-window", "-t", target, clean).Run()
}

// SelectWindow switches which window the attached client is showing.
//
// This is what makes the window strip in the UI a control rather than a
// display: tmux redraws the pane for every attached client, so the browser
// sees the new window without reconnecting anything.
func (m *Manager) SelectWindow(ctx context.Context, tmuxName string, index int) error {
	if !m.useTmux || tmuxName == "" {
		return ErrNoPersistence
	}
	if !m.owns(ctx, tmuxName) {
		return ErrNotFound
	}
	target := tmuxName + ":" + strconv.Itoa(index)
	return hostexec.CommandOnHost(ctx, "tmux", "select-window", "-t", target).Run()
}

// KillWindow closes one window, taking whatever runs in it.
//
// Refused for the last window, because tmux would destroy the session with it
// — and "close this tab" quietly ending the whole session, its siblings and
// their work is the kind of surprise this panel exists to avoid. The caller is
// told to close the session instead, which is a route with a typed
// confirmation in front of it.
func (m *Manager) KillWindow(ctx context.Context, tmuxName string, index int) error {
	if !m.useTmux || tmuxName == "" {
		return ErrNoPersistence
	}
	windows, err := m.Windows(ctx, tmuxName)
	if err != nil {
		return err
	}
	if len(windows) <= 1 {
		return errors.New("this is the session's only window; close the session itself instead")
	}
	target := tmuxName + ":" + strconv.Itoa(index)
	return hostexec.CommandOnHost(ctx, "tmux", "kill-window", "-t", target).Run()
}

// sessionPrefix marks the tmux sessions this dashboard owns, and is what keeps
// every listing and every action off the operator's personal ones.
const sessionPrefix = "vpsd-"

// idleDetach is how long a persisted session may sit with nobody attached
// before its PTY is released.
//
// Long, and it only ever *detaches*: the tmux session, its processes and its
// scrollback all continue, and the session reappears under "still running"
// with its name intact. The point is to give back the PTY and the slot, not to
// end anybody's work — which is why the reaper no longer kills a persisted
// session at all, however long it has been idle.
var idleDetach = 12 * time.Hour

package term

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/Wayy01/Just-Dashboard/backend/internal/hostexec"
)

// Panes: the last of tmux's three levels, and the one every web terminal drops.
//
// A session is the work, a window is a task within it, and a pane is the
// two things you have to watch at once — the build and the log it writes, the
// server and the client hitting it. Cockpit, ttyd, Wetty and Portainer all
// stop at a single rectangle, so the operator who wants two either opens two
// browser tabs and loses the pairing, or learns `C-b %` and drives it blind
// through a UI that shows one pane's worth of a split screen.
//
// Everything here is tmux doing the work. The dashboard's contribution is to
// name the operations and to show what the layout currently is, because a
// split screen you cannot see the shape of is worse than no split at all.

// Pane is one rectangle inside a window.
type Pane struct {
	Index  int  `json:"index"`
	Active bool `json:"active"`
	Width  int  `json:"width"`
	Height int  `json:"height"`
	PID    int  `json:"pid"`
	// Command is what is running in the pane right now — `vim`, `psql`, the
	// shell when it is idle. It is the only label a pane has, and it is a
	// better one than an index: "pane 2" says nothing, "pg_dump" says which
	// half of the screen not to close.
	Command string `json:"command,omitempty"`
	CWD     string `json:"cwd,omitempty"`
	// Dead marks a pane whose process exited while `remain-on-exit` kept the
	// rectangle. It reads as a frozen terminal otherwise.
	Dead bool `json:"dead"`
	// Where the rectangle is, in cells, within the window.
	//
	// The browser sees one terminal, not four: tmux composes every pane into a
	// single screen and the PTY carries the result. So a click lands on a cell
	// and nothing else, and the only way to answer "which pane was that" is to
	// know where each one starts and ends. Right and Bottom are inclusive, as
	// tmux reports them.
	Left   int `json:"left"`
	Top    int `json:"top"`
	Right  int `json:"right"`
	Bottom int `json:"bottom"`
}

// Layouts are tmux's own named arrangements, and the list is closed: the value
// is passed to `select-layout`, and anything outside this set is either a
// custom layout string (which the UI has no way to produce) or a typo.
var Layouts = []string{"even-horizontal", "even-vertical", "main-horizontal", "main-vertical", "tiled"}

func validLayout(v string) bool {
	for _, l := range Layouts {
		if v == l {
			return true
		}
	}
	return false
}

// Panes lists the panes of one window.
func (m *Manager) Panes(ctx context.Context, tmuxName string, window int) ([]Pane, error) {
	out := []Pane{}
	if !m.useTmux || tmuxName == "" {
		return out, nil
	}
	if !m.owns(ctx, tmuxName) {
		return nil, ErrNotFound
	}
	// The path last, as everywhere else in this package. `pane_title` is
	// deliberately not asked for: it is free-form text an application can set
	// to anything, including the separator, and `pane_current_command`
	// answers the same question without being a parsing hazard.
	const format = "#{pane_index}" + fieldSep + "#{pane_active}" + fieldSep +
		"#{pane_width}" + fieldSep + "#{pane_height}" + fieldSep +
		"#{pane_pid}" + fieldSep + "#{pane_dead}" + fieldSep +
		"#{pane_left}" + fieldSep + "#{pane_top}" + fieldSep +
		"#{pane_right}" + fieldSep + "#{pane_bottom}" + fieldSep +
		"#{pane_current_command}" + fieldSep + "#{pane_current_path}"
	const fields = 12
	raw, err := hostexec.CommandOnHost(ctx, "tmux", "list-panes",
		"-t", target(tmuxName, window), "-F", format).Output()
	if err != nil {
		return out, nil
	}
	for _, line := range splitLines(string(raw)) {
		f := strings.SplitN(line, fieldSep, fields)
		if len(f) < fields {
			continue
		}
		p := Pane{Active: f[1] != "0", Dead: f[5] != "0", Command: f[10], CWD: f[11]}
		p.Index, _ = strconv.Atoi(f[0])
		p.Width, _ = strconv.Atoi(f[2])
		p.Height, _ = strconv.Atoi(f[3])
		p.PID, _ = strconv.Atoi(f[4])
		p.Left, _ = strconv.Atoi(f[6])
		p.Top, _ = strconv.Atoi(f[7])
		p.Right, _ = strconv.Atoi(f[8])
		p.Bottom, _ = strconv.Atoi(f[9])
		out = append(out, p)
	}
	return out, nil
}

// SplitPane divides the active pane of a window.
//
// The new pane starts in the current one's directory rather than in the
// session's, which is what makes "split this" mean "another shell where I
// already am" — the thing anybody splitting a pane is actually asking for.
// `#{pane_current_path}` is tmux's own expansion of that, resolved by tmux
// after it has decided which pane is current, so there is nothing to race.
//
// The command is left to the session's `default-command`, set at creation, so
// a pane is the same login as every other shell here rather than the root
// shell tmux would otherwise inherit from this process.
func (m *Manager) SplitPane(ctx context.Context, tmuxName string, window int, vertical bool) error {
	if !m.useTmux || tmuxName == "" {
		return ErrNoPersistence
	}
	if !m.owns(ctx, tmuxName) {
		return ErrNotFound
	}
	// tmux's own vocabulary is the opposite of the intuitive one: `-h` splits
	// into left and right. The API here names the *result* — a vertical split
	// puts the panes side by side — and translates once, so the UI never has
	// to carry the confusion.
	direction := "-v"
	if vertical {
		direction = "-h"
	}
	return hostexec.CommandOnHost(ctx, "tmux", "split-window", direction,
		"-t", target(tmuxName, window), "-c", "#{pane_current_path}").Run()
}

// SelectPane moves the focus, which is what typing goes to.
func (m *Manager) SelectPane(ctx context.Context, tmuxName string, window, pane int) error {
	if !m.useTmux || tmuxName == "" {
		return ErrNoPersistence
	}
	if !m.owns(ctx, tmuxName) {
		return ErrNotFound
	}
	return hostexec.CommandOnHost(ctx, "tmux", "select-pane", "-t", paneTarget(tmuxName, window, pane)).Run()
}

// ZoomPane toggles one pane to fill the window and back.
//
// The one tmux feature that a browser terminal needs more than a native one
// does: the pane is already small because the page has a sidebar and a header,
// and reading a stack trace in a quarter of it is not reading.
func (m *Manager) ZoomPane(ctx context.Context, tmuxName string, window, pane int) error {
	if !m.useTmux || tmuxName == "" {
		return ErrNoPersistence
	}
	if !m.owns(ctx, tmuxName) {
		return ErrNotFound
	}
	return hostexec.CommandOnHost(ctx, "tmux", "resize-pane", "-Z",
		"-t", paneTarget(tmuxName, window, pane)).Run()
}

// KillPane closes one rectangle.
//
// Refused for the last pane in a window, for the reason KillWindow refuses the
// last window in a session: tmux would take the window with it, and a control
// labelled "close this pane" that closes the tab is exactly the surprise this
// panel exists to avoid.
func (m *Manager) KillPane(ctx context.Context, tmuxName string, window, pane int) error {
	if !m.useTmux || tmuxName == "" {
		return ErrNoPersistence
	}
	panes, err := m.Panes(ctx, tmuxName, window)
	if err != nil {
		return err
	}
	if len(panes) <= 1 {
		return errors.New("this is the window's only pane; close the window itself instead")
	}
	return hostexec.CommandOnHost(ctx, "tmux", "kill-pane", "-t", paneTarget(tmuxName, window, pane)).Run()
}

// SetLayout rearranges a window's panes into one of tmux's named shapes.
func (m *Manager) SetLayout(ctx context.Context, tmuxName string, window int, layout string) error {
	if !m.useTmux || tmuxName == "" {
		return ErrNoPersistence
	}
	if !m.owns(ctx, tmuxName) {
		return ErrNotFound
	}
	if !validLayout(layout) {
		return errors.New("unknown layout")
	}
	return hostexec.CommandOnHost(ctx, "tmux", "select-layout", "-t", target(tmuxName, window), layout).Run()
}

// Synchronize sends every keystroke to every pane in the window at once.
//
// The classic use is running the same command on four machines that each have
// an SSH pane. It is also the single easiest way to do something four times by
// accident, which is why the state is reported in the window listing and drawn
// as a standing warning rather than as a checkbox you have to remember
// ticking.
func (m *Manager) Synchronize(ctx context.Context, tmuxName string, window int, on bool) error {
	if !m.useTmux || tmuxName == "" {
		return ErrNoPersistence
	}
	if !m.owns(ctx, tmuxName) {
		return ErrNotFound
	}
	value := "off"
	if on {
		value = "on"
	}
	return hostexec.CommandOnHost(ctx, "tmux", "set-option", "-w",
		"-t", target(tmuxName, window), "synchronize-panes", value).Run()
}

// SendKeys types into a session without the operator's keyboard.
//
// It exists for the controls that a browser cannot deliver as keystrokes at
// all — Ctrl+C into a pane that does not have focus, a stored command sent to
// the shell — and it is deliberately the *only* way this package writes to a
// session other than the PTY the operator is attached to. `-l` sends the
// string literally, so nothing in it is interpreted as a key name; the special
// keys go through the named-key form instead, from a closed list.
func (m *Manager) SendKeys(ctx context.Context, tmuxName string, window int, literal string, keys []string) error {
	if !m.useTmux || tmuxName == "" {
		return ErrNoPersistence
	}
	if !m.owns(ctx, tmuxName) {
		return ErrNotFound
	}
	if literal != "" {
		if err := hostexec.CommandOnHost(ctx, "tmux", "send-keys",
			"-t", target(tmuxName, window), "-l", "--", literal).Run(); err != nil {
			return err
		}
	}
	for _, key := range keys {
		if !validKey(key) {
			return errors.New("unknown key " + key)
		}
		if err := hostexec.CommandOnHost(ctx, "tmux", "send-keys",
			"-t", target(tmuxName, window), key).Run(); err != nil {
			return err
		}
	}
	return nil
}

// sendableKeys is closed because the value reaches `send-keys` as a key name,
// where tmux's own syntax gives it meaning. The set is what a control strip
// needs: the interrupts, the ones a shell cannot receive as text, and Enter.
var sendableKeys = []string{"C-c", "C-d", "C-z", "C-l", "C-\\", "Enter", "Escape", "Up", "Down", "Tab", "q"}

func validKey(k string) bool {
	for _, v := range sendableKeys {
		if k == v {
			return true
		}
	}
	return false
}

func target(tmuxName string, window int) string {
	return tmuxName + ":" + strconv.Itoa(window)
}

func paneTarget(tmuxName string, window, pane int) string {
	return target(tmuxName, window) + "." + strconv.Itoa(pane)
}

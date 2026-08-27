package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wayy01/Just-Dashboard/backend/internal/auth"
	"github.com/Wayy01/Just-Dashboard/backend/internal/httpx"
	"github.com/Wayy01/Just-Dashboard/backend/internal/term"
	"github.com/go-chi/chi/v5"
)

func (s *Server) mountTerminalRoutes(r chi.Router) {
	r.Route("/terminal", func(r chi.Router) {
		// The entire terminal surface, listing included, requires the
		// terminal capability: knowing which shells are open is itself
		// information a read-only account has no business with.
		r.Use(httpx.RequireCapability(auth.CapTerminal))
		r.Method(http.MethodGet, "/", s.handle(s.handleTerminalList))
		r.Method(http.MethodPost, "/", s.handle(s.handleTerminalCreate))
		r.Method(http.MethodPost, "/reattach", s.handle(s.handleTerminalReattach))
		r.Method(http.MethodGet, "/{id}/attach", s.handle(s.handleTerminalAttach))
		r.Method(http.MethodPost, "/{id}/detach", s.handle(s.handleTerminalDetach))

		// Naming and filing a session. Addressed by tmux name rather than by
		// session id, because the sessions most in need of a name are exactly
		// the ones this process is not currently holding a PTY for — the ones
		// left running after a restart.
		r.Method(http.MethodPatch, "/persistent/{name}", s.handle(s.handleTerminalMeta))

		// Folders. They are the dashboard's own record rather than tmux's,
		// for the reason handlers_terminal_folders.go opens with, but they
		// are part of the same surface and mount here so the route map for
		// the terminal stays in one place.
		s.mountTerminalFolderRoutes(r)

		// The windows inside a session: a tab strip within a tab. tmux has had
		// these all along and an operator who has not memorised `C-b c` had no
		// way to reach them.
		r.Method(http.MethodGet, "/persistent/{name}/windows", s.handle(s.handleTerminalWindows))
		r.Method(http.MethodPost, "/persistent/{name}/windows", s.handle(s.handleTerminalWindowCreate))
		r.Method(http.MethodPatch, "/persistent/{name}/windows/{index}", s.handle(s.handleTerminalWindowUpdate))
		// Closing a window, a pane or a session is destructive — it takes
		// whatever is running with it — and stays inside `s.destructive` for
		// the capability check, the tighter budget and the audit entry. It
		// deliberately carries **no typed phrase**, which is the one place in
		// this API that combination appears.
		//
		// The phrase exists so that an irreversible action cannot be a
		// mis-click. That reasoning holds for deleting a container or a backup,
		// which somebody does a handful of times a year. Closing a shell is an
		// everyday act — a dozen a day for anyone using this panel as intended
		// — and a phrase in front of an everyday act does not get read, it gets
		// typed. Training the operator to type "close terminal" without looking
		// is worse than no guard at all, because it is exactly the habit the
		// typed confirmation exists to prevent everywhere it still applies.
		s.destructive(r, func(r chi.Router) {
			r.Method(http.MethodDelete, "/persistent/{name}/windows/{index}", s.handle(s.handleTerminalWindowKill))
		})

		// The panes inside a window: tmux's third level, and the one no
		// browser terminal in this class exposes at all.
		r.Method(http.MethodGet, "/persistent/{name}/windows/{index}/panes", s.handle(s.handleTerminalPanes))
		r.Method(http.MethodPost, "/persistent/{name}/windows/{index}/panes", s.handle(s.handleTerminalPaneSplit))
		r.Method(http.MethodPatch, "/persistent/{name}/windows/{index}/panes/{pane}", s.handle(s.handleTerminalPaneUpdate))
		s.destructive(r, func(r chi.Router) {
			r.Method(http.MethodDelete, "/persistent/{name}/windows/{index}/panes/{pane}", s.handle(s.handleTerminalPaneKill))
		})

		// Typing into a session that does not have the browser's focus — the
		// interrupt keys and the stored one-liners. A write, and audited as
		// one: "Ctrl+C was sent to a shell" is an event.
		r.Method(http.MethodPost, "/persistent/{name}/windows/{index}/keys", s.handle(s.handleTerminalSendKeys))
		s.destructive(r, func(r chi.Router) {
			// Killing a session takes whatever is running in it with it. No
			// typed phrase, for the reason given above the window route.
			r.Method(http.MethodDelete, "/{id}", s.handle(s.handleTerminalKill))
		})
		r.Method(http.MethodGet, "/{id}/cwd", s.handle(s.handleTerminalCWD))
	})
}

func mapTermError(err error) error {
	switch {
	case errors.Is(err, term.ErrDisabled):
		return httpx.Err(http.StatusServiceUnavailable, "terminal_disabled", err.Error())
	case errors.Is(err, term.ErrNotFound):
		return httpx.ErrNotFound
	case errors.Is(err, term.ErrTooMany):
		return httpx.Err(http.StatusTooManyRequests, "too_many_sessions", err.Error())
	case errors.Is(err, term.ErrNoPersistence):
		return httpx.Err(http.StatusConflict, "not_persistent", err.Error())
	default:
		return httpx.Internal(err)
	}
}

// workspace is one terminal as the operator thinks of it: a named, filed piece
// of work that may or may not have a PTY attached right now.
//
// The page used to receive two lists — live sessions and "detached" tmux names
// — and had to reconcile them itself, which is why a session that had been
// idle for an hour appeared to vanish and reappear somewhere else. They are the
// same thing in two states, so they are one list with a flag.
type workspace struct {
	// ID is set only while a PTY is attached; it is what the socket addresses.
	// Without one, the session is running and reattaching gives it an id.
	ID        string `json:"id,omitempty"`
	TmuxName  string `json:"tmuxName,omitempty"`
	Title     string `json:"title"`
	Folder    string `json:"folder,omitempty"`
	Favourite bool   `json:"favourite"`
	Colour    string `json:"colour,omitempty"`
	// Live distinguishes "this dashboard is holding a PTY for it" from "it is
	// running on the host and can be picked up".
	Live      bool      `json:"live"`
	Persisted bool      `json:"persisted"`
	CWD       string    `json:"cwd,omitempty"`
	Windows   int       `json:"windows"`
	CreatedAt time.Time `json:"createdAt"`
	Attached  int       `json:"attached"`
	User      string    `json:"user,omitempty"`
	Shell     string    `json:"shell,omitempty"`
	Owner     string    `json:"owner,omitempty"`
}

func (s *Server) handleTerminalList(w http.ResponseWriter, r *http.Request) error {
	// Keyed by tmux name so a session the dashboard is holding and the same
	// session as tmux reports it collapse into one entry rather than appearing
	// twice under different names.
	byTmux := map[string]*workspace{}
	out := []*workspace{}
	// Kept beside the workspaces so a session whose directory tmux did not
	// answer for can be asked directly, below.
	live := map[*workspace]*term.Session{}

	for _, sess := range s.modules.term.List() {
		meta := sess.Meta()
		ws := &workspace{
			ID: sess.ID, TmuxName: sess.TmuxName, Title: meta.Title,
			Folder: meta.Folder, Favourite: meta.Favourite, Colour: meta.Colour,
			Live: true, Persisted: sess.Persisted, CreatedAt: sess.CreatedAt,
			Attached: sess.Attached(), User: sess.User, Shell: sess.Shell,
			Owner: sess.Owner, Windows: 1,
		}
		out = append(out, ws)
		live[ws] = sess
		if sess.TmuxName != "" {
			byTmux[sess.TmuxName] = ws
		}
	}

	// tmux answers for the sessions this process is not holding, and for the
	// live ones it answers only about the *shell's* state — where it is, how
	// many windows it has.
	//
	// It deliberately does not answer for the name, the folder or the colour
	// of a live session, though it stores all three. `tmux new-session` is
	// handed to a PTY and the `set-option` that files the session away can
	// land half a second later, so a listing taken from tmux immediately
	// after a create reports a session with no folder — which is precisely
	// how a shell opened inside a folder appeared under "Other" and moved
	// into place on some later poll. The in-memory copy is seeded on create,
	// read back on reattach and written on every change, so for a live
	// session it is never behind and is sometimes ahead.
	for _, t := range s.modules.term.TmuxSessions(r.Context()) {
		if ws, ok := byTmux[t.Name]; ok {
			ws.Windows = t.Windows
			// Only when tmux actually answered. A blank from a tmux that is
			// there but has not caught up must not erase a directory this
			// process can read for itself.
			if t.CWD != "" {
				ws.CWD = t.CWD
			}
			continue
		}
		out = append(out, &workspace{
			TmuxName: t.Name, Title: orDefault(t.Title, t.Name), Folder: t.Folder,
			Favourite: t.Favourite, Colour: t.Colour, Persisted: true, CWD: t.CWD,
			Windows: t.Windows, CreatedAt: t.CreatedAt,
		})
	}

	// Where a session is now, for the ones tmux did not answer for.
	//
	// tmux reports every session's directory in the one `list-sessions` call
	// above, so on a host that has it this loop does nothing. On a host that
	// does not — Debian installs no tmux by default — that call returns
	// nothing at all, and the listing used to carry no directory for any
	// session. The files and git panel beside the terminal is rooted at
	// exactly that value, so both of them silently had nowhere to look: the
	// panel opened empty and stayed empty, on a shell sitting in a repository.
	//
	// Session.CWD reads it from /proc, which needs no multiplexer, and it is
	// asked only where the answer is still missing so a tmux host pays nothing
	// for it.
	for ws, sess := range live {
		if ws.CWD == "" {
			ws.CWD = sess.CWD()
		}
	}

	// Who a new session will belong to, and where it will start. The page
	// says so before you open one: a root-equivalent shell is not somewhere to
	// discover your identity by running whoami.
	account, accountErr := s.modules.term.Account()
	login := map[string]any{"user": account.Name, "home": account.Home, "shell": account.Shell}
	if accountErr != nil {
		login["error"] = accountErr.Error()
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"enabled": s.modules.term.Enabled(),
		"tmux":    s.modules.term.TmuxAvailable(),
		"login":   login,
		// The folders come with the listing rather than from a second
		// request, because the rail cannot be drawn without both and two
		// polls would render a session in a folder that has not arrived yet
		// — the same class of flicker this endpoint was collapsed into one
		// list to remove.
		"folders":  s.mergedTerminalFolders(r.Context(), out),
		"colours":  term.Colours,
		"layouts":  term.Layouts,
		"sessions": out,
	})
	return nil
}

func orDefault(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

type createTerminalRequest struct {
	Title   string `json:"title"`
	CWD     string `json:"cwd"`
	Folder  string `json:"folder"`
	Colour  string `json:"colour"`
	Rows    uint16 `json:"rows"`
	Cols    uint16 `json:"cols"`
	Persist *bool  `json:"persist"`
}

func (s *Server) handleTerminalCreate(w http.ResponseWriter, r *http.Request) error {
	var req createTerminalRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	p := httpx.MustPrincipal(r)
	persist := true
	if req.Persist != nil {
		persist = *req.Persist
	}
	// A session opened into a folder takes the folder's colour unless it was
	// given one, which is what "the sessions inside carry the highlight"
	// means in practice: the operator paints the folder once and every shell
	// they open there is already the right colour.
	colour := req.Colour
	if colour == "" && req.Folder != "" {
		for _, f := range s.terminalFolders(r.Context()) {
			if strings.EqualFold(f.Name, req.Folder) {
				colour = f.Colour
			}
		}
	}
	sess, err := s.modules.term.Create(r.Context(), term.CreateOptions{
		Title: req.Title, Owner: p.Username(), CWD: req.CWD, Folder: req.Folder,
		Colour: colour, Rows: req.Rows, Cols: req.Cols, Persist: persist,
	})
	if err != nil {
		return mapTermError(err)
	}
	// The account is the part of this record that matters later: "a shell was
	// opened" and "a shell was opened as root" are different events.
	httpx.SetAudit(r, "terminal.create", sess.ID,
		map[string]any{"shell": sess.Shell, "user": sess.User,
			"persisted": sess.Persisted, "cwd": req.CWD})
	meta := sess.Meta()
	httpx.JSON(w, http.StatusCreated, map[string]any{
		"id": sess.ID, "title": meta.Title, "shell": sess.Shell, "user": sess.User,
		"persisted": sess.Persisted, "tmuxName": sess.TmuxName, "pid": sess.PID,
		"folder": meta.Folder, "colour": meta.Colour,
	})
	return nil
}

type reattachRequest struct {
	TmuxName string `json:"tmuxName"`
	Rows     uint16 `json:"rows"`
	Cols     uint16 `json:"cols"`
}

func (s *Server) handleTerminalReattach(w http.ResponseWriter, r *http.Request) error {
	var req reattachRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	p := httpx.MustPrincipal(r)
	sess, err := s.modules.term.Reattach(r.Context(), req.TmuxName, p.Username(), req.Rows, req.Cols)
	if err != nil {
		return mapTermError(err)
	}
	httpx.SetAudit(r, "terminal.reattach", req.TmuxName, map[string]any{"sessionId": sess.ID})
	httpx.JSON(w, http.StatusOK, map[string]any{
		"id": sess.ID, "title": sess.Title, "tmuxName": sess.TmuxName, "persisted": true,
	})
	return nil
}

type terminalControl struct {
	Type string `json:"type"`
	Rows uint16 `json:"rows"`
	Cols uint16 `json:"cols"`
	Data string `json:"data"`
}

// handleTerminalAttach wires a browser to a PTY. Binary frames carry raw
// terminal bytes in both directions; JSON frames carry control messages. The
// session is written to the audit trail at attach time, because a shell that
// crashes the process must still have left a record that it was opened.
func (s *Server) handleTerminalAttach(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	sess, err := s.modules.term.Get(id)
	if err != nil {
		return mapTermError(err)
	}
	s.recordAudit(r, "terminal.attach", id,
		map[string]any{"shell": sess.Shell, "user": sess.User, "tmux": sess.TmuxName})

	conn, err := s.WS.Upgrade(w, r)
	if err != nil {
		return nil
	}
	defer conn.Close()
	ctx, cancel := contextWithCancel(r)
	defer cancel()
	go conn.Keepalive(ctx)

	snapshot, subID, out, err := sess.Subscribe()
	if err != nil {
		conn.SendError(err.Error())
		return nil
	}
	defer sess.Unsubscribe(subID)

	if len(snapshot) > 0 {
		// Announced before it is sent, because the browser has to know that
		// what follows is a replay rather than live output.
		//
		// A terminal emulator answers some of what it is written: `CSI c` and
		// friends are questions the shell asks the terminal, and xterm replies
		// down the same channel a keystroke uses. Replaying a scrollback that
		// contains one makes it answer a question that was already answered —
		// and the answer lands at whatever prompt is there now, which is how
		// reopening a tab typed `1;2c0;276` into the shell and left a column
		// of "command not found". The client suppresses its replies for the
		// duration of the replay; it cannot work that out on its own, because
		// from the browser's side the bytes are identical either way.
		if err := conn.Send("scrollback", map[string]any{"bytes": len(snapshot)}); err != nil {
			return nil
		}
		if err := conn.WriteBinary(snapshot); err != nil {
			return nil
		}
	}
	go func() {
		for chunk := range out {
			if err := conn.WriteBinary(chunk); err != nil {
				cancel()
				return
			}
		}
		cancel()
	}()

	// A repaint owed to the session because its size changed, held back until
	// the size stops changing. Dragging the panel divider produces a resize a
	// frame, and a `refresh-client` per frame would be a subprocess per frame
	// for a picture that is about to be wrong again. One repaint after the
	// drag settles is the whole point — see term.Manager.Redraw.
	var redraw *time.Timer
	defer func() {
		if redraw != nil {
			redraw.Stop()
		}
	}()

	for {
		kind, data, err := conn.ReadMessage()
		if err != nil {
			break
		}
		if kind == websocketTextFrame && len(data) > 0 && data[0] == '{' {
			var ctrl terminalControl
			if json.Unmarshal(data, &ctrl) == nil {
				switch ctrl.Type {
				case "resize":
					if changed, _ := sess.Resize(ctrl.Rows, ctrl.Cols); changed && sess.TmuxName != "" {
						name := sess.TmuxName
						if redraw != nil {
							redraw.Stop()
						}
						redraw = time.AfterFunc(200*time.Millisecond, func() {
							// Detached: the repaint is owed to the tmux
							// session, which outlives this socket, and a
							// browser that closed the tab mid-drag has left a
							// half-drawn screen for whoever attaches next.
							ctx, cancel := detachedContext(10)
							defer cancel()
							_ = s.modules.term.Redraw(ctx, name)
						})
					}
					continue
				case "exit-copy":
					// The first keystroke after scrolling up. tmux is in copy
					// mode and would swallow it as a copy command; this is the
					// browser asking to be put back at the prompt before the
					// keystroke that follows is delivered. Synchronous, so the
					// order the operator typed in is the order tmux sees.
					if sess.TmuxName != "" {
						ctx, cancel := detachedContext(5)
						_ = s.modules.term.ExitCopyMode(ctx, sess.TmuxName)
						cancel()
					}
					continue
				case "input":
					sess.Write([]byte(ctrl.Data))
					continue
				case "ping":
					continue
				}
			}
		}
		if _, err := sess.Write(data); err != nil {
			break
		}
	}
	s.recordAudit(r, "terminal.detach", id, map[string]any{"attached": sess.Attached()})
	return nil
}

// gorilla's TextMessage constant, kept local so handlers do not need to import
// the websocket package directly.
const websocketTextFrame = 1

func (s *Server) handleTerminalDetach(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	if err := s.modules.term.Detach(id); err != nil {
		return mapTermError(err)
	}
	httpx.SetAudit(r, "terminal.detach", id, nil)
	httpx.NoContent(w)
	return nil
}

func (s *Server) handleTerminalKill(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	if err := s.modules.term.Kill(r.Context(), id); err != nil {
		return mapTermError(err)
	}
	httpx.SetAudit(r, "terminal.kill", id, nil)
	httpx.NoContent(w)
	return nil
}

// handleTerminalCWD lets the frontend anchor in-session upload and download to
// wherever the shell has navigated, which is what makes those actions feel
// like they belong to the terminal rather than to the file manager.
func (s *Server) handleTerminalCWD(w http.ResponseWriter, r *http.Request) error {
	sess, err := s.modules.term.Get(chi.URLParam(r, "id"))
	if err != nil {
		return mapTermError(err)
	}
	cwd := sess.CWD()
	if cwd == "" {
		return httpx.Err(http.StatusServiceUnavailable, "cwd_unavailable",
			"cannot determine the session's working directory")
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"cwd": cwd, "checkedAt": time.Now().UTC()})
	return nil
}

// handleTerminalMeta renames a session and files it away.
//
// A rename is not a destructive action and deliberately carries no typed
// confirmation: the whole point is that naming a session should be as cheap as
// naming a browser tab, or nobody does it and every session stays
// `vpsd-3f2a91c4`.
// Every field is a pointer so that an omitted one means "leave it alone"
// rather than "set it to empty". Dragging a session into a folder sends the
// folder and nothing else, and a request shaped the other way would quietly
// erase the name the operator had given it.
type sessionMetaRequest struct {
	Title     *string `json:"title"`
	Folder    *string `json:"folder"`
	Favourite *bool   `json:"favourite"`
	Colour    *string `json:"colour"`
}

func (s *Server) handleTerminalMeta(w http.ResponseWriter, r *http.Request) error {
	var req sessionMetaRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	name := chi.URLParam(r, "name")
	meta, err := s.modules.term.Meta(r.Context(), name)
	if err != nil {
		return mapTermError(err)
	}
	if req.Title != nil {
		meta.Title = *req.Title
	}
	if req.Folder != nil {
		meta.Folder = *req.Folder
	}
	if req.Favourite != nil {
		meta.Favourite = *req.Favourite
	}
	if req.Colour != nil {
		meta.Colour = *req.Colour
	}
	if err := s.modules.term.SetMeta(r.Context(), name, meta); err != nil {
		return mapTermError(err)
	}
	httpx.SetAudit(r, "terminal.rename", name, map[string]any{
		"title": meta.Title, "folder": meta.Folder,
		"favourite": meta.Favourite, "colour": meta.Colour,
	})
	httpx.JSON(w, http.StatusOK, meta)
	return nil
}

func (s *Server) handleTerminalWindows(w http.ResponseWriter, r *http.Request) error {
	windows, err := s.modules.term.Windows(r.Context(), chi.URLParam(r, "name"))
	if err != nil {
		return mapTermError(err)
	}
	httpx.JSON(w, http.StatusOK, windows)
	return nil
}

func (s *Server) handleTerminalWindowCreate(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Name string `json:"name"`
		CWD  string `json:"cwd"`
	}
	if r.ContentLength > 0 {
		if err := httpx.DecodeJSON(r, &req); err != nil {
			return err
		}
	}
	name := chi.URLParam(r, "name")
	if err := s.modules.term.NewWindow(r.Context(), name, req.Name, req.CWD); err != nil {
		return mapTermError(err)
	}
	httpx.SetAudit(r, "terminal.window.create", name, map[string]any{"name": req.Name})
	httpx.NoContent(w)
	return nil
}

// handleTerminalWindowUpdate renames a window, selects it, or both.
//
// Selecting is a write rather than a read because tmux redraws every attached
// client when the active window changes — the browser sees the switch without
// reconnecting, which is what makes the strip a control instead of a picture.
func (s *Server) handleTerminalWindowUpdate(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Name   string  `json:"name,omitempty"`
		Select bool    `json:"select,omitempty"`
		Colour *string `json:"colour,omitempty"`
		// Position is where a drag dropped the window in the strip, counted
		// in the strip rather than in tmux indices — the operator never sees
		// the latter, and they stop being contiguous the moment a window in
		// the middle is closed.
		Position *int `json:"position,omitempty"`
		// Session moves the window to a different session altogether, which
		// is the drag that recovers from opening the build in the wrong place.
		Session string `json:"session,omitempty"`
		// Synchronize sends every keystroke to every pane in the window.
		Synchronize *bool `json:"synchronize,omitempty"`
		// Layout rearranges the window's panes into one of tmux's shapes.
		Layout string `json:"layout,omitempty"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	name := chi.URLParam(r, "name")
	index, err := strconv.Atoi(chi.URLParam(r, "index"))
	if err != nil {
		return httpx.BadRequest("window index must be a number")
	}
	if req.Name != "" {
		if err := s.modules.term.RenameWindow(r.Context(), name, index, req.Name); err != nil {
			return mapTermError(err)
		}
	}
	if req.Colour != nil {
		if err := s.modules.term.ColourWindow(r.Context(), name, index, *req.Colour); err != nil {
			return mapTermError(err)
		}
	}
	if req.Position != nil {
		if err := s.modules.term.MoveWindow(r.Context(), name, index, *req.Position); err != nil {
			return mapTermError(err)
		}
	}
	if req.Session != "" {
		if err := s.modules.term.MoveWindowToSession(r.Context(), name, index, req.Session); err != nil {
			return mapTermError(err)
		}
	}
	if req.Layout != "" {
		if err := s.modules.term.SetLayout(r.Context(), name, index, req.Layout); err != nil {
			return mapTermError(err)
		}
	}
	if req.Synchronize != nil {
		if err := s.modules.term.Synchronize(r.Context(), name, index, *req.Synchronize); err != nil {
			return mapTermError(err)
		}
	}
	if req.Select {
		if err := s.modules.term.SelectWindow(r.Context(), name, index); err != nil {
			return mapTermError(err)
		}
	}
	httpx.SetAudit(r, "terminal.window.update", name, map[string]any{
		"index": index, "name": req.Name, "select": req.Select,
		"position": req.Position, "session": req.Session,
		"layout": req.Layout, "synchronize": req.Synchronize,
	})
	httpx.NoContent(w)
	return nil
}

func (s *Server) handleTerminalPanes(w http.ResponseWriter, r *http.Request) error {
	index, err := strconv.Atoi(chi.URLParam(r, "index"))
	if err != nil {
		return httpx.BadRequest("window index must be a number")
	}
	panes, err := s.modules.term.Panes(r.Context(), chi.URLParam(r, "name"), index)
	if err != nil {
		return mapTermError(err)
	}
	httpx.JSON(w, http.StatusOK, panes)
	return nil
}

func (s *Server) handleTerminalPaneSplit(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		// Vertical describes the *result* — two panes side by side — not
		// tmux's `-h`, whose name means the opposite of what everyone reads
		// it as.
		Vertical bool `json:"vertical"`
	}
	if r.ContentLength > 0 {
		if err := httpx.DecodeJSON(r, &req); err != nil {
			return err
		}
	}
	name := chi.URLParam(r, "name")
	index, err := strconv.Atoi(chi.URLParam(r, "index"))
	if err != nil {
		return httpx.BadRequest("window index must be a number")
	}
	if err := s.modules.term.SplitPane(r.Context(), name, index, req.Vertical); err != nil {
		return mapTermError(err)
	}
	httpx.SetAudit(r, "terminal.pane.split", name,
		map[string]any{"window": index, "vertical": req.Vertical})
	httpx.NoContent(w)
	return nil
}

func (s *Server) handleTerminalPaneUpdate(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Select bool `json:"select,omitempty"`
		Zoom   bool `json:"zoom,omitempty"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	name := chi.URLParam(r, "name")
	window, pane, err := windowAndPane(r)
	if err != nil {
		return err
	}
	if req.Select {
		if err := s.modules.term.SelectPane(r.Context(), name, window, pane); err != nil {
			return mapTermError(err)
		}
	}
	if req.Zoom {
		if err := s.modules.term.ZoomPane(r.Context(), name, window, pane); err != nil {
			return mapTermError(err)
		}
	}
	httpx.SetAudit(r, "terminal.pane.update", name,
		map[string]any{"window": window, "pane": pane, "zoom": req.Zoom})
	httpx.NoContent(w)
	return nil
}

func (s *Server) handleTerminalPaneKill(w http.ResponseWriter, r *http.Request) error {
	name := chi.URLParam(r, "name")
	window, pane, err := windowAndPane(r)
	if err != nil {
		return err
	}
	if err := s.modules.term.KillPane(r.Context(), name, window, pane); err != nil {
		if errors.Is(err, term.ErrNotFound) || errors.Is(err, term.ErrNoPersistence) {
			return mapTermError(err)
		}
		// "This is the only pane" names the route to take instead, which is a
		// sentence the operator needs rather than a 500.
		return httpx.BadRequest("%s", err.Error())
	}
	httpx.SetAudit(r, "terminal.pane.kill", name, map[string]any{"window": window, "pane": pane})
	httpx.NoContent(w)
	return nil
}

// handleTerminalSendKeys types into a session the browser is not focused on.
//
// The literal text and the named keys are separate fields rather than one
// string with escapes, because tmux's `send-keys` decides between the two by
// parsing what it is given: a stored one-liner that happened to contain the
// word `Enter` would otherwise become a keypress. `-l` for the text, a closed
// list for the keys.
func (s *Server) handleTerminalSendKeys(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Text string   `json:"text"`
		Keys []string `json:"keys"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	name := chi.URLParam(r, "name")
	index, err := strconv.Atoi(chi.URLParam(r, "index"))
	if err != nil {
		return httpx.BadRequest("window index must be a number")
	}
	if err := s.modules.term.SendKeys(r.Context(), name, index, req.Text, req.Keys); err != nil {
		if errors.Is(err, term.ErrNotFound) || errors.Is(err, term.ErrNoPersistence) {
			return mapTermError(err)
		}
		return httpx.BadRequest("%s", err.Error())
	}
	// The text is recorded: this is a way to run a command on the host, and
	// an audit entry saying only "keys were sent" would be worth nothing.
	httpx.SetAudit(r, "terminal.keys", name,
		map[string]any{"window": index, "text": req.Text, "keys": req.Keys})
	httpx.NoContent(w)
	return nil
}

func windowAndPane(r *http.Request) (window, pane int, err error) {
	window, convErr := strconv.Atoi(chi.URLParam(r, "index"))
	if convErr != nil {
		return 0, 0, httpx.BadRequest("window index must be a number")
	}
	pane, convErr = strconv.Atoi(chi.URLParam(r, "pane"))
	if convErr != nil {
		return 0, 0, httpx.BadRequest("pane index must be a number")
	}
	return window, pane, nil
}

func (s *Server) handleTerminalWindowKill(w http.ResponseWriter, r *http.Request) error {
	name := chi.URLParam(r, "name")
	index, err := strconv.Atoi(chi.URLParam(r, "index"))
	if err != nil {
		return httpx.BadRequest("window index must be a number")
	}
	if err := s.modules.term.KillWindow(r.Context(), name, index); err != nil {
		// "This is the only window" is a sentence the operator needs, not a
		// 500 — it names the route they should have taken instead.
		if errors.Is(err, term.ErrNotFound) {
			return httpx.ErrNotFound
		}
		return httpx.BadRequest("%s", err.Error())
	}
	httpx.SetAudit(r, "terminal.window.kill", name, map[string]any{"index": index})
	httpx.NoContent(w)
	return nil
}

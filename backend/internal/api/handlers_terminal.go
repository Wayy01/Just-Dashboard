package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
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

		// The windows inside a session: a tab strip within a tab. tmux has had
		// these all along and an operator who has not memorised `C-b c` had no
		// way to reach them.
		r.Method(http.MethodGet, "/persistent/{name}/windows", s.handle(s.handleTerminalWindows))
		r.Method(http.MethodPost, "/persistent/{name}/windows", s.handle(s.handleTerminalWindowCreate))
		r.Method(http.MethodPatch, "/persistent/{name}/windows/{index}", s.handle(s.handleTerminalWindowUpdate))
		s.destructive(r, func(r chi.Router) {
			r.Method(http.MethodDelete, "/persistent/{name}/windows/{index}", s.handle(s.handleTerminalWindowKill))
		})
		s.destructive(r, func(r chi.Router) {
			// Killing a session takes whatever is running in it with it.
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

	for _, sess := range s.modules.term.List() {
		ws := &workspace{
			ID: sess.ID, TmuxName: sess.TmuxName, Title: sess.Title,
			Live: true, Persisted: sess.Persisted, CreatedAt: sess.CreatedAt,
			Attached: sess.Attached(), User: sess.User, Shell: sess.Shell,
			Owner: sess.Owner, Windows: 1,
		}
		out = append(out, ws)
		if sess.TmuxName != "" {
			byTmux[sess.TmuxName] = ws
		}
	}

	// tmux is the authority on everything an operator chose — the name, the
	// folder, whether it is a favourite — because that is where those are
	// stored. A live session takes them from here rather than from the copy
	// this process happens to hold in memory.
	for _, t := range s.modules.term.TmuxSessions(r.Context()) {
		if ws, ok := byTmux[t.Name]; ok {
			ws.Title, ws.Folder, ws.Favourite = orDefault(t.Title, ws.Title), t.Folder, t.Favourite
			ws.CWD, ws.Windows = t.CWD, t.Windows
			continue
		}
		out = append(out, &workspace{
			TmuxName: t.Name, Title: orDefault(t.Title, t.Name), Folder: t.Folder,
			Favourite: t.Favourite, Persisted: true, CWD: t.CWD,
			Windows: t.Windows, CreatedAt: t.CreatedAt,
		})
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
		"enabled":  s.modules.term.Enabled(),
		"tmux":     s.modules.term.TmuxAvailable(),
		"login":    login,
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
	sess, err := s.modules.term.Create(r.Context(), term.CreateOptions{
		Title: req.Title, Owner: p.Username(), CWD: req.CWD, Folder: req.Folder,
		Rows: req.Rows, Cols: req.Cols, Persist: persist,
	})
	if err != nil {
		return mapTermError(err)
	}
	// The account is the part of this record that matters later: "a shell was
	// opened" and "a shell was opened as root" are different events.
	httpx.SetAudit(r, "terminal.create", sess.ID,
		map[string]any{"shell": sess.Shell, "user": sess.User,
			"persisted": sess.Persisted, "cwd": req.CWD})
	httpx.JSON(w, http.StatusCreated, map[string]any{
		"id": sess.ID, "title": sess.Title, "shell": sess.Shell, "user": sess.User,
		"persisted": sess.Persisted, "tmuxName": sess.TmuxName, "pid": sess.PID,
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
					sess.Resize(ctrl.Rows, ctrl.Cols)
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
	if err := httpx.RequireTypedConfirmation(w, r, "close terminal"); err != nil {
		return err
	}
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
func (s *Server) handleTerminalMeta(w http.ResponseWriter, r *http.Request) error {
	var meta term.SessionMeta
	if err := httpx.DecodeJSON(r, &meta); err != nil {
		return err
	}
	name := chi.URLParam(r, "name")
	if err := s.modules.term.SetMeta(r.Context(), name, meta); err != nil {
		return mapTermError(err)
	}
	httpx.SetAudit(r, "terminal.rename", name,
		map[string]any{"title": meta.Title, "folder": meta.Folder, "favourite": meta.Favourite})
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
		Name   string `json:"name,omitempty"`
		Select bool   `json:"select,omitempty"`
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
	if req.Select {
		if err := s.modules.term.SelectWindow(r.Context(), name, index); err != nil {
			return mapTermError(err)
		}
	}
	httpx.SetAudit(r, "terminal.window.update", name,
		map[string]any{"index": index, "name": req.Name, "select": req.Select})
	httpx.NoContent(w)
	return nil
}

func (s *Server) handleTerminalWindowKill(w http.ResponseWriter, r *http.Request) error {
	name := chi.URLParam(r, "name")
	index, err := strconv.Atoi(chi.URLParam(r, "index"))
	if err != nil {
		return httpx.BadRequest("window index must be a number")
	}
	if err := httpx.RequireTypedConfirmation(w, r, "close window"); err != nil {
		return err
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

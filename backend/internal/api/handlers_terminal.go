package api

import (
	"encoding/json"
	"errors"
	"net/http"
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
	default:
		return httpx.Internal(err)
	}
}

func (s *Server) handleTerminalList(w http.ResponseWriter, r *http.Request) error {
	sessions := s.modules.term.List()
	view := make([]map[string]any, 0, len(sessions))
	for _, sess := range sessions {
		rows, cols := sess.Size()
		view = append(view, map[string]any{
			"id": sess.ID, "title": sess.Title, "shell": sess.Shell,
			"user":      sess.User,
			"persisted": sess.Persisted, "tmuxName": sess.TmuxName,
			"createdAt": sess.CreatedAt, "owner": sess.Owner,
			"rows": rows, "cols": cols, "pid": sess.PID,
			"attached": sess.Attached(), "lastActive": sess.LastActive().UTC(),
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
		"sessions": view,
		"detached": s.modules.term.TmuxSessions(r.Context()),
	})
	return nil
}

type createTerminalRequest struct {
	Title   string `json:"title"`
	CWD     string `json:"cwd"`
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
		Title: req.Title, Owner: p.Username(), CWD: req.CWD,
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

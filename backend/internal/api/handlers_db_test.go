package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wayy01/Just-Dashboard/backend/internal/auth"
	"github.com/Wayy01/Just-Dashboard/backend/internal/dbx"
	"github.com/Wayy01/Just-Dashboard/backend/internal/httpx"
	"github.com/go-chi/chi/v5"
)

// The database surface grew from four destructive routes to eleven. Invariant 3
// says every one enforces a typed confirmation server-side, and the way that
// regresses is somebody adding a twelfth, mounting it correctly inside
// s.destructive, and forgetting the phrase inside the handler. No router-level
// test catches that: the mount looks right.
//
// So this drives each destructive route with no X-Confirm header and asserts it
// is refused. The routes are listed by hand on purpose — a test deriving the
// list from the router would pass just as happily if one fell out of the group.

func dbTestRouter(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	s := testServer(t)
	t.Cleanup(s.Shutdown)

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			p := &httpx.Principal{
				User: &auth.User{ID: 1, Username: "tester"},
				Role: auth.RoleAdmin, Kind: "session", IP: "127.0.0.1",
			}
			next.ServeHTTP(w, req.WithContext(httpx.WithPrincipal(req.Context(), p)))
		})
	})
	s.mountDatabaseRoutes(r)

	// A connection has to exist for a handler to reach its confirmation check.
	// The DSN points nowhere on purpose: if a handler ever dials before
	// confirming, the failure it produces will not mention a confirmation and
	// the assertion below catches the reordering.
	sealed, err := s.Sealer.Seal("/nonexistent/never-opened.db")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.DB.Exec(
		`INSERT INTO db_connections(name, driver, dsn_enc, created_at) VALUES(?,?,?,?)`,
		"testconn", string(dbx.DriverSQLite), sealed, 0); err != nil {
		t.Fatal(err)
	}
	return s, r
}

func TestDestructiveDatabaseRoutesDemandConfirmation(t *testing.T) {
	_, router := dbTestRouter(t)

	cases := []struct{ method, path, body string }{
		{http.MethodDelete, "/databases/1/ddl/table", `{"table":"t"}`},
		{http.MethodDelete, "/databases/1/ddl/column", `{"table":"t","name":"c"}`},
		{http.MethodDelete, "/databases/1/ddl/index", `{"table":"t","name":"i"}`},
		{http.MethodPost, "/databases/1/ddl/truncate", `{"table":"t"}`},
		{http.MethodDelete, "/databases/1/rows", `{"table":"t","key":{"id":1}}`},
		{http.MethodDelete, "/databases/1", `{}`},
		{http.MethodDelete, "/databases/1/keys", `{"key":"k"}`},
		{http.MethodPost, "/databases/1/activity/kill", `{"pid":"42"}`},
	}

	for _, c := range cases {
		req := httptest.NewRequest(c.method, c.path, strings.NewReader(c.body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if !strings.Contains(rec.Body.String(), "confirmation") {
			t.Errorf("%s %s without X-Confirm was not refused for want of a phrase: %d %s",
				c.method, c.path, rec.Code, strings.TrimSpace(rec.Body.String()))
		}
	}
}

// A route whose destructiveness depends on the body rather than the path has to
// check by hand — the same reason POST /query classifies its SQL. An import
// with truncate:true empties the table before loading.
func TestImportChecksDestructivenessByContent(t *testing.T) {
	_, router := dbTestRouter(t)

	truncating := httptest.NewRequest(http.MethodPost, "/databases/1/import",
		strings.NewReader(`{"table":"t","data":"a,b\n1,2","truncate":true}`))
	truncating.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, truncating)
	if !strings.Contains(rec.Body.String(), "confirmation") {
		t.Errorf("a truncating import without X-Confirm was not refused: %d %s",
			rec.Code, strings.TrimSpace(rec.Body.String()))
	}

	// The same route without truncate is an ordinary append and must not demand
	// a phrase — over-confirming teaches people to type without reading, which
	// is the habit the typed confirmation exists to prevent.
	appending := httptest.NewRequest(http.MethodPost, "/databases/1/import",
		strings.NewReader(`{"table":"t","data":"a,b\n1,2"}`))
	appending.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, appending)
	if strings.Contains(rec2.Body.String(), "confirmation") {
		t.Errorf("a non-truncating import demanded a confirmation: %s",
			strings.TrimSpace(rec2.Body.String()))
	}
}

// The engine catalogue is what the frontend builds its tab list from, so an
// engine missing from it is an engine the UI cannot reach.
func TestDriverCatalogueCoversEveryEngine(t *testing.T) {
	_, router := dbTestRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/databases/drivers", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /databases/drivers = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, d := range dbx.Drivers() {
		if !strings.Contains(body, `"id":"`+string(d)+`"`) {
			t.Errorf("driver %q missing from the catalogue: %s", d, body)
		}
	}
}

// The generator catalogue is the other list the frontend used to keep its own
// copy of. Every target the package can generate must appear, with the filename
// the download will use — a target with no filename is a download called
// "undefined".
func TestORMTargetCatalogueCoversEveryGenerator(t *testing.T) {
	_, router := dbTestRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/databases/orm/targets", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /databases/orm/targets = %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, tg := range dbx.ORMTargets() {
		if !strings.Contains(body, `"id":"`+string(tg)+`"`) {
			t.Errorf("target %q missing from the catalogue: %s", tg, body)
		}
		if f := ormFilename(tg); f == "schema.txt" {
			t.Errorf("target %q has no filename of its own", tg)
		}
	}
}

// Rendering a row as SQL must never reach the database. It is a pure
// transformation, and the connection in this harness points at a path that does
// not exist — so a handler that opened a pool first would fail here.
func TestRowSQLDoesNotTouchTheDatabase(t *testing.T) {
	_, router := dbTestRouter(t)
	req := httptest.NewRequest(http.MethodPost, "/databases/1/rows/sql",
		strings.NewReader(`{"table":"users","rows":[{"id":1,"email":"a@x.io"}]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("POST /databases/1/rows/sql = %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "INSERT INTO") {
		t.Errorf("no statement rendered: %s", rec.Body.String())
	}
}

// Search is a read, but it reads every table in the schema, so it is the one
// read that is worth an audit entry. This pins that it asks for a needle rather
// than defaulting to "match everything".
func TestSearchRequiresANeedle(t *testing.T) {
	_, router := dbTestRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/databases/1/search", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("search with no q = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

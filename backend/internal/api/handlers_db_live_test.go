package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/Wayy01/Just-Dashboard/backend/internal/auth"
	"github.com/Wayy01/Just-Dashboard/backend/internal/dbx"
	"github.com/Wayy01/Just-Dashboard/backend/internal/httpx"
	"github.com/go-chi/chi/v5"
)

// End-to-end tests: real HTTP handlers, real dialects, real servers.
//
// The dbx live tests prove the engine layer works. These prove the layer above
// it is wired to the right half of it — which is a different failure. Routing
// Mongo through the SQL export path, or forgetting that Redis has no pool, are
// both bugs that every lower-level test passes straight through.
//
// Like the dbx ones, each engine skips when it is not reachable.

func liveAPIRouter(t *testing.T, driver dbx.Driver, dsn string) (*Server, http.Handler, int64) {
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

	sealed, err := s.Sealer.Seal(dsn)
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.Store.DB.Exec(
		`INSERT INTO db_connections(name, driver, dsn_enc, created_at) VALUES(?,?,?,?)`,
		"live-"+string(driver), string(driver), sealed, 0)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()

	// Reachability is checked through the product's own ping route, so a
	// skipped test means the engine is down rather than that the route is.
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, pathf("/databases/%d/ping", id), nil))
	var ping struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &ping)
	if !ping.OK {
		t.Skipf("%s unreachable: %s", driver, ping.Error)
	}
	return s, r, id
}

func pathf(format string, id int64) string {
	return strings.Replace(format, "%d", itoaLocal(int(id)), 1)
}

func do(t *testing.T, r http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *strings.Reader
	if body == "" {
		rdr = strings.NewReader("")
	} else {
		rdr = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rdr)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// TestLiveAPIMongo drives the Mongo surface through HTTP, including the export
// and import routes that branch by driver inside a handler shared with SQL.
func TestLiveAPIMongo(t *testing.T) {
	dsn := envOr("JD_TEST_MONGO_DSN", "mongodb://127.0.0.1:27017/jdtest")
	_, r, id := liveAPIRouter(t, dbx.DriverMongo, dsn)
	const coll = "jd_api_docs"

	// Start clean, then seed through the product's own insert route.
	do(t, r, http.MethodDelete, pathf("/databases/%d/collections", id),
		`{"collection":"`+coll+`"}`)
	t.Cleanup(func() {
		do(t, r, http.MethodDelete, pathf("/databases/%d/collections", id),
			`{"collection":"`+coll+`"}`)
	})

	// The document travels as a JSON string, which is what the editor produces.
	for _, doc := range []string{`{\"name\":\"Ann\",\"age\":31}`, `{\"name\":\"Bo\",\"age\":24}`} {
		rec := do(t, r, http.MethodPost, pathf("/databases/%d/documents", id),
			`{"collection":"`+coll+`","document":"`+doc+`"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("insert: %d %s", rec.Code, rec.Body.String())
		}
	}

	t.Run("export_csv_routes_to_mongo", func(t *testing.T) {
		rec := do(t, r, http.MethodGet,
			pathf("/databases/%d/export", id)+"?table="+coll+"&format=csv", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("export: %d %s", rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		if !strings.HasPrefix(body, "_id") {
			t.Errorf("csv header = %q, want _id first", strings.SplitN(body, "\n", 2)[0])
		}
		if !strings.Contains(body, "Ann") {
			t.Errorf("export missing seeded data:\n%s", body)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "text/csv" {
			t.Errorf("content-type = %q", ct)
		}
	})

	t.Run("export_honours_filter", func(t *testing.T) {
		rec := do(t, r, http.MethodGet,
			pathf("/databases/%d/export", id)+"?table="+coll+"&format=json&filter="+
				`%7B%22name%22%3A%22Ann%22%7D`, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("filtered export: %d %s", rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "Bo") {
			t.Errorf("filtered export leaked a non-matching document:\n%s", rec.Body.String())
		}
	})

	t.Run("import_routes_to_mongo", func(t *testing.T) {
		rec := do(t, r, http.MethodPost, pathf("/databases/%d/import", id),
			`{"table":"`+coll+`","format":"json","data":"[{\"name\":\"Cy\"}]"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("import: %d %s", rec.Code, rec.Body.String())
		}
		var res struct {
			Inserted int `json:"inserted"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &res)
		if res.Inserted != 1 {
			t.Errorf("imported %d, want 1: %s", res.Inserted, rec.Body.String())
		}
	})

	t.Run("browse_and_indexes", func(t *testing.T) {
		rec := do(t, r, http.MethodGet,
			pathf("/databases/%d/browse", id)+"?schema=jdtest&table="+coll+"&limit=10", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("browse: %d %s", rec.Code, rec.Body.String())
		}
		rec = do(t, r, http.MethodGet,
			pathf("/databases/%d/collections/indexes", id)+"?schema=jdtest&table="+coll, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("indexes: %d %s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "_id_") {
			t.Errorf("index listing missing the _id index: %s", rec.Body.String())
		}
	})

	t.Run("sql_only_routes_refuse_mongo", func(t *testing.T) {
		// The SQL surface must decline rather than half-work.
		rec := do(t, r, http.MethodGet, pathf("/databases/%d/outline", id), "")
		if rec.Code == http.StatusOK {
			t.Errorf("outline answered for a Mongo connection: %s", rec.Body.String())
		}
	})
}

// TestLiveAPIRedis drives the key surface through HTTP, including the member
// operations added for collection editing.
func TestLiveAPIRedis(t *testing.T) {
	dsn := envOr("JD_TEST_REDIS_DSN", "redis://127.0.0.1:6379/0")
	_, r, id := liveAPIRouter(t, dbx.DriverRedis, dsn)
	const key = "jdapi:hash"

	do(t, r, http.MethodDelete, pathf("/databases/%d/keys", id), `{"key":"`+key+`"}`)

	t.Run("create_and_read", func(t *testing.T) {
		rec := do(t, r, http.MethodPost, pathf("/databases/%d/keys/value", id),
			`{"key":"`+key+`","type":"hash","field":"name","value":"Ann","create":true}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
		}
		rec = do(t, r, http.MethodPost, pathf("/databases/%d/keys/value", id),
			`{"key":"`+key+`","type":"hash","field":"city","value":"Oslo"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("add field: %d %s", rec.Code, rec.Body.String())
		}
		rec = do(t, r, http.MethodGet, pathf("/databases/%d/keys/value", id)+"?key="+key, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("read: %d %s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "Oslo") {
			t.Errorf("value missing the field just written: %s", rec.Body.String())
		}
	})

	t.Run("member_delete_needs_confirmation", func(t *testing.T) {
		rec := do(t, r, http.MethodDelete, pathf("/databases/%d/keys", id),
			`{"key":"`+key+`","type":"hash","member":"city"}`)
		if !strings.Contains(rec.Body.String(), "confirmation") {
			t.Fatalf("member delete ran without a confirmation: %d %s", rec.Code, rec.Body.String())
		}
		// With the phrase — the member, not the key — it goes through.
		req := httptest.NewRequest(http.MethodDelete, pathf("/databases/%d/keys", id),
			strings.NewReader(`{"key":"`+key+`","type":"hash","member":"city"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Confirm", "city")
		rec2 := httptest.NewRecorder()
		r.ServeHTTP(rec2, req)
		if rec2.Code != http.StatusOK {
			t.Fatalf("confirmed member delete: %d %s", rec2.Code, rec2.Body.String())
		}
		// The rest of the hash must survive.
		rec3 := do(t, r, http.MethodGet, pathf("/databases/%d/keys/value", id)+"?key="+key, "")
		if strings.Contains(rec3.Body.String(), "Oslo") {
			t.Error("member survived its deletion")
		}
		if !strings.Contains(rec3.Body.String(), "Ann") {
			t.Errorf("deleting one member took the rest with it: %s", rec3.Body.String())
		}
	})

	t.Run("rename_and_scan", func(t *testing.T) {
		target := "jdapi:renamed"
		do(t, r, http.MethodDelete, pathf("/databases/%d/keys", id), `{"key":"`+target+`"}`)
		rec := do(t, r, http.MethodPost, pathf("/databases/%d/keys/rename", id),
			`{"key":"`+key+`","to":"`+target+`"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("rename: %d %s", rec.Code, rec.Body.String())
		}
		rec = do(t, r, http.MethodGet, pathf("/databases/%d/keys", id)+"?pattern=jdapi:*", "")
		if !strings.Contains(rec.Body.String(), target) {
			t.Errorf("scan did not find the renamed key: %s", rec.Body.String())
		}
		// Clean up through the product's own route.
		req := httptest.NewRequest(http.MethodDelete, pathf("/databases/%d/keys", id),
			strings.NewReader(`{"key":"`+target+`"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Confirm", target)
		r.ServeHTTP(httptest.NewRecorder(), req)
	})
}

// TestLiveAPIPostgres exercises the SQL surface end to end on the one engine
// most installs will actually point at.
func TestLiveAPIPostgres(t *testing.T) {
	dsn := envOr("JD_TEST_POSTGRES_DSN", "postgres://jdtest:jdtest@127.0.0.1:5432/jdtest?sslmode=disable")
	_, r, id := liveAPIRouter(t, dbx.DriverPostgres, dsn)
	const table = "jd_api_rows"

	drop := func() {
		req := httptest.NewRequest(http.MethodDelete, pathf("/databases/%d/ddl/table", id),
			strings.NewReader(`{"schema":"public","table":"`+table+`"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Confirm", table)
		r.ServeHTTP(httptest.NewRecorder(), req)
	}
	drop()
	t.Cleanup(drop)

	t.Run("create_table_then_use_it", func(t *testing.T) {
		rec := do(t, r, http.MethodPost, pathf("/databases/%d/ddl/table", id),
			`{"schema":"public","table":"`+table+`","columns":[
				{"name":"id","type":"integer","primaryKey":true,"notNull":true},
				{"name":"label","type":"text"}]}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("create table: %d %s", rec.Code, rec.Body.String())
		}

		rec = do(t, r, http.MethodPost, pathf("/databases/%d/rows", id),
			`{"schema":"public","table":"`+table+`","values":{"id":1,"label":"one"}}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("insert row: %d %s", rec.Code, rec.Body.String())
		}

		rec = do(t, r, http.MethodPost, pathf("/databases/%d/import", id),
			`{"schema":"public","table":"`+table+`","format":"csv","hasHeader":true,"data":"id,label\n2,two\n3,three\n"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("import: %d %s", rec.Code, rec.Body.String())
		}

		rec = do(t, r, http.MethodGet,
			pathf("/databases/%d/count", id)+"?schema=public&table="+table, "")
		if !strings.Contains(rec.Body.String(), `"count":3`) {
			t.Errorf("count after import = %s, want 3", rec.Body.String())
		}

		// Sorted, filtered browse through the query string the grid builds.
		rec = do(t, r, http.MethodGet,
			pathf("/databases/%d/browse", id)+"?schema=public&table="+table+
				"&limit=10&orderBy=id&dir=desc&filters="+
				`%5B%7B%22column%22%3A%22label%22%2C%22op%22%3A%22contains%22%2C%22value%22%3A%22t%22%7D%5D`, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("browse: %d %s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "three") || strings.Contains(rec.Body.String(), "one") {
			t.Errorf("filtered browse returned the wrong rows: %s", rec.Body.String())
		}

		rec = do(t, r, http.MethodGet,
			pathf("/databases/%d/export", id)+"?schema=public&table="+table+"&format=csv", "")
		if !strings.Contains(rec.Body.String(), "id,label") {
			t.Errorf("export: %s", rec.Body.String())
		}

		rec = do(t, r, http.MethodGet, pathf("/databases/%d/outline", id)+"?schema=public", "")
		if !strings.Contains(rec.Body.String(), table) {
			t.Errorf("outline missing the new table: %s", rec.Body.String())
		}
		rec = do(t, r, http.MethodPost, pathf("/databases/%d/orm", id), `{"target":"prisma"}`)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "model") {
			t.Errorf("orm generation: %d %s", rec.Code, rec.Body.String())
		}
	})
}

package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wayy01/Just-Dashboard/backend/internal/auth"
	"github.com/Wayy01/Just-Dashboard/backend/internal/files"
)

// The file manager's read surface, driven through the whole chain with a real
// signed-in admin: the allowlist, the limiter, authentication, the capability
// group and the handler.
//
// Unit tests in internal/files pin what each of these decides. What they
// cannot say is whether the route was mounted inside the group it was meant to
// be in, which is the failure that ships — a preview route accidentally inside
// the file.write group is invisible to an admin and a 403 for everybody else.

// fileFixture writes a small tree into the server's configured root and
// returns it.
func fileFixture(t *testing.T, s *Server) string {
	t.Helper()
	root := s.modules.files.Roots()[0]
	if err := os.MkdirAll(filepath.Join(root, "etc", "nginx"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "etc", "nginx", "nginx.conf"),
		[]byte("server {\n  listen 80;\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "index.html"),
		[]byte("<script>alert(1)</script>"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func query(path string, q map[string]string) string {
	values := url.Values{}
	for k, v := range q {
		values.Set(k, v)
	}
	return path + "?" + values.Encode()
}

func TestFileBrowsingRoutesAnswerAReader(t *testing.T) {
	c, s := newClient(t)
	root := fileFixture(t, s)

	for _, tc := range []struct{ name, path string }{
		{"places", "/api/v1/files/places"},
		{"complete", query("/api/v1/files/complete", map[string]string{"prefix": root + "/"})},
		{"find", query("/api/v1/files/find", map[string]string{"path": root, "q": "ngnx"})},
		{"preview", query("/api/v1/files/preview", map[string]string{"path": filepath.Join(root, "etc/nginx/nginx.conf")})},
		{"usage", query("/api/v1/files/usage", map[string]string{"path": root})},
		{"checksum", query("/api/v1/files/checksum", map[string]string{"path": filepath.Join(root, "index.html")})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := c.do(http.MethodGet, tc.path, "", nil)
			if w.Code != http.StatusOK {
				t.Fatalf("got %d: %s", w.Code, strings.TrimSpace(w.Body.String()))
			}
			var body any
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("body is not JSON: %v", err)
			}
		})
	}

	// The finder is the feature, not the plumbing: a typo'd query with the
	// characters in order has to find the file, or nobody uses it twice.
	w := c.do(http.MethodGet, query("/api/v1/files/find", map[string]string{"path": root, "q": "ngnxcnf"}), "", nil)
	var found files.FindResult
	if err := json.Unmarshal(w.Body.Bytes(), &found); err != nil {
		t.Fatal(err)
	}
	if len(found.Hits) == 0 || filepath.Base(found.Hits[0].Path) != "nginx.conf" {
		t.Fatalf("fuzzy find returned %+v", found.Hits)
	}
}

// The page opens at home rather than at "/", and home has to be a path the
// very next request can list.
func TestPlacesStartsSomewhereListable(t *testing.T) {
	c, s := newClient(t)
	fileFixture(t, s)

	w := c.do(http.MethodGet, "/api/v1/files/places", "", nil)
	var places struct {
		Home   string        `json:"home"`
		Roots  []string      `json:"roots"`
		Places []files.Place `json:"places"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &places); err != nil {
		t.Fatal(err)
	}
	if places.Home == "" || len(places.Places) == 0 {
		t.Fatalf("places = %+v", places)
	}
	listing := c.do(http.MethodGet, query("/api/v1/files/list", map[string]string{"path": places.Home}), "", nil)
	if listing.Code != http.StatusOK {
		t.Fatalf("home %q does not list: %d %s", places.Home, listing.Code, listing.Body.String())
	}
	for _, p := range places.Places {
		if got := c.do(http.MethodGet, query("/api/v1/files/list", map[string]string{"path": p.Path}), "", nil); got.Code != http.StatusOK {
			t.Errorf("place %q (%s) does not list: %d", p.Path, p.Kind, got.Code)
		}
	}
}

// The raw route hands a file back with a content type the browser acts on, on
// the origin that holds the session. What it will serve is a closed list, and
// this is the test that keeps it closed.
func TestRawRefusesAnythingButMedia(t *testing.T) {
	c, s := newClient(t)
	root := fileFixture(t, s)

	w := c.do(http.MethodGet, query("/api/v1/files/raw", map[string]string{"path": filepath.Join(root, "index.html")}), "", nil)
	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("HTML was served inline: %d %s", w.Code, w.Body.String())
	}
	w = c.do(http.MethodGet, query("/api/v1/files/raw", map[string]string{"path": filepath.Join(root, "etc/nginx/nginx.conf")}), "", nil)
	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("a config file was served inline: %d", w.Code)
	}

	png := filepath.Join(root, "logo.png")
	// The eight bytes of a PNG signature: enough for the route, which decides
	// on the name rather than on the content.
	if err := os.WriteFile(png, []byte("\x89PNG\r\n\x1a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	w = c.do(http.MethodGet, query("/api/v1/files/raw", map[string]string{"path": png}), "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("a PNG was refused: %d %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); got != "image/png" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := w.Header().Get("Content-Disposition"); !strings.HasPrefix(got, "inline") {
		t.Errorf("Content-Disposition = %q, want inline", got)
	}
	if got := w.Header().Get("Content-Security-Policy"); !strings.Contains(got, "sandbox") {
		t.Errorf("CSP = %q, want the sandbox that neuters an inline SVG", got)
	}
}

// Containment reaches the new routes too. Every one of them takes a path from
// the client, and every one of them has to answer the same way to a path
// outside JD_FILE_ROOTS.
func TestNewFileRoutesRefusePathsOutsideTheRoots(t *testing.T) {
	c, _ := newClient(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.png"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		query("/api/v1/files/preview", map[string]string{"path": filepath.Join(outside, "secret.png")}),
		query("/api/v1/files/usage", map[string]string{"path": outside}),
		query("/api/v1/files/checksum", map[string]string{"path": filepath.Join(outside, "secret.png")}),
		query("/api/v1/files/raw", map[string]string{"path": filepath.Join(outside, "secret.png")}),
		query("/api/v1/files/find", map[string]string{"path": outside, "q": "secret"}),
	} {
		w := c.do(http.MethodGet, path, "", nil)
		if w.Code != http.StatusForbidden {
			t.Errorf("%s answered %d, want 403 outside_root", path, w.Code)
		}
	}
}

// Bookmarks are a write, so they sit in the file.write group: a readonly
// session may read the rail and may not rearrange it for everybody else.
func TestBookmarksAreAWriteAndAreStored(t *testing.T) {
	c, s := newClient(t)
	root := fileFixture(t, s)

	body := `{"bookmarks":[{"path":"` + filepath.Join(root, "etc/nginx") + `","name":"nginx"}]}`
	if w := c.do(http.MethodPut, "/api/v1/files/bookmarks", body, nil); w.Code != http.StatusOK {
		t.Fatalf("saving a bookmark: %d %s", w.Code, w.Body.String())
	}
	w := c.do(http.MethodGet, "/api/v1/files/places", "", nil)
	var places struct {
		Bookmarks []struct{ Path, Name string } `json:"bookmarks"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &places); err != nil {
		t.Fatal(err)
	}
	if len(places.Bookmarks) != 1 || places.Bookmarks[0].Name != "nginx" {
		t.Fatalf("bookmarks = %+v", places.Bookmarks)
	}
	// Stored resolved, so a bookmark can never be the way a path the roots
	// refuse gets remembered and offered.
	outside := t.TempDir()
	if w := c.do(http.MethodPut, "/api/v1/files/bookmarks",
		`{"bookmarks":[{"path":"`+outside+`"}]}`, nil); w.Code != http.StatusForbidden {
		t.Fatalf("a bookmark outside the roots was accepted: %d", w.Code)
	}

	readonly := &client{t: t, h: s.Routes(), cookie: signInAs(t, s, "reader", auth.RoleReadOnly)}
	if w := readonly.do(http.MethodGet, "/api/v1/files/places", "", nil); w.Code != http.StatusOK {
		t.Fatalf("a reader cannot see the rail: %d", w.Code)
	}
	if w := readonly.do(http.MethodPut, "/api/v1/files/bookmarks", body, nil); w.Code != http.StatusForbidden {
		t.Fatalf("a reader rearranged the rail: %d %s", w.Code, w.Body.String())
	}
}

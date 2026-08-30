package api

import (
	"context"
	"encoding/json"
	"mime"
	"net/http"
	"path/filepath"
	"time"

	"github.com/Wayy01/Just-Dashboard/backend/internal/files"
	"github.com/Wayy01/Just-Dashboard/backend/internal/httpx"
)

// The read side of the file manager that is not "give me this file": where to
// start, what is under the cursor, and how to get somewhere by typing.
//
// It is a second file for the reason handlers_docker_manage.go is: the write
// surface in handlers_files.go is already long, these routes share nothing
// with it but the module, and the route map stays in one place because
// mountFileRoutes still mounts both.
const filesBookmarksKey = "files.bookmarks"

// A bookmark is the dashboard's record and not the browser's, unlike the sort
// order or which panel is open. Where an operator's work lives on a given
// server is a fact about the server: it should be there from a laptop, from a
// phone, and after the browser's storage is cleared — the same argument that
// puts terminal folders in the settings table.
type fileBookmark struct {
	Path string `json:"path"`
	Name string `json:"name,omitempty"`
}

type filePlacesResponse struct {
	Home      string         `json:"home"`
	Roots     []string       `json:"roots"`
	Places    []files.Place  `json:"places"`
	Bookmarks []fileBookmark `json:"bookmarks"`
}

func (s *Server) fileBookmarks(ctx context.Context) []fileBookmark {
	out := []fileBookmark{}
	raw, ok, err := s.Store.Setting(ctx, filesBookmarksKey)
	if err != nil || !ok || raw == "" {
		return out
	}
	if json.Unmarshal([]byte(raw), &out) != nil {
		return []fileBookmark{}
	}
	return out
}

func (s *Server) handleFilePlaces(w http.ResponseWriter, r *http.Request) error {
	svc := s.modules.files
	httpx.JSON(w, http.StatusOK, filePlacesResponse{
		Home:      svc.Home(),
		Roots:     svc.Roots(),
		Places:    svc.Places(),
		Bookmarks: s.fileBookmarks(r.Context()),
	})
	return nil
}

// handleFileBookmarks takes the whole list rather than one entry, because the
// list is also reorderable: an add, a rename, a removal and a drag are then
// the same request and cannot disagree about the order.
func (s *Server) handleFileBookmarks(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Bookmarks []fileBookmark `json:"bookmarks"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	clean := []fileBookmark{}
	seen := map[string]bool{}
	for _, b := range req.Bookmarks {
		// Resolved before it is stored, so a bookmark can never be a way to
		// keep a path the roots would refuse — and so the stored form is the
		// canonical one the listing will report.
		full, err := s.modules.files.Resolve(b.Path)
		if err != nil {
			return mapFileError(err)
		}
		if seen[full] || len(clean) >= 60 {
			continue
		}
		seen[full] = true
		name := b.Name
		if name == "" {
			name = filepath.Base(full)
		}
		clean = append(clean, fileBookmark{Path: full, Name: name})
	}
	encoded, err := json.Marshal(clean)
	if err != nil {
		return httpx.Internal(err)
	}
	if err := s.Store.SetSetting(r.Context(), filesBookmarksKey, string(encoded)); err != nil {
		return httpx.Internal(err)
	}
	httpx.SetAudit(r, "file.bookmarks", "", map[string]any{"count": len(clean)})
	httpx.JSON(w, http.StatusOK, map[string]any{"bookmarks": clean})
	return nil
}

func (s *Server) handleFileComplete(w http.ResponseWriter, r *http.Request) error {
	entries := s.modules.files.Complete(r.URL.Query().Get("prefix"),
		atoiDefault(r.URL.Query().Get("limit"), 0))
	httpx.JSON(w, http.StatusOK, entries)
	return nil
}

func (s *Server) handleFileFind(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	if q.Get("q") == "" {
		return httpx.BadRequest("q query parameter is required")
	}
	// The handler's own ceiling sits above the walk's budget on purpose: the
	// walk stops itself and reports a partial answer, and this is only the
	// backstop for a filesystem that blocks on a read.
	ctx, cancel := timeoutCtx(r, 15*time.Second)
	defer cancel()
	result, err := s.modules.files.Find(ctx, files.FindOptions{
		Root:     q.Get("path"),
		Query:    q.Get("q"),
		Limit:    atoiDefault(q.Get("limit"), 0),
		Hidden:   q.Get("hidden") == "true",
		MaxDepth: atoiDefault(q.Get("depth"), 0),
		Budget:   time.Duration(atoiDefault(q.Get("budgetMs"), 0)) * time.Millisecond,
	})
	if err != nil {
		return mapFileError(err)
	}
	httpx.JSON(w, http.StatusOK, result)
	return nil
}

func (s *Server) handleFilePreview(w http.ResponseWriter, r *http.Request) error {
	preview, err := s.modules.files.Preview(r.URL.Query().Get("path"))
	if err != nil {
		return mapFileError(err)
	}
	httpx.JSON(w, http.StatusOK, preview)
	return nil
}

func (s *Server) handleFileUsage(w http.ResponseWriter, r *http.Request) error {
	ctx, cancel := timeoutCtx(r, 45*time.Second)
	defer cancel()
	usage, err := s.modules.files.Usage(ctx, r.URL.Query().Get("path"),
		time.Duration(atoiDefault(r.URL.Query().Get("budgetMs"), 0))*time.Millisecond)
	if err != nil {
		return mapFileError(err)
	}
	httpx.JSON(w, http.StatusOK, usage)
	return nil
}

func (s *Server) handleFileChecksum(w http.ResponseWriter, r *http.Request) error {
	ctx, cancel := timeoutCtx(r, 5*time.Minute)
	defer cancel()
	sum, err := s.modules.files.Checksum(ctx, r.URL.Query().Get("path"), r.URL.Query().Get("algo"))
	if err != nil {
		return mapFileError(err)
	}
	httpx.JSON(w, http.StatusOK, sum)
	return nil
}

// handleFileRaw serves a file's own bytes for the browser to render: an image
// in the preview pane, a video with a seek bar, a PDF.
//
// It is a separate route from /files/download rather than a flag on it, and
// the difference is the security boundary. Download says
// `Content-Type: application/octet-stream` and `Content-Disposition:
// attachment`, which is inert whatever the file contains. This one hands a
// file back with a content type the browser will act on, on the same origin as
// a session that drives the Docker socket — so what it may serve is a closed
// allowlist of media types (files.MediaType), and anything else is refused
// rather than guessed at. An HTML file uploaded to a web root is not
// previewable here, and that is the point: it would run as this dashboard.
//
// The CSP is tightened on top of the middleware's, because the one media type
// on the list that can carry script is SVG. `sandbox` puts a directly opened
// one in an opaque origin, where its script — already blocked by
// `default-src 'none'` — could not reach a cookie even if it ran.
func (s *Server) handleFileRaw(w http.ResponseWriter, r *http.Request) error {
	path := r.URL.Query().Get("path")
	if path == "" {
		return httpx.BadRequest("path query parameter is required")
	}
	mediaType := files.MediaType(filepath.Base(path))
	if mediaType == "" {
		return httpx.Err(http.StatusUnsupportedMediaType, "not_inline",
			"this file is not one of the media types served inline; download it instead")
	}
	f, st, err := s.modules.files.Open(path)
	if err != nil {
		return mapFileError(err)
	}
	defer f.Close()
	if st.IsDir() {
		return httpx.BadRequest("path is a directory")
	}
	name := filepath.Base(st.Name())
	w.Header().Set("Content-Type", mediaType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": name}))
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox; frame-ancestors 'none'")
	// A short private cache rather than the API's usual no-store: a grid of
	// forty thumbnails re-reads every JPEG on every scroll otherwise. Callers
	// append the file's modification time to the URL, so a saved image is a
	// different URL and is never served from a stale entry.
	w.Header().Set("Cache-Control", "private, max-age=30")
	http.ServeContent(w, r, name, st.ModTime(), f)
	return nil
}

package api

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/Wayy01/Just-Dashboard/backend/internal/httpx"
	"github.com/Wayy01/Just-Dashboard/backend/internal/logsx"
	"github.com/go-chi/chi/v5"
)

func (s *Server) mountLogRoutes(r chi.Router) {
	r.Route("/logs", func(r chi.Router) {
		r.Method(http.MethodGet, "/sources", s.handle(s.handleLogSources))
		r.Method(http.MethodGet, "/search", s.handle(s.handleLogSearch))
		r.Method(http.MethodGet, "/download", s.handle(s.handleLogDownload))
		r.Method(http.MethodGet, "/stream", s.handle(s.handleLogStream))
		r.Method(http.MethodGet, "/logrotate", s.handle(s.handleLogrotate))
	})
}

// handleLogSources merges file-backed sources with the live sources that are
// not files — docker containers and PM2 processes — so the viewer offers one
// list regardless of where the log actually lives.
func (s *Server) handleLogSources(w http.ResponseWriter, r *http.Request) error {
	sources, err := s.modules.logs.Discover(r.Context())
	if err != nil {
		return httpx.Internal(err)
	}
	if containers, err := s.modules.docker.ListContainers(r.Context(), false); err == nil {
		for _, c := range containers {
			sources = append(sources, logsx.Source{
				ID:    "docker:" + c.ID,
				Label: c.Name,
				Kind:  logsx.KindDocker,
			})
		}
	}
	if s.modules.pm2.Available() {
		if list, err := s.modules.pm2.List(r.Context()); err == nil {
			for _, p := range list {
				sources = append(sources, logsx.Source{
					ID:    "pm2:" + p.Name,
					Label: p.Name,
					Kind:  logsx.KindPM2,
					Path:  p.OutLogPath,
				})
				// PM2 writes its logs wherever the ecosystem file says, which
				// is routinely outside /var/log. Trusting the path because
				// PM2 reported it keeps those readable without widening the
				// roots for everything else.
				if p.OutLogPath != "" {
					s.modules.logs.AllowExtra(filepath.Dir(p.OutLogPath))
				}
			}
		}
	}
	sources = append(sources, logsx.Source{ID: "journal:", Label: "systemd journal", Kind: logsx.KindJournal})
	httpx.JSON(w, http.StatusOK, sources)
	return nil
}

func parseSearchOptions(r *http.Request) logsx.SearchOptions {
	q := r.URL.Query()
	opts := logsx.SearchOptions{
		Query:      q.Get("q"),
		Regex:      q.Get("regex") == "true",
		IgnoreCase: q.Get("ignoreCase") != "false",
		Limit:      atoiDefault(q.Get("limit"), 2000),
	}
	if levels := q.Get("levels"); levels != "" {
		opts.Levels = strings.Split(levels, ",")
	}
	if v := q.Get("since"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			opts.Since = t
		}
	}
	if v := q.Get("until"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			opts.Until = t
		}
	}
	return opts
}

func (s *Server) handleLogSearch(w http.ResponseWriter, r *http.Request) error {
	path := r.URL.Query().Get("path")
	if path == "" {
		return httpx.BadRequest("path query parameter is required")
	}
	ctx, cancel := timeoutCtx(r, 60*time.Second)
	defer cancel()
	res, err := s.modules.logs.Search(ctx, path, parseSearchOptions(r))
	if err != nil {
		return httpx.BadRequest("%v", err)
	}
	httpx.JSON(w, http.StatusOK, res)
	return nil
}

// handleLogDownload streams the requested window straight to the client rather
// than buffering it, so exporting a day out of a large log costs no memory.
func (s *Server) handleLogDownload(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	path := q.Get("path")
	if path == "" {
		return httpx.BadRequest("path query parameter is required")
	}
	var since, until time.Time
	if v := q.Get("since"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return httpx.BadRequest("since must be an RFC3339 timestamp")
		}
		since = t
	}
	if v := q.Get("until"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return httpx.BadRequest("until must be an RFC3339 timestamp")
		}
		until = t
	}
	name := filepath.Base(path)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name+".txt"))
	ctx, cancel := timeoutCtx(r, 5*time.Minute)
	defer cancel()
	if _, err := s.modules.logs.Range(ctx, path, since, until, w); err != nil {
		// Headers are already committed; the truncated body plus the audit
		// record is the honest outcome here.
		s.Log.Error("log export failed", "path", path, "err", err)
	}
	return nil
}

// handleLogStream is the unified live tail. The source ID determines the
// backend: a docker container, a PM2 process, the journal, or a plain file.
func (s *Server) handleLogStream(w http.ResponseWriter, r *http.Request) error {
	source := r.URL.Query().Get("source")
	if source == "" {
		source = r.URL.Query().Get("path")
	}
	if source == "" {
		return httpx.BadRequest("source query parameter is required")
	}
	if id, ok := strings.CutPrefix(source, "docker:"); ok {
		return s.streamDockerLogsByID(w, r, id)
	}
	if name, ok := strings.CutPrefix(source, "pm2:"); ok {
		return s.streamPM2LogsByName(w, r, name)
	}
	if unit, ok := strings.CutPrefix(source, "journal:"); ok {
		return s.streamJournal(w, r, unit)
	}

	lines := atoiDefault(r.URL.Query().Get("lines"), 300)
	filter := strings.ToLower(r.URL.Query().Get("q"))
	levels := map[string]bool{}
	for _, l := range strings.Split(r.URL.Query().Get("levels"), ",") {
		if n := logsx.Normalise(l); n != "" {
			levels[n] = true
		}
	}

	conn, err := s.WS.Upgrade(w, r)
	if err != nil {
		return nil
	}
	defer conn.Close()
	ctx, cancel := contextWithCancel(r)
	defer cancel()
	go conn.Keepalive(ctx)
	go conn.DrainControl(cancel)

	ch, err := s.modules.logs.Tail(ctx, source, lines)
	if err != nil {
		conn.SendError(err.Error())
		return nil
	}
	name := filepath.Base(source)
	batch := make([]logsx.Line, 0, 256)
	flush := time.NewTicker(150 * time.Millisecond)
	defer flush.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case text, ok := <-ch:
			if !ok {
				if len(batch) > 0 {
					conn.Send("logs", batch)
				}
				conn.Send("eof", nil)
				return nil
			}
			// Filtering server-side keeps a chatty log from being shipped in
			// full only for the browser to discard most of it.
			if filter != "" && !strings.Contains(strings.ToLower(text), filter) {
				continue
			}
			line := logsx.ParseLine(text, name)
			if len(levels) > 0 && !levels[line.Level] {
				continue
			}
			batch = append(batch, line)
			if len(batch) >= 256 {
				if err := conn.Send("logs", batch); err != nil {
					return nil
				}
				batch = batch[:0]
			}
		case <-flush.C:
			if len(batch) > 0 {
				if err := conn.Send("logs", batch); err != nil {
					return nil
				}
				batch = batch[:0]
			}
		}
	}
}

func (s *Server) streamDockerLogsByID(w http.ResponseWriter, r *http.Request, id string) error {
	rctx := chi.RouteContext(r.Context())
	rctx.URLParams.Add("id", id)
	return s.handleContainerLogStream(w, r)
}

func (s *Server) streamPM2LogsByName(w http.ResponseWriter, r *http.Request, name string) error {
	rctx := chi.RouteContext(r.Context())
	rctx.URLParams.Add("name", name)
	return s.handlePM2LogStream(w, r)
}

func (s *Server) streamJournal(w http.ResponseWriter, r *http.Request, unit string) error {
	rctx := chi.RouteContext(r.Context())
	rctx.URLParams.Add("name", unit)
	return s.handleUnitJournalStream(w, r)
}

func (s *Server) handleLogrotate(w http.ResponseWriter, r *http.Request) error {
	st, err := logsx.LogrotateStatus(r.Context())
	if err != nil {
		return httpx.Internal(err)
	}
	httpx.JSON(w, http.StatusOK, st)
	return nil
}

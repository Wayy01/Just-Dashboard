package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/Wayy01/vps-dashboard/backend/internal/httpx"
	"github.com/Wayy01/vps-dashboard/backend/internal/sysinfo"
	"github.com/go-chi/chi/v5"
)

func (s *Server) mountSystemRoutes(r chi.Router) {
	r.Route("/system", func(r chi.Router) {
		r.Method(http.MethodGet, "/host", s.handle(s.handleSystemHost))
		r.Method(http.MethodGet, "/metrics", s.handle(s.handleSystemMetrics))
		r.Method(http.MethodGet, "/disk-usage", s.handle(s.handleDiskBreakdown))
		r.Method(http.MethodGet, "/stream", s.handle(s.handleSystemStream))
	})
}

func (s *Server) handleSystemHost(w http.ResponseWriter, r *http.Request) error {
	info, err := s.modules.sys.Host(r.Context())
	if err != nil {
		return httpx.Internal(err)
	}
	httpx.JSON(w, http.StatusOK, info)
	return nil
}

func (s *Server) handleSystemMetrics(w http.ResponseWriter, r *http.Request) error {
	snap, err := s.modules.sys.Collect(r.Context())
	if err != nil {
		return httpx.Internal(err)
	}
	httpx.JSON(w, http.StatusOK, snap)
	return nil
}

// handleDiskBreakdown answers "what is filling this mount". It is a recursive
// walk, so it runs under a hard deadline and returns whatever it measured if
// the tree is too large to finish.
func (s *Server) handleDiskBreakdown(w http.ResponseWriter, r *http.Request) error {
	path := r.URL.Query().Get("path")
	if path == "" {
		path = "/"
	}
	limit := atoiDefault(r.URL.Query().Get("limit"), 25)
	ctx, cancel := timeoutCtx(r, 45*time.Second)
	defer cancel()
	entries, err := sysinfo.DirBreakdown(ctx, path, limit)
	if err != nil {
		return httpx.BadRequest("cannot scan %s: %v", path, err)
	}
	httpx.JSON(w, http.StatusOK, entries)
	return nil
}

// handleSystemStream pushes a metrics snapshot on an interval the client
// chooses within a sane band, so a dashboard tab left open does not sample
// the host harder than it needs to.
func (s *Server) handleSystemStream(w http.ResponseWriter, r *http.Request) error {
	interval := 2 * time.Second
	if v := r.URL.Query().Get("interval"); v != "" {
		if ms, err := strconv.Atoi(v); err == nil {
			interval = clampDuration(time.Duration(ms)*time.Millisecond, time.Second, 30*time.Second)
		}
	}
	conn, err := s.WS.Upgrade(w, r)
	if err != nil {
		return nil // the upgrader has already written a response
	}
	defer conn.Close()

	ctx, cancel := contextWithCancel(r)
	defer cancel()
	go conn.Keepalive(ctx)
	go conn.DrainControl(cancel)

	// A fresh collector per socket keeps each client's rate deltas aligned to
	// its own interval instead of whatever the last poller happened to use.
	collector := sysinfo.NewCollector()
	if host, err := s.modules.sys.Host(ctx); err == nil {
		conn.Send("host", host)
	}
	collector.Collect(ctx) // prime the counters so the first frame carries rates

	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			snap, err := collector.Collect(ctx)
			if err != nil {
				conn.SendError(err.Error())
				continue
			}
			if err := conn.Send("metrics", snap); err != nil {
				return nil
			}
		}
	}
}

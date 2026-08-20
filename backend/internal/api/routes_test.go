package api

import (
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Wayy01/Just-Dashboard/backend/internal/audit"
	"github.com/Wayy01/Just-Dashboard/backend/internal/auth"
	"github.com/Wayy01/Just-Dashboard/backend/internal/config"
	"github.com/Wayy01/Just-Dashboard/backend/internal/store"
	"github.com/go-chi/chi/v5"
)

// These are properties of the router rather than of any one handler, which is
// the point: invariants 1 and 4 are about what every route has in front of it,
// and reading handlers one at a time is how a route ends up mounted outside
// the group it was supposed to be in. POST /proxy/validate sat outside the
// capability group for exactly that reason.

func testServer(t *testing.T) *Server {
	t.Helper()
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
		Addr:         "127.0.0.1:8080",
		DataDir:      t.TempDir(),
		AllowedCIDRs: []*net.IPNet{loopback},
		Require2FA:   true,
		SessionTTL:   time.Hour,
		IdleTTL:      time.Minute,
		FileRoots:    []string{t.TempDir()},
		LogRoots:     []string{t.TempDir()},
		// History recording on, so the metrics routes behave as they do in a
		// real install rather than reporting themselves disabled.
		MetricsInterval:  15 * time.Second,
		MetricsRetention: 24 * time.Hour,
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := auth.NewService(st, sealer, cfg.SessionTTL, cfg.IdleTTL, cfg.Require2FA)
	s := New(cfg, log, st, svc, sealer, audit.New(st, log), nil)
	t.Cleanup(s.Shutdown)
	return s
}

var routeParam = regexp.MustCompile(`\{[^}]*\}`)

type route struct{ method, pattern, path string }

func apiRoutes(t *testing.T, h http.Handler) []route {
	t.Helper()
	r, ok := h.(chi.Routes)
	if !ok {
		t.Fatal("Routes() no longer returns something chi can walk")
	}
	var out []route
	err := chi.Walk(r, func(method, pattern string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if !strings.HasPrefix(pattern, "/api/v1/") {
			return nil
		}
		// A concrete value for every {param}; the request never gets far
		// enough for its shape to matter.
		out = append(out, route{method, pattern, routeParam.ReplaceAllString(pattern, "1")})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) < 100 {
		t.Fatalf("only %d API routes walked; the walk is not seeing the real router", len(out))
	}
	return out
}

// Invariant 1: the allowlist runs before authentication, so an off-network
// caller cannot reach the login handler — or anything else.
func TestEveryAPIRouteIsBehindTheNetworkAllowlist(t *testing.T) {
	s := testServer(t)
	h := s.Routes()
	for _, rt := range apiRoutes(t, h) {
		req := httptest.NewRequest(rt.method, rt.path, nil)
		req.RemoteAddr = "203.0.113.5:9999"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "network_denied") {
			t.Errorf("%s %s: reachable from outside the allowlist (%d %s)",
				rt.method, rt.pattern, w.Code, strings.TrimSpace(w.Body.String()))
		}
	}
}

// Nothing under /api/v1 answers an unauthenticated caller, even one on the
// right network. The deploy webhook authenticates by HMAC rather than by
// session, and answers 401 to an unsigned request like everything else.
func TestNoAPIRouteAnswersWithoutAuthentication(t *testing.T) {
	s := testServer(t)
	h := s.Routes()
	for _, rt := range apiRoutes(t, h) {
		req := httptest.NewRequest(rt.method, rt.path, nil)
		req.RemoteAddr = "127.0.0.1:9999"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code < 400 {
			t.Errorf("%s %s: answered %d without a session", rt.method, rt.pattern, w.Code)
		}
	}
}

// /healthz is the one deliberate exception: unauthenticated, outside the
// allowlist, and revealing nothing.
func TestHealthzIsReachable(t *testing.T) {
	s := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.RemoteAddr = "203.0.113.5:9999"
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("healthz returned %d", w.Code)
	}
}

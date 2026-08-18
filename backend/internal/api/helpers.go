package api

import (
	"context"
	"net/http"
	"time"

	"github.com/Wayy01/vps-dashboard/backend/internal/audit"
	"github.com/Wayy01/vps-dashboard/backend/internal/httpx"
)

// contextWithCancel detaches from the request context's cancellation only in
// the sense of adding our own cancel; the request context still ends the
// stream when the client disconnects.
func contextWithCancel(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithCancel(r.Context())
}

func timeoutCtx(r *http.Request, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), d)
}

func clampDuration(d, lo, hi time.Duration) time.Duration {
	if d < lo {
		return lo
	}
	if d > hi {
		return hi
	}
	return d
}

// recordAudit writes an entry immediately rather than through the mutation
// middleware. WebSocket endpoints are GET requests and long-lived, so the
// interesting event is "a terminal was opened", recorded the moment it
// happens — not a status code written when the socket eventually closes.
func (s *Server) recordAudit(r *http.Request, action, target string, detail any) {
	p := httpx.MustPrincipal(r)
	s.Audit.Record(r.Context(), audit.Entry{
		UserID:   p.UserID(),
		Username: p.Username(),
		Role:     string(p.Role),
		IP:       httpx.ClientIP(r),
		Actor:    p.Kind,
		Action:   action,
		Target:   target,
		Method:   r.Method,
		Path:     r.URL.Path,
		Status:   http.StatusOK,
		Success:  true,
		Detail:   audit.Detail(detail),
	})
}

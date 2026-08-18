package httpx

import (
	"context"
	"net/http"
	"regexp"
	"strings"

	"github.com/Wayy01/vps-dashboard/backend/internal/audit"
)

const auditCtxKey ctxKey = 200

// auditInfo is the mutable slot a handler enriches while it runs. Recording
// happens in the middleware after the response is written, so every mutating
// route is logged whether or not the handler remembered to.
type auditInfo struct {
	Action string
	Target string
	Detail string
	Skip   bool
}

// SetAudit labels the current request for the audit trail.
func SetAudit(r *http.Request, action, target string, detail any) {
	if info, ok := r.Context().Value(auditCtxKey).(*auditInfo); ok {
		info.Action = action
		info.Target = target
		if detail != nil {
			info.Detail = audit.Detail(detail)
		}
	}
}

// SkipAudit suppresses the record for a route that is mutating in HTTP terms
// but carries no security meaning (terminal keystrokes, for instance, which
// are logged once at session open rather than per byte).
func SkipAudit(r *http.Request) {
	if info, ok := r.Context().Value(auditCtxKey).(*auditInfo); ok {
		info.Skip = true
	}
}

// AuditMutations records every state-changing request. Reads are excluded:
// they are high-volume and the interesting question for an operator is who
// changed the machine, not who looked at it.
func AuditMutations(log *audit.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}
			info := &auditInfo{Action: defaultAction(r)}
			ctx := context.WithValue(r.Context(), auditCtxKey, info)
			sw := &statusWriter{ResponseWriter: w}
			r = r.WithContext(ctx)
			next.ServeHTTP(sw, r)

			if info.Skip {
				return
			}
			p := MustPrincipal(r)
			detail := info.Detail
			if sw.Status() >= 400 && p.FailureReason != "" {
				detail = strings.TrimSpace(detail + " " + p.FailureReason)
			}
			log.Record(r.Context(), audit.Entry{
				UserID:   p.UserID(),
				Username: p.Username(),
				Role:     string(p.Role),
				IP:       ClientIP(r),
				Actor:    principalKind(p),
				Action:   info.Action,
				Target:   info.Target,
				Method:   r.Method,
				Path:     r.URL.Path,
				Status:   sw.Status(),
				Success:  sw.Status() < 400,
				Detail:   detail,
			})
		})
	}
}

func principalKind(p *Principal) string {
	if p == nil || p.Kind == "" {
		return "anonymous"
	}
	return p.Kind
}

// apiVersionSegment matches the leading "v1/" of a trimmed API path.
var apiVersionSegment = regexp.MustCompile(`^v[0-9]+/`)

// defaultAction derives a stable label from the path so an un-annotated route
// still produces a meaningful audit entry.
//
// The version segment is stripped along with the prefix: an audit trail should
// read "docker.container.restart" for the life of the action, not gain a "v1."
// today and a "v2." the day the API is revised.
func defaultAction(r *http.Request) string {
	path := strings.TrimPrefix(r.URL.Path, "/api/")
	path = apiVersionSegment.ReplaceAllString(path, "")
	parts := strings.Split(path, "/")
	keep := make([]string, 0, 3)
	for _, p := range parts {
		if p == "" || looksLikeID(p) {
			continue
		}
		keep = append(keep, p)
		if len(keep) == 3 {
			break
		}
	}
	return strings.ToLower(r.Method) + ":" + strings.Join(keep, ".")
}

func looksLikeID(s string) bool {
	if len(s) >= 12 {
		return true
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(s) > 0
}

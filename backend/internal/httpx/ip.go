package httpx

import (
	"context"
	"net"
	"net/http"
)

const ipCtxKey ctxKey = 100

func withIP(ctx context.Context, ip string) context.Context {
	return context.WithValue(ctx, ipCtxKey, ip)
}

// ClientIP returns the address resolved by RealIP, falling back to the raw
// peer address when the middleware is not mounted.
func ClientIP(r *http.Request) string {
	if v, ok := r.Context().Value(ipCtxKey).(string); ok && v != "" {
		return v
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

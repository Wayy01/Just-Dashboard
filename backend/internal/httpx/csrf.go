package httpx

import "net/http"

const (
	CSRFHeader = "X-JD-CSRF"
	csrfValue  = "1"
)

// RequireCSRF makes browser mutations non-simple CORS requests. A hostile
// same-site sibling can receive SameSite cookies, but it cannot add this header
// without a preflight that the dashboard never authorises.
func RequireCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if csrfSafeMethod(r.Method) {
			next.ServeHTTP(w, r)
			return
		}
		if p, ok := PrincipalFrom(r.Context()); ok && p.Kind != "session" {
			// Bearer-token and mutually authenticated hub requests do not rely
			// on ambient browser credentials, so CSRF does not apply to them.
			next.ServeHTTP(w, r)
			return
		}
		if r.Header.Get(CSRFHeader) != csrfValue {
			WriteError(w, r, Err(http.StatusForbidden, "csrf_required",
				"browser state changes require the CSRF request header"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func csrfSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return true
	default:
		return false
	}
}

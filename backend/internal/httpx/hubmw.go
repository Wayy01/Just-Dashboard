package httpx

import (
	"net/http"

	"github.com/Wayy01/Just-Dashboard/backend/internal/agent"
	"github.com/Wayy01/Just-Dashboard/backend/internal/auth"
)

// KindHub marks a request that arrived from the enrolled hub rather than from
// a person. It is a third principal kind alongside "session" and "token", and
// RequireSession already refuses it everywhere a human is mandatory.
const KindHub = "hub"

// HubOnly authenticates a request by its TLS client certificate and nothing
// else. There is no cookie, no bearer token and no password involved: the
// caller either presents the certificate this agent was enrolled against, or
// it does not get in.
//
// The hub is trusted to have already decided which of its own users may make
// this call, which is why the principal carries admin capabilities. That trust
// is the reason enrolment pins one certificate rather than accepting a CA.
func HubOnly(id *agent.Identity) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
				WriteError(w, r, Err(http.StatusUnauthorized, "hub_certificate_required",
					"this agent only accepts requests from its enrolled hub"))
				return
			}
			if !id.Enrolled() {
				WriteError(w, r, Err(http.StatusServiceUnavailable, "not_enrolled",
					"this agent has not been enrolled with a hub yet"))
				return
			}
			if !id.TrustsCert(r.TLS.PeerCertificates[0].Raw) {
				WriteError(w, r, Err(http.StatusForbidden, "hub_certificate_unknown",
					"the presented certificate is not the hub this agent is enrolled with"))
				return
			}
			p := &Principal{
				Role:      auth.RoleAdmin,
				Kind:      KindHub,
				IP:        ClientIP(r),
				UserAgent: r.UserAgent(),
			}
			next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), p)))
		})
	}
}

package httpx

import (
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func cidrs(t *testing.T, specs ...string) []*net.IPNet {
	t.Helper()
	out := make([]*net.IPNet, 0, len(specs))
	for _, spec := range specs {
		_, n, err := net.ParseCIDR(spec)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, n)
	}
	return out
}

func quiet() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// chain is the real order: RealIP resolves the address the allowlist then
// judges. Testing them apart would miss the only interesting question, which
// is whether a header can move a client from one side of the allowlist to the
// other.
func chain(trusted, allowed []*net.IPNet) http.Handler {
	final := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, ClientIP(r))
	})
	return RealIP(trusted)(AllowlistCIDRs(allowed, quiet())(final))
}

func do(h http.Handler, remote string, headers map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/system/host", nil)
	r.RemoteAddr = remote
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// This is the outermost control in the whole system: it runs before
// authentication, so an off-network attacker cannot reach the login handler at
// all. Everything below is a way of getting past it with a header.
func TestAllowlistCannotBeSpoofedByAnUntrustedPeer(t *testing.T) {
	h := chain(cidrs(t, "127.0.0.1/32"), cidrs(t, "10.0.0.0/8"))

	// The peer is not a trusted proxy, so its X-Forwarded-For is ignored and
	// the peer address itself is judged — and refused.
	res := do(h, "203.0.113.5:1234", map[string]string{"X-Forwarded-For": "10.0.0.9"})
	if res.Code != http.StatusForbidden {
		t.Fatalf("a spoofed X-Forwarded-For got past the allowlist: %d", res.Code)
	}
	res = do(h, "203.0.113.5:1234", map[string]string{"X-Real-IP": "10.0.0.9"})
	if res.Code != http.StatusForbidden {
		t.Fatalf("a spoofed X-Real-IP got past the allowlist: %d", res.Code)
	}
}

func TestAllowlistTrustsForwardedForFromAProxyItTrusts(t *testing.T) {
	h := chain(cidrs(t, "127.0.0.1/32"), cidrs(t, "10.0.0.0/8"))

	res := do(h, "127.0.0.1:5555", map[string]string{"X-Forwarded-For": "10.0.0.9"})
	if res.Code != http.StatusOK {
		t.Fatalf("the trusted proxy's report should be honoured: %d", res.Code)
	}
	if got := res.Body.String(); got != "10.0.0.9" {
		t.Fatalf("ClientIP = %q, want the forwarded address", got)
	}

	// A client behind the trusted proxy prepending its own entries must not
	// move itself into the allowlist: the right-most entry is the only one the
	// proxy actually observed.
	res = do(h, "127.0.0.1:5555", map[string]string{"X-Forwarded-For": "10.0.0.9, 203.0.113.5"})
	if res.Code != http.StatusForbidden {
		t.Fatalf("the left-most, attacker-controlled entry was trusted: %d", res.Code)
	}
}

func TestAllowlistHandlesIPv6AndMalformedAddresses(t *testing.T) {
	h := chain(cidrs(t, "::1/128"), cidrs(t, "::1/128", "fd00::/8"))

	if res := do(h, "[::1]:4000", nil); res.Code != http.StatusOK {
		t.Fatalf("loopback over IPv6 should be permitted: %d", res.Code)
	}
	if res := do(h, "[fd00::5]:4000", nil); res.Code != http.StatusOK {
		t.Fatalf("an IPv6 CIDR in the allowlist should match: %d", res.Code)
	}
	if res := do(h, "[2001:db8::1]:4000", nil); res.Code != http.StatusForbidden {
		t.Fatalf("an IPv6 address outside the allowlist must be refused: %d", res.Code)
	}
	// An unparseable header from a trusted proxy falls back to the peer, which
	// is that proxy — not to "allow anything".
	res := do(h, "[::1]:4000", map[string]string{"X-Forwarded-For": "not-an-address"})
	if res.Code != http.StatusOK || res.Body.String() != "::1" {
		t.Fatalf("expected a fallback to the peer address, got %d %q", res.Code, res.Body.String())
	}
	if res := do(h, "garbage", nil); res.Code != http.StatusForbidden {
		t.Fatalf("an unparseable peer address must fail closed: %d", res.Code)
	}
}

func TestAllowlistRefusesWhenNothingIsAllowed(t *testing.T) {
	h := chain(nil, nil)
	if res := do(h, "127.0.0.1:1", nil); res.Code != http.StatusForbidden {
		t.Fatalf("an empty allowlist must permit nothing: %d", res.Code)
	}
}

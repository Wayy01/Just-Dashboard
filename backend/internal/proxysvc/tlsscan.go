package proxysvc

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// What a domain is actually serving, graded.
//
// The certificate list on this page reads files from disk, which answers a
// question nobody has: what matters is what the internet gets when it asks.
// Those differ constantly — a certificate renewed and never reloaded, a proxy
// still offering TLS 1.0 because the config was copied from a 2015 blog post,
// a redirect to HTTPS that quietly stopped working. SSL Labs answers this and
// takes two minutes and a public hostname; every panel in this class leaves
// you to go there.
//
// The grade is deliberately coarse and its reasoning is attached to every
// finding, because a letter with no working is a number to optimise rather
// than a thing to fix.

type TLSScan struct {
	Domain    string    `json:"domain"`
	Port      int       `json:"port"`
	CheckedAt time.Time `json:"checkedAt"`
	Reachable bool      `json:"reachable"`
	Error     string    `json:"error,omitempty"`

	// Grade is A+ down to F, and Summary is the one sentence behind it.
	Grade   string `json:"grade"`
	Summary string `json:"summary"`

	Negotiated  string `json:"negotiated,omitempty"`
	CipherSuite string `json:"cipherSuite,omitempty"`
	// Protocols reports each version the server was asked for. "unknown" is a
	// real answer: this client cannot always ask for the oldest ones, and
	// saying so beats reporting a version as absent because we could not test.
	Protocols []ProtocolResult `json:"protocols"`

	Certificate *Certificate `json:"certificate,omitempty"`
	Chain       []ChainLink  `json:"chain"`
	// ChainComplete reports whether the server sent its intermediates. A
	// missing intermediate works in every desktop browser (they cache them)
	// and fails on exactly the clients nobody tests with.
	ChainComplete bool   `json:"chainComplete"`
	Trusted       bool   `json:"trusted"`
	TrustError    string `json:"trustError,omitempty"`
	NameMatches   bool   `json:"nameMatches"`

	KeyType      string `json:"keyType,omitempty"`
	KeyBits      int    `json:"keyBits,omitempty"`
	SignatureAlg string `json:"signatureAlgorithm,omitempty"`
	Fingerprint  string `json:"fingerprint,omitempty"`
	Serial       string `json:"serial,omitempty"`
	OCSPStapled  bool   `json:"ocspStapled"`

	HTTP     *HTTPScan     `json:"http,omitempty"`
	Findings []ScanFinding `json:"findings"`
}

type ProtocolResult struct {
	Name string `json:"name"`
	// Status is "offered", "refused" or "unknown".
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// ChainLink is one certificate as the server presented it.
type ChainLink struct {
	Subject    string    `json:"subject"`
	Issuer     string    `json:"issuer"`
	NotAfter   time.Time `json:"notAfter"`
	IsCA       bool      `json:"isCa"`
	KeyType    string    `json:"keyType,omitempty"`
	KeyBits    int       `json:"keyBits,omitempty"`
	SelfIssued bool      `json:"selfIssued"`
}

// HTTPScan is what the site says about itself over HTTP.
type HTTPScan struct {
	StatusCode int    `json:"statusCode"`
	Server     string `json:"server,omitempty"`
	// PlainRedirects reports whether http:// sends the visitor to https://.
	// A site with a perfect certificate that still answers on port 80 is one
	// bookmark away from being read in plain text.
	PlainRedirects bool          `json:"plainRedirects"`
	PlainStatus    int           `json:"plainStatus,omitempty"`
	PlainLocation  string        `json:"plainLocation,omitempty"`
	PlainError     string        `json:"plainError,omitempty"`
	HSTS           *HSTS         `json:"hsts,omitempty"`
	Headers        []HeaderCheck `json:"headers"`
}

// HSTS is the parsed Strict-Transport-Security header.
type HSTS struct {
	MaxAge            int    `json:"maxAge"`
	IncludeSubDomains bool   `json:"includeSubDomains"`
	Preload           bool   `json:"preload"`
	Raw               string `json:"raw"`
}

// HeaderCheck is one security header, present or not, with what it is for.
type HeaderCheck struct {
	Name    string `json:"name"`
	Value   string `json:"value,omitempty"`
	Present bool   `json:"present"`
	// Level is how much its absence matters: "important" or "optional".
	Level  string `json:"level"`
	Detail string `json:"detail"`
}

// ScanFinding mirrors the shape used by the health and Docker verdicts: what
// was measured, what it means, what to do.
type ScanFinding struct {
	ID     string `json:"id"`
	Level  string `json:"level"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
	Advice string `json:"advice,omitempty"`
}

// hstsStrongMaxAge is six months, the threshold the preload list requires and
// the point at which HSTS is doing the job it exists for.
const hstsStrongMaxAge = 15552000

// ScanTLS runs the whole examination: a handshake, a version probe, the chain,
// and an HTTP request for the headers.
func ScanTLS(ctx context.Context, domain string, port int) *TLSScan {
	if port == 0 {
		port = 443
	}
	scan := &TLSScan{
		Domain: domain, Port: port, CheckedAt: time.Now().UTC(),
		Protocols: []ProtocolResult{}, Chain: []ChainLink{}, Findings: []ScanFinding{},
	}
	addr := net.JoinHostPort(domain, strconv.Itoa(port))

	conn, err := dialTLS(ctx, addr, domain, 0, 0)
	if err != nil {
		scan.Error = err.Error()
		scan.Grade = "F"
		scan.Summary = "Nothing answered a TLS handshake on " + addr + "."
		scan.Findings = append(scan.Findings, ScanFinding{
			ID: "tls.unreachable", Level: "critical", Title: "No TLS on this address",
			Detail: err.Error(),
			Advice: "Check the domain resolves to this server and that the proxy is listening on " + strconv.Itoa(port) + ".",
		})
		return scan
	}
	state := conn.ConnectionState()
	conn.Close()

	scan.Reachable = true
	scan.Negotiated = tls.VersionName(state.Version)
	scan.CipherSuite = tls.CipherSuiteName(state.CipherSuite)
	scan.OCSPStapled = len(state.OCSPResponse) > 0
	describeChain(scan, state.PeerCertificates, domain)

	scan.Protocols = probeProtocols(ctx, addr, domain)
	scan.HTTP = scanHTTP(ctx, domain, port)

	grade(scan)
	return scan
}

func dialTLS(ctx context.Context, addr, serverName string, minVer, maxVer uint16) (*tls.Conn, error) {
	dialer := &tls.Dialer{
		NetDialer: &net.Dialer{Timeout: 8 * time.Second},
		Config: &tls.Config{
			ServerName: serverName,
			// The certificate is being examined, not trusted. A failed
			// verification is the finding, not a reason to stop.
			InsecureSkipVerify: true,
			MinVersion:         minVer,
			MaxVersion:         maxVer,
		},
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	tlsConn, ok := conn.(*tls.Conn)
	if !ok {
		conn.Close()
		return nil, fmt.Errorf("unexpected connection type")
	}
	return tlsConn, nil
}

// probedVersions are asked for one at a time, pinned to exactly one version so
// the server's answer is unambiguous.
var probedVersions = []struct {
	name    string
	version uint16
}{
	{"TLS 1.0", tls.VersionTLS10},
	{"TLS 1.1", tls.VersionTLS11},
	{"TLS 1.2", tls.VersionTLS12},
	{"TLS 1.3", tls.VersionTLS13},
}

// probeProtocols asks for each version on a connection of its own.
//
// Concurrently, because they are independent and a host that black-holes
// refused versions would otherwise cost one dial timeout each — four of them
// in series is most of the request's budget spent proving nothing.
func probeProtocols(ctx context.Context, addr, serverName string) []ProtocolResult {
	out := make([]ProtocolResult, len(probedVersions))
	var wg sync.WaitGroup
	for i, v := range probedVersions {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn, err := dialTLS(ctx, addr, serverName, v.version, v.version)
			if err == nil {
				conn.Close()
				out[i] = ProtocolResult{Name: v.name, Status: "offered"}
				return
			}
			// Distinguish "the server said no" from "this client would not
			// ask". Reporting the second as absent would be a false
			// reassurance about exactly the versions that matter most.
			if isLocalVersionRefusal(err) {
				out[i] = ProtocolResult{
					Name: v.name, Status: "unknown",
					Detail: "This dashboard's TLS library will not negotiate it, so the server was never asked.",
				}
				return
			}
			out[i] = ProtocolResult{Name: v.name, Status: "refused"}
		}()
	}
	wg.Wait()
	return out
}

func isLocalVersionRefusal(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no supported versions") ||
		strings.Contains(msg, "protocol version not supported") ||
		strings.Contains(msg, "unsupported protocol version")
}

func describeChain(scan *TLSScan, chain []*x509.Certificate, domain string) {
	if len(chain) == 0 {
		return
	}
	leaf := chain[0]
	scan.Certificate = summarise(leaf, domain, "")
	scan.Certificate.Source = "live"
	scan.KeyType, scan.KeyBits = keyInfo(leaf)
	scan.SignatureAlg = leaf.SignatureAlgorithm.String()
	sum := sha256.Sum256(leaf.Raw)
	scan.Fingerprint = colonHex(sum[:])
	scan.Serial = leaf.SerialNumber.String()

	for _, c := range chain {
		keyType, keyBits := keyInfo(c)
		scan.Chain = append(scan.Chain, ChainLink{
			Subject:  nameOf(c.Subject.CommonName, c.Subject.String()),
			Issuer:   nameOf(c.Issuer.CommonName, c.Issuer.String()),
			NotAfter: c.NotAfter.UTC(), IsCA: c.IsCA,
			KeyType: keyType, KeyBits: keyBits,
			SelfIssued: c.Issuer.String() == c.Subject.String(),
		})
	}
	// A chain of one is only complete if that one is self-signed; otherwise
	// the intermediate is missing and the server is relying on the client
	// having seen it before.
	scan.ChainComplete = len(chain) > 1 || scan.Chain[0].SelfIssued

	if err := leaf.VerifyHostname(domain); err == nil {
		scan.NameMatches = true
	}
	roots, _ := x509.SystemCertPool()
	if _, err := leaf.Verify(x509.VerifyOptions{
		DNSName: domain, Roots: roots, Intermediates: intermediates(chain),
	}); err != nil {
		scan.TrustError = err.Error()
	} else {
		scan.Trusted = true
	}
}

func nameOf(common, full string) string {
	if common != "" {
		return common
	}
	return full
}

func keyInfo(c *x509.Certificate) (string, int) {
	switch pub := c.PublicKey.(type) {
	case *rsa.PublicKey:
		return "RSA", pub.N.BitLen()
	case *ecdsa.PublicKey:
		return "ECDSA", pub.Curve.Params().BitSize
	case ed25519.PublicKey:
		return "Ed25519", 256
	}
	return c.PublicKeyAlgorithm.String(), 0
}

func colonHex(b []byte) string {
	s := hex.EncodeToString(b)
	var out strings.Builder
	for i := 0; i < len(s); i += 2 {
		if i > 0 {
			out.WriteByte(':')
		}
		out.WriteString(strings.ToUpper(s[i : i+2]))
	}
	return out.String()
}

// securityHeaders is the set worth reporting, with why. Kept short: a report
// listing twenty headers trains people to ignore the report.
var securityHeaders = []struct {
	name   string
	level  string
	detail string
}{
	{"Content-Security-Policy", "optional",
		"Restricts where scripts and styles may come from. The strongest defence against a script injected into your pages, and the fiddliest to get right."},
	{"X-Content-Type-Options", "important",
		"With nosniff, the browser stops guessing at content types — which is how an uploaded file becomes executable script."},
	{"X-Frame-Options", "important",
		"Stops another site embedding yours in a frame and collecting the clicks."},
	{"Referrer-Policy", "optional",
		"Controls how much of your URLs is leaked to the sites you link to."},
	{"Permissions-Policy", "optional",
		"Turns off browser features the page does not use: camera, microphone, geolocation."},
}

func scanHTTP(ctx context.Context, domain string, port int) *HTTPScan {
	out := &HTTPScan{Headers: []HeaderCheck{}}
	client := &http.Client{
		Timeout: 10 * time.Second,
		// Redirects are the subject of the test, not something to follow: a
		// client that follows them reports the headers of wherever it ended
		// up, which may be an entirely different host.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Transport: &http.Transport{
			TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
			DisableKeepAlives:   true,
			TLSHandshakeTimeout: 8 * time.Second,
		},
	}

	url := "https://" + net.JoinHostPort(domain, strconv.Itoa(port)) + "/"
	if req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil); err == nil {
		req.Header.Set("User-Agent", "Just-Dashboard TLS check")
		if resp, err := client.Do(req); err == nil {
			defer resp.Body.Close()
			out.StatusCode = resp.StatusCode
			out.Server = resp.Header.Get("Server")
			out.HSTS = parseHSTS(resp.Header.Get("Strict-Transport-Security"))
			for _, h := range securityHeaders {
				value := resp.Header.Get(h.name)
				out.Headers = append(out.Headers, HeaderCheck{
					Name: h.name, Value: value, Present: value != "",
					Level: h.level, Detail: h.detail,
				})
			}
		}
	}

	// The plain-HTTP half. Port 80 whatever the TLS port is: a redirect on a
	// non-standard port tells nobody anything, because no browser goes there.
	plain := "http://" + domain + "/"
	if req, err := http.NewRequestWithContext(ctx, http.MethodGet, plain, nil); err == nil {
		req.Header.Set("User-Agent", "Just-Dashboard TLS check")
		resp, err := client.Do(req)
		if err != nil {
			out.PlainError = err.Error()
		} else {
			defer resp.Body.Close()
			out.PlainStatus = resp.StatusCode
			out.PlainLocation = resp.Header.Get("Location")
			out.PlainRedirects = resp.StatusCode >= 300 && resp.StatusCode < 400 &&
				strings.HasPrefix(strings.ToLower(out.PlainLocation), "https://")
		}
	}
	return out
}

// parseHSTS reads the header's directives. max-age is the only one that does
// anything on its own, and a max-age of zero is an instruction to forget the
// policy — which is not the same as not sending the header, and is worth
// reporting as itself.
func parseHSTS(raw string) *HSTS {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	h := &HSTS{Raw: raw, MaxAge: -1}
	for _, part := range strings.Split(raw, ";") {
		part = strings.TrimSpace(strings.ToLower(part))
		switch {
		case strings.HasPrefix(part, "max-age"):
			_, value, _ := strings.Cut(part, "=")
			if n, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
				h.MaxAge = n
			}
		case part == "includesubdomains":
			h.IncludeSubDomains = true
		case part == "preload":
			h.Preload = true
		}
	}
	return h
}

// grade turns the scan into a letter and the findings that justify it.
//
// Kept as a pure function of the scan so the rules can be tested without a
// network, and so that "why did this get a B" has one place to look.
func grade(scan *TLSScan) {
	findings := scan.Findings
	worst := gradeA

	demote := func(to int, f ScanFinding) {
		findings = append(findings, f)
		if to > worst {
			worst = to
		}
	}

	if !scan.Reachable {
		scan.Grade, scan.Summary = "F", "Nothing answered a TLS handshake."
		scan.Findings = findings
		return
	}
	cert := scan.Certificate
	switch {
	case cert == nil:
		demote(gradeF, ScanFinding{ID: "tls.no-cert", Level: "critical",
			Title: "No certificate was presented", Detail: "The handshake completed without one."})
	case cert.Expired:
		demote(gradeF, ScanFinding{ID: "tls.expired", Level: "critical",
			Title:  "The certificate has expired",
			Detail: fmt.Sprintf("It expired on %s.", cert.NotAfter.Format("2 January 2006")),
			Advice: "Every browser is refusing this site now. Renew it, then find out why the renewal did not run on its own."})
	case cert.DaysLeft <= 14:
		demote(gradeB, ScanFinding{ID: "tls.expiring", Level: "warning",
			Title:  fmt.Sprintf("The certificate expires in %d days", cert.DaysLeft),
			Detail: "Automatic renewal starts at 30 days left, so this one is not renewing.",
			Advice: "Renew it by hand and check the renewal timer."})
	}
	if !scan.NameMatches && cert != nil {
		demote(gradeF, ScanFinding{ID: "tls.name-mismatch", Level: "critical",
			Title:  "The certificate is for a different name",
			Detail: "It covers " + strings.Join(cert.Domains, ", ") + ".",
			Advice: "Reissue it including " + scan.Domain + ", or point that name at the host that has a certificate for it."})
	}
	if !scan.Trusted {
		level, id := gradeF, "tls.untrusted"
		advice := "Browsers will show a warning page. Use a certificate from a public authority — certbot issues one free."
		if cert != nil && cert.SelfSigned {
			advice = "A self-signed certificate is fine for something only you reach, and a full-page warning for anybody else."
		}
		demote(level, ScanFinding{ID: id, Level: "critical",
			Title: "The chain is not trusted", Detail: scan.TrustError, Advice: advice})
	}
	if !scan.ChainComplete {
		demote(gradeB, ScanFinding{ID: "tls.incomplete-chain", Level: "warning",
			Title:  "The server did not send its intermediate certificate",
			Detail: "Only the leaf was presented.",
			Advice: "Point the proxy at fullchain.pem rather than cert.pem. Desktop browsers paper over this from cache; phones, curl and payment gateways do not."})
	}

	for _, p := range scan.Protocols {
		if p.Status != "offered" {
			continue
		}
		switch p.Name {
		case "TLS 1.0", "TLS 1.1":
			demote(gradeC, ScanFinding{ID: "tls.old-protocol." + p.Name, Level: "warning",
				Title:  p.Name + " is still offered",
				Detail: "Deprecated since 2021 and disabled in every current browser.",
				Advice: "Set ssl_protocols to TLSv1.2 TLSv1.3. Nothing that can reach this site today needs the older ones."})
		}
	}
	if protocolStatus(scan.Protocols, "TLS 1.3") == "refused" {
		demote(gradeB, ScanFinding{ID: "tls.no-13", Level: "notice",
			Title:  "TLS 1.3 is not offered",
			Detail: "The server negotiated " + scan.Negotiated + " at best.",
			Advice: "Add TLSv1.3 to ssl_protocols. It is faster and removes a whole category of downgrade problem."})
	}
	if scan.KeyType == "RSA" && scan.KeyBits > 0 && scan.KeyBits < 2048 {
		demote(gradeF, ScanFinding{ID: "tls.weak-key", Level: "critical",
			Title:  fmt.Sprintf("The key is only %d bits", scan.KeyBits),
			Detail: "Below the 2048-bit minimum every authority has enforced for a decade.",
			Advice: "Reissue with a 2048-bit RSA key or, better, an ECDSA P-256 one."})
	}
	if !scan.OCSPStapled {
		findings = append(findings, ScanFinding{ID: "tls.no-ocsp", Level: "notice",
			Title:  "No OCSP response is stapled",
			Detail: "The server is not attaching proof that its certificate has not been revoked.",
			Advice: "Optional, and increasingly so — Let's Encrypt has stopped publishing OCSP entirely. Worth turning on only if your authority still supports it."})
	}

	if http := scan.HTTP; http != nil {
		if http.HSTS == nil {
			// A notice rather than a demotion: HSTS is what separates A from
			// A+, and letterFor reads the header itself for that.
			findings = append(findings, ScanFinding{ID: "tls.no-hsts", Level: "notice",
				Title:  "HSTS is not set",
				Detail: "No Strict-Transport-Security header.",
				Advice: "Add it with a max-age of six months. Without it, the first request a visitor makes is over plain HTTP and can be intercepted before the redirect."})
		} else if http.HSTS.MaxAge < hstsStrongMaxAge {
			findings = append(findings, ScanFinding{ID: "tls.weak-hsts", Level: "notice",
				Title:  "HSTS is set but short",
				Detail: fmt.Sprintf("max-age is %d seconds.", http.HSTS.MaxAge),
				Advice: "Six months (15552000) is the value browsers and the preload list expect."})
		}
		if !http.PlainRedirects && http.PlainError == "" {
			demote(gradeB, ScanFinding{ID: "tls.no-redirect", Level: "warning",
				Title:  "Plain HTTP does not redirect to HTTPS",
				Detail: fmt.Sprintf("http://%s answered %d.", scan.Domain, http.PlainStatus),
				Advice: "Redirect port 80 to HTTPS permanently. A certificate protects nobody who arrives on the unencrypted port."})
		}
		for _, h := range http.Headers {
			if h.Present || h.Level != "important" {
				continue
			}
			findings = append(findings, ScanFinding{ID: "http.header." + h.Name, Level: "notice",
				Title: h.Name + " is not set", Detail: h.Detail})
		}
	}

	SortFindings(findings)
	scan.Findings = findings
	scan.Grade, scan.Summary = letterFor(worst, scan)
}

// Grade levels, worst-highest so a demotion is a max().
const (
	gradeA = iota
	gradeB
	gradeC
	gradeF
)

func letterFor(worst int, scan *TLSScan) (string, string) {
	switch worst {
	case gradeF:
		return "F", "Browsers will refuse or warn about this site."
	case gradeC:
		return "C", "Valid, but still offering protocol versions that were retired years ago."
	case gradeB:
		return "B", "Working correctly, with something worth fixing."
	}
	// A+ is A with the two things that make a correct configuration a
	// complete one: a long HSTS policy and no way in over plain HTTP.
	if scan.HTTP != nil && scan.HTTP.PlainRedirects &&
		scan.HTTP.HSTS != nil && scan.HTTP.HSTS.MaxAge >= hstsStrongMaxAge {
		return "A+", "Trusted, current, and HTTP-only visitors are pushed to HTTPS and kept there."
	}
	return "A", "Trusted certificate on a current protocol."
}

func protocolStatus(list []ProtocolResult, name string) string {
	for _, p := range list {
		if p.Name == name {
			return p.Status
		}
	}
	return "unknown"
}

// SortFindings orders a scan's findings worst-first, which is how they are
// rendered and is worth doing once here rather than in the client.
func SortFindings(findings []ScanFinding) {
	rank := map[string]int{"critical": 3, "warning": 2, "notice": 1}
	sort.SliceStable(findings, func(i, j int) bool {
		return rank[findings[i].Level] > rank[findings[j].Level]
	})
}

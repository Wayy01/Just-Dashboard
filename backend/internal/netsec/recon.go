package netsec

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wayy01/Just-Dashboard/backend/internal/hostexec"
)

// The recon half of the tools page: the questions that come up while pointing
// a browser, a firewall rule or a certificate at a host, and that otherwise
// send the operator to a shell.
//
// They obey the same rules as the diagnostics next to them — read-only, a
// single validated target, argv never a shell, and behind system.admin because
// they make the server send traffic to an address the caller chose. Three of
// the four need no subprocess for the reason Lookup does not shell to dig: the
// commonest tool on the page must not be the one that is not installed.

// maxProbeOutput bounds a tool whose output is not self-limiting. ping and
// traceroute stop themselves; whois does not, and some registry and netblock
// answers run to megabytes straight into a JSON body. 256 KB is dockerx's own
// per-line cap, for the same reason.
const maxProbeOutput = 256 * 1024

// httpHeaders is the curated set the HTTP check reports: what the server is,
// what it serves, and whether it set the headers that keep a browser honest.
// A full dump is noise; these are the lines an operator actually reads.
var httpHeaders = []string{
	"Server",
	"Content-Type",
	"Strict-Transport-Security",
	"Content-Security-Policy",
	"X-Frame-Options",
	"X-Content-Type-Options",
	"Referrer-Policy",
	"X-Powered-By",
}

// HTTPCheck asks a host what it serves: the status, the redirect chain and a
// few headers. It is the "is my proxy actually up, and where does it send me"
// question, answered without leaving the page.
//
// The transport skips certificate verification on purpose — a self-signed or
// expired certificate should still yield an answer about the response, and the
// TLS tool is the one that judges the certificate. The output says so, so a
// 200 here is never mistaken for a valid chain.
func (s *Service) HTTPCheck(ctx context.Context, target string, port int) (*ProbeResult, error) {
	if !ValidTarget(target) {
		return nil, fmt.Errorf("target must be a hostname or IP address")
	}
	if port == 0 {
		port = 443
	}
	if port < 1 || port > 65535 {
		return nil, fmt.Errorf("port must be between 1 and 65535")
	}
	scheme := "https"
	if port == 80 {
		scheme = "http"
	}
	start := url.URL{Scheme: scheme, Host: net.JoinHostPort(target, strconv.Itoa(port)), Path: "/"}
	res := &ProbeResult{Tool: "http", Target: start.String()}

	client := &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig:   &tls.Config{InsecureSkipVerify: true},
			DisableKeepAlives: true,
		},
		// Follow redirects by hand below so each hop's status and destination
		// can be shown; ErrUseLastResponse hands the redirect back unfollowed.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	began := time.Now()
	var b strings.Builder
	current := start.String()
	var final *http.Response
	for hop := 0; hop < 10; hop++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, current, nil)
		if err != nil {
			res.Duration = time.Since(began).Round(time.Millisecond).String()
			res.Error = err.Error()
			return res, nil
		}
		req.Header.Set("User-Agent", "Just-Dashboard/probe")
		resp, err := client.Do(req)
		if err != nil {
			res.Duration = time.Since(began).Round(time.Millisecond).String()
			res.Error = err.Error()
			res.Output = strings.TrimSpace(b.String() + "\n" + describeDialError(err))
			return res, nil
		}
		fmt.Fprintf(&b, "%s %s\n%s\n", req.Method, current, resp.Status)
		loc := resp.Header.Get("Location")
		if isRedirect(resp.StatusCode) && loc != "" {
			next, perr := resp.Request.URL.Parse(loc)
			resp.Body.Close()
			if perr != nil {
				res.Error = perr.Error()
				break
			}
			fmt.Fprintf(&b, "  → %s\n", next.String())
			current = next.String()
			continue
		}
		final = resp
		break
	}

	res.Duration = time.Since(began).Round(time.Millisecond).String()
	if final != nil {
		final.Body.Close()
		res.OK = final.StatusCode < 400
		b.WriteString("\n")
		for _, h := range httpHeaders {
			if v := final.Header.Get(h); v != "" {
				fmt.Fprintf(&b, "%s: %s\n", h, v)
			}
		}
		if scheme == "https" {
			b.WriteString("\nThe certificate was not verified here — use the TLS tool to check it.")
		}
	}
	res.Output = strings.TrimSpace(b.String())
	return res, nil
}

func isRedirect(code int) bool {
	return code == http.StatusMovedPermanently || code == http.StatusFound ||
		code == http.StatusSeeOther || code == http.StatusTemporaryRedirect ||
		code == http.StatusPermanentRedirect
}

// TLSCert inspects the certificate a TLS port actually presents — subject,
// issuer, names, validity and whether it is trusted for this host — which is
// the recon question the proxy page answers only for the sites it manages.
//
// It dials without verification so an expired or self-signed certificate is
// reported rather than refused; the trust check is then run explicitly and its
// verdict shown, so "not trusted" is a finding and not a failure.
func (s *Service) TLSCert(ctx context.Context, target string, port int) (*ProbeResult, error) {
	if !ValidTarget(target) {
		return nil, fmt.Errorf("target must be a hostname or IP address")
	}
	if port == 0 {
		port = 443
	}
	if port < 1 || port > 65535 {
		return nil, fmt.Errorf("port must be between 1 and 65535")
	}
	addr := net.JoinHostPort(target, strconv.Itoa(port))
	res := &ProbeResult{Tool: "tls", Target: addr, Records: []string{}}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	began := time.Now()
	dialer := &tls.Dialer{Config: &tls.Config{InsecureSkipVerify: true, ServerName: target}}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	res.Duration = time.Since(began).Round(time.Millisecond).String()
	if err != nil {
		res.Error = err.Error()
		res.Output = describeDialError(err)
		return res, nil
	}
	defer conn.Close()
	state := conn.(*tls.Conn).ConnectionState()
	if len(state.PeerCertificates) == 0 {
		res.Error = "the server presented no certificate"
		return res, nil
	}
	leaf := state.PeerCertificates[0]
	res.Records = append(res.Records, leaf.DNSNames...)
	res.OK = true

	var b strings.Builder
	fmt.Fprintf(&b, "%s, %s\n\n", tlsVersionName(state.Version), tls.CipherSuiteName(state.CipherSuite))
	fmt.Fprintf(&b, "Subject:  %s\n", nameOrString(leaf.Subject.CommonName, leaf.Subject.String()))
	fmt.Fprintf(&b, "Issuer:   %s\n", nameOrString(leaf.Issuer.CommonName, leaf.Issuer.String()))
	if len(leaf.DNSNames) > 0 {
		fmt.Fprintf(&b, "Names:    %s\n", strings.Join(leaf.DNSNames, ", "))
	}
	fmt.Fprintf(&b, "Valid:    %s → %s\n", leaf.NotBefore.UTC().Format("2006-01-02"), leaf.NotAfter.UTC().Format("2006-01-02"))

	now := time.Now()
	switch {
	case now.After(leaf.NotAfter):
		fmt.Fprintf(&b, "Expired %d days ago.\n", int(now.Sub(leaf.NotAfter).Hours()/24))
	case now.Before(leaf.NotBefore):
		b.WriteString("Not valid yet.\n")
	default:
		fmt.Fprintf(&b, "Expires in %d days.\n", int(time.Until(leaf.NotAfter).Hours()/24))
	}

	// The trust check the insecure dial skipped, run on its own so its result
	// is information rather than a refusal to connect.
	roots, _ := x509.SystemCertPool()
	inter := x509.NewCertPool()
	for _, c := range state.PeerCertificates[1:] {
		inter.AddCert(c)
	}
	if _, verr := leaf.Verify(x509.VerifyOptions{DNSName: target, Roots: roots, Intermediates: inter}); verr != nil {
		fmt.Fprintf(&b, "Not trusted for %s: %v\n", target, verr)
	} else {
		fmt.Fprintf(&b, "Trusted for %s.\n", target)
	}
	res.Output = strings.TrimSpace(b.String())
	return res, nil
}

func tlsVersionName(v uint16) string {
	switch v {
	case tls.VersionTLS13:
		return "TLS 1.3"
	case tls.VersionTLS12:
		return "TLS 1.2"
	case tls.VersionTLS11:
		return "TLS 1.1"
	case tls.VersionTLS10:
		return "TLS 1.0"
	}
	return fmt.Sprintf("0x%04x", v)
}

func nameOrString(cn, full string) string {
	if cn != "" {
		return cn
	}
	return full
}

// PortScan opens a TCP connection to each of the catalogue's well-known
// service ports and reports which answer. It is the port check's neighbour:
// one asks about a port you name, this asks about the ones a single-server
// operator tends to run — and names each from the same catalogue the firewall
// form teaches from, so an open database reads with its warning attached.
//
// Only TCP ports are scanned: a connect scan cannot speak to a UDP service, so
// including DNS or WireGuard would report a working port as closed. The set is
// deliberately the catalogue and nothing wider — a full range scan is a
// different tool with a different blast radius, and the operator who wants one
// has a shell.
func (s *Service) PortScan(ctx context.Context, target string) (*ProbeResult, error) {
	if !ValidTarget(target) {
		return nil, fmt.Errorf("target must be a hostname or IP address")
	}
	res := &ProbeResult{Tool: "scan", Target: target, Records: []string{}}
	ports := scanPorts()

	began := time.Now()
	open := make([]bool, len(ports))
	sem := make(chan struct{}, 12)
	var wg sync.WaitGroup
	for i, sp := range ports {
		wg.Add(1)
		go func(i int, sp ServicePreset) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			d := &net.Dialer{Timeout: 2 * time.Second}
			conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(target, sp.Port))
			if err == nil {
				conn.Close()
				open[i] = true
			}
		}(i, sp)
	}
	wg.Wait()
	res.Duration = time.Since(began).Round(time.Millisecond).String()

	var b strings.Builder
	count := 0
	for i, sp := range ports {
		if !open[i] {
			continue
		}
		count++
		res.Records = append(res.Records, sp.Port+" "+sp.Name)
		line := fmt.Sprintf("%-6s open   %s", sp.Port, sp.Name)
		if sp.Danger != "" {
			line += "  ⚠ " + sp.Danger
		}
		b.WriteString(line + "\n")
	}
	// The scan succeeding is the scan running, not finding an open port: a host
	// with everything closed is the good answer, and reporting it as "no answer"
	// would paint the best outcome red. The count of open ports is in the output.
	res.OK = true
	if count == 0 {
		fmt.Fprintf(&b, "None of the %d common service ports answered on %s.", len(ports), target)
	} else {
		fmt.Fprintf(&b, "\n%d of %d common ports open.", count, len(ports))
	}
	res.Output = strings.TrimSpace(b.String())
	return res, nil
}

// scanPorts is the set PortScan probes: the catalogue's TCP ports, deduped.
// UDP entries are excluded because a connect scan cannot reach a UDP service,
// so scanning one would report a working port as closed — the dedupe then
// keeps 443 once rather than as both its TCP and HTTP/3 rows.
func scanPorts() []ServicePreset {
	seen := map[string]bool{}
	var ports []ServicePreset
	for _, sp := range ServiceCatalogue {
		if sp.Protocol != "tcp" || seen[sp.Port] {
			continue
		}
		seen[sp.Port] = true
		ports = append(ports, sp)
	}
	return ports
}

// Whois looks a domain or address up in the registries. It shells out because
// there is no resolver equivalent — and it fails soft when whois is not
// installed, the way traceroute does when neither traceroute nor tracepath is,
// so a missing optional tool is a sentence rather than a red toast.
func (s *Service) Whois(ctx context.Context, target string) (*ProbeResult, error) {
	if !ValidTarget(target) {
		return nil, fmt.Errorf("target must be a hostname or IP address")
	}
	res := &ProbeResult{Tool: "whois", Target: target}
	if !hostexec.AvailableOnHost("whois") {
		res.Error = "whois is not installed on this host"
		return res, nil
	}
	out, elapsed, err := runProbe(ctx, 20*time.Second, "whois", target)
	if len(out) > maxProbeOutput {
		out = out[:maxProbeOutput] + "\n… (truncated)"
	}
	res.Output, res.Duration, res.OK = out, elapsed, err == nil
	if err != nil {
		res.Error = err.Error()
	}
	return res, nil
}

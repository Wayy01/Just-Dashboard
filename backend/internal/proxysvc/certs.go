package proxysvc

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Certificate struct {
	Name       string    `json:"name"`
	Path       string    `json:"path"`
	Domains    []string  `json:"domains"`
	Issuer     string    `json:"issuer"`
	NotBefore  time.Time `json:"notBefore"`
	NotAfter   time.Time `json:"notAfter"`
	DaysLeft   int       `json:"daysLeft"`
	Expired    bool      `json:"expired"`
	Expiring   bool      `json:"expiring"`
	SelfSigned bool      `json:"selfSigned"`
	Source     string    `json:"source"`
	Error      string    `json:"error,omitempty"`
}

// expiryWarningDays matches Let's Encrypt's own renewal window: certbot
// renews at 30 days, so anything inside that window and still un-renewed is
// worth flagging.
const expiryWarningDays = 30

// ListCertificates reads certbot's live directory plus any certificate paths
// referenced by the proxy config, so a manually installed certificate is not
// invisible just because certbot does not know about it.
func (s *Service) ListCertificates(ctx context.Context) ([]Certificate, error) {
	seen := map[string]bool{}
	out := []Certificate{}

	add := func(path, source string) {
		resolved := path
		if r, err := filepath.EvalSymlinks(path); err == nil {
			resolved = r
		}
		if seen[resolved] {
			return
		}
		seen[resolved] = true
		cert, err := readCertificate(path)
		if err != nil {
			out = append(out, Certificate{
				Name: filepath.Base(filepath.Dir(path)), Path: path,
				Source: source, Error: err.Error(), Domains: []string{},
			})
			return
		}
		cert.Source = source
		out = append(out, *cert)
	}

	if entries, err := os.ReadDir("/etc/letsencrypt/live"); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			add(filepath.Join("/etc/letsencrypt/live", e.Name(), "fullchain.pem"), "certbot")
		}
	}
	for _, v := range s.nginxVHosts() {
		if v.CertPath != "" {
			add(v.CertPath, "nginx:"+v.Name)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].DaysLeft != out[j].DaysLeft {
			return out[i].DaysLeft < out[j].DaysLeft
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func readCertificate(path string) (*Certificate, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, fmt.Errorf("not a PEM certificate")
	}
	parsed, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, err
	}
	return summarise(parsed, filepath.Base(filepath.Dir(path)), path), nil
}

func summarise(c *x509.Certificate, name, path string) *Certificate {
	cert := &Certificate{
		Name: name, Path: path,
		Domains:   append([]string{}, c.DNSNames...),
		Issuer:    c.Issuer.CommonName,
		NotBefore: c.NotBefore.UTC(),
		NotAfter:  c.NotAfter.UTC(),
	}
	if len(cert.Domains) == 0 && c.Subject.CommonName != "" {
		cert.Domains = []string{c.Subject.CommonName}
	}
	if cert.Issuer == "" {
		cert.Issuer = c.Issuer.String()
	}
	cert.DaysLeft = int(time.Until(c.NotAfter).Hours() / 24)
	cert.Expired = time.Now().After(c.NotAfter)
	cert.Expiring = !cert.Expired && cert.DaysLeft <= expiryWarningDays
	cert.SelfSigned = c.Issuer.String() == c.Subject.String()
	return cert
}

// CheckDomain opens a TLS connection and reports what the domain is actually
// serving. Reading the file on disk is not enough: a certificate can be renewed
// on disk and never reloaded, and only a live handshake catches that.
func CheckDomain(ctx context.Context, domain string, port int) (*Certificate, error) {
	if port == 0 {
		port = 443
	}
	dialer := &net.Dialer{Timeout: 8 * time.Second}
	conn, err := tls.DialWithDialer(dialer, "tcp", net.JoinHostPort(domain, fmt.Sprint(port)), &tls.Config{
		ServerName: domain,
		// The certificate is being inspected, not trusted — a failed
		// verification is a finding to report, not a reason to give up.
		InsecureSkipVerify: true,
	})
	if err != nil {
		return &Certificate{Name: domain, Domains: []string{domain}, Source: "live", Error: err.Error()}, nil
	}
	defer conn.Close()

	state := conn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return nil, fmt.Errorf("%s presented no certificate", domain)
	}
	cert := summarise(state.PeerCertificates[0], domain, "")
	cert.Source = "live"
	// Verify against the system roots separately so the report can say
	// "valid but untrusted" rather than conflating the two.
	roots, _ := x509.SystemCertPool()
	if _, err := state.PeerCertificates[0].Verify(x509.VerifyOptions{
		DNSName:       domain,
		Roots:         roots,
		Intermediates: intermediates(state.PeerCertificates),
	}); err != nil {
		cert.Error = err.Error()
	}
	return cert, nil
}

func intermediates(chain []*x509.Certificate) *x509.CertPool {
	pool := x509.NewCertPool()
	for _, c := range chain[1:] {
		pool.AddCert(c)
	}
	return pool
}

// CertbotCertificates asks certbot itself, which knows about renewal
// configuration that the PEM files alone do not reveal.
func CertbotCertificates(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := execOutput(ctx, "certbot", "certificates")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

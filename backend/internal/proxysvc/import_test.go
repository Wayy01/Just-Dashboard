package proxysvc

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"
)

// The check that matters is that the key belongs to the certificate. A
// mismatched pair is accepted by every text editor and rejected by nginx at
// reload, which on a live server means finding out during an outage.

func selfSigned(t *testing.T, name string, notAfter time.Time) (string, string, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: name},
		DNSNames:     []string{name},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     notAfter,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}))
	return certPEM, keyPEM, key
}

func TestParseCertChain(t *testing.T) {
	certPEM, _, _ := selfSigned(t, "example.com", time.Now().Add(90*24*time.Hour))
	leaf, count, err := parseCertChain(certPEM)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 || leaf.Subject.CommonName != "example.com" {
		t.Fatalf("got %d certs, leaf %q", count, leaf.Subject.CommonName)
	}

	// A chain is more than one block, and the first is the leaf.
	other, _, _ := selfSigned(t, "intermediate.example", time.Now().Add(365*24*time.Hour))
	leaf, count, err = parseCertChain(certPEM + other)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 || leaf.Subject.CommonName != "example.com" {
		t.Fatalf("got %d certs, leaf %q", count, leaf.Subject.CommonName)
	}
}

func TestParseCertChainRejectsRubbish(t *testing.T) {
	for _, bad := range []string{"", "hello", "-----BEGIN CERTIFICATE-----\nnope\n-----END CERTIFICATE-----"} {
		if _, _, err := parseCertChain(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestKeyMatchesCertificate(t *testing.T) {
	certPEM, keyPEM, _ := selfSigned(t, "example.com", time.Now().Add(90*24*time.Hour))
	leaf, _, err := parseCertChain(certPEM)
	if err != nil {
		t.Fatal(err)
	}
	if err := keyMatchesCertificate(keyPEM, leaf); err != nil {
		t.Fatalf("a matching pair was rejected: %v", err)
	}

	// The whole reason this check exists.
	_, otherKey, _ := selfSigned(t, "other.example", time.Now().Add(90*24*time.Hour))
	if err := keyMatchesCertificate(otherKey, leaf); err == nil {
		t.Fatal("a mismatched key was accepted, which nginx would refuse at reload")
	}
	if err := keyMatchesCertificate("not a key", leaf); err == nil {
		t.Fatal("accepted something that is not a key")
	}
}

// Authorities hand back PKCS#1 as often as PKCS#8, and rejecting one of them
// would look like a broken key.
func TestKeyMatchesCertificateAcceptsPKCS1(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "rsa.example"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, _ := x509.ParseCertificate(der)
	pkcs1 := string(pem.EncodeToMemory(&pem.Block{
		Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key),
	}))
	if err := keyMatchesCertificate(pkcs1, leaf); err != nil {
		t.Fatalf("PKCS#1 rejected: %v", err)
	}
}

func TestNormalisePEM(t *testing.T) {
	got := normalisePEM("-----BEGIN CERTIFICATE-----\r\nabc\r\n-----END CERTIFICATE-----")
	if strings.Contains(got, "\r") {
		t.Error("carriage returns survived a paste from Windows")
	}
	if !strings.HasSuffix(got, "\n") {
		t.Error("no trailing newline, which openssl complains about")
	}
}

func TestImportCertificateRejectsABadName(t *testing.T) {
	certPEM, keyPEM, _ := selfSigned(t, "example.com", time.Now().Add(24*time.Hour))
	for _, bad := range []string{"", "../../etc/passwd", "Name With Spaces", "a/b"} {
		if _, err := ImportCertificate(bad, certPEM, keyPEM); err == nil {
			t.Errorf("accepted name %q", bad)
		}
	}
}

// A DNS challenge is the only way to get a wildcard, and saying so before the
// attempt beats relaying certbot's version of it afterwards.
func TestIssueRefusesAWildcardOverHTTP(t *testing.T) {
	s := New("/etc/nginx", "/etc/caddy/Caddyfile")
	_, err := s.Issue(t.Context(), IssueRequest{
		Domains: []string{"*.example.com"}, Email: "a@example.com", Method: "nginx",
	})
	if err == nil {
		t.Fatal("accepted a wildcard over an HTTP challenge")
	}
	if !strings.Contains(err.Error(), "DNS challenge") {
		t.Errorf("the error should say what to do instead: %v", err)
	}
}

func TestIssueDNSRequiresAKnownProvider(t *testing.T) {
	s := New("/etc/nginx", "/etc/caddy/Caddyfile")
	_, err := s.Issue(t.Context(), IssueRequest{
		Domains: []string{"example.com"}, Email: "a@example.com", Method: "dns",
	})
	if err == nil {
		t.Fatal("accepted a DNS challenge with no provider")
	}
	_, err = s.Issue(t.Context(), IssueRequest{
		Domains: []string{"example.com"}, Email: "a@example.com",
		Method: "dns", DNSProvider: "some-registrar",
	})
	if err == nil {
		t.Fatal("accepted an unknown provider")
	}
}

// Every plugin names its arguments after itself, and route53 has none —
// getting that wrong is an error about an unrecognised flag.
func TestDNSIssueArgs(t *testing.T) {
	cf, _ := DNSProviderFor("cloudflare")
	args := strings.Join(dnsIssueArgs(cf, 45), " ")
	for _, want := range []string{
		"--dns-cloudflare", "--dns-cloudflare-credentials", "--dns-cloudflare-propagation-seconds 45",
	} {
		if !strings.Contains(args, want) {
			t.Errorf("missing %q from %q", want, args)
		}
	}

	r53, _ := DNSProviderFor("route53")
	args = strings.Join(dnsIssueArgs(r53, 45), " ")
	if strings.Contains(args, "credentials") || strings.Contains(args, "propagation") {
		t.Errorf("route53 takes neither: %q", args)
	}
}

func TestWriteDNSCredentialsRejectsUnknownProviders(t *testing.T) {
	if _, err := WriteDNSCredentials("nope", "token = x"); err == nil {
		t.Error("accepted an unknown provider")
	}
	if _, err := WriteDNSCredentials("cloudflare", "   "); err == nil {
		t.Error("accepted empty credentials")
	}
}

// The list is a choice, and the entries that will actually work are the ones
// worth reading first.
func TestListDNSProvidersPutsInstalledFirst(t *testing.T) {
	s := New("/etc/nginx", "/etc/caddy/Caddyfile")
	got := s.ListDNSProviders()
	if len(got) != len(dnsProviders) {
		t.Fatalf("got %d providers", len(got))
	}
	seenUninstalled := false
	for _, p := range got {
		if p.Credentials == "" {
			t.Errorf("%s does not say what its credentials look like", p.Name)
		}
		if !p.Installed {
			seenUninstalled = true
		} else if seenUninstalled {
			t.Error("an installed provider was sorted below an uninstalled one")
		}
	}
}

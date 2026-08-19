package agent

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// hubCert stands in for the certificate a hub would present.
func hubCert(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "hub"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return der
}

func load(t *testing.T, dir string) *Identity {
	t.Helper()
	id, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestIdentityIsStableAcrossRestarts(t *testing.T) {
	dir := t.TempDir()
	first := load(t, dir)
	second := load(t, dir)

	if first.ID() != second.ID() {
		t.Fatalf("agent id changed across restart: %s vs %s", first.ID(), second.ID())
	}
	if first.Fingerprint() != second.Fingerprint() {
		t.Fatal("certificate was regenerated on restart; the hub's pin would break")
	}
}

func TestPrivateKeyIsNotWorldReadable(t *testing.T) {
	dir := t.TempDir()
	load(t, dir)

	info, err := os.Stat(filepath.Join(dir, "identity.key"))
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("private key mode is %o, want 600", mode)
	}
}

func TestEnrolmentPinsTheHub(t *testing.T) {
	id := load(t, t.TempDir())
	cert := hubCert(t)

	token, err := id.NewEnrolmentToken()
	if err != nil {
		t.Fatal(err)
	}
	if id.Enrolled() {
		t.Fatal("minting a token must not count as being enrolled")
	}
	if err := id.Enrol(token, cert); err != nil {
		t.Fatal(err)
	}
	if !id.Enrolled() {
		t.Fatal("agent should be enrolled")
	}
	if !id.TrustsCert(cert) {
		t.Fatal("the enrolled hub's certificate should be trusted")
	}
	if id.TrustsCert(hubCert(t)) {
		t.Fatal("a different certificate must not be trusted")
	}
}

func TestEnrolmentSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	id := load(t, dir)
	cert := hubCert(t)

	token, _ := id.NewEnrolmentToken()
	if err := id.Enrol(token, cert); err != nil {
		t.Fatal(err)
	}

	reloaded := load(t, dir)
	if !reloaded.Enrolled() || !reloaded.TrustsCert(cert) {
		t.Fatal("enrolment did not survive a restart")
	}
}

func TestTokenIsSingleUse(t *testing.T) {
	id := load(t, t.TempDir())
	token, _ := id.NewEnrolmentToken()

	if err := id.Enrol(token, hubCert(t)); err != nil {
		t.Fatal(err)
	}
	// The same token, presented again by anyone who saw it.
	if err := id.Enrol(token, hubCert(t)); !errors.Is(err, ErrAlreadyEnrolled) {
		t.Fatalf("replay should be refused with ErrAlreadyEnrolled, got %v", err)
	}
}

func TestEnrolledAgentRefusesToBeRePointed(t *testing.T) {
	id := load(t, t.TempDir())
	first := hubCert(t)
	token, _ := id.NewEnrolmentToken()
	if err := id.Enrol(token, first); err != nil {
		t.Fatal(err)
	}

	// An attacker who obtains a fresh token cannot get one: minting is refused
	// once enrolled, which is what stops a live agent being stolen.
	if _, err := id.NewEnrolmentToken(); !errors.Is(err, ErrAlreadyEnrolled) {
		t.Fatalf("minting after enrolment should fail, got %v", err)
	}
	if !id.TrustsCert(first) {
		t.Fatal("the original hub must still be trusted")
	}
}

func TestWrongTokenIsRejected(t *testing.T) {
	id := load(t, t.TempDir())
	if _, err := id.NewEnrolmentToken(); err != nil {
		t.Fatal(err)
	}
	if err := id.Enrol("not-the-token", hubCert(t)); !errors.Is(err, ErrTokenMismatch) {
		t.Fatalf("want ErrTokenMismatch, got %v", err)
	}
	if id.Enrolled() {
		t.Fatal("a failed enrolment must not enrol the agent")
	}
}

func TestEnrolWithoutATokenIsRejected(t *testing.T) {
	id := load(t, t.TempDir())
	if err := id.Enrol("anything", hubCert(t)); !errors.Is(err, ErrTokenMismatch) {
		t.Fatalf("want ErrTokenMismatch when no token is outstanding, got %v", err)
	}
}

func TestExpiredTokenIsRejectedAndBurned(t *testing.T) {
	id := load(t, t.TempDir())
	token, _ := id.NewEnrolmentToken()

	id.mu.Lock()
	id.st.TokenExpiry = time.Now().Add(-time.Second)
	_ = id.persist()
	id.mu.Unlock()

	if err := id.Enrol(token, hubCert(t)); !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("want ErrTokenExpired, got %v", err)
	}
	// Expiry clears the token, so a later clock change cannot revive it.
	if err := id.Enrol(token, hubCert(t)); !errors.Is(err, ErrTokenMismatch) {
		t.Fatalf("an expired token should be gone, got %v", err)
	}
}

func TestResetAllowsReEnrolmentButKeepsIdentity(t *testing.T) {
	id := load(t, t.TempDir())
	before := id.Fingerprint()

	token, _ := id.NewEnrolmentToken()
	if err := id.Enrol(token, hubCert(t)); err != nil {
		t.Fatal(err)
	}
	if err := id.Reset(); err != nil {
		t.Fatal(err)
	}
	if id.Enrolled() {
		t.Fatal("reset should clear the enrolment")
	}
	if id.Fingerprint() != before {
		t.Fatal("reset must keep the agent's own keypair")
	}

	second := hubCert(t)
	token2, err := id.NewEnrolmentToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := id.Enrol(token2, second); err != nil {
		t.Fatal(err)
	}
	if !id.TrustsCert(second) {
		t.Fatal("the new hub should be trusted after a reset")
	}
}

func TestUnenrolledAgentTrustsNothing(t *testing.T) {
	id := load(t, t.TempDir())
	if id.TrustsCert(hubCert(t)) {
		t.Fatal("an un-enrolled agent must not trust any certificate")
	}
}

func TestGarbageCertificateIsRejected(t *testing.T) {
	id := load(t, t.TempDir())
	token, _ := id.NewEnrolmentToken()
	if err := id.Enrol(token, []byte("not a certificate")); err == nil {
		t.Fatal("a malformed certificate should not enrol")
	}
	if id.Enrolled() {
		t.Fatal("a failed enrolment must leave the agent un-enrolled")
	}
}

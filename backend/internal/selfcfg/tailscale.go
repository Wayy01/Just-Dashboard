package selfcfg

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Wayy01/Just-Dashboard/backend/internal/hostexec"
)

// The one configuration in which a browser opening this dashboard sees an
// ordinary padlock and no warning.
//
// Everything else about reaching a private dashboard over TLS is a compromise.
// Caddy's internal CA encrypts perfectly well but nothing outside this machine
// has heard of the issuer, so every browser says "not secure" — which teaches
// the operator to click through certificate warnings, on the panel that has
// root on their server. Public ACME cannot help either: there is no public DNS
// name and no public address to validate against.
//
// Tailscale can, and it is the reason this file exists. A tailnet with HTTPS
// enabled gets Let's Encrypt certificates for its MagicDNS names through
// Tailscale's own DNS-01 validation, so `tailscale cert box.tailnet.ts.net`
// returns a genuinely trusted certificate for a name that resolves only inside
// the tailnet. The dashboard then answers on https://box.tailnet.ts.net:8443
// with no warning to click through and nothing to explain.
//
// The certificate is short-lived, which is what makes this a keeper rather
// than an install step: it is checked daily and renewed well before it runs
// out, and the proxy is restarted afterwards because Caddy reads a file-based
// certificate once, at start-up.

const (
	// CertFile and KeyFile live in a directory both the backend and the proxy
	// mount, under the names the Caddyfile expects.
	CertFile = "site.crt"
	KeyFile  = "site.key"
	// renewBefore is how much life a certificate must have left to be left
	// alone. Tailscale issues for 90 days; a month of slack means a machine
	// that is off for a fortnight still comes back to a valid certificate.
	renewBefore = 30 * 24 * time.Hour
	// checkInterval is deliberately not "once at boot". A dashboard that is
	// never restarted is the normal case for this product, and it is exactly
	// the install whose certificate would otherwise expire underneath it.
	checkInterval = 12 * time.Hour
)

// CertKeeper issues and renews the Tailscale certificate.
type CertKeeper struct {
	site string
	mode string
	dir  string
	// restartProxy is what makes a renewed certificate take effect. Caddy
	// loads a file-based certificate at start-up and does not watch the file,
	// so a renewal nothing acted on would be a renewal that changed nothing
	// until the next reboot.
	restartProxy func(ctx context.Context) error
	log          *slog.Logger

	stop context.CancelFunc
}

func NewCertKeeper(site, mode, dataDir string, restartProxy func(context.Context) error, log *slog.Logger) *CertKeeper {
	return &CertKeeper{
		site:         strings.TrimSpace(site),
		mode:         strings.ToLower(strings.TrimSpace(mode)),
		dir:          filepath.Join(dataDir, "certs"),
		restartProxy: restartProxy,
		log:          log,
	}
}

// Start keeps the certificate current for as long as ctx lives. It returns
// immediately: a dashboard must not refuse to boot because a tailnet was
// briefly unreachable, and the proxy has whatever certificate it was last
// given in the meantime.
func (k *CertKeeper) Start(ctx context.Context) {
	if k.mode != TLSTailscale {
		return
	}
	ctx, k.stop = context.WithCancel(ctx)
	go func() {
		ticker := time.NewTicker(checkInterval)
		defer ticker.Stop()
		for {
			if err := k.Ensure(ctx); err != nil {
				k.log.Warn("tailscale certificate not renewed", "site", k.site, "error", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (k *CertKeeper) Stop() {
	if k.stop != nil {
		k.stop()
	}
}

// Ensure issues the certificate when there is not a healthy one already.
func (k *CertKeeper) Ensure(ctx context.Context) error {
	if k.mode != TLSTailscale {
		return nil
	}
	if k.site == "" || !strings.HasSuffix(strings.ToLower(k.site), ".ts.net") {
		return fmt.Errorf("JD_TLS is %q but JD_SITE (%q) is not a MagicDNS name, "+
			"so no certificate can be issued for it", k.mode, k.site)
	}
	if fresh, until := k.healthy(); fresh {
		k.log.Debug("tailscale certificate is current", "site", k.site, "expires", until)
		return nil
	}
	if err := Issue(ctx, k.site, k.dir); err != nil {
		return err
	}
	k.log.Info("tailscale certificate issued", "site", k.site, "dir", k.dir)
	if k.restartProxy == nil {
		return nil
	}
	if err := k.restartProxy(ctx); err != nil {
		return fmt.Errorf("the certificate was issued but the proxy could not be restarted to load it: %w", err)
	}
	return nil
}

// healthy reports whether the certificate on disk covers this site and has
// enough life left.
func (k *CertKeeper) healthy() (bool, time.Time) {
	cert, err := readCertificate(filepath.Join(k.dir, CertFile))
	if err != nil {
		return false, time.Time{}
	}
	if err := cert.VerifyHostname(k.site); err != nil {
		return false, cert.NotAfter
	}
	return time.Until(cert.NotAfter) > renewBefore, cert.NotAfter
}

// Issue runs `tailscale cert` on the host and writes the pair into dir.
//
// On the host, not in this container: tailscaled's socket is the host's, and
// the dashboard's own image deliberately does not carry a Tailscale client.
// The paths are the host's too, which is why the certificate directory is
// mounted at the same absolute path on both sides.
func Issue(ctx context.Context, site, dir string) error {
	if !hostexec.Available("tailscale") {
		return errors.New("tailscale is not installed on this host, so it cannot issue a certificate")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	cmd := hostexec.CommandOnHost(ctx, "tailscale", "cert",
		"--cert-file", filepath.Join(dir, CertFile),
		"--key-file", filepath.Join(dir, KeyFile),
		site)
	out, err := cmd.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(out))
		if detail == "" {
			detail = err.Error()
		}
		return fmt.Errorf("tailscale cert %s: %s", site, detail)
	}
	// The proxy runs as a different user inside its container and only ever
	// reads these, so the key is readable by root alone and the certificate by
	// anyone who can reach the directory.
	_ = os.Chmod(filepath.Join(dir, CertFile), 0o644)
	_ = os.Chmod(filepath.Join(dir, KeyFile), 0o640)
	return nil
}

func readCertificate(path string) (*x509.Certificate, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	for len(b) > 0 {
		var block *pem.Block
		block, b = pem.Decode(b)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		return x509.ParseCertificate(block.Bytes)
	}
	return nil, errors.New("no certificate in " + path)
}

package config

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is resolved once at boot from the environment. Every field that
// weakens the security posture (allowlist, TLS, 2FA) fails closed: the zero
// value is the restrictive one.
type Config struct {
	Addr           string
	DataDir        string
	MasterKeyHex   string
	TrustedProxies []*net.IPNet
	AllowedCIDRs   []*net.IPNet
	AllowedOrigins []string
	Require2FA     bool
	SessionTTL     time.Duration
	IdleTTL        time.Duration
	DockerHost     string
	TerminalEnable bool
	TerminalShell  string
	FileRoots      []string
	LogRoots       []string
	NginxDir       string
	CaddyFile      string
	ComposeRoots   []string
	DeployRoot     string
	BackupLocalDir string
	Dev            bool
}

func Load() (*Config, error) {
	c := &Config{
		Addr:           env("VPSD_ADDR", "127.0.0.1:8080"),
		DataDir:        env("VPSD_DATA_DIR", "/var/lib/vps-dashboard"),
		MasterKeyHex:   os.Getenv("VPSD_MASTER_KEY"),
		Require2FA:     envBool("VPSD_REQUIRE_2FA", true),
		SessionTTL:     envDur("VPSD_SESSION_TTL", 12*time.Hour),
		IdleTTL:        envDur("VPSD_SESSION_IDLE_TTL", 60*time.Minute),
		DockerHost:     env("VPSD_DOCKER_HOST", "unix:///var/run/docker.sock"),
		TerminalEnable: envBool("VPSD_TERMINAL_ENABLED", true),
		TerminalShell:  env("VPSD_TERMINAL_SHELL", "/bin/bash"),
		FileRoots:      envList("VPSD_FILE_ROOTS", "/"),
		LogRoots:       envList("VPSD_LOG_ROOTS", "/var/log"),
		NginxDir:       env("VPSD_NGINX_DIR", "/etc/nginx"),
		CaddyFile:      env("VPSD_CADDYFILE", "/etc/caddy/Caddyfile"),
		ComposeRoots:   envList("VPSD_COMPOSE_ROOTS", "/opt,/srv,/home"),
		DeployRoot:     env("VPSD_DEPLOY_ROOT", "/srv"),
		BackupLocalDir: env("VPSD_BACKUP_DIR", "/var/backups/vps-dashboard"),
		Dev:            envBool("VPSD_DEV", false),
	}

	// The dashboard is root-equivalent. It must never be reachable from the
	// open internet, so an explicit network allowlist is mandatory. Binding
	// to loopback only is the one configuration that implies its own
	// perimeter (reverse proxy / WireGuard terminates in front of us).
	raw := strings.TrimSpace(os.Getenv("VPSD_ALLOWED_CIDRS"))
	if raw == "" {
		if !isLoopbackBind(c.Addr) {
			return nil, fmt.Errorf("VPSD_ALLOWED_CIDRS is required when VPSD_ADDR (%s) is not bound to loopback: "+
				"expose this dashboard only through a WireGuard/VPN interface or a reverse proxy with an IP allowlist", c.Addr)
		}
		raw = "127.0.0.1/32,::1/128"
	}
	nets, err := parseCIDRs(raw)
	if err != nil {
		return nil, fmt.Errorf("VPSD_ALLOWED_CIDRS: %w", err)
	}
	c.AllowedCIDRs = nets

	if tp := strings.TrimSpace(os.Getenv("VPSD_TRUSTED_PROXIES")); tp != "" {
		nets, err := parseCIDRs(tp)
		if err != nil {
			return nil, fmt.Errorf("VPSD_TRUSTED_PROXIES: %w", err)
		}
		c.TrustedProxies = nets
	}

	c.AllowedOrigins = envList("VPSD_ALLOWED_ORIGINS", "")
	if len(c.MasterKeyHex) != 64 {
		return nil, fmt.Errorf("VPSD_MASTER_KEY must be 64 hex characters (32 bytes); generate one with: openssl rand -hex 32")
	}
	return c, nil
}

func parseCIDRs(raw string) ([]*net.IPNet, error) {
	var out []*net.IPNet
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if !strings.Contains(part, "/") {
			ip := net.ParseIP(part)
			if ip == nil {
				return nil, fmt.Errorf("invalid address %q", part)
			}
			bits := 32
			if ip.To4() == nil {
				bits = 128
			}
			part = fmt.Sprintf("%s/%d", part, bits)
		}
		_, n, err := net.ParseCIDR(part)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no usable entries")
	}
	return out, nil
}

func isLoopbackBind(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func env(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

func envBool(k string, def bool) bool {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

func envDur(k string, def time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}

func envList(k, def string) []string {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		v = def
	}
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

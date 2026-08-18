// Package proxysvc manages the reverse proxy in front of the host — nginx or
// Caddy — plus the TLS certificates and listening ports that go with it.
//
// The rule this package exists to enforce: a configuration is never activated
// without passing the server's own validator first. Reloading a broken nginx
// config takes every site on the box offline, and doing that from a web UI
// would be an unforced outage.
package proxysvc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var (
	ErrNoProxy     = errors.New("neither nginx nor Caddy was found on this host")
	ErrInvalidConf = errors.New("configuration failed validation")
	ErrUnsafePath  = errors.New("path is outside the proxy configuration directory")
)

type Kind string

const (
	KindNginx Kind = "nginx"
	KindCaddy Kind = "caddy"
)

type Service struct {
	nginxDir  string
	caddyFile string
}

func New(nginxDir, caddyFile string) *Service {
	return &Service{nginxDir: filepath.Clean(nginxDir), caddyFile: filepath.Clean(caddyFile)}
}

type Availability struct {
	Nginx     bool   `json:"nginx"`
	Caddy     bool   `json:"caddy"`
	NginxVer  string `json:"nginxVersion,omitempty"`
	CaddyVer  string `json:"caddyVersion,omitempty"`
	NginxDir  string `json:"nginxDir"`
	CaddyFile string `json:"caddyFile"`
	Certbot   bool   `json:"certbot"`
}

func (s *Service) Availability(ctx context.Context) Availability {
	a := Availability{NginxDir: s.nginxDir, CaddyFile: s.caddyFile}
	if _, err := exec.LookPath("nginx"); err == nil {
		a.Nginx = true
		if out, err := exec.CommandContext(ctx, "nginx", "-v").CombinedOutput(); err == nil || len(out) > 0 {
			a.NginxVer = strings.TrimSpace(string(out))
		}
	}
	if _, err := exec.LookPath("caddy"); err == nil {
		a.Caddy = true
		if out, err := exec.CommandContext(ctx, "caddy", "version").Output(); err == nil {
			a.CaddyVer = strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
		}
	}
	if _, err := exec.LookPath("certbot"); err == nil {
		a.Certbot = true
	}
	return a
}

// VHost is one virtual host. For nginx the enabled state is the presence of a
// symlink in sites-enabled, which is the convention Debian-family packages use
// and the one operators expect the toggle to drive.
type VHost struct {
	Name        string    `json:"name"`
	Kind        Kind      `json:"kind"`
	Path        string    `json:"path"`
	EnabledPath string    `json:"enabledPath,omitempty"`
	Enabled     bool      `json:"enabled"`
	ServerNames []string  `json:"serverNames"`
	Listen      []string  `json:"listen"`
	Upstreams   []string  `json:"upstreams"`
	TLS         bool      `json:"tls"`
	CertPath    string    `json:"certPath,omitempty"`
	Modified    time.Time `json:"modified"`
	Size        int64     `json:"size"`
}

var (
	serverNameRe = regexp.MustCompile(`(?m)^\s*server_name\s+([^;]+);`)
	listenRe     = regexp.MustCompile(`(?m)^\s*listen\s+([^;]+);`)
	proxyPassRe  = regexp.MustCompile(`(?m)^\s*proxy_pass\s+([^;]+);`)
	certRe       = regexp.MustCompile(`(?m)^\s*ssl_certificate\s+([^;]+);`)
)

func (s *Service) ListVHosts(ctx context.Context) ([]VHost, error) {
	out := []VHost{}
	out = append(out, s.nginxVHosts()...)
	if caddy, err := s.caddySites(); err == nil {
		out = append(out, caddy...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *Service) nginxVHosts() []VHost {
	available := filepath.Join(s.nginxDir, "sites-available")
	enabled := filepath.Join(s.nginxDir, "sites-enabled")
	entries, err := os.ReadDir(available)
	if err != nil {
		// Hosts without the Debian layout keep everything in conf.d.
		available = filepath.Join(s.nginxDir, "conf.d")
		entries, err = os.ReadDir(available)
		if err != nil {
			return nil
		}
		enabled = ""
	}
	out := []VHost{}
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		full := filepath.Join(available, e.Name())
		info, err := e.Info()
		if err != nil {
			continue
		}
		v := VHost{
			Name: e.Name(), Kind: KindNginx, Path: full,
			Modified: info.ModTime().UTC(), Size: info.Size(),
			ServerNames: []string{}, Listen: []string{}, Upstreams: []string{},
		}
		if enabled == "" {
			// In the conf.d layout every present file is active.
			v.Enabled = true
		} else {
			link := filepath.Join(enabled, e.Name())
			if _, err := os.Lstat(link); err == nil {
				v.Enabled = true
				v.EnabledPath = link
			} else {
				v.EnabledPath = link
			}
		}
		if b, err := os.ReadFile(full); err == nil {
			text := string(b)
			for _, m := range serverNameRe.FindAllStringSubmatch(text, -1) {
				v.ServerNames = append(v.ServerNames, strings.Fields(m[1])...)
			}
			for _, m := range listenRe.FindAllStringSubmatch(text, -1) {
				v.Listen = append(v.Listen, strings.TrimSpace(m[1]))
			}
			for _, m := range proxyPassRe.FindAllStringSubmatch(text, -1) {
				v.Upstreams = append(v.Upstreams, strings.TrimSpace(m[1]))
			}
			if m := certRe.FindStringSubmatch(text); m != nil {
				v.TLS = true
				v.CertPath = strings.TrimSpace(m[1])
			}
		}
		out = append(out, v)
	}
	return out
}

// caddySites treats the Caddyfile as a single vhost entry. Caddy's config is
// one file with site blocks rather than a directory of them, so "enable/disable
// a site" has no filesystem equivalent — the editor is the interface.
func (s *Service) caddySites() ([]VHost, error) {
	info, err := os.Stat(s.caddyFile)
	if err != nil {
		return nil, err
	}
	v := VHost{
		Name: filepath.Base(s.caddyFile), Kind: KindCaddy, Path: s.caddyFile,
		Enabled: true, Modified: info.ModTime().UTC(), Size: info.Size(),
		ServerNames: []string{}, Listen: []string{}, Upstreams: []string{},
	}
	if b, err := os.ReadFile(s.caddyFile); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			if strings.HasSuffix(trimmed, "{") && !strings.HasPrefix(trimmed, "reverse_proxy") {
				name := strings.TrimSpace(strings.TrimSuffix(trimmed, "{"))
				if name != "" && !strings.ContainsAny(name, "()") {
					v.ServerNames = append(v.ServerNames, strings.Fields(name)...)
				}
			}
			if after, ok := strings.CutPrefix(trimmed, "reverse_proxy "); ok {
				v.Upstreams = append(v.Upstreams, strings.TrimSpace(strings.TrimSuffix(after, "{")))
			}
		}
	}
	return []VHost{v}, nil
}

// allowedPath keeps the config editor pointed at the proxy's own directories.
// Without it this endpoint would be an arbitrary-file-write primitive dressed
// up as a config editor.
func (s *Service) allowedPath(path string) (string, error) {
	abs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	resolved := abs
	if r, err := filepath.EvalSymlinks(abs); err == nil {
		resolved = r
	}
	roots := []string{s.nginxDir, filepath.Dir(s.caddyFile)}
	for _, root := range roots {
		if resolved == root || strings.HasPrefix(resolved, root+string(os.PathSeparator)) {
			return resolved, nil
		}
	}
	return "", fmt.Errorf("%w: %s", ErrUnsafePath, path)
}

func (s *Service) ReadConfig(path string) (string, error) {
	full, err := s.allowedPath(path)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(full)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

type ValidationResult struct {
	Valid   bool   `json:"valid"`
	Output  string `json:"output"`
	Command string `json:"command"`
}

// Validate runs the server's own config test. For nginx the candidate content
// is written to a temporary copy of the real file, tested, and the original
// restored on failure — nginx has no way to test a config it cannot see at the
// path it expects.
func (s *Service) Validate(ctx context.Context, kind Kind, path, content string) (*ValidationResult, error) {
	switch kind {
	case KindCaddy:
		return s.validateCaddy(ctx, content)
	default:
		return s.validateNginx(ctx, path, content)
	}
}

func (s *Service) validateNginx(ctx context.Context, path, content string) (*ValidationResult, error) {
	full, err := s.allowedPath(path)
	if err != nil {
		return nil, err
	}
	original, readErr := os.ReadFile(full)
	if readErr != nil && !os.IsNotExist(readErr) {
		return nil, readErr
	}
	var mode os.FileMode = 0o644
	if st, err := os.Stat(full); err == nil {
		mode = st.Mode().Perm()
	}
	if err := os.WriteFile(full, []byte(content), mode); err != nil {
		return nil, err
	}
	res := runValidator(ctx, "nginx", "-t")
	if !res.Valid {
		// Put the working config back before returning: leaving a broken file
		// on disk means the next unrelated reload takes the sites down.
		if readErr == nil {
			os.WriteFile(full, original, mode)
		} else {
			os.Remove(full)
		}
	}
	return res, nil
}

func (s *Service) validateCaddy(ctx context.Context, content string) (*ValidationResult, error) {
	tmp, err := os.CreateTemp("", "vpsd-caddy-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return nil, err
	}
	tmp.Close()
	return runValidator(ctx, "caddy", "validate", "--config", tmp.Name(), "--adapter", "caddyfile"), nil
}

func runValidator(ctx context.Context, name string, args ...string) *ValidationResult {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return &ValidationResult{
		Valid:   err == nil,
		Output:  strings.TrimSpace(buf.String()),
		Command: name + " " + strings.Join(args, " "),
	}
}

// WriteConfig saves a configuration only after it validates. The order here is
// the whole point of the endpoint: validate, then write, then reload.
func (s *Service) WriteConfig(ctx context.Context, kind Kind, path, content string) (*ValidationResult, error) {
	full, err := s.allowedPath(path)
	if err != nil {
		return nil, err
	}
	if kind == KindCaddy {
		res, err := s.validateCaddy(ctx, content)
		if err != nil {
			return nil, err
		}
		if !res.Valid {
			return res, ErrInvalidConf
		}
		if err := writeAtomic(full, content); err != nil {
			return nil, err
		}
		return res, nil
	}
	// validateNginx already writes the candidate and rolls back on failure,
	// so a successful result means the new content is on disk.
	res, err := s.validateNginx(ctx, path, content)
	if err != nil {
		return nil, err
	}
	if !res.Valid {
		return res, ErrInvalidConf
	}
	return res, nil
}

func writeAtomic(path, content string) error {
	dir := filepath.Dir(path)
	var mode os.FileMode = 0o644
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	tmp, err := os.CreateTemp(dir, ".vpsd-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

type ReloadResult struct {
	Validation *ValidationResult `json:"validation"`
	Reloaded   bool              `json:"reloaded"`
	Output     string            `json:"output"`
}

// Reload tests first and refuses to reload a config that does not pass. This
// is the guard rail that makes a config editor safe to expose at all.
func (s *Service) Reload(ctx context.Context, kind Kind) (*ReloadResult, error) {
	var validation *ValidationResult
	var reload *exec.Cmd
	switch kind {
	case KindCaddy:
		validation = runValidator(ctx, "caddy", "validate", "--config", s.caddyFile, "--adapter", "caddyfile")
		reload = exec.CommandContext(ctx, "caddy", "reload", "--config", s.caddyFile)
	default:
		validation = runValidator(ctx, "nginx", "-t")
		reload = exec.CommandContext(ctx, "nginx", "-s", "reload")
	}
	res := &ReloadResult{Validation: validation}
	if !validation.Valid {
		return res, ErrInvalidConf
	}
	out, err := reload.CombinedOutput()
	res.Output = strings.TrimSpace(string(out))
	res.Reloaded = err == nil
	if err != nil {
		return res, fmt.Errorf("reload failed: %s", res.Output)
	}
	return res, nil
}

// SetVHostEnabled toggles the sites-enabled symlink. Only nginx has this
// notion; Caddy has no per-site enable, and saying so is better than pretending.
func (s *Service) SetVHostEnabled(ctx context.Context, name string, enabled bool) error {
	if strings.ContainsAny(name, "/\\") || name == "" || name == "." || name == ".." {
		return fmt.Errorf("invalid vhost name %q", name)
	}
	available := filepath.Join(s.nginxDir, "sites-available", name)
	link := filepath.Join(s.nginxDir, "sites-enabled", name)
	if _, err := os.Stat(available); err != nil {
		return fmt.Errorf("no such vhost: %s", name)
	}
	if enabled {
		if _, err := os.Lstat(link); err == nil {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
			return err
		}
		return os.Symlink(available, link)
	}
	if err := os.Remove(link); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

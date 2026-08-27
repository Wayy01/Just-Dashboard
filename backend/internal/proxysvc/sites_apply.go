package proxysvc

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/Wayy01/Just-Dashboard/backend/internal/hostexec"
)

// ParseSiteSpec reads a site file back into the form.
//
// Not an nginx parser — a line reader that tracks brace depth and which
// location block it is inside, which is enough for the files this form
// produces and for the great majority of hand-written ones. What it cannot
// promise is that saving the form reproduces the file, so it reports whether
// the file carries our marker: a managed file round-trips, and for anything
// else the UI says plainly that saving will replace what is there.
func ParseSiteSpec(name, content string) (*SiteSpec, bool) {
	spec := &SiteSpec{
		Name: name, Kind: "proxy",
		Domains: []string{}, AllowFrom: []string{}, DenyFrom: []string{}, Locations: []SiteLocation{},
	}
	managed := strings.Contains(content, managedMarker)

	seenDomains := map[string]bool{}
	depth := 0
	location := ""
	var current *SiteLocation
	rootLocationUpstream := ""
	rootLocationWS := false
	sawTLSListen := false
	sawPlainRedirect := false

	sc := bufio.NewScanner(strings.NewReader(content))
	sc.Buffer(make([]byte, 0, 8192), 1<<20)
	for sc.Scan() {
		raw := strings.TrimSpace(sc.Text())
		if raw == "" || strings.HasPrefix(raw, "#") {
			continue
		}
		if m := locationOpenRe.FindStringSubmatch(raw); m != nil {
			depth++
			location = m[1]
			// The ACME challenge location belongs to the redirect block this
			// renderer writes for a forced-HTTPS site. Reading it back as one
			// of the operator's own locations makes a round trip emit it
			// twice — once in the redirect server and once inside the TLS
			// one, where it does nothing.
			if location != "/" && location != acmeChallengePath &&
				!strings.HasPrefix(location, "~") && locationPathRe.MatchString(location) {
				spec.Locations = append(spec.Locations, SiteLocation{Path: location})
				current = &spec.Locations[len(spec.Locations)-1]
			} else {
				current = nil
			}
			continue
		}
		if strings.HasSuffix(raw, "{") {
			depth++
			continue
		}
		if raw == "}" {
			depth--
			location, current = "", nil
			continue
		}

		directive, value := cutDirective(raw)
		switch directive {
		case "server_name":
			for _, d := range strings.Fields(value) {
				if d == "_" || seenDomains[d] {
					continue
				}
				seenDomains[d] = true
				spec.Domains = append(spec.Domains, d)
			}
		case "listen":
			if strings.Contains(value, "ssl") || strings.HasPrefix(value, "443") {
				spec.TLS = true
				sawTLSListen = true
			}
		case "http2":
			spec.HTTP2 = value == "on"
		case "ssl_certificate":
			spec.CertPath = value
		case "ssl_certificate_key":
			spec.KeyPath = value
		case "client_max_body_size":
			spec.ClientMaxBody = value
		case "gzip":
			spec.Gzip = value == "on"
		case "access_log":
			spec.AccessLog = value != "off"
		case "auth_basic":
			spec.BasicAuthRealm = strings.Trim(value, `"`)
		case "auth_basic_user_file":
			spec.BasicAuthFile = value
		case "allow":
			spec.AllowFrom = append(spec.AllowFrom, value)
		case "deny":
			if location == "" {
				spec.DenyFrom = append(spec.DenyFrom, value)
			}
		case "add_header":
			if strings.HasPrefix(value, "Strict-Transport-Security") {
				spec.HSTS = true
			}
			if strings.HasPrefix(value, "X-Content-Type-Options") {
				spec.SecurityHeaders = true
			}
		case "root":
			if location == "" || location == "/" {
				spec.Root = value
			} else if current != nil {
				current.Root = value
			}
		case "proxy_pass":
			if current != nil {
				current.Upstream = value
			} else if location == "/" || location == "" {
				rootLocationUpstream = value
			}
		case "proxy_set_header":
			if strings.HasPrefix(value, "Upgrade") {
				if current != nil {
					current.WebSockets = true
				} else {
					rootLocationWS = true
				}
			}
		case "proxy_read_timeout":
			spec.ProxyTimeout = parseSeconds(value)
		case "return":
			fields := strings.Fields(value)
			if len(fields) >= 2 {
				target := fields[1]
				if strings.Contains(target, "$host$request_uri") {
					sawPlainRedirect = true
				} else if strings.HasPrefix(target, "http") {
					spec.Kind = "redirect"
					spec.RedirectTo = strings.TrimSuffix(target, "$request_uri")
					spec.Permanent = fields[0] == "301"
				}
			}
		}
	}

	spec.Upstream = rootLocationUpstream
	spec.WebSockets = rootLocationWS
	if spec.Kind != "redirect" {
		if spec.Upstream == "" && spec.Root != "" {
			spec.Kind = "static"
		}
	}
	spec.ForceHTTPS = sawTLSListen && sawPlainRedirect
	// A file with a plain-HTTP redirect block and nothing else is a redirect
	// site; one that also serves something is a TLS site forcing HTTPS.
	if spec.Kind == "proxy" && spec.Upstream == "" && spec.Root == "" && sawPlainRedirect {
		spec.Kind = "redirect"
		spec.RedirectTo = "https://" + firstOr(spec.Domains, "")
		spec.Permanent = true
	}
	// Extra locations that ended up with neither an upstream nor a root are
	// something this form cannot express; dropping them is better than
	// offering to save a location that proxies nowhere.
	kept := spec.Locations[:0]
	for _, loc := range spec.Locations {
		if loc.Upstream != "" || loc.Root != "" {
			kept = append(kept, loc)
		}
	}
	spec.Locations = kept
	return spec, managed
}

const acmeChallengePath = "/.well-known/acme-challenge/"

var locationOpenRe = regexp.MustCompile(`^location\s+(?:[~^=*]+\s+)?(\S+)\s*\{`)

func cutDirective(line string) (string, string) {
	line = strings.TrimSuffix(strings.TrimSpace(line), ";")
	name, value, ok := strings.Cut(line, " ")
	if !ok {
		return name, ""
	}
	return name, strings.TrimSpace(value)
}

func parseSeconds(value string) int {
	value = strings.TrimSuffix(strings.TrimSpace(value), "s")
	n, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return n
}

func firstOr(list []string, fallback string) string {
	if len(list) == 0 {
		return fallback
	}
	return list[0]
}

// SiteResult reports what happened to a site, in order.
type SiteResult struct {
	Name       string            `json:"name"`
	Path       string            `json:"path"`
	Content    string            `json:"content"`
	Warnings   []string          `json:"warnings"`
	Validation *ValidationResult `json:"validation,omitempty"`
	Enabled    bool              `json:"enabled"`
	Reloaded   bool              `json:"reloaded"`
	Output     string            `json:"output,omitempty"`
}

// ApplySite writes a site, enables it, tests the whole configuration and puts
// everything back if the test fails.
//
// The order matters and is different from the plain config editor's. A brand
// new file in sites-available is not in nginx's include tree, so `nginx -t`
// has nothing to say about it — the existing editor documents that gap. Here
// the symlink goes in *before* the test, which is what makes the test mean
// something, and both the file and the link are undone together if it fails.
func (s *Service) ApplySite(ctx context.Context, spec *SiteSpec, enable, reload, overwrite bool) (*SiteResult, error) {
	content, err := RenderNginx(spec)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	available := filepath.Join(s.nginxDir, "sites-available", spec.Name)
	if _, err := os.Stat(filepath.Dir(available)); err != nil {
		// A host keeping everything in conf.d has no sites-available, and
		// there is no enable/disable there either.
		available = filepath.Join(s.nginxDir, "conf.d", spec.Name+".conf")
		enable = true
	}
	full, err := s.allowedPath(available)
	if err != nil {
		return nil, err
	}
	original, existed := readIfPresent(full)
	if existed && !overwrite {
		return nil, fmt.Errorf("a site called %s already exists", spec.Name)
	}

	res := &SiteResult{
		Name: spec.Name, Path: full, Content: content, Warnings: SpecWarnings(spec),
	}
	if err := writeAtomic(full, content); err != nil {
		return nil, err
	}
	undoLink := func() {}
	if !strings.Contains(full, "sites-available") {
		// In the conf.d layout every present file is active, so there is no
		// symlink to make and nothing to report as pending.
		res.Enabled = true
	} else if enable {
		link := filepath.Join(s.nginxDir, "sites-enabled", spec.Name)
		if _, err := os.Lstat(link); err != nil {
			if err := os.MkdirAll(filepath.Dir(link), 0o755); err == nil &&
				os.Symlink(full, link) == nil {
				undoLink = func() { os.Remove(link) }
			}
		}
		res.Enabled = true
	}

	res.Validation = runValidator(ctx, "nginx", "-t")
	if !res.Validation.Valid {
		undoLink()
		restoreConfig(full, original, existed)
		res.Enabled = false
		return res, ErrInvalidConf
	}
	if reload {
		raw, err := hostexec.Command(ctx, "nginx", "-s", "reload").CombinedOutput()
		out := strings.TrimSpace(string(raw))
		res.Output = out
		if err != nil {
			// The config tested clean, so a reload failure is about the
			// running process rather than the file. Undoing the write would
			// lose the operator's work for a problem it did not cause.
			return res, fmt.Errorf("reload failed: %s", out)
		}
		res.Reloaded = true
	}
	return res, nil
}

func readIfPresent(path string) (string, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return string(b), true
}

func restoreConfig(path, original string, existed bool) {
	if existed {
		writeAtomic(path, original)
		return
	}
	os.Remove(path)
}

// DeleteSite removes a site's file and its symlink.
//
// Both, and in that order: leaving the link behind points nginx at a file that
// no longer exists, which takes every site on the box down at the next reload.
func (s *Service) DeleteSite(ctx context.Context, name string) error {
	if !siteNameRe.MatchString(name) {
		return fmt.Errorf("invalid site name")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	link := filepath.Join(s.nginxDir, "sites-enabled", name)
	if _, err := os.Lstat(link); err == nil {
		if err := os.Remove(link); err != nil {
			return err
		}
	}
	for _, candidate := range []string{
		filepath.Join(s.nginxDir, "sites-available", name),
		filepath.Join(s.nginxDir, "conf.d", name+".conf"),
	} {
		full, err := s.allowedPath(candidate)
		if err != nil {
			continue
		}
		if _, err := os.Stat(full); err != nil {
			continue
		}
		// Kept as .bak for the same reason a compose file is: validation
		// catches a broken config, not a correct one that says the wrong
		// thing, and the only cure for the second is the previous version.
		if b, err := os.ReadFile(full); err == nil {
			os.WriteFile(full+".bak", b, 0o644)
		}
		return os.Remove(full)
	}
	return fmt.Errorf("no such site: %s", name)
}

package selfcfg

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/Wayy01/Just-Dashboard/backend/internal/selfupdate"
)

// Service is the module the API holds: what the dashboard is configured to do,
// what it is actually doing, and the two ways of changing either.
type Service struct {
	dataDir string
	store   *Store
	applier *Applier
	// locate is where this install lives. It is a function rather than a
	// second copy of the discovery logic: internal/selfupdate already works
	// this out by asking Docker which container this is, caches the answer,
	// and is the package that owns the question.
	locate   func(ctx context.Context) (*selfupdate.Location, error)
	observed func() Observed
	list     selfupdate.Lister
	log      *slog.Logger

	// The tailnet identity, cached: the settings page polls, and shelling out
	// to `tailscale status` on every poll would be a subprocess a second for an
	// answer that changes when somebody runs `tailscale up`.
	mu        sync.Mutex
	tailnet   Identity
	tailnetAt time.Time
}

// tailnetTTL is how long the discovered tailnet identity is trusted.
const tailnetTTL = 30 * time.Second

// Observed is what this process is actually running, as opposed to what the
// file on disk says it should be.
//
// Only the settings this backend can see for itself are here: it binds its own
// port and enforces its own allowlist, but the dashboard port and the frontend
// port belong to two other containers and it has no honest way to observe
// them. Reporting a guess for those would be worse than reporting nothing.
type Observed struct {
	// Site and TLS are passed to this container by compose, so the backend can
	// see them even though the proxy is what acts on them.
	Site            string
	TLS             string
	BackendPort     int
	AllowedCIDRs    string
	TerminalEnabled bool
	Require2FA      bool
	SessionTTL      string
	IdleTTL         string
	UpdateCheck     bool
}

type Options struct {
	DataDir    string
	DockerHost string
	Locate     func(ctx context.Context) (*selfupdate.Location, error)
	// Observed reports the running configuration. A function rather than a
	// value because the process can be told to reload, and a snapshot taken at
	// construction would go quietly out of date.
	Observed func() Observed
	List     selfupdate.Lister
	Log      *slog.Logger
}

func New(o Options) *Service {
	store := NewStore(o.DataDir)
	return &Service{
		dataDir:  o.DataDir,
		store:    store,
		applier:  NewApplier(store, o.DataDir, o.DockerHost, o.Log),
		locate:   o.Locate,
		observed: o.Observed,
		list:     o.List,
		log:      o.Log,
	}
}

// Start settles a restart that was in flight when this process began.
func (s *Service) Start(ctx context.Context) { s.applier.Reconcile(ctx, s.list) }

// Report is the whole answer the settings page renders.
type Report struct {
	// Supported is whether this install can restart itself. An install with no
	// compose project — a binary under a systemd unit — reads its settings
	// here perfectly well and is told plainly that changing them is a job for
	// ssh, rather than being shown buttons that fail.
	Supported bool   `json:"supported"`
	Reason    string `json:"reason,omitempty"`

	Dir     string `json:"dir,omitempty"`
	Compose string `json:"compose,omitempty"`
	EnvPath string `json:"envPath,omitempty"`

	Settings Settings `json:"settings"`
	Endpoint string   `json:"endpoint"`
	// Tailscale is what this machine is on its tailnet, so the form can fill
	// the address, the interface and the allowlist in for itself when the
	// operator picks that certificate — rather than rejecting what they typed
	// because they had to guess at three facts the machine already knew.
	Tailscale Identity `json:"tailscale"`
	// Drift is the file disagreeing with the running process, which is what an
	// operator who edited .env over ssh and never restarted is looking at. It
	// covers only the settings this backend can observe about itself.
	Drift []Change `json:"drift,omitempty"`

	Run *Run   `json:"run,omitempty"`
	Log string `json:"log,omitempty"`
}

func (s *Service) Report(ctx context.Context) Report {
	rep := Report{Settings: defaults()}

	loc, err := s.locate(ctx)
	if err != nil {
		rep.Reason = err.Error()
	} else {
		rep.Supported = true
		rep.Dir, rep.Compose = loc.Dir, loc.Compose
		rep.EnvPath = filepath.Join(loc.Dir, ".env")
		if env, err := LoadEnv(s.visibleEnv(loc)); err == nil {
			rep.Settings = ReadSettings(env)
		} else {
			rep.Supported = false
			rep.Reason = fmt.Sprintf("%s could not be read: %v", rep.EnvPath, err)
		}
	}
	rep.Endpoint = rep.Settings.Endpoint()
	rep.Drift = s.drift(rep.Settings)
	rep.Tailscale = s.Tailnet(ctx)

	if run, err := s.store.Load(); err == nil && run != nil {
		rep.Run = run
		rep.Log = s.store.Tail()
	}
	return rep
}

// Tailnet is the machine's tailnet identity, cached for tailnetTTL.
func (s *Service) Tailnet(ctx context.Context) Identity {
	s.mu.Lock()
	if time.Since(s.tailnetAt) < tailnetTTL {
		id := s.tailnet
		s.mu.Unlock()
		return id
	}
	s.mu.Unlock()

	id := DetectTailscale(ctx)

	s.mu.Lock()
	s.tailnet, s.tailnetAt = id, time.Now()
	s.mu.Unlock()
	return id
}

// drift compares the file with the process, on the fields the process knows.
func (s *Service) drift(onDisk Settings) []Change {
	if s.observed == nil {
		return nil
	}
	live := s.observed()
	running := onDisk
	// The allowlist is compared through the same parser both sides went
	// through, so "127.0.0.1" in the file and "127.0.0.1/32" in the process
	// are not reported as a disagreement for the lifetime of the install.
	running.AllowedCIDRs = normalizeCIDRs(live.AllowedCIDRs)
	onDisk.AllowedCIDRs = normalizeCIDRs(onDisk.AllowedCIDRs)
	if live.Site != "" {
		running.Site = live.Site
	}
	if live.TLS != "" {
		running.TLS = live.TLS
	}
	running.BackendPort = live.BackendPort
	running.TerminalEnabled = live.TerminalEnabled
	running.Require2FA = live.Require2FA
	running.SessionTTL = live.SessionTTL
	running.IdleTTL = live.IdleTTL
	running.UpdateCheck = live.UpdateCheck
	// Diff the other way round from the apply path: "from" is what is running
	// and "to" is what the file asks for, which is the order the sentence
	// "restarting would change X from A to B" needs.
	return Diff(running, onDisk)
}

// visibleEnv is the .env as this process can open it. Bind mounts are resolved
// against the host, so the checkout may only be reachable under /host.
func (s *Service) visibleEnv(loc *selfupdate.Location) string {
	dir := loc.Visible
	if dir == "" {
		dir = loc.Dir
	}
	return filepath.Join(dir, ".env")
}

// Apply validates a proposed configuration, writes it, and restarts into it.
//
// The order matters and is the whole safety argument: everything that can be
// checked is checked before a single byte is written, the previous file is
// copied before it is replaced, and the process that decides whether the
// result works is a container that outlives the one running this code.
func (s *Service) Apply(ctx context.Context, next Settings, actor, clientIP string) (*Run, error) {
	loc, err := s.locate(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNoLocation, err)
	}
	if prev, err := s.store.Load(); err == nil && prev != nil && prev.Status.Live() {
		return nil, ErrInProgress
	}

	envPath := s.visibleEnv(loc)
	env, err := LoadEnv(envPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", envPath, err)
	}
	current := ReadSettings(env)
	if err := next.Validate(current, clientIP, PortFree); err != nil {
		return nil, err
	}
	changes := Diff(current, next)
	if len(changes) == 0 {
		return nil, ErrNoChange
	}

	// Switching to a Tailscale certificate obtains it *now*, before anything
	// is written. The proxy cannot start without the file, so an install that
	// wrote the setting first and discovered the tailnet had HTTPS turned off
	// afterwards would take the dashboard down and recover it by rollback —
	// a minute of outage to deliver an error message that was available up
	// front. Refusing here costs nothing and says exactly what is wrong.
	if next.TLS == TLSTailscale && (current.TLS != TLSTailscale || current.Site != next.Site) {
		if err := Issue(ctx, next.Site, filepath.Join(s.dataDir, "certs")); err != nil {
			return nil, fmt.Errorf("no certificate could be issued for %s, so the dashboard was left as it is: %w",
				next.Site, err)
		}
	}

	env.Apply(next.EnvValues())
	backup, err := env.Save()
	if err != nil {
		return nil, fmt.Errorf("write %s: %w", envPath, err)
	}

	run, err := s.applier.Start(ctx, loc, StartRequest{
		Action:  ActionApply,
		Changes: changes,
		// Host paths: the sibling mounts the checkout at its real name, so
		// what it restores is the file compose reads.
		EnvPath:        filepath.Join(loc.Dir, ".env"),
		Backup:         hostPath(loc, backup),
		Health:         healthURL(next.BackendPort),
		RollbackHealth: healthURL(current.BackendPort),
		Endpoint:       next.Endpoint(),
		Actor:          actor,
	})
	if err != nil {
		return nil, err
	}
	return run, nil
}

// Restart recreates the containers on the configuration already on disk,
// optionally rebuilding the images first.
func (s *Service) Restart(ctx context.Context, rebuild bool, actor string) (*Run, error) {
	loc, err := s.locate(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNoLocation, err)
	}
	env, err := LoadEnv(s.visibleEnv(loc))
	if err != nil {
		return nil, err
	}
	current := ReadSettings(env)
	action := ActionRestart
	if rebuild {
		action = ActionRebuild
	}
	// No EnvPath and no Backup: nothing is written, so there is nothing to put
	// back. A restart that fails to come up is a broken configuration that was
	// already on disk, and the transcript is what the operator needs.
	return s.applier.Start(ctx, loc, StartRequest{
		Action:   action,
		Health:   healthURL(current.BackendPort),
		Endpoint: current.Endpoint(),
		Actor:    actor,
	})
}

// Dismiss forgets a finished run. One still in flight is left alone.
func (s *Service) Dismiss() error {
	run, err := s.store.Load()
	if err != nil {
		// A corrupt record is exactly the one worth being able to clear.
		return s.store.Clear()
	}
	if run == nil {
		return nil
	}
	if run.Status.Live() {
		return ErrInProgress
	}
	return s.store.Clear()
}

// healthURL is the probe for a backend on this host at the given port. The
// backend binds loopback in every configuration, and the sibling runs on the
// host network, so this address means the same thing to both.
func healthURL(port int) string {
	if port <= 0 {
		return ""
	}
	return "http://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(port)) + "/healthz"
}

// hostPath translates a path this process can see into the one the host knows
// it by, which is what a bind mount needs.
func hostPath(loc *selfupdate.Location, visible string) string {
	if visible == "" || loc.Visible == "" || loc.Visible == loc.Dir {
		return visible
	}
	rel, err := filepath.Rel(loc.Visible, visible)
	if err != nil {
		return visible
	}
	return filepath.Join(loc.Dir, rel)
}

// ErrNotSupported is returned to handlers when the install has no stack to
// act on, so the API can answer 503 rather than 500.
var ErrNotSupported = errors.New("this install cannot manage its own configuration")

// Deadline for the whole start-up path, so a wedged Docker daemon cannot hold
// an HTTP handler open indefinitely.
const startTimeout = 30 * time.Second

// WithTimeout is the context every start goes through.
func WithTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, startTimeout)
}

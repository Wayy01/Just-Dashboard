package api

import (
	"context"
	"net"
	"path/filepath"
	"strings"

	"github.com/Wayy01/Just-Dashboard/backend/internal/backups"
	"github.com/Wayy01/Just-Dashboard/backend/internal/dbx"
	"github.com/Wayy01/Just-Dashboard/backend/internal/deploy"
	"github.com/Wayy01/Just-Dashboard/backend/internal/dockerx"
	"github.com/Wayy01/Just-Dashboard/backend/internal/files"
	"github.com/Wayy01/Just-Dashboard/backend/internal/ghx"
	"github.com/Wayy01/Just-Dashboard/backend/internal/gitx"
	"github.com/Wayy01/Just-Dashboard/backend/internal/linuxusers"
	"github.com/Wayy01/Just-Dashboard/backend/internal/logsx"
	"github.com/Wayy01/Just-Dashboard/backend/internal/metrics"
	"github.com/Wayy01/Just-Dashboard/backend/internal/netsec"
	"github.com/Wayy01/Just-Dashboard/backend/internal/procs"
	"github.com/Wayy01/Just-Dashboard/backend/internal/proxysvc"
	"github.com/Wayy01/Just-Dashboard/backend/internal/selfupdate"
	"github.com/Wayy01/Just-Dashboard/backend/internal/sysinfo"
	"github.com/Wayy01/Just-Dashboard/backend/internal/term"
	"github.com/Wayy01/Just-Dashboard/backend/internal/updates"
	"github.com/Wayy01/Just-Dashboard/backend/internal/version"
)

// moduleSet holds the feature backends. Each is optional: a host without
// Docker, systemd or PM2 still serves everything else, and the corresponding
// routes report a precise "unavailable on this host" error instead.
type moduleSet struct {
	sys          *sysinfo.Collector
	metrics      *metrics.Recorder
	docker       *dockerx.Client
	dockerStats  *dockerx.StatsSampler
	dockerEvents *dockerx.EventLog
	pm2          *procs.PM2
	systemd      *procs.Systemd
	table        *procs.Table
	cron         *procs.Cron
	logs         *logsx.Service
	term         *term.Manager
	files        *files.Service
	git          *gitx.Service
	github       *ghx.Service
	updates      *updates.Service
	selfUpdate   *selfupdate.Service
	proxy        *proxysvc.Service
	dbs          *dbx.Manager
	linuxUsers   *linuxusers.Service
	netsec       *netsec.Service
	backupStore  *backups.Store
	backupRunner *backups.Runner
	backupSched  *backups.Scheduler
	deployStore  *deploy.Store
	deployer     *deploy.Deployer
}

func (s *Server) initModules() {
	s.modules.sys = sysinfo.NewCollector()
	s.modules.docker = dockerx.New(s.Cfg.DockerHost)
	s.modules.dockerStats = s.modules.docker.NewStatsSampler()
	s.modules.dockerEvents = s.modules.docker.NewEventLog(s.Log)
	// The recorder gets a sampler of its own rather than the shared one: a
	// series kept for a week is worth measuring over even intervals, and an
	// operator refreshing the container table would otherwise keep shortening
	// the window the next stored sample is differenced over.
	s.modules.metrics = metrics.New(s.Store, s.Log, s.Cfg.MetricsInterval, s.Cfg.MetricsRetention).
		WithContainers(s.modules.docker.NewStatsSampler())
	// Docker's disk accounting walks every layer and volume, which takes
	// seconds on a busy host. Priming it here means the first operator to open
	// the Volumes tab reads a warm cache instead of waiting for the walk.
	s.modules.docker.WarmCaches()
	s.modules.pm2 = procs.NewPM2()
	s.modules.systemd = procs.NewSystemd()
	s.modules.table = procs.NewTable()
	s.modules.cron = procs.NewCron()
	s.modules.logs = logsx.New(s.Cfg.LogRoots)
	s.modules.term = term.NewManager(s.Cfg.TerminalEnable, s.Cfg.TerminalShell, s.Cfg.TerminalUser)
	s.modules.files = files.New(s.Cfg.FileRoots)
	s.modules.git = gitx.New(s.Cfg.GitRoots)
	s.modules.github = ghx.New()
	s.modules.updates = updates.New()
	// The one module that manages the dashboard rather than the server. It is
	// given a *function* for listing containers rather than the Docker client,
	// so that working out which container this dashboard is — the part with
	// all the decisions in it — is testable without a daemon.
	s.modules.selfUpdate = selfupdate.New(selfupdate.Options{
		Current:     version.Version,
		DataDir:     s.Cfg.DataDir,
		UpdateDir:   s.Cfg.UpdateDir,
		Repo:        s.Cfg.UpdateRepo,
		Ref:         s.Cfg.UpdateBranch,
		CheckOnline: s.Cfg.UpdateCheck,
		DockerHost:  s.Cfg.DockerHost,
		Health:      s.healthURL(),
		List:        s.listSiblings,
		Log:         s.Log,
	})
	s.modules.proxy = proxysvc.New(s.Cfg.NginxDir, s.Cfg.CaddyFile)
	s.modules.dbs = dbx.NewManager()
	s.modules.linuxUsers = linuxusers.New()
	s.modules.netsec = netsec.New()

	s.modules.backupStore = backups.NewStore(s.Store, s.Sealer)
	s.modules.backupRunner = backups.NewRunner(s.modules.backupStore,
		filepath.Join(s.Cfg.DataDir, "staging"), s.Log)
	s.modules.backupSched = backups.NewScheduler(s.modules.backupStore, s.modules.backupRunner, s.Log)

	s.modules.deployStore = deploy.NewStore(s.Store, s.Sealer, s.Cfg.DeployRoots)
	s.modules.deployer = deploy.NewDeployer(s.modules.deployStore, s.Log)
}

// listSiblings is how internal/selfupdate sees this host's containers.
//
// All of them, not just the running ones: the updater container it is looking
// for may have exited, and "the container is gone" and "the container failed"
// are the two answers that decide whether an upgrade is reported as finished
// or as abandoned.
func (s *Server) listSiblings(ctx context.Context) ([]selfupdate.Sibling, error) {
	list, err := s.modules.docker.ListContainers(ctx, true)
	if err != nil {
		return nil, err
	}
	out := make([]selfupdate.Sibling, 0, len(list))
	for _, c := range list {
		sib := selfupdate.Sibling{
			ID: c.ID, Name: c.Name, Image: c.Image, State: c.State,
			Project: c.ComposeStack, Service: c.ComposeSvc,
			WorkDir: c.Labels["com.docker.compose.project.working_dir"],
		}
		for _, f := range strings.Split(c.Labels["com.docker.compose.project.config_files"], ",") {
			if f = strings.TrimSpace(f); f != "" {
				sib.ConfigFiles = append(sib.ConfigFiles, f)
			}
		}
		for _, m := range c.Mounts {
			sib.Mounts = append(sib.Mounts, selfupdate.Mount{Source: m.Source, Destination: m.Destination})
		}
		out = append(out, sib)
	}
	return out, nil
}

// healthURL is the address the rebuilt dashboard will answer on, which the
// updater probes before calling an upgrade finished.
//
// It is derived from the address this server binds because that is the address
// the next one will bind: the updater is given no configuration of its own, so
// anything it needs to know travels in the job record. A wildcard bind is
// rewritten to loopback — the updater runs on the host network, and asking
// 0.0.0.0 for a page is not a question.
func (s *Server) healthURL() string {
	addr := s.Cfg.Addr
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return ""
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	scheme := "http"
	if s.Cfg.AgentMode {
		// An agent serves TLS on this same address. The certificate is
		// self-signed by design, so the probe there is about liveness only.
		scheme = "https"
	}
	return scheme + "://" + net.JoinHostPort(host, port) + "/healthz"
}

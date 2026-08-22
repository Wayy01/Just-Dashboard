package api

import (
	"path/filepath"

	"github.com/Wayy01/Just-Dashboard/backend/internal/backups"
	"github.com/Wayy01/Just-Dashboard/backend/internal/dbx"
	"github.com/Wayy01/Just-Dashboard/backend/internal/deploy"
	"github.com/Wayy01/Just-Dashboard/backend/internal/dockerx"
	"github.com/Wayy01/Just-Dashboard/backend/internal/files"
	"github.com/Wayy01/Just-Dashboard/backend/internal/gitx"
	"github.com/Wayy01/Just-Dashboard/backend/internal/linuxusers"
	"github.com/Wayy01/Just-Dashboard/backend/internal/logsx"
	"github.com/Wayy01/Just-Dashboard/backend/internal/metrics"
	"github.com/Wayy01/Just-Dashboard/backend/internal/netsec"
	"github.com/Wayy01/Just-Dashboard/backend/internal/procs"
	"github.com/Wayy01/Just-Dashboard/backend/internal/proxysvc"
	"github.com/Wayy01/Just-Dashboard/backend/internal/sysinfo"
	"github.com/Wayy01/Just-Dashboard/backend/internal/term"
	"github.com/Wayy01/Just-Dashboard/backend/internal/updates"
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
	updates      *updates.Service
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
	s.modules.updates = updates.New()
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

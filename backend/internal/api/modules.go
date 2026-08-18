package api

import (
	"path/filepath"

	"github.com/Wayy01/vps-dashboard/backend/internal/backups"
	"github.com/Wayy01/vps-dashboard/backend/internal/dbx"
	"github.com/Wayy01/vps-dashboard/backend/internal/deploy"
	"github.com/Wayy01/vps-dashboard/backend/internal/dockerx"
	"github.com/Wayy01/vps-dashboard/backend/internal/files"
	"github.com/Wayy01/vps-dashboard/backend/internal/linuxusers"
	"github.com/Wayy01/vps-dashboard/backend/internal/logsx"
	"github.com/Wayy01/vps-dashboard/backend/internal/netsec"
	"github.com/Wayy01/vps-dashboard/backend/internal/procs"
	"github.com/Wayy01/vps-dashboard/backend/internal/proxysvc"
	"github.com/Wayy01/vps-dashboard/backend/internal/sysinfo"
	"github.com/Wayy01/vps-dashboard/backend/internal/term"
)

// moduleSet holds the feature backends. Each is optional: a host without
// Docker, systemd or PM2 still serves everything else, and the corresponding
// routes report a precise "unavailable on this host" error instead.
type moduleSet struct {
	sys          *sysinfo.Collector
	docker       *dockerx.Client
	pm2          *procs.PM2
	systemd      *procs.Systemd
	table        *procs.Table
	cron         *procs.Cron
	logs         *logsx.Service
	term         *term.Manager
	files        *files.Service
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
	// Docker's disk accounting walks every layer and volume, which takes
	// seconds on a busy host. Priming it here means the first operator to open
	// the Volumes tab reads a warm cache instead of waiting for the walk.
	s.modules.docker.WarmCaches()
	s.modules.pm2 = procs.NewPM2()
	s.modules.systemd = procs.NewSystemd()
	s.modules.table = procs.NewTable()
	s.modules.cron = procs.NewCron()
	s.modules.logs = logsx.New(s.Cfg.LogRoots)
	s.modules.term = term.NewManager(s.Cfg.TerminalEnable, s.Cfg.TerminalShell)
	s.modules.files = files.New(s.Cfg.FileRoots)
	s.modules.proxy = proxysvc.New(s.Cfg.NginxDir, s.Cfg.CaddyFile)
	s.modules.dbs = dbx.NewManager()
	s.modules.linuxUsers = linuxusers.New()
	s.modules.netsec = netsec.New()

	s.modules.backupStore = backups.NewStore(s.Store, s.Sealer)
	s.modules.backupRunner = backups.NewRunner(s.modules.backupStore,
		filepath.Join(s.Cfg.DataDir, "staging"), s.Log)
	s.modules.backupSched = backups.NewScheduler(s.modules.backupStore, s.modules.backupRunner, s.Log)

	s.modules.deployStore = deploy.NewStore(s.Store, s.Sealer)
	s.modules.deployer = deploy.NewDeployer(s.modules.deployStore, s.Log)
}

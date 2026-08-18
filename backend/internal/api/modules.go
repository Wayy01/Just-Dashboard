package api

import (
	"github.com/Wayy01/vps-dashboard/backend/internal/dbx"
	"github.com/Wayy01/vps-dashboard/backend/internal/dockerx"
	"github.com/Wayy01/vps-dashboard/backend/internal/files"
	"github.com/Wayy01/vps-dashboard/backend/internal/logsx"
	"github.com/Wayy01/vps-dashboard/backend/internal/procs"
	"github.com/Wayy01/vps-dashboard/backend/internal/proxysvc"
	"github.com/Wayy01/vps-dashboard/backend/internal/sysinfo"
	"github.com/Wayy01/vps-dashboard/backend/internal/term"
	"github.com/go-chi/chi/v5"
)

// moduleSet holds the feature backends. Each is optional: a host without
// Docker, systemd or PM2 still serves everything else, and the corresponding
// routes report a precise "unavailable on this host" error instead.
type moduleSet struct {
	sys     *sysinfo.Collector
	docker  *dockerx.Client
	pm2     *procs.PM2
	systemd *procs.Systemd
	table   *procs.Table
	cron    *procs.Cron
	logs    *logsx.Service
	term    *term.Manager
	files   *files.Service
	proxy   *proxysvc.Service
	dbs     *dbx.Manager
}

func (s *Server) initModules() {
	s.modules.sys = sysinfo.NewCollector()
	s.modules.docker = dockerx.New(s.Cfg.DockerHost)
	s.modules.pm2 = procs.NewPM2()
	s.modules.systemd = procs.NewSystemd()
	s.modules.table = procs.NewTable()
	s.modules.cron = procs.NewCron()
	s.modules.logs = logsx.New(s.Cfg.LogRoots)
	s.modules.term = term.NewManager(s.Cfg.TerminalEnable, s.Cfg.TerminalShell)
	s.modules.files = files.New(s.Cfg.FileRoots)
	s.modules.proxy = proxysvc.New(s.Cfg.NginxDir, s.Cfg.CaddyFile)
	s.modules.dbs = dbx.NewManager()
}

func (s *Server) mountLinuxUserRoutes(r chi.Router) {}
func (s *Server) mountNetSecRoutes(r chi.Router)    {}
func (s *Server) mountBackupRoutes(r chi.Router)    {}
func (s *Server) mountDeployRoutes(r chi.Router)    {}

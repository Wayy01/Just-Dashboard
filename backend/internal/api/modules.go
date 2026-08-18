package api

import (
	"github.com/Wayy01/vps-dashboard/backend/internal/dockerx"
	"github.com/Wayy01/vps-dashboard/backend/internal/sysinfo"
	"github.com/go-chi/chi/v5"
)

// moduleSet holds the feature backends. Each is optional: a host without
// Docker, systemd or PM2 still serves everything else, and the corresponding
// routes report a precise "unavailable on this host" error instead.
type moduleSet struct {
	sys    *sysinfo.Collector
	docker *dockerx.Client
}

func (s *Server) initModules() {
	s.modules.sys = sysinfo.NewCollector()
	s.modules.docker = dockerx.New(s.Cfg.DockerHost)
}

func (s *Server) mountProcessRoutes(r chi.Router)   {}
func (s *Server) mountLogRoutes(r chi.Router)       {}
func (s *Server) mountTerminalRoutes(r chi.Router)  {}
func (s *Server) mountFileRoutes(r chi.Router)      {}
func (s *Server) mountProxyRoutes(r chi.Router)     {}
func (s *Server) mountDatabaseRoutes(r chi.Router)  {}
func (s *Server) mountLinuxUserRoutes(r chi.Router) {}
func (s *Server) mountNetSecRoutes(r chi.Router)    {}
func (s *Server) mountBackupRoutes(r chi.Router)    {}
func (s *Server) mountDeployRoutes(r chi.Router)    {}

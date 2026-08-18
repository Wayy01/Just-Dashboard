package api

import "github.com/go-chi/chi/v5"

// moduleSet holds the feature backends. Each is optional: a host without
// Docker, systemd or PM2 still serves everything else, and the corresponding
// routes report a precise "unavailable on this host" error instead.
type moduleSet struct{}

func (s *Server) initModules() {}

func (s *Server) mountSystemRoutes(r chi.Router)    {}
func (s *Server) mountDockerRoutes(r chi.Router)    {}
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

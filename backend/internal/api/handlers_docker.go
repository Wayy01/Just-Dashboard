package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wayy01/Just-Dashboard/backend/internal/auth"
	"github.com/Wayy01/Just-Dashboard/backend/internal/dockerx"
	"github.com/Wayy01/Just-Dashboard/backend/internal/httpx"
	"github.com/go-chi/chi/v5"
)

func (s *Server) mountDockerRoutes(r chi.Router) {
	r.Route("/docker", func(r chi.Router) {
		r.Method(http.MethodGet, "/ping", s.handle(s.handleDockerPing))
		r.Method(http.MethodGet, "/info", s.handle(s.handleDockerInfo))
		r.Method(http.MethodGet, "/disk-usage", s.handle(s.handleDockerDiskUsage))

		r.Route("/containers", func(r chi.Router) {
			r.Method(http.MethodGet, "/", s.handle(s.handleContainerList))
			r.Method(http.MethodGet, "/stats", s.handle(s.handleContainerStatsAll))
			r.Method(http.MethodGet, "/stream", s.handle(s.handleContainerStream))
			r.Method(http.MethodGet, "/{id}", s.handle(s.handleContainerInspect))
			r.Method(http.MethodGet, "/{id}/logs", s.handle(s.handleContainerLogs))
			r.Method(http.MethodGet, "/{id}/logs/stream", s.handle(s.handleContainerLogStream))
			r.Method(http.MethodGet, "/{id}/stats/stream", s.handle(s.handleContainerStatStream))

			r.Group(func(r chi.Router) {
				r.Use(httpx.RequireCapability(auth.CapServiceControl))
				r.Method(http.MethodPost, "/{id}/start", s.handle(s.containerLifecycle(dockerx.ActionStart)))
				r.Method(http.MethodPost, "/{id}/pause", s.handle(s.containerLifecycle(dockerx.ActionPause)))
				r.Method(http.MethodPost, "/{id}/unpause", s.handle(s.containerLifecycle(dockerx.ActionUnpause)))
			})
			// Stop, restart and kill interrupt a running service, so they
			// carry the typed-confirmation requirement even though the
			// container itself survives.
			s.destructive(r, func(r chi.Router) {
				r.Method(http.MethodPost, "/{id}/stop", s.handle(s.containerLifecycle(dockerx.ActionStop)))
				r.Method(http.MethodPost, "/{id}/restart", s.handle(s.containerLifecycle(dockerx.ActionRestart)))
				r.Method(http.MethodPost, "/{id}/kill", s.handle(s.containerLifecycle(dockerx.ActionKill)))
				r.Method(http.MethodDelete, "/{id}", s.handle(s.handleContainerRemove))
				r.Method(http.MethodPost, "/prune", s.handle(s.handleContainerPrune))
			})
			r.Group(func(r chi.Router) {
				r.Use(httpx.RequireCapability(auth.CapTerminal))
				r.Method(http.MethodGet, "/{id}/exec", s.handle(s.handleContainerExec))
			})
		})

		r.Route("/images", func(r chi.Router) {
			r.Method(http.MethodGet, "/", s.handle(s.handleImageList))
			r.Group(func(r chi.Router) {
				r.Use(httpx.RequireCapability(auth.CapServiceControl))
				r.Method(http.MethodGet, "/pull", s.handle(s.handleImagePull))
			})
			s.destructive(r, func(r chi.Router) {
				r.Method(http.MethodDelete, "/{id}", s.handle(s.handleImageRemove))
				r.Method(http.MethodPost, "/prune", s.handle(s.handleImagePrune))
			})
		})

		r.Route("/volumes", func(r chi.Router) {
			r.Method(http.MethodGet, "/", s.handle(s.handleVolumeList))
			r.Method(http.MethodGet, "/{name}", s.handle(s.handleVolumeInspect))
			s.destructive(r, func(r chi.Router) {
				r.Method(http.MethodDelete, "/{name}", s.handle(s.handleVolumeRemove))
				r.Method(http.MethodPost, "/prune", s.handle(s.handleVolumePrune))
			})
		})

		r.Route("/networks", func(r chi.Router) {
			r.Method(http.MethodGet, "/", s.handle(s.handleNetworkList))
			r.Method(http.MethodGet, "/{id}", s.handle(s.handleNetworkInspect))
			s.destructive(r, func(r chi.Router) {
				r.Method(http.MethodDelete, "/{id}", s.handle(s.handleNetworkRemove))
			})
		})

		r.Route("/stacks", func(r chi.Router) {
			r.Method(http.MethodGet, "/", s.handle(s.handleStackList))
			r.Method(http.MethodGet, "/{name}/config", s.handle(s.handleStackConfig))
			r.Group(func(r chi.Router) {
				r.Use(httpx.RequireCapability(auth.CapServiceControl))
				r.Method(http.MethodPost, "/{name}/up", s.handle(s.stackAction(dockerx.ComposeUp)))
				r.Method(http.MethodPost, "/{name}/start", s.handle(s.stackAction(dockerx.ComposeStart)))
				r.Method(http.MethodPost, "/{name}/pull", s.handle(s.stackAction(dockerx.ComposePull)))
			})
			s.destructive(r, func(r chi.Router) {
				r.Method(http.MethodPost, "/{name}/down", s.handle(s.stackAction(dockerx.ComposeDown)))
				r.Method(http.MethodPost, "/{name}/stop", s.handle(s.stackAction(dockerx.ComposeStop)))
				r.Method(http.MethodPost, "/{name}/restart", s.handle(s.stackAction(dockerx.ComposeRestart)))
			})
		})

		s.destructive(r, func(r chi.Router) {
			r.Method(http.MethodPost, "/prune", s.handle(s.handlePruneAll))
		})
	})
}

func (s *Server) dockerErr(err error) error {
	if errors.Is(err, dockerx.ErrUnavailable) {
		return httpx.Err(http.StatusServiceUnavailable, "docker_unavailable", err.Error())
	}
	if strings.Contains(strings.ToLower(err.Error()), "no such") {
		return httpx.ErrNotFound
	}
	return httpx.Wrap(http.StatusBadGateway, "docker_error", err)
}

func (s *Server) handleDockerPing(w http.ResponseWriter, r *http.Request) error {
	httpx.JSON(w, http.StatusOK, s.modules.docker.Ping(r.Context()))
	return nil
}

func (s *Server) handleDockerInfo(w http.ResponseWriter, r *http.Request) error {
	info, err := s.modules.docker.Info(r.Context())
	if err != nil {
		return s.dockerErr(err)
	}
	httpx.JSON(w, http.StatusOK, info)
	return nil
}

func (s *Server) handleDockerDiskUsage(w http.ResponseWriter, r *http.Request) error {
	du, err := s.modules.docker.DiskUsage(r.Context())
	if err != nil {
		return s.dockerErr(err)
	}
	httpx.JSON(w, http.StatusOK, du)
	return nil
}

func (s *Server) handleContainerList(w http.ResponseWriter, r *http.Request) error {
	all := r.URL.Query().Get("all") != "false"
	list, err := s.modules.docker.ListContainers(r.Context(), all)
	if err != nil {
		return s.dockerErr(err)
	}
	httpx.JSON(w, http.StatusOK, list)
	return nil
}

func (s *Server) handleContainerInspect(w http.ResponseWriter, r *http.Request) error {
	detail, err := s.modules.docker.Inspect(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		return s.dockerErr(err)
	}
	// Every authenticated role may read container detail, and a container's
	// environment routinely holds the master key, database passwords and
	// deploy credentials. Anyone below system.admin gets them redacted here,
	// on the server, so a readonly API token cannot lift them either.
	//
	// This raises the cost of reading a secret; it does not make it
	// impossible, and it was never meant to be read as though it did. Below
	// system.admin the same values are still reachable through the compose
	// file at /docker/stacks/{name}/config, through /files/read within
	// JD_FILE_ROOTS, and through a deploy run's log. Every role sees
	// everything on this box by design — see the roles table in the README —
	// and narrowing that is a decision about the product, not about this
	// handler.
	if !httpx.MustPrincipal(r).Can(auth.CapSystemAdmin) {
		detail.Env = dockerx.RedactEnv(detail.Env)
	}
	httpx.JSON(w, http.StatusOK, detail)
	return nil
}

func (s *Server) handleContainerStatsAll(w http.ResponseWriter, r *http.Request) error {
	list, err := s.modules.docker.ListContainers(r.Context(), false)
	if err != nil {
		return s.dockerErr(err)
	}
	ids := make([]string, 0, len(list))
	for _, c := range list {
		if c.State == "running" {
			ids = append(ids, c.ID)
		}
	}
	stats, err := s.modules.docker.StatsOnce(r.Context(), ids)
	if err != nil {
		return s.dockerErr(err)
	}
	httpx.JSON(w, http.StatusOK, stats)
	return nil
}

// handleContainerStream is the `docker stats`-equivalent feed backing the
// container table: one socket, one sampling loop, all running containers.
func (s *Server) handleContainerStream(w http.ResponseWriter, r *http.Request) error {
	conn, err := s.WS.Upgrade(w, r)
	if err != nil {
		return nil
	}
	defer conn.Close()
	ctx, cancel := contextWithCancel(r)
	defer cancel()
	go conn.Keepalive(ctx)
	go conn.DrainControl(cancel)

	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		list, err := s.modules.docker.ListContainers(ctx, true)
		if err != nil {
			conn.SendError(err.Error())
			return nil
		}
		if err := conn.Send("containers", list); err != nil {
			return nil
		}
		ids := make([]string, 0, len(list))
		for _, c := range list {
			if c.State == "running" {
				ids = append(ids, c.ID)
			}
		}
		if stats, err := s.modules.docker.StatsOnce(ctx, ids); err == nil {
			if err := conn.Send("stats", stats); err != nil {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
	}
}

func (s *Server) handleContainerLogs(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	ch, closer, err := s.modules.docker.Logs(r.Context(), chi.URLParam(r, "id"), dockerx.LogOptions{
		Tail:       defaultStr(q.Get("tail"), "500"),
		Since:      q.Get("since"),
		Until:      q.Get("until"),
		Timestamps: q.Get("timestamps") == "true",
	})
	if err != nil {
		return s.dockerErr(err)
	}
	defer closer.Close()
	lines := []dockerx.LogLine{}
	for line := range ch {
		lines = append(lines, line)
		if len(lines) >= 20000 {
			break
		}
	}
	httpx.JSON(w, http.StatusOK, lines)
	return nil
}

func (s *Server) handleContainerLogStream(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	conn, err := s.WS.Upgrade(w, r)
	if err != nil {
		return nil
	}
	defer conn.Close()
	ctx, cancel := contextWithCancel(r)
	defer cancel()
	go conn.Keepalive(ctx)
	go conn.DrainControl(cancel)

	ch, closer, err := s.modules.docker.Logs(ctx, chi.URLParam(r, "id"), dockerx.LogOptions{
		Tail:       defaultStr(q.Get("tail"), "500"),
		Timestamps: q.Get("timestamps") == "true",
		Follow:     true,
	})
	if err != nil {
		conn.SendError(err.Error())
		return nil
	}
	defer closer.Close()

	// Lines are batched on a short tick: a chatty container can emit
	// thousands per second, and one frame each would drown the browser.
	batch := make([]dockerx.LogLine, 0, 256)
	flush := time.NewTicker(150 * time.Millisecond)
	defer flush.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case line, ok := <-ch:
			if !ok {
				if len(batch) > 0 {
					conn.Send("logs", batch)
				}
				conn.Send("eof", nil)
				return nil
			}
			batch = append(batch, line)
			if len(batch) >= 256 {
				if err := conn.Send("logs", batch); err != nil {
					return nil
				}
				batch = batch[:0]
			}
		case <-flush.C:
			if len(batch) > 0 {
				if err := conn.Send("logs", batch); err != nil {
					return nil
				}
				batch = batch[:0]
			}
		}
	}
}

func (s *Server) handleContainerStatStream(w http.ResponseWriter, r *http.Request) error {
	conn, err := s.WS.Upgrade(w, r)
	if err != nil {
		return nil
	}
	defer conn.Close()
	ctx, cancel := contextWithCancel(r)
	defer cancel()
	go conn.Keepalive(ctx)
	go conn.DrainControl(cancel)

	out := make(chan dockerx.ContainerStats, 8)
	go func() {
		defer close(out)
		if err := s.modules.docker.StatsStream(ctx, chi.URLParam(r, "id"), out); err != nil {
			conn.SendError(err.Error())
		}
	}()
	for st := range out {
		if err := conn.Send("stats", st); err != nil {
			return nil
		}
	}
	return nil
}

func (s *Server) containerLifecycle(action dockerx.LifecycleAction) httpx.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		id := chi.URLParam(r, "id")
		detail, err := s.modules.docker.Inspect(r.Context(), id)
		if err != nil {
			return s.dockerErr(err)
		}
		// Confirmation is keyed on the container's name, so a mis-clicked row
		// cannot be confirmed by muscle memory from the previous dialog.
		if action == dockerx.ActionStop || action == dockerx.ActionRestart || action == dockerx.ActionKill {
			if err := httpx.RequireTypedConfirmation(w, r, detail.Name); err != nil {
				return err
			}
		}
		var timeout *int
		if v := r.URL.Query().Get("timeout"); v != "" {
			t := atoiDefault(v, 10)
			timeout = &t
		}
		if err := s.modules.docker.Lifecycle(r.Context(), id, action, timeout); err != nil {
			return s.dockerErr(err)
		}
		httpx.SetAudit(r, "docker.container."+string(action), detail.Name,
			map[string]any{"id": id, "image": detail.Image})
		httpx.NoContent(w)
		return nil
	}
}

func (s *Server) handleContainerRemove(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	detail, err := s.modules.docker.Inspect(r.Context(), id)
	if err != nil {
		return s.dockerErr(err)
	}
	if err := httpx.RequireTypedConfirmation(w, r, detail.Name); err != nil {
		return err
	}
	force := r.URL.Query().Get("force") == "true"
	volumes := r.URL.Query().Get("volumes") == "true"
	if err := s.modules.docker.RemoveContainer(r.Context(), id, force, volumes); err != nil {
		return s.dockerErr(err)
	}
	httpx.SetAudit(r, "docker.container.remove", detail.Name,
		map[string]any{"id": id, "force": force, "removeVolumes": volumes})
	httpx.NoContent(w)
	return nil
}

func (s *Server) handleContainerPrune(w http.ResponseWriter, r *http.Request) error {
	if err := httpx.RequireTypedConfirmation(w, r, "prune containers"); err != nil {
		return err
	}
	space, deleted, err := s.modules.docker.PruneContainers(r.Context())
	if err != nil {
		return s.dockerErr(err)
	}
	httpx.SetAudit(r, "docker.container.prune", "", map[string]any{"deleted": len(deleted), "reclaimed": space})
	httpx.JSON(w, http.StatusOK, dockerx.PruneReport{Kind: "containers", SpaceReclaimed: space, Items: deleted})
	return nil
}

// handleContainerExec bridges a browser terminal to a shell inside the
// container. This is root-equivalent access to the host in practice, so the
// session is logged the moment it opens rather than at close, when a crashed
// process might have swallowed the record.
func (s *Server) handleContainerExec(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	detail, err := s.modules.docker.Inspect(r.Context(), id)
	if err != nil {
		return s.dockerErr(err)
	}
	p := httpx.MustPrincipal(r)
	s.recordAudit(r, "docker.container.exec.open", detail.Name, map[string]any{"id": id})

	conn, err := s.WS.Upgrade(w, r)
	if err != nil {
		return nil
	}
	defer conn.Close()
	ctx, cancel := contextWithCancel(r)
	defer cancel()
	go conn.Keepalive(ctx)

	q := r.URL.Query()
	var cmd []string
	if c := q.Get("cmd"); c != "" {
		cmd = strings.Fields(c)
	}
	sess, err := s.modules.docker.Exec(ctx, id, cmd, q.Get("user"),
		uint(atoiDefault(q.Get("rows"), 24)), uint(atoiDefault(q.Get("cols"), 80)))
	if err != nil {
		conn.SendError(err.Error())
		return nil
	}
	defer sess.Close()

	go func() {
		buf := make([]byte, 8192)
		for {
			n, err := sess.Conn.Read(buf)
			if n > 0 {
				if err := conn.WriteBinary(buf[:n]); err != nil {
					cancel()
					return
				}
			}
			if err != nil {
				if err != io.EOF {
					conn.SendError(err.Error())
				}
				cancel()
				return
			}
		}
	}()

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			break
		}
		// Control frames are JSON; everything else is raw keystrokes. A
		// resize is the only control message an exec session needs.
		if len(data) > 0 && data[0] == '{' {
			var ctrl struct {
				Type string `json:"type"`
				Rows uint   `json:"rows"`
				Cols uint   `json:"cols"`
			}
			if json.Unmarshal(data, &ctrl) == nil && ctrl.Type == "resize" {
				sess.Resize(ctx, ctrl.Rows, ctrl.Cols)
				continue
			}
		}
		if _, err := sess.Conn.Write(data); err != nil {
			break
		}
	}
	s.recordAudit(r, "docker.container.exec.close", detail.Name,
		map[string]any{"id": id, "user": p.Username()})
	return nil
}

func (s *Server) handleImageList(w http.ResponseWriter, r *http.Request) error {
	list, err := s.modules.docker.ListImages(r.Context(), r.URL.Query().Get("all") == "true")
	if err != nil {
		return s.dockerErr(err)
	}
	httpx.JSON(w, http.StatusOK, list)
	return nil
}

// handleImagePull streams layer progress over a socket; a pull of a large
// image otherwise looks indistinguishable from a hung request.
func (s *Server) handleImagePull(w http.ResponseWriter, r *http.Request) error {
	ref := dockerx.ImageRef(r.URL.Query().Get("ref"))
	if ref == "" {
		return httpx.BadRequest("ref query parameter is required")
	}
	s.recordAudit(r, "docker.image.pull", ref, nil)

	conn, err := s.WS.Upgrade(w, r)
	if err != nil {
		return nil
	}
	defer conn.Close()
	ctx, cancel := contextWithCancel(r)
	defer cancel()
	go conn.Keepalive(ctx)
	go conn.DrainControl(cancel)

	out := make(chan dockerx.PullProgress, 32)
	done := make(chan error, 1)
	go func() { done <- s.modules.docker.PullImage(ctx, ref, out); close(out) }()
	for msg := range out {
		if err := conn.Send("progress", msg); err != nil {
			cancel()
			break
		}
	}
	if err := <-done; err != nil && ctx.Err() == nil {
		conn.SendError(err.Error())
		return nil
	}
	conn.Send("done", map[string]string{"ref": ref})
	return nil
}

func (s *Server) handleImageRemove(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	// Keyed on the image's own tag, for the reason containerLifecycle gives:
	// a fixed phrase like "delete image" carries no information about which
	// image, so deleting several in a row is exactly the muscle-memory case
	// the typed confirmation exists to prevent.
	if err := httpx.RequireTypedConfirmation(w, r, s.imageName(r.Context(), id)); err != nil {
		return err
	}
	res, err := s.modules.docker.RemoveImage(r.Context(), id,
		r.URL.Query().Get("force") == "true", true)
	if err != nil {
		return s.dockerErr(err)
	}
	httpx.SetAudit(r, "docker.image.remove", id, map[string]any{"result": res})
	httpx.JSON(w, http.StatusOK, res)
	return nil
}

func (s *Server) handleImagePrune(w http.ResponseWriter, r *http.Request) error {
	all := r.URL.Query().Get("all") == "true"
	phrase := "prune images"
	if all {
		phrase = "prune all images"
	}
	if err := httpx.RequireTypedConfirmation(w, r, phrase); err != nil {
		return err
	}
	rep, err := s.modules.docker.PruneImages(r.Context(), all)
	if err != nil {
		return s.dockerErr(err)
	}
	httpx.SetAudit(r, "docker.image.prune", "", map[string]any{"all": all, "reclaimed": rep.SpaceReclaimed})
	httpx.JSON(w, http.StatusOK, rep)
	return nil
}

func (s *Server) handleVolumeList(w http.ResponseWriter, r *http.Request) error {
	list, err := s.modules.docker.ListVolumes(r.Context())
	if err != nil {
		return s.dockerErr(err)
	}
	httpx.JSON(w, http.StatusOK, list)
	return nil
}

func (s *Server) handleVolumeInspect(w http.ResponseWriter, r *http.Request) error {
	v, err := s.modules.docker.InspectVolume(r.Context(), chi.URLParam(r, "name"))
	if err != nil {
		return s.dockerErr(err)
	}
	httpx.JSON(w, http.StatusOK, v)
	return nil
}

func (s *Server) handleVolumeRemove(w http.ResponseWriter, r *http.Request) error {
	name := chi.URLParam(r, "name")
	if err := httpx.RequireTypedConfirmation(w, r, name); err != nil {
		return err
	}
	if err := s.modules.docker.RemoveVolume(r.Context(), name, r.URL.Query().Get("force") == "true"); err != nil {
		return s.dockerErr(err)
	}
	httpx.SetAudit(r, "docker.volume.remove", name, nil)
	httpx.NoContent(w)
	return nil
}

func (s *Server) handleVolumePrune(w http.ResponseWriter, r *http.Request) error {
	if err := httpx.RequireTypedConfirmation(w, r, "prune volumes"); err != nil {
		return err
	}
	rep, err := s.modules.docker.PruneVolumes(r.Context())
	if err != nil {
		return s.dockerErr(err)
	}
	httpx.SetAudit(r, "docker.volume.prune", "", map[string]any{"deleted": rep.Items})
	httpx.JSON(w, http.StatusOK, rep)
	return nil
}

func (s *Server) handleNetworkList(w http.ResponseWriter, r *http.Request) error {
	list, err := s.modules.docker.ListNetworks(r.Context())
	if err != nil {
		return s.dockerErr(err)
	}
	httpx.JSON(w, http.StatusOK, list)
	return nil
}

func (s *Server) handleNetworkInspect(w http.ResponseWriter, r *http.Request) error {
	n, err := s.modules.docker.InspectNetwork(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		return s.dockerErr(err)
	}
	httpx.JSON(w, http.StatusOK, n)
	return nil
}

func (s *Server) handleNetworkRemove(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	n, err := s.modules.docker.InspectNetwork(r.Context(), id)
	if err != nil {
		return s.dockerErr(err)
	}
	if err := httpx.RequireTypedConfirmation(w, r, n.Name); err != nil {
		return err
	}
	if err := s.modules.docker.RemoveNetwork(r.Context(), id); err != nil {
		return s.dockerErr(err)
	}
	httpx.SetAudit(r, "docker.network.remove", id, nil)
	httpx.NoContent(w)
	return nil
}

// imageName is the phrase an operator types to confirm removing an image: its
// first tag, or a short id for one that was never tagged. It falls back to the
// id it was given rather than failing, so a confirmation is always possible.
func (s *Server) imageName(ctx context.Context, id string) string {
	images, err := s.modules.docker.ListImages(ctx, true)
	if err != nil {
		return shortID(id)
	}
	for _, img := range images {
		if img.ID != id && !strings.HasPrefix(img.ID, id) {
			continue
		}
		if len(img.RepoTags) > 0 && img.RepoTags[0] != "<none>:<none>" {
			return img.RepoTags[0]
		}
		return shortID(img.ID)
	}
	return shortID(id)
}

func shortID(id string) string {
	id = strings.TrimPrefix(id, "sha256:")
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func (s *Server) handleStackList(w http.ResponseWriter, r *http.Request) error {
	list, err := s.modules.docker.ListStacks(r.Context(), s.Cfg.ComposeRoots)
	if err != nil {
		return s.dockerErr(err)
	}
	httpx.JSON(w, http.StatusOK, list)
	return nil
}

func (s *Server) handleStackConfig(w http.ResponseWriter, r *http.Request) error {
	stack, err := s.findStack(r, chi.URLParam(r, "name"))
	if err != nil {
		return err
	}
	if len(stack.ConfigFiles) == 0 {
		return httpx.ErrNotFound
	}
	content, err := dockerx.ReadComposeFile(stack.ConfigFiles[0])
	if err != nil {
		return httpx.Wrap(http.StatusInternalServerError, "read_failed", err)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"path": stack.ConfigFiles[0], "content": content})
	return nil
}

func (s *Server) findStack(r *http.Request, name string) (*dockerx.ComposeStack, error) {
	stacks, err := s.modules.docker.ListStacks(r.Context(), s.Cfg.ComposeRoots)
	if err != nil {
		return nil, s.dockerErr(err)
	}
	for i := range stacks {
		if stacks[i].Name == name {
			return &stacks[i], nil
		}
	}
	return nil, httpx.ErrNotFound
}

func (s *Server) stackAction(action dockerx.ComposeAction) httpx.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		name := chi.URLParam(r, "name")
		stack, err := s.findStack(r, name)
		if err != nil {
			return err
		}
		if !stack.Managed {
			return httpx.BadRequest("stack %q has no compose file on disk that this dashboard can reach", name)
		}
		if action == dockerx.ComposeDown || action == dockerx.ComposeStop || action == dockerx.ComposeRestart {
			if err := httpx.RequireTypedConfirmation(w, r, name); err != nil {
				return err
			}
		}
		res, err := s.modules.docker.RunCompose(r.Context(), stack.WorkingDir, action, r.URL.Query().Get("service"))
		if err != nil {
			return httpx.Wrap(http.StatusBadGateway, "compose_failed", err)
		}
		httpx.SetAudit(r, "docker.stack."+string(action), name,
			map[string]any{"exitCode": res.ExitCode, "dir": stack.WorkingDir})
		status := http.StatusOK
		if res.ExitCode != 0 {
			status = http.StatusBadGateway
		}
		httpx.JSON(w, status, res)
		return nil
	}
}

func (s *Server) handlePruneAll(w http.ResponseWriter, r *http.Request) error {
	if err := httpx.RequireTypedConfirmation(w, r, "prune everything"); err != nil {
		return err
	}
	includeVolumes := r.URL.Query().Get("volumes") == "true"
	allImages := r.URL.Query().Get("allImages") == "true"
	reports, err := s.modules.docker.PruneAll(r.Context(), includeVolumes, allImages)
	if err != nil {
		return s.dockerErr(err)
	}
	httpx.SetAudit(r, "docker.prune.all", "",
		map[string]any{"volumes": includeVolumes, "allImages": allImages, "reports": reports})
	httpx.JSON(w, http.StatusOK, reports)
	return nil
}

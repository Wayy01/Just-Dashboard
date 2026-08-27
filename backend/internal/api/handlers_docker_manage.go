package api

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Wayy01/Just-Dashboard/backend/internal/auth"
	"github.com/Wayy01/Just-Dashboard/backend/internal/dockerx"
	"github.com/Wayy01/Just-Dashboard/backend/internal/httpx"
	"github.com/Wayy01/Just-Dashboard/backend/internal/proxysvc"
	"github.com/go-chi/chi/v5"
)

// The write half of the Docker surface: creating containers, volumes and
// networks, editing and running compose stacks, building images, and the
// streaming endpoints that let an operator watch any of it happen.
//
// Everything here goes through the same chain as the rest of the API and adds
// two rules of its own, both because the Docker socket is root-equivalent:
//
//   - A container spec that is privileged, or that mounts a path from the
//     host, needs system.admin on top of service.control. Those two settings
//     are the documented way to turn "may run a container" into "owns the
//     server", and unlike the other fields there is no way to grant one
//     without the other. This is a check on the *content* of a request rather
//     than on its route, which the codebase already does once — dbx.Classify
//     deciding whether a SQL statement is destructive — and for the same
//     reason: the route cannot know.
//   - Every path a client supplies goes through files.Resolve, including the
//     ones that do not look like file operations: a bind-mount source, a
//     build context, a new stack's directory.

// containerSpecRequest is the create body. The spec is the whole of it; the
// wrapper exists so options that are about the request rather than the
// container (whether to wait for a pull) have somewhere to live later.
type containerSpecRequest struct {
	dockerx.ContainerSpec
}

func (s *Server) handleContainerCreate(w http.ResponseWriter, r *http.Request) error {
	var req containerSpecRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	spec := req.ContainerSpec
	if err := s.authoriseSpec(r, &spec); err != nil {
		return err
	}

	// A pull can take minutes on a slow link, and the browser has already
	// been shown layer progress over the pull socket if it asked for it.
	ctx, cancel := timeoutCtx(r, 15*time.Minute)
	defer cancel()

	res, err := s.modules.docker.Create(ctx, spec, nil)
	if err != nil {
		if errors.Is(err, dockerx.ErrNameTaken) {
			return httpx.Err(http.StatusConflict, "name_taken", err.Error())
		}
		return s.dockerErr(err)
	}
	httpx.SetAudit(r, "docker.container.create", res.Name, map[string]any{
		"image": spec.Image, "id": res.ID, "started": res.Started,
	})
	httpx.JSON(w, http.StatusCreated, res)
	return nil
}

// authoriseSpec applies the content-dependent capability check and resolves
// every host path the spec names.
//
// Bind mounts are resolved through files.Resolve rather than merely inspected:
// JD_FILE_ROOTS is what the operator configured as "the parts of this server
// the dashboard may touch", and a bind mount is the dashboard handing a
// container a piece of the filesystem. A deployment that narrowed the roots
// deliberately would otherwise find the restriction bypassable by mounting the
// same path into a container instead.
func (s *Server) authoriseSpec(r *http.Request, spec *dockerx.ContainerSpec) error {
	p := httpx.MustPrincipal(r)
	admin := p.Can(auth.CapSystemAdmin)

	if spec.Privileged && !admin {
		return httpx.Err(http.StatusForbidden, "requires_admin",
			"a privileged container can reach every device on the server and is equivalent to root on the host, so creating one needs an administrator")
	}
	if len(spec.CapAdd) > 0 && !admin {
		return httpx.Err(http.StatusForbidden, "requires_admin",
			"adding Linux capabilities to a container needs an administrator")
	}
	if len(spec.Devices) > 0 && !admin {
		return httpx.Err(http.StatusForbidden, "requires_admin",
			"giving a container a device from the server needs an administrator")
	}
	if spec.NetworkMode == "host" && !admin {
		return httpx.Err(http.StatusForbidden, "requires_admin",
			"a container on the host's network shares every port on the server, so it needs an administrator")
	}

	for i, m := range spec.Mounts {
		if m.Type != "bind" {
			continue
		}
		if !admin {
			return httpx.Err(http.StatusForbidden, "requires_admin",
				"mounting a folder from the server into a container needs an administrator. A managed volume does not, and is the better choice for data the container owns.")
		}
		// Checked before resolving: files.Resolve reads an empty path as the
		// first configured root, which would turn a blank field into a mount
		// of the whole of /srv — or of / on a default install.
		if !strings.HasPrefix(strings.TrimSpace(m.Source), "/") {
			return httpx.BadRequest("a folder to mount has to be an absolute path on the server")
		}
		resolved, err := s.modules.files.Resolve(m.Source)
		if err != nil {
			return httpx.BadRequest("%s is outside the paths this dashboard may reach", m.Source)
		}
		spec.Mounts[i].Source = resolved
	}
	return nil
}

// handleContainerPreview renders a spec as the command and the compose file
// that would produce it, without creating anything.
//
// The point is that the form is not a black box. Someone who has never run a
// container reads their own choices back as the line they would otherwise have
// had to type; someone who has been doing this for years checks the dashboard's
// arithmetic before letting it near their server. It is also how "save this as
// a stack" is built — the compose half is the file that gets written.
func (s *Server) handleContainerPreview(w http.ResponseWriter, r *http.Request) error {
	var req containerSpecRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	spec := req.ContainerSpec
	// Not authorised here: nothing is created, and refusing to *show* someone
	// the command for a privileged container while the create route explains
	// why it is refused would be a worse experience than showing both.
	httpx.SkipAudit(r)
	httpx.JSON(w, http.StatusOK, map[string]any{
		"run":     spec.RunCommand(),
		"compose": spec.ComposeService(spec.Name),
	})
	return nil
}

func (s *Server) handleContainerSpec(w http.ResponseWriter, r *http.Request) error {
	spec, err := s.modules.docker.SpecOf(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		return s.dockerErr(err)
	}
	// The same rule as container inspect: a spec carries the environment, and
	// the environment carries credentials.
	if !httpx.MustPrincipal(r).Can(auth.CapSystemAdmin) {
		for i, e := range spec.Env {
			if dockerx.IsSecretEnvKey(e.Name) {
				spec.Env[i].Value = dockerx.RedactedEnvValue
			}
		}
	}
	httpx.JSON(w, http.StatusOK, spec)
	return nil
}

func (s *Server) handleContainerRecreate(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	detail, err := s.modules.docker.Inspect(r.Context(), id)
	if err != nil {
		return s.dockerErr(err)
	}
	// No typed phrase: Recreate is the one destructive path built to be undone.
	// The old container is renamed aside and put back if anything after that
	// point fails, and it is only removed once the replacement is running.
	var opts dockerx.RecreateOptions
	if r.ContentLength > 0 {
		if err := httpx.DecodeJSON(r, &opts); err != nil {
			return err
		}
	}
	if opts.Spec != nil {
		if err := s.authoriseSpec(r, opts.Spec); err != nil {
			return err
		}
	}
	ctx, cancel := timeoutCtx(r, 15*time.Minute)
	defer cancel()

	res, err := s.modules.docker.Recreate(ctx, id, opts, nil)
	if err != nil {
		if errors.Is(err, dockerx.ErrComposeManaged) {
			// Not a failure to explain away: the stack is where this belongs,
			// and the code tells the UI to offer that button instead.
			return httpx.Err(http.StatusConflict, "compose_managed", err.Error())
		}
		return s.dockerErr(err)
	}
	httpx.SetAudit(r, "docker.container.recreate", detail.Name, map[string]any{
		"id": id, "newId": res.ID, "pulled": opts.PullLatest, "edited": opts.Spec != nil,
	})
	httpx.JSON(w, http.StatusOK, res)
	return nil
}

func (s *Server) handleContainerRename(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	var req struct {
		Name string `json:"name"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	detail, err := s.modules.docker.Inspect(r.Context(), id)
	if err != nil {
		return s.dockerErr(err)
	}
	if err := s.modules.docker.Rename(r.Context(), id, req.Name); err != nil {
		return s.dockerErr(err)
	}
	httpx.SetAudit(r, "docker.container.rename", detail.Name, map[string]any{"id": id, "to": req.Name})
	httpx.NoContent(w)
	return nil
}

// handleContainerResources changes the limits on a container that is already
// running — the one part of a container's configuration Docker will let you
// edit in place, and the one an operator most often gets wrong first time.
func (s *Server) handleContainerResources(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	var limits dockerx.ResourceLimits
	if err := httpx.DecodeJSON(r, &limits); err != nil {
		return err
	}
	detail, err := s.modules.docker.Inspect(r.Context(), id)
	if err != nil {
		return s.dockerErr(err)
	}
	warnings, err := s.modules.docker.UpdateResources(r.Context(), id, limits)
	if err != nil {
		return s.dockerErr(err)
	}
	httpx.SetAudit(r, "docker.container.resources", detail.Name, map[string]any{
		"id": id, "memoryMb": limits.MemoryMB, "cpus": limits.CPUs,
	})
	httpx.JSON(w, http.StatusOK, map[string]any{"warnings": warnings})
	return nil
}

// handleContainerRaw is the escape hatch: the Engine's own inspect output,
// unmodified.
//
// Every panel in this product is a chosen subset of something, and eventually
// somebody needs the field nobody chose. Portainer has this and it is the
// thing experienced operators reach for when they suspect the UI is lying to
// them — which is a reasonable suspicion to be able to check.
func (s *Server) handleContainerRaw(w http.ResponseWriter, r *http.Request) error {
	detail, err := s.modules.docker.InspectRaw(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		return s.dockerErr(err)
	}
	if !httpx.MustPrincipal(r).Can(auth.CapSystemAdmin) {
		redactRawEnv(detail)
	}
	httpx.JSON(w, http.StatusOK, detail)
	return nil
}

// redactRawEnv masks the environment inside a decoded inspect document.
//
// The raw view would otherwise be a way around the redaction the typed view
// applies — the same secrets, one route over. It walks the two places the
// Engine puts an environment rather than every string in the document, because
// a blanket scrub would mangle labels and commands that legitimately contain
// the word "key".
func redactRawEnv(doc map[string]any) {
	for _, section := range []string{"Config"} {
		cfg, ok := doc[section].(map[string]any)
		if !ok {
			continue
		}
		env, ok := cfg["Env"].([]any)
		if !ok {
			continue
		}
		for i, entry := range env {
			line, ok := entry.(string)
			if !ok {
				continue
			}
			name, _, found := strings.Cut(line, "=")
			if found && dockerx.IsSecretEnvKey(name) {
				env[i] = name + "=" + dockerx.RedactedEnvValue
			}
		}
	}
}

// handleContainerChanges lists what the container has written to its own
// filesystem since it started from the image.
//
// This is the evidence behind the "data in the writable layer" finding: a
// container with a database in /var/lib/postgresql and no volume mounted there
// shows exactly that here, and that data is destroyed the next time the
// container is recreated.
func (s *Server) handleContainerChanges(w http.ResponseWriter, r *http.Request) error {
	changes, err := s.modules.docker.Changes(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		return s.dockerErr(err)
	}
	httpx.JSON(w, http.StatusOK, changes)
	return nil
}

// ---------------------------------------------------------------- images ---

func (s *Server) handleImageDetail(w http.ResponseWriter, r *http.Request) error {
	detail, err := s.modules.docker.InspectImage(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		return s.dockerErr(err)
	}
	// An image's baked-in environment carries secrets about as often as a
	// container's — a Dockerfile that turned a build ARG into an ENV is a
	// common enough mistake that this should not be the thing that publishes
	// it.
	if !httpx.MustPrincipal(r).Can(auth.CapSystemAdmin) {
		detail.Env = dockerx.RedactEnv(detail.Env)
	}
	httpx.JSON(w, http.StatusOK, detail)
	return nil
}

// handleImageUpdates answers "is anything I am running out of date".
//
// Defaults to the images containers are actually using rather than every image
// on the host: an old layer nobody references is not news, and each answer
// costs a registry round trip.
func (s *Server) handleImageUpdates(w http.ResponseWriter, r *http.Request) error {
	force := r.URL.Query().Get("refresh") == "true"
	var refs []string
	if raw := r.URL.Query().Get("refs"); raw != "" {
		refs = strings.Split(raw, ",")
	}
	ctx, cancel := timeoutCtx(r, 2*time.Minute)
	defer cancel()

	if len(refs) == 0 {
		httpx.JSON(w, http.StatusOK, s.modules.docker.CheckRunningUpdates(ctx, force))
		return nil
	}
	httpx.JSON(w, http.StatusOK, s.modules.docker.CheckUpdates(ctx, refs, force))
	return nil
}

func (s *Server) handleImageTag(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	var req struct {
		Tag string `json:"tag"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if strings.TrimSpace(req.Tag) == "" {
		return httpx.BadRequest("a tag is required")
	}
	if err := s.modules.docker.TagImage(r.Context(), id, req.Tag); err != nil {
		return s.dockerErr(err)
	}
	httpx.SetAudit(r, "docker.image.tag", id, map[string]any{"tag": req.Tag})
	httpx.NoContent(w)
	return nil
}

// handleImageBuild builds an image from a directory on this server and streams
// the output.
//
// The directory is the link between the git panel and Docker: a repository the
// dashboard already pulls is a build context, and the two features knowing
// about each other is the difference between "clone, then ssh in and build"
// and pressing a button. It is resolved through files.Resolve like any other
// client-supplied path.
func (s *Server) handleImageBuild(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	// Required explicitly: files.Resolve treats an empty path as the first
	// configured root, which would quietly make the build context the whole of
	// /srv — or of / on a default install.
	requested := strings.TrimSpace(q.Get("dir"))
	if requested == "" {
		return httpx.BadRequest("a directory to build from is required")
	}
	dir, err := s.modules.files.Resolve(requested)
	if err != nil {
		return httpx.BadRequest("%s is outside the paths this dashboard may reach", requested)
	}
	opts := dockerx.BuildOptions{
		Dir:        dir,
		Dockerfile: q.Get("dockerfile"),
		Tag:        q.Get("tag"),
		NoCache:    q.Get("noCache") == "true",
		Pull:       q.Get("pull") == "true",
		Target:     q.Get("target"),
	}
	if strings.TrimSpace(opts.Tag) == "" {
		return httpx.BadRequest("a name for the built image is required")
	}
	s.recordAudit(r, "docker.image.build", opts.Tag, map[string]any{"dir": dir})

	return s.streamLines(w, r, func(ctx context.Context, out chan<- dockerx.LogLine) (int, error) {
		return s.modules.docker.Build(ctx, opts, out)
	})
}

// --------------------------------------------------------------- volumes ---

func (s *Server) handleVolumeCreate(w http.ResponseWriter, r *http.Request) error {
	var spec dockerx.VolumeSpec
	if err := httpx.DecodeJSON(r, &spec); err != nil {
		return err
	}
	vol, err := s.modules.docker.CreateVolume(r.Context(), spec)
	if err != nil {
		return s.dockerErr(err)
	}
	httpx.SetAudit(r, "docker.volume.create", vol.Name, nil)
	httpx.JSON(w, http.StatusCreated, vol)
	return nil
}

// -------------------------------------------------------------- networks ---

func (s *Server) handleNetworkCreate(w http.ResponseWriter, r *http.Request) error {
	var spec dockerx.NetworkSpec
	if err := httpx.DecodeJSON(r, &spec); err != nil {
		return err
	}
	net, err := s.modules.docker.CreateNetwork(r.Context(), spec)
	if err != nil {
		return s.dockerErr(err)
	}
	httpx.SetAudit(r, "docker.network.create", net.Name, map[string]any{"driver": net.Driver})
	httpx.JSON(w, http.StatusCreated, net)
	return nil
}

func (s *Server) handleNetworkConnect(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	var req struct {
		Container string   `json:"container"`
		Aliases   []string `json:"aliases,omitempty"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if req.Container == "" {
		return httpx.BadRequest("a container is required")
	}
	if err := s.modules.docker.ConnectNetwork(r.Context(), id, req.Container, req.Aliases); err != nil {
		return s.dockerErr(err)
	}
	httpx.SetAudit(r, "docker.network.connect", id, map[string]any{"container": req.Container, "aliases": req.Aliases})
	httpx.NoContent(w)
	return nil
}

func (s *Server) handleNetworkDisconnect(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	var req struct {
		Container string `json:"container"`
		Force     bool   `json:"force,omitempty"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if req.Container == "" {
		return httpx.BadRequest("a container is required")
	}
	if err := s.modules.docker.DisconnectNetwork(r.Context(), id, req.Container, req.Force); err != nil {
		return s.dockerErr(err)
	}
	httpx.SetAudit(r, "docker.network.disconnect", id, map[string]any{"container": req.Container})
	httpx.NoContent(w)
	return nil
}

func (s *Server) handleNetworkPrune(w http.ResponseWriter, r *http.Request) error {
	if err := httpx.RequireTypedConfirmation(w, r, "prune networks"); err != nil {
		return err
	}
	rep, err := s.modules.docker.PruneNetworks(r.Context())
	if err != nil {
		return s.dockerErr(err)
	}
	httpx.SetAudit(r, "docker.network.prune", "", map[string]any{"deleted": rep.Items})
	httpx.JSON(w, http.StatusOK, rep)
	return nil
}

// ---------------------------------------------------------------- stacks ---

// StackDetail is everything the stack page needs in one request: what compose
// declares, what is actually running, where the file is, and what else in the
// dashboard knows about that directory.
type StackDetail struct {
	dockerx.ComposeStack
	ConfigPath string `json:"configPath,omitempty"`
	// Declared is what the compose file says should exist. A service listed
	// here with no container is a service that failed to start and that
	// nothing else in Docker will ever mention.
	Declared []string `json:"declared"`
	// DeclaredError carries a compose file that no longer parses, which is
	// itself the most important thing to say about a stack.
	DeclaredError string `json:"declaredError,omitempty"`
	// Git and Files are the links out. A stack is a directory, and this
	// dashboard already has a file manager, a git panel and a terminal that
	// can each be pointed at it — knowing that a stack's directory is a
	// checkout with uncommitted changes is exactly the context somebody needs
	// before pressing redeploy.
	Git *StackGit `json:"git,omitempty"`
}

type StackGit struct {
	Path   string `json:"path"`
	Branch string `json:"branch,omitempty"`
	// Dirty and Behind are the two that change what a redeploy will do:
	// uncommitted edits in the working tree are what compose will actually
	// read, and commits behind the remote are what a pull would bring in
	// first.
	Dirty   bool   `json:"dirty"`
	Changes int    `json:"changes"`
	Ahead   int    `json:"ahead"`
	Behind  int    `json:"behind"`
	Commit  string `json:"commit,omitempty"`
	Subject string `json:"subject,omitempty"`
}

func (s *Server) handleStackDetail(w http.ResponseWriter, r *http.Request) error {
	name := chi.URLParam(r, "name")
	stack, err := s.findStack(r, name)
	if err != nil {
		return err
	}
	detail := StackDetail{ComposeStack: *stack, Declared: []string{}}
	if path, err := dockerx.ComposeFileFor(stack); err == nil {
		detail.ConfigPath = path
	}
	if stack.Managed {
		ctx, cancel := timeoutCtx(r, 45*time.Second)
		defer cancel()
		declared, err := s.modules.docker.DeclaredServices(ctx, stack.WorkingDir)
		if err != nil {
			detail.DeclaredError = err.Error()
		} else {
			detail.Declared = declared
			detail.Services = mergeDeclared(detail.Services, declared)
			detail.Total = len(detail.Services)
		}
		detail.Git = s.stackGit(r, stack.WorkingDir)
	}
	httpx.JSON(w, http.StatusOK, detail)
	return nil
}

// mergeDeclared adds a placeholder row for every service the compose file
// names that has no container.
func mergeDeclared(running []dockerx.ComposeService, declared []string) []dockerx.ComposeService {
	present := map[string]bool{}
	for _, svc := range running {
		present[svc.Name] = true
	}
	out := append([]dockerx.ComposeService{}, running...)
	for _, name := range declared {
		if present[name] {
			continue
		}
		out = append(out, dockerx.ComposeService{
			Name: name, State: "missing", Status: "declared but not created",
			Missing: true, Ports: []dockerx.Port{},
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// stackGit reports whether a stack's directory is a checkout and what state it
// is in. Best effort by design: git being unavailable, or the directory not
// being a repository, is the ordinary case and not worth an error.
//
// The repository root is resolved first, and both halves of that matter.
// `gitx.Summary` never fails — it fills in what it can for any directory at
// all — so without this every stack claimed to be a checkout, with an empty
// branch and a link to a repository that does not exist. And a stack that is a
// *subdirectory* of a repo has to link to the repo's root, because that is
// what the repository list is keyed by; linking to the subdirectory opens
// nothing.
func (s *Server) stackGit(r *http.Request, dir string) *StackGit {
	if !s.modules.git.Available() {
		return nil
	}
	ctx, cancel := timeoutCtx(r, 15*time.Second)
	defer cancel()
	root, err := s.modules.git.Toplevel(ctx, dir)
	if err != nil {
		return nil
	}
	repo, err := s.modules.git.Summary(ctx, root)
	if err != nil || repo == nil {
		return nil
	}
	return &StackGit{
		Path:    root,
		Branch:  repo.Branch,
		Dirty:   repo.Dirty,
		Changes: repo.Changes,
		Ahead:   repo.Ahead,
		Behind:  repo.Behind,
		Commit:  repo.Head,
		Subject: repo.Subject,
	}
}

func (s *Server) handleStackValidate(w http.ResponseWriter, r *http.Request) error {
	stack, err := s.findStack(r, chi.URLParam(r, "name"))
	if err != nil {
		return err
	}
	var req struct {
		Content string `json:"content"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	httpx.SkipAudit(r)
	res, err := s.modules.docker.ValidateCompose(r.Context(), stack.WorkingDir, req.Content)
	if err != nil {
		return httpx.Wrap(http.StatusBadGateway, "compose_failed", err)
	}
	httpx.JSON(w, http.StatusOK, res)
	return nil
}

// handleStackConfigWrite replaces a stack's compose file.
//
// Validated before it is written, always: the file on disk currently works,
// and replacing it with one that does not parse turns every button on the
// stack page into an error message. The previous content is kept beside it —
// validation catches a broken file, not a correct file that says the wrong
// thing, and the second is only discovered after the stack comes back up
// wrong.
func (s *Server) handleStackConfigWrite(w http.ResponseWriter, r *http.Request) error {
	stack, err := s.findStack(r, chi.URLParam(r, "name"))
	if err != nil {
		return err
	}
	var req struct {
		Content string `json:"content"`
		// Force writes a file compose rejects. Offered because compose
		// validates variable substitution too, and a file that is correct but
		// depends on an .env this process cannot read would otherwise be
		// unsaveable.
		Force bool `json:"force,omitempty"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if strings.TrimSpace(req.Content) == "" {
		return httpx.BadRequest("an empty compose file would leave this stack with nothing to run")
	}
	path, err := dockerx.ComposeFileFor(stack)
	if err != nil {
		return httpx.Err(http.StatusNotFound, "no_compose_file",
			"this stack has no compose file on disk that the dashboard can reach")
	}
	// files.Resolve on the path we are about to write, even though it came
	// from the daemon's own labels rather than from the client: an operator
	// who narrowed JD_FILE_ROOTS meant it, and a compose file outside them is
	// not ours to edit.
	resolved, err := s.modules.files.Resolve(path)
	if err != nil {
		return httpx.Err(http.StatusForbidden, "outside_roots",
			"this stack's compose file is outside the paths this dashboard may write to")
	}

	validation, err := s.modules.docker.ValidateCompose(r.Context(), stack.WorkingDir, req.Content)
	if err != nil {
		return httpx.Wrap(http.StatusBadGateway, "compose_failed", err)
	}
	if !validation.Valid && !req.Force {
		return httpx.Err(http.StatusUnprocessableEntity, "compose_invalid", validation.Error)
	}
	if err := dockerx.WriteComposeFile(resolved, req.Content); err != nil {
		return httpx.Wrap(http.StatusInternalServerError, "write_failed", err)
	}
	httpx.SetAudit(r, "docker.stack.config.write", stack.Name, map[string]any{
		"path": resolved, "bytes": len(req.Content), "forced": !validation.Valid,
	})
	httpx.JSON(w, http.StatusOK, map[string]any{
		"path": resolved, "validation": validation,
		// Saving does not deploy. Compose reads the file on the next command,
		// so a saved edit changes nothing until the stack is brought up — and
		// a UI that implied otherwise would be the worst kind of wrong.
		"applied": false,
	})
	return nil
}

func (s *Server) handleStackCreate(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Name    string `json:"name"`
		Dir     string `json:"dir,omitempty"`
		Content string `json:"content"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return httpx.BadRequest("a name is required")
	}
	if !validStackName(name) {
		return httpx.BadRequest("a stack name may contain lower-case letters, digits, dashes and underscores")
	}
	dir := strings.TrimSpace(req.Dir)
	if dir == "" {
		roots := s.Cfg.ComposeRoots
		if len(roots) == 0 {
			return httpx.BadRequest("no compose roots are configured, so there is nowhere to put a new stack")
		}
		dir = filepath.Join(roots[0], name)
	}
	// Two checks, and they are different questions: files.Resolve asks
	// whether the dashboard may write there at all, and the compose-root test
	// asks whether a stack put there will ever be found again — the stack
	// list only discovers files under JD_COMPOSE_ROOTS, so a stack created
	// outside them would vanish from the UI the moment it was stopped.
	resolved, err := s.modules.files.Resolve(dir)
	if err != nil {
		return httpx.BadRequest("%s is outside the paths this dashboard may write to", dir)
	}
	if !underAnyRoot(resolved, s.Cfg.ComposeRoots) {
		return httpx.BadRequest(
			"a new stack has to live under one of the configured compose directories (%s), or the dashboard will not find it again",
			strings.Join(s.Cfg.ComposeRoots, ", "))
	}
	content := req.Content
	if strings.TrimSpace(content) == "" {
		content = dockerx.StarterCompose(name)
	}
	path, err := dockerx.NewStack(resolved, content)
	if err != nil {
		return httpx.Wrap(http.StatusBadRequest, "stack_exists", err)
	}
	httpx.SetAudit(r, "docker.stack.create", name, map[string]any{"path": path})
	httpx.JSON(w, http.StatusCreated, map[string]any{"name": name, "dir": resolved, "path": path})
	return nil
}

func validStackName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for i, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case i > 0 && (r == '-' || r == '_'):
		default:
			return false
		}
	}
	return true
}

func underAnyRoot(path string, roots []string) bool {
	clean := filepath.Clean(path)
	for _, root := range roots {
		root = filepath.Clean(root)
		if clean == root || strings.HasPrefix(clean, root+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// handleStackRun is compose, watched rather than waited for.
//
// The action arrives as a query parameter rather than in the path because a
// WebSocket handshake is a GET with no body, and the alternative — a socket
// per action — would repeat the confirmation and capability logic six times.
// The destructive actions are checked here with the same phrase and the same
// budget the POST routes use, so the socket is not a way around either.
func (s *Server) handleStackRun(w http.ResponseWriter, r *http.Request) error {
	name := chi.URLParam(r, "name")
	stack, err := s.findStack(r, name)
	if err != nil {
		return err
	}
	if !stack.Managed {
		return httpx.BadRequest("stack %q has no compose file on disk that this dashboard can reach", name)
	}
	action := dockerx.ComposeAction(r.URL.Query().Get("action"))
	if action == "" {
		action = dockerx.ComposeUp
	}
	if composeIsDestructive(action) {
		p := httpx.MustPrincipal(r)
		if !p.Can(auth.CapDestructive) {
			return httpx.Err(http.StatusForbidden, "forbidden",
				"this action interrupts running services and needs the destructive capability")
		}
		if !s.destrLim.Allow(p.Username()) {
			return httpx.Err(http.StatusTooManyRequests, "rate_limited", "too many destructive actions; try again shortly")
		}
		// The websocket variant: a browser cannot set a header on an upgrade
		// request, so the phrase arrives as a query parameter here. wsx's
		// origin check is what replaces the header's cross-origin guarantee.
		if err := requireComposePhraseWS(w, r, action, name); err != nil {
			return err
		}
	} else if !httpx.MustPrincipal(r).Can(auth.CapServiceControl) {
		return httpx.Err(http.StatusForbidden, "forbidden", "this action needs the service.control capability")
	}

	service := r.URL.Query().Get("service")
	s.recordAudit(r, "docker.stack."+string(action), name, map[string]any{
		"dir": stack.WorkingDir, "service": service, "streamed": true,
	})
	return s.streamLines(w, r, func(ctx context.Context, out chan<- dockerx.LogLine) (int, error) {
		return s.modules.docker.RunComposeStream(ctx, stack.WorkingDir, action, service, out)
	})
}

// composeIsDestructive names the actions that interrupt something already
// running. It is the single answer to "which of these needs confirming", used
// by both the socket and the POST routes so the two cannot drift apart.
func composeIsDestructive(action dockerx.ComposeAction) bool {
	switch action {
	case dockerx.ComposeDown, dockerx.ComposeStop, dockerx.ComposeRestart,
		dockerx.ComposeUpdate, dockerx.ComposeRecreate:
		return true
	default:
		return false
	}
}

// composeNeedsPhrase is deliberately narrower than composeIsDestructive.
//
// All five interrupt a running service, so all five keep the destructive
// capability, the tighter budget and the audit entry. Only `down` removes the
// containers, and only `down` is therefore typed for: stop, restart, update and
// recreate are the ordinary redeploy cycle, run several times in an afternoon,
// and asking for the stack's name each time is how an operator learns to type a
// stack name without reading which stack it is.
func composeNeedsPhrase(action dockerx.ComposeAction) bool {
	return action == dockerx.ComposeDown
}

// requireComposePhraseWS applies the same narrowing on the socket, so the two
// entry points cannot disagree about which actions ask.
func requireComposePhraseWS(w http.ResponseWriter, r *http.Request, action dockerx.ComposeAction, name string) error {
	if !composeNeedsPhrase(action) {
		return nil
	}
	return httpx.RequireTypedConfirmationWS(w, r, name)
}

// handleStackLogStream follows every container in a stack at once.
//
// A stack is one application; its logs are one story told by four processes,
// and reading them means opening four panels and correlating timestamps by
// eye. Each line is tagged with the service it came from so the merged stream
// stays readable, which is what `docker compose logs -f` does and what nothing
// in this dashboard could do until now.
func (s *Server) handleStackLogStream(w http.ResponseWriter, r *http.Request) error {
	name := chi.URLParam(r, "name")
	stack, err := s.findStack(r, name)
	if err != nil {
		return err
	}

	conn, err := s.WS.Upgrade(w, r)
	if err != nil {
		return nil
	}
	defer conn.Close()
	ctx, cancel := contextWithCancel(r)
	defer cancel()
	go conn.Keepalive(ctx)
	go conn.DrainControl(cancel)

	tail := defaultStr(r.URL.Query().Get("tail"), "200")
	merged := make(chan dockerx.LogLine, 512)
	closers := []func(){}
	for _, svc := range stack.Services {
		if svc.Container == "" || svc.State != "running" {
			continue
		}
		ch, closer, err := s.modules.docker.Logs(ctx, svc.Container, dockerx.LogOptions{
			Tail: tail, Follow: true, Timestamps: r.URL.Query().Get("timestamps") == "true",
		})
		if err != nil {
			continue
		}
		closers = append(closers, func() { closer.Close() })
		go func(service string, in <-chan dockerx.LogLine) {
			for line := range in {
				line.Service = service
				select {
				case <-ctx.Done():
					return
				case merged <- line:
				}
			}
		}(svc.Name, ch)
	}
	defer func() {
		for _, c := range closers {
			c()
		}
	}()
	if len(closers) == 0 {
		conn.Send("eof", map[string]string{"reason": "nothing in this stack is running"})
		return nil
	}

	// Batched on the same short tick the single-container stream uses: a
	// stack of chatty services can emit thousands of lines a second between
	// them, and one frame each would drown the browser.
	batch := make([]dockerx.LogLine, 0, 256)
	flush := time.NewTicker(150 * time.Millisecond)
	defer flush.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case line := <-merged:
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

// ---------------------------------------------------- events & diagnosis ---

func (s *Server) handleDockerDiagnose(w http.ResponseWriter, r *http.Request) error {
	ctx, cancel := timeoutCtx(r, 60*time.Second)
	defer cancel()
	d, err := s.modules.docker.Diagnose(ctx)
	if err != nil {
		return s.dockerErr(err)
	}
	httpx.JSON(w, http.StatusOK, d)
	return nil
}

func (s *Server) handleDockerEvents(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	var kinds []string
	if raw := q.Get("kinds"); raw != "" {
		kinds = strings.Split(raw, ",")
	}
	running, since, buffered := s.modules.dockerEvents.Status()
	httpx.JSON(w, http.StatusOK, map[string]any{
		"events": s.modules.dockerEvents.Recent(atoiDefault(q.Get("limit"), 200), kinds, q.Get("search")),
		// A feed with nothing in it means one of two very different things,
		// and the client cannot tell them apart without this.
		"listening": running,
		"since":     since,
		"buffered":  buffered,
	})
	return nil
}

func (s *Server) handleDockerEventStream(w http.ResponseWriter, r *http.Request) error {
	conn, err := s.WS.Upgrade(w, r)
	if err != nil {
		return nil
	}
	defer conn.Close()
	ctx, cancel := contextWithCancel(r)
	defer cancel()
	go conn.Keepalive(ctx)
	go conn.DrainControl(cancel)

	// The buffered past first, so a feed opened at 09:00 can still show the
	// container that died at 03:00.
	if recent := s.modules.dockerEvents.Recent(200, nil, ""); len(recent) > 0 {
		if err := conn.Send("events", recent); err != nil {
			return nil
		}
	}
	events, unsubscribe := s.modules.dockerEvents.Subscribe()
	defer unsubscribe()
	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-events:
			if !ok {
				return nil
			}
			if err := conn.Send("events", []dockerx.Event{ev}); err != nil {
				return nil
			}
		}
	}
}

// ---------------------------------------------------------------- shared ---

// streamLines is the shape every "watch a long command run" endpoint takes: a
// socket, a batched line feed, and a final frame carrying the exit code.
//
// The exit frame is the part worth spelling out. Compose and the builder both
// print their failures to stdout as ordinary lines, so a client that only
// watched the text could not tell a build that failed from one that succeeded
// noisily — it has to be told.
func (s *Server) streamLines(w http.ResponseWriter, r *http.Request, run func(context.Context, chan<- dockerx.LogLine) (int, error)) error {
	conn, err := s.WS.Upgrade(w, r)
	if err != nil {
		return nil
	}
	defer conn.Close()
	ctx, cancel := contextWithCancel(r)
	defer cancel()
	go conn.Keepalive(ctx)
	go conn.DrainControl(cancel)

	lines := make(chan dockerx.LogLine, 256)
	type outcome struct {
		code int
		err  error
	}
	done := make(chan outcome, 1)
	go func() {
		code, err := run(ctx, lines)
		close(lines)
		done <- outcome{code: code, err: err}
	}()

	batch := make([]dockerx.LogLine, 0, 64)
	flush := time.NewTicker(120 * time.Millisecond)
	defer flush.Stop()
	send := func() bool {
		if len(batch) == 0 {
			return true
		}
		if err := conn.Send("output", batch); err != nil {
			return false
		}
		batch = batch[:0]
		return true
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case line, ok := <-lines:
			if !ok {
				send()
				res := <-done
				if res.err != nil {
					conn.SendError(res.err.Error())
					return nil
				}
				conn.Send("done", map[string]any{"exitCode": res.code})
				return nil
			}
			batch = append(batch, line)
			if len(batch) >= 64 && !send() {
				return nil
			}
		case <-flush.C:
			if !send() {
				return nil
			}
		}
	}
}

// PortRoute answers "where is this actually reachable from" for one published
// port.
//
// A container publishing 3000 on loopback and a Caddy site proxying to
// 127.0.0.1:3000 are two facts the dashboard already holds in two different
// panels, and joining them is what turns "it is running on port 3000" into
// "it is at https://app.example.com". Nothing else in this class knows about
// the reverse proxy in front of it, because nothing else manages one.
type PortRoute struct {
	HostIP        string `json:"hostIp,omitempty"`
	HostPort      int    `json:"hostPort"`
	ContainerPort int    `json:"containerPort"`
	// Public reports whether this port is published on every interface, which
	// is the case the security panel cares about.
	Public bool `json:"public"`
	// VHost and URL are set when a reverse-proxy site forwards to this port.
	VHost string `json:"vhost,omitempty"`
	URL   string `json:"url,omitempty"`
	TLS   bool   `json:"tls,omitempty"`
}

func (s *Server) handleContainerRoutes(w http.ResponseWriter, r *http.Request) error {
	detail, err := s.modules.docker.Inspect(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		return s.dockerErr(err)
	}
	// The proxy being absent is the ordinary case on a host that does not run
	// one, and produces routes with no vhost rather than an error.
	vhosts, _ := s.modules.proxy.ListVHosts(r.Context())

	out := []PortRoute{}
	for _, p := range detail.Ports {
		if p.PublicPort == 0 {
			continue
		}
		route := PortRoute{
			HostIP:        p.IP,
			HostPort:      int(p.PublicPort),
			ContainerPort: int(p.PrivatePort),
			Public:        p.IP == "" || p.IP == "0.0.0.0" || p.IP == "::",
		}
		if v := matchVHost(vhosts, int(p.PublicPort)); v != nil {
			route.VHost = v.Name
			route.TLS = v.TLS
			if len(v.ServerNames) > 0 {
				scheme := "http"
				if v.TLS {
					scheme = "https"
				}
				route.URL = scheme + "://" + v.ServerNames[0]
			}
		}
		out = append(out, route)
	}
	httpx.JSON(w, http.StatusOK, out)
	return nil
}

// matchVHost finds a proxy site forwarding to a port on this machine.
//
// Matched on the port alone rather than on the whole address: an upstream may
// be written as localhost:3000, 127.0.0.1:3000 or [::1]:3000, and all three
// mean the same thing. A port collision between two sites is possible in
// principle and is not worth guarding against — both would be pointing at the
// same service.
func matchVHost(vhosts []proxysvc.VHost, port int) *proxysvc.VHost {
	needle := ":" + strconv.Itoa(port)
	for i := range vhosts {
		if !vhosts[i].Enabled {
			continue
		}
		for _, up := range vhosts[i].Upstreams {
			if strings.HasSuffix(up, needle) || strings.Contains(up, needle+"/") {
				return &vhosts[i]
			}
		}
	}
	return nil
}

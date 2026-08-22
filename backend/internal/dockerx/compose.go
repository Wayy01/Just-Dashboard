package dockerx

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Compose stacks are discovered from the labels the compose plugin writes onto
// every container it creates, so a stack is visible whether or not its project
// directory is somewhere we were told to look.
const (
	labelProject    = "com.docker.compose.project"
	labelService    = "com.docker.compose.service"
	labelWorkingDir = "com.docker.compose.project.working_dir"
	labelConfigFile = "com.docker.compose.project.config_files"
)

type ComposeService struct {
	Name      string `json:"name"`
	Container string `json:"container"`
	State     string `json:"state"`
	Status    string `json:"status"`
	Image     string `json:"image"`
	// Health and Ports are carried so a stack card can answer "is it working"
	// and "where do I reach it" without opening every container in turn —
	// which is the whole reason somebody looks at a stack rather than at the
	// container table.
	Health string `json:"health,omitempty"`
	Ports  []Port `json:"ports"`
	// Missing marks a service the compose file declares that has no container
	// at all. Nothing else in Docker will ever mention it: `docker ps` cannot
	// list what does not exist, so a service that failed to create is
	// indistinguishable from one that was never written down.
	Missing bool `json:"missing,omitempty"`
}

type ComposeStack struct {
	Name        string           `json:"name"`
	WorkingDir  string           `json:"workingDir"`
	ConfigFiles []string         `json:"configFiles"`
	Services    []ComposeService `json:"services"`
	Running     int              `json:"running"`
	Total       int              `json:"total"`
	Managed     bool             `json:"managed"`
}

// ListStacks groups running containers by compose project and then folds in
// any compose file found on disk under the configured roots, so a stack that
// is fully stopped still appears and can be brought up.
func (c *Client) ListStacks(ctx context.Context, roots []string) ([]ComposeStack, error) {
	// The general container listing rather than a filtered one of its own:
	// it already resolves health and uptime for everything running, which a
	// stack card needs and which a second query would have to inspect for all
	// over again.
	items, err := c.ListContainers(ctx, true)
	if err != nil {
		return nil, err
	}
	stacks := map[string]*ComposeStack{}
	for _, it := range items {
		project := it.Labels[labelProject]
		if project == "" {
			continue
		}
		st, ok := stacks[project]
		if !ok {
			st = &ComposeStack{
				Name:        project,
				WorkingDir:  it.Labels[labelWorkingDir],
				ConfigFiles: splitConfigFiles(it.Labels[labelConfigFile]),
				Services:    []ComposeService{},
			}
			stacks[project] = st
		}
		name := it.Labels[labelService]
		if name == "" {
			name = it.Name
		}
		ports := it.Ports
		if ports == nil {
			ports = []Port{}
		}
		st.Services = append(st.Services, ComposeService{
			Name:      name,
			Container: it.ID,
			State:     it.State,
			Status:    it.Status,
			Image:     it.Image,
			Health:    it.Health,
			Ports:     ports,
		})
		st.Total++
		if it.State == "running" {
			st.Running++
		}
	}
	for _, found := range discoverComposeFiles(roots) {
		name := filepath.Base(found.dir)
		if st, ok := stacks[name]; ok {
			if st.WorkingDir == "" {
				st.WorkingDir = found.dir
			}
			if len(st.ConfigFiles) == 0 {
				st.ConfigFiles = []string{found.file}
			}
			continue
		}
		stacks[name] = &ComposeStack{
			Name:        name,
			WorkingDir:  found.dir,
			ConfigFiles: []string{found.file},
			Services:    []ComposeService{},
		}
	}
	out := make([]ComposeStack, 0, len(stacks))
	for _, st := range stacks {
		sort.Slice(st.Services, func(i, j int) bool { return st.Services[i].Name < st.Services[j].Name })
		// Only a stack whose compose file we can locate can be acted on;
		// the UI greys out up/down for the rest instead of failing later.
		st.Managed = st.WorkingDir != "" && dirExists(st.WorkingDir)
		out = append(out, *st)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func splitConfigFiles(v string) []string {
	if v == "" {
		return []string{}
	}
	return strings.Split(v, ",")
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

type composeFile struct{ dir, file string }

var composeNames = []string{
	"docker-compose.yml", "docker-compose.yaml", "compose.yml", "compose.yaml",
}

// discoverComposeFiles walks the configured roots shallowly. A full-depth walk
// of /home on a busy server is slow and would surface vendored fixtures, so
// the search stops three levels down.
func discoverComposeFiles(roots []string) []composeFile {
	found := []composeFile{}
	seen := map[string]bool{}
	for _, root := range roots {
		base := strings.Count(filepath.Clean(root), string(os.PathSeparator))
		filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				name := d.Name()
				if name == "node_modules" || name == ".git" || name == "vendor" {
					return filepath.SkipDir
				}
				if strings.Count(filepath.Clean(path), string(os.PathSeparator))-base > 3 {
					return filepath.SkipDir
				}
				return nil
			}
			for _, want := range composeNames {
				if d.Name() == want {
					dir := filepath.Dir(path)
					if !seen[dir] {
						seen[dir] = true
						found = append(found, composeFile{dir: dir, file: path})
					}
				}
			}
			return nil
		})
	}
	return found
}

type ComposeAction string

const (
	ComposeUp      ComposeAction = "up"
	ComposeDown    ComposeAction = "down"
	ComposeRestart ComposeAction = "restart"
	ComposePull    ComposeAction = "pull"
	ComposeStop    ComposeAction = "stop"
	ComposeStart   ComposeAction = "start"
)

type ComposeResult struct {
	Action   ComposeAction `json:"action"`
	Stack    string        `json:"stack"`
	ExitCode int           `json:"exitCode"`
	Output   string        `json:"output"`
	Duration string        `json:"duration"`
}

// RunCompose drives the compose plugin. Compose orchestration has no Engine
// API equivalent — the daemon knows nothing about projects — so this is the
// one place the package invokes a binary. The argument vector is built
// explicitly and never passed through a shell, and the project directory comes
// from the container labels rather than from user input.
func (c *Client) RunCompose(ctx context.Context, dir string, action ComposeAction, service string) (*ComposeResult, error) {
	if !dirExists(dir) {
		return nil, os.ErrNotExist
	}
	args := []string{"compose"}
	switch action {
	case ComposeUp:
		args = append(args, "up", "-d", "--remove-orphans")
	case ComposeDown:
		args = append(args, "down")
	case ComposeRestart:
		args = append(args, "restart")
	case ComposePull:
		args = append(args, "pull")
	case ComposeStop:
		args = append(args, "stop")
	case ComposeStart:
		args = append(args, "start")
	default:
		return nil, errUnknownAction(LifecycleAction(action))
	}
	if service != "" && action != ComposeDown {
		args = append(args, service)
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	start := time.Now()
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "COMPOSE_PROGRESS=plain", "DOCKER_CLI_HINTS=false")
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()

	res := &ComposeResult{
		Action:   action,
		Stack:    filepath.Base(dir),
		Output:   buf.String(),
		Duration: time.Since(start).Round(time.Millisecond).String(),
	}
	if cmd.ProcessState != nil {
		res.ExitCode = cmd.ProcessState.ExitCode()
	}
	if err != nil && res.ExitCode == 0 {
		res.ExitCode = -1
	}
	return res, nil
}

// ReadComposeFile returns a stack's compose file for the config viewer.
func ReadComposeFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

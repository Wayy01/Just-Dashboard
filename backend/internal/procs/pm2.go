package procs

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"sync"
	"time"
)

type PM2Process struct {
	ID          int     `json:"id"`
	Name        string  `json:"name"`
	Namespace   string  `json:"namespace"`
	Status      string  `json:"status"`
	PID         int     `json:"pid"`
	CPU         float64 `json:"cpu"`
	Memory      int64   `json:"memory"`
	Restarts    int     `json:"restarts"`
	Unstable    int     `json:"unstableRestarts"`
	UptimeMS    int64   `json:"uptimeMs"`
	ExecMode    string  `json:"execMode"`
	Instances   int     `json:"instances"`
	ScriptPath  string  `json:"scriptPath"`
	CWD         string  `json:"cwd"`
	NodeVersion string  `json:"nodeVersion"`
	OutLogPath  string  `json:"outLogPath"`
	ErrLogPath  string  `json:"errLogPath"`
	User        string  `json:"user"`
	Watching    bool    `json:"watching"`
}

// pm2Raw mirrors the subset of `pm2 jlist` output we consume. PM2's schema is
// wide and unstable at the edges, so only stable fields are mapped.
type pm2Raw struct {
	PID       int    `json:"pid"`
	Name      string `json:"name"`
	PMID      int    `json:"pm_id"`
	Namespace string `json:"namespace"`
	Monit     struct {
		Memory int64   `json:"memory"`
		CPU    float64 `json:"cpu"`
	} `json:"monit"`
	PM2Env struct {
		Status           string `json:"status"`
		PMUptime         int64  `json:"pm_uptime"`
		RestartTime      int    `json:"restart_time"`
		UnstableRestarts int    `json:"unstable_restarts"`
		ExecMode         string `json:"exec_mode"`
		Instances        any    `json:"instances"`
		PMExecPath       string `json:"pm_exec_path"`
		PMCwd            string `json:"pm_cwd"`
		NodeVersion      string `json:"node_version"`
		PMOutLogPath     string `json:"pm_out_log_path"`
		PMErrLogPath     string `json:"pm_err_log_path"`
		Username         string `json:"username"`
		Watch            any    `json:"watch"`
	} `json:"pm2_env"`
}

type PM2 struct {
	mu       sync.Mutex
	cached   []PM2Process
	cachedAt time.Time
}

func NewPM2() *PM2 { return &PM2{} }

func (p *PM2) Available() bool { return binaryExists("pm2") }

func (p *PM2) List(ctx context.Context) ([]PM2Process, error) {
	if !p.Available() {
		return nil, fmt.Errorf("pm2 %w", ErrNotInstalled)
	}
	// Both the PM2 tab and the live process inventory ask for this list. One
	// `pm2 jlist` every few seconds is enough; spawning two CLI clients on every
	// poll costs more than the process table itself and they return the same
	// daemon snapshot.
	p.mu.Lock()
	defer p.mu.Unlock()
	if time.Since(p.cachedAt) < 3*time.Second {
		return append([]PM2Process(nil), p.cached...), nil
	}
	res, err := run(ctx, 20*time.Second, "pm2", "jlist")
	if err != nil {
		return nil, err
	}
	var raw []pm2Raw
	if err := json.Unmarshal([]byte(res.Stdout), &raw); err != nil {
		return nil, fmt.Errorf("parse pm2 jlist output: %w", err)
	}
	now := time.Now().UnixMilli()
	out := make([]PM2Process, 0, len(raw))
	for _, r := range raw {
		proc := PM2Process{
			ID: r.PMID, Name: r.Name, Namespace: r.Namespace,
			Status: r.PM2Env.Status, PID: r.PID,
			CPU: r.Monit.CPU, Memory: r.Monit.Memory,
			Restarts: r.PM2Env.RestartTime, Unstable: r.PM2Env.UnstableRestarts,
			ExecMode: r.PM2Env.ExecMode, ScriptPath: r.PM2Env.PMExecPath,
			CWD: r.PM2Env.PMCwd, NodeVersion: r.PM2Env.NodeVersion,
			OutLogPath: r.PM2Env.PMOutLogPath, ErrLogPath: r.PM2Env.PMErrLogPath,
			User: r.PM2Env.Username,
		}
		if r.PM2Env.Status == "online" && r.PM2Env.PMUptime > 0 {
			proc.UptimeMS = now - r.PM2Env.PMUptime
		}
		proc.Instances = coerceInt(r.PM2Env.Instances)
		proc.Watching = coerceBool(r.PM2Env.Watch)
		out = append(out, proc)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	p.cached = append(p.cached[:0], out...)
	p.cachedAt = time.Now()
	return append([]PM2Process(nil), out...), nil
}

// PM2 reports these fields as number, string or bool depending on version and
// how the ecosystem file was written.
func coerceInt(v any) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	}
	return 0
}

func coerceBool(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case []any:
		return len(t) > 0
	case string:
		return t != "" && t != "false"
	}
	return false
}

type PM2Action string

const (
	PM2Start   PM2Action = "start"
	PM2Stop    PM2Action = "stop"
	PM2Restart PM2Action = "restart"
	PM2Reload  PM2Action = "reload"
	PM2Delete  PM2Action = "delete"
)

func (p *PM2) Control(ctx context.Context, name string, action PM2Action) (*CommandResult, error) {
	if err := ValidateName(name); err != nil {
		return nil, err
	}
	switch action {
	case PM2Start, PM2Stop, PM2Restart, PM2Reload, PM2Delete:
	default:
		return nil, fmt.Errorf("unknown pm2 action %q", action)
	}
	result, err := run(ctx, 60*time.Second, "pm2", string(action), name)
	if err == nil {
		p.mu.Lock()
		p.cachedAt = time.Time{}
		p.mu.Unlock()
	}
	return result, err
}

// Save persists PM2's current inventory for its startup hook to resurrect on
// boot. It does not install or rewrite the init integration: PM2 owns that
// platform-specific configuration, while this action makes the current list
// match what an existing hook will restore.
func (p *PM2) Save(ctx context.Context) (*CommandResult, error) {
	if !p.Available() {
		return nil, fmt.Errorf("pm2 %w", ErrNotInstalled)
	}
	return run(ctx, 60*time.Second, "pm2", "save")
}

// LogPaths returns the on-disk log files for a process so the log tailer can
// follow them directly, which survives `pm2 logs` being killed and gives the
// same view after a restart.
func (p *PM2) LogPaths(ctx context.Context, name string) (outPath, errPath string, err error) {
	list, err := p.List(ctx)
	if err != nil {
		return "", "", err
	}
	for _, proc := range list {
		if proc.Name == name {
			return proc.OutLogPath, proc.ErrLogPath, nil
		}
	}
	return "", "", fmt.Errorf("pm2 process %q not found", name)
}

// StreamLogs follows PM2's own combined output. It is used when the on-disk
// paths are unavailable (for instance a process configured with /dev/null logs).
func (p *PM2) StreamLogs(ctx context.Context, name string, lines int) (*exec.Cmd, error) {
	if err := ValidateName(name); err != nil {
		return nil, err
	}
	if lines <= 0 || lines > 5000 {
		lines = 200
	}
	return exec.CommandContext(ctx, "pm2", "logs", name, "--raw", "--lines", fmt.Sprint(lines)), nil
}

package procs

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/shirou/gopsutil/v4/process"
)

// Process is one row of the htop-style table.
type Process struct {
	PID        int32     `json:"pid"`
	PPID       int32     `json:"ppid"`
	Name       string    `json:"name"`
	Cmdline    string    `json:"cmdline"`
	Username   string    `json:"username"`
	Status     string    `json:"status"`
	CPUPercent float64   `json:"cpuPercent"`
	MemPercent float64   `json:"memPercent"`
	RSS        uint64    `json:"rss"`
	VMS        uint64    `json:"vms"`
	Threads    int32     `json:"threads"`
	Nice       int32     `json:"nice"`
	CreateTime time.Time `json:"createTime"`
	CWD        string    `json:"cwd,omitempty"`
	Exe        string    `json:"exe,omitempty"`
}

type Table struct{}

func NewTable() *Table { return &Table{} }

// List walks the process table. Errors on individual processes are ignored:
// a short-lived process disappearing mid-scan is normal, not a failure of the
// whole listing.
func (t *Table) List(ctx context.Context, limit int, order Order) ([]Process, error) {
	procs, err := process.ProcessesWithContext(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Process, 0, len(procs))
	for _, p := range procs {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		row := Process{PID: p.Pid}
		row.Name, _ = p.NameWithContext(ctx)
		if row.Name == "" {
			continue
		}
		row.PPID, _ = p.PpidWithContext(ctx)
		row.Username, _ = p.UsernameWithContext(ctx)
		if cmd, err := p.CmdlineWithContext(ctx); err == nil {
			row.Cmdline = cmd
		}
		if st, err := p.StatusWithContext(ctx); err == nil && len(st) > 0 {
			row.Status = strings.Join(st, ",")
		}
		row.CPUPercent, _ = p.CPUPercentWithContext(ctx)
		row.CPUPercent = round2(row.CPUPercent)
		if mp, err := p.MemoryPercentWithContext(ctx); err == nil {
			row.MemPercent = round2(float64(mp))
		}
		if mi, err := p.MemoryInfoWithContext(ctx); err == nil && mi != nil {
			row.RSS, row.VMS = mi.RSS, mi.VMS
		}
		row.Threads, _ = p.NumThreadsWithContext(ctx)
		row.Nice, _ = p.NiceWithContext(ctx)
		if ct, err := p.CreateTimeWithContext(ctx); err == nil {
			row.CreateTime = time.UnixMilli(ct).UTC()
		}
		out = append(out, row)
	}
	SortBy(out, order)
	// The cut happens after the sort, which is what makes the limit mean "the
	// heaviest N" rather than "the first N the kernel happened to list".
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// Order is what "heaviest" means for a particular question.
//
// "What is eating my CPU" and "what is eating my RAM" are asked about equally
// often and have completely different answers — a leaking service can sit at
// 0% CPU while holding six gigabytes, and sorting only by CPU puts it below
// two hundred rows of nothing.
type Order string

const (
	ByCPU    Order = "cpu"
	ByMemory Order = "memory"
)

// ParseOrder reads the sort parameter, defaulting to CPU rather than
// rejecting an unknown value: this decides which rows a table shows, and a
// 400 is a worse answer than the usual one.
func ParseOrder(s string) Order {
	if s == string(ByMemory) {
		return ByMemory
	}
	return ByCPU
}

// SortBy orders the table, using the other measure as the tie-break so a run
// of processes all reporting 0% CPU still comes back in a stable, meaningful
// order rather than in whatever order /proc was read.
func SortBy(rows []Process, order Order) {
	sort.Slice(rows, func(i, j int) bool {
		if order == ByMemory {
			if rows[i].RSS != rows[j].RSS {
				return rows[i].RSS > rows[j].RSS
			}
			return rows[i].CPUPercent > rows[j].CPUPercent
		}
		if rows[i].CPUPercent != rows[j].CPUPercent {
			return rows[i].CPUPercent > rows[j].CPUPercent
		}
		return rows[i].RSS > rows[j].RSS
	})
}

func (t *Table) Detail(ctx context.Context, pid int32) (*Process, error) {
	p, err := process.NewProcessWithContext(ctx, pid)
	if err != nil {
		return nil, err
	}
	row := &Process{PID: pid}
	row.Name, _ = p.NameWithContext(ctx)
	row.PPID, _ = p.PpidWithContext(ctx)
	row.Username, _ = p.UsernameWithContext(ctx)
	row.Cmdline, _ = p.CmdlineWithContext(ctx)
	row.Exe, _ = p.ExeWithContext(ctx)
	row.CWD, _ = p.CwdWithContext(ctx)
	if st, err := p.StatusWithContext(ctx); err == nil {
		row.Status = strings.Join(st, ",")
	}
	row.CPUPercent, _ = p.CPUPercentWithContext(ctx)
	if mi, err := p.MemoryInfoWithContext(ctx); err == nil && mi != nil {
		row.RSS, row.VMS = mi.RSS, mi.VMS
	}
	row.Threads, _ = p.NumThreadsWithContext(ctx)
	row.Nice, _ = p.NiceWithContext(ctx)
	if ct, err := p.CreateTimeWithContext(ctx); err == nil {
		row.CreateTime = time.UnixMilli(ct).UTC()
	}
	return row, nil
}

var allowedSignals = map[string]syscall.Signal{
	"SIGTERM": syscall.SIGTERM,
	"SIGKILL": syscall.SIGKILL,
	"SIGINT":  syscall.SIGINT,
	"SIGHUP":  syscall.SIGHUP,
	"SIGUSR1": syscall.SIGUSR1,
	"SIGUSR2": syscall.SIGUSR2,
	"SIGSTOP": syscall.SIGSTOP,
	"SIGCONT": syscall.SIGCONT,
}

// Signal delivers a signal to one process. PID 1 is refused outright: on a
// normal host that is init, and on a containerised deployment it is the
// dashboard's own supervisor — either way, signalling it takes the machine
// down and is never what the operator meant to click.
func (t *Table) Signal(ctx context.Context, pid int32, sig string) error {
	if pid <= 1 {
		return fmt.Errorf("refusing to signal pid %d", pid)
	}
	if pid == int32(os.Getpid()) {
		return fmt.Errorf("refusing to signal the dashboard's own process")
	}
	signal, ok := allowedSignals[strings.ToUpper(sig)]
	if !ok {
		return fmt.Errorf("signal %q is not permitted", sig)
	}
	p, err := process.NewProcessWithContext(ctx, pid)
	if err != nil {
		return fmt.Errorf("no process with pid %d", pid)
	}
	return p.SendSignalWithContext(ctx, signal)
}

func round2(f float64) float64 { return float64(int64(f*100+0.5)) / 100 }

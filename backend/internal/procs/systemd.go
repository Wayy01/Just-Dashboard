package procs

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Unit struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	LoadState   string `json:"loadState"`
	ActiveState string `json:"activeState"`
	SubState    string `json:"subState"`
	Following   string `json:"following"`
	UnitFile    string `json:"unitFileState"`
	Enabled     bool   `json:"enabled"`
	MainPID     int    `json:"mainPid,omitempty"`
	Memory      uint64 `json:"memoryBytes,omitempty"`
	Tasks       int    `json:"tasks,omitempty"`
	SinceUnix   int64  `json:"activeSince,omitempty"`
	Fragment    string `json:"fragmentPath,omitempty"`
	Result      string `json:"result,omitempty"`
	Restarts    int    `json:"restarts,omitempty"`
}

type Systemd struct{}

func NewSystemd() *Systemd { return &Systemd{} }

func (s *Systemd) Available() bool { return binaryExists("systemctl") }

// List returns every service unit, loaded or not. `--all` matters: a failed or
// inactive unit is exactly what an operator opens this page to find, and the
// default listing hides them.
func (s *Systemd) List(ctx context.Context) ([]Unit, error) {
	if !s.Available() {
		return nil, fmt.Errorf("systemctl %w", ErrNotInstalled)
	}
	res, err := run(ctx, 30*time.Second, "systemctl",
		"list-units", "--type=service", "--all", "--no-pager", "--no-legend", "--output=json")
	if err != nil {
		return nil, err
	}
	var raw []struct {
		Unit        string `json:"unit"`
		Load        string `json:"load"`
		Active      string `json:"active"`
		Sub         string `json:"sub"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal([]byte(res.Stdout), &raw); err != nil {
		return nil, fmt.Errorf("parse systemctl output: %w", err)
	}
	enabled := s.enabledStates(ctx)
	out := make([]Unit, 0, len(raw))
	for _, r := range raw {
		u := Unit{
			Name: r.Unit, Description: r.Description,
			LoadState: r.Load, ActiveState: r.Active, SubState: r.Sub,
		}
		if state, ok := enabled[r.Unit]; ok {
			u.UnitFile = state
			u.Enabled = state == "enabled" || state == "enabled-runtime" || state == "static"
		}
		out = append(out, u)
	}
	// Failed units first: they are why someone opened this page.
	sort.Slice(out, func(i, j int) bool {
		rank := func(u Unit) int {
			switch u.ActiveState {
			case "failed":
				return 0
			case "activating", "deactivating":
				return 1
			case "active":
				return 2
			default:
				return 3
			}
		}
		if rank(out[i]) != rank(out[j]) {
			return rank(out[i]) < rank(out[j])
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func (s *Systemd) enabledStates(ctx context.Context) map[string]string {
	states := map[string]string{}
	res, err := run(ctx, 30*time.Second, "systemctl",
		"list-unit-files", "--type=service", "--no-pager", "--no-legend", "--output=json")
	if err != nil {
		return states
	}
	var raw []struct {
		UnitFile string `json:"unit_file"`
		State    string `json:"state"`
	}
	if json.Unmarshal([]byte(res.Stdout), &raw) != nil {
		return states
	}
	for _, r := range raw {
		states[r.UnitFile] = r.State
	}
	return states
}

// Show pulls the detailed properties for one unit. `systemctl show` emits
// key=value lines, which is a stable interface across systemd versions where
// the JSON output for show is not.
func (s *Systemd) Show(ctx context.Context, name string) (*Unit, map[string]string, error) {
	if err := ValidateName(name); err != nil {
		return nil, nil, err
	}
	res, err := run(ctx, 20*time.Second, "systemctl", "show", name, "--no-pager")
	if err != nil {
		return nil, nil, err
	}
	props := map[string]string{}
	for _, line := range strings.Split(res.Stdout, "\n") {
		k, v, ok := strings.Cut(line, "=")
		if ok {
			props[k] = v
		}
	}
	u := &Unit{
		Name:        props["Id"],
		Description: props["Description"],
		LoadState:   props["LoadState"],
		ActiveState: props["ActiveState"],
		SubState:    props["SubState"],
		Following:   props["Following"],
		UnitFile:    props["UnitFileState"],
		Fragment:    props["FragmentPath"],
		Result:      props["Result"],
	}
	u.Enabled = u.UnitFile == "enabled" || u.UnitFile == "enabled-runtime" || u.UnitFile == "static"
	u.MainPID = atoi(props["MainPID"])
	u.Tasks = atoi(props["TasksCurrent"])
	u.Restarts = atoi(props["NRestarts"])
	if mem, err := strconv.ParseUint(props["MemoryCurrent"], 10, 64); err == nil {
		u.Memory = mem
	}
	// systemd reports the activation timestamp in microseconds since epoch.
	if ts, err := strconv.ParseInt(props["ActiveEnterTimestampMonotonic"], 10, 64); err == nil && ts > 0 {
		if realtime, err := strconv.ParseInt(props["ActiveEnterTimestamp"], 10, 64); err == nil && realtime > 0 {
			u.SinceUnix = realtime / 1_000_000
		}
	}
	if u.Name == "" {
		u.Name = name
	}
	return u, props, nil
}

func atoi(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return n
}

type UnitAction string

const (
	UnitStart   UnitAction = "start"
	UnitStop    UnitAction = "stop"
	UnitRestart UnitAction = "restart"
	UnitReload  UnitAction = "reload"
	UnitEnable  UnitAction = "enable"
	UnitDisable UnitAction = "disable"
)

func (s *Systemd) Control(ctx context.Context, name string, action UnitAction) (*CommandResult, error) {
	if err := ValidateName(name); err != nil {
		return nil, err
	}
	switch action {
	case UnitStart, UnitStop, UnitRestart, UnitReload, UnitEnable, UnitDisable:
	default:
		return nil, fmt.Errorf("unknown systemd action %q", action)
	}
	return run(ctx, 90*time.Second, "systemctl", string(action), name)
}

// JournalEntry is one record from journalctl's JSON output. The interesting
// fields are normalised out of the __-prefixed noise the journal emits.
type JournalEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Message   string    `json:"message"`
	Priority  int       `json:"priority"`
	Unit      string    `json:"unit,omitempty"`
	PID       string    `json:"pid,omitempty"`
	Hostname  string    `json:"hostname,omitempty"`
	Syslog    string    `json:"syslogIdentifier,omitempty"`
}

// JournalCommand builds a journalctl invocation. JSON output is used rather
// than the default text so message boundaries and priorities survive
// multi-line log records intact.
func JournalCommand(ctx context.Context, unit string, lines int, follow bool, since string) (*exec.Cmd, error) {
	args := []string{"--output=json", "--no-pager"}
	if unit != "" {
		if err := ValidateName(unit); err != nil {
			return nil, err
		}
		args = append(args, "-u", unit)
	}
	if lines <= 0 || lines > 20000 {
		lines = 300
	}
	args = append(args, "-n", strconv.Itoa(lines))
	if since != "" {
		if !safeTimeSpec.MatchString(since) {
			return nil, fmt.Errorf("%w: since=%q", ErrInvalidName, since)
		}
		args = append(args, "--since", since)
	}
	if follow {
		args = append(args, "-f")
	}
	return exec.CommandContext(ctx, "journalctl", args...), nil
}

// journalctl accepts free-form time expressions ("2 hours ago", "today",
// timestamps). The permitted character set is narrow enough to stay safe while
// still covering the expressions people actually type.
var safeTimeSpec = regexp.MustCompile(`^[A-Za-z0-9 :\-+.]{1,64}$`)

// ParseJournalLine converts one JSON record into a JournalEntry.
func ParseJournalLine(line []byte) (JournalEntry, bool) {
	var raw map[string]any
	if err := json.Unmarshal(line, &raw); err != nil {
		return JournalEntry{}, false
	}
	e := JournalEntry{
		Message:  journalString(raw["MESSAGE"]),
		Unit:     journalString(raw["_SYSTEMD_UNIT"]),
		PID:      journalString(raw["_PID"]),
		Hostname: journalString(raw["_HOSTNAME"]),
		Syslog:   journalString(raw["SYSLOG_IDENTIFIER"]),
	}
	if p, err := strconv.Atoi(journalString(raw["PRIORITY"])); err == nil {
		e.Priority = p
	} else {
		e.Priority = 6
	}
	if us, err := strconv.ParseInt(journalString(raw["__REALTIME_TIMESTAMP"]), 10, 64); err == nil {
		e.Timestamp = time.UnixMicro(us).UTC()
	}
	return e, e.Message != ""
}

// A journal MESSAGE is a string for text records and a byte array for binary
// ones; both shapes have to be handled or binary records break the stream.
func journalString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []any:
		b := make([]byte, 0, len(t))
		for _, n := range t {
			if f, ok := n.(float64); ok {
				b = append(b, byte(int(f)))
			}
		}
		return string(b)
	}
	return ""
}

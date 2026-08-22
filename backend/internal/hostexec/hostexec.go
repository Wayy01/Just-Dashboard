// Package hostexec runs a command that belongs to the host rather than to this
// container.
//
// The dashboard ships the tools it knows it needs, but it cannot ship every
// tool a given server happens to run — nginx is the common case: the config
// lives under a mount this container can read, while the binary that validates
// and reloads it exists only on the host. Installing a copy inside the image
// would be worse than useless, because a second nginx with a different build
// would validate against different modules than the one actually serving.
//
// The container already shares the host PID namespace, so PID 1 is the host's
// init and nsenter can borrow its namespaces. That is the same privilege the
// dashboard already holds through the Docker socket and /host; it widens
// nothing. Where the binary is present locally it is preferred, so a plain
// non-container deployment behaves identically.
//
// The package also handles the other half of "run this in the right context":
// dropping to the account that owns a directory, so a tool the dashboard runs
// as root does not leave root-owned files in somebody else's tree.
package hostexec

import (
	"context"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"sync"
	"syscall"
)

// nsenterArgs enters the host's mount, UTS, IPC, network and PID namespaces.
// The mount namespace is the one that matters — it is what makes the host's
// /usr/sbin and its shared libraries visible.
var nsenterArgs = []string{"--target", "1", "--mount", "--uts", "--ipc", "--net", "--pid", "--"}

var (
	hostOnce sync.Once
	hostOK   bool
)

// hostReachable reports whether host commands can be run at all. It is false
// on a normal (non-container) install, where they are simply run directly.
func hostReachable() bool {
	hostOnce.Do(func() {
		if _, err := exec.LookPath("nsenter"); err != nil {
			return
		}
		// Only meaningful when PID 1 is something other than this process's
		// own init — that is, when the host PID namespace is shared.
		if _, err := os.Stat("/proc/1/ns/mnt"); err != nil {
			return
		}
		probe := exec.Command("nsenter", append(nsenterArgs, "true")...)
		hostOK = probe.Run() == nil
	})
	return hostOK
}

// LookPath finds a binary in this container, falling back to the host. The
// returned string is only meaningful as "it exists"; use Command to run it,
// since a host path is not resolvable from here.
func LookPath(name string) (string, error) {
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	if hostReachable() {
		if err := exec.Command("nsenter", append(nsenterArgs, "sh", "-c", `command -v "$1" >/dev/null 2>&1`, "sh", name)...).Run(); err == nil {
			return name, nil
		}
	}
	return "", exec.ErrNotFound
}

// Available reports whether a binary can be run either locally or on the host.
func Available(name string) bool {
	_, err := LookPath(name)
	return err == nil
}

// OnHost reports whether the binary is missing locally but present on the
// host, which is the case where Command has to cross the namespace boundary.
func OnHost(name string) bool {
	if _, err := exec.LookPath(name); err == nil {
		return false
	}
	return Available(name)
}

// Command builds a command that runs locally when it can and on the host when
// it must. The argument vector is passed through unchanged and never through a
// shell, so the caller's validation still holds.
func Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	if _, err := exec.LookPath(name); err == nil {
		return exec.CommandContext(ctx, name, args...)
	}
	if hostReachable() {
		full := append(append([]string{}, nsenterArgs...), name)
		full = append(full, args...)
		return exec.CommandContext(ctx, "nsenter", full...)
	}
	// Nothing can run it; return the direct form so the caller reports the
	// ordinary "executable not found" rather than an nsenter error.
	return exec.CommandContext(ctx, name, args...)
}

// CommandOnHost always crosses into the host's namespaces, even when the
// binary also exists here. Use it when the command reads host state rather
// than merely being a host tool — `who` is the example: it exists in this
// image, but in here it reports on this container's (empty) session records
// instead of the server's. Falls back to a plain local command when the host
// is not reachable, so a non-container install still works.
func CommandOnHost(ctx context.Context, name string, args ...string) *exec.Cmd {
	return CommandOnHostInDir(ctx, "", name, args...)
}

// CommandOnHostInDir is CommandOnHost starting in a chosen directory.
//
// Setting cmd.Dir is not enough and silently does nothing useful: Go applies
// it in *this* mount namespace, and nsenter then enters the host's and sets
// the working directory to that namespace's root. Every host command therefore
// starts in `/` however it was launched — which is why a terminal asked to
// open in a stack's directory opened in `/` instead. `--wd` is nsenter's own
// answer, applied after it crosses, and is the only thing that survives.
//
// The directory is an argv element, never text for a shell, and the caller is
// expected to have established that it exists.
func CommandOnHostInDir(ctx context.Context, dir, name string, args ...string) *exec.Cmd {
	if !hostReachable() {
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Dir = dir
		return cmd
	}
	full := append([]string{}, nsenterArgs...)
	if dir != "" {
		// Inserted before the `--` that ends nsenter's own options.
		full = append(full[:len(full)-1], "--wd="+dir, "--")
	}
	full = append(full, name)
	full = append(full, args...)
	return exec.CommandContext(ctx, "nsenter", full...)
}

// AvailableOnHost reports whether the host has a binary, ignoring this
// container's own copy.
//
// The lookup needs a shell because `command -v` is a builtin, so the name is
// passed as a positional argument rather than interpolated into the script —
// every caller passes a literal today, and this keeps that from mattering if
// one ever stops.
//
// This is the right question for anything that manages a host service. The
// dashboard image ships fail2ban so it can drive one, but a binary sitting in
// this image says nothing about whether the server runs the service — and
// answering "available" from the image alone turns a server with no fail2ban
// into a dashboard reporting an inexplicably broken one. When the host is not
// reachable as a separate namespace, this is an ordinary local lookup.
func AvailableOnHost(name string) bool {
	if !hostReachable() {
		_, err := exec.LookPath(name)
		return err == nil
	}
	return exec.Command("nsenter", append(nsenterArgs, "sh", "-c", `command -v "$1" >/dev/null 2>&1`, "sh", name)...).Run() == nil
}

// Owner identifies the account that owns a path.
type Owner struct {
	UID, GID uint32
	Name     string
	Home     string
}

// OwnerOf reports the account that owns a path, when this process is root and
// the path belongs to somebody else. It returns false when there is nothing to
// drop to — not running as root, or the path is already ours — and the caller
// then simply runs as itself.
func OwnerOf(dir string) (Owner, bool) {
	if os.Geteuid() != 0 {
		return Owner{}, false
	}
	fi, err := os.Stat(dir)
	if err != nil {
		return Owner{}, false
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok || st.Uid == 0 {
		return Owner{}, false
	}
	o := Owner{UID: st.Uid, GID: st.Gid, Home: "/tmp", Name: strconv.FormatUint(uint64(st.Uid), 10)}
	if u, err := user.LookupId(o.Name); err == nil {
		o.Name = u.Username
		if u.HomeDir != "" {
			o.Home = u.HomeDir
		}
	}
	return o, true
}

// AsOwner makes cmd run as the account that owns cmd.Dir, appending the
// matching HOME/USER to its environment. It is a no-op when there is nothing
// to drop to.
//
// Two things depend on this. Anything the command writes stays owned by the
// account that owns the rest of the tree, rather than becoming root-owned and
// unusable by the service that lives there. And tools that read per-user
// configuration — git's safe.directory check, ssh's keys — see the account
// they should, instead of root's empty home.
func AsOwner(cmd *exec.Cmd) {
	owner, ok := OwnerOf(cmd.Dir)
	if !ok {
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{Uid: owner.UID, Gid: owner.GID},
	}
	if cmd.Env == nil {
		cmd.Env = os.Environ()
	}
	cmd.Env = append(cmd.Env, "HOME="+owner.Home, "USER="+owner.Name, "LOGNAME="+owner.Name)
}

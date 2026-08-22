package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wayy01/Just-Dashboard/backend/internal/auth"
	"github.com/Wayy01/Just-Dashboard/backend/internal/dockerx"
	"github.com/Wayy01/Just-Dashboard/backend/internal/httpx"
)

// Creating a container is the strongest thing the Docker socket can do, and
// two fields on the spec turn "may run a container" into "owns the server":
// privileged, and a bind mount of a path from the host. The route cannot see
// either — they are in the body — so the check is in the handler, the same
// shape dbx.Classify takes for SQL.
//
// These pin that check. A regression here is not a bug in a form, it is the
// `limited` role acquiring root on the machine.

func specRequest(t *testing.T, role auth.Role) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/docker/containers", nil)
	p := &httpx.Principal{Role: role, Kind: "session", User: &auth.User{ID: 1, Username: "tester"}}
	return r.WithContext(httpx.WithPrincipal(r.Context(), p))
}

func TestAuthoriseSpecRefusesPrivilegedBelowAdmin(t *testing.T) {
	s := testServer(t)
	spec := dockerx.ContainerSpec{Image: "nginx", Privileged: true}
	err := s.authoriseSpec(specRequest(t, auth.RoleLimited), &spec)
	if err == nil {
		t.Fatal("a limited role must not be able to create a privileged container")
	}
	if !strings.Contains(err.Error(), "administrator") {
		t.Errorf("the refusal should say what is needed, got %q", err)
	}
}

func TestAuthoriseSpecRefusesBindMountBelowAdmin(t *testing.T) {
	s := testServer(t)
	spec := dockerx.ContainerSpec{
		Image:  "nginx",
		Mounts: []dockerx.MountSpec{{Type: "bind", Source: "/", Target: "/host"}},
	}
	if err := s.authoriseSpec(specRequest(t, auth.RoleLimited), &spec); err == nil {
		t.Fatal("a limited role must not be able to mount a host path into a container")
	}
}

func TestAuthoriseSpecRefusesCapabilitiesAndDevicesBelowAdmin(t *testing.T) {
	s := testServer(t)
	for name, spec := range map[string]dockerx.ContainerSpec{
		"capabilities": {Image: "nginx", CapAdd: []string{"SYS_ADMIN"}},
		"devices":      {Image: "nginx", Devices: []dockerx.DeviceSpec{{Host: "/dev/sda"}}},
		"host network": {Image: "nginx", NetworkMode: "host"},
	} {
		candidate := spec
		if err := s.authoriseSpec(specRequest(t, auth.RoleLimited), &candidate); err == nil {
			t.Errorf("%s must need an administrator", name)
		}
	}
}

// A managed volume is the safe way to give a container storage, and is exactly
// what the UI steers a newcomer towards. Requiring admin for it would push
// them to a bind mount instead, which is the opposite of the point.
func TestAuthoriseSpecAllowsNamedVolumeForLimited(t *testing.T) {
	s := testServer(t)
	spec := dockerx.ContainerSpec{
		Image:  "postgres:16",
		Mounts: []dockerx.MountSpec{{Type: "volume", Source: "pgdata", Target: "/var/lib/postgresql/data"}},
		Ports:  []dockerx.PortMapping{{HostIP: "127.0.0.1", HostPort: 5432, ContainerPort: 5432}},
	}
	if err := s.authoriseSpec(specRequest(t, auth.RoleLimited), &spec); err != nil {
		t.Fatalf("a named volume needs no special privilege: %v", err)
	}
}

// An admin's bind mount is still confined to JD_FILE_ROOTS. The roots are what
// the operator configured as "the parts of this server the dashboard may
// touch", and a container mount is the dashboard handing a piece of the
// filesystem to something else — narrowing the roots would be pointless if it
// were bypassable one route over.
func TestAuthoriseSpecConfinesAdminBindMountsToFileRoots(t *testing.T) {
	s := testServer(t)
	root := s.Cfg.FileRoots[0]

	outside := dockerx.ContainerSpec{
		Image:  "nginx",
		Mounts: []dockerx.MountSpec{{Type: "bind", Source: "/etc", Target: "/host-etc"}},
	}
	if err := s.authoriseSpec(specRequest(t, auth.RoleAdmin), &outside); err == nil {
		t.Fatal("a bind mount outside the configured file roots must be refused even for an admin")
	}

	inside := dockerx.ContainerSpec{
		Image:  "nginx",
		Mounts: []dockerx.MountSpec{{Type: "bind", Source: filepath.Join(root, "site"), Target: "/usr/share/nginx/html"}},
	}
	if err := s.authoriseSpec(specRequest(t, auth.RoleAdmin), &inside); err != nil {
		t.Fatalf("a bind mount inside the roots should be allowed for an admin: %v", err)
	}
	// Resolved in place, so what reaches the daemon is the checked path
	// rather than the string the client sent.
	if !strings.HasPrefix(inside.Mounts[0].Source, root) {
		t.Errorf("source = %q, want it resolved under %q", inside.Mounts[0].Source, root)
	}
}

// The compose actions that interrupt a running service must be the same set on
// the POST routes and on the streaming socket, or the socket becomes a way to
// skip the typed confirmation.
func TestComposeDestructiveSet(t *testing.T) {
	destructive := []dockerx.ComposeAction{
		dockerx.ComposeDown, dockerx.ComposeStop, dockerx.ComposeRestart,
		dockerx.ComposeUpdate, dockerx.ComposeRecreate,
	}
	for _, a := range destructive {
		if !composeIsDestructive(a) {
			t.Errorf("%q interrupts running services and must require confirmation", a)
		}
	}
	for _, a := range []dockerx.ComposeAction{dockerx.ComposeUp, dockerx.ComposeStart, dockerx.ComposePull, dockerx.ComposeBuild} {
		if composeIsDestructive(a) {
			t.Errorf("%q starts or fetches things and should not demand a typed phrase", a)
		}
	}
}

func TestStackNameValidation(t *testing.T) {
	for _, ok := range []string{"shop", "my-stack", "app_2"} {
		if !validStackName(ok) {
			t.Errorf("%q should be a valid stack name", ok)
		}
	}
	for _, bad := range []string{"", "-leading", "Upper", "with space", "../escape", strings.Repeat("a", 65)} {
		if validStackName(bad) {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func TestUnderAnyRoot(t *testing.T) {
	roots := []string{"/opt/stacks", "/srv"}
	for _, in := range []string{"/opt/stacks/shop", "/srv", "/srv/app/nested"} {
		if !underAnyRoot(in, roots) {
			t.Errorf("%q is under a configured root", in)
		}
	}
	// The prefix test has to be path-aware: /opt/stacks-evil shares a string
	// prefix with /opt/stacks and is a different directory.
	for _, out := range []string{"/opt/stacks-evil", "/home/user", "/srvx"} {
		if underAnyRoot(out, roots) {
			t.Errorf("%q is not under any configured root", out)
		}
	}
}

// files.Resolve reads an empty path as the first configured root, so a blank
// source would silently become a mount of the whole of it. The check has to
// happen before the resolve, not after.
func TestAuthoriseSpecRejectsBlankBindSource(t *testing.T) {
	s := testServer(t)
	for _, source := range []string{"", "   ", "relative/path"} {
		spec := dockerx.ContainerSpec{
			Image:  "nginx",
			Mounts: []dockerx.MountSpec{{Type: "bind", Source: source, Target: "/data"}},
		}
		if err := s.authoriseSpec(specRequest(t, auth.RoleAdmin), &spec); err == nil {
			t.Errorf("a bind source of %q must be refused", source)
		}
	}
}

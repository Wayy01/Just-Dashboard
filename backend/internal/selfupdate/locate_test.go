package selfupdate

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// checkout builds a directory that looks like an install: a git checkout with
// a compose file in it.
func checkout(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte("name: just-dashboard\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func lister(containers ...Sibling) Lister {
	return func(context.Context) ([]Sibling, error) { return containers, nil }
}

// The data directory is the signal that identifies this dashboard's own
// container, because it is the directory holding the database this process has
// open. Everything else on a busy host can look similar; that cannot.
func TestLocateFindsItselfByItsDataDirectory(t *testing.T) {
	dir := checkout(t)
	list := lister(
		Sibling{Name: "someone-elses-app", Service: "backend", WorkDir: "/opt/other",
			Mounts: []Mount{{Source: "/opt/other/data", Destination: "/data"}}},
		Sibling{Name: "just-dashboard-backend-1", Service: "backend",
			Image: "just-dashboard-backend:latest", WorkDir: dir,
			Mounts: []Mount{{Source: "/var/lib/jd", Destination: "/var/lib/jd"}}},
	)
	loc, err := Locate(context.Background(), "", "/var/lib/jd", list)
	if err != nil {
		t.Fatal(err)
	}
	if loc.Dir != dir {
		t.Fatalf("located %s, want %s", loc.Dir, dir)
	}
	if loc.Image != "just-dashboard-backend:latest" {
		t.Fatalf("image %q — the updater would run the wrong thing", loc.Image)
	}
	if loc.Compose != "docker-compose.yml" {
		t.Fatalf("compose file %q", loc.Compose)
	}
}

// Without a data-directory match there is still the weaker rule, which is what
// answers on an install whose data directory was mounted from somewhere this
// listing does not show.
func TestLocateFallsBackToTheComposeService(t *testing.T) {
	dir := checkout(t)
	list := lister(
		Sibling{Name: "redis", Service: "redis", Image: "redis:7"},
		Sibling{Name: "jd-backend", Service: "backend", Image: "just-dashboard-backend:latest", WorkDir: dir},
	)
	loc, err := Locate(context.Background(), "", "", list)
	if err != nil {
		t.Fatal(err)
	}
	if loc.Dir != dir {
		t.Fatalf("located %s, want %s", loc.Dir, dir)
	}
}

// Two candidates and no way to tell them apart is a case where guessing is
// worse than asking: picking the wrong one would rebuild somebody else's
// stack. The error has to name the way out.
func TestLocateRefusesToGuessBetweenTwoInstalls(t *testing.T) {
	a, b := checkout(t), checkout(t)
	list := lister(
		Sibling{Name: "one", Service: "backend", Image: "just-dashboard-backend:latest", WorkDir: a},
		Sibling{Name: "two", Service: "backend", Image: "just-dashboard-backend:latest", WorkDir: b},
	)
	_, err := Locate(context.Background(), "", "", list)
	if err == nil {
		t.Fatal("picked one of two indistinguishable installs")
	}
	if !strings.Contains(err.Error(), "JD_UPDATE_DIR") {
		t.Fatalf("error %q does not say how to resolve it", err)
	}
}

// The compose config_files label is compose's own record of which file made
// the stack, and is right even where nothing would have guessed the name.
func TestLocateUsesTheComposeFileTheStackWasMadeFrom(t *testing.T) {
	dir := checkout(t)
	if err := os.WriteFile(filepath.Join(dir, "stack.yml"), []byte("name: jd\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	list := lister(Sibling{
		Name: "jd", Service: "backend", Image: "just-dashboard-backend:latest",
		WorkDir: dir, ConfigFiles: []string{filepath.Join(dir, "stack.yml")},
	})
	loc, err := Locate(context.Background(), "", "", list)
	if err != nil {
		t.Fatal(err)
	}
	if loc.Compose != "stack.yml" {
		t.Fatalf("compose file %q, want stack.yml", loc.Compose)
	}
}

// A dashboard installed some other way is a perfectly good install. It simply
// has nothing to pull or rebuild, and the whole point of reporting that
// precisely is that the UI can say so instead of offering a button that fails.
func TestLocateRefusesSomethingThatIsNotAnInstall(t *testing.T) {
	t.Run("not a checkout", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := Locate(context.Background(), dir, "", nil)
		if err == nil || !strings.Contains(err.Error(), "not a git checkout") {
			t.Fatalf("error %v does not name the missing repository", err)
		}
	})
	t.Run("no compose file", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
		_, err := Locate(context.Background(), dir, "", nil)
		if err == nil || !strings.Contains(err.Error(), "docker-compose.yml") {
			t.Fatalf("error %v does not name the missing compose file", err)
		}
	})
	t.Run("no containers at all", func(t *testing.T) {
		_, err := Locate(context.Background(), "", "", lister())
		if err == nil || !strings.Contains(err.Error(), "JD_UPDATE_DIR") {
			t.Fatalf("error %v does not say how to resolve it", err)
		}
	})
	t.Run("no docker", func(t *testing.T) {
		_, err := Locate(context.Background(), "", "", nil)
		if err == nil {
			t.Fatal("located an install with no way to look for one")
		}
	})
}

// An explicit directory is believed, and is the escape hatch for every install
// this cannot recognise.
func TestLocateBelievesAnExplicitDirectory(t *testing.T) {
	dir := checkout(t)
	loc, err := Locate(context.Background(), dir, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if loc.Dir != dir {
		t.Fatalf("located %s, want %s", loc.Dir, dir)
	}
	if loc.Image != fallbackImage {
		t.Fatalf("image %q, want the documented default when the listing cannot say", loc.Image)
	}
}

// The listing reports a bare image ID once the tag has moved off the running
// container's image, and that ID is the one thing on the machine that a
// containerd-store daemon will collect out from under a running container. So
// it is not a reference the updater may be given: the pinned name is.
//
// This is what "could not start the updater: No such image: sha256:…" was,
// reported to an operator whose dashboard was working perfectly at the time.
func TestLocateWillNotRunTheUpdaterFromAnImageID(t *testing.T) {
	dir := checkout(t)
	for _, id := range []string{
		"sha256:8980bbe27b5066bc1fe1c0d6b46a27119f206df484209451acea07c08d912086",
		"8980bbe27b50",
	} {
		list := lister(Sibling{
			Name: "just-dashboard-backend-1", Service: "backend", Image: id, WorkDir: dir,
			Mounts: []Mount{{Source: "/var/lib/jd", Destination: "/var/lib/jd"}},
		})
		loc, err := Locate(context.Background(), "", "/var/lib/jd", list)
		if err != nil {
			t.Fatal(err)
		}
		if loc.Image != fallbackImage {
			t.Fatalf("image %q for a listing reporting %q — the updater cannot run an ID that has been collected", loc.Image, id)
		}
	}
}

// And the other direction, which is the ordinary case: a name is a name, even
// one this project did not choose, and must be passed through untouched.
func TestLocateKeepsAnImageName(t *testing.T) {
	dir := checkout(t)
	for _, name := range []string{
		"just-dashboard-backend:latest",
		"ghcr.io/wayy01/just-dashboard-backend:0.6",
		"deadbeef.example.com/backend:latest",
	} {
		list := lister(Sibling{
			Name: "just-dashboard-backend-1", Service: "backend", Image: name, WorkDir: dir,
			Mounts: []Mount{{Source: "/var/lib/jd", Destination: "/var/lib/jd"}},
		})
		loc, err := Locate(context.Background(), "", "/var/lib/jd", list)
		if err != nil {
			t.Fatal(err)
		}
		if loc.Image != name {
			t.Fatalf("image %q, want %q — the operator's own name was replaced", loc.Image, name)
		}
	}
}

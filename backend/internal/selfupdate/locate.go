package selfupdate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Working out where this dashboard was installed from.
//
// The upgrade is `git pull` and `docker compose up --build` in the directory
// the operator cloned into, and nothing in this process knows where that is.
// Asking them to configure it would mean every install carrying a setting that
// is already recorded somewhere more reliable — so the dashboard asks Docker
// where its own stack was deployed from, which is a question Docker has always
// been able to answer and which nothing in this class of software asks.
//
// The answer is a label compose writes onto every container it creates. The
// only real work here is deciding *which* container is this one, on a machine
// that may be running dozens.

// Mount is one bind mount of a container, as this package needs to see it.
type Mount struct {
	Source      string
	Destination string
}

// Sibling is one container on this host. It is this package's own shape rather
// than dockerx's so that locating an install is testable without a Docker
// daemon — which matters, because every bug this file can have is a bug in
// picking the wrong container out of a list.
type Sibling struct {
	ID          string
	Name        string
	Image       string
	State       string
	Project     string
	Service     string
	WorkDir     string
	ConfigFiles []string
	Mounts      []Mount
}

// Lister returns every container on the host, running or not.
type Lister func(ctx context.Context) ([]Sibling, error)

// Location is everything the installer needs to carry out an upgrade.
type Location struct {
	// Dir is the checkout, as a **host** path. It is not necessarily a path
	// this process can open: the updater container bind-mounts it by this
	// name, and Docker resolves bind mounts against the host's filesystem, so
	// the host path is the one that has to be recorded.
	Dir string
	// Visible is the same directory as this process can reach it, which is
	// either Dir itself or Dir under the /host mount. Used for the checks that
	// happen before an upgrade is offered at all.
	Visible string
	// Compose is the compose file to rebuild with, relative to Dir.
	Compose string
	// Image is the backend's own image, which is what the updater container
	// runs: it already carries git, the docker CLI and the compose plugin, and
	// it is on the machine by definition — no pull, no second image to keep
	// current, and nothing to go wrong on a server with no registry access.
	Image   string
	Project string
	// Container is the backend container this was worked out from, reported so
	// an operator can see what the dashboard concluded about itself.
	Container string
}

// hostMount is where docker-compose.yml mounts the host's root inside the
// backend container. It is what lets this process check a checkout that lives
// outside the handful of directories mounted at their real names.
const hostMount = "/host"

// Locate works out where this install lives.
//
// explicitDir short-circuits everything: an operator who has moved their
// checkout, or who runs the dashboard some way this cannot recognise, sets
// JD_UPDATE_DIR and is believed. Otherwise the container is identified by the
// one thing that is unmistakably ours — the bind mount backing our own data
// directory — and only then by the weaker "a compose service called backend
// running an image with our name in it".
func Locate(ctx context.Context, explicitDir, dataDir string, list Lister) (*Location, error) {
	if explicitDir != "" {
		loc := &Location{Dir: filepath.Clean(explicitDir), Compose: "docker-compose.yml"}
		visible, ok := visiblePath(loc.Dir)
		if !ok {
			return nil, fmt.Errorf("JD_UPDATE_DIR is set to %s, which this container cannot see", loc.Dir)
		}
		loc.Visible = visible
		if name, ok := composeFileIn(visible); ok {
			loc.Compose = name
		}
		if err := validCheckout(loc); err != nil {
			return nil, err
		}
		// The image is still worth discovering: it is what the updater runs.
		if self := findSelf(ctx, dataDir, list); self != nil {
			loc.Image, loc.Project, loc.Container = namedImage(self.Image), self.Project, self.Name
		}
		if loc.Image == "" {
			loc.Image = fallbackImage
		}
		return loc, nil
	}

	self := findSelf(ctx, dataDir, list)
	if self == nil {
		return nil, fmt.Errorf("could not find this dashboard's own container, so there is nothing to work out " +
			"where it was installed from; set JD_UPDATE_DIR to the directory you cloned into")
	}
	if self.WorkDir == "" {
		return nil, fmt.Errorf("container %s carries no compose project directory, so this dashboard was not started "+
			"by docker compose; set JD_UPDATE_DIR to the directory you cloned into", self.Name)
	}
	loc := &Location{
		Dir:       filepath.Clean(self.WorkDir),
		Image:     namedImage(self.Image),
		Project:   self.Project,
		Container: self.Name,
		Compose:   "docker-compose.yml",
	}
	// The config_files label is compose's own answer to "which file made this",
	// and is right even when the operator used -f with a name nothing guesses.
	for _, f := range self.ConfigFiles {
		if f = strings.TrimSpace(f); f != "" {
			loc.Compose = relativeTo(loc.Dir, f)
			break
		}
	}
	visible, ok := visiblePath(loc.Dir)
	if !ok {
		return nil, fmt.Errorf("this dashboard was deployed from %s, which this container cannot see; "+
			"mount that directory into the backend, or set JD_UPDATE_DIR", loc.Dir)
	}
	loc.Visible = visible
	if err := validCheckout(loc); err != nil {
		return nil, err
	}
	if loc.Image == "" {
		loc.Image = fallbackImage
	}
	return loc, nil
}

// fallbackImage is the name docker-compose.yml gives the backend. It is only
// reached when the container listing could not say, which means the upgrade is
// about to be attempted on an install this code does not fully recognise —
// guessing the documented name is better than refusing outright.
const fallbackImage = "just-dashboard-backend:latest"

// namedImage returns the listing's image reference if it is a name, and ""
// if it is a bare image ID — which the caller treats as "the listing could not
// say" and answers with fallbackImage.
//
// Docker's container listing does not report the reference a container was
// created with. It reports the *tag*, and only while that tag still points at
// the same image; the moment a rebuild moves just-dashboard-backend:latest
// onto the new build, the still-running container's own image is untagged and
// this field silently becomes sha256:… instead.
//
// That is not a weaker reference, it is a reference to the one image on the
// machine with nothing holding it. On the containerd image store an untagged
// image is collected even while a container runs from it — the container keeps
// its unpacked snapshot and never notices, which is why the dashboard goes on
// working — and `docker run sha256:…` then fails with "No such image" at
// exactly the moment the operator asks for an upgrade. The classic image store
// refuses that delete, so this failed only on newer daemons, and only after a
// rebuild that did not recreate the backend.
//
// The name the compose file pins is the better answer in every case where the
// two differ: it is a tag, so it moves *with* the rebuilds rather than being
// orphaned by them, and the updater only needs an image carrying git and
// compose — not this exact build of one.
func namedImage(image string) string {
	if strings.HasPrefix(image, "sha256:") || isHex(image) {
		return ""
	}
	return image
}

// isHex reports whether s is a bare image ID written without its algorithm —
// which is how the listing renders a short digest, and is never a valid
// reference to hand to `docker run`.
func isHex(s string) bool {
	if len(s) < 12 {
		return false
	}
	for _, r := range s {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return false
		}
	}
	return true
}

// findSelf picks this dashboard's backend container out of the listing.
func findSelf(ctx context.Context, dataDir string, list Lister) *Sibling {
	if list == nil {
		return nil
	}
	all, err := list(ctx)
	if err != nil || len(all) == 0 {
		return nil
	}
	// The strongest signal available: a container bind-mounting *our* data
	// directory is this dashboard, because that directory holds the database
	// this process has open. Two dashboards on one host have two data
	// directories, so this stays decisive where a service name would not.
	if dataDir != "" {
		want := filepath.Clean(dataDir)
		var hits []Sibling
		for _, c := range all {
			for _, m := range c.Mounts {
				if filepath.Clean(m.Destination) == want {
					hits = append(hits, c)
					break
				}
			}
		}
		if len(hits) == 1 {
			return &hits[0]
		}
		// More than one is the compose project plus something else mounting
		// the same path; fall through to the service-name rule rather than
		// picking arbitrarily.
		if len(hits) > 1 {
			for i := range hits {
				if hits[i].Service == "backend" {
					return &hits[i]
				}
			}
		}
	}
	var named []Sibling
	for _, c := range all {
		if c.Service == "backend" && strings.Contains(c.Image, "just-dashboard") {
			named = append(named, c)
		}
	}
	if len(named) == 1 {
		return &named[0]
	}
	return nil
}

// visiblePath maps a host path to one this process can open, which is either
// the path itself (the directories docker-compose.yml mounts at their real
// names) or the same path under /host (everywhere else).
func visiblePath(dir string) (string, bool) {
	if dir == "" {
		return "", false
	}
	if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
		return dir, true
	}
	under := filepath.Join(hostMount, dir)
	if fi, err := os.Stat(under); err == nil && fi.IsDir() {
		return under, true
	}
	return "", false
}

var composeNames = []string{
	"docker-compose.yml", "docker-compose.yaml",
	"compose.yml", "compose.yaml",
}

func composeFileIn(dir string) (string, bool) {
	for _, name := range composeNames {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return name, true
		}
	}
	return "", false
}

// relativeTo turns compose's absolute config-file path back into the name to
// pass with -f, so an upgrade run from the project directory addresses the
// same file compose did.
func relativeTo(dir, file string) string {
	if rel, err := filepath.Rel(dir, file); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return file
}

// validCheckout refuses the two shapes that cannot be upgraded in place: a
// directory that is not a git checkout (there is nothing to pull) and one with
// no compose file (there is nothing to rebuild). Both are reported as what
// they are, because the operator in each case installed some other way and
// needs to be told so rather than shown a button that fails.
func validCheckout(loc *Location) error {
	if fi, err := os.Stat(filepath.Join(loc.Visible, ".git")); err != nil || (!fi.IsDir() && fi.Size() == 0) {
		return fmt.Errorf("%s is not a git checkout, so there is nothing to pull; "+
			"in-app updates need the repository you installed from", loc.Dir)
	}
	if _, err := os.Stat(filepath.Join(loc.Visible, loc.Compose)); err != nil {
		return fmt.Errorf("%s has no %s, so this install cannot be rebuilt from it", loc.Dir, loc.Compose)
	}
	return nil
}

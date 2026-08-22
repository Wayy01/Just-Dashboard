package dockerx

import (
	"strings"
	"testing"
)

// The rendered command is shown to the operator as "this is exactly what
// creating it will do", and is meant to be copied and run. A rendering that is
// merely close is worse than none at all, so these pin the parts that decide
// whether it is still the same command.

func sampleSpec() ContainerSpec {
	return ContainerSpec{
		Name:          "shop-db",
		Image:         "postgres:16-alpine",
		RestartPolicy: "unless-stopped",
		Env:           []EnvVar{{Name: "POSTGRES_PASSWORD", Value: "hunter2"}},
		Ports:         []PortMapping{{HostIP: "127.0.0.1", HostPort: 5432, ContainerPort: 5432, Protocol: "tcp"}},
		Mounts:        []MountSpec{{Type: "volume", Source: "shop-db-data", Target: "/var/lib/postgresql/data"}},
		Limits:        ResourceLimits{MemoryMB: 512, CPUs: 1.5},
		Networks:      []string{"shop"},
	}
}

// The image is the single most important token in the line. It used to be
// appended to the last flag's line — `--memory 512m postgres:16-alpine` — where
// it is the easiest thing on screen to miss.
func TestRunCommandPutsImageOnItsOwnLine(t *testing.T) {
	got := sampleSpec().RunCommand()
	lines := strings.Split(got, "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	if last != "postgres:16-alpine" {
		t.Errorf("the image should be the last line on its own, got %q\nfull command:\n%s", last, got)
	}
	for _, line := range lines {
		if strings.Contains(line, "--memory") && strings.Contains(line, "postgres") {
			t.Errorf("the image must not share a line with a flag: %q", line)
		}
	}
}

func TestRunCommandCarriesTheChoicesThatMatter(t *testing.T) {
	got := sampleSpec().RunCommand()
	for _, want := range []string{
		"--name shop-db",
		"--restart unless-stopped",
		// The bind address is the difference between a database on the
		// internet and one that is not; losing it in the rendering would make
		// the command a lie in the most expensive direction.
		"--publish 127.0.0.1:5432:5432",
		"--mount type=volume,source=shop-db-data,target=/var/lib/postgresql/data",
		"--memory 512m",
		"--cpus 1.5",
		"--network shop",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

// A value with a space or a quote in it has to survive being pasted into a
// shell, or the command does something different from what the form said.
func TestRunCommandQuotesAwkwardValues(t *testing.T) {
	spec := ContainerSpec{
		Image: "alpine",
		Env: []EnvVar{
			{Name: "GREETING", Value: "hello world"},
			{Name: "QUOTED", Value: "it's fine"},
		},
	}
	got := spec.RunCommand()
	if !strings.Contains(got, `--env 'GREETING=hello world'`) {
		t.Errorf("a value with a space must be quoted:\n%s", got)
	}
	if !strings.Contains(got, `'QUOTED=it'\''s fine'`) {
		t.Errorf("a single quote must be escaped, not dropped:\n%s", got)
	}
}

func TestComposeServiceShape(t *testing.T) {
	got := sampleSpec().ComposeService("db")
	for _, want := range []string{
		"services:",
		"  db:",
		"    image: postgres:16-alpine",
		"    container_name: shop-db",
		"    restart: unless-stopped",
		`      - "127.0.0.1:5432:5432"`,
		`      - "shop-db-data:/var/lib/postgresql/data"`,
		"          memory: 512M",
		"volumes:",
		"    external: true",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	// A volume marked external that does not exist yet is a loud error at
	// deploy time; the file has to say how to get the other behaviour.
	if !strings.Contains(got, "# These already exist on this server") {
		t.Errorf("the external volume choice must explain itself in the file:\n%s", got)
	}
}

// YAML's implicit typing is the classic way a config means something other
// than what was written: an unquoted `no` is false, and an unquoted version
// number is a float.
func TestComposeQuotesAmbiguousValues(t *testing.T) {
	spec := ContainerSpec{
		Image: "alpine",
		Env: []EnvVar{
			{Name: "ENABLED", Value: "no"},
			{Name: "VERSION", Value: "1.20"},
			{Name: "PLAIN", Value: "alpha"},
		},
	}
	got := spec.ComposeService("app")
	for _, want := range []string{`ENABLED: "no"`, `VERSION: "1.20"`, `PLAIN: alpha`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestImageRefDefaultsTheTag(t *testing.T) {
	cases := map[string]string{
		"nginx":                "nginx:latest",
		"nginx:alpine":         "nginx:alpine",
		"ghcr.io/org/app":      "ghcr.io/org/app:latest",
		"ghcr.io/org/app:1.2":  "ghcr.io/org/app:1.2",
		"registry:5000/app":    "registry:5000/app:latest",
		"registry:5000/app:v1": "registry:5000/app:v1",
	}
	for in, want := range cases {
		if got := ImageRef(in); got != want {
			t.Errorf("ImageRef(%q) = %q, want %q", in, got, want)
		}
	}
}

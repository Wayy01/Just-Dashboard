package dockerx

import (
	"slices"
	"strings"
	"testing"
)

func TestBuildxCommandKeepsSecretsOutOfArgvAndScrubsDashboardEnvironment(t *testing.T) {
	t.Setenv("JD_MASTER_KEY", "must-not-enter-build")
	t.Setenv("VPSD_LEGACY_SECRET", "must-not-enter-build-either")
	options := ImmutableBuildOptions{
		Dockerfile: ".just-dashboard/Dockerfile", Tag: "just-dashboard/test:run-7",
		Platform: "linux/amd64", NoCache: true, Pull: true,
		Secrets: []BuildxSecret{
			{ID: "Z_TOKEN", Value: "z-super-secret"},
			{ID: "A_TOKEN", Value: "a-super-secret"},
		},
	}
	argv, environment, err := BuildxCommand(options, "/tmp/build-metadata.json")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"buildx", "build", "--progress=plain", "--file", ".just-dashboard/Dockerfile",
		"--tag", "just-dashboard/test:run-7", "--load", "--metadata-file", "/tmp/build-metadata.json",
		"--pull", "--platform", "linux/amd64", "--no-cache",
		"--secret", "id=A_TOKEN,env=JD_DEPLOY_BUILDKIT_SECRET_0",
		"--secret", "id=Z_TOKEN,env=JD_DEPLOY_BUILDKIT_SECRET_1", ".",
	}
	if !slices.Equal(argv, want) {
		t.Fatalf("argv = %#v\nwant %#v", argv, want)
	}
	joinedArgs := strings.Join(argv, "\x00")
	if strings.Contains(joinedArgs, "super-secret") {
		t.Fatalf("secret entered argv: %q", joinedArgs)
	}
	joinedEnvironment := strings.Join(environment, "\x00")
	for _, absent := range []string{"JD_MASTER_KEY=", "VPSD_LEGACY_SECRET="} {
		if strings.Contains(joinedEnvironment, absent) {
			t.Fatalf("dashboard secret environment survived scrub: %s", absent)
		}
	}
	for _, present := range []string{
		"JD_DEPLOY_BUILDKIT_SECRET_0=a-super-secret",
		"JD_DEPLOY_BUILDKIT_SECRET_1=z-super-secret",
		"BUILDKIT_PROGRESS=plain", "DOCKER_CLI_HINTS=false",
	} {
		if !strings.Contains(joinedEnvironment, present) {
			t.Fatalf("build environment missing %s", present)
		}
	}
}

func TestBuildxCommandRejectsDuplicateOrMalformedSecretAndPlatform(t *testing.T) {
	for name, options := range map[string]ImmutableBuildOptions{
		"duplicate secret": {
			Dockerfile: "Dockerfile", Tag: "test:tag",
			Secrets: []BuildxSecret{{ID: "TOKEN"}, {ID: "TOKEN"}},
		},
		"malformed secret": {
			Dockerfile: "Dockerfile", Tag: "test:tag",
			Secrets: []BuildxSecret{{ID: "BAD,ID"}},
		},
		"multiple platforms": {
			Dockerfile: "Dockerfile", Tag: "test:tag", Platform: "linux/amd64,linux/arm64",
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := BuildxCommand(options, "/tmp/metadata"); err == nil {
				t.Fatal("unsafe Buildx command was accepted")
			}
		})
	}
}

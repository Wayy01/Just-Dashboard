package deploy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeImagesAndComposeOverridesUseImmutableIdentities(t *testing.T) {
	configDigest := fakeContentDigest("local-image-config")
	manifestDigest := fakeContentDigest("registry-manifest")
	if got := immutableRuntimeImage(ResolvedImage{
		Reference: "registry.example/app:latest", Digest: manifestDigest, ConfigDigest: configDigest,
	}); got != configDigest {
		t.Fatalf("local immutable image = %q, want config digest", got)
	}
	if got := immutableRuntimeImage(ResolvedImage{
		Reference: "registry.example/app:latest", Digest: manifestDigest,
	}); got != "registry.example/app:latest@"+manifestDigest {
		t.Fatalf("remote immutable image = %q", got)
	}

	request := CandidateRuntimeRequest{
		Run: EngineRun{ID: 41}, Release: Release{ID: 71, EnvironmentID: 9, Number: 3},
		Snapshot: runtimeReleaseSnapshot{Compose: &ResolvedComposeSnapshot{Services: []ResolvedComposeService{
			{Plan: ComposeServicePlan{Name: "api"}, Reference: "example/api:current", Digest: manifestDigest, ConfigDigest: configDigest},
			{Plan: ComposeServicePlan{Name: "worker"}, Reference: "example/worker:current", Digest: fakeContentDigest("worker")},
		}}},
	}
	override, err := renderComposeReleaseOverride(request)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"image: \"" + configDigest + "\"", "example/worker:current@" + fakeContentDigest("worker"),
		"pull_policy: never", "io.just-dashboard.release-id: \"71\"",
	} {
		if !strings.Contains(override, required) {
			t.Fatalf("override omitted %q:\n%s", required, override)
		}
	}
	if strings.Contains(override, "latest") || strings.Contains(override, "TOKEN") {
		t.Fatalf("override retained a mutable tag or variable: %s", override)
	}
}

func TestImmutableRuntimeFileRejectsDifferentRetryBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".just-dashboard", "release.yml")
	if err := writeImmutableRuntimeFile(path, "services: {}\n"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("runtime file mode = %v, error=%v", info.Mode().Perm(), err)
	}
	if err := writeImmutableRuntimeFile(path, "services: {}\n"); err != nil {
		t.Fatalf("idempotent write: %v", err)
	}
	if err := writeImmutableRuntimeFile(path, "services:\n  changed: {}\n"); err == nil {
		t.Fatal("runtime override accepted different retry bytes")
	}
}

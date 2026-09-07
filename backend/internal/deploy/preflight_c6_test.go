package deploy

import (
	"context"
	"encoding/json"
	"net"
	"strconv"
	"strings"
	"testing"

	"github.com/Wayy01/Just-Dashboard/backend/internal/dockerx"
	"github.com/Wayy01/Just-Dashboard/backend/internal/proxysvc"
)

func TestC6PreflightSurfacesNetworkFirewallAndDependencyGates(t *testing.T) {
	candidate := newDetectedCandidate("", BuildNone, DetectedCandidate{
		Name: "existing", Profile: ProfileImported, Confidence: ConfidenceHigh,
		Evidence: []DetectionEvidence{}, NeedsDecision: []string{},
	})
	draft := &Draft{Data: DraftData{
		Intent: &DraftIntentConfig{Name: "existing", Profile: ProfileImported},
		Source: &DraftSourceConfig{Kind: SourceImport, Mode: SourceModeExistingContainer, ResourceID: "runtime-1"},
		Detection: &DetectionResult{
			Source:     SourceIdentity{Kind: SourceImport, Repository: "runtime-1"},
			Candidates: []DetectedCandidate{candidate}, SelectedID: candidate.ID,
		},
	}}
	configuration := PlanConfiguration{
		Build: BuildPlanConfig{Method: BuildNone},
		Runtime: RuntimePlanConfig{
			Strategy: StrategyStopFirst, BindAddress: "0.0.0.0", HostPort: 8443,
			Command: []string{}, Capabilities: []string{}, Devices: []string{}, Mounts: []RuntimeMount{},
		},
		Variables: []PlannedVariable{}, Checks: []PlannedCheck{},
		Domains: []PlannedDomain{{Hostname: "app.example.test", HTTPS: true, Ownership: OwnershipManaged}},
		Dependencies: []PlannedDependency{
			{Kind: "database", Ownership: OwnershipLinked, ResourceKind: "database", ResourceID: "12", Config: json.RawMessage(`{}`)},
			{Kind: "storage", Ownership: OwnershipObserved, ResourceKind: "volume", ResourceID: "shared", Config: json.RawMessage(`{}`)},
			{Kind: "backup", Ownership: OwnershipLinked, ResourceKind: "backup_job", ResourceID: "42", Config: json.RawMessage(`{"maxAgeSeconds":3600}`)},
		},
	}
	observation := HostObservation{
		Facilities: map[string]FacilityObservation{"docker": {Available: true}},
		Ports:      []PortObservation{{Address: "0.0.0.0", Port: 8443, Protocol: "tcp", InUse: true, Detail: "claimed by another runtime"}},
		Domains: []DomainObservation{{
			Hostname: "app.example.test", ProxyAvailable: true, Conflict: true,
			DNSAvailable: false, CertificateAvailable: false, CertificateAutomation: true,
			Detail: "lookup timed out",
		}},
		Firewall: FirewallObservation{Available: true, Enabled: true, Allows: false, Backend: "ufw"},
		Dependencies: []DependencyObservation{
			{Kind: "database", ResourceKind: "database", ResourceID: "12", Available: false, Detail: "database is stopped", DeepLink: "/databases/12"},
			{Kind: "storage", ResourceKind: "volume", ResourceID: "shared", Available: false, Detail: "volume was not found", DeepLink: "/docker/volumes/shared"},
			{Kind: "backup", ResourceKind: "backup_job", ResourceID: "42", Available: true, Fresh: false, Status: "last success is stale", DeepLink: "/backups/42"},
		},
	}

	findings := preflightFindings(draft, configuration, observation, false)
	assertC6Finding(t, findings, "port_conflict", PreflightBlocked, "")
	assertC6Finding(t, findings, "domain_conflict", PreflightBlocked, "/proxy/sites")
	assertC6Finding(t, findings, "dns_unverified", PreflightUnavailable, "/proxy/sites")
	assertC6Finding(t, findings, "certificate_unavailable", PreflightBlocked, "/certificates")
	assertC6Finding(t, findings, "public_bind", PreflightWarning, "")
	assertC6Finding(t, findings, "firewall_mismatch", PreflightBlocked, "/security?tab=firewall")
	assertC6Finding(t, findings, "database_unavailable", PreflightBlocked, "/databases/12")
	assertC6Finding(t, findings, "storage_unavailable", PreflightDecision, "/docker/volumes/shared")
	assertC6Finding(t, findings, "backup_stale", PreflightWarning, "/backups/42")
}

func TestExecutionPreflightBlocksFrozenPortConflictBeforeBuild(t *testing.T) {
	fixture := newReleaseStoreFixture(t)
	planRuntime := RuntimePlanConfig{
		InternalPort: 3000, HostPort: 18443, BindAddress: "127.0.0.1", Strategy: StrategyStopFirst,
		Command: []string{}, Capabilities: []string{}, Devices: []string{}, Mounts: []RuntimeMount{},
	}
	fixture.addPlanWithRuntime(t, 1, strings.Repeat("a", 40), planRuntime)
	run, _ := fixture.claimedRun(t, 1)
	plan, err := fixture.runs.ExecutionPlan(context.Background(), *run)
	if err != nil {
		t.Fatal(err)
	}
	observer := &preflightObserverFake{observation: HostObservation{
		Facilities: map[string]FacilityObservation{
			"git": {Available: true}, "docker": {Available: true}, "buildx": {Available: true},
		},
		Ports: []PortObservation{{Address: "127.0.0.1", Port: 18443, Protocol: "tcp", InUse: true}},
	}}
	executor := &NormalizedStepExecutor{store: fixture.runs, preflight: observer}
	result := executor.analyzePlan(context.Background(), StepExecution{Run: *run}, plan)
	if result.State != StepFailed || result.ErrorCode != "port_conflict" || observer.calls != 1 {
		t.Fatalf("execution preflight = %#v, observations=%d", result, observer.calls)
	}
	if len(observer.requests) != 1 || observer.requests[0].ExistingProxySite !=
		"just-dashboard-env-"+strconv.FormatInt(fixture.envID, 10)+".conf" {
		t.Fatalf("execution observation request = %#v", observer.requests)
	}
}

func TestExecutionObservationExcludesOnlyItsOwnManagedProxySiteFromConflicts(t *testing.T) {
	proxy := &planningProxyFake{vhosts: []proxysvc.VHost{{
		Name: "just-dashboard-env-12.conf", ServerNames: []string{"localhost"},
	}}}
	observer := NewHostPreflightObserver([]string{t.TempDir()}, t.TempDir(), nil, proxy)
	request := ObservationRequest{
		Domains:           []PlannedDomain{{Hostname: "localhost", Ownership: OwnershipManaged}},
		ExistingProxySite: "just-dashboard-env-12.conf",
	}
	observation, err := observer.Observe(context.Background(), request)
	if err != nil || len(observation.Domains) != 1 || observation.Domains[0].Conflict {
		t.Fatalf("own route observation = %#v, error=%v", observation.Domains, err)
	}
	proxy.vhosts = append(proxy.vhosts, proxysvc.VHost{Name: "operator-site.conf", ServerNames: []string{"localhost"}})
	observation, err = observer.Observe(context.Background(), request)
	if err != nil || len(observation.Domains) != 1 || !observation.Domains[0].Conflict {
		t.Fatalf("other route observation = %#v, error=%v", observation.Domains, err)
	}
}

func TestExecutionObservationRecognizesPortHeldByItsLiveContainer(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	docker := &planningDockerFake{container: &dockerx.ContainerSpec{Ports: []dockerx.PortMapping{{
		HostIP: "127.0.0.1", HostPort: port, ContainerPort: 3000, Protocol: "tcp",
	}}}}
	observer := NewHostPreflightObserver([]string{t.TempDir()}, t.TempDir(), docker)
	observation, err := observer.Observe(context.Background(), ObservationRequest{
		ExistingRuntimeID: "live-container", ExistingRuntimeKind: "container",
		Ports: []PortObservation{{Address: "127.0.0.1", Port: port, Protocol: "tcp"}},
	})
	if err != nil || len(observation.Ports) != 1 || observation.Ports[0].InUse ||
		!observation.Ports[0].OwnedByDeployment {
		t.Fatalf("live deployment port observation = %#v, error=%v", observation.Ports, err)
	}
}

func assertC6Finding(t *testing.T, findings []PreflightFinding, code string, severity PreflightSeverity, deepLink string) {
	t.Helper()
	for _, finding := range findings {
		if finding.Code != code || finding.Severity != severity {
			continue
		}
		if deepLink != "" && finding.DeepLink != deepLink {
			t.Fatalf("finding %s deep link = %q, want %q", code, finding.DeepLink, deepLink)
		}
		return
	}
	t.Fatalf("missing %s/%s finding in %#v", code, severity, findings)
}

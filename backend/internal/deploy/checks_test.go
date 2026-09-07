package deploy

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type checkBackendFake struct {
	mu       sync.Mutex
	health   []string
	exitCode int
	output   []byte
	err      error
	commands [][]string
}

func (f *checkBackendFake) ContainerHealth(_ context.Context, _ string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.health) == 0 {
		return "", f.err
	}
	status := f.health[0]
	f.health = f.health[1:]
	return status, f.err
}

func (f *checkBackendFake) ExecCheck(_ context.Context, _ string, command []string, _ time.Duration) (int, []byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.commands = append(f.commands, append([]string(nil), command...))
	return f.exitCode, append([]byte(nil), f.output...), f.err
}

func TestCheckRunnerCoversHTTPRetriesTCPDockerCommandAndClosedOutcomes(t *testing.T) {
	t.Parallel()
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		if requests == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	runner := NewCheckRunner(nil)
	httpEvidence := runner.Run(context.Background(), PlannedCheck{
		Name: "ready", Kind: "http", Phase: "readiness", Required: true,
		Config: json.RawMessage(`{"url":"` + server.URL + `","attempts":2,"timeoutSeconds":1}`),
	}, CheckTarget{})
	if httpEvidence.Outcome != HealthPassed || len(httpEvidence.Attempts) != 2 ||
		httpEvidence.Attempts[0].Code != "unexpected_status" || httpEvidence.Attempts[1].StatusCode != http.StatusNoContent {
		t.Fatalf("HTTP evidence = %#v", httpEvidence)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	tcpEvidence := runner.Run(context.Background(), PlannedCheck{
		Name: "socket", Kind: "tcp", Phase: "readiness", Required: true,
		Config: json.RawMessage(`{"attempts":1,"timeoutSeconds":1}`),
	}, CheckTarget{Host: "127.0.0.1", Port: port})
	if tcpEvidence.Outcome != HealthPassed || tcpEvidence.Attempts[0].Address != listener.Addr().String() {
		t.Fatalf("TCP evidence = %#v", tcpEvidence)
	}

	backend := &checkBackendFake{health: []string{"starting", "healthy"}}
	runner = NewCheckRunner(backend)
	dockerEvidence := runner.Run(context.Background(), PlannedCheck{
		Name: "container", Kind: "docker_health", Phase: "readiness", Required: true,
		Config: json.RawMessage(`{"attempts":2,"timeoutSeconds":1}`),
	}, CheckTarget{ContainerID: "candidate"})
	if dockerEvidence.Outcome != HealthPassed || len(dockerEvidence.Attempts) != 2 ||
		dockerEvidence.Attempts[0].ContainerState != "starting" {
		t.Fatalf("Docker health evidence = %#v", dockerEvidence)
	}

	secretOutput := []byte("runtime secret must never be persisted")
	backend = &checkBackendFake{exitCode: 9, output: secretOutput, err: errors.New("exit 9")}
	runner = NewCheckRunner(backend)
	commandEvidence := runner.Run(context.Background(), PlannedCheck{
		Name: "smoke", Kind: "command", Phase: "smoke", Required: false,
		Config: json.RawMessage(`{"command":["app","check"],"attempts":1,"timeoutSeconds":1}`),
	}, CheckTarget{ContainerID: "candidate"})
	encoded, _ := json.Marshal(commandEvidence)
	if commandEvidence.Outcome != HealthWarning || commandEvidence.Attempts[0].ExitCode != 9 ||
		commandEvidence.Attempts[0].OutputDigest == "" || strings.Contains(string(encoded), string(secretOutput)) {
		t.Fatalf("command evidence = %s", encoded)
	}

	disabled := runner.Run(context.Background(), PlannedCheck{
		Name: "off", Kind: "http", Phase: "readiness", Required: true,
		Config: json.RawMessage(`{"disabled":true}`),
	}, CheckTarget{})
	unavailable := NewCheckRunner(nil).Run(context.Background(), PlannedCheck{
		Name: "missing", Kind: "docker_health", Phase: "readiness", Required: true,
		Config: json.RawMessage(`{"attempts":1}`),
	}, CheckTarget{})
	if disabled.Outcome != HealthDisabled || len(disabled.Attempts) != 0 ||
		unavailable.Outcome != HealthUnavailable || summarizeChecks([]CheckEvidence{disabled}) != HealthDisabled ||
		summarizeChecks([]CheckEvidence{commandEvidence}) != HealthWarning {
		t.Fatalf("closed outcomes: disabled=%#v unavailable=%#v", disabled, unavailable)
	}
}

func TestCheckConfigurationRejectsUnknownFieldsCredentialURLsAndSecretArgv(t *testing.T) {
	t.Parallel()
	for _, fixture := range []struct {
		kind string
		raw  string
	}{
		{"http", `{"headers":{"X-Test":"value"}}`},
		{"http", `{"url":"https://user:pass@example.test/health"}`},
		{"http", `{"path":"relative"}`},
		{"command", `{"command":["check","--api-token","plain"]}`},
		{"command", `{"command":[]}`},
		{"tcp", `{"url":"https://example.test"}`},
	} {
		if err := validateCheckConfiguration(fixture.kind, json.RawMessage(fixture.raw)); err == nil {
			t.Fatalf("%s accepted invalid configuration %s", fixture.kind, fixture.raw)
		}
	}
}

func TestCheckRunnerTimeoutIsBoundedAndEvidenceDoesNotExposeTransportError(t *testing.T) {
	t.Parallel()
	runner := NewCheckRunner(nil)
	runner.dial = func(ctx context.Context, _, _ string) (net.Conn, error) {
		<-ctx.Done()
		return nil, errors.New("credential-like upstream error token=do-not-store")
	}
	evidence := runner.Run(context.Background(), PlannedCheck{
		Name: "bounded", Kind: "tcp", Phase: "readiness", Required: true,
		Config: json.RawMessage(`{"host":"127.0.0.1","port":9,"attempts":1,"timeoutSeconds":1}`),
	}, CheckTarget{})
	raw, _ := json.Marshal(evidence)
	if evidence.Outcome != HealthFailed || evidence.Attempts[0].Code != "timeout" ||
		strings.Contains(string(raw), "do-not-store") {
		t.Fatalf("timeout evidence = %s", raw)
	}
}

package deploy

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
)

type adapterTranscript struct {
	Name       string          `json:"name"`
	Owner      string          `json:"owner"`
	Boundary   string          `json:"boundary"`
	WorkingDir string          `json:"workingDir"`
	Argv       []string        `json:"argv"`
	Events     []recordedEvent `json:"events"`
	Outcome    StepState       `json:"outcome"`
	Evidence   json.RawMessage `json:"evidence"`
}

type recordedEvent struct {
	Stream string `json:"stream"`
	Text   string `json:"text"`
}

// recordedAdapter is the deterministic feature-owner fake used by the C1
// orchestration tests. It deliberately replays persisted-shape evidence rather
// than pretending Git, Docker, proxy or sockets share one implementation.
type recordedAdapter struct {
	transcript adapterTranscript
	next       int
}

func (a *recordedAdapter) Next() (recordedEvent, bool) {
	if a.next == len(a.transcript.Events) {
		return recordedEvent{}, false
	}
	event := a.transcript.Events[a.next]
	a.next++
	return event, true
}

func TestC0AdapterTranscriptsCoverEveryExecutionOwner(t *testing.T) {
	raw, err := os.ReadFile("testdata/c0-adapter-transcripts.json")
	if err != nil {
		t.Fatal(err)
	}
	var transcripts []adapterTranscript
	if err := json.Unmarshal(raw, &transcripts); err != nil {
		t.Fatal(err)
	}

	required := []string{"git", "docker", "proxy", "http_check", "tcp_check"}
	seen := make(map[string]bool, len(required))
	for _, transcript := range transcripts {
		seen[transcript.Owner] = true
		if transcript.Name == "" || transcript.Boundary == "" || len(transcript.Evidence) == 0 {
			t.Fatalf("incomplete transcript: %#v", transcript)
		}
		if transcript.Outcome != StepPassed && transcript.Outcome != StepFailed {
			t.Fatalf("%s has non-terminal outcome %q", transcript.Name, transcript.Outcome)
		}
		if len(transcript.Argv) > 0 {
			if transcript.Boundary != "hostexec.CommandInDir" || transcript.WorkingDir == "" {
				t.Fatalf("%s bypasses the frozen host command boundary", transcript.Name)
			}
			if slices.Contains([]string{"sh", "bash", "/bin/sh", "/bin/bash"}, transcript.Argv[0]) {
				t.Fatalf("%s introduced an unapproved shell boundary", transcript.Name)
			}
		}
		encoded := string(transcript.Evidence)
		for _, event := range transcript.Events {
			encoded += event.Text
		}
		if strings.Contains(encoded, "JD_MASTER_KEY") || strings.Contains(encoded, "VPSD_") {
			t.Fatalf("%s records inherited dashboard secrets", transcript.Name)
		}

		fake := recordedAdapter{transcript: transcript}
		for range transcript.Events {
			if _, ok := fake.Next(); !ok {
				t.Fatalf("%s fake stopped before its transcript ended", transcript.Name)
			}
		}
		if _, ok := fake.Next(); ok {
			t.Fatalf("%s fake replayed past its transcript", transcript.Name)
		}
	}
	for _, owner := range required {
		if !seen[owner] {
			t.Errorf("missing recorded adapter for %s", owner)
		}
	}
}

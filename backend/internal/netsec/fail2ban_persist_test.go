package netsec

import (
	"strings"
	"testing"
)

// The override file holds every jail the operator has tuned, so writing one
// must not disturb the others — and must not eat a line somebody added by hand.

func TestMergeJailOverridesCreatesASection(t *testing.T) {
	got := mergeJailOverrides("", "sshd", map[string]string{"bantime": "3600", "maxretry": "3"})
	if !strings.Contains(got, "[sshd]") {
		t.Fatalf("no section:\n%s", got)
	}
	if !strings.Contains(got, "bantime = 3600") || !strings.Contains(got, "maxretry = 3") {
		t.Fatalf("values missing:\n%s", got)
	}
	if !strings.Contains(got, "Written by Just Dashboard") {
		t.Fatalf("the file should explain itself:\n%s", got)
	}
}

func TestMergeJailOverridesUpdatesInPlace(t *testing.T) {
	existing := "[sshd]\nbantime = 600\nmaxretry = 5\n"
	got := mergeJailOverrides(existing, "sshd", map[string]string{"bantime": "3600"})
	if !strings.Contains(got, "bantime = 3600") {
		t.Fatalf("not updated:\n%s", got)
	}
	if strings.Contains(got, "bantime = 600") {
		t.Fatalf("old value left behind:\n%s", got)
	}
	if !strings.Contains(got, "maxretry = 5") {
		t.Fatalf("an untouched value was lost:\n%s", got)
	}
	if strings.Count(got, "[sshd]") != 1 {
		t.Fatalf("section duplicated:\n%s", got)
	}
}

func TestMergeJailOverridesLeavesOtherJailsAlone(t *testing.T) {
	existing := "[sshd]\nbantime = 600\n\n[nginx-botsearch]\nmaxretry = 2\n"
	got := mergeJailOverrides(existing, "sshd", map[string]string{"bantime": "3600"})
	if !strings.Contains(got, "[nginx-botsearch]") || !strings.Contains(got, "maxretry = 2") {
		t.Fatalf("another jail's section was disturbed:\n%s", got)
	}
}

// Somebody may have added a line by hand. Regenerating the section from the
// values being edited would eat it.
func TestMergeJailOverridesKeepsUnmanagedKeys(t *testing.T) {
	existing := "[sshd]\nbantime = 600\naction = %(action_mwl)s\n"
	got := mergeJailOverrides(existing, "sshd", map[string]string{"bantime": "3600"})
	if !strings.Contains(got, "action = %(action_mwl)s") {
		t.Fatalf("a hand-written line was dropped:\n%s", got)
	}
}

func TestMergeJailOverridesAppendsMissingValuesToAnExistingSection(t *testing.T) {
	existing := "[sshd]\nbantime = 600\n"
	got := mergeJailOverrides(existing, "sshd", map[string]string{"bantime": "3600", "findtime": "900"})
	if !strings.Contains(got, "findtime = 900") {
		t.Fatalf("a new value was not added to the existing section:\n%s", got)
	}
	if strings.Count(got, "[sshd]") != 1 {
		t.Fatalf("section duplicated:\n%s", got)
	}
}

func TestSetJailParamsRejectsBadInput(t *testing.T) {
	s := New()
	if _, err := s.SetJailParams(t.Context(), "sshd", nil); err == nil {
		t.Error("accepted an empty change set")
	}
	if _, err := s.SetJailParams(t.Context(), "../etc/passwd", map[string]int{"bantime": 60}); err == nil {
		t.Error("accepted a path as a jail name")
	}
	if _, err := s.SetJailParams(t.Context(), "sshd", map[string]int{"action": 1}); err == nil {
		t.Error("accepted a parameter outside the closed set")
	}
	if _, err := s.SetJailParams(t.Context(), "sshd", map[string]int{"bantime": 1}); err == nil {
		t.Error("accepted a ban time below the bound")
	}
}

// The allowlist had exactly the problem this file exists to fix.
//
// fail2ban-client's addignoreip changes the running server and nothing else,
// so the address an operator allowlists after banning themselves is gone at
// the next restart — silently, with the page still showing it. It goes into
// the same drop-in as the numeric parameters now, which is why the merge takes
// strings rather than integers.
func TestMergeJailOverridesCarriesANonNumericValue(t *testing.T) {
	got := mergeJailOverrides("[sshd]\nbantime = 600\n", "sshd",
		map[string]string{"ignoreip": "127.0.0.1/8 10.0.0.0/8"})
	if !strings.Contains(got, "ignoreip = 127.0.0.1/8 10.0.0.0/8") {
		t.Fatalf("allowlist not written:\n%s", got)
	}
	if !strings.Contains(got, "bantime = 600") {
		t.Fatalf("an unmanaged key in the same section was eaten:\n%s", got)
	}
}

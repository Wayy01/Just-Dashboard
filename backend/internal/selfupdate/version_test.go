package selfupdate

import "testing"

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.5", "0.5", 0},
		{"0.5", "0.5.0", 0},
		{"v0.5", "0.5", 0},
		{"0.5", "0.5.1", -1},
		{"0.5.1", "0.6", -1},
		{"0.6", "0.5.9", 1},
		// The reason this is not string comparison. An install on 0.10 offered
		// "0.5" as an update is the bug this case exists to keep out.
		{"0.10", "0.5", 1},
		{"0.9", "0.10", -1},
		{"1.0", "0.99", 1},
		// Anything unparseable sorts as older, so the failure is an update
		// offered that cannot be described — never an install that silently
		// decides it is ahead of everything.
		{"nightly", "0.5", -1},
		{"nightly", "also-nightly", 0},
	}
	for _, tc := range cases {
		if got := Compare(tc.a, tc.b); got != tc.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
		if got := Compare(tc.b, tc.a); got != -tc.want {
			t.Errorf("Compare(%q, %q) = %d, want %d — comparison is not symmetric", tc.b, tc.a, got, -tc.want)
		}
	}
}

func TestValidVersion(t *testing.T) {
	for _, ok := range []string{"0.5", "0.5.1", "v1.0", "1", "1.2.3.4"} {
		if !ValidVersion(ok) {
			t.Errorf("%q refused", ok)
		}
	}
	for _, bad := range []string{"", "  ", "next", "0.", ".5", "0.5.", "1.2.3.4.5", "-1.0", "0.5-rc1"} {
		if ValidVersion(bad) {
			t.Errorf("%q accepted as a version", bad)
		}
	}
}

func TestNewer(t *testing.T) {
	if !Newer("0.5", "0.5.1") {
		t.Error("0.5.1 is not offered to 0.5")
	}
	if Newer("0.5.1", "0.5") {
		t.Error("0.5 offered as an update to 0.5.1 — that is a downgrade")
	}
	if Newer("0.5", "0.5") {
		t.Error("a version offered as an update to itself")
	}
	if Newer("0.5", "tomorrow") {
		t.Error("an unparseable version offered as an update")
	}
}

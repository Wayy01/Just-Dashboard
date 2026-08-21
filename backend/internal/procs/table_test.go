package procs

import "testing"

// The two orders exist because the two questions have different answers: a
// leaking service can hold gigabytes while sitting at 0% CPU, and a CPU-only
// sort buries it below every busy process on the box.
func TestSortByPutsTheRightRowFirst(t *testing.T) {
	rows := []Process{
		{Name: "busy", CPUPercent: 90, RSS: 10 << 20},
		{Name: "leaky", CPUPercent: 0, RSS: 6 << 30},
		{Name: "idle", CPUPercent: 0, RSS: 1 << 20},
	}

	SortBy(rows, ByCPU)
	if rows[0].Name != "busy" {
		t.Errorf("by cpu, first row = %q, want busy", rows[0].Name)
	}
	// Tie-broken by the other measure, so a run of 0% processes still comes
	// back in a meaningful order rather than /proc's.
	if rows[1].Name != "leaky" {
		t.Errorf("by cpu, second row = %q, want leaky (tie broken by RSS)", rows[1].Name)
	}

	SortBy(rows, ByMemory)
	if rows[0].Name != "leaky" {
		t.Errorf("by memory, first row = %q, want leaky", rows[0].Name)
	}
	if rows[1].Name != "busy" {
		t.Errorf("by memory, second row = %q, want busy (tie broken by CPU)", rows[1].Name)
	}
}

// An unknown sort decides which rows a table shows. Falling back beats a 400,
// which would leave the page with no data at all over a typo in a query string.
func TestParseOrderFallsBackToCPU(t *testing.T) {
	if got := ParseOrder("memory"); got != ByMemory {
		t.Errorf(`ParseOrder("memory") = %q, want memory`, got)
	}
	for _, in := range []string{"", "cpu", "banana", "MEMORY"} {
		if got := ParseOrder(in); got != ByCPU {
			t.Errorf("ParseOrder(%q) = %q, want cpu", in, got)
		}
	}
}

package dockerx

import (
	"testing"

	"github.com/docker/docker/api/types/container"
)

// Docker's one-shot stats endpoint returns precpu_stats zeroed. Subtracting
// zero turns the usual arithmetic into "this container's whole lifetime over
// the host's whole uptime" — an average since start, presented as the reading
// now. A long-lived container that is working hard reads as nearly idle, which
// is the failure this guards.
func TestOneShotSampleClaimsNoPercentageOfItsOwn(t *testing.T) {
	raw := container.StatsResponse{}
	raw.CPUStats.CPUUsage.TotalUsage = 900_000_000_000 // 900s of CPU since start
	raw.CPUStats.SystemUsage = 90_000_000_000_000      // host has been up far longer
	raw.CPUStats.OnlineCPUs = 4

	st := convertStats("abc", raw)
	if st.CPUPercent != 0 {
		t.Fatalf("cpu = %v from a sample with no predecessor; it should report none", st.CPUPercent)
	}
	if st.CPUTotal != raw.CPUStats.CPUUsage.TotalUsage || st.SystemCPU != raw.CPUStats.SystemUsage {
		t.Fatal("the cumulative counters a caller needs to difference were not carried through")
	}
}

// The streaming endpoint does fill precpu_stats, and that sample is complete
// on its own.
func TestStreamedSampleUsesItsOwnPredecessor(t *testing.T) {
	raw := container.StatsResponse{}
	raw.PreCPUStats.CPUUsage.TotalUsage = 100_000_000_000
	raw.PreCPUStats.SystemUsage = 1_000_000_000_000
	raw.CPUStats.CPUUsage.TotalUsage = 110_000_000_000 // 10s of CPU
	raw.CPUStats.SystemUsage = 1_040_000_000_000       // over 40s of host CPU
	raw.CPUStats.OnlineCPUs = 4

	// 10/40 of the host's four cores is one whole core: 100%.
	if got := convertStats("abc", raw).CPUPercent; got != 100 {
		t.Fatalf("cpu = %v, want 100", got)
	}
}

func TestSamplerDerivesPercentFromTwoSamples(t *testing.T) {
	s := (&Client{}).NewStatsSampler()

	first := ContainerStats{ID: "abc", CPUTotal: 100_000_000_000, SystemCPU: 1_000_000_000_000}
	s.fillCPU("abc", &first, 4)
	if first.CPUPercent != 0 {
		t.Fatalf("first sample reported %v%%; there is nothing to difference against yet", first.CPUPercent)
	}

	second := ContainerStats{ID: "abc", CPUTotal: 110_000_000_000, SystemCPU: 1_040_000_000_000}
	s.fillCPU("abc", &second, 4)
	if second.CPUPercent != 100 {
		t.Fatalf("cpu = %v, want 100", second.CPUPercent)
	}
}

// A counter that goes backwards means the host rebooted or the id was reused.
// Reporting the resulting enormous delta as a spike would be worse than
// reporting nothing, and it would be stored for a week.
func TestCounterResetIsNotASpike(t *testing.T) {
	s := (&Client{}).NewStatsSampler()
	first := ContainerStats{ID: "abc", CPUTotal: 500_000_000_000, SystemCPU: 9_000_000_000_000}
	s.fillCPU("abc", &first, 4)

	after := ContainerStats{ID: "abc", CPUTotal: 1_000_000, SystemCPU: 2_000_000}
	s.fillCPU("abc", &after, 4)
	if after.CPUPercent != 0 {
		t.Fatalf("cpu = %v after a counter reset, want 0", after.CPUPercent)
	}
}

// A host that recreates containers often would otherwise carry every dead
// container's counters for the life of the process.
func TestSamplerForgetsContainersItNoLongerSees(t *testing.T) {
	s := (&Client{}).NewStatsSampler()
	for _, id := range []string{"a", "b", "c"} {
		st := ContainerStats{ID: id, CPUTotal: 1, SystemCPU: 1}
		s.fillCPU(id, &st, 1)
	}
	s.forget(map[string]struct{}{"b": {}})

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.prev) != 1 {
		t.Fatalf("holding %d containers, want only the one still running", len(s.prev))
	}
	if _, ok := s.prev["b"]; !ok {
		t.Fatal("forgot the container that is still running")
	}
}

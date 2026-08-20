package metrics

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/Wayy01/Just-Dashboard/backend/internal/store"
	"github.com/Wayy01/Just-Dashboard/backend/internal/sysinfo"
)

func testRecorder(t *testing.T, interval, retention time.Duration) *Recorder {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return New(st, slog.New(slog.NewTextHandler(io.Discard, nil)), interval, retention)
}

func TestParseWindow(t *testing.T) {
	cases := map[string]time.Duration{
		"30s":  30 * time.Second,
		"15m":  15 * time.Minute,
		"6h":   6 * time.Hour,
		"1d":   24 * time.Hour,
		"7d":   7 * 24 * time.Hour,
		"0.5d": 12 * time.Hour,
		"2w":   14 * 24 * time.Hour,
	}
	for in, want := range cases {
		got, err := ParseWindow(in)
		if err != nil {
			t.Fatalf("ParseWindow(%q): %v", in, err)
		}
		if got != want {
			t.Errorf("ParseWindow(%q) = %v, want %v", in, got, want)
		}
	}
	for _, in := range []string{"", "d", "-1d", "0d", "banana", "7 d"} {
		if _, err := ParseWindow(in); err == nil {
			t.Errorf("ParseWindow(%q) accepted a value it should have refused", in)
		}
	}
}

// A bucket narrower than the sampling interval would produce empty buckets
// between real ones, which a chart draws as a gap that never happened.
func TestBucketNeverFinerThanTheData(t *testing.T) {
	step := bucketFor(time.Minute, 600, 15*time.Second)
	if step != 15*time.Second {
		t.Fatalf("bucket = %v, want the 15s sampling interval", step)
	}
	if step := bucketFor(24*time.Hour, 240, 15*time.Second); step != 6*time.Minute {
		t.Fatalf("bucket for a day at 240 points = %v, want 6m", step)
	}
	// A window that does not divide evenly must round up: rounding down would
	// hand back more points than the caller asked for.
	if step := bucketFor(100*time.Second, 3, time.Second); step != 34*time.Second {
		t.Fatalf("bucket = %v, want 34s", step)
	}
}

// The whole point of storing peaks alongside means: a one-sample spike inside
// a wide bucket has to survive downsampling, or the record says the night was
// quiet when it was not.
func TestRangeKeepsPeaksThroughDownsampling(t *testing.T) {
	r := testRecorder(t, 15*time.Second, 24*time.Hour)
	ctx := context.Background()
	base := time.Now().Add(-time.Hour).Truncate(time.Second)

	for i := range 60 {
		cpu := 5.0
		if i == 37 {
			cpu = 99.0 // the spike nobody was watching
		}
		if err := r.record(ctx, Sample{
			TS:         base.Add(time.Duration(i) * 15 * time.Second),
			CPUPercent: cpu,
			NetRx:      float64(i),
		}); err != nil {
			t.Fatal(err)
		}
	}

	series, err := r.Range(ctx, base.Add(-time.Minute), time.Now(), 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(series.Points) == 0 {
		t.Fatal("no points returned")
	}
	if len(series.Points) > 4 {
		t.Fatalf("got %d points, asked for at most 4", len(series.Points))
	}
	var peak, mean float64
	var samples int
	for _, p := range series.Points {
		samples += p.Samples
		if p.CPUPeak > peak {
			peak = p.CPUPeak
		}
		if p.CPU > mean {
			mean = p.CPU
		}
	}
	if samples != 60 {
		t.Fatalf("aggregated %d samples, want 60", samples)
	}
	if peak != 99 {
		t.Fatalf("peak CPU = %v, want the 99%% spike to survive the bucket", peak)
	}
	if mean >= 99 {
		t.Fatalf("mean CPU = %v; the average should have flattened the spike, not carried it", mean)
	}
	if series.Earliest == nil || !series.Earliest.Equal(base.UTC()) {
		t.Fatalf("earliest = %v, want %v", series.Earliest, base.UTC())
	}
	if series.RetentionSeconds != int(24*time.Hour/time.Second) {
		t.Fatalf("retention = %ds", series.RetentionSeconds)
	}
}

func TestRangeIsEmptyRatherThanNilWithoutData(t *testing.T) {
	r := testRecorder(t, 15*time.Second, time.Hour)
	series, err := r.Range(context.Background(), time.Now().Add(-time.Hour), time.Now(), 100)
	if err != nil {
		t.Fatal(err)
	}
	if series.Points == nil {
		t.Fatal("Points is nil; the frontend distinguishes an empty series from a failed one")
	}
	if series.Earliest != nil {
		t.Fatalf("earliest = %v with no rows stored", series.Earliest)
	}
}

func TestPruneDropsExpiredSamples(t *testing.T) {
	r := testRecorder(t, 15*time.Second, time.Hour)
	ctx := context.Background()
	now := time.Now()
	for _, age := range []time.Duration{3 * time.Hour, 2 * time.Hour, 30 * time.Minute, time.Minute} {
		if err := r.record(ctx, Sample{TS: now.Add(-age), CPUPercent: 1}); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.prune(ctx); err != nil {
		t.Fatal(err)
	}
	series, err := r.Range(ctx, now.Add(-24*time.Hour), now, 2000)
	if err != nil {
		t.Fatal(err)
	}
	if len(series.Points) != 2 {
		t.Fatalf("kept %d points, want the 2 inside the retention window", len(series.Points))
	}
}

// Recording twice in one second is what a restart looks like; it must not
// fail the insert, because the sampler would then log an error every tick.
func TestRecordReplacesWithinTheSameSecond(t *testing.T) {
	r := testRecorder(t, 15*time.Second, time.Hour)
	ctx := context.Background()
	at := time.Now().Truncate(time.Second)
	if err := r.record(ctx, Sample{TS: at, CPUPercent: 10}); err != nil {
		t.Fatal(err)
	}
	if err := r.record(ctx, Sample{TS: at, CPUPercent: 80}); err != nil {
		t.Fatal(err)
	}
	series, err := r.Range(ctx, at.Add(-time.Minute), at.Add(time.Minute), 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(series.Points) != 1 || series.Points[0].Samples != 1 {
		t.Fatalf("got %d points; the second write should have replaced the first", len(series.Points))
	}
	if series.Points[0].CPU != 80 {
		t.Fatalf("cpu = %v, want the later value", series.Points[0].CPU)
	}
}

func TestReduceSummarisesTheSnapshot(t *testing.T) {
	s := Reduce(&sysinfo.Snapshot{
		CPU:    sysinfo.CPUStats{TotalPercent: 42, LoadAvg1: 1.5},
		Memory: sysinfo.MemoryStats{UsedPercent: 60, Used: 100, Total: 200},
		Net: []sysinfo.NetStats{
			{RecvRate: 10, SendRate: 1},
			{RecvRate: 5, SendRate: 2},
		},
		Mounts: []sysinfo.MountStats{
			{UsedPercent: 12, ReadRate: 100, WriteRate: 10},
			{UsedPercent: 91, ReadRate: 50, WriteRate: 5},
		},
	})
	if s.NetRx != 15 || s.NetTx != 3 {
		t.Fatalf("network rates = %v/%v, want the sum across interfaces", s.NetRx, s.NetTx)
	}
	if s.DiskRead != 150 || s.DiskWrite != 15 {
		t.Fatalf("disk rates = %v/%v", s.DiskRead, s.DiskWrite)
	}
	// The fullest mount, not the mean: an almost-full disk is the thing worth
	// noticing, and averaging it with an empty one hides it.
	if s.DiskPercent != 91 {
		t.Fatalf("disk percent = %v, want the fullest mount", s.DiskPercent)
	}
}

func TestDisabledRecorderRecordsNothing(t *testing.T) {
	r := testRecorder(t, 15*time.Second, 0)
	if r.Enabled() {
		t.Fatal("retention 0 should disable recording")
	}
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	r.Stop() // must be a no-op rather than a nil-channel receive
}

func TestNewClampsAbsurdIntervals(t *testing.T) {
	if got := testRecorder(t, time.Millisecond, time.Hour).Interval(); got != MinInterval {
		t.Fatalf("interval = %v, want it clamped to %v", got, MinInterval)
	}
	if got := testRecorder(t, time.Hour, time.Hour).Interval(); got != MaxInterval {
		t.Fatalf("interval = %v, want it clamped to %v", got, MaxInterval)
	}
	if got := testRecorder(t, 15*time.Second, time.Minute).Retention(); got != MinRetention {
		t.Fatalf("retention = %v, want it clamped to %v", got, MinRetention)
	}
}

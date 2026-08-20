// Package metrics keeps a durable record of host utilisation.
//
// The dashboard's charts were, until this package existed, drawn entirely from
// a WebSocket the browser opened when the Overview page was mounted. That made
// the graph a view of "since you arrived" rather than a view of the server:
// close the tab, and the history went with it; open it ten hours after the
// last visit, and the load spike that woke the box at 04:00 had never been
// recorded by anything.
//
// So the sampler runs in the backend, on its own timer, whether or not a
// browser is connected. The live socket is still what feeds the two-second
// chart — it is far finer-grained than anything worth storing — but it is now
// the tail of a series that starts at boot instead of the whole of it.
package metrics

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/Wayy01/Just-Dashboard/backend/internal/store"
	"github.com/Wayy01/Just-Dashboard/backend/internal/sysinfo"
)

// Bounds on the sampling interval. Below the floor the write rate stops being
// negligible for no visible gain — the live socket already covers fine detail;
// above the ceiling a short spike falls between two samples and the record
// says nothing happened.
const (
	MinInterval = 5 * time.Second
	MaxInterval = 5 * time.Minute

	// DefaultInterval is a compromise between resolution and rows: at 15s a
	// day costs 5,760 rows, so the default week of retention is ~40k rows and
	// a few megabytes of SQLite.
	DefaultInterval = 15 * time.Second

	// DefaultRetention is how far back the charts can look by default.
	DefaultRetention = 7 * 24 * time.Hour

	// MinRetention keeps a misconfigured tiny value from making the feature
	// pointless without disabling it outright, which is what 0 is for.
	MinRetention = time.Hour
)

// pruneEvery is how often expired rows are deleted. Pruning on every sample
// would run a DELETE every 15 seconds to remove, on average, one row.
const pruneEvery = time.Hour

// Sample is one recorded moment. Rates are per second, averaged over the
// interval since the previous sample, exactly as the live socket reports them.
type Sample struct {
	TS          time.Time `json:"ts"`
	CPUPercent  float64   `json:"cpuPercent"`
	Load1       float64   `json:"load1"`
	MemPercent  float64   `json:"memPercent"`
	MemUsed     uint64    `json:"memUsed"`
	MemTotal    uint64    `json:"memTotal"`
	SwapPercent float64   `json:"swapPercent"`
	NetRx       float64   `json:"netRx"`
	NetTx       float64   `json:"netTx"`
	DiskRead    float64   `json:"diskRead"`
	DiskWrite   float64   `json:"diskWrite"`
	DiskPercent float64   `json:"diskPercent"`
	Uptime      uint64    `json:"uptimeSeconds"`
}

// Point is one bucket of the aggregated series.
//
// Every value carries its peak as well as its mean, because the mean is what
// hides the thing an operator is looking for: a 100% CPU second inside a
// ten-minute bucket averages down to nothing, and a chart that only draws
// means reports a quiet night. The peaks are what make a downsampled week
// still worth reading.
type Point struct {
	TS      time.Time `json:"ts"`
	Samples int       `json:"samples"`

	CPU     float64 `json:"cpu"`
	CPUPeak float64 `json:"cpuPeak"`

	Mem     float64 `json:"mem"`
	MemPeak float64 `json:"memPeak"`

	Swap     float64 `json:"swap"`
	SwapPeak float64 `json:"swapPeak"`

	RX     float64 `json:"rx"`
	RXPeak float64 `json:"rxPeak"`
	TX     float64 `json:"tx"`
	TXPeak float64 `json:"txPeak"`

	DiskRead      float64 `json:"diskRead"`
	DiskReadPeak  float64 `json:"diskReadPeak"`
	DiskWrite     float64 `json:"diskWrite"`
	DiskWritePeak float64 `json:"diskWritePeak"`

	Load1     float64 `json:"load1"`
	Load1Peak float64 `json:"load1Peak"`

	DiskPercent float64 `json:"diskPercent"`
	MemUsed     uint64  `json:"memUsed"`
}

// Series is a window of history plus the facts a client needs to draw it
// honestly: how wide each bucket is (so a gap in the data can be told from a
// flat line), and how far back the record actually goes (so "no data before
// here" can be labelled as such rather than looking like an idle server).
type Series struct {
	From             time.Time  `json:"from"`
	To               time.Time  `json:"to"`
	StepSeconds      int        `json:"stepSeconds"`
	IntervalSeconds  int        `json:"sampleIntervalSeconds"`
	RetentionSeconds int        `json:"retentionSeconds"`
	Earliest         *time.Time `json:"earliest"`
	Points           []Point    `json:"points"`
}

// Recorder samples the host and answers questions about the past.
type Recorder struct {
	db        *sql.DB
	log       *slog.Logger
	interval  time.Duration
	retention time.Duration

	// The recorder keeps its own collector rather than sharing the one the
	// request handlers use. Rates are deltas against the previous call, so a
	// shared collector would have every one-shot GET /system/metrics silently
	// shorten the interval the next recorded rate is divided by.
	col *sysinfo.Collector

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

// New builds a recorder. An interval or retention outside the supported band
// is clamped rather than rejected: this is a monitoring nicety, and refusing
// to boot the whole dashboard over it would be out of proportion. A retention
// of zero disables recording entirely, which is the supported way to opt out.
func New(st *store.Store, log *slog.Logger, interval, retention time.Duration) *Recorder {
	if interval <= 0 {
		interval = DefaultInterval
	}
	interval = clamp(interval, MinInterval, MaxInterval)
	if retention > 0 && retention < MinRetention {
		retention = MinRetention
	}
	return &Recorder{
		db:        st.DB,
		log:       log,
		interval:  interval,
		retention: retention,
		col:       sysinfo.NewCollector(),
	}
}

// Enabled reports whether history is being recorded at all.
func (r *Recorder) Enabled() bool { return r.retention > 0 }

func (r *Recorder) Interval() time.Duration  { return r.interval }
func (r *Recorder) Retention() time.Duration { return r.retention }

// Start begins sampling. It returns as soon as the loop is running; a failure
// to write one sample is logged and retried on the next tick rather than
// killing the dashboard, since metrics history is not what the process is for.
func (r *Recorder) Start(parent context.Context) error {
	if !r.Enabled() {
		r.log.Info("metrics history disabled", "reason", "JD_METRICS_RETENTION=0")
		return nil
	}
	r.mu.Lock()
	if r.cancel != nil {
		r.mu.Unlock()
		return nil
	}
	ctx, cancel := context.WithCancel(parent)
	r.cancel = cancel
	done := make(chan struct{})
	r.done = done
	r.mu.Unlock()

	// Prime the collector so the first stored sample carries real rates
	// instead of the zeroes a first delta always produces.
	if _, err := r.col.Collect(ctx); err != nil {
		r.log.Warn("metrics recorder could not prime counters", "error", err)
	}
	if err := r.prune(ctx); err != nil {
		r.log.Warn("metrics prune failed", "error", err)
	}

	go r.loop(ctx, done)
	r.log.Info("recording metrics history",
		"interval", r.interval.String(), "retention", r.retention.String())
	return nil
}

// Stop ends sampling and waits for the loop to leave, so a shutdown does not
// race a write against the closing database handle.
func (r *Recorder) Stop() {
	r.mu.Lock()
	cancel, done := r.cancel, r.done
	r.cancel, r.done = nil, nil
	r.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	<-done
}

func (r *Recorder) loop(ctx context.Context, done chan struct{}) {
	defer close(done)
	t := time.NewTicker(r.interval)
	defer t.Stop()
	lastPrune := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := r.sample(ctx); err != nil && ctx.Err() == nil {
				r.log.Warn("metrics sample failed", "error", err)
			}
			if time.Since(lastPrune) >= pruneEvery {
				lastPrune = time.Now()
				if err := r.prune(ctx); err != nil && ctx.Err() == nil {
					r.log.Warn("metrics prune failed", "error", err)
				}
			}
		}
	}
}

func (r *Recorder) sample(ctx context.Context) error {
	snap, err := r.col.Collect(ctx)
	if err != nil {
		return err
	}
	return r.record(ctx, Reduce(snap))
}

// Reduce flattens a full snapshot into the handful of series worth keeping for
// months. Per-core percentages, per-interface counters and per-mount inode
// counts are all in the live feed and none of them survive a downsampled
// chart, so storing them would cost rows for a view nothing renders.
func Reduce(snap *sysinfo.Snapshot) Sample {
	s := Sample{
		TS:          snap.TS,
		CPUPercent:  snap.CPU.TotalPercent,
		Load1:       snap.CPU.LoadAvg1,
		MemPercent:  snap.Memory.UsedPercent,
		MemUsed:     snap.Memory.Used,
		MemTotal:    snap.Memory.Total,
		SwapPercent: snap.Swap.UsedPercent,
		Uptime:      snap.Uptime,
	}
	for _, n := range snap.Net {
		s.NetRx += n.RecvRate
		s.NetTx += n.SendRate
	}
	for _, m := range snap.Mounts {
		s.DiskRead += m.ReadRate
		s.DiskWrite += m.WriteRate
		// The fullest mount, not the sum: "is a disk about to fill up" is the
		// question, and averaging a full /boot with an empty /srv answers it
		// wrongly in the reassuring direction.
		if m.UsedPercent > s.DiskPercent {
			s.DiskPercent = m.UsedPercent
		}
	}
	return s
}

// record stores one sample. A second sample landing in the same second (a
// restart, an interval below the resolution of the key) replaces the earlier
// one rather than failing the insert.
func (r *Recorder) record(ctx context.Context, s Sample) error {
	ts := s.TS
	if ts.IsZero() {
		ts = time.Now()
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO metric_samples
		  (ts, cpu_percent, load1, mem_percent, mem_used, mem_total, swap_percent,
		   net_rx, net_tx, disk_read, disk_write, disk_percent, uptime_seconds)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(ts) DO UPDATE SET
		  cpu_percent    = excluded.cpu_percent,
		  load1          = excluded.load1,
		  mem_percent    = excluded.mem_percent,
		  mem_used       = excluded.mem_used,
		  mem_total      = excluded.mem_total,
		  swap_percent   = excluded.swap_percent,
		  net_rx         = excluded.net_rx,
		  net_tx         = excluded.net_tx,
		  disk_read      = excluded.disk_read,
		  disk_write     = excluded.disk_write,
		  disk_percent   = excluded.disk_percent,
		  uptime_seconds = excluded.uptime_seconds`,
		ts.Unix(), s.CPUPercent, s.Load1, s.MemPercent, int64(s.MemUsed), int64(s.MemTotal),
		s.SwapPercent, s.NetRx, s.NetTx, s.DiskRead, s.DiskWrite, s.DiskPercent, int64(s.Uptime))
	return err
}

func (r *Recorder) prune(ctx context.Context) error {
	if !r.Enabled() {
		return nil
	}
	cutoff := time.Now().Add(-r.retention).Unix()
	_, err := r.db.ExecContext(ctx, `DELETE FROM metric_samples WHERE ts < ?`, cutoff)
	return err
}

// Range returns the recorded window [from, to] reduced to at most maxPoints
// buckets.
//
// The reduction happens in SQL rather than by shipping every row to the
// client: a week at 15s is 40,000 samples, and no chart 900 pixels wide has
// anything to do with them.
func (r *Recorder) Range(ctx context.Context, from, to time.Time, maxPoints int) (*Series, error) {
	if to.Before(from) {
		from, to = to, from
	}
	if maxPoints < 1 {
		maxPoints = 1
	}
	step := bucketFor(to.Sub(from), maxPoints, r.interval)

	series := &Series{
		From:             from.UTC(),
		To:               to.UTC(),
		StepSeconds:      int(step / time.Second),
		IntervalSeconds:  int(r.interval / time.Second),
		RetentionSeconds: int(r.retention / time.Second),
		Points:           []Point{},
	}
	if earliest, ok, err := r.earliest(ctx); err != nil {
		return nil, err
	} else if ok {
		series.Earliest = &earliest
	}

	secs := int64(step / time.Second)
	rows, err := r.db.QueryContext(ctx, `
		SELECT (ts / ?) * ?                          AS bucket,
		       COUNT(*),
		       AVG(cpu_percent),  MAX(cpu_percent),
		       AVG(mem_percent),  MAX(mem_percent),
		       AVG(swap_percent), MAX(swap_percent),
		       AVG(net_rx),       MAX(net_rx),
		       AVG(net_tx),       MAX(net_tx),
		       AVG(disk_read),    MAX(disk_read),
		       AVG(disk_write),   MAX(disk_write),
		       AVG(load1),        MAX(load1),
		       AVG(disk_percent), AVG(mem_used)
		  FROM metric_samples
		 WHERE ts >= ? AND ts <= ?
		 GROUP BY bucket
		 ORDER BY bucket`,
		secs, secs, from.Unix(), to.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var bucket int64
		var p Point
		var memUsed float64
		if err := rows.Scan(&bucket, &p.Samples,
			&p.CPU, &p.CPUPeak,
			&p.Mem, &p.MemPeak,
			&p.Swap, &p.SwapPeak,
			&p.RX, &p.RXPeak,
			&p.TX, &p.TXPeak,
			&p.DiskRead, &p.DiskReadPeak,
			&p.DiskWrite, &p.DiskWritePeak,
			&p.Load1, &p.Load1Peak,
			&p.DiskPercent, &memUsed); err != nil {
			return nil, err
		}
		p.TS = time.Unix(bucket, 0).UTC()
		p.MemUsed = uint64(memUsed)
		round(&p)
		series.Points = append(series.Points, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return series, nil
}

func (r *Recorder) earliest(ctx context.Context) (time.Time, bool, error) {
	var ts sql.NullInt64
	if err := r.db.QueryRowContext(ctx, `SELECT MIN(ts) FROM metric_samples`).Scan(&ts); err != nil {
		return time.Time{}, false, err
	}
	if !ts.Valid {
		return time.Time{}, false, nil
	}
	return time.Unix(ts.Int64, 0).UTC(), true, nil
}

// bucketFor picks a bucket width that fits the window into maxPoints without
// ever going below the sampling interval — a bucket finer than the data is a
// chart with holes in it that mean nothing.
func bucketFor(window time.Duration, maxPoints int, interval time.Duration) time.Duration {
	if window <= 0 {
		return interval
	}
	step := window / time.Duration(maxPoints)
	if step < interval {
		step = interval
	}
	// Round up to a whole second: the key is unix seconds, so a fractional
	// bucket would silently truncate to something narrower than requested.
	if step%time.Second != 0 {
		step = (step/time.Second + 1) * time.Second
	}
	if step < time.Second {
		step = time.Second
	}
	return step
}

func round(p *Point) {
	p.CPU, p.CPUPeak = round1(p.CPU), round1(p.CPUPeak)
	p.Mem, p.MemPeak = round1(p.Mem), round1(p.MemPeak)
	p.Swap, p.SwapPeak = round1(p.Swap), round1(p.SwapPeak)
	p.RX, p.RXPeak = round1(p.RX), round1(p.RXPeak)
	p.TX, p.TXPeak = round1(p.TX), round1(p.TXPeak)
	p.DiskRead, p.DiskReadPeak = round1(p.DiskRead), round1(p.DiskReadPeak)
	p.DiskWrite, p.DiskWritePeak = round1(p.DiskWrite), round1(p.DiskWritePeak)
	p.Load1, p.Load1Peak = round2(p.Load1), round2(p.Load1Peak)
	p.DiskPercent = round1(p.DiskPercent)
}

func round1(f float64) float64 { return math.Round(f*10) / 10 }
func round2(f float64) float64 { return math.Round(f*100) / 100 }

func clamp(v, lo, hi time.Duration) time.Duration {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// ParseWindow reads the range shorthand the UI sends. time.ParseDuration knows
// nothing longer than an hour, and a dashboard whose retention is measured in
// days has to be able to say "7d" without spelling it "168h".
func ParseWindow(s string) (time.Duration, error) {
	if s == "" {
		return 0, fmt.Errorf("empty duration")
	}
	unit := s[len(s)-1]
	var mult time.Duration
	switch unit {
	case 'd':
		mult = 24 * time.Hour
	case 'w':
		mult = 7 * 24 * time.Hour
	default:
		return time.ParseDuration(s)
	}
	// strconv rather than Sscanf: Sscanf stops at the first character it
	// cannot use and reports success, so "7 d" would parse as seven days.
	n, err := strconv.ParseFloat(s[:len(s)-1], 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid duration %q", s)
	}
	return time.Duration(n * float64(mult)), nil
}

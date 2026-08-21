package dockerx

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
)

// ContainerStats mirrors what `docker stats` shows, already reduced to the
// percentages and rates a dashboard needs. Doing the arithmetic here keeps the
// frontend from having to understand cgroup counter semantics.
type ContainerStats struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	TS         time.Time `json:"ts"`
	CPUPercent float64   `json:"cpuPercent"`
	MemUsage   uint64    `json:"memUsage"`
	MemLimit   uint64    `json:"memLimit"`
	MemPercent float64   `json:"memPercent"`
	NetRx      uint64    `json:"netRx"`
	NetTx      uint64    `json:"netTx"`
	BlockRead  uint64    `json:"blockRead"`
	BlockWrite uint64    `json:"blockWrite"`
	PIDs       uint64    `json:"pids"`
	OnlineCPUs uint32    `json:"onlineCpus"`

	// Cumulative CPU counters, carried so a caller sampling repeatedly can
	// work out utilisation itself. They are nanosecond totals since the
	// container started and since the host booted respectively — the same
	// pair `docker stats` divides — and are only meaningful as a difference
	// between two samples.
	CPUTotal  uint64 `json:"cpuTotal"`
	SystemCPU uint64 `json:"systemCpu"`
}

// StatsStream follows one container's stats until the context ends.
func (c *Client) StatsStream(ctx context.Context, id string, out chan<- ContainerStats) error {
	cli, err := c.api()
	if err != nil {
		return err
	}
	resp, err := cli.ContainerStats(ctx, id, true)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	dec := json.NewDecoder(resp.Body)
	for {
		var raw container.StatsResponse
		if err := dec.Decode(&raw); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case out <- convertStats(id, raw):
		}
	}
}

// StatsSampler turns Docker's cheap one-shot stats into current utilisation.
//
// One-shot is the endpoint worth calling for a whole table of containers: it
// answers immediately, where the non-streaming call primes for about a second
// per container. The cost is that it reports no previous sample, so the
// percentage has to be derived from the difference between two of its own
// calls — which is what this holds the counters for.
//
// Unlike sysinfo's collector, sharing one of these between callers is safe.
// CPU percent is a ratio of two deltas measured over the same window, so it
// does not depend on how long that window was; a second caller sampling in
// between makes the next reading noisier, never wrong. Everything else on
// ContainerStats is a cumulative counter, not a rate. A sampler per caller is
// still the better default where the caller has one to spare — a series stored
// for a week deserves its own even intervals — but nothing breaks if it does
// not.
type StatsSampler struct {
	client *Client

	mu   sync.Mutex
	prev map[string]cpuCounters
}

type cpuCounters struct {
	total  uint64
	system uint64
}

func (c *Client) NewStatsSampler() *StatsSampler {
	return &StatsSampler{client: c, prev: map[string]cpuCounters{}}
}

// Sample reads every named container once.
//
// The first call for a container reports no CPU percentage, because there is
// nothing to difference against yet; the caller either primes the sampler or
// accepts one blank frame.
func (s *StatsSampler) Sample(ctx context.Context, ids []string) ([]ContainerStats, error) {
	cli, err := s.client.api()
	if err != nil {
		return nil, err
	}
	out := make([]ContainerStats, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if ctx.Err() != nil {
			break
		}
		resp, err := cli.ContainerStatsOneShot(ctx, id)
		if err != nil {
			continue
		}
		var raw container.StatsResponse
		decErr := json.NewDecoder(resp.Body).Decode(&raw)
		resp.Body.Close()
		if decErr != nil {
			continue
		}
		st := convertStats(id, raw)
		seen[id] = struct{}{}
		s.fillCPU(id, &st, cpuCount(raw))
		out = append(out, st)
	}
	s.forget(seen)
	return out, nil
}

// SampleAll reads every running container, which is what a recorder wants: it
// should follow whatever is up now rather than a list captured at startup.
func (s *StatsSampler) SampleAll(ctx context.Context) ([]ContainerStats, error) {
	list, err := s.client.ListContainers(ctx, false)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(list))
	for _, c := range list {
		if c.State == "running" {
			ids = append(ids, c.ID)
		}
	}
	if len(ids) == 0 {
		s.forget(nil)
		return nil, nil
	}
	return s.Sample(ctx, ids)
}

func (s *StatsSampler) fillCPU(id string, st *ContainerStats, cpus float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	prev, ok := s.prev[id]
	s.prev[id] = cpuCounters{total: st.CPUTotal, system: st.SystemCPU}
	if !ok || st.CPUPercent > 0 {
		// Either nothing to difference against, or the sample already carried
		// its own predecessor because it came from the streaming endpoint.
		return
	}
	st.CPUPercent = cpuPercent(
		float64(st.CPUTotal)-float64(prev.total),
		float64(st.SystemCPU)-float64(prev.system),
		cpus,
	)
}

// forget drops containers that were not in this pass, so a host that recreates
// containers often does not accumulate their counters for the life of the
// process. A nil set clears everything, which is what "nothing is running"
// means.
func (s *StatsSampler) forget(seen map[string]struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id := range s.prev {
		if _, ok := seen[id]; !ok {
			delete(s.prev, id)
		}
	}
}

// cpuPercent is the share of the whole host this container used over the
// window, scaled the way `docker stats` scales it: 100% means one core.
//
// A counter that went backwards means the container was recreated under the
// same id-space or the host rebooted, and reporting the resulting enormous
// delta as a spike would be worse than reporting nothing.
func cpuPercent(cpuDelta, sysDelta, cpus float64) float64 {
	if cpuDelta <= 0 || sysDelta <= 0 || cpus <= 0 {
		return 0
	}
	return round2(cpuDelta / sysDelta * cpus * 100)
}

func cpuCount(raw container.StatsResponse) float64 {
	if raw.CPUStats.OnlineCPUs > 0 {
		return float64(raw.CPUStats.OnlineCPUs)
	}
	return float64(len(raw.CPUStats.CPUUsage.PercpuUsage))
}

func convertStats(id string, raw container.StatsResponse) ContainerStats {
	s := ContainerStats{
		ID:         id,
		Name:       trimName(raw.Name),
		TS:         raw.Read.UTC(),
		MemLimit:   raw.MemoryStats.Limit,
		PIDs:       raw.PidsStats.Current,
		OnlineCPUs: raw.CPUStats.OnlineCPUs,
	}
	// Docker reports total memory including the page cache; subtracting the
	// reclaimable portion is what the CLI does and is what operators expect.
	usage := raw.MemoryStats.Usage
	if cache, ok := raw.MemoryStats.Stats["inactive_file"]; ok && cache < usage {
		usage -= cache
	} else if cache, ok := raw.MemoryStats.Stats["total_inactive_file"]; ok && cache < usage {
		usage -= cache
	}
	s.MemUsage = usage
	if s.MemLimit > 0 {
		s.MemPercent = round2(float64(usage) / float64(s.MemLimit) * 100)
	}

	s.CPUTotal = raw.CPUStats.CPUUsage.TotalUsage
	s.SystemCPU = raw.CPUStats.SystemUsage

	// Network and block totals are cumulative counters and have nothing to do
	// with the CPU delta below, so they are read before the early return that
	// the one-shot path takes. They used to sit after it, which meant every
	// caller that samples rather than streams — the container table, and the
	// recorder that keeps the history — saw a permanent zero for both.
	//
	// An absent `networks` is not the same failure: Docker omits it entirely
	// for a container sharing the host's network namespace, because there is
	// no per-container interface to measure. Nothing is missing there and
	// nothing can be reported.
	for _, n := range raw.Networks {
		s.NetRx += n.RxBytes
		s.NetTx += n.TxBytes
	}
	for _, b := range raw.BlkioStats.IoServiceBytesRecursive {
		switch b.Op {
		case "read", "Read":
			s.BlockRead += b.Value
		case "write", "Write":
			s.BlockWrite += b.Value
		}
	}

	// Only the streaming endpoint fills PreCPUStats. The one-shot endpoint
	// zeroes it, and subtracting zero turns the arithmetic below into "this
	// container's whole lifetime divided by the host's whole uptime" — an
	// average since start, reported as though it were the reading now. On
	// anything long-lived that reads as nearly idle however hard the container
	// is working, so a sample without a predecessor claims no percentage at
	// all and leaves StatsSampler to work it out from two of them.
	if raw.PreCPUStats.SystemUsage == 0 {
		return s
	}
	cpuDelta := float64(raw.CPUStats.CPUUsage.TotalUsage) - float64(raw.PreCPUStats.CPUUsage.TotalUsage)
	sysDelta := float64(raw.CPUStats.SystemUsage) - float64(raw.PreCPUStats.SystemUsage)
	s.CPUPercent = cpuPercent(cpuDelta, sysDelta, cpuCount(raw))
	return s
}

func trimName(n string) string {
	if len(n) > 0 && n[0] == '/' {
		return n[1:]
	}
	return n
}

func round2(f float64) float64 { return float64(int64(f*100+0.5)) / 100 }

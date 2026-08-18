package dockerx

import (
	"context"
	"encoding/json"
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

// StatsOnce samples every container once, which is what the container table
// polls. A per-container follow stream would mean one goroutine and one HTTP
// connection per row.
func (c *Client) StatsOnce(ctx context.Context, ids []string) ([]ContainerStats, error) {
	cli, err := c.api()
	if err != nil {
		return nil, err
	}
	out := make([]ContainerStats, 0, len(ids))
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
		out = append(out, convertStats(id, raw))
	}
	return out, nil
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

	cpuDelta := float64(raw.CPUStats.CPUUsage.TotalUsage) - float64(raw.PreCPUStats.CPUUsage.TotalUsage)
	sysDelta := float64(raw.CPUStats.SystemUsage) - float64(raw.PreCPUStats.SystemUsage)
	cpus := float64(raw.CPUStats.OnlineCPUs)
	if cpus == 0 {
		cpus = float64(len(raw.CPUStats.CPUUsage.PercpuUsage))
	}
	if cpuDelta > 0 && sysDelta > 0 && cpus > 0 {
		s.CPUPercent = round2(cpuDelta / sysDelta * cpus * 100)
	}

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
	return s
}

func trimName(n string) string {
	if len(n) > 0 && n[0] == '/' {
		return n[1:]
	}
	return n
}

func round2(f float64) float64 { return float64(int64(f*100+0.5)) / 100 }

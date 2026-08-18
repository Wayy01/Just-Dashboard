package dockerx

import (
	"context"
	"encoding/json"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
)

type Image struct {
	ID          string            `json:"id"`
	RepoTags    []string          `json:"repoTags"`
	RepoDigests []string          `json:"repoDigests"`
	Size        int64             `json:"size"`
	Created     time.Time         `json:"created"`
	Containers  int64             `json:"containers"`
	Labels      map[string]string `json:"labels"`
	Dangling    bool              `json:"dangling"`
}

func (c *Client) ListImages(ctx context.Context, all bool) ([]Image, error) {
	cli, err := c.api()
	if err != nil {
		return nil, err
	}
	items, err := cli.ImageList(ctx, image.ListOptions{All: all})
	if err != nil {
		return nil, err
	}
	out := make([]Image, 0, len(items))
	for _, it := range items {
		img := Image{
			ID:          it.ID,
			RepoTags:    it.RepoTags,
			RepoDigests: it.RepoDigests,
			Size:        it.Size,
			Created:     time.Unix(it.Created, 0).UTC(),
			Containers:  it.Containers,
			Labels:      it.Labels,
		}
		if len(img.RepoTags) == 0 || (len(img.RepoTags) == 1 && img.RepoTags[0] == "<none>:<none>") {
			img.Dangling = true
		}
		out = append(out, img)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Size > out[j].Size })
	return out, nil
}

// PullProgress is one line of the pull stream, forwarded to the browser so the
// user sees layer progress rather than a spinner.
type PullProgress struct {
	ID       string `json:"id,omitempty"`
	Status   string `json:"status"`
	Progress string `json:"progress,omitempty"`
	Error    string `json:"error,omitempty"`
}

func (c *Client) PullImage(ctx context.Context, ref string, out chan<- PullProgress) error {
	cli, err := c.api()
	if err != nil {
		return err
	}
	rc, err := cli.ImagePull(ctx, ref, image.PullOptions{})
	if err != nil {
		return err
	}
	defer rc.Close()
	dec := json.NewDecoder(rc)
	for {
		var msg struct {
			ID             string `json:"id"`
			Status         string `json:"status"`
			Error          string `json:"error"`
			ProgressDetail struct {
				Current int64 `json:"current"`
				Total   int64 `json:"total"`
			} `json:"progressDetail"`
			Progress string `json:"progress"`
		}
		if err := dec.Decode(&msg); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case out <- PullProgress{ID: msg.ID, Status: msg.Status, Progress: msg.Progress, Error: msg.Error}:
		}
	}
}

func (c *Client) RemoveImage(ctx context.Context, id string, force, pruneChildren bool) ([]string, error) {
	cli, err := c.api()
	if err != nil {
		return nil, err
	}
	res, err := cli.ImageRemove(ctx, id, image.RemoveOptions{Force: force, PruneChildren: pruneChildren})
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, r := range res {
		if r.Deleted != "" {
			out = append(out, "deleted "+r.Deleted)
		}
		if r.Untagged != "" {
			out = append(out, "untagged "+r.Untagged)
		}
	}
	return out, nil
}

// PruneReport is the shared shape for every prune operation so the UI can
// render one confirmation summary regardless of what was reclaimed.
type PruneReport struct {
	Kind           string   `json:"kind"`
	SpaceReclaimed uint64   `json:"spaceReclaimed"`
	Items          []string `json:"items"`
}

// PruneImages removes dangling images by default. Passing all=true removes
// every image not referenced by a container, which is far more destructive and
// is why the handler demands a typed confirmation for it.
func (c *Client) PruneImages(ctx context.Context, all bool) (PruneReport, error) {
	cli, err := c.api()
	if err != nil {
		return PruneReport{}, err
	}
	args := filters.NewArgs()
	args.Add("dangling", boolStr(!all))
	rep, err := cli.ImagesPrune(ctx, args)
	if err != nil {
		return PruneReport{}, err
	}
	out := PruneReport{Kind: "images", SpaceReclaimed: rep.SpaceReclaimed, Items: []string{}}
	for _, d := range rep.ImagesDeleted {
		if d.Deleted != "" {
			out.Items = append(out.Items, "deleted "+d.Deleted)
		}
		if d.Untagged != "" {
			out.Items = append(out.Items, "untagged "+d.Untagged)
		}
	}
	return out, nil
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

type Volume struct {
	Name       string            `json:"name"`
	Driver     string            `json:"driver"`
	Mountpoint string            `json:"mountpoint"`
	CreatedAt  string            `json:"createdAt"`
	Scope      string            `json:"scope"`
	Labels     map[string]string `json:"labels"`
	Size       int64             `json:"size"`
	RefCount   int64             `json:"refCount"`
	InUse      bool              `json:"inUse"`
}

func (c *Client) ListVolumes(ctx context.Context) ([]Volume, error) {
	cli, err := c.api()
	if err != nil {
		return nil, err
	}
	res, err := cli.VolumeList(ctx, volume.ListOptions{})
	if err != nil {
		return nil, err
	}
	// The volume list has no usage data; disk usage does. Joining them is what
	// makes "which volume is safe to delete" answerable in the UI.
	usage := map[string]struct {
		size int64
		refs int64
	}{}
	if du := c.diskUsage(ctx); du != nil {
		for _, v := range du.Volumes {
			if v.UsageData != nil {
				usage[v.Name] = struct {
					size int64
					refs int64
				}{v.UsageData.Size, v.UsageData.RefCount}
			}
		}
	}
	out := make([]Volume, 0, len(res.Volumes))
	for _, v := range res.Volumes {
		vol := Volume{
			Name: v.Name, Driver: v.Driver, Mountpoint: v.Mountpoint,
			CreatedAt: v.CreatedAt, Scope: v.Scope, Labels: v.Labels, RefCount: -1,
		}
		if u, ok := usage[v.Name]; ok {
			vol.Size = u.size
			vol.RefCount = u.refs
			vol.InUse = u.refs > 0
		}
		out = append(out, vol)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (c *Client) InspectVolume(ctx context.Context, name string) (volume.Volume, error) {
	cli, err := c.api()
	if err != nil {
		return volume.Volume{}, err
	}
	return cli.VolumeInspect(ctx, name)
}

func (c *Client) RemoveVolume(ctx context.Context, name string, force bool) error {
	cli, err := c.api()
	if err != nil {
		return err
	}
	return cli.VolumeRemove(ctx, name, force)
}

func (c *Client) PruneVolumes(ctx context.Context) (PruneReport, error) {
	cli, err := c.api()
	if err != nil {
		return PruneReport{}, err
	}
	// "all" is required to reach named volumes; without it Docker only
	// considers anonymous ones, which is rarely what the operator meant.
	args := filters.NewArgs()
	args.Add("all", "true")
	rep, err := cli.VolumesPrune(ctx, args)
	if err != nil {
		return PruneReport{}, err
	}
	return PruneReport{Kind: "volumes", SpaceReclaimed: rep.SpaceReclaimed, Items: rep.VolumesDeleted}, nil
}

type Network struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Driver     string            `json:"driver"`
	Scope      string            `json:"scope"`
	Internal   bool              `json:"internal"`
	Attachable bool              `json:"attachable"`
	IPv6       bool              `json:"ipv6"`
	Created    time.Time         `json:"created"`
	Labels     map[string]string `json:"labels"`
	Subnets    []string          `json:"subnets"`
	Containers int               `json:"containers"`
}

func (c *Client) ListNetworks(ctx context.Context) ([]Network, error) {
	cli, err := c.api()
	if err != nil {
		return nil, err
	}
	items, err := cli.NetworkList(ctx, network.ListOptions{})
	if err != nil {
		return nil, err
	}
	out := make([]Network, 0, len(items))
	for _, n := range items {
		nw := Network{
			ID: n.ID, Name: n.Name, Driver: n.Driver, Scope: n.Scope,
			Internal: n.Internal, Attachable: n.Attachable, IPv6: n.EnableIPv6,
			Created: n.Created.UTC(), Labels: n.Labels, Subnets: []string{},
			Containers: len(n.Containers),
		}
		for _, cfg := range n.IPAM.Config {
			if cfg.Subnet != "" {
				nw.Subnets = append(nw.Subnets, cfg.Subnet)
			}
		}
		out = append(out, nw)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (c *Client) InspectNetwork(ctx context.Context, id string) (network.Inspect, error) {
	cli, err := c.api()
	if err != nil {
		return network.Inspect{}, err
	}
	return cli.NetworkInspect(ctx, id, network.InspectOptions{Verbose: true})
}

func (c *Client) RemoveNetwork(ctx context.Context, id string) error {
	cli, err := c.api()
	if err != nil {
		return err
	}
	return cli.NetworkRemove(ctx, id)
}

type DiskUsage struct {
	LayersSize int64 `json:"layersSize"`
	Images     int64 `json:"imagesSize"`
	Containers int64 `json:"containersSize"`
	Volumes    int64 `json:"volumesSize"`
	BuildCache int64 `json:"buildCacheSize"`
}

func (c *Client) DiskUsage(ctx context.Context) (DiskUsage, error) {
	if _, err := c.api(); err != nil {
		return DiskUsage{}, err
	}
	// Shares the cache with the volume list: the same multi-second walk of
	// every layer and volume answers both, and it is not worth doing twice.
	du := c.diskUsage(ctx)
	if du == nil {
		return DiskUsage{}, ErrUnavailable
	}
	out := DiskUsage{LayersSize: du.LayersSize}
	for _, i := range du.Images {
		out.Images += i.Size
	}
	for _, ct := range du.Containers {
		out.Containers += ct.SizeRw
	}
	for _, v := range du.Volumes {
		if v.UsageData != nil {
			out.Volumes += v.UsageData.Size
		}
	}
	for _, b := range du.BuildCache {
		out.BuildCache += b.Size
	}
	return out, nil
}

// PruneAll is the "reclaim everything" button: stopped containers, dangling
// images, unused volumes and the build cache, reported as separate lines so
// the operator can see exactly what went.
func (c *Client) PruneAll(ctx context.Context, includeVolumes, allImages bool) ([]PruneReport, error) {
	cli, err := c.api()
	if err != nil {
		return nil, err
	}
	reports := []PruneReport{}

	if rep, err := cli.ContainersPrune(ctx, filters.NewArgs()); err == nil {
		reports = append(reports, PruneReport{
			Kind: "containers", SpaceReclaimed: rep.SpaceReclaimed, Items: rep.ContainersDeleted,
		})
	}
	if rep, err := c.PruneImages(ctx, allImages); err == nil {
		reports = append(reports, rep)
	}
	if includeVolumes {
		if rep, err := c.PruneVolumes(ctx); err == nil {
			reports = append(reports, rep)
		}
	}
	if rep, err := c.pruneNetworks(ctx); err == nil {
		reports = append(reports, rep)
	}
	return reports, nil
}

func (c *Client) pruneNetworks(ctx context.Context) (PruneReport, error) {
	cli, err := c.api()
	if err != nil {
		return PruneReport{}, err
	}
	rep, err := cli.NetworksPrune(ctx, filters.NewArgs())
	if err != nil {
		return PruneReport{}, err
	}
	return PruneReport{Kind: "networks", Items: rep.NetworksDeleted}, nil
}

// ImageRef normalises a user-supplied reference so "nginx" pulls nginx:latest
// rather than failing, matching CLI behaviour.
func ImageRef(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ref
	}
	lastColon := strings.LastIndex(ref, ":")
	lastSlash := strings.LastIndex(ref, "/")
	if lastColon <= lastSlash {
		return ref + ":latest"
	}
	return ref
}

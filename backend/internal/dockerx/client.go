// Package dockerx talks to the Docker Engine API over its unix socket through
// the official Go SDK. Nothing here shells out to the docker CLI: the socket
// is the interface, and shelling out would both lose typed errors and widen
// the command-injection surface.
//
// Access to /var/run/docker.sock is root-equivalent — a caller who can create
// a container can bind-mount the host root and escape. Every route that
// reaches this package therefore sits behind authentication, a capability
// check and the audit trail, and the destructive ones behind a typed
// confirmation as well.
package dockerx

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/docker/docker/api/types/system"
	"github.com/docker/docker/client"
)

var ErrUnavailable = errors.New("docker is not available on this host")

type Client struct {
	mu   sync.RWMutex
	cli  *client.Client
	host string
	err  error
}

// New never fails hard: a host without Docker should still serve the rest of
// the dashboard. The connection error is retained and surfaced per request.
func New(host string) *Client {
	c := &Client{host: host}
	cli, err := client.NewClientWithOpts(
		client.WithHost(host),
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		c.err = err
		return c
	}
	c.cli = cli
	return c
}

func (c *Client) api() (*client.Client, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.cli == nil {
		if c.err != nil {
			return nil, fmt.Errorf("%w: %v", ErrUnavailable, c.err)
		}
		return nil, ErrUnavailable
	}
	return c.cli, nil
}

func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cli != nil {
		return c.cli.Close()
	}
	return nil
}

type Availability struct {
	Available     bool   `json:"available"`
	Host          string `json:"host"`
	Error         string `json:"error,omitempty"`
	ServerVersion string `json:"serverVersion,omitempty"`
	APIVersion    string `json:"apiVersion,omitempty"`
}

// Ping is what the UI calls before rendering the Docker section, so an
// unreachable daemon reads as "not configured" rather than a wall of errors.
func (c *Client) Ping(ctx context.Context) Availability {
	a := Availability{Host: c.host}
	cli, err := c.api()
	if err != nil {
		a.Error = err.Error()
		return a
	}
	p, err := cli.Ping(ctx)
	if err != nil {
		a.Error = err.Error()
		return a
	}
	a.Available = true
	a.APIVersion = p.APIVersion
	if info, err := cli.ServerVersion(ctx); err == nil {
		a.ServerVersion = info.Version
	}
	return a
}

func (c *Client) Info(ctx context.Context) (system.Info, error) {
	cli, err := c.api()
	if err != nil {
		return system.Info{}, err
	}
	return cli.Info(ctx)
}

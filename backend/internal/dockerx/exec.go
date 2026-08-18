package dockerx

import (
	"context"
	"errors"
	"io"

	"github.com/docker/docker/api/types/container"
)

// ExecSession is an interactive shell inside a container. It is functionally
// equivalent to handing the caller a root shell on the host — a container can
// be started privileged, and the socket that created this session can create
// another one with the host filesystem mounted. It is gated on the terminal
// capability and recorded in the audit trail at open time.
type ExecSession struct {
	ID     string
	Conn   io.ReadWriteCloser
	client *Client
}

var ErrNoShell = errors.New("no usable shell found in container (tried bash, sh)")

// Exec starts a shell. When no command is given it probes for bash and falls
// back to sh, which is the behaviour operators expect from a "connect" button.
func (c *Client) Exec(ctx context.Context, id string, cmd []string, user string, rows, cols uint) (*ExecSession, error) {
	cli, err := c.api()
	if err != nil {
		return nil, err
	}
	if len(cmd) == 0 {
		shell, err := c.detectShell(ctx, id)
		if err != nil {
			return nil, err
		}
		cmd = []string{shell}
	}
	created, err := cli.ContainerExecCreate(ctx, id, container.ExecOptions{
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          true,
		User:         user,
		Cmd:          cmd,
		Env:          []string{"TERM=xterm-256color"},
	})
	if err != nil {
		return nil, err
	}
	att, err := cli.ContainerExecAttach(ctx, created.ID, container.ExecAttachOptions{Tty: true})
	if err != nil {
		return nil, err
	}
	sess := &ExecSession{ID: created.ID, Conn: att.Conn, client: c}
	if rows > 0 && cols > 0 {
		sess.Resize(ctx, rows, cols)
	}
	return sess, nil
}

func (c *Client) detectShell(ctx context.Context, id string) (string, error) {
	cli, err := c.api()
	if err != nil {
		return "", err
	}
	for _, sh := range []string{"/bin/bash", "/bin/sh"} {
		created, err := cli.ContainerExecCreate(ctx, id, container.ExecOptions{
			Cmd: []string{sh, "-c", "exit 0"},
		})
		if err != nil {
			continue
		}
		att, err := cli.ContainerExecAttach(ctx, created.ID, container.ExecAttachOptions{})
		if err != nil {
			continue
		}
		io.Copy(io.Discard, att.Reader)
		att.Close()
		insp, err := cli.ContainerExecInspect(ctx, created.ID)
		if err == nil && insp.ExitCode == 0 {
			return sh, nil
		}
	}
	return "", ErrNoShell
}

func (s *ExecSession) Resize(ctx context.Context, rows, cols uint) error {
	cli, err := s.client.api()
	if err != nil {
		return err
	}
	return cli.ContainerExecResize(ctx, s.ID, container.ResizeOptions{Height: rows, Width: cols})
}

func (s *ExecSession) Close() error { return s.Conn.Close() }
